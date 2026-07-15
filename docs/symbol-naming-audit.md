# Symbol Naming and Responsibility Audit

## Scope

This audit covers every production Go package in minivm. Generated handlers
were reviewed through `internal/cmd/geninterp`; generated output was not edited
directly.

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
| `asm` | `patchExternalRelocs` | `patch` |
| `asm` | `regAlloc`, `newRegAlloc` | `allocator`, `newAllocator` |
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
| `prof` | `appendEntryCounters`, `activeKeys` | `appendEntries`, `active` |
| `prof` | `mergeCounters`, `counterFor`, `resetCounters` | `merge`, `counter`, `reset` |

## Retained Names

These candidates were re-reviewed and intentionally retained:

- `analysis.gslot`: `slot` is already a live local name throughout the
  analysis; the prefix distinguishes the abstract GVN operand-stack entry and
  avoids shadowing.
- `cli.ensureDebugger`: `debugger` is already a field, and `ensure` accurately
  exposes the initialization side effect.
- `interp.recordCompile`: `record` is ambiguous among trace, profile, and
  compilation events.
- `interp.globalKinds` and `interp.globalReprs`: `global` distinguishes these
  facts from local, capture, stack, and result kinds; removing it weakens call
  sites in the large `Interpreter` receiver.
- `internal/cmd/geninterp.slotHandler`, `dynamicCall`, `clearRange`,
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
| `asm/amd64` | Existing `arch`, `abi`, `encoder`, and `New` retained. |
| `asm/arm64` | Operand-shape and architecture symmetry retained. |
| `cli` | Command handlers and renderers renamed by role. |
| `cmd/minivm` | Only `main`; no change required. |
| `debug` | Public API retained; private pause state normalized. |
| `instr` | Opcode/specification symmetry retained. |
| `internal/cmd/geninterp` | Existing qualified generator roles retained. |
| `interp` | Public option/builder consumers and private runtime/JIT roles normalized. |
| `optimize` | Package-primary constructor and pass insertion shortened. |
| `pass` | Pipeline insertion shortened; manager APIs retained. |
| `prof` | Private aggregation roles shortened; public record/register distinction retained. |
| `program` | Parser phases shortened without obscuring section parsing. |
| `transform` | Standard algorithm abbreviations and `Pass` suffixes adopted. |
| `types` | Builder and slot APIs normalized; map constructor roles retained. |

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
