# Symbol Naming and Responsibility Audit

## Scope

This audit covers every production Go package in minivm. Generated handlers
were reviewed through `internal/codegen`; generated output was not edited
directly.

A second round (see Round 2 below) covers the symbols the JIT
internal-boundary restructuring introduced or moved: `internal/journal`,
`internal/jit`, `internal/jit/arm64`, the new register-allocator files in
`internal/asm` (`block.go`, `dominance.go`, `uses.go`, `carry.go`,
`eligibility.go`), `internal/asm/arm64/stack.go`, and interp's JIT
integration (`jit.go`, `tier.go`, `jit_arm64.go`, `jit_stub.go`).

Applied rules:

- Keep meaningful suffixes such as `Pass`, `Analysis`, `Type`, `Info`,
  `Builder`, and `Error`.
- Remove redundant package, receiver, implementation, and phase prefixes.
- Prefer one word, but retain a qualifier when it distinguishes real domains.
- Use standard abbreviations such as CFG, GVN, DCE, ABI, JIT, IP, and VM.
- Preserve opcode, ABI, trap, architecture, interpreter, and JIT symmetry.
- Do not keep compatibility aliases before the first stable release.

## Public API Changes

| Package | Previous | Current |
|---|---|---|
| `types` | `Function.LocalKinds` | `Function.Slots` |
| `types` | `FunctionBuilder.WithParams` | `FunctionBuilder.Params` |
| `types` | `FunctionBuilder.WithReturns` | `FunctionBuilder.Returns` |
| `types` | `FunctionBuilder.WithLocals` | `FunctionBuilder.Locals` |
| `types` | `FunctionBuilder.WithCaptures` | `FunctionBuilder.Captures` |
| `interp` | `WithMaxHeap` | `WithHeapLimit` |
| `analysis` | `BasicBlocksAnalysis` | `BlocksAnalysis` |
| `analysis` | `NewBasicBlocksAnalysis` | `NewBlocksAnalysis` |
| `analysis` | `GlobalValueNumberingAnalysis` | `GVNAnalysis` |
| `analysis` | `GlobalValueNumbering` | `GVN` |
| `analysis` | `NewGlobalValueNumberingAnalysis` | `NewGVNAnalysis` |
| `transform` | `AlgebraicSimplificationPass` | `AlgebraicPass` |
| `transform` | `ConstantFoldingPass` | `FoldPass` |
| `transform` | `ConstantDeduplicationPass` | `DedupPass` |
| `transform` | `DeadCodeEliminationPass` | `DCEPass` |
| `transform` | `GlobalValueNumberingPass` | `GVNPass` |
| `optimize` | `NewOptimizer` | `New` |
| `optimize` | `Optimizer.AddPass` | `Optimizer.Add` |
| `pass` | `Pipeline.AddPass` | `Pipeline.Add` |
| `asm` | `RegInfo.FltReserved` | `RegInfo.FloatReserved` |

The map constructors remain deliberately distinct:

- `NewMap` creates the general map implementation.
- `NewMapWithCapacity` creates the general implementation with reserved capacity.
- `NewTypedMap` explicitly creates the exported primitive-key specialization.
- `NewMapForType` selects an implementation from a runtime `MapType`.

## Private Changes

| Package | Previous | Current |
|---|---|---|
| `analysis` | `gnumbering`, `newGNumbering` | `numbering`, `newNumbering` |
| `analysis` | `gcompute` | `compute` |
| `asm` | `collectRelaxations`, `spliceRelaxations` | `collect`, `splice` |
| `asm` | `regAlloc`, `newRegAlloc` | `rewriter`, `newRewriter` |
| `asm` | `scanLastUses` | `scan` |
| `cli` | `doBreak` | `breakpoint` |
| `cli` | `doClear` | `clearBreakpoint` |
| `cli` | `doEnable` | `enableBreakpoint` |
| `cli` | `doDebug` | `debug` |
| `cli` | `printProfile`, `printBreakpoints`, `printStop` | `showProfile`, `showBreakpoints`, `showStop` |
| `program` | `parseSectioned`, `processSection`, `parseLegacy` | `sections`, `parseSection`, `legacy` |
| `debug` | `stopped`, `pausedDepth` | `pause`, `pauseDepth` |
| `interp` | `newThreader`, `traceLoop` | `threader`, `trace` |
| `interp` | `runtimeError`, `framesInfo` | `fault`, `stacktrace` |
| `interp` | `newCompileInput` | `input` |
| `interp` | `applyPlanBlock`, `applyPlanStep` | `applyBlock`, `applyStep` |
| `prof` | `function` | `samples` |
| `prof` | `appendEntryCounters`, `activeKeys` | `appendRows` |
| `prof` | `mergeCounters`, `counterFor`, `resetCounters` | `mergeRows`, `register`, `resetRows` |

## Retained Names

These candidates were re-reviewed and intentionally retained:

- `analysis.gslot`: `slot` is already a live local name throughout the
  analysis; the prefix distinguishes the abstract GVN operand-stack entry and
  avoids shadowing.
- `cli.ensureDebugger`: `debugger` is already a field, and `ensure` accurately
  exposes the initialization side effect.
- `interp.recordCompile`: `record` is ambiguous among trace, profile, and
  compilation events.
- `interp.globalKinds` and `interp.globalDecls`: `global` distinguishes these
  facts from local, capture, stack, and result kinds; removing it weakens call
  sites in the large `Interpreter` receiver.
- `internal/codegen.slotStandalone`, `dynamicCall`, `clearRange`,
  `numericKind`, and `kindName`: each qualifier distinguishes a real generator
  role. The shorter candidates either collide, hide the predeclared `clear`, or
  lose meaning at package scope.
- Architecture encoder helpers retain operand-shape qualifiers because symmetry
  and auditability are more valuable than local brevity.
- Opcode constants, trap errors, enum prefixes, and JIT `To` variants retain
  their established symmetric names.

`program.parseSection` is used instead of the shorter `section` because the
parser already owns a local `section` variable and the helper performs parsing.
`debug.pause` is used instead of `stop` because `Debugger` already owns the
current `stop` field.

## Package Coverage

| Package | Result |
|---|---|
| `analysis` | CFG/GVN public terminology and private numbering roles normalized. |
| `asm` | Phase prefixes removed; register field spelling normalized. |
| `internal/asm/amd64` | Existing `arch`, `abi`, `encoder`, and `New` retained. |
| `internal/asm/arm64` | Operand-shape and architecture symmetry retained. |
| `cli` | Command handlers and renderers renamed by role. |
| `cmd/minivm` | Only `main`; no change required. |
| `debug` | Public API retained; private pause state normalized. |
| `instr` | Opcode/specification symmetry retained. |
| `internal/codegen` | Bare verbs and adjectives renamed by role; see Round 3 below. |
| `interp` | Public option/builder consumers and private runtime/JIT roles normalized. |
| `optimize` | Package-primary constructor and pass insertion shortened. |
| `pass` | Pipeline insertion shortened; manager APIs retained. |
| `prof` | Private aggregation roles shortened; public record/register distinction retained. |
| `program` | Parser phases shortened without obscuring section parsing. |
| `transform` | Standard algorithm abbreviations and `Pass` suffixes adopted. |
| `types` | Builder and slot APIs normalized; map constructor roles retained. |

## Round 2: JIT Internal-Boundary Restructuring

The restructuring split the JIT into `internal/journal` (the frame-journal
layout), `internal/jit` (the architecture-neutral compiler: plan IR,
frontends, driver, the `Machine` seam, and the `Code` artifact),
`internal/jit/arm64` (the ARM64 machine), a real control-flow graph for
`internal/asm`'s register allocator (`block.go`, `dominance.go`, `uses.go`,
`carry.go`, `eligibility.go`), `internal/asm/arm64/stack.go` (native
stack-reserve sizing), and interp's JIT integration (`jit.go`, `tier.go`,
`jit_arm64.go`, `jit_stub.go`). This round audits the symbols that
restructuring introduced or moved.

### Round 2 Public API Changes

| Package | Previous | Current |
|---|---|---|
| `internal/jit` | `ExitDescriptor` | `Exit` |
| `internal/jit` | `Resolve` | `FunctionAt` |
| `internal/jit` | `ElemShapeOf` | `ElemShapeByKind` |
| `internal/jit` | `HostShapeOf` | `HostShapeByKind` |
| `internal/jit` | `Layout.CoroValue` | `Layout.CoroutineValue` |
| `internal/jit` | `Layout.CoroDone` | `Layout.CoroutineDone` |
| `internal/asm` | `V` | `Virtual` |
| `internal/asm` | `P` | `Physical` |

`ExitDescriptor` carried a representation suffix `Entry.Exits` did not need;
`Exit` reads as the value it is (`Entry.Exits []Exit`, matching
`Code.Entries map[Anchor]Entry`). `Resolve` was a bare domain verb over a
plain address lookup - §4.2 reserves verbs like `Build`, `Compile`, and
`Publish` for computations and transitions, not lookups - and `FunctionAt`
matches the position-lookup convention `journal.At` already established.
`ElemShapeOf` and `HostShapeOf` used a different preposition than their
sibling `ElemShapeByItab` for the same two shape tables; all three now read
`<Shape>By<Selector>`. `CoroValue`/`CoroDone` abbreviated "Coroutine" beside
`CoroutineItab` in the same struct and the same doc comment; `Coro` is not an
established project abbreviation, so both now spell out the term the private
`coroutine` type and `CoroutineItab` already use.

`V` and `P` were exported one-letter functions, which §4.1 limits to
conventional indexes, receivers, and very small scopes. They wrap a `VReg` and
a `PReg` as operands, so `Virtual` and `Physical` name the role each plays
without repeating the register type. `Imm` and `Mem` keep their spelling:
both are established domain abbreviations, which §4.1 permits. The change cost
22 call sites - the instruction builders in `internal/asm/arm64/instr.go` wrap
`Reg` internally rather than calling these constructors, so the surface was far
smaller than the encoder's size suggests.

### Round 2 Private Changes

| Package | Previous | Current |
|---|---|---|
| `internal/jit/arm64` | `lowering.exits []sideExit` | `lowering.sideExits []sideExit` |
| `internal/jit/arm64` | `lowering.descriptors []jit.Exit` | `lowering.exits []jit.Exit` |

Renaming the exported `ExitDescriptor` to `Exit` left the `lowering` struct
with `exits []sideExit` and `descriptors []jit.Exit` side by side - the field
holding the reported `jit.Exit` values kept the old type's name instead of
the concept's. Swapping the pair frees `exits` for the list the type is
actually named after and gives the cold-stub bookkeeping list a name that
says what it holds.

### Round 2 Retained Names

These candidates were reviewed and intentionally retained:

- `internal/jit.carried` and `internal/jit/arm64.carriedLocal` are not one
  concept spelled two ways. `jit.carried` is a planning-time computation
  that decides which VM locals are eligible to become native-loop
  registers, recorded on `Plan.Carried`; `carriedLocal` is the ARM64
  backend's runtime bookkeeping for one such local while lowering emits it.
  The shared root names the same domain concept at two real phases, the way
  `internal/asm`'s own `carryHazards` and `internal/jit/arm64`'s
  loop-carry prologue share "carry" for a related but distinct hazard.
- `internal/asm.P`, `V`, `Imm`, and `Mem` are one- and two-letter exported
  operand constructors, which §4.1 restricts to indexes, receivers, and very
  small scopes. Measured call-site count is real but modest - about 15
  production and 34 test sites across 12 files, not the thousands a first
  glance at the encoder suggests, because most instruction builders
  (`ADD`, `MOVZ`, `LDR`, ...) take `asm.Reg` and wrap it internally rather
  than calling these constructors at each call site. They are retained here
  because this restructuring did not introduce or move them and they read no
  more or less consistently after it; `P`/`V` are the two names that would
  fail §4.1 on their own, and renaming them is a self-contained follow-up
  worth doing in a dedicated `internal/asm` pass rather than folding it into
  a JIT-boundary audit.

### Round 2 Package Coverage

| Package | Result |
|---|---|
| `internal/journal` | No change: cell, record, and trap vocabulary already minimal and symmetric. |
| `internal/jit` | Lookup naming unified (`FunctionAt`, `ElemShapeByKind`, `HostShapeByKind`); representation suffix dropped from `Exit`; `Layout` coroutine fields despecialized to match `CoroutineItab`. |
| `internal/jit/arm64` | Lowering, activation, and control-flow mechanics already private and role-named; `carriedLocal` retained as a distinct phase from `jit.carried`. |
| `internal/asm` | New allocator files (`block`, `cfg`, `dominance`, `useIndex`, `hazard`, `crosses`) already role-named; no change. |
| `internal/asm/arm64` | `stack.go`'s `SpillBytes`/`SaveAreaBytes`/`StackReserve`/`FrameSize` already role-named; no change. `P`/`V`/`Imm`/`Mem` retained (see Round 2 Retained Names). |
| `interp` | JIT integration (`jit.go`, `tier.go`) already role-named; `Layout` construction call sites updated for the `internal/jit` renames. |

## Round 3: Generator Package Restructuring

`internal/cmd/geninterp` became the importable `internal/codegen` package plus a
thin `internal/cmd/codegen` entry point. `lower.go` (4,496 lines, 198
declarations) split into one file per opcode domain, mirroring
`internal/jit/arm64/`. Generated output is unchanged apart from its header
comment, so every rename below is generator-internal.

### Round 3 Public API Changes

| Before | After | Reason |
|---|---|---|
| `main.output` | `codegen.File` | The generated file is now the package's value type, not a command-local record. |
| `main.generate` | `codegen.Generate` | Sole exported entry point; returns files, leaves I/O to the command. |
| `(output).sync(check, stdout)` | `main.verify`, `main.write` | One job each instead of a boolean mode switch; both own I/O in the command. |

### Round 3 Private Changes

| Before | After | Reason |
|---|---|---|
| `handler` | `standalone` | Names which handler shape it builds. |
| `slotHandler`, `constHandler` | `slotStandalone`, `constStandalone` | Follow `standalone`. |
| `bind` | `standaloneLowerer` | Names the adaptation, not the verb. |
| `source` | `slotRead` | `source` is pattern-catalog vocabulary. |
| `ref` | `refOp` | Collided with `target.ref` and local `ref` bindings. |
| `index`, `structIndex` | `containerGet`, `structGet` | They lower `array.get`/`struct.get`, not an index. |
| `lookup` | `containerFallback` | It is the unfused fallback body. |
| `bounds` (function) | `indexGuard` | Collided with `(loader).bounds`. |
| `element` | `arrayStore` | It lowers `array.set`. |
| `store` | `localStore` | It lowers `local.set` only. |
| `scalar` | `arithmetic` | `scalar` names a value class, not an action. |
| `apply` | `compute` | Builds the raw computation expression. |
| `checked` | `trapping` | Matches the `traps` predicate. |
| `take` | `unbox` | Pairs with `borrow`. |
| `boxed` (function) | `boxedTemp` | Collided with `value.boxed`. |
| `input` | `operands` | Returns operand kind and count. |
| `count` | `backedge` | Emits the back-edge warmup counter. |
| `direct`, `closure`, `host` | `callDirect`, `callClosure`, `callHost` | Adjectives became named call arms. |
| `enter`, `frame`, `reuse`, `replace`, `entered` | `enterFrame`, `pushFrame`, `reuseFrame`, `replaceFrame`, `frameEntered` | One verb family for frame lifecycle. |
| `create` | `allocClosure` | Says what is created. |
| `declare`, `initialize`, `compile` | `threaderType`, `fallbackInit`, `compileMethod` | Name the emitted declaration. |
| `handlers`, `fusions`, `matches` | `handlerTable`, `fusionTable`, `matchGuard` | Name the emitted value. |
| `storeArray`, `boxArray` | `storeElem`, `boxElem` | `storeArray` was a word transposition of `arrayStore`; the pair now names the element accessors. |

### Round 3 Retained Names

- `compose`, `resolve`, `lower`, `lowerers`, `load`, `loader` and its methods,
  `call`, `branch`, `numeric`, `produce`, `consume`, `materialize`, `dispatch`,
  `dynamicCall`, `retire`, `release`, `invoke`, `arity`, `traps`, `width`,
  `add`, `temp`, `reject`, `overflow`: each already names a generator role that
  no shorter or more specific candidate improves.
- Every `<opcode>()` emitter keeps its mnemonic-derived name so the `lowerers`
  table reads as the instruction set.

### Round 3 Package Coverage

| Package | Result |
|---|---|
| `internal/codegen` | New package: composition engine in `lower.go`, one file per opcode domain, `File`/`Generate` as the only exported symbols. |
| `internal/cmd/codegen` | Reduced to flag parsing plus `run`/`verify`/`write`. |

## Verification

The refactor requires all of the following before merge:

```bash
make check-generated
go test -race ./...
(cd benchmarks && go test -race ./...)
go vet ./...
(cd benchmarks && go vet ./...)
git diff --check
make benchmark-pr
```
