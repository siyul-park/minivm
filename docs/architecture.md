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
interp/jit_arm64 → asm/arm64
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
interp/jit_arm64 → asm/arm64
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
| `buffer *asm.Buffer` | shared executable memory, allocated on first JIT |

`threadedCompiler` (`threaded.go`): `[256]func` table populated in `init()`. Each compile-time entry reads operands, advances `c.ip`, and returns a runtime closure that captures constants and advances `f.ip` by instruction width.

`jitCompiler` (`jit.go`): architecture-agnostic. Runs `BasicBlocksPass`, ranks blocks by heat, then groups eligible adjacent natural-fallthrough blocks into JIT traces. A trace compiles once into a native object; internal trace block starts may be callable entries when incoming stack signatures are proven compatible. Branches emit deferred aliases, resolved after entry signatures are known, then `assembler.Link()` patches relocations. Each accepted entry IP installs one closure. JIT does not recompile or tier-up.

`HostFunction` (`host.go`): wraps `func(i *Interpreter, params []Boxed) ([]Boxed, error)` as `types.Value`. Lives in constants, called by `CONST_GET` + `CALL`. Use `Interpreter.Marshal`/`Unmarshal` to convert Go values; the default converter caches per-type reflection plans, while arbitrary Go function calls still use `reflect.Call`. Go `func` marshals to `HostFunction`, final `error` return propagated as host-call error. `WithMarshaler` replaces the default converter.

`HostObject` (`host.go`): wraps a Go value that carries methods or unexported fields. `STRUCT_GET`/`STRUCT_SET` use it as the concrete host-value fallback after the native `*types.Struct` fast path. Field reads/writes use compiled metadata and unsafe offsets against an internal addressable copy of the receiver; methods are pre-bound as `*HostFunction` values allocated on the VM heap.

`Pool` (`pool.go`): multi-goroutine entry point. `Interpreter` is single-goroutine; only `program.Program` is safe to share across goroutines. `NewPool(prog, size, opts...)` lazily lends up to `size` interpreters. Use `Get`/`Put` or `Run(ctx, fn)` to borrow one per goroutine. `Put` calls `Reset`; `Close` releases every idle interpreter's JIT buffer. Outstanding interpreters close on their next `Put` after `Close`. Heap refs from a borrowed interpreter are invalid after `Put` because `Reset` wipes the heap.

### `asm/`

`Assembler`: low-level IR emission. It allocates VRegs, emits instructions, and declares ABI boundaries. It has no VM stack semantics.

Core API:

- `NewVReg(type,width)`: allocate virtual register.
- `Emit(inst)`: append instruction.
- `Pin(vreg, preg)`: bind VReg to physical register (JIT uses for ABI slots).
- `Site(0, live)`: declare the single entry parameter signature.
- `Site(idx, live)` for `idx > 0`: declare a return site signature.
- `Entry(label, live)`: declare a callable internal object entry.
- `Alias(label, target)`: defer branch target choice until link.
- `Compile()`: allocate physical regs, encode, append buffer, return `RelocObject`.
- `Link([]*RelocObject)`: patch cross-segment labels, return native `Caller`s.
- `CallerAt(obj, label)`: create a caller at an internal entry offset.

`Compile()` + `Link()`:

1. `snapshot()`: capture instructions + VReg pins into immutable `program`.
2. `newCompiler()`: allocate physical regs via `RegAlloc`, encode IR to machine code.
3. `resolve()`: two-pass encode. Pass 1 measures sizes via `Imm(0)` placeholders; Pass 2 patches labels and records `Relocation`s.
4. `buffer.Unseal()` → `buffer.Append(code)` → `buffer.Seal()`: write to executable memory.
5. Return `*RelocObject` with chunk, `Signature{Params, Returns, Scratch}` for the primary entry, plus `Entries map[int]Entry{Offset, Params}` for internal callable entries, and relocations.
6. `Link([]*RelocObject)`: unseal, resolve aliases/relocations, seal, create primary callers; `CallerAt` creates internal callers.

### `asm/arm64/`

Exposes the `asm.Arch` singleton and encoder/caller, with an unexported adapter implementing `asm.ABI`.

`Caller` invokes native chunks through `abi_arm64.s`. The trampoline marshals `argv` as `[header, reserved…, params…]`, copies `argv[0]` into the header register, loads scratch `X10–X14`, calls with `BL`, copies the header register back to `argv[0]`, then writes scratch outputs and return values. `header uint64` encodes param/return counts, reserved count, and float/width masks. `X8`/`X9` are excluded from `arch.Scratch` for trampoline temporaries; `X15` is the header register.

ARM64-specific files use `//go:build arm64`; `abi_stub.go` with `//go:build !arm64` keeps other platforms compilable.

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
   └─ if JIT enabled and prof.Samples(addr) reaches threshold rounded to tick cadence:
      jitCompiler.Compile(instrs[addr])
      └─ heat-sorted single-compile JIT trace pipeline:
         ├─ hot/forced basic blocks → natural-fallthrough traces
         ├─ each trace compiles once, collecting entries and deferred edges
         ├─ compatible edges alias to native entries; others alias to fallback exits
         ├─ assembler.Link/CallerAt → primary and internal callers
         └─ out[entryIP] = closure(caller, sig) per accepted entry

5. interp.Close()
   └─ buffer.Free() → munmap
```

`WithThreshold(0)` enables JIT on the first sample. Negative thresholds disable JIT. Bytecode debugging uses `NewDebugger` via `WithDebugger`, enabling instruction-accurate hooks and disabling JIT for exact instruction-boundary stops.

## Focus Areas

| Area | Direction |
|---|---|
| JIT coverage | calls, globals, refs, heap objects stay threaded until benchmarks justify |
| Architecture support | ARM64 optimized; x86-64 can follow once users + benchmarks clear |
| Benchmarks | broaden numeric loops, host calls, heap-object workloads |
| Program format | keep `instr`/`program` as compact Go-native bytecode surface |
| Host integration | keep `interp.NewHostFunction` as primary call bridge; use `Marshal`/`Unmarshal` for Go value conversion |
| Resource policy | document how `context.Context`, fuel, hooks, stack/heap/frame limits, host policy work together |

See `docs/roadmap.md` for current priorities.
