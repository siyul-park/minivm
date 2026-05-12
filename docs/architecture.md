# Architecture

Detailed component design and data flow for minivm.

## Agent Quick Map

Read this when a change crosses package boundaries or you need to know where state lives.

| If you touch | Also read |
| --- | --- |
| `interp/` runtime state, frames, globals | [memory-model.md](memory-model.md), [value-representation.md](value-representation.md) |
| `interp/threaded.go` or `interp/jit*.go` | [jit-internals.md](jit-internals.md), [instruction-set.md](instruction-set.md) |
| `analysis/`, `transform/`, `optimize/`, `pass/` | [pass-system.md](pass-system.md) |
| `cmd/repl/` or `cmd/minivm/` | [guides/repl.md](guides/repl.md) |

Keep package boundaries stable: `instr` must stay leaf-like, `types` must not import `interp`, and optimizer code should flow through `pass.Manager`.

## Package Dependency Graph

Arrows show import direction (A → B means package A imports package B).

```
instr ◄───────────────────────────────────────────────────────────────┐
types ◄──────────────────────────────────────────────────────┐        │
                                                             │        │
program ──────────────────────────────────────────────────► instr    │
   │                                                         │        │
   ▼                                                         │        │
interp ──► types    asm ──► asm/arm64                        │        │
   │       │         ▲                                        │        │
   ├───────┘         │                                        │        │
   ├────────────────►┘  (jit_arm64.go via init())            │        │
   ├──► prof                                                  │        │
   │                                                          │        │
   ├──► pass                                                  │        │
   │    └── manager.go  (reflection-based pipeline)           │        │
   │                                                          │        │
   └──► analysis ──► pass                                     │        │
        │            └── types ──────────────────────────────►┘        │
        │            └── instr ──────────────────────────────────────►┘
        │
        ▲ (also imported by transform and interp/jit.go)
        │
transform ──► analysis, pass, types, instr, program
   ▲
   │
optimize ──► transform, analysis, pass, program

cmd/repl ──► instr, interp, program, types
   ▲
   │
cmd/minivm ──► cmd/repl, cobra
```

**Simplified view** (most important paths):
```
program → instr → (nothing)
interp  → program, instr, types, asm, pass, analysis
optimize → transform → analysis → pass
cmd/repl → instr, interp, program, types
```

## Component Responsibilities

### `program/`

`program.Program` is the hand-off type between the compiler/assembler that produced bytecode and the VM that will run it:

```go
type Program struct {
    Code      []byte        // marshaled instructions for the top-level function
    Constants []types.Value // functions, strings, arrays, etc.
    Types     []types.Type  // type descriptors referenced by ARRAY_NEW, STRUCT_NEW
}
```

Constants that are `*types.Function` have their own `Code []byte`. The interpreter compiles all functions at `interp.New()` time and stores them at parallel index `j+1` (index 0 is the program's own code).

### `instr/`

The instruction set is a `byte`-sized `Opcode`. Each opcode has an associated `Type` in `instr/type.go` that declares its mnemonic and `Widths []int`, which drives variable-width encoding and decoding.

`instr.Marshal([]Instruction) []byte` → serializes.  
`instr.Unmarshal([]byte) []Instruction` → deserializes.  
`instr.Format([]byte) string` → human-readable output for debugging.  
`instr.Parse(line string) (Instruction, error)` → parses one text line back to bytecode; accepts both plain (`i32.const 42`) and offset-prefixed (`0000:\ti32.const 0x0000002a`) formats.  
`instr.ParseAll(r io.Reader) ([]Instruction, error)` → reads from any `io.Reader` line-by-line, skipping blank lines.

### `types/`

The type system has two layers:

1. **`types.Value`** — a runtime value (implements `Kind() Kind`, `Type() Type`, `String() string`)
2. **`types.Type`** — a type descriptor (implements `Kind() Kind`, `Cast(Type) bool`, `Equals(Type) bool`)

`types.Boxed` (a `uint64`) is the universal currency inside the VM. All values on the stack and in globals are `Boxed`. Heap objects are stored as `types.Value` and referenced via `KindRef` in a `Boxed`. See [value-representation.md](value-representation.md) for the full encoding.

`types.Traceable` is implemented by heap objects that contain references (`Array`, `Struct`). The GC uses `Refs() []Ref` to walk the object graph.

### `interp/`

The interpreter owns all runtime state in `Interpreter`:

| Field | Purpose |
|---|---|
| `instrs [][]byte` | raw bytecode per function slot |
| `code [][]func(*Interpreter)` | threaded closures per function slot |
| `prof *prof.Stats` | aggregate and per-IP execution samples |
| `frames []frame` | call stack (addr, ip, bp) |
| `stack []Boxed` | value stack |
| `heap []Value` | flat heap array |
| `rc []int` | reference counts, parallel to heap |
| `free []int` | free list of heap indices |
| `globals []Boxed` | global variables |
| `buffer *asm.Buffer` | shared executable memory, allocated on first JIT |

**`threadedCompiler`** (in `threaded.go`): A `[256]func` table populated in `init()`. Each entry is a compile-time function that reads operands from `c.code[c.ip+N:]`, advances `c.ip`, and returns a runtime closure. The closure captures compile-time constants and advances `f.ip` by the instruction width when executed.

**`jitCompiler`** (in `jit.go`): Architecture-agnostic driver. Runs `BasicBlocksPass` to find block boundaries. For each block, `compile(b)` calls `segment(code, start, end)` in a loop to extract every maximal consecutive run of compilable instructions. Completed segments emit when they reach `WithEmit`'s minimum instruction count (default 4); a segment cut short by an unsupported instruction is kept only after more than 4 compiled instructions. Within each block, multiple independent segments may be produced. A two-pass strategy (non-terminated blocks first, then branch-terminated blocks) ensures branch targets have known signatures before linking decisions are made. All compiled segments across a function are linked together via `assembler.Link()`, which patches cross-segment label relocations. Each linked segment is installed as a closure at `out[entryIP]`.

**`HostFunction`** (in `host.go`): Wraps a Go `func(i *Interpreter, params []Boxed) ([]Boxed, error)` as a `types.Value`. Stored in the constant table and called with `CONST_GET` + `CALL` like any `*types.Function`.

### `asm/`

`Assembler` maintains two virtual-register (VReg) stacks:

- **`stack []VReg`**: values currently in-flight (mirrors the VM value stack within the sub-block being compiled).
- **`params []VReg`**: VRegs that were `Take`n from an empty `stack` — these become the native function's ABI input parameters.

`Compile()` + `Link()` pipeline:
1. `compile()` — strip pseudo-labels; `signature()` derives `Signature` from `params` and `stack`; `assign()` performs linear-scan VReg→PReg allocation.
2. `resolve(physAssigned)` — two-pass encode: pass 1 encodes with Imm(0) placeholders to measure architecture-agnostic byte sizes; pass 2 patches local labels and records `Relocation` entries for cross-segment references.
3. `buffer.Unseal()` → `buffer.Append(code)` → `buffer.Seal()` — writes the segment's machine code.
4. Returns `*RelocObject` (encoded chunk + `Signature` + relocation table).
5. `Link([]*RelocObject)` — unseals the buffer, patches each relocation by re-encoding the branch instruction with the resolved offset, then creates a `Caller` per object whose `Call(params, reserved)` invokes the chunk.

### `asm/arm64/`

Implements `asm.Arch` (the `Arch` singleton), `asm.Encoder`, `asm.ABI`, and `asm.Caller`.

The `Caller` invokes native chunks via the assembly trampoline in `abi_arm64.s`. The trampoline marshals arguments from an `argv` buffer (`[header, reserved…, params…]`), loads scratch register inputs (X10–X15), calls the chunk via `BL`, then reads scratch register outputs back into `argv[1..nReserved]` and return values into `argv[nReserved+1..]`. The `header uint64` encodes param/return counts, reserved count, and float-register masks. Scratch registers X8 and X9 are left outside `arch.Scratch` so the trampoline can use them as temporaries without conflicting with the chunk's reserved in/out values.

Platform-specific files carry `//go:build arm64`. `abi_stub.go` (`//go:build !arm64`) keeps the package compilable on other platforms.

### `pass/`

`pass.Manager` is a reflection-based pipeline dispatcher:

- `Register(pass)` — stores the pass keyed by its `Run` return type (`reflect.Type`).
- `Run(value)` — seeds the cache with `value` as the starting input.
- `Load(&result)` — triggers all passes that produce `typeof(result)`, caching the output.
- `Convert(src, dst)` — creates a child manager, runs `src` through it, and loads `dst`.

Passes communicate by storing their output in the manager. Downstream passes `Load` the output of upstream passes. Caching means each pass runs at most once per `Manager.Run` call.

### `analysis/` + `transform/` + `optimize/`

`BasicBlocksPass` is the shared foundation: both `jitCompiler` and the optimizer use it to find block boundaries. A block boundary is placed at: the start of the code, after any `BR`/`BR_IF`/`BR_TABLE`/`UNREACHABLE`/`RETURN`, and at every jump target.

`optimize.NewOptimizer(O1)` wires the following passes in order:
```
BasicBlocksPass → ConstantFoldingPass → ConstantDeduplicationPass → DeadCodeEliminationPass
```

All transform passes operate on `*program.Program` in-place by mutating `prog.Code` bytes and `prog.Constants`.

### `cmd/repl/`

`REPL` is the interactive read-eval-print loop. It holds two pieces of state between steps:

| Field | Purpose |
|---|---|
| `instrs []instr.Instruction` | accumulated instruction history (used only by `.show` and `.reset`) |
| `codeLen int` | byte length of accumulated instruction history, used for absolute branch normalization |
| `constants []types.Value` | function constants declared with `.const` |
| `types []types.Type` | type descriptors declared with `.type` |

For each new instruction the REPL builds a fresh `program.Program` from the accumulated history plus the new instruction, initializes a new `interp.Interpreter`, runs the full program, and prints the resulting stack. Accepted instructions, constants, and types are kept as source history; heap objects are recreated on each step so references remain valid. Execution cost is O(N) per step for N accumulated instructions.

On error the new instruction is not committed, so the session remains consistent. `.reset` clears instruction history, code length, constants, and types.

### `cmd/minivm/`

Thin cobra entry point. The root command (no subcommand) launches the REPL with `os.Stdin` / `os.Stdout`. Cobra provides `--help`. Future subcommands (e.g. `run <file>`) can be added here with no changes to `cmd/repl`.

## Execution Flow (detailed)

```
1. program.New(instrs, options...)
   └─ instr.Marshal(instrs) → prog.Code

2. optimize.Optimize(prog)  [optional AOT]
   └─ BasicBlocksPass → CF → CD → DCE

3. interp.New(prog, opts...)
   ├─ threadedCompiler.Compile(prog.Code) → i.code[0]
   └─ for each *Function constant j:
       threadedCompiler.Compile(fn.Code) → i.code[j+1]

4. interp.Run(ctx)
   ├─ main loop: code[f.ip](i)
   ├─ every 128 instructions: prof.Record(addr, ip)
   └─ when prof.Count(addr) == threshold/tick:
       jitCompiler.Compile(instrs[addr])
       └─ two-pass over basic blocks:
           ├─ pass 1: for each sampled block, segment() loop → emit eligible segments
           │   non-terminated segments → objs; terminated blocks → deferred
           ├─ pass 2: recompile terminated blocks with full signature knowledge
           ├─ assembler.Link(objs) → patches cross-segment relocations → []Caller
           └─ out[entryIP] = closure(caller, sig) for each segment

5. interp.Close()
   └─ buffer.Free() → munmap
```

## Focus Areas

| Area | Direction |
|------|-----------|
| JIT coverage | calls, globals, refs, and heap objects stay threaded until benchmarks justify native handling |
| Architecture support | ARM64 is the optimized JIT path; x86-64 can follow once target users and benchmarks are clear |
| Benchmarks | broaden coverage across numeric loops, host calls, and heap-object workloads |
| Program format | keep `instr`/`program` as the compact Go-native bytecode surface |
| Host integration | keep `interp.NewHostFunction` as the primary typed bridge into Go applications |
| Resource policy | document how `context.Context`, stack/heap/frame limits, and host-side policy work together |

See [`docs/roadmap.md`](roadmap.md) for current priorities.
