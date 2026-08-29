# Testing

Executable specification ownership, completeness gates, and migration status for minivm tests.

## When to Read

Read when adding or changing a public API, opcode, verifier rule, interpreter behavior, optimizer path, JIT path, fuzz target, or benchmark fixture.

## Source of Truth

| Concern | Source |
|---|---|
| test shape and naming | `docs/coding-patterns.md` §12 |
| public API behavior | production owner test matching the defining file |
| opcode metadata and mnemonic | `instr/type.go` and `TestValid` |
| verifier policy | `program/verify.go` and `TestVerify/defines_a_policy_for_every_opcode` |
| runtime opcode corpus | `interp/interp_test.go` `runTests` and `TestInterpreter_Run/covers_every_runtime_opcode` |
| backend support | `docs/instruction-set.md` |

## Test Layers

| Layer | Default owner | Contract |
|---|---|---|
| Public contract | production-matched package test file | exported constructors, functions, methods, options, errors, and lifecycle behavior |
| Runtime specification | `interp.TestInterpreter_Run` | one visible bytecode fixture per opcode behavior, including traps and ownership |
| Semantic parity | owning transform, optimizer, or interpreter test | compare observable output across threaded, optimized, fused, JIT, exit, and deoptimization paths |
| Internal invariant | nearest public or artifact boundary | safety or deterministic mechanics observed through public behavior, generated output, or executable artifacts |
| Fuzz | package `fuzz_test.go` | bounded trust-boundary and semantic differential properties |
| Integration | highest public package boundary | real parse-to-close flows without duplicating unit cases |

Every test package uses the production package name plus `_test` and acts as an
importing client. Tests do not access private symbols or representation; the
files listed under White-Box Test Files are the recorded exceptions, each with
the condition that ends it.
Internal invariants use public behavior, generated output, or executable
artifacts; tests that cannot express an observable contract are removed rather
than widening production APIs for test access.

### White-Box Test Files

Three test files still declare `package interp` because the contracts they
assert have no external-package expression yet. Each is recorded with the
condition that removes it (`docs/coding-patterns.md` §1.1).

| File | Why it is white-box | Removal condition |
|---|---|---|
| `interp/tier_test.go` | Ties private `nativeFrameLimit` (declared in `interp/tier.go`) to the ARM64 invoke trampoline's stack reserve and total frame size via `arm64.StackReserve`/`arm64.FrameSize`, which own the byte arithmetic (`internal/asm/arm64/stack.go`), and reads `abi_arm64.s`'s two literals directly to compare against them. The complementary half of that reserve invariant — that `abi_arm64.s`'s own two literals agree with each other — needs no private interp state and lives as an ordinary external test, `arm64.TestFrameSize` (`internal/asm/arm64/stack_test.go`). | None known. `nativeFrameLimit` is interp-private bookkeeping with no contract to move with the ARM64 backend, which already lives in `internal/jit/arm64`. |
| `interp/jit_test.go` | Drives `jit.Compiler.Compile` against a snapshot only the private `Interpreter.compileSnapshot` can build (module, constants, globals, heap, declared types, and the `HostStruct`/`field`/`conversion`/`coroutine` layout offsets `jit.Layout` needs), installs the result with the private `Interpreter.install`, and calls the compiled callable directly with `journalPtr`, asserting the native trap encoding (`journal.CellTrap`, `journal.CellExitID`, `jit.Exit`) and splicing frame state to force a specific exit; also reads install bookkeeping (`tried`, `exits`) and `tracer.headers` that no metric separates from "not attempted" (`TestARM64_Backedge`). | None known. `newCompiler`, `compileSnapshot`, `journalPtr`, `install`, `tried`, `exits`, and the loop/call dispatch wrappers are interp-private mechanics with no contract to move with the ARM64 backend, which already lives in `internal/jit/arm64`. |
| `interp/trace_test.go` | Calls `tracer.capture` without `Run`, which is the only way to prove speculative capture snapshots a container instead of mutating the live heap. Any public path runs the real instructions too, so the isolation claim is unobservable by construction. | None known. The recorder stays in `interp`, and the claim is only provable from inside it. |

### Known Coverage Gaps

Recorded so a gap stays visible instead of being rediscovered. Each is a claim
with no expression through the public API, removed rather than kept alive by a
proxy double (`docs/coding-patterns.md` §12.2, §12.3).

| Uncovered | Why it cannot be reached publicly |
|---|---|
| `Interpreter.retire` and the watchdog (`retireWindow`, `retireGiveUpThreshold` in `interp/tier.go`; `checkRetire` in `interp/jit.go`) | Retirement is not observable from outside. A program producing a `trace-cut` give-up on every native entry runs past 80,000 entries without native-entry counts plateauing, so no public metric distinguishes a retired entry from a live one. The mechanism is what handled the RecursiveFib/35 regression, where a native entry was a net loss; a break in the give-up accounting would not fail any test today. |
| Hot-entry counter saturation | The counter and the tier-up trigger can only reach their overflow edge by being written directly. |
| Trace-tree attribution to the true entry IP | Requires driving compilation at a fabricated frame IP. |
| Dataflow fact widening at a control-flow join (`mergeSlot` in `internal/jit`) | A slot's `refKnown`/`calleeKnown` facts are planning-internal: the backend never reads them, so nothing exports them. Their only external effect is which plans a frontend produces or rejects, which no fixture isolates from the rest of planning. |
| Loop-invariant container selection (`hoistable` in `internal/jit`) | Reachable only through `TracePlan`, so asserting it needs a hand-built recorded trace that survives every other planning check. The recorder that produces real traces lives in `interp` and cannot be called without running the program. Covered end to end by `interp.TestARM64_HoistedContainerLoop`; the selection rule itself has no isolated public expression. |
| A committing flush's hot-backedge codegen (`lowerer.flush` in `internal/jit/arm64`) emits no VM-slot store for a dirty carried local | The claim is about which instructions a `flush(flushCommit)` call emits into an `asm.Assembler`, which is package-private mechanics of `internal/jit/arm64` with no public accessor. It moved with the ARM64 backend from `interp/jit_test.go`'s `TestARM64_Flush` and could not become an `arm64_test` external test because it constructed the unexported `lowering`/`activation` types directly; deleted rather than kept white-box inside `internal/jit/arm64` outside its normal contract. The behavior it protected — a hot loop back-edge keeps carried locals register-authoritative — is still covered end to end by `interp.TestARM64_LoopCarriedLocals`, which observes the result rather than the emitted bytes. |

Closing the first one needs either a public signal that an anchor was retired,
or a workload that makes retirement observable through existing metrics.

### JIT Harness Migration

The former `internal/jitcheck` package has no remaining contract. Its cases are
owned by the normal specification layers:

| Removed harness case | Current owner | Contract |
|---|---|---|
| iterative loop | `interp.TestInterpreter_Run/jits top-level loop` | loop result plus native emission |
| recursive calls | `interp.TestARM64_SelfCallWithRefArg` and `asm.TestAssembler_Build/resolves a backward call within the build` | recursive native entry plus in-build backward `BL` resolution |
| wide live ranges | `asm.TestAssembler_Build/spills under register pressure` and `interp.TestPool_Get/runs a spilled shared native entry from fresh goroutines` | oversubscribed register bank, balanced spill frame, and emitted native execution |
| threaded recursion | threaded oracle in `interp.TestARM64_SelfCallWithRefArg` | recursive result with JIT disabled |

## Public API Ownership

Exact ownership means a top-level `Test<Func>` or `Test<Type>_<Method>` exists. Shared family ownership is allowed only for large mechanical constructor families when a completeness gate proves every constructor has a golden or smoke case.

The inventory includes exported functions and exported methods on exported receiver types, including architecture-specific APIs. It excludes exported constants, variables, and type declarations because their behavior is owned by functions and methods; exported methods on private receiver types because they are not externally nameable; and generated declarations because generator completeness owns them. Architecture stubs remain included when they expose a callable public contract.

ARM64 instruction factories are the sole shared-family exception. `TestEncoder_Encode` owns exact machine bytes, `TestInstructionFactories` owns convenience-wrapper shape, and its AST gate fails when any exported factory lacks a test call. This avoids 152 one-line top-level tests that would duplicate the encoder table without adding behavior.

### Package Summary

| Package | Exported owners | Owned | Shared family | Missing |
|---|---:|---:|---:|---:|
| `analysis` | 5 | 5 | 0 | 0 |
| `internal/asm` | 37 | 37 | 0 | 0 |
| `internal/asm/amd64` | 1 | 1 | 0 | 0 |
| `internal/asm/arm64` | 155 | 155 | 152 | 0 |
| `cli` | 6 | 6 | 0 | 0 |
| `debug` | 12 | 12 | 0 | 0 |
| `instr` | 44 | 44 | 0 | 0 |
| `interp` | 83 | 83 | 0 | 0 |
| `optimize` | 4 | 4 | 0 | 0 |
| `pass` | 9 | 9 | 0 | 0 |
| `prof` | 22 | 22 | 0 | 0 |
| `program` | 25 | 25 | 0 | 0 |
| `transform` | 10 | 10 | 0 | 0 |
| `types` | 171 | 171 | 0 | 0 |

### Symbol Matrix

| Production file | Expected owner test | Ownership |
|---|---|---|
| `analysis/blocks.go` | `TestBlocksAnalysis_Run` | ✅ |
| `analysis/blocks.go` | `TestBlocks` | ✅ |
| `analysis/blocks.go` | `TestNewBlocksAnalysis` | ✅ |
| `analysis/gvn.go` | `TestGVNAnalysis_Run` | ✅ |
| `analysis/gvn.go` | `TestNewGVNAnalysis` | ✅ |
| `internal/asm/assembler.go` | `TestNew` | ✅ |
| `internal/asm/assembler.go` | `TestAssembler_Reg` | ✅ |
| `internal/asm/assembler.go` | `TestAssembler_Label` | ✅ |
| `internal/asm/assembler.go` | `TestAssembler_Bind` | ✅ |
| `internal/asm/assembler.go` | `TestAssembler_Pin` | ✅ |
| `internal/asm/assembler.go` | `TestAssembler_Emit` | ✅ |
| `internal/asm/assembler.go` | `TestAssembler_Build` | ✅ |
| `internal/asm/buffer.go` | `TestNewBuffer` | ✅ |
| `internal/asm/buffer.go` | `TestBuffer_Free` | ✅ |
| `internal/asm/instr.go` | `TestInstruction_String` | ✅ |
| `internal/asm/link.go` | `TestLink` | ✅ |
| `internal/asm/operand.go` | `TestV` | ✅ |
| `internal/asm/operand.go` | `TestP` | ✅ |
| `internal/asm/operand.go` | `TestImm` | ✅ |
| `internal/asm/operand.go` | `TestMem` | ✅ |
| `internal/asm/operand.go` | `TestVRegOperand_String` | ✅ |
| `internal/asm/operand.go` | `TestPRegOperand_String` | ✅ |
| `internal/asm/operand.go` | `TestImmOperand_String` | ✅ |
| `internal/asm/operand.go` | `TestLabelOperand_String` | ✅ |
| `internal/asm/operand.go` | `TestMemOperand_String` | ✅ |
| `internal/asm/reg.go` | `TestNewPReg` | ✅ |
| `internal/asm/reg.go` | `TestNewVReg` | ✅ |
| `internal/asm/reg.go` | `TestNewRegInfo` | ✅ |
| `internal/asm/reg.go` | `TestNewRegMask` | ✅ |
| `internal/asm/reg.go` | `TestPReg_ID` | ✅ |
| `internal/asm/reg.go` | `TestPReg_Type` | ✅ |
| `internal/asm/reg.go` | `TestPReg_Width` | ✅ |
| `internal/asm/reg.go` | `TestPReg_String` | ✅ |
| `internal/asm/reg.go` | `TestVReg_ID` | ✅ |
| `internal/asm/reg.go` | `TestVReg_Type` | ✅ |
| `internal/asm/reg.go` | `TestVReg_Width` | ✅ |
| `internal/asm/reg.go` | `TestVReg_String` | ✅ |
| `internal/asm/reg.go` | `TestRegMask_Set` | ✅ |
| `internal/asm/reg.go` | `TestRegMask_Clear` | ✅ |
| `internal/asm/reg.go` | `TestRegMask_Contains` | ✅ |
| `internal/asm/reg.go` | `TestRegMask_First` | ✅ |
| `internal/asm/reg.go` | `TestRegInfo_Allocatable` | ✅ |
| `internal/asm/amd64/arch.go` | `TestNew` | ✅ |
| `internal/asm/arm64/arch.go` | `TestNew` | ✅ |
| `internal/asm/arm64/encoder.go` | `TestEncoder_Encode` | ✅ |
| `internal/asm/arm64/encoder.go` | `TestNewEncoder` | ✅ |
| `internal/asm/arm64/instr.go` | `TestADC` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestADCS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestADD` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestADDI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestADDS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestADDSI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestADDV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestAND` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestANDI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestANDS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestANDSI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestASR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestASRI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBCC` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBCS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBCondLabel` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBEQ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBGE` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBGT` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBHI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBIC` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBICS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBL` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBLE` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBLLabel` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBLR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBLS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBLT` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBLabel` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBMI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBNE` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBPL` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBRK` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBVC` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestBVS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCBNZ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCBNZLabel` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCBZ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCBZLabel` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCCMP` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCCMPI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCLZ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCMN` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCMNI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCMP` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCMPI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCNT` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCSEL` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCSET` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCSETM` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCSINC` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCSINV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestCSNEG` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestDMB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestDSB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestEON` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestEOR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestEORI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestERET` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFABS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFADD` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFCMP` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFCMPE` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFCVT` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFCVTZS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFCVTZU` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFDIV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFMADD` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFMAX` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFMIN` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFMOV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFMSUB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFMUL` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFNEG` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFNMADD` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFNMSUB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFRINTM` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFRINTN` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFRINTP` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFRINTZ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFSQRT` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestFSUB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestHLT` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestISB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDP` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDRB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDRH` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDRR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDRSB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDRSH` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLDRSW` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLSL` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLSLI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLSR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestLSRI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMADD` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMNEG` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMOV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMOVI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMOVK` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMOVN` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMOVZ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMRS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMSR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMSUB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMUL` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestMVN` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestNEG` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestNEGS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestNOP` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestORN` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestORR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestORRI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestRBIT` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestRET` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestREV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestREV16` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestREV32` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestROR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestRORI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSBC` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSBCS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSBFX` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSCVTF` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSDIV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSTP` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSTR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSTRB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSTRH` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSTRR` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSTRW` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSUB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSUBI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSUBS` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSUBSI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSVC` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSXTB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSXTH` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestSXTW` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestTBNZ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestTBZ` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestTST` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestTSTI` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestUCVTF` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestUDIV` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestUXTB` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestUXTH` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `internal/asm/arm64/instr.go` | `TestUXTW` | Shared: `TestEncoder_Encode` / `TestInstructionFactories` |
| `cli/cli.go` | `TestRoot` | ✅ |
| `cli/cli.go` | `TestWithFS` | ✅ |
| `cli/fs.go` | `TestOS` | ✅ |
| `cli/repl.go` | `TestNewREPL` | ✅ |
| `cli/repl.go` | `TestREPL_Run` | ✅ |
| `cli/run.go` | `TestNewRunCommand` | ✅ |
| `debug/debugger.go` | `TestDebugger_Break` | ✅ |
| `debug/debugger.go` | `TestDebugger_BreakIf` | ✅ |
| `debug/debugger.go` | `TestDebugger_Breakpoints` | ✅ |
| `debug/debugger.go` | `TestDebugger_Clear` | ✅ |
| `debug/debugger.go` | `TestDebugger_Continue` | ✅ |
| `debug/debugger.go` | `TestDebugger_Enable` | ✅ |
| `debug/debugger.go` | `TestDebugger_Finish` | ✅ |
| `debug/debugger.go` | `TestDebugger_Hook` | ✅ |
| `debug/debugger.go` | `TestDebugger_Next` | ✅ |
| `debug/debugger.go` | `TestDebugger_Step` | ✅ |
| `debug/debugger.go` | `TestDebugger_Stop` | ✅ |
| `debug/debugger.go` | `TestNewDebugger` | ✅ |
| `instr/builder.go` | `TestBuilder_Append` | ✅ |
| `instr/builder.go` | `TestBuilder_Assemble` | ✅ |
| `instr/builder.go` | `TestBuilder_Bind` | ✅ |
| `instr/builder.go` | `TestBuilder_Br` | ✅ |
| `instr/builder.go` | `TestBuilder_BrIf` | ✅ |
| `instr/builder.go` | `TestBuilder_BrTable` | ✅ |
| `instr/builder.go` | `TestBuilder_Emit` | ✅ |
| `instr/builder.go` | `TestBuilder_Handlers` | ✅ |
| `instr/builder.go` | `TestBuilder_Label` | ✅ |
| `instr/builder.go` | `TestBuilder_Try` | ✅ |
| `instr/builder.go` | `TestNewBuilder` | ✅ |
| `instr/code.go` | `TestFormat` | ✅ |
| `instr/code.go` | `TestMarshal` | ✅ |
| `instr/code.go` | `TestTargets` | ✅ |
| `instr/code.go` | `TestUnmarshal` | ✅ |
| `instr/instr.go` | `TestInstruction_Opcode` | ✅ |
| `instr/instr.go` | `TestInstruction_Operand` | ✅ |
| `instr/instr.go` | `TestInstruction_Operands` | ✅ |
| `instr/instr.go` | `TestInstruction_SetOperand` | ✅ |
| `instr/instr.go` | `TestInstruction_String` | ✅ |
| `instr/instr.go` | `TestInstruction_Type` | ✅ |
| `instr/instr.go` | `TestInstruction_Width` | ✅ |
| `instr/instr.go` | `TestNew` | ✅ |
| `instr/kind.go` | `TestKind_IsNumeric` | ✅ |
| `instr/kind.go` | `TestKind_Repr` | ✅ |
| `instr/kind.go` | `TestKind_Size` | ✅ |
| `instr/kind.go` | `TestKind_String` | ✅ |
| `instr/opcode.go` | `TestOpcode_IsBranch` | ✅ |
| `instr/parse.go` | `TestParse` | ✅ |
| `instr/parse.go` | `TestParseAll` | ✅ |
| `instr/parse.go` | `TestParseI16` | ✅ |
| `instr/parse.go` | `TestParseI32` | ✅ |
| `instr/parse.go` | `TestParseI8` | ✅ |
| `instr/parse.go` | `TestParseU16` | ✅ |
| `instr/parse.go` | `TestParseU32` | ✅ |
| `instr/parse.go` | `TestParseU8` | ✅ |
| `instr/parse.go` | `TestReadI16` | ✅ |
| `instr/parse.go` | `TestReadI32` | ✅ |
| `instr/parse.go` | `TestReadI8` | ✅ |
| `instr/parse.go` | `TestReadU16` | ✅ |
| `instr/parse.go` | `TestReadU32` | ✅ |
| `instr/parse.go` | `TestReadU8` | ✅ |
| `instr/type.go` | `TestTypeOf` | ✅ |
| `instr/type.go` | `TestValid` | ✅ |
| `interp/codec.go` | `TestNewRegistry` | ✅ |
| `interp/codec.go` | `TestRegistry_Marshal` | ✅ |
| `interp/codec.go` | `TestRegistry_Unmarshal` | ✅ |
| `interp/codec.go` | `TestWithMarshaler` | ✅ |
| `interp/codec.go` | `TestWithUnmarshaler` | ✅ |
| `interp/decode.go` | `TestDecoder_Interp` | ✅ |
| `interp/decode.go` | `TestDecoder_Unmarshal` | ✅ |
| `interp/decode.go` | `TestUnmarshalerFunc_Unmarshal` | ✅ |
| `interp/encode.go` | `TestEncoder_Interp` | ✅ |
| `interp/encode.go` | `TestEncoder_Marshal` | ✅ |
| `interp/encode.go` | `TestMarshalerFunc_Marshal` | ✅ |
| `interp/error.go` | `TestErrorCode` | ✅ |
| `interp/error.go` | `TestRuntimeError_Error` | ✅ |
| `interp/error.go` | `TestRuntimeError_Unwrap` | ✅ |
| `interp/host.go` | `TestHostArray_Append` | ✅ |
| `interp/host.go` | `TestHostArray_Array` | ✅ |
| `interp/host.go` | `TestHostArray_Delete` | ✅ |
| `interp/host.go` | `TestHostArray_Element` | ✅ |
| `interp/host.go` | `TestHostArray_Fill` | ✅ |
| `interp/host.go` | `TestHostArray_Kind` | ✅ |
| `interp/host.go` | `TestHostArray_Len` | ✅ |
| `interp/host.go` | `TestHostArray_SetElement` | ✅ |
| `interp/host.go` | `TestHostArray_String` | ✅ |
| `interp/host.go` | `TestHostArray_Type` | ✅ |
| `interp/host.go` | `TestHostFunction_Kind` | ✅ |
| `interp/host.go` | `TestHostFunction_String` | ✅ |
| `interp/host.go` | `TestHostFunction_Type` | ✅ |
| `interp/host.go` | `TestHostMap_Clear` | ✅ |
| `interp/host.go` | `TestHostMap_Delete` | ✅ |
| `interp/host.go` | `TestHostMap_Get` | ✅ |
| `interp/host.go` | `TestHostMap_Kind` | ✅ |
| `interp/host.go` | `TestHostMap_Len` | ✅ |
| `interp/host.go` | `TestHostMap_Map` | ✅ |
| `interp/host.go` | `TestHostMap_Set` | ✅ |
| `interp/host.go` | `TestHostMap_String` | ✅ |
| `interp/host.go` | `TestHostMap_Type` | ✅ |
| `interp/host.go` | `TestHostStruct_Field` | ✅ |
| `interp/host.go` | `TestHostStruct_Kind` | ✅ |
| `interp/host.go` | `TestHostStruct_SetField` | ✅ |
| `interp/host.go` | `TestHostStruct_String` | ✅ |
| `interp/host.go` | `TestHostStruct_Type` | ✅ |
| `interp/host.go` | `TestNewHostFunction` | ✅ |
| `interp/interp.go` | `TestInterpreter_Alloc` | ✅ |
| `interp/interp.go` | `TestInterpreter_Close` | ✅ |
| `interp/interp.go` | `TestInterpreter_Const` | ✅ |
| `interp/interp.go` | `TestInterpreter_Context` | ✅ |
| `interp/interp.go` | `TestInterpreter_FP` | ✅ |
| `interp/interp.go` | `TestInterpreter_Frame` | ✅ |
| `interp/interp.go` | `TestInterpreter_Func` | ✅ |
| `interp/interp.go` | `TestInterpreter_Global` | ✅ |
| `interp/interp.go` | `TestInterpreter_IP` | ✅ |
| `interp/interp.go` | `TestInterpreter_Len` | ✅ |
| `interp/interp.go` | `TestInterpreter_Load` | ✅ |
| `interp/interp.go` | `TestInterpreter_Local` | ✅ |
| `interp/interp.go` | `TestInterpreter_Marshal` | ✅ |
| `interp/interp.go` | `TestInterpreter_Opcode` | ✅ |
| `interp/interp.go` | `TestInterpreter_Peek` | ✅ |
| `interp/interp.go` | `TestInterpreter_Pop` | ✅ |
| `interp/interp.go` | `TestInterpreter_PopBoxed` | ✅ |
| `interp/interp.go` | `TestInterpreter_Push` | ✅ |
| `interp/interp.go` | `TestInterpreter_Release` | ✅ |
| `interp/interp.go` | `TestInterpreter_Reset` | ✅ |
| `interp/interp.go` | `TestInterpreter_Retain` | ✅ |
| `interp/interp.go` | `TestInterpreter_Run` | ✅ |
| `interp/interp.go` | `TestInterpreter_SetGlobal` | ✅ |
| `interp/interp.go` | `TestInterpreter_SetLocal` | ✅ |
| `interp/interp.go` | `TestInterpreter_Store` | ✅ |
| `interp/interp.go` | `TestInterpreter_Unmarshal` | ✅ |
| `interp/interp.go` | `TestNew` | ✅ |
| `interp/interp.go` | `TestWithCodec` | ✅ |
| `interp/interp.go` | `TestWithFrame` | ✅ |
| `interp/interp.go` | `TestWithFuel` | ✅ |
| `interp/interp.go` | `TestWithHeap` | ✅ |
| `interp/interp.go` | `TestWithHeapLimit` | ✅ |
| `interp/interp.go` | `TestWithHook` | ✅ |
| `interp/interp.go` | `TestWithProfiler` | ✅ |
| `interp/interp.go` | `TestWithStack` | ✅ |
| `interp/interp.go` | `TestWithThreshold` | ✅ |
| `interp/interp.go` | `TestWithTick` | ✅ |
| `interp/pool.go` | `TestNewPool` | ✅ |
| `interp/pool.go` | `TestPool_Close` | ✅ |
| `interp/pool.go` | `TestPool_Get` | ✅ |
| `interp/pool.go` | `TestPool_Put` | ✅ |
| `optimize/optimizer.go` | `TestNew` | ✅ |
| `optimize/optimizer.go` | `TestOptimizer_Add` | ✅ |
| `optimize/optimizer.go` | `TestOptimizer_Level` | ✅ |
| `optimize/optimizer.go` | `TestOptimizer_Optimize` | ✅ |
| `pass/manager.go` | `TestGetResult` | ✅ |
| `pass/manager.go` | `TestManager_Invalidate` | ✅ |
| `pass/manager.go` | `TestNewManager` | ✅ |
| `pass/manager.go` | `TestRegister` | ✅ |
| `pass/pass.go` | `TestPreserveAll` | ✅ |
| `pass/pass.go` | `TestPreserveNone` | ✅ |
| `pass/pipeline.go` | `TestNewPipeline` | ✅ |
| `pass/pipeline.go` | `TestPipeline_Add` | ✅ |
| `pass/pipeline.go` | `TestPipeline_Run` | ✅ |
| `prof/collector.go` | `TestCollector_Add` | ✅ |
| `prof/collector.go` | `TestCollector_AddMetric` | ✅ |
| `prof/collector.go` | `TestCollector_IP` | ✅ |
| `prof/collector.go` | `TestCollector_IPs` | ✅ |
| `prof/collector.go` | `TestCollector_Metric` | ✅ |
| `prof/collector.go` | `TestCollector_Metrics` | ✅ |
| `prof/collector.go` | `TestCollector_Opcode` | ✅ |
| `prof/collector.go` | `TestCollector_Samples` | ✅ |
| `prof/collector.go` | `TestCollector_Total` | ✅ |
| `prof/collector.go` | `TestCollector_Value` | ✅ |
| `prof/collector.go` | `TestNewCollector` | ✅ |
| `prof/jit.go` | `TestCollector_RecordCapture` | ✅ |
| `prof/jit.go` | `TestCollector_RecordCompile` | ✅ |
| `prof/jit.go` | `TestCollector_RecordEmit` | ✅ |
| `prof/jit.go` | `TestCollector_RegisterEntry` | ✅ |
| `prof/jit.go` | `TestCollector_RegisterExit` | ✅ |
| `prof/jit.go` | `TestCollector_RegisterYield` | ✅ |
| `prof/jit.go` | `TestCounter_Inc` | ✅ |
| `prof/profiler.go` | `TestNew` | ✅ |
| `prof/profiler.go` | `TestProfiler_Flush` | ✅ |
| `prof/profiler.go` | `TestProfiler_Metric` | ✅ |
| `prof/profiler.go` | `TestProfiler_Metrics` | ✅ |
| `program/builder.go` | `TestBuilder_Bind` | ✅ |
| `program/builder.go` | `TestBuilder_Br` | ✅ |
| `program/builder.go` | `TestBuilder_BrIf` | ✅ |
| `program/builder.go` | `TestBuilder_BrTable` | ✅ |
| `program/builder.go` | `TestBuilder_Build` | ✅ |
| `program/builder.go` | `TestBuilder_Const` | ✅ |
| `program/builder.go` | `TestBuilder_ConstGet` | ✅ |
| `program/builder.go` | `TestBuilder_Emit` | ✅ |
| `program/builder.go` | `TestBuilder_Globals` | ✅ |
| `program/builder.go` | `TestBuilder_Label` | ✅ |
| `program/builder.go` | `TestBuilder_Locals` | ✅ |
| `program/builder.go` | `TestBuilder_Try` | ✅ |
| `program/builder.go` | `TestBuilder_Type` | ✅ |
| `program/builder.go` | `TestNewBuilder` | ✅ |
| `program/parse.go` | `TestParse` | ✅ |
| `program/program.go` | `TestNew` | ✅ |
| `program/program.go` | `TestProgram_String` | ✅ |
| `program/program.go` | `TestWithConstants` | ✅ |
| `program/program.go` | `TestWithGlobals` | ✅ |
| `program/program.go` | `TestWithHandlers` | ✅ |
| `program/program.go` | `TestWithLocals` | ✅ |
| `program/program.go` | `TestWithTypes` | ✅ |
| `program/verify.go` | `TestVerify` | ✅ |
| `program/verify.go` | `TestVerifyError_Error` | ✅ |
| `program/verify.go` | `TestVerifyError_Unwrap` | ✅ |
| `transform/as.go` | `TestAlgebraicPass_Run` | ✅ |
| `transform/as.go` | `TestNewAlgebraicPass` | ✅ |
| `transform/cd.go` | `TestDedupPass_Run` | ✅ |
| `transform/cd.go` | `TestNewDedupPass` | ✅ |
| `transform/cf.go` | `TestFoldPass_Run` | ✅ |
| `transform/cf.go` | `TestNewFoldPass` | ✅ |
| `transform/dce.go` | `TestDCEPass_Run` | ✅ |
| `transform/dce.go` | `TestNewDCEPass` | ✅ |
| `transform/gvn.go` | `TestGVNPass_Run` | ✅ |
| `transform/gvn.go` | `TestNewGVNPass` | ✅ |
| `types/array.go` | `TestArrayType_Cast` | ✅ |
| `types/array.go` | `TestArrayType_Equals` | ✅ |
| `types/array.go` | `TestArrayType_Kind` | ✅ |
| `types/array.go` | `TestArrayType_String` | ✅ |
| `types/array.go` | `TestArray_Kind` | ✅ |
| `types/array.go` | `TestArray_Refs` | ✅ |
| `types/array.go` | `TestArray_String` | ✅ |
| `types/array.go` | `TestArray_Type` | ✅ |
| `types/array.go` | `TestNewArray` | ✅ |
| `types/array.go` | `TestNewArrayType` | ✅ |
| `types/array.go` | `TestTypedArray_Kind` | ✅ |
| `types/array.go` | `TestTypedArray_String` | ✅ |
| `types/array.go` | `TestTypedArray_Type` | ✅ |
| `types/boxed.go` | `TestBox` | ✅ |
| `types/boxed.go` | `TestBoxF32` | ✅ |
| `types/boxed.go` | `TestBoxF64` | ✅ |
| `types/boxed.go` | `TestBoxI1` | ✅ |
| `types/boxed.go` | `TestBoxI32` | ✅ |
| `types/boxed.go` | `TestBoxI64` | ✅ |
| `types/boxed.go` | `TestBoxI8` | ✅ |
| `types/boxed.go` | `TestBoxRef` | ✅ |
| `types/boxed.go` | `TestBoxed_Bool` | ✅ |
| `types/boxed.go` | `TestBoxed_F32` | ✅ |
| `types/boxed.go` | `TestBoxed_F64` | ✅ |
| `types/boxed.go` | `TestBoxed_I32` | ✅ |
| `types/boxed.go` | `TestBoxed_I64` | ✅ |
| `types/boxed.go` | `TestBoxed_I8` | ✅ |
| `types/boxed.go` | `TestBoxed_Kind` | ✅ |
| `types/boxed.go` | `TestBoxed_Ref` | ✅ |
| `types/boxed.go` | `TestBoxed_String` | ✅ |
| `types/boxed.go` | `TestBoxed_Type` | ✅ |
| `types/boxed.go` | `TestIsBoxable` | ✅ |
| `types/boxed.go` | `TestTag` | ✅ |
| `types/boxed.go` | `TestUnbox` | ✅ |
| `types/closure.go` | `TestClosure_Kind` | ✅ |
| `types/closure.go` | `TestClosure_Refs` | ✅ |
| `types/closure.go` | `TestClosure_String` | ✅ |
| `types/closure.go` | `TestClosure_Type` | ✅ |
| `types/closure.go` | `TestNewClosure` | ✅ |
| `types/error.go` | `TestError_Code` | ✅ |
| `types/error.go` | `TestError_Error` | ✅ |
| `types/error.go` | `TestError_Kind` | ✅ |
| `types/error.go` | `TestError_Refs` | ✅ |
| `types/error.go` | `TestError_String` | ✅ |
| `types/error.go` | `TestError_Type` | ✅ |
| `types/error.go` | `TestError_Unwrap` | ✅ |
| `types/error.go` | `TestError_Value` | ✅ |
| `types/error.go` | `TestNewError` | ✅ |
| `types/error.go` | `TestWrapError` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Bind` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Br` | ✅ |
| `types/function.go` | `TestFunctionBuilder_BrIf` | ✅ |
| `types/function.go` | `TestFunctionBuilder_BrTable` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Build` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Emit` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Label` | ✅ |
| `types/function.go` | `TestFunctionBuilder_MustBuild` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Try` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Captures` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Locals` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Params` | ✅ |
| `types/function.go` | `TestFunctionBuilder_Returns` | ✅ |
| `types/function.go` | `TestFunctionType_Cast` | ✅ |
| `types/function.go` | `TestFunctionType_Equals` | ✅ |
| `types/function.go` | `TestFunctionType_Kind` | ✅ |
| `types/function.go` | `TestFunctionType_String` | ✅ |
| `types/function.go` | `TestFunction_Declared` | ✅ |
| `types/function.go` | `TestFunction_Kind` | ✅ |
| `types/function.go` | `TestFunction_Slots` | ✅ |
| `types/function.go` | `TestFunction_String` | ✅ |
| `types/function.go` | `TestFunction_Type` | ✅ |
| `types/function.go` | `TestNewFunction` | ✅ |
| `types/function.go` | `TestNewFunctionBuilder` | ✅ |
| `types/iterator.go` | `TestIteratorType_Cast` | ✅ |
| `types/iterator.go` | `TestIteratorType_Equals` | ✅ |
| `types/iterator.go` | `TestIteratorType_Kind` | ✅ |
| `types/iterator.go` | `TestIteratorType_String` | ✅ |
| `types/iterator.go` | `TestNewIteratorType` | ✅ |
| `types/map.go` | `TestMapIterator_Current` | ✅ |
| `types/map.go` | `TestMapIterator_Done` | ✅ |
| `types/map.go` | `TestMapIterator_Kind` | ✅ |
| `types/map.go` | `TestMapIterator_Next` | ✅ |
| `types/map.go` | `TestMapIterator_Refs` | ✅ |
| `types/map.go` | `TestMapIterator_String` | ✅ |
| `types/map.go` | `TestMapIterator_Type` | ✅ |
| `types/map.go` | `TestMapKey_String` | ✅ |
| `types/map.go` | `TestMapKey_Value` | ✅ |
| `types/map.go` | `TestMapType_Cast` | ✅ |
| `types/map.go` | `TestMapType_Equals` | ✅ |
| `types/map.go` | `TestMapType_Kind` | ✅ |
| `types/map.go` | `TestMapType_String` | ✅ |
| `types/map.go` | `TestMap_Clear` | ✅ |
| `types/map.go` | `TestMap_Delete` | ✅ |
| `types/map.go` | `TestMap_Get` | ✅ |
| `types/map.go` | `TestMap_Kind` | ✅ |
| `types/map.go` | `TestMap_Len` | ✅ |
| `types/map.go` | `TestMap_Range` | ✅ |
| `types/map.go` | `TestMap_Refs` | ✅ |
| `types/map.go` | `TestMap_Set` | ✅ |
| `types/map.go` | `TestMap_String` | ✅ |
| `types/map.go` | `TestMap_Type` | ✅ |
| `types/map.go` | `TestNewMap` | ✅ |
| `types/map.go` | `TestNewMapForType` | ✅ |
| `types/map.go` | `TestNewMapIterator` | ✅ |
| `types/map.go` | `TestNewMapType` | ✅ |
| `types/map.go` | `TestNewMapWithCapacity` | ✅ |
| `types/map.go` | `TestNewTypedMap` | ✅ |
| `types/map.go` | `TestTypedMap_Clear` | ✅ |
| `types/map.go` | `TestTypedMap_Delete` | ✅ |
| `types/map.go` | `TestTypedMap_Get` | ✅ |
| `types/map.go` | `TestTypedMap_Kind` | ✅ |
| `types/map.go` | `TestTypedMap_Len` | ✅ |
| `types/map.go` | `TestTypedMap_Range` | ✅ |
| `types/map.go` | `TestTypedMap_Refs` | ✅ |
| `types/map.go` | `TestTypedMap_Set` | ✅ |
| `types/map.go` | `TestTypedMap_String` | ✅ |
| `types/map.go` | `TestTypedMap_Type` | ✅ |
| `types/parse.go` | `TestParse` | ✅ |
| `types/parse.go` | `TestParseFunction` | ✅ |
| `types/primitive.go` | `TestBool` | ✅ |
| `types/primitive.go` | `TestF32_Kind` | ✅ |
| `types/primitive.go` | `TestF32_String` | ✅ |
| `types/primitive.go` | `TestF32_Type` | ✅ |
| `types/primitive.go` | `TestF64_Kind` | ✅ |
| `types/primitive.go` | `TestF64_String` | ✅ |
| `types/primitive.go` | `TestF64_Type` | ✅ |
| `types/primitive.go` | `TestI1_Kind` | ✅ |
| `types/primitive.go` | `TestI1_String` | ✅ |
| `types/primitive.go` | `TestI1_Type` | ✅ |
| `types/primitive.go` | `TestI32_Kind` | ✅ |
| `types/primitive.go` | `TestI32_String` | ✅ |
| `types/primitive.go` | `TestI32_Type` | ✅ |
| `types/primitive.go` | `TestI64_Kind` | ✅ |
| `types/primitive.go` | `TestI64_String` | ✅ |
| `types/primitive.go` | `TestI64_Type` | ✅ |
| `types/primitive.go` | `TestI8_Kind` | ✅ |
| `types/primitive.go` | `TestI8_String` | ✅ |
| `types/primitive.go` | `TestI8_Type` | ✅ |
| `types/primitive.go` | `TestRef_Kind` | ✅ |
| `types/primitive.go` | `TestRef_String` | ✅ |
| `types/primitive.go` | `TestRef_Type` | ✅ |
| `types/string.go` | `TestNewStringIterator` | ✅ |
| `types/string.go` | `TestStringIterator_Current` | ✅ |
| `types/string.go` | `TestStringIterator_Done` | ✅ |
| `types/string.go` | `TestStringIterator_Kind` | ✅ |
| `types/string.go` | `TestStringIterator_Next` | ✅ |
| `types/string.go` | `TestStringIterator_Refs` | ✅ |
| `types/string.go` | `TestStringIterator_String` | ✅ |
| `types/string.go` | `TestStringIterator_Type` | ✅ |
| `types/string.go` | `TestString_Kind` | ✅ |
| `types/string.go` | `TestString_String` | ✅ |
| `types/string.go` | `TestString_Type` | ✅ |
| `types/struct.go` | `TestFieldWithName` | ✅ |
| `types/struct.go` | `TestNewStruct` | ✅ |
| `types/struct.go` | `TestNewStructField` | ✅ |
| `types/struct.go` | `TestNewStructType` | ✅ |
| `types/struct.go` | `TestStructType_Cast` | ✅ |
| `types/struct.go` | `TestStructType_Equals` | ✅ |
| `types/struct.go` | `TestStructType_FieldByName` | ✅ |
| `types/struct.go` | `TestStructType_FieldIndex` | ✅ |
| `types/struct.go` | `TestStructType_Kind` | ✅ |
| `types/struct.go` | `TestStructType_String` | ✅ |
| `types/struct.go` | `TestStruct_FieldByName` | ✅ |
| `types/struct.go` | `TestStruct_Kind` | ✅ |
| `types/struct.go` | `TestStruct_Refs` | ✅ |
| `types/struct.go` | `TestStruct_SetField` | ✅ |
| `types/struct.go` | `TestStruct_SetRaw` | ✅ |
| `types/struct.go` | `TestStruct_String` | ✅ |
| `types/struct.go` | `TestStruct_Type` | ✅ |
| `types/value.go` | `TestIsNull` | ✅ |
| `types/value.go` | `TestKinds` | ✅ |
| `types/value.go` | `TestZero` | ✅ |

## Opcode Ownership Matrix

`Metadata / parse` and `Runtime success` are enforced for every registered opcode. `Runtime error` is checked only where the shared corpus owns an error-bearing row; `—` means the opcode is currently specified through successful execution. `ARM64 parity` records exact differential scope, not backend capability; capability remains owned by `docs/instruction-set.md`.

| Opcode | Mnemonic | Metadata / parse | Verifier policy | Runtime success | Runtime error | ARM64 capability | ARM64 parity |
|---|---|---:|---|---:|---|---:|---|
| `NOP` | `nop` | ✅ | fixed zero | ✅ | — | ✅ | Runtime corpus only |
| `UNREACHABLE` | `unreachable` | ✅ | terminator | Review in Phase 6 | ✅ | ◐ | Runtime corpus only |
| `DROP` | `drop` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `DUP` | `dup` | ✅ | explicit | ✅ | — | ✅ | Runtime corpus only |
| `SWAP` | `swap` | ✅ | explicit | ✅ | — | ✅ | Runtime corpus only |
| `BR` | `br` | ✅ | fixed zero | ✅ | — | ✅ | Runtime corpus only |
| `BR_IF` | `br_if` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `BR_TABLE` | `br_table` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `SELECT` | `select` | ✅ | explicit | ✅ | — | ✅ | Runtime corpus only |
| `CALL` | `call` | ✅ | callee signature | ✅ | — | ◐ | Representative differential |
| `RETURN` | `return` | ✅ | return arity | ✅ | — | ✅ | Representative differential |
| `RETURN_CALL` | `return_call` | ✅ | callee signature | ✅ | — | ◐ | Runtime corpus only |
| `YIELD` | `yield` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Representative differential |
| `RESUME` | `resume` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `CORO_DONE` | `coro.done` | ✅ | fixed metadata | ✅ | — | ✅ | Representative differential |
| `CORO_VALUE` | `coro.value` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `GLOBAL_GET` | `global.get` | ✅ | fixed metadata | ✅ | — | ✅ | Representative differential |
| `GLOBAL_SET` | `global.set` | ✅ | fixed metadata | ✅ | — | ✅ | Representative differential |
| `GLOBAL_TEE` | `global.tee` | ✅ | explicit | ✅ | — | ✅ | Runtime corpus only |
| `LOCAL_GET` | `local.get` | ✅ | declared local | ✅ | — | ✅ | Runtime corpus only |
| `LOCAL_SET` | `local.set` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `LOCAL_TEE` | `local.tee` | ✅ | explicit | ✅ | — | ✅ | Runtime corpus only |
| `CONST_GET` | `const.get` | ✅ | constant kind | ✅ | — | ◐ | Representative differential |
| `UPVAL_GET` | `upval.get` | ✅ | declared capture | ✅ | — | ✅ | Runtime corpus only |
| `UPVAL_SET` | `upval.set` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `REF_NULL` | `ref.null` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `REF_NEW` | `ref.new` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `REF_GET` | `ref.get` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `REF_SET` | `ref.set` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `REF_TEST` | `ref.test` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `REF_CAST` | `ref.cast` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `REF_IS_NULL` | `ref.is_null` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `REF_EQ` | `ref.eq` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `REF_NE` | `ref.ne` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `I32_CONST` | `i32.const` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Representative differential |
| `I32_ADD` | `i32.add` | ✅ | fixed metadata | ✅ | — | ✅ | Bounded differential fuzz |
| `I32_SUB` | `i32.sub` | ✅ | fixed metadata | ✅ | — | ✅ | Bounded differential fuzz |
| `I32_MUL` | `i32.mul` | ✅ | fixed metadata | ✅ | — | ✅ | Bounded differential fuzz |
| `I32_DIV_S` | `i32.div_s` | ✅ | fixed metadata | ✅ | — | ✅ | Representative differential |
| `I32_DIV_U` | `i32.div_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_REM_S` | `i32.rem_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_REM_U` | `i32.rem_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_SHL` | `i32.shl` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_SHR_S` | `i32.shr_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_SHR_U` | `i32.shr_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_XOR` | `i32.xor` | ✅ | fixed metadata | ✅ | — | ✅ | Bounded differential fuzz |
| `I32_AND` | `i32.and` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_OR` | `i32.or` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_CLZ` | `i32.clz` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_CTZ` | `i32.ctz` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_POPCNT` | `i32.popcnt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_ROTL` | `i32.rotl` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_ROTR` | `i32.rotr` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_EXTEND8_S` | `i32.extend8_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_EXTEND16_S` | `i32.extend16_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_EQZ` | `i32.eqz` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_EQ` | `i32.eq` | ✅ | fixed metadata | ✅ | — | ✅ | Bounded differential fuzz |
| `I32_NE` | `i32.ne` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_LT_S` | `i32.lt_s` | ✅ | fixed metadata | ✅ | — | ✅ | Bounded differential fuzz |
| `I32_LT_U` | `i32.lt_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_GT_S` | `i32.gt_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_GT_U` | `i32.gt_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_LE_S` | `i32.le_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_LE_U` | `i32.le_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_GE_S` | `i32.ge_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_GE_U` | `i32.ge_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_TO_I64_S` | `i32.to_i64_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_TO_I64_U` | `i32.to_i64_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_TO_F32_U` | `i32.to_f32_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_TO_F32_S` | `i32.to_f32_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_TO_F64_U` | `i32.to_f64_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_TO_F64_S` | `i32.to_f64_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I32_REINTERPRET_F32` | `i32.reinterpret_f32` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_CONST` | `i64.const` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `I64_ADD` | `i64.add` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_SUB` | `i64.sub` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_MUL` | `i64.mul` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_DIV_S` | `i64.div_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_DIV_U` | `i64.div_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_REM_S` | `i64.rem_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_REM_U` | `i64.rem_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_SHL` | `i64.shl` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_SHR_S` | `i64.shr_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_SHR_U` | `i64.shr_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_XOR` | `i64.xor` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_AND` | `i64.and` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_OR` | `i64.or` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_CLZ` | `i64.clz` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_CTZ` | `i64.ctz` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_POPCNT` | `i64.popcnt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_ROTL` | `i64.rotl` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_ROTR` | `i64.rotr` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_EXTEND8_S` | `i64.extend8_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_EXTEND16_S` | `i64.extend16_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_EXTEND32_S` | `i64.extend32_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_EQZ` | `i64.eqz` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_EQ` | `i64.eq` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_NE` | `i64.ne` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_LT_S` | `i64.lt_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_LT_U` | `i64.lt_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_GT_S` | `i64.gt_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_GT_U` | `i64.gt_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_LE_S` | `i64.le_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_LE_U` | `i64.le_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_GE_S` | `i64.ge_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_GE_U` | `i64.ge_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_TO_I32` | `i64.to_i32` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_TO_F32_S` | `i64.to_f32_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_TO_F32_U` | `i64.to_f32_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_TO_F64_S` | `i64.to_f64_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_TO_F64_U` | `i64.to_f64_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `I64_REINTERPRET_F64` | `i64.reinterpret_f64` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_CONST` | `f32.const` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Runtime corpus only |
| `F32_ADD` | `f32.add` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_SUB` | `f32.sub` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_MUL` | `f32.mul` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_DIV` | `f32.div` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_REM` | `f32.rem` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Runtime corpus only |
| `F32_MOD` | `f32.mod` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Runtime corpus only |
| `F32_ABS` | `f32.abs` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_NEG` | `f32.neg` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_SQRT` | `f32.sqrt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_CEIL` | `f32.ceil` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_FLOOR` | `f32.floor` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_TRUNC` | `f32.trunc` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_NEAREST` | `f32.nearest` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_MIN` | `f32.min` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_MAX` | `f32.max` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_COPYSIGN` | `f32.copysign` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_EQ` | `f32.eq` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_NE` | `f32.ne` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_LT` | `f32.lt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_GT` | `f32.gt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_LE` | `f32.le` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_GE` | `f32.ge` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_TO_I32_S` | `f32.to_i32_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_TO_I32_U` | `f32.to_i32_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_TO_I64_S` | `f32.to_i64_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_TO_I64_U` | `f32.to_i64_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_TO_F64` | `f32.to_f64` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F32_REINTERPRET_I32` | `f32.reinterpret_i32` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_CONST` | `f64.const` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Runtime corpus only |
| `F64_ADD` | `f64.add` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_SUB` | `f64.sub` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_MUL` | `f64.mul` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_DIV` | `f64.div` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_REM` | `f64.rem` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Runtime corpus only |
| `F64_MOD` | `f64.mod` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Runtime corpus only |
| `F64_ABS` | `f64.abs` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_NEG` | `f64.neg` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_SQRT` | `f64.sqrt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_CEIL` | `f64.ceil` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_FLOOR` | `f64.floor` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_TRUNC` | `f64.trunc` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_NEAREST` | `f64.nearest` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_MIN` | `f64.min` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_MAX` | `f64.max` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_COPYSIGN` | `f64.copysign` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_EQ` | `f64.eq` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_NE` | `f64.ne` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_LT` | `f64.lt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_GT` | `f64.gt` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_LE` | `f64.le` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_GE` | `f64.ge` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_TO_I32_S` | `f64.to_i32_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_TO_I32_U` | `f64.to_i32_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_TO_I64_S` | `f64.to_i64_s` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_TO_I64_U` | `f64.to_i64_u` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_TO_F32` | `f64.to_f32` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `F64_REINTERPRET_I64` | `f64.reinterpret_i64` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `STRING_NEW_UTF32` | `string.new_utf32` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_LEN` | `string.len` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `STRING_CONCAT` | `string.concat` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_EQ` | `string.eq` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_NE` | `string.ne` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_LT` | `string.lt` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_GT` | `string.gt` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_LE` | `string.le` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_GE` | `string.ge` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRING_ENCODE_UTF32` | `string.encode_utf32` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `ARRAY_NEW` | `array.new` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Runtime corpus only |
| `ARRAY_NEW_DEFAULT` | `array.new_default` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Runtime corpus only |
| `ARRAY_LEN` | `array.len` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `ARRAY_GET` | `array.get` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Representative differential |
| `ARRAY_SET` | `array.set` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Representative differential |
| `ARRAY_FILL` | `array.fill` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Runtime corpus only |
| `ARRAY_COPY` | `array.copy` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Runtime corpus only |
| `ARRAY_APPEND` | `array.append` | ✅ | indeterminate arity | ✅ | — | ⬜ | Runtime corpus only |
| `ARRAY_DELETE` | `array.delete` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Runtime corpus only |
| `ARRAY_SLICE` | `array.slice` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRUCT_NEW` | `struct.new` | ✅ | declared fields | ✅ | — | ⬜ | Runtime corpus only |
| `STRUCT_NEW_DEFAULT` | `struct.new_default` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `STRUCT_GET` | `struct.get` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `STRUCT_SET` | `struct.set` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `MAP_NEW` | `map.new` | ✅ | indeterminate arity | ✅ | — | ⬜ | Runtime corpus only |
| `MAP_NEW_DEFAULT` | `map.new_default` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `MAP_LEN` | `map.len` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `MAP_GET` | `map.get` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `MAP_LOOKUP` | `map.lookup` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `MAP_SET` | `map.set` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `MAP_DELETE` | `map.delete` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `MAP_CLEAR` | `map.clear` | ✅ | fixed metadata | ✅ | — | ⬜ | Runtime corpus only |
| `MAP_KEYS` | `map.keys` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `CLOSURE_NEW` | `closure.new` | ✅ | indeterminate arity | ✅ | — | ⬜ | Runtime corpus only |
| `MAP_ITER` | `map.iter` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `THROW` | `throw` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `ERROR_NEW` | `error.new` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `ERROR_GET` | `error.get` | ✅ | fixed metadata | ✅ | — | ✅ | Runtime corpus only |
| `ERROR_CODE` | `error.code` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |
| `STRING_ITER` | `string.iter` | ✅ | fixed metadata | ✅ | — | ◐ | Runtime corpus only |

## Automated Gates

| Command | Contract |
|---|---|
| `make check` | generated files, module tidiness, formatting, vet, race tests, and ARM64 build checks |
| `make coverage-check` | full cross-package coverage run (`-coverpkg=./...`, so a package exercised only through its consumer still counts) and total coverage of at least the recorded 72.5% baseline |
| `make fuzz` | bounded smoke runs for every declared fuzz target |
| `make benchmark-pr` | quick deterministic benchmark report; no performance threshold |
| `make benchmark-core` | all canonical package and VM-kernel benchmarks |
| `make benchmark-nightly` | repeated canonical suite for scheduled reporting |
| `make benchmark-compare` | optional `compare`-tagged external runtime suite |

Coverage and contract ownership are separate. A line-coverage increase does not replace an owner test, opcode row, parity case, or trust-boundary fuzz target. Performance jobs fail only when fixtures or benchmark execution fail; measured regressions remain report-only until stable variance and practical thresholds are recorded.

## Maintenance Notes

- Add a public owner test with every behavior-bearing exported symbol.
- Add opcode metadata before runtime or verifier code; completeness gates must fail during an incomplete addition.
- Keep `runTests` as runtime opcode source of truth and use behavior-oriented names.
- Update this ownership matrix at each migration phase until every applicable owner is present.
- Keep backend capability in `docs/instruction-set.md`; record only parity-test ownership here.

## Related Docs

- `docs/coding-patterns.md` - test structure and naming rules
- `docs/instruction-set.md` - opcode semantics and backend capability
- `docs/verification.md` - verifier architecture and policies
- `docs/benchmarks.md` - benchmark methodology
