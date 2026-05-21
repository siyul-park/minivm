# Architecture

Detailed component design + data flow for minivm.

## Agent Quick Map

Read when changes cross package boundaries or need state ownership.

| Touch | Also read |
|---|---|
| `interp/` runtime state, frames, globals | `memory-model.md`, `value-representation.md` |
| `interp/` debugger API or bytecode stepping | `debugging.md`, `profile.md` |
| `prof/` or profile options | `profile.md` |
| `interp/threaded.go` or `interp/jit*.go` | `jit-internals.md`, `instruction-set.md` |
| `analysis/`, `transform/`, `optimize/`, `pass/` | `pass-system.md` |
| `cmd/repl/` or `cmd/minivm/` | `guides/repl.md` |

Keep boundaries stable: `instr` stays leaf-like, `types` must not import `interp`, optimizer code flows through `pass.Manager`.

## Package Dependency Graph

Import direction: `A → B` means `A` imports `B`.

```text
program → instr
interp  → program, instr, types, asm, prof, pass, analysis
asm     → asm/arm64
analysis → pass, types, instr
transform → analysis, pass, types, instr, program
optimize → transform, analysis, pass, program
cmd/repl → instr, interp, program, types
cmd/minivm → cmd/repl, cobra
```

Important paths:

```text
program → instr → nothing
interp → program, instr, types, asm, pass, analysis
optimize → transform → analysis → pass
cmd/repl → instr, interp, program, types
```

## Component Responsibilities

### `program/`

`program.Program` is hand-off between bytecode producers and VM:

```go
type Program struct {
    Code      []byte
    Constants []types.Value
    Types     []types.Type
}
```

`Code`: top-level bytecode. `Constants`: functions, strings, arrays, etc. `Types`: descriptors for `ARRAY_NEW` and `STRUCT_NEW`. `*types.Function` constants have own `Code []byte`. `interp.New()` compiles all functions to parallel index `j+1`; index `0` is program code.

### `instr/`

Instruction set: byte-sized `Opcode`; each opcode has `Type` in `instr/type.go` with mnemonic and `Widths []int` for variable-width encoding/decoding.

- `Marshal([]Instruction) []byte`: serialize.
- `Unmarshal([]byte) []Instruction`: deserialize.
- `Format([]byte) string`: debug text.
- `Parse(line string) (Instruction, error)`: parse plain or offset-prefixed lines, e.g. `i32.const 42` or `0000:\ti32.const 0x0000002a`.
- `ParseAll(r io.Reader) ([]Instruction, error)`: parse line-by-line, skipping blanks.

### `types/`

Two layers:

1. `types.Value`: runtime value with `Kind()`, `Type()`, `String()`.
2. `types.Type`: descriptor with `Kind()`, `Cast(Type)`, `Equals(Type)`.

`types.Boxed` (`uint64`) is VM stack/global currency. Heap objects are `types.Value` referenced by `KindRef` in `Boxed`. See `value-representation.md`.

`types.Traceable` marks heap objects containing refs (`Array`, `Struct`); GC walks them via `Refs() []Ref`.

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

`threadedCompiler` (`threaded.go`): `[256]func` table populated in `init()`. Each compile-time entry reads operands, advances `c.ip`, returns runtime closure capturing constants + advancing `f.ip` by instruction width.

`jitCompiler` (`jit.go`): architecture-agnostic. Runs `BasicBlocksPass`, ranks blocks by heat, compiles hotter blocks first. `compile(b)` loops `segment(code,start,end)` to extract maximal compilable sampled runs. Completed segments emit at `WithCutoff` min count, default `8`; truncated and branch-terminated segments emit only when meeting same cutoff. Cold segments in hot blocks skipped. Two-pass: non-terminated blocks first, then branch-terminated, so branch targets have known signatures. `assembler.Link()` patches cross-segment labels. Each linked segment installs closure at `out[entryIP]`. JIT does not recompile or tier-up.

`HostFunction` (`host.go`): wraps `func(i *Interpreter, params []Boxed) ([]Boxed, error)` as `types.Value`. Lives in constants, called by `CONST_GET` + `CALL`. Use `Interpreter.Marshal`/`Unmarshal` to convert Go values; Go `func` marshals to `HostFunction`, final `error` return propagated as host-call error. `WithMarshaler` replaces default reflection-based converter.

### `asm/`

`Assembler`: low-level IR emission — allocate VRegs, emit instructions, declare ABI boundaries. No VM stack semantics.

Core API:

- `NewVReg(type,width)`: allocate virtual register.
- `Emit(inst)`: append instruction.
- `Pin(vreg, preg)`: bind VReg to physical register (JIT uses for ABI slots).
- `Site(idx, live)`: declare ABI boundary at instruction idx with live values.
- `Compile()`: allocate physical regs, encode, append buffer, return `RelocObject`.
- `Link([]*RelocObject)`: patch cross-segment labels, return native `Caller`s.

`Compile()` + `Link()` pipeline:

1. `snapshot()`: capture instructions + VReg pins into immutable `program`.
2. `newCompiler()`: allocate physical regs via `RegAlloc`, encode IR to machine code.
3. `resolve()`: two-pass encode. Pass 1 measures sizes via `Imm(0)` placeholders; Pass 2 patches labels and records `Relocation`s.
4. `buffer.Unseal()` → `buffer.Append(code)` → `buffer.Seal()`: write to executable memory.
5. Return `*RelocObject` with chunk, `Signature` (from `Site` declarations), relocations.
6. `Link([]*RelocObject)`: unseal, patch relocations, seal, create one `Caller` per object.

### `asm/arm64/`

Implements `asm.Arch` singleton, `asm.Encoder`, `asm.ABI`, `asm.Caller`.

`Caller` invokes native chunks via `abi_arm64.s`. Trampoline marshals `argv` as `[header, reserved…, params…]`, copies `argv[0]` into header register, loads scratch `X10–X14`, calls with `BL`, copies header register back to `argv[0]`, writes scratch outputs + return values. `header uint64` encodes param/return counts, reserved count, float/width masks. `X8`/`X9` excluded from `arch.Scratch` for trampoline temporaries; `X15` reserved for header register.

ARM64-specific files use `//go:build arm64`; `abi_stub.go` with `//go:build !arm64` keeps other platforms compilable.

### `pass/`

`pass.Manager`: reflection-based pipeline dispatcher.

- `Register(pass)`: key pass by `Run` return type.
- `Run(value)`: seed cache with input.
- `Load(&result)`: run/cached-load passes producing `typeof(result)`.
- `Convert(src,dst)`: child manager runs `src`, then loads `dst`.

Passes communicate through manager outputs. Downstream `Load` upstream outputs. Each pass runs at most once per `Manager.Run`.

### `analysis/`, `transform/`, `optimize/`

`BasicBlocksPass` underpins JIT + optimizer. Boundaries at code start, after `BR`/`BR_IF`/`BR_TABLE`/`UNREACHABLE`/`RETURN`, at every jump target.

`optimize.NewOptimizer(O1)` order:

```text
BasicBlocksPass → ConstantFoldingPass → ConstantDeduplicationPass → DeadCodeEliminationPass
```

Transform passes mutate `*program.Program` in-place: edit `prog.Code` bytes and `prog.Constants`.

### `cmd/repl/`

`REPL` state between steps:

| Field | Purpose |
|---|---|
| `instrs []instr.Instruction` | instruction history for `.show` / `.reset` |
| `codeLen int` | byte length of history for absolute branch normalization |
| `constants []types.Value` | `.const` function constants |
| `types []types.Type` | `.type` descriptors |

Each instruction: build fresh `program.Program` from history + new instruction, create new `interp.Interpreter`, run full program, print stack. Accepted instructions/constants/types stay as source history. Heap recreated each step, refs stay valid. Cost `O(N)` per step for `N` accumulated instructions.

On error, new instruction not committed. `.reset` clears instruction history, code length, constants, types.

### `cmd/minivm/`

Thin cobra entrypoint. Root command launches REPL with `os.Stdin`/`os.Stdout`; cobra provides `--help`. Future subcommands like `run <file>` can be added without changing `cmd/repl`.

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
      └─ heat-sorted two-pass sampled basic-block compile:
         ├─ pass 1: hot blocks → segment() loop → sampled eligible segments
         │  non-terminated segments → objs; terminated blocks deferred
         ├─ pass 2: recompile terminated blocks with full signature knowledge
         ├─ assembler.Link(objs) → patch cross-segment relocations → []Caller
         └─ out[entryIP] = closure(caller, sig) per segment

5. interp.Close()
   └─ buffer.Free() → munmap
```

`WithThreshold(0)` enables JIT on first sample. Negative thresholds disable JIT. Bytecode debugging uses `NewDebugger` via `WithDebugger`, enabling instruction-accurate hooks and disabling JIT for exact instruction-boundary stops.

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
