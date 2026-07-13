# Testing

Executable specification ownership, completeness gates, and migration status for minivm tests.

## When to Read

Read when adding or changing a public API, opcode, verifier rule, interpreter behavior, optimizer path, JIT path, fuzz target, or benchmark fixture.

## Source of Truth

| Concern | Source |
|---|---|
| test shape and naming | `docs/coding-patterns.md` §6 |
| public API behavior | production owner test matching the defining file |
| opcode metadata and mnemonic | `instr/type.go` and `TestValid` |
| verifier policy | `program/verify.go` and `TestVerify/defines_a_policy_for_every_opcode` |
| runtime opcode corpus | `interp/interp_test.go` `runTests` and `TestInterpreter_Run/covers_every_runtime_opcode` |
| backend support | `docs/instruction-set.md` |
| migration execution | `docs/plans/test-benchmark-rework.md` |

## Test Layers

| Layer | Default owner | Contract |
|---|---|---|
| Public contract | production-matched package test file | exported constructors, functions, methods, options, errors, and lifecycle behavior |
| Runtime specification | `interp.TestInterpreter_Run` | one visible bytecode fixture per opcode behavior, including traps and ownership |
| Semantic parity | owning transform, optimizer, or interpreter test | compare observable output across threaded, optimized, fused, JIT, exit, and deoptimization paths |
| Internal invariant | nearest implementation test file | only safety properties unavailable through public behavior |
| Fuzz | package `fuzz_test.go` | bounded trust-boundary and semantic differential properties |
| Integration | highest public package boundary | real parse-to-close flows without duplicating unit cases |

## Public API Ownership

Exact ownership means a top-level `Test<Func>` or `Test<Type>_<Method>` exists. Existing umbrella tests may already exercise missing exact owners; migration phases split them without changing semantics.

The inventory includes exported functions and exported methods on exported receiver types, including architecture-specific APIs. It excludes exported constants, variables, and type declarations because their behavior is owned by functions and methods; exported methods on private receiver types because they are not externally nameable; and generated declarations because generator completeness owns them. Architecture stubs remain included when they expose a callable public contract.

### Package Summary

| Package | Exported owners | Exact owners present | Missing |
|---|---:|---:|---:|
| `analysis` | 5 | 2 | 3 |
| `asm` | 44 | 29 | 15 |
| `asm/amd64` | 1 | 0 | 1 |
| `asm/arm64` | 155 | 3 | 152 |
| `cli` | 6 | 4 | 2 |
| `debug` | 12 | 1 | 11 |
| `instr` | 44 | 44 | 0 |
| `interp` | 66 | 66 | 0 |
| `optimize` | 4 | 3 | 1 |
| `pass` | 9 | 4 | 5 |
| `prof` | 22 | 12 | 10 |
| `program` | 25 | 25 | 0 |
| `transform` | 10 | 5 | 5 |
| `types` | 172 | 172 | 0 |

### Symbol Matrix

| Production file | Expected owner test | Present |
|---|---|---:|
| `analysis/blocks.go` | `TestBasicBlocksAnalysis_Run` | ✅ |
| `analysis/blocks.go` | `TestBlocks` | ⬜ |
| `analysis/blocks.go` | `TestNewBasicBlocksAnalysis` | ⬜ |
| `analysis/gvn.go` | `TestGlobalValueNumberingAnalysis_Run` | ✅ |
| `analysis/gvn.go` | `TestNewGlobalValueNumberingAnalysis` | ⬜ |
| `asm/assembler.go` | `TestAssembler_Bind` | ⬜ |
| `asm/assembler.go` | `TestAssembler_Build` | ✅ |
| `asm/assembler.go` | `TestAssembler_Emit` | ⬜ |
| `asm/assembler.go` | `TestAssembler_Entry` | ✅ |
| `asm/assembler.go` | `TestAssembler_Label` | ⬜ |
| `asm/assembler.go` | `TestAssembler_Pin` | ✅ |
| `asm/assembler.go` | `TestAssembler_Reg` | ⬜ |
| `asm/assembler.go` | `TestNew` | ⬜ |
| `asm/buffer.go` | `TestBuffer_Free` | ⬜ |
| `asm/buffer.go` | `TestBuffer_Write` | ✅ |
| `asm/buffer.go` | `TestNewBuffer` | ⬜ |
| `asm/instr.go` | `TestInstruction_Def` | ✅ |
| `asm/instr.go` | `TestInstruction_String` | ✅ |
| `asm/instr.go` | `TestInstruction_Uses` | ✅ |
| `asm/link.go` | `TestLink` | ✅ |
| `asm/operand.go` | `TestImm` | ✅ |
| `asm/operand.go` | `TestImmOperand_String` | ✅ |
| `asm/operand.go` | `TestLabelOp` | ✅ |
| `asm/operand.go` | `TestLabelOperand_String` | ✅ |
| `asm/operand.go` | `TestMem` | ✅ |
| `asm/operand.go` | `TestMemOperand_String` | ✅ |
| `asm/operand.go` | `TestP` | ✅ |
| `asm/operand.go` | `TestPRegOperand_String` | ✅ |
| `asm/operand.go` | `TestV` | ✅ |
| `asm/operand.go` | `TestVRegOperand_String` | ✅ |
| `asm/reg.go` | `TestNewPReg` | ✅ |
| `asm/reg.go` | `TestNewRegInfo` | ⬜ |
| `asm/reg.go` | `TestNewRegMask` | ✅ |
| `asm/reg.go` | `TestNewVReg` | ✅ |
| `asm/reg.go` | `TestPReg_ID` | ⬜ |
| `asm/reg.go` | `TestPReg_String` | ✅ |
| `asm/reg.go` | `TestPReg_Type` | ⬜ |
| `asm/reg.go` | `TestPReg_Width` | ⬜ |
| `asm/reg.go` | `TestRegInfo_Allocatable` | ⬜ |
| `asm/reg.go` | `TestRegMask_Clear` | ✅ |
| `asm/reg.go` | `TestRegMask_Contains` | ✅ |
| `asm/reg.go` | `TestRegMask_Count` | ✅ |
| `asm/reg.go` | `TestRegMask_First` | ✅ |
| `asm/reg.go` | `TestRegMask_PopFirst` | ✅ |
| `asm/reg.go` | `TestRegMask_Set` | ✅ |
| `asm/reg.go` | `TestVReg_ID` | ⬜ |
| `asm/reg.go` | `TestVReg_String` | ✅ |
| `asm/reg.go` | `TestVReg_Type` | ⬜ |
| `asm/reg.go` | `TestVReg_Width` | ⬜ |
| `asm/amd64/arch.go` | `TestNew` | ⬜ |
| `asm/arm64/arch.go` | `TestNew` | ✅ |
| `asm/arm64/encoder.go` | `TestEncoder_Encode` | ✅ |
| `asm/arm64/encoder.go` | `TestNewEncoder` | ⬜ |
| `asm/arm64/instr.go` | `TestADC` | ⬜ |
| `asm/arm64/instr.go` | `TestADCS` | ⬜ |
| `asm/arm64/instr.go` | `TestADD` | ⬜ |
| `asm/arm64/instr.go` | `TestADDI` | ⬜ |
| `asm/arm64/instr.go` | `TestADDS` | ⬜ |
| `asm/arm64/instr.go` | `TestADDSI` | ⬜ |
| `asm/arm64/instr.go` | `TestADDV` | ⬜ |
| `asm/arm64/instr.go` | `TestAND` | ⬜ |
| `asm/arm64/instr.go` | `TestANDI` | ⬜ |
| `asm/arm64/instr.go` | `TestANDS` | ⬜ |
| `asm/arm64/instr.go` | `TestANDSI` | ⬜ |
| `asm/arm64/instr.go` | `TestASR` | ⬜ |
| `asm/arm64/instr.go` | `TestASRI` | ⬜ |
| `asm/arm64/instr.go` | `TestB` | ⬜ |
| `asm/arm64/instr.go` | `TestBCC` | ⬜ |
| `asm/arm64/instr.go` | `TestBCS` | ⬜ |
| `asm/arm64/instr.go` | `TestBCondLabel` | ⬜ |
| `asm/arm64/instr.go` | `TestBEQ` | ⬜ |
| `asm/arm64/instr.go` | `TestBGE` | ⬜ |
| `asm/arm64/instr.go` | `TestBGT` | ⬜ |
| `asm/arm64/instr.go` | `TestBHI` | ⬜ |
| `asm/arm64/instr.go` | `TestBIC` | ⬜ |
| `asm/arm64/instr.go` | `TestBICS` | ⬜ |
| `asm/arm64/instr.go` | `TestBL` | ⬜ |
| `asm/arm64/instr.go` | `TestBLE` | ⬜ |
| `asm/arm64/instr.go` | `TestBLLabel` | ⬜ |
| `asm/arm64/instr.go` | `TestBLR` | ⬜ |
| `asm/arm64/instr.go` | `TestBLS` | ⬜ |
| `asm/arm64/instr.go` | `TestBLT` | ⬜ |
| `asm/arm64/instr.go` | `TestBLabel` | ⬜ |
| `asm/arm64/instr.go` | `TestBMI` | ⬜ |
| `asm/arm64/instr.go` | `TestBNE` | ⬜ |
| `asm/arm64/instr.go` | `TestBPL` | ⬜ |
| `asm/arm64/instr.go` | `TestBR` | ⬜ |
| `asm/arm64/instr.go` | `TestBRK` | ⬜ |
| `asm/arm64/instr.go` | `TestBVC` | ⬜ |
| `asm/arm64/instr.go` | `TestBVS` | ⬜ |
| `asm/arm64/instr.go` | `TestCBNZ` | ⬜ |
| `asm/arm64/instr.go` | `TestCBNZLabel` | ⬜ |
| `asm/arm64/instr.go` | `TestCBZ` | ⬜ |
| `asm/arm64/instr.go` | `TestCBZLabel` | ⬜ |
| `asm/arm64/instr.go` | `TestCCMP` | ⬜ |
| `asm/arm64/instr.go` | `TestCCMPI` | ⬜ |
| `asm/arm64/instr.go` | `TestCLZ` | ⬜ |
| `asm/arm64/instr.go` | `TestCMN` | ⬜ |
| `asm/arm64/instr.go` | `TestCMNI` | ⬜ |
| `asm/arm64/instr.go` | `TestCMP` | ⬜ |
| `asm/arm64/instr.go` | `TestCMPI` | ⬜ |
| `asm/arm64/instr.go` | `TestCNT` | ⬜ |
| `asm/arm64/instr.go` | `TestCSEL` | ⬜ |
| `asm/arm64/instr.go` | `TestCSET` | ⬜ |
| `asm/arm64/instr.go` | `TestCSETM` | ⬜ |
| `asm/arm64/instr.go` | `TestCSINC` | ⬜ |
| `asm/arm64/instr.go` | `TestCSINV` | ⬜ |
| `asm/arm64/instr.go` | `TestCSNEG` | ⬜ |
| `asm/arm64/instr.go` | `TestDMB` | ⬜ |
| `asm/arm64/instr.go` | `TestDSB` | ⬜ |
| `asm/arm64/instr.go` | `TestEON` | ⬜ |
| `asm/arm64/instr.go` | `TestEOR` | ⬜ |
| `asm/arm64/instr.go` | `TestEORI` | ⬜ |
| `asm/arm64/instr.go` | `TestERET` | ⬜ |
| `asm/arm64/instr.go` | `TestFABS` | ⬜ |
| `asm/arm64/instr.go` | `TestFADD` | ⬜ |
| `asm/arm64/instr.go` | `TestFCMP` | ⬜ |
| `asm/arm64/instr.go` | `TestFCMPE` | ⬜ |
| `asm/arm64/instr.go` | `TestFCVT` | ⬜ |
| `asm/arm64/instr.go` | `TestFCVTZS` | ⬜ |
| `asm/arm64/instr.go` | `TestFCVTZU` | ⬜ |
| `asm/arm64/instr.go` | `TestFDIV` | ⬜ |
| `asm/arm64/instr.go` | `TestFMADD` | ⬜ |
| `asm/arm64/instr.go` | `TestFMAX` | ⬜ |
| `asm/arm64/instr.go` | `TestFMIN` | ⬜ |
| `asm/arm64/instr.go` | `TestFMOV` | ⬜ |
| `asm/arm64/instr.go` | `TestFMSUB` | ⬜ |
| `asm/arm64/instr.go` | `TestFMUL` | ⬜ |
| `asm/arm64/instr.go` | `TestFNEG` | ⬜ |
| `asm/arm64/instr.go` | `TestFNMADD` | ⬜ |
| `asm/arm64/instr.go` | `TestFNMSUB` | ⬜ |
| `asm/arm64/instr.go` | `TestFRINTM` | ⬜ |
| `asm/arm64/instr.go` | `TestFRINTN` | ⬜ |
| `asm/arm64/instr.go` | `TestFRINTP` | ⬜ |
| `asm/arm64/instr.go` | `TestFRINTZ` | ⬜ |
| `asm/arm64/instr.go` | `TestFSQRT` | ⬜ |
| `asm/arm64/instr.go` | `TestFSUB` | ⬜ |
| `asm/arm64/instr.go` | `TestHLT` | ⬜ |
| `asm/arm64/instr.go` | `TestISB` | ⬜ |
| `asm/arm64/instr.go` | `TestLDI` | ✅ |
| `asm/arm64/instr.go` | `TestLDP` | ⬜ |
| `asm/arm64/instr.go` | `TestLDR` | ⬜ |
| `asm/arm64/instr.go` | `TestLDRB` | ⬜ |
| `asm/arm64/instr.go` | `TestLDRH` | ⬜ |
| `asm/arm64/instr.go` | `TestLDRR` | ⬜ |
| `asm/arm64/instr.go` | `TestLDRSB` | ⬜ |
| `asm/arm64/instr.go` | `TestLDRSH` | ⬜ |
| `asm/arm64/instr.go` | `TestLDRSW` | ⬜ |
| `asm/arm64/instr.go` | `TestLSL` | ⬜ |
| `asm/arm64/instr.go` | `TestLSLI` | ⬜ |
| `asm/arm64/instr.go` | `TestLSR` | ⬜ |
| `asm/arm64/instr.go` | `TestLSRI` | ⬜ |
| `asm/arm64/instr.go` | `TestMADD` | ⬜ |
| `asm/arm64/instr.go` | `TestMNEG` | ⬜ |
| `asm/arm64/instr.go` | `TestMOV` | ⬜ |
| `asm/arm64/instr.go` | `TestMOVI` | ⬜ |
| `asm/arm64/instr.go` | `TestMOVK` | ⬜ |
| `asm/arm64/instr.go` | `TestMOVN` | ⬜ |
| `asm/arm64/instr.go` | `TestMOVZ` | ⬜ |
| `asm/arm64/instr.go` | `TestMRS` | ⬜ |
| `asm/arm64/instr.go` | `TestMSR` | ⬜ |
| `asm/arm64/instr.go` | `TestMSUB` | ⬜ |
| `asm/arm64/instr.go` | `TestMUL` | ⬜ |
| `asm/arm64/instr.go` | `TestMVN` | ⬜ |
| `asm/arm64/instr.go` | `TestNEG` | ⬜ |
| `asm/arm64/instr.go` | `TestNEGS` | ⬜ |
| `asm/arm64/instr.go` | `TestNOP` | ⬜ |
| `asm/arm64/instr.go` | `TestORN` | ⬜ |
| `asm/arm64/instr.go` | `TestORR` | ⬜ |
| `asm/arm64/instr.go` | `TestORRI` | ⬜ |
| `asm/arm64/instr.go` | `TestRBIT` | ⬜ |
| `asm/arm64/instr.go` | `TestRET` | ⬜ |
| `asm/arm64/instr.go` | `TestREV` | ⬜ |
| `asm/arm64/instr.go` | `TestREV16` | ⬜ |
| `asm/arm64/instr.go` | `TestREV32` | ⬜ |
| `asm/arm64/instr.go` | `TestROR` | ⬜ |
| `asm/arm64/instr.go` | `TestRORI` | ⬜ |
| `asm/arm64/instr.go` | `TestSBC` | ⬜ |
| `asm/arm64/instr.go` | `TestSBCS` | ⬜ |
| `asm/arm64/instr.go` | `TestSBFX` | ⬜ |
| `asm/arm64/instr.go` | `TestSCVTF` | ⬜ |
| `asm/arm64/instr.go` | `TestSDIV` | ⬜ |
| `asm/arm64/instr.go` | `TestSTP` | ⬜ |
| `asm/arm64/instr.go` | `TestSTR` | ⬜ |
| `asm/arm64/instr.go` | `TestSTRB` | ⬜ |
| `asm/arm64/instr.go` | `TestSTRH` | ⬜ |
| `asm/arm64/instr.go` | `TestSTRR` | ⬜ |
| `asm/arm64/instr.go` | `TestSTRW` | ⬜ |
| `asm/arm64/instr.go` | `TestSUB` | ⬜ |
| `asm/arm64/instr.go` | `TestSUBI` | ⬜ |
| `asm/arm64/instr.go` | `TestSUBS` | ⬜ |
| `asm/arm64/instr.go` | `TestSUBSI` | ⬜ |
| `asm/arm64/instr.go` | `TestSVC` | ⬜ |
| `asm/arm64/instr.go` | `TestSXTB` | ⬜ |
| `asm/arm64/instr.go` | `TestSXTH` | ⬜ |
| `asm/arm64/instr.go` | `TestSXTW` | ⬜ |
| `asm/arm64/instr.go` | `TestTBNZ` | ⬜ |
| `asm/arm64/instr.go` | `TestTBZ` | ⬜ |
| `asm/arm64/instr.go` | `TestTST` | ⬜ |
| `asm/arm64/instr.go` | `TestTSTI` | ⬜ |
| `asm/arm64/instr.go` | `TestUCVTF` | ⬜ |
| `asm/arm64/instr.go` | `TestUDIV` | ⬜ |
| `asm/arm64/instr.go` | `TestUXTB` | ⬜ |
| `asm/arm64/instr.go` | `TestUXTH` | ⬜ |
| `asm/arm64/instr.go` | `TestUXTW` | ⬜ |
| `cli/cli.go` | `TestRoot` | ✅ |
| `cli/cli.go` | `TestWithFS` | ⬜ |
| `cli/fs.go` | `TestOS` | ✅ |
| `cli/repl.go` | `TestNewREPL` | ⬜ |
| `cli/repl.go` | `TestREPL_Run` | ✅ |
| `cli/run.go` | `TestNewRunCommand` | ✅ |
| `debug/debugger.go` | `TestDebugger_Break` | ⬜ |
| `debug/debugger.go` | `TestDebugger_BreakIf` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Breakpoints` | ✅ |
| `debug/debugger.go` | `TestDebugger_Clear` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Continue` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Enable` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Finish` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Hook` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Next` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Step` | ⬜ |
| `debug/debugger.go` | `TestDebugger_Stop` | ⬜ |
| `debug/debugger.go` | `TestNewDebugger` | ⬜ |
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
| `interp/cache.go` | `TestCache_Close` | ✅ |
| `interp/cache.go` | `TestNewCache` | ✅ |
| `interp/coroutine.go` | `TestCoroutine_Kind` | ✅ |
| `interp/coroutine.go` | `TestCoroutine_Refs` | ✅ |
| `interp/coroutine.go` | `TestCoroutine_String` | ✅ |
| `interp/coroutine.go` | `TestCoroutine_Type` | ✅ |
| `interp/error.go` | `TestErrorCode` | ✅ |
| `interp/error.go` | `TestRuntimeError_Error` | ✅ |
| `interp/error.go` | `TestRuntimeError_Unwrap` | ✅ |
| `interp/host.go` | `TestHostFunction_Kind` | ✅ |
| `interp/host.go` | `TestHostFunction_String` | ✅ |
| `interp/host.go` | `TestHostFunction_Type` | ✅ |
| `interp/host.go` | `TestHostObject_Field` | ✅ |
| `interp/host.go` | `TestHostObject_Kind` | ✅ |
| `interp/host.go` | `TestHostObject_Raw` | ✅ |
| `interp/host.go` | `TestHostObject_Refs` | ✅ |
| `interp/host.go` | `TestHostObject_SetField` | ✅ |
| `interp/host.go` | `TestHostObject_SetRaw` | ✅ |
| `interp/host.go` | `TestHostObject_String` | ✅ |
| `interp/host.go` | `TestHostObject_Type` | ✅ |
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
| `interp/interp.go` | `TestWithCache` | ✅ |
| `interp/interp.go` | `TestWithConverter` | ✅ |
| `interp/interp.go` | `TestWithFrame` | ✅ |
| `interp/interp.go` | `TestWithFuel` | ✅ |
| `interp/interp.go` | `TestWithHeap` | ✅ |
| `interp/interp.go` | `TestWithHook` | ✅ |
| `interp/interp.go` | `TestWithMarshaler` | ✅ |
| `interp/interp.go` | `TestWithMaxHeap` | ✅ |
| `interp/interp.go` | `TestWithProfiler` | ✅ |
| `interp/interp.go` | `TestWithStack` | ✅ |
| `interp/interp.go` | `TestWithThreshold` | ✅ |
| `interp/interp.go` | `TestWithTick` | ✅ |
| `interp/interp.go` | `TestWithTracer` | ✅ |
| `interp/pool.go` | `TestNewPool` | ✅ |
| `interp/pool.go` | `TestPool_Close` | ✅ |
| `interp/pool.go` | `TestPool_Get` | ✅ |
| `interp/pool.go` | `TestPool_Put` | ✅ |
| `interp/trace.go` | `TestNewTracer` | ✅ |
| `optimize/optimizer.go` | `TestNewOptimizer` | ⬜ |
| `optimize/optimizer.go` | `TestOptimizer_AddPass` | ✅ |
| `optimize/optimizer.go` | `TestOptimizer_Level` | ✅ |
| `optimize/optimizer.go` | `TestOptimizer_Optimize` | ✅ |
| `pass/manager.go` | `TestGetResult` | ✅ |
| `pass/manager.go` | `TestManager_Invalidate` | ✅ |
| `pass/manager.go` | `TestNewManager` | ⬜ |
| `pass/manager.go` | `TestRegister` | ✅ |
| `pass/pass.go` | `TestPreserveAll` | ⬜ |
| `pass/pass.go` | `TestPreserveNone` | ⬜ |
| `pass/pipeline.go` | `TestNewPipeline` | ⬜ |
| `pass/pipeline.go` | `TestPipeline_AddPass` | ⬜ |
| `pass/pipeline.go` | `TestPipeline_Run` | ✅ |
| `prof/collector.go` | `TestCollector_Add` | ⬜ |
| `prof/collector.go` | `TestCollector_AddMetric` | ⬜ |
| `prof/collector.go` | `TestCollector_IP` | ⬜ |
| `prof/collector.go` | `TestCollector_IPs` | ⬜ |
| `prof/collector.go` | `TestCollector_Metric` | ⬜ |
| `prof/collector.go` | `TestCollector_Metrics` | ✅ |
| `prof/collector.go` | `TestCollector_Opcode` | ⬜ |
| `prof/collector.go` | `TestCollector_Samples` | ⬜ |
| `prof/collector.go` | `TestCollector_Total` | ⬜ |
| `prof/collector.go` | `TestCollector_Value` | ⬜ |
| `prof/collector.go` | `TestNewCollector` | ⬜ |
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
| `transform/as.go` | `TestAlgebraicSimplificationPass_Run` | ✅ |
| `transform/as.go` | `TestNewAlgebraicSimplificationPass` | ⬜ |
| `transform/cd.go` | `TestConstantDeduplicationPass_Run` | ✅ |
| `transform/cd.go` | `TestNewConstantDeduplicationPass` | ⬜ |
| `transform/cf.go` | `TestConstantFoldingPass_Run` | ✅ |
| `transform/cf.go` | `TestNewConstantFoldingPass` | ⬜ |
| `transform/dce.go` | `TestDeadCodeEliminationPass_Run` | ✅ |
| `transform/dce.go` | `TestNewDeadCodeEliminationPass` | ⬜ |
| `transform/gvn.go` | `TestGlobalValueNumberingPass_Run` | ✅ |
| `transform/gvn.go` | `TestNewGlobalValueNumberingPass` | ⬜ |
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
| `types/function.go` | `TestFunctionBuilder_WithCaptures` | ✅ |
| `types/function.go` | `TestFunctionBuilder_WithLocals` | ✅ |
| `types/function.go` | `TestFunctionBuilder_WithParams` | ✅ |
| `types/function.go` | `TestFunctionBuilder_WithReturns` | ✅ |
| `types/function.go` | `TestFunctionType_Cast` | ✅ |
| `types/function.go` | `TestFunctionType_Equals` | ✅ |
| `types/function.go` | `TestFunctionType_Kind` | ✅ |
| `types/function.go` | `TestFunctionType_String` | ✅ |
| `types/function.go` | `TestFunction_Kind` | ✅ |
| `types/function.go` | `TestFunction_LocalKinds` | ✅ |
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
| `types/struct.go` | `TestStruct_Field` | ✅ |
| `types/struct.go` | `TestStruct_FieldByName` | ✅ |
| `types/struct.go` | `TestStruct_Kind` | ✅ |
| `types/struct.go` | `TestStruct_Raw` | ✅ |
| `types/struct.go` | `TestStruct_Refs` | ✅ |
| `types/struct.go` | `TestStruct_SetField` | ✅ |
| `types/struct.go` | `TestStruct_SetRaw` | ✅ |
| `types/struct.go` | `TestStruct_String` | ✅ |
| `types/struct.go` | `TestStruct_Type` | ✅ |
| `types/value.go` | `TestIsNull` | ✅ |
| `types/value.go` | `TestKinds` | ✅ |
| `types/value.go` | `TestZero` | ✅ |

## Opcode Ownership Matrix

`Metadata / parse` and `Runtime corpus` are enforced now. `Runtime error` records whether the current shared corpus contains an error-bearing row that includes the opcode; Phase 6 reviews whether an opcode-specific error case is applicable. `ARM64 parity` tracks test work, not backend capability; capability remains owned by `docs/instruction-set.md`.

| Opcode | Mnemonic | Metadata / parse | Verifier policy | Runtime success | Runtime error | ARM64 capability | ARM64 parity |
|---|---|---:|---|---:|---|---:|---|
| `NOP` | `nop` | ✅ | fixed zero | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `UNREACHABLE` | `unreachable` | ✅ | terminator | Review in Phase 6 | ✅ | ◐ | Phase 6 |
| `DROP` | `drop` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `DUP` | `dup` | ✅ | explicit | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `SWAP` | `swap` | ✅ | explicit | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `BR` | `br` | ✅ | fixed zero | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `BR_IF` | `br_if` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `BR_TABLE` | `br_table` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `SELECT` | `select` | ✅ | explicit | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `CALL` | `call` | ✅ | callee signature | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `RETURN` | `return` | ✅ | return arity | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `RETURN_CALL` | `return_call` | ✅ | callee signature | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `YIELD` | `yield` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Phase 6 |
| `RESUME` | `resume` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `CORO_DONE` | `coro.done` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `CORO_VALUE` | `coro.value` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `GLOBAL_GET` | `global.get` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `GLOBAL_SET` | `global.set` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `GLOBAL_TEE` | `global.tee` | ✅ | explicit | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `LOCAL_GET` | `local.get` | ✅ | declared local | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `LOCAL_SET` | `local.set` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `LOCAL_TEE` | `local.tee` | ✅ | explicit | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `CONST_GET` | `const.get` | ✅ | constant kind | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `UPVAL_GET` | `upval.get` | ✅ | declared capture | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `UPVAL_SET` | `upval.set` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `REF_NULL` | `ref.null` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `REF_NEW` | `ref.new` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `REF_GET` | `ref.get` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `REF_SET` | `ref.set` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `REF_TEST` | `ref.test` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `REF_CAST` | `ref.cast` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `REF_IS_NULL` | `ref.is_null` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `REF_EQ` | `ref.eq` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `REF_NE` | `ref.ne` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `I32_CONST` | `i32.const` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Phase 6 |
| `I32_ADD` | `i32.add` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_SUB` | `i32.sub` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_MUL` | `i32.mul` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_DIV_S` | `i32.div_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_DIV_U` | `i32.div_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_REM_S` | `i32.rem_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_REM_U` | `i32.rem_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_SHL` | `i32.shl` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_SHR_S` | `i32.shr_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_SHR_U` | `i32.shr_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_XOR` | `i32.xor` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_AND` | `i32.and` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_OR` | `i32.or` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_CLZ` | `i32.clz` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_CTZ` | `i32.ctz` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_POPCNT` | `i32.popcnt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_ROTL` | `i32.rotl` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_ROTR` | `i32.rotr` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_EXTEND8_S` | `i32.extend8_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_EXTEND16_S` | `i32.extend16_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_EQZ` | `i32.eqz` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_EQ` | `i32.eq` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_NE` | `i32.ne` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_LT_S` | `i32.lt_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_LT_U` | `i32.lt_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_GT_S` | `i32.gt_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_GT_U` | `i32.gt_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_LE_S` | `i32.le_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_LE_U` | `i32.le_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_GE_S` | `i32.ge_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_GE_U` | `i32.ge_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_TO_I64_S` | `i32.to_i64_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_TO_I64_U` | `i32.to_i64_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_TO_F32_U` | `i32.to_f32_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_TO_F32_S` | `i32.to_f32_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_TO_F64_U` | `i32.to_f64_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_TO_F64_S` | `i32.to_f64_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I32_REINTERPRET_F32` | `i32.reinterpret_f32` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_CONST` | `i64.const` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `I64_ADD` | `i64.add` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_SUB` | `i64.sub` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_MUL` | `i64.mul` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_DIV_S` | `i64.div_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_DIV_U` | `i64.div_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_REM_S` | `i64.rem_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_REM_U` | `i64.rem_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_SHL` | `i64.shl` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_SHR_S` | `i64.shr_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_SHR_U` | `i64.shr_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_XOR` | `i64.xor` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_AND` | `i64.and` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_OR` | `i64.or` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_CLZ` | `i64.clz` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_CTZ` | `i64.ctz` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_POPCNT` | `i64.popcnt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_ROTL` | `i64.rotl` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_ROTR` | `i64.rotr` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_EXTEND8_S` | `i64.extend8_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_EXTEND16_S` | `i64.extend16_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_EXTEND32_S` | `i64.extend32_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_EQZ` | `i64.eqz` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_EQ` | `i64.eq` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_NE` | `i64.ne` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_LT_S` | `i64.lt_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_LT_U` | `i64.lt_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_GT_S` | `i64.gt_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_GT_U` | `i64.gt_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_LE_S` | `i64.le_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_LE_U` | `i64.le_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_GE_S` | `i64.ge_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_GE_U` | `i64.ge_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_TO_I32` | `i64.to_i32` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_TO_F32_S` | `i64.to_f32_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_TO_F32_U` | `i64.to_f32_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_TO_F64_S` | `i64.to_f64_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_TO_F64_U` | `i64.to_f64_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `I64_REINTERPRET_F64` | `i64.reinterpret_f64` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_CONST` | `f32.const` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Phase 6 |
| `F32_ADD` | `f32.add` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_SUB` | `f32.sub` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_MUL` | `f32.mul` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_DIV` | `f32.div` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_REM` | `f32.rem` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Phase 6 |
| `F32_MOD` | `f32.mod` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Phase 6 |
| `F32_ABS` | `f32.abs` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_NEG` | `f32.neg` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_SQRT` | `f32.sqrt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_CEIL` | `f32.ceil` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_FLOOR` | `f32.floor` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_TRUNC` | `f32.trunc` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_NEAREST` | `f32.nearest` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_MIN` | `f32.min` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_MAX` | `f32.max` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_COPYSIGN` | `f32.copysign` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_EQ` | `f32.eq` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_NE` | `f32.ne` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_LT` | `f32.lt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_GT` | `f32.gt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_LE` | `f32.le` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_GE` | `f32.ge` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_TO_I32_S` | `f32.to_i32_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_TO_I32_U` | `f32.to_i32_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_TO_I64_S` | `f32.to_i64_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_TO_I64_U` | `f32.to_i64_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_TO_F64` | `f32.to_f64` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F32_REINTERPRET_I32` | `f32.reinterpret_i32` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_CONST` | `f64.const` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Phase 6 |
| `F64_ADD` | `f64.add` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_SUB` | `f64.sub` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_MUL` | `f64.mul` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_DIV` | `f64.div` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_REM` | `f64.rem` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Phase 6 |
| `F64_MOD` | `f64.mod` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Phase 6 |
| `F64_ABS` | `f64.abs` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_NEG` | `f64.neg` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_SQRT` | `f64.sqrt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_CEIL` | `f64.ceil` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_FLOOR` | `f64.floor` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_TRUNC` | `f64.trunc` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_NEAREST` | `f64.nearest` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_MIN` | `f64.min` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_MAX` | `f64.max` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_COPYSIGN` | `f64.copysign` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_EQ` | `f64.eq` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_NE` | `f64.ne` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_LT` | `f64.lt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_GT` | `f64.gt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_LE` | `f64.le` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_GE` | `f64.ge` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_TO_I32_S` | `f64.to_i32_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_TO_I32_U` | `f64.to_i32_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_TO_I64_S` | `f64.to_i64_s` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_TO_I64_U` | `f64.to_i64_u` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_TO_F32` | `f64.to_f32` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `F64_REINTERPRET_I64` | `f64.reinterpret_i64` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `STRING_NEW_UTF32` | `string.new_utf32` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_LEN` | `string.len` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `STRING_CONCAT` | `string.concat` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_EQ` | `string.eq` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_NE` | `string.ne` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_LT` | `string.lt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_GT` | `string.gt` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_LE` | `string.le` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_GE` | `string.ge` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRING_ENCODE_UTF32` | `string.encode_utf32` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `ARRAY_NEW` | `array.new` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Fallback review in Phase 6 |
| `ARRAY_NEW_DEFAULT` | `array.new_default` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Fallback review in Phase 6 |
| `ARRAY_LEN` | `array.len` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `ARRAY_GET` | `array.get` | ✅ | fixed metadata | ✅ | ✅ | ✅ | Phase 6 |
| `ARRAY_SET` | `array.set` | ✅ | fixed metadata | ✅ | ✅ | ◐ | Phase 6 |
| `ARRAY_FILL` | `array.fill` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Fallback review in Phase 6 |
| `ARRAY_COPY` | `array.copy` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Fallback review in Phase 6 |
| `ARRAY_APPEND` | `array.append` | ✅ | indeterminate arity | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `ARRAY_DELETE` | `array.delete` | ✅ | fixed metadata | ✅ | ✅ | ⬜ | Fallback review in Phase 6 |
| `ARRAY_SLICE` | `array.slice` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRUCT_NEW` | `struct.new` | ✅ | declared fields | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRUCT_NEW_DEFAULT` | `struct.new_default` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `STRUCT_GET` | `struct.get` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `STRUCT_SET` | `struct.set` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `MAP_NEW` | `map.new` | ✅ | indeterminate arity | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `MAP_NEW_DEFAULT` | `map.new_default` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `MAP_LEN` | `map.len` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `MAP_GET` | `map.get` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `MAP_LOOKUP` | `map.lookup` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `MAP_SET` | `map.set` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `MAP_DELETE` | `map.delete` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `MAP_CLEAR` | `map.clear` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `MAP_KEYS` | `map.keys` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `CLOSURE_NEW` | `closure.new` | ✅ | indeterminate arity | ✅ | Review in Phase 6 | ⬜ | Fallback review in Phase 6 |
| `MAP_ITER` | `map.iter` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `THROW` | `throw` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `ERROR_NEW` | `error.new` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `ERROR_GET` | `error.get` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ✅ | Phase 6 |
| `ERROR_CODE` | `error.code` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |
| `STRING_ITER` | `string.iter` | ✅ | fixed metadata | ✅ | Review in Phase 6 | ◐ | Phase 6 |

## Intentional White-Box Coverage

White-box tests remain only when exported behavior cannot directly protect the invariant: reference counts and free-list reuse, generated handler completeness, verifier dataflow policies, trace/cache state transitions, journal materialization, register allocation, relocation encoding, executable-buffer W^X and pointer stability, and architecture ABI encoding.

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
- `docs/plans/test-benchmark-rework.md` - phased migration and review log
