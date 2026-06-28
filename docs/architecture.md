# Architecture

Component map, ownership rules, and execution flow for minivm.

## Agent Quick Map

Read when a change crosses package boundaries or depends on state ownership.

| Touch | Also read |
|---|---|
| `interp/` runtime state, frames, globals | `memory-model.md`, `value-representation.md` |
| `interp/` debugger API or bytecode stepping | `debugging.md`, `profile.md` |
| `interp.Tracer` or profile options | `profile.md` |
| `interp/threaded.go` or `interp/jit*.go` | `jit-internals.md`, `instruction-set.md` |
| `analysis/`, `transform/`, `optimize/`, `pass/` | `pass-system.md` |
| `program/verify.go` or admitting untrusted bytecode | `verification.md` |
| `cli/` or `cmd/minivm/` | `guides/repl.md` |

Boundary rules: `instr` stays leaf-like, `types` must not import `interp`, and optimizer code flows through `pass.Pipeline` + `pass.Manager`.

## Package Dependency Graph

Import direction: `A → B` means `A` imports `B`.

```text
types   → instr
program → instr, types
prof → instr
asm/amd64 → asm
asm/arm64 → asm
interp  → program, instr, types, asm, asm/arm64, pass, analysis, prof
debug → interp
analysis → pass, types, instr
transform → analysis, pass, types, instr, program
optimize → transform, analysis, pass, program
cli → debug, instr, interp, prof, program, types, cobra
cmd/minivm → cli
```

`program/verify.go` (the bytecode verifier) deliberately does **not** import
`analysis`/`pass`: those packages' tests import `program`, so an edge back would
cycle. It inlines its own basic-block construction instead.

## Component Responsibilities

### `program/`

`program.Program` is the hand-off between bytecode producers and the VM:

```go
type Program struct {
    Code      []byte
    Locals    []types.Type
    Constants []types.Value
    Types     []types.Type
}
```

`Code` holds top-level bytecode. `Locals` declares the entry frame's local scratch slots, so module-level code addresses temporaries with `LOCAL_*` exactly as a function does — a compiler keeps top-level temporaries in frame locals instead of reserving globals. `Constants` hold functions, strings, arrays, and other values. `Types` holds descriptors for `ARRAY_NEW` and `STRUCT_NEW`. `*types.Function` constants have their own `Code []byte`. `interp.New()` compiles function constants into dispatch slots keyed by their heap ref; slot `0` is program code, framed with `Locals` reserved on the stack at entry.

`program.Builder` is the high-level way to author a `Program`. It wraps an `instr.Builder` (label-patching assembler: branch to a `Label`, offsets back-patched on `Build`) and interned constant/type pools (`Const`/`ConstGet`/`Type` dedup by `String()` and return a stable index), so authors never hand-compute branch byte offsets or pool indices. `types.FunctionBuilder` carries the same `Label`/`Bind`/`Br`/`BrIf`/`BrTable` methods for function bodies. Branch targets are PC-relative signed-i16 byte offsets relative to the end of the branch instruction (see `interp/threaded.go`); `instr.Builder.Assemble` is the single source of that encoding.

### `instr/`

Instruction set: byte-sized `Opcode`. Each opcode has `Type` metadata in `instr/type.go`: mnemonic plus `Widths []int` for variable-width encoding/decoding.

- `Marshal([]Instruction) []byte`: serialize.
- `Unmarshal([]byte) []Instruction`: deserialize.
- `Format([]byte) string`: debug text.
- `Parse(line string) (Instruction, error)`: parse plain or offset-prefixed lines, e.g. `i32.const 42` or `0000:\ti32.const 0x0000002a`.
- `ParseAll(r io.Reader) ([]Instruction, error)`: parse line-by-line, skipping blanks.

### `types/`

Two layers:

1. `types.Value`: runtime value with `Kind()`, `Type()`, `String()`.
2. `types.Type`: descriptor with `Kind()`, `Cast(Type)`, `Equals(Type)`.

`types.Boxed` (`uint64`) is the VM stack/global currency. Heap objects are `types.Value` references carried as `KindRef` boxed values. See `value-representation.md`.

`types.Traceable` marks heap objects containing refs (`Array`, `Struct`, `Map`, `HostObject`). GC walks them via `Refs() []Ref`; implementations do not allocate a result slice until they find a nested ref. `STRUCT_GET`/`STRUCT_SET` handle native `*types.Struct` first, then use `HostObject` for host values.

### `interp/`

`Interpreter` owns runtime state:

| Field | Purpose |
|---|---|
| `instrs [][]byte` | raw bytecode per function slot |
| `code [][]func(*Interpreter)` | threaded closures per function slot |
| `tracer *Tracer` | shared trace/profile front-end and aggregate profile |
| `local *stats` | interpreter-local function, IP, opcode, JIT samples |
| `frames []frame` | call stack: `addr`, `ip`, `bp` |
| `stack []Boxed` | value stack |
| `heap []Value` | flat heap |
| `rc []int` | ref counts parallel to heap |
| `free []int` | free heap indices |
| `globals []Boxed` | globals |
| `compiler *compiler` | private JIT compiler and executable buffer for solo interpreters |
| `cache *Cache` | shared compiled native modules when owned by an `interp.Pool` |

`threadedCompiler` (`threaded.go`): `[256]func` table populated in `init()`. Each compile-time entry reads operands, advances `c.ip`, and returns a runtime closure that captures constants and advances `f.ip` by instruction width.

`Tracer` (`trace.go`): samples execution, records entry traces, loop-header traces, and hot side exits, and aggregates pool profile data.

`compiler` (`jit.go`, `jit_arm64.go`): ARM64 JIT kept private inside `interp`. A hot attempt records the current threaded execution path, then lowers each usable root — the function entry and any hot loop header — through a typed switch. Native code uses a single context pointer in X0, loads VM stack inputs directly from journal-provided scratch registers, keeps operands in registers, and materializes `i.stack`, `sp`, and `ip` only on returns, guards, yields, or deopt exits. Recorded direct calls, recursive calls, guarded indirect function-value calls, closure-body upvalues, scalar globals/locals, selected read-only heap paths including `error.get`, and loops (native back-edge with a safepoint poll) can run natively. Unsupported paths stay threaded through guard fallback; `error.new`, `error.code`, and `throw` are terminal deopt boundaries. The JIT does not tier beyond the ARM64 trace backend.

`HostFunction` (`host.go`): wraps `func(i *Interpreter, params []Boxed) ([]Boxed, error)` as `types.Value`. Lives in constants, called by `CONST_GET` + `CALL`. Use `Interpreter.Marshal`/`Unmarshal` to convert Go values; the default converter caches per-type reflection plans, while arbitrary Go function calls still use `reflect.Call`. Go `func` marshals to `HostFunction`, final `error` return propagated as host-call error. `WithMarshaler` replaces the default converter.

Dynamic `*types.Function` values can be installed through `Interpreter.Alloc` or
`Interpreter.Store`. The interpreter keeps callable dispatch slots keyed by heap
address in sync with function heap rows, and removes dispatch slots when the heap
row is overwritten or reclaimed. It trusts installed functions; verify untrusted
bytecode with `program.Verify` before installing it.

`Coroutine` (`coroutine.go`): `yield`/`resume` suspension. A `YIELD` in the entry frame (`fp == 1`) is an **interpreter escape**: it panics the private `errYield`, `Run` recovers it and returns the exported `ErrYield` without wrapping or losing state, and the next `Run` resumes after the `YIELD` (the host swaps the stack-top value to feed the result back in). A `YIELD` inside a called function is an **asymmetric coroutine** yield. A function whose body contains `YIELD` is a coroutine-function (scanned once at `New` into `coros []bool`, parallel to `code`); its `CALL` allocates a `Coroutine` heap handle and tags the new `frame.coro`, so the call always yields a single handle instead of plain returns. `suspend` captures the one frame (its `ip`, function ref, upvals, and `stack[bp:sp]` image, ownership moved to the handle) and unwinds to the caller; `resume` restores the frame above the resumer's stack and delivers the in-value as the pending `YIELD`'s result; `finish` (`RETURN` with `frame.coro != 0`) records the final value, marks the handle done, and delivers it. `coro.done`/`coro.value` read status and last value. The collector roots every active frame's `ref` and `coro`, and the handle's `Refs` keeps the suspended image live. `YIELD`/`RESUME` cannot run natively, so a trace that reaches one in its anchor frame records it as a **terminal** and the JIT lowers it to an unconditional deopt that hands the real suspend/resume to the threaded handler (resume IP is the op itself, since each handler does its own `ip++`); a suspension inside an inlined callee frame still aborts the trace, because deopt rebuilds inlined frames without their `coro` handle and the threaded handler would mis-read. Coroutine-functions' fused `CONST_GET; CALL` superinstruction stays suppressed so the generic coroutine `CALL` path runs. `CORO_DONE`/`CORO_VALUE` are pure heap reads that the JIT lowers natively behind an itab guard, like `STRUCT_GET`/`REF_GET`.

`HostObject` (`host.go`): wraps a Go value that carries methods or unexported fields. `STRUCT_GET`/`STRUCT_SET` use it as the concrete host-value fallback after the native `*types.Struct` fast path. Field reads/writes use compiled metadata and unsafe offsets against an internal addressable copy of the receiver; methods are pre-bound as `*HostFunction` values allocated on the VM heap.

`Pool` (`pool.go`): multi-goroutine entry point. `Interpreter` is single-goroutine; `NewPool(prog, size, opts...)` lazily lends up to `size` interpreters that share one `Cache`. The cache aggregates JIT trigger samples, compiles each hot function once, publishes immutable native modules, and exposes `Pool.Profile()` from member profiles flushed on `Put`/`Close`. Use `Get`/`Put` or `Run(ctx, fn)` to borrow one interpreter per goroutine. `Get` checks the closed state, takes an idle interpreter, and grows under the pool read lock; only the blocking wait runs unlocked and observes `Close` through the closed idle channel. `Put` flushes profile samples, calls `Reset`, and returns the interpreter to idle storage. `Close` releases idle interpreters and drops the cache owner reference; executable buffers are freed only after the cache owner and all outstanding interpreters are closed. Heap refs from a borrowed interpreter are invalid after `Put` because `Reset` wipes the heap.

### `debug/`

`Debugger` is the instruction-accurate control layer over `interp`. It owns
breakpoints and stepping state, installs through `interp.WithDebugger`, and
forces `WithTick(1)` plus JIT disablement so `Step`, `Next`, and `Finish` stop on
bytecode boundaries rather than trace exits. Keep debugger policy out of the
interpreter's hot threaded handlers; the interpreter only calls into debugger
hooks at tick/dispatch boundaries.

### `prof/`

`prof.Collector` stores local samples and named metrics. `prof.Profiler` is the
locked aggregate used by shared tracers and pools. Function/IP/opcode samples are
plain counters; JIT activity is exported through the same metric list, so CLI and
embedding code read one reporting surface.

### `asm/`

`Assembler`: low-level IR emission. It allocates VRegs, emits instructions, and declares ABI boundaries. It has no VM stack semantics.

Core API:

- `NewVReg(type,width)`: allocate virtual register.
- `Emit(inst)`: append instruction.
- `Pin(vreg, preg)`: bind VReg to physical register.
- `Entry(label)`: declare a callable internal entry.
- `Build()`: allocate physical regs, encode bytes, and return `*Code`.
- `Link(buf, arch, codes, resolve)`: patch relocations and return native `Callable`s.

`asm.Arch` supplies register info, encoder, ABI, and an optional spill `Frame`. `Frame()` returns nil when the backend does not support spill slots.

`Build()` + `Link()`:

1. `Assembler.Build`: rewrite VRegs to physical registers, insert spill frame code if needed, encode IR to machine code, and record relocations.
2. `resolve()`: two-pass encode. Pass 1 measures sizes via `Imm(0)` placeholders; Pass 2 patches labels and records `Relocation`s.
3. Return `*Code` with bytes, optional entry labels, and relocations.
4. `Link`: writes code into the executable buffer via `buffer.Write` (which brackets the copy in its own W^X unseal/seal), patches external relocations through `buffer.writeAt`, and creates primary/internal `Callable`s.

### `asm/arm64/`

Exposes the `asm.Arch` singleton and encoder/caller, with unexported adapters implementing `asm.ABI` and `asm.Frame`.

`Caller` invokes native chunks through `abi_arm64.s`. The trampoline passes `&i.journal[0]` in X0, preserves X19-X26 for AAPCS64/Go caller safety, and leaves context loading to native entry prologues. There is no param/return marshal path.

The native trampoline uses `abi_arm64.go`/`abi_arm64.s` behind `//go:build arm64`; `abi_stub.go` with `//go:build !arm64` keeps other platforms compilable.

### `asm/amd64/`

Exposes an `asm.Arch` placeholder only. Register metadata exists so the generic
`asm` surface stays portable, but the encoder and ABI return
`asm.ErrNotImplemented`; no minivm JIT path emits amd64 native code yet.

### `pass/`

Generics-based, modeled on LLVM's new pass manager (see `pass-system.md`):

- `pass.Manager`: lazy analysis cache. `Register[U,R]` adds an `Analysis[U,R]`;
  `GetResult[R](m, unit)` runs/caches by result type and unit; `Invalidate` drops
  non-preserved results. Only reflection is `reflect.TypeFor[R]()` as a map key.
- `pass.Pipeline[U]`: ordered transform sequence. `AddPass` appends; `Run(m, unit)`
  runs each `Pass[U]` and invalidates between them by its returned `Preserved`.

Transforms request analyses through the manager; analyses are recomputed after a
transform mutates code.

### `analysis/`, `transform/`, `optimize/`

`BasicBlocksAnalysis` underpins JIT + optimizer. Boundaries at code start, after `BR`/`BR_IF`/`BR_TABLE`/`UNREACHABLE`/`RETURN`, at every jump target.

`optimize.NewOptimizer(level)` builds a cumulative pipeline:

```text
O1  ConstantFoldingPass → ConstantDeduplicationPass
O2  ConstantFoldingPass → AlgebraicSimplificationPass → ConstantDeduplicationPass → DeadCodeEliminationPass
O3  ConstantFoldingPass → AlgebraicSimplificationPass → GlobalValueNumberingPass → ConstantDeduplicationPass → DeadCodeEliminationPass
```

Transform passes mutate `*program.Program` in-place: edit `prog.Code` bytes and `prog.Constants`.

#### `program/verify.go`

Static pre-execution validator (a single cohesive file in `program`).
`program.Verify(prog, opts...)` proves each function slot decodes within bounds,
references valid opcodes and in-range pool/local/upval indices, has valid branch
targets and proper termination, and (where statically determinable) keeps the
operand stack balanced and type-consistent. Per-opcode stack effects live on
`instr.Type` (`Pop`/`Push`); the abstract kind lattice reuses `types.Kind` (an
alias of `instr.Kind`) rather than a parallel enum. It builds its own CFG inline
instead of importing `analysis`/`pass` (which would cycle through `program`).
Verification is decoupled from `interp.New`; callers run it first. See
`verification.md`.

### `cli/`

Top-level command tree, interactive REPL, and shared stack/value formatting live flat in one package. `cli.Root()` returns the `minivm` cobra command (REPL by default) plus subcommands. `cli.NewRunCommand(fs.FS)` builds `run <file>` and accepts any `io/fs.FS`, so embedders can use `os.DirFS`, `embed.FS`, or `fstest.MapFS`. `cli.NewREPL(in, out, fs WriteFS)` constructs the REPL directly; pass `nil` to disable `.load`/`.save`. `cli.WriteFS` extends `fs.FS` with `Create`; `cli.OS()` returns the host filesystem. `cli.WithFS(WriteFS)` overrides the filesystem used by `run` and the REPL's `.load` / `.save`.

`REPL` state between steps:

| Field | Purpose |
|---|---|
| `instrs []instr.Instruction` | instruction history for `.show` / `.reset` |
| `codeLen int` | byte length of history for absolute branch normalization |
| `constants []types.Value` | `.const` function constants |
| `types []types.Type` | `.type` descriptors |
| `fs WriteFS` | filesystem used by `.load` and `.save`; nil disables both commands (`cli.Root` injects `cli.OS()`) |

Each instruction builds a fresh `program.Program` from history plus the new instruction, creates a new `interp.Interpreter`, runs the full program, and prints the stack. Accepted instructions/constants/types stay as source history. Heap is recreated each step, so refs stay valid. Cost is `O(N)` per step for `N` accumulated instructions.

On error, the new instruction is not committed. `.reset` clears instruction history, code length, constants, and types. `.load <file>` parses a `Program.String()` dump and *replaces* state; merging would require renumbering instruction-embedded constant/type indices. `.save <file>` writes `r.build().String()` and refuses host-typed constants because they have no textual form.

Stack/value formatting (`printStack`, `formatValue` in `cli/display.go`) is unexported; both the `run` subcommand and the REPL call into it directly.

### `cmd/minivm/`

Thin entrypoint around `cli.Root().Execute()`.

## Execution Flow

```text
1. program.New(instrs, options...)
   └─ instr.Marshal(instrs) → prog.Code

2. program.Verify(prog, opts...) [optional, for untrusted bytecode]
   └─ *VerifyError on malformed bytecode, before any interpreter is built

3. optimize.Optimize(prog) [optional AOT]
   └─ CF → (AS) → (GVN) → CD → (DCE), each CFG pass requesting BasicBlocksAnalysis

4. interp.New(prog, opts...) → *Interpreter
   ├─ threadedCompiler.Compile(prog.Code) → i.code[0]
   └─ for each *Function constant:
      threadedCompiler.Compile(fn.Code) → i.code[fn heap ref]

5. interp.Run(ctx)
   ├─ main loop: code[f.ip](i)
   ├─ every 128 instructions:
   │  check ctx, consume fuel, call hook, local.Add(addr, ip, opcode)
   └─ if JIT enabled and solo local.Samples(addr), or pool cache trigger count, reaches threshold rounded to tick cadence:
      Tracer records entry trace, then compiler.Compile lowers it privately or publishes one shared Cache module
         ├─ each trace compiles once and exits through journal SP/IP
         ├─ branches either stay in native code or exit to threaded successors
         ├─ assembler.Link → primary/internal context-pointer Callables
         └─ i.code[entryIP] = closure(callable) per accepted entry

6. interp.Close()
   └─ buffer.Free() → munmap
```

`WithThreshold(0)` enables JIT on the first sample. Negative thresholds disable JIT. Bytecode debugging uses `NewDebugger` via `WithDebugger`, enabling instruction-accurate hooks and disabling JIT for exact instruction-boundary stops.

## Focus Areas

| Area | Direction |
|---|---|
| JIT coverage | recorded numeric traces, direct calls, small function-value indirect dispatches, closure-body upvalues, guarded ref slots/upvalues, selected heap reads including `error.get`, and exception terminal fallbacks compile or deopt cleanly; host calls, heap allocation/mutation, maps, and unsupported heap shapes fall back |
| Architecture support | ARM64 optimized; `asm/amd64` is a placeholder until users + benchmarks justify x86-64 JIT |
| Benchmarks | broaden numeric loops, host calls, heap-object workloads |
| Program format | keep `instr`/`program` as compact Go-native bytecode surface |
| Host integration | keep `interp.NewHostFunction` as primary call bridge; use `Marshal`/`Unmarshal` for Go value conversion |
| Resource policy | document how `context.Context`, fuel, hooks, stack/heap/frame limits, host policy work together |

See `docs/roadmap.md` for current priorities.
