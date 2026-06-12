# Architecture

Component map, ownership rules, and execution flow for minivm.

## Agent Quick Map

Read when a change crosses package boundaries or depends on state ownership.

| Touch | Also read |
|---|---|
| `interp/` runtime state, frames, globals | `memory-model.md`, `value-representation.md` |
| `interp/` debugger API or bytecode stepping | `debugging.md`, `profile.md` |
| `prof/` or profile options | `profile.md` |
| `interp/threaded.go` or `interp/jit*.go` | `jit-internals.md`, `instruction-set.md` |
| `analysis/`, `transform/`, `optimize/`, `pass/` | `pass-system.md` |
| `cli/` or `cmd/minivm/` | `guides/repl.md` |

Boundary rules: `instr` stays leaf-like, `types` must not import `interp`, and optimizer code flows through `pass.Manager`.

## Package Dependency Graph

Import direction: `A → B` means `A` imports `B`.

```text
program → instr
interp  → program, instr, types, asm, prof, pass, analysis
asm/arm64 → asm
analysis → pass, types, instr
transform → analysis, pass, types, instr, program
optimize → transform, analysis, pass, program
cli → instr, interp, prof, program, types, cobra
cmd/minivm → cli
```

Core paths:

```text
program → instr → nothing
interp → program, instr, types, asm, pass, analysis
asm/arm64 → asm
optimize → transform → analysis → pass
cli → instr, interp, program, types, cobra
```

## Component Responsibilities

### `program/`

`program.Program` is the hand-off between bytecode producers and the VM:

```go
type Program struct {
    Code      []byte
    Constants []types.Value
    Types     []types.Type
}
```

`Code` holds top-level bytecode. `Constants` hold functions, strings, arrays, and other values. `Types` holds descriptors for `ARRAY_NEW` and `STRUCT_NEW`. `*types.Function` constants have their own `Code []byte`. `interp.New()` compiles function constant `j` into slot `j+1`; slot `0` is program code.

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
| `prof *prof.Stats` | aggregate function, IP, opcode, JIT samples |
| `frames []frame` | call stack: `addr`, `ip`, `bp` |
| `stack []Boxed` | value stack |
| `heap []Value` | flat heap |
| `rc []int` | ref counts parallel to heap |
| `free []int` | free heap indices |
| `globals []Boxed` | globals |
| `compiler *jitCompiler` | private JIT compiler and executable buffer for solo interpreters |
| `cache *Cache` | shared JIT module/profile cache when owned by an `interp.Pool` |

`threadedCompiler` (`threaded.go`): `[256]func` table populated in `init()`. Each compile-time entry reads operands, advances `c.ip`, and returns a runtime closure that captures constants and advances `f.ip` by instruction width.

`jitCompiler` (`jit.go`, `jit_arm64.go`): ARM64 JIT kept private inside `interp`. Its opcode walk intentionally mirrors `threadedCompiler`: decode opcode, lower through a switch, advance IP unless the opcode terminates the segment. Native code uses a single context pointer in X0, loads VM stack inputs directly from journal-provided scratch registers, keeps operands in registers, and materializes `i.stack`, `sp`, and `ip` only on exits. Complete numeric direct-call closures, including recursive SCCs, lower `CONST_GET function; CALL` to native `BL`; native calls consume a journal-carried frame budget and trap back to `ErrFrameOverflow` when exhausted. Incomplete functions still compile supported partial segments around threaded calls. JIT does not recompile or tier-up.

`HostFunction` (`host.go`): wraps `func(i *Interpreter, params []Boxed) ([]Boxed, error)` as `types.Value`. Lives in constants, called by `CONST_GET` + `CALL`. Use `Interpreter.Marshal`/`Unmarshal` to convert Go values; the default converter caches per-type reflection plans, while arbitrary Go function calls still use `reflect.Call`. Go `func` marshals to `HostFunction`, final `error` return propagated as host-call error. `WithMarshaler` replaces the default converter.

`HostObject` (`host.go`): wraps a Go value that carries methods or unexported fields. `STRUCT_GET`/`STRUCT_SET` use it as the concrete host-value fallback after the native `*types.Struct` fast path. Field reads/writes use compiled metadata and unsafe offsets against an internal addressable copy of the receiver; methods are pre-bound as `*HostFunction` values allocated on the VM heap.

`Pool` (`pool.go`): multi-goroutine entry point. `Interpreter` is single-goroutine; `NewPool(prog, size, opts...)` lazily lends up to `size` interpreters that share one `Cache`. The cache aggregates JIT trigger samples, compiles each hot function once, publishes immutable native modules, and exposes `Pool.Profile()` from member profiles flushed on `Put`/`Close`. Use `Get`/`Put` or `Run(ctx, fn)` to borrow one interpreter per goroutine. `Put` flushes profile samples, calls `Reset`, and returns the interpreter to idle storage. `Close` releases idle interpreters and drops the cache owner reference; executable buffers are freed only after the cache owner and all outstanding interpreters are closed. Heap refs from a borrowed interpreter are invalid after `Put` because `Reset` wipes the heap.

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

### `pass/`

`pass.Manager`: reflection-based pipeline dispatcher.

- `Register(pass)`: key pass by `Run` return type.
- `Run(value)`: seed cache with input.
- `Load(&result)`: run/cached-load passes producing `typeof(result)`.
- `Convert(src,dst)`: child manager runs `src`, then loads `dst`.

Passes communicate through manager outputs. Downstream passes `Load` from upstream outputs. Each pass runs at most once per `Manager.Run`.

### `analysis/`, `transform/`, `optimize/`

`BasicBlocksPass` underpins JIT + optimizer. Boundaries at code start, after `BR`/`BR_IF`/`BR_TABLE`/`UNREACHABLE`/`RETURN`, at every jump target.

`optimize.NewOptimizer(O1)` order:

```text
BasicBlocksPass → ConstantFoldingPass → ConstantDeduplicationPass → DeadCodeEliminationPass
```

Transform passes mutate `*program.Program` in-place: edit `prog.Code` bytes and `prog.Constants`.

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

2. optimize.Optimize(prog) [optional AOT]
   └─ BasicBlocksPass → CF → CD → DCE

3. interp.New(prog, opts...)
   ├─ threadedCompiler.Compile(prog.Code) → i.code[0]
   └─ for each *Function constant j:
      threadedCompiler.Compile(fn.Code) → i.code[j+1]

4. interp.Run(ctx)
   ├─ main loop: code[f.ip](i)
   ├─ every 128 instructions:
   │  check ctx, consume fuel, call hook, prof.Add(addr, ip, opcode)
   └─ if JIT enabled and solo prof.Samples(addr), or pool cache samples(addr), reach threshold rounded to tick cadence:
      jitCompiler.Compile(instrs[addr]) privately, or publish one shared Cache module
      └─ heat-sorted single-compile JIT trace pipeline:
         ├─ hot/forced basic blocks → natural-fallthrough traces
         ├─ each trace compiles once and exits through journal SP/IP
         ├─ branches either stay in native code or exit to threaded successors
         ├─ assembler.Link → primary/internal context-pointer Callables
         └─ i.code[entryIP] = closure(callable) per accepted entry

5. interp.Close()
   └─ buffer.Free() → munmap
```

`WithThreshold(0)` enables JIT on the first sample. Negative thresholds disable JIT. Bytecode debugging uses `NewDebugger` via `WithDebugger`, enabling instruction-accurate hooks and disabling JIT for exact instruction-boundary stops.

## Focus Areas

| Area | Direction |
|---|---|
| JIT coverage | numeric segments, direct calls, small function-value indirect dispatches, guarded ref slots/upvalues, and selected heap reads compile to native code; host calls, closure call sites, heap allocation/mutation, maps, and unsupported heap shapes fall back |
| Architecture support | ARM64 optimized; x86-64 can follow once users + benchmarks clear |
| Benchmarks | broaden numeric loops, host calls, heap-object workloads |
| Program format | keep `instr`/`program` as compact Go-native bytecode surface |
| Host integration | keep `interp.NewHostFunction` as primary call bridge; use `Marshal`/`Unmarshal` for Go value conversion |
| Resource policy | document how `context.Context`, fuel, hooks, stack/heap/frame limits, host policy work together |

See `docs/roadmap.md` for current priorities.
