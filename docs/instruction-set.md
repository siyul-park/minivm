# Instruction Set

Opcode reference for minivm bytecode.

## Source of Truth

| Concern | File |
|---|---|
| opcode byte values | `instr/opcode.go` |
| mnemonic, operand widths, fixed stack effects, machine effects | `instr/type.go` |
| dynamic verification rules | `program/verify.go` |
| runtime semantics | `interp/threaded.go` |
| ARM64 encoding | `internal/asm/arm64/` |
| ARM64 native lowering | `internal/jit/arm64/` |
| platform support | `docs/compatibility.md` |

## Core Rules

The following rules `MUST` hold for every opcode:

- opcodes are one byte;
- operands are fixed-width or length-prefixed;
- operands are little-endian unless specified otherwise;
- stack operands pop right-to-left;
- boolean results use `i1`;
- `i1`/`i8`/`i32` share one computational representation;
- ref-exposing instructions retain refs; ref-overwriting instructions release replaced refs.

## Native Status

Threaded execution defines semantics. `internal/jit/arm64` lowers a subset when `WithThreshold` compiles a function. Unsupported operations become `ExitBridge`: only `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, and `ARRAY_NEW_DEFAULT` resume native execution; other bridges materialize and deoptimize. `RETURN_CALL`, `YIELD`, and `RESUME` deoptimize directly.

| Status | Meaning |
|---|---|
| ✅ | `internal/jit/arm64` lowers this opcode to native code |
| ◐ | `internal/jit/arm64` lowers this opcode only for a subset of its cases; the rest bridge or deoptimize |
| ⬜ | no ARM64 lowering; bridges or deoptimizes to threaded execution |
| 🔲 | backend unavailable (no encoder for that architecture) |

AMD64 has no encoder (`internal/asm/`), so every opcode is 🔲 there regardless of ARM64 status.

ARM64 branches `MUST` be range-checked before encoding. A conditional branch outside signed imm19 range `MUST` be relaxed to an inverted conditional skip plus an imm26 branch when reachable.

## Operand Widths

Declared in `instr/type.go`. `{}` has no operands; `{n}` has one fixed-width operand; `{-n, n}` is a count byte followed by `count × n`-byte operands. Branch offsets are signed 16-bit offsets from the end of the branch instruction:

```text
target = instruction_start + instruction_width + operand
```

## Operand Kinds

`i1`, `i8`, and `i32` share one computational class. An opcode accepting `i32` `MAY` accept `i1`/`i8` when the verifier proves compatibility.

Comparisons, `eqz`, and ref tests `MUST` produce `i1`. `i32.and/or/xor` `MUST` preserve the narrow kind; other integer arithmetic `MUST` widen to `i32`.

## Machine Effects

`instr.Type` states what an opcode touches outside the operand stack, as two `Effect` sets that every opcode declares: `Reads` and `Writes`.

| Effect | Meaning |
|---|---|
| `Local` | the running frame's local slots |
| `Global` | the module's globals |
| `Upval` | the running closure's captured upvalues |
| `Heap` | the contents of a heap container |
| `Frame` | the call frame chain: the opcode enters a function named on the operand stack |
| `Branch` | the instruction pointer, moved other than by falling through |

Query them with `op.Reads(effect)` and `op.Writes(effect)`. Rules:

- allocating a container writes `Heap`; overwriting the contents of one both reads and writes it, which is where a replaced reference is released;
- only `call` and `return_call` write `Frame`; resuming a coroutine pushes no frame;
- `op.IsPure()` is derived, not declared: an opcode that reads nothing, writes nothing, and pushes at least one value computes from its operands alone, so a consumer `MAY` number, fold, or reorder it.

New opcodes `MUST` declare effects beside stack effects in `instr/type.go`; consumers `MUST` query `instr` rather than re-derive effects.

## Opcode Reference

One opcode per row, in opcode-value order.

| Family | Opcode | Mnemonic | ARM64 | AMD64 | Notes |
|---|---|---|---:|---:|---|
| Stack | `NOP` | `nop` | ✅ | 🔲 | lowered on ARM64 |
| Stack | `UNREACHABLE` | `unreachable` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Stack | `DROP` | `drop` | ✅ | 🔲 | lowered on ARM64 |
| Stack | `DUP` | `dup` | ✅ | 🔲 | lowered on ARM64 |
| Stack | `SWAP` | `swap` | ✅ | 🔲 | lowered on ARM64 |
| Control | `BR` | `br` | ✅ | 🔲 | lowered on ARM64 |
| Control | `BR_IF` | `br_if` | ✅ | 🔲 | lowered on ARM64 |
| Control | `BR_TABLE` | `br_table` | ✅ | 🔲 | lowered on ARM64 |
| Stack | `SELECT` | `select` | ✅ | 🔲 | lowered on ARM64 |
| Control | `CALL` | `call` | ◐ | 🔲 | ARM64 lowers a call to a constant target, or to the one function recorded at a dynamic site behind `guard.value`, when the return is not i64; other calls bridge |
| Control | `RETURN` | `return` | ✅ | 🔲 | lowered on ARM64 |
| Control | `RETURN_CALL` | `return_call` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Coroutines | `YIELD` | `yield` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Coroutines | `RESUME` | `resume` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Coroutines | `CORO_DONE` | `coro.done` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Coroutines | `CORO_VALUE` | `coro.value` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Variables | `GLOBAL_GET` | `global.get` | ✅ | 🔲 | lowered on ARM64 |
| Variables | `GLOBAL_SET` | `global.set` | ✅ | 🔲 | lowered on ARM64 |
| Variables | `GLOBAL_TEE` | `global.tee` | ✅ | 🔲 | lowered on ARM64 |
| Variables | `LOCAL_GET` | `local.get` | ✅ | 🔲 | lowered on ARM64 |
| Variables | `LOCAL_SET` | `local.set` | ✅ | 🔲 | lowered on ARM64 |
| Variables | `LOCAL_TEE` | `local.tee` | ✅ | 🔲 | lowered on ARM64 |
| Variables | `CONST_GET` | `const.get` | ✅ | 🔲 | lowered on ARM64 |
| Variables | `UPVAL_GET` | `upval.get` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Variables | `UPVAL_SET` | `upval.set` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| References | `REF_NULL` | `ref.null` | ✅ | 🔲 | lowered on ARM64 |
| References | `REF_NEW` | `ref.new` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| References | `REF_GET` | `ref.get` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| References | `REF_SET` | `ref.set` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| References | `REF_TEST` | `ref.test` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| References | `REF_CAST` | `ref.cast` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| References | `REF_IS_NULL` | `ref.is_null` | ✅ | 🔲 | |
| References | `REF_EQ` | `ref.eq` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| References | `REF_NE` | `ref.ne` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I32_CONST` | `i32.const` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_ADD` | `i32.add` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_SUB` | `i32.sub` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_MUL` | `i32.mul` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_DIV_S` | `i32.div_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_DIV_U` | `i32.div_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_REM_S` | `i32.rem_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_REM_U` | `i32.rem_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_SHL` | `i32.shl` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_SHR_S` | `i32.shr_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_SHR_U` | `i32.shr_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_XOR` | `i32.xor` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_AND` | `i32.and` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_OR` | `i32.or` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_CLZ` | `i32.clz` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I32_CTZ` | `i32.ctz` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I32_POPCNT` | `i32.popcnt` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I32_ROTL` | `i32.rotl` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I32_ROTR` | `i32.rotr` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I32_EXTEND8_S` | `i32.extend8_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_EXTEND16_S` | `i32.extend16_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_EQZ` | `i32.eqz` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_EQ` | `i32.eq` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_NE` | `i32.ne` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_LT_S` | `i32.lt_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_LT_U` | `i32.lt_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_GT_S` | `i32.gt_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_GT_U` | `i32.gt_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_LE_S` | `i32.le_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_LE_U` | `i32.le_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_GE_S` | `i32.ge_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_GE_U` | `i32.ge_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_TO_I64_S` | `i32.to_i64_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_TO_I64_U` | `i32.to_i64_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_TO_F32_U` | `i32.to_f32_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_TO_F32_S` | `i32.to_f32_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_TO_F64_U` | `i32.to_f64_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_TO_F64_S` | `i32.to_f64_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I32_REINTERPRET_F32` | `i32.reinterpret_f32` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_CONST` | `i64.const` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_ADD` | `i64.add` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_SUB` | `i64.sub` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_MUL` | `i64.mul` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_DIV_S` | `i64.div_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_DIV_U` | `i64.div_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_REM_S` | `i64.rem_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_REM_U` | `i64.rem_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_SHL` | `i64.shl` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_SHR_S` | `i64.shr_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_SHR_U` | `i64.shr_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_XOR` | `i64.xor` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_AND` | `i64.and` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_OR` | `i64.or` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_CLZ` | `i64.clz` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I64_CTZ` | `i64.ctz` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I64_POPCNT` | `i64.popcnt` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I64_ROTL` | `i64.rotl` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I64_ROTR` | `i64.rotr` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Integers | `I64_EXTEND8_S` | `i64.extend8_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_EXTEND16_S` | `i64.extend16_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_EXTEND32_S` | `i64.extend32_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_EQZ` | `i64.eqz` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_EQ` | `i64.eq` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_NE` | `i64.ne` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_LT_S` | `i64.lt_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_LT_U` | `i64.lt_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_GT_S` | `i64.gt_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_GT_U` | `i64.gt_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_LE_S` | `i64.le_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_LE_U` | `i64.le_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_GE_S` | `i64.ge_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_GE_U` | `i64.ge_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_TO_I32` | `i64.to_i32` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_TO_F32_S` | `i64.to_f32_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_TO_F32_U` | `i64.to_f32_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_TO_F64_S` | `i64.to_f64_s` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_TO_F64_U` | `i64.to_f64_u` | ✅ | 🔲 | lowered on ARM64 |
| Integers | `I64_REINTERPRET_F64` | `i64.reinterpret_f64` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_CONST` | `f32.const` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_ADD` | `f32.add` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_SUB` | `f32.sub` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_MUL` | `f32.mul` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_DIV` | `f32.div` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_REM` | `f32.rem` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Floating point | `F32_MOD` | `f32.mod` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Floating point | `F32_ABS` | `f32.abs` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_NEG` | `f32.neg` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_SQRT` | `f32.sqrt` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_CEIL` | `f32.ceil` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_FLOOR` | `f32.floor` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_TRUNC` | `f32.trunc` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_NEAREST` | `f32.nearest` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_MIN` | `f32.min` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_MAX` | `f32.max` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_COPYSIGN` | `f32.copysign` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Floating point | `F32_EQ` | `f32.eq` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_NE` | `f32.ne` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_LT` | `f32.lt` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_GT` | `f32.gt` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_LE` | `f32.le` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_GE` | `f32.ge` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_TO_I32_S` | `f32.to_i32_s` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_TO_I32_U` | `f32.to_i32_u` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_TO_I64_S` | `f32.to_i64_s` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_TO_I64_U` | `f32.to_i64_u` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_TO_F64` | `f32.to_f64` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F32_REINTERPRET_I32` | `f32.reinterpret_i32` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_CONST` | `f64.const` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_ADD` | `f64.add` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_SUB` | `f64.sub` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_MUL` | `f64.mul` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_DIV` | `f64.div` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_REM` | `f64.rem` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Floating point | `F64_MOD` | `f64.mod` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Floating point | `F64_ABS` | `f64.abs` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_NEG` | `f64.neg` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_SQRT` | `f64.sqrt` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_CEIL` | `f64.ceil` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_FLOOR` | `f64.floor` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_TRUNC` | `f64.trunc` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_NEAREST` | `f64.nearest` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_MIN` | `f64.min` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_MAX` | `f64.max` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_COPYSIGN` | `f64.copysign` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Floating point | `F64_EQ` | `f64.eq` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_NE` | `f64.ne` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_LT` | `f64.lt` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_GT` | `f64.gt` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_LE` | `f64.le` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_GE` | `f64.ge` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_TO_I32_S` | `f64.to_i32_s` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_TO_I32_U` | `f64.to_i32_u` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_TO_I64_S` | `f64.to_i64_s` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_TO_I64_U` | `f64.to_i64_u` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_TO_F32` | `f64.to_f32` | ✅ | 🔲 | lowered on ARM64 |
| Floating point | `F64_REINTERPRET_I64` | `f64.reinterpret_i64` | ✅ | 🔲 | lowered on ARM64 |
| Strings | `STRING_NEW_UTF32` | `string.new_utf32` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_LEN` | `string.len` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_CONCAT` | `string.concat` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_EQ` | `string.eq` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_NE` | `string.ne` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_LT` | `string.lt` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_GT` | `string.gt` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_LE` | `string.le` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_GE` | `string.ge` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_ENCODE_UTF32` | `string.encode_utf32` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Arrays | `ARRAY_NEW` | `array.new` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Arrays | `ARRAY_NEW_DEFAULT` | `array.new_default` | ⬜ | 🔲 | bridges to threaded on ARM64; resumes native code (interp.bridgeable) |
| Arrays | `ARRAY_LEN` | `array.len` | ✅ | 🔲 | guarded, else bridges |
| Arrays | `ARRAY_GET` | `array.get` | ✅ | 🔲 | guarded, else bridges |
| Arrays | `ARRAY_SET` | `array.set` | ✅ | 🔲 | guarded, else bridges |
| Arrays | `ARRAY_FILL` | `array.fill` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Arrays | `ARRAY_COPY` | `array.copy` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Arrays | `ARRAY_APPEND` | `array.append` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Arrays | `ARRAY_DELETE` | `array.delete` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Arrays | `ARRAY_SLICE` | `array.slice` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Structs | `STRUCT_NEW` | `struct.new` | ⬜ | 🔲 | bridges to threaded on ARM64; resumes native code (interp.bridgeable) |
| Structs | `STRUCT_NEW_DEFAULT` | `struct.new_default` | ⬜ | 🔲 | bridges to threaded on ARM64; resumes native code (interp.bridgeable) |
| Structs | `STRUCT_GET` | `struct.get` | ✅ | 🔲 | guarded, else bridges |
| Structs | `STRUCT_SET` | `struct.set` | ✅ | 🔲 | guarded, else bridges |
| Maps | `MAP_NEW` | `map.new` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_NEW_DEFAULT` | `map.new_default` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_LEN` | `map.len` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_GET` | `map.get` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_LOOKUP` | `map.lookup` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_SET` | `map.set` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_DELETE` | `map.delete` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_CLEAR` | `map.clear` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_KEYS` | `map.keys` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Closures | `CLOSURE_NEW` | `closure.new` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Maps | `MAP_ITER` | `map.iter` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Structured errors | `THROW` | `throw` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Structured errors | `ERROR_NEW` | `error.new` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Structured errors | `ERROR_GET` | `error.get` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Structured errors | `ERROR_CODE` | `error.code` | ⬜ | 🔲 | bridges to threaded on ARM64 |
| Strings | `STRING_ITER` | `string.iter` | ⬜ | 🔲 | bridges to threaded on ARM64 |

## Family Rules

### Control

Branch offsets are relative to instruction end. Function bodies `MUST` terminate with `RETURN`, `RETURN_CALL`, or `UNREACHABLE`; top-level code `MAY` fall through.

`RETURN_CALL` transfers ownership to the new activation and releases the retiring activation exactly once.

### References

`any` is the dynamic VM value type. `REF_TEST`/`REF_CAST` recover dynamic types. `REF_SET` mutates scalar cells from `REF_NEW`; other targets trap. Coroutine tail calls preserve the coroutine; completion exposes the last declared return.

### Arrays

`ARRAY_APPEND` moves values into the array. `ARRAY_DELETE` moves the removed element to the stack. `ARRAY_GET`/`ARRAY_SLICE` `MUST` retain copied refs. `ARRAY_SLICE` consumes the source ref; callers `MUST` use `DUP` to preserve it.

### Strings

All string comparisons are by content and require string operands. Mismatches `MUST` trap `ErrTypeMismatch`; `REF_EQ`/`REF_NE` test identity.

`string.concat` allocates a fresh ref and `MAY` reuse append-only storage without mutating published strings.

### Maps

Primitive keys use value identity; `i1`/`i8` use their `i32` representation. Strings use content. Other refs use heap identity.

`(*Interpreter).mapKey` owns the rule for map operations and `Marshal`. Missing keys return element zero values. `MAP_LOOKUP` also returns an `i1` presence flag.

### Structured Errors

Handler tables own exception routing. `types.Error` is the structured payload. Code `0` is unclassified; VM traps use negative codes; source errors use `types.ErrorCodeUserBase` and above.

## Fusion

Threaded fusion rules are owned by `fusion.md`.

## Maintenance

- Opcode numbers are append-only in `instr/opcode.go`.
- Widths, stack effects, and machine effects live in `instr/type.go`.
- Threaded handlers are generated; native status stays in the table above.
- Change procedure belongs to `guides/add-opcode.md`.

## Related

- `docs/guides/add-opcode.md` — checklist for adding or changing an opcode
- `docs/verification.md` — static validation and stack rules
- `docs/value-representation.md` — kinds, boxed layout, and boolean representation
- `docs/jit-internals.md` — native tier status, lowering, and runtime contracts
- `docs/compatibility.md` — platform and backend availability
