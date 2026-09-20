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
| Planned native execution | JIT rebuild |
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

Native status is per opcode. The current implementation is threaded; native lowering is planned.

| Status | Meaning |
|---|---|
| ✅ | native lowering exists |
| ◐ | partial native lowering |
| ⬜ | threaded-only |
| 🔲 | backend unavailable |

No native backend is present in the current tree.

ARM64 branches `MUST` be range-checked before encoding. A conditional branch outside signed imm19 range `MUST` be relaxed to an inverted conditional skip plus an imm26 branch when reachable. Otherwise the current implementation MUST remain on threaded execution.

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

A new opcode `MUST` declare its effects in the same table entry as its stack effect. Consumers `MUST` ask `instr`; they `MUST NOT` re-derive an effect from an opcode list.

## Opcode Reference

One opcode per row, in opcode-value order.

| Family | Opcode | Mnemonic | ARM64 planned | AMD64 planned | Notes |
|---|---|---|---:|---:|---|
| Stack | `NOP` | `nop` | ⬜ | 🔲 | current threaded execution |
| Stack | `UNREACHABLE` | `unreachable` | ⬜ | 🔲 | current threaded execution |
| Stack | `DROP` | `drop` | ⬜ | 🔲 | current threaded execution |
| Stack | `DUP` | `dup` | ⬜ | 🔲 | current threaded execution |
| Stack | `SWAP` | `swap` | ⬜ | 🔲 | current threaded execution |
| Control | `BR` | `br` | ⬜ | 🔲 | current threaded execution |
| Control | `BR_IF` | `br_if` | ⬜ | 🔲 | current threaded execution |
| Control | `BR_TABLE` | `br_table` | ⬜ | 🔲 | current threaded execution |
| Stack | `SELECT` | `select` | ⬜ | 🔲 | current threaded execution |
| Control | `CALL` | `call` | ⬜ | 🔲 | current threaded execution |
| Control | `RETURN` | `return` | ⬜ | 🔲 | current threaded execution |
| Control | `RETURN_CALL` | `return_call` | ⬜ | 🔲 | current threaded execution |
| Coroutines | `YIELD` | `yield` | ⬜ | 🔲 | current threaded execution |
| Coroutines | `RESUME` | `resume` | ⬜ | 🔲 | current threaded execution |
| Coroutines | `CORO_DONE` | `coro.done` | ⬜ | 🔲 | current threaded execution |
| Coroutines | `CORO_VALUE` | `coro.value` | ⬜ | 🔲 | current threaded execution |
| Variables | `GLOBAL_GET` | `global.get` | ⬜ | 🔲 | current threaded execution |
| Variables | `GLOBAL_SET` | `global.set` | ⬜ | 🔲 | current threaded execution |
| Variables | `GLOBAL_TEE` | `global.tee` | ⬜ | 🔲 | current threaded execution |
| Variables | `LOCAL_GET` | `local.get` | ⬜ | 🔲 | current threaded execution |
| Variables | `LOCAL_SET` | `local.set` | ⬜ | 🔲 | current threaded execution |
| Variables | `LOCAL_TEE` | `local.tee` | ⬜ | 🔲 | current threaded execution |
| Variables | `CONST_GET` | `const.get` | ⬜ | 🔲 | current threaded execution |
| Variables | `UPVAL_GET` | `upval.get` | ⬜ | 🔲 | current threaded execution |
| Variables | `UPVAL_SET` | `upval.set` | ⬜ | 🔲 | current threaded execution |
| References | `REF_NULL` | `ref.null` | ⬜ | 🔲 | current threaded execution |
| References | `REF_NEW` | `ref.new` | ⬜ | 🔲 | current threaded execution |
| References | `REF_GET` | `ref.get` | ⬜ | 🔲 | current threaded execution |
| References | `REF_SET` | `ref.set` | ⬜ | 🔲 | current threaded execution |
| References | `REF_TEST` | `ref.test` | ⬜ | 🔲 | current threaded execution |
| References | `REF_CAST` | `ref.cast` | ⬜ | 🔲 | current threaded execution |
| References | `REF_IS_NULL` | `ref.is_null` | ⬜ | 🔲 | current threaded execution |
| References | `REF_EQ` | `ref.eq` | ⬜ | 🔲 | current threaded execution |
| References | `REF_NE` | `ref.ne` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_CONST` | `i32.const` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_ADD` | `i32.add` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_SUB` | `i32.sub` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_MUL` | `i32.mul` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_DIV_S` | `i32.div_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_DIV_U` | `i32.div_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_REM_S` | `i32.rem_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_REM_U` | `i32.rem_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_SHL` | `i32.shl` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_SHR_S` | `i32.shr_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_SHR_U` | `i32.shr_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_XOR` | `i32.xor` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_AND` | `i32.and` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_OR` | `i32.or` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_CLZ` | `i32.clz` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_CTZ` | `i32.ctz` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_POPCNT` | `i32.popcnt` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_ROTL` | `i32.rotl` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_ROTR` | `i32.rotr` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_EXTEND8_S` | `i32.extend8_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_EXTEND16_S` | `i32.extend16_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_EQZ` | `i32.eqz` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_EQ` | `i32.eq` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_NE` | `i32.ne` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_LT_S` | `i32.lt_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_LT_U` | `i32.lt_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_GT_S` | `i32.gt_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_GT_U` | `i32.gt_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_LE_S` | `i32.le_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_LE_U` | `i32.le_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_GE_S` | `i32.ge_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_GE_U` | `i32.ge_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_TO_I64_S` | `i32.to_i64_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_TO_I64_U` | `i32.to_i64_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_TO_F32_U` | `i32.to_f32_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_TO_F32_S` | `i32.to_f32_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_TO_F64_U` | `i32.to_f64_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_TO_F64_S` | `i32.to_f64_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I32_REINTERPRET_F32` | `i32.reinterpret_f32` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_CONST` | `i64.const` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_ADD` | `i64.add` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_SUB` | `i64.sub` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_MUL` | `i64.mul` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_DIV_S` | `i64.div_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_DIV_U` | `i64.div_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_REM_S` | `i64.rem_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_REM_U` | `i64.rem_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_SHL` | `i64.shl` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_SHR_S` | `i64.shr_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_SHR_U` | `i64.shr_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_XOR` | `i64.xor` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_AND` | `i64.and` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_OR` | `i64.or` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_CLZ` | `i64.clz` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_CTZ` | `i64.ctz` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_POPCNT` | `i64.popcnt` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_ROTL` | `i64.rotl` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_ROTR` | `i64.rotr` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_EXTEND8_S` | `i64.extend8_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_EXTEND16_S` | `i64.extend16_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_EXTEND32_S` | `i64.extend32_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_EQZ` | `i64.eqz` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_EQ` | `i64.eq` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_NE` | `i64.ne` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_LT_S` | `i64.lt_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_LT_U` | `i64.lt_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_GT_S` | `i64.gt_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_GT_U` | `i64.gt_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_LE_S` | `i64.le_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_LE_U` | `i64.le_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_GE_S` | `i64.ge_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_GE_U` | `i64.ge_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_TO_I32` | `i64.to_i32` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_TO_F32_S` | `i64.to_f32_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_TO_F32_U` | `i64.to_f32_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_TO_F64_S` | `i64.to_f64_s` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_TO_F64_U` | `i64.to_f64_u` | ⬜ | 🔲 | current threaded execution |
| Integers | `I64_REINTERPRET_F64` | `i64.reinterpret_f64` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_CONST` | `f32.const` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_ADD` | `f32.add` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_SUB` | `f32.sub` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_MUL` | `f32.mul` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_DIV` | `f32.div` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_REM` | `f32.rem` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_MOD` | `f32.mod` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_ABS` | `f32.abs` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_NEG` | `f32.neg` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_SQRT` | `f32.sqrt` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_CEIL` | `f32.ceil` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_FLOOR` | `f32.floor` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_TRUNC` | `f32.trunc` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_NEAREST` | `f32.nearest` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_MIN` | `f32.min` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_MAX` | `f32.max` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_COPYSIGN` | `f32.copysign` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_EQ` | `f32.eq` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_NE` | `f32.ne` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_LT` | `f32.lt` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_GT` | `f32.gt` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_LE` | `f32.le` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_GE` | `f32.ge` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_TO_I32_S` | `f32.to_i32_s` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_TO_I32_U` | `f32.to_i32_u` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_TO_I64_S` | `f32.to_i64_s` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_TO_I64_U` | `f32.to_i64_u` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_TO_F64` | `f32.to_f64` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F32_REINTERPRET_I32` | `f32.reinterpret_i32` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_CONST` | `f64.const` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_ADD` | `f64.add` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_SUB` | `f64.sub` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_MUL` | `f64.mul` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_DIV` | `f64.div` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_REM` | `f64.rem` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_MOD` | `f64.mod` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_ABS` | `f64.abs` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_NEG` | `f64.neg` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_SQRT` | `f64.sqrt` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_CEIL` | `f64.ceil` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_FLOOR` | `f64.floor` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_TRUNC` | `f64.trunc` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_NEAREST` | `f64.nearest` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_MIN` | `f64.min` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_MAX` | `f64.max` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_COPYSIGN` | `f64.copysign` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_EQ` | `f64.eq` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_NE` | `f64.ne` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_LT` | `f64.lt` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_GT` | `f64.gt` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_LE` | `f64.le` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_GE` | `f64.ge` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_TO_I32_S` | `f64.to_i32_s` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_TO_I32_U` | `f64.to_i32_u` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_TO_I64_S` | `f64.to_i64_s` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_TO_I64_U` | `f64.to_i64_u` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_TO_F32` | `f64.to_f32` | ⬜ | 🔲 | current threaded execution |
| Floating point | `F64_REINTERPRET_I64` | `f64.reinterpret_i64` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_NEW_UTF32` | `string.new_utf32` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_LEN` | `string.len` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_CONCAT` | `string.concat` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_EQ` | `string.eq` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_NE` | `string.ne` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_LT` | `string.lt` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_GT` | `string.gt` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_LE` | `string.le` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_GE` | `string.ge` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_ENCODE_UTF32` | `string.encode_utf32` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_NEW` | `array.new` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_NEW_DEFAULT` | `array.new_default` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_LEN` | `array.len` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_GET` | `array.get` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_SET` | `array.set` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_FILL` | `array.fill` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_COPY` | `array.copy` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_APPEND` | `array.append` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_DELETE` | `array.delete` | ⬜ | 🔲 | current threaded execution |
| Arrays | `ARRAY_SLICE` | `array.slice` | ⬜ | 🔲 | current threaded execution |
| Structs | `STRUCT_NEW` | `struct.new` | ⬜ | 🔲 | current threaded execution |
| Structs | `STRUCT_NEW_DEFAULT` | `struct.new_default` | ⬜ | 🔲 | current threaded execution |
| Structs | `STRUCT_GET` | `struct.get` | ⬜ | 🔲 | current threaded execution |
| Structs | `STRUCT_SET` | `struct.set` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_NEW` | `map.new` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_NEW_DEFAULT` | `map.new_default` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_LEN` | `map.len` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_GET` | `map.get` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_LOOKUP` | `map.lookup` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_SET` | `map.set` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_DELETE` | `map.delete` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_CLEAR` | `map.clear` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_KEYS` | `map.keys` | ⬜ | 🔲 | current threaded execution |
| Closures | `CLOSURE_NEW` | `closure.new` | ⬜ | 🔲 | current threaded execution |
| Maps | `MAP_ITER` | `map.iter` | ⬜ | 🔲 | current threaded execution |
| Structured errors | `THROW` | `throw` | ⬜ | 🔲 | current threaded execution |
| Structured errors | `ERROR_NEW` | `error.new` | ⬜ | 🔲 | current threaded execution |
| Structured errors | `ERROR_GET` | `error.get` | ⬜ | 🔲 | current threaded execution |
| Structured errors | `ERROR_CODE` | `error.code` | ⬜ | 🔲 | current threaded execution |
| Strings | `STRING_ITER` | `string.iter` | ⬜ | 🔲 | current threaded execution |

## Family Rules

### Control

Branch offsets are relative to instruction end. Function bodies `MUST` terminate with `RETURN`, `RETURN_CALL`, or `UNREACHABLE`; top-level code `MAY` fall through.

`RETURN_CALL` transfers ownership to the new activation and releases the retiring activation exactly once.

### References

`any` is the dynamic VM value type. `REF_TEST`/`REF_CAST` recover dynamic types. `REF_SET` mutates scalar cells from `REF_NEW`; other targets trap. Coroutine tail calls preserve the coroutine; completion exposes the last declared return.

### Arrays

`ARRAY_APPEND` moves values into the array. `ARRAY_DELETE` moves the removed element to the stack. `ARRAY_GET`/`ARRAY_SLICE` `MUST` retain copied refs. `ARRAY_SLICE` consumes the source ref; the agent `MUST` use `DUP` to preserve it.

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

## Maintenance Notes

When changing the instruction set, the agent `MUST`:

- append opcodes only;
- keep names short and standard;
- prefer composition over narrow variants;
- update metadata, verifier, threaded lowering, backend status, tests, and owner docs together;
- keep stack effects and ownership explicit;
- preserve threaded semantic parity; native parity becomes applicable when the rebuild exists.

## Related

- `docs/guides/add-opcode.md` — checklist for adding or changing an opcode
- `docs/verification.md` — static validation and stack rules
- `docs/value-representation.md` — kinds, boxed layout, and boolean representation
- `docs/jit-internals.md` — planned native rebuild contracts
- `docs/compatibility.md` — platform and backend availability
