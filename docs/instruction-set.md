# Instruction Set

Opcode reference for minivm bytecode.

## Source of Truth

| Concern | File |
|---|---|
| opcode byte values | `instr/opcode.go` |
| mnemonic, operand widths, fixed stack effects, machine effects | `instr/type.go` |
| dynamic verification rules | `program/verify.go` |
| runtime semantics | `interp/threaded.go` |
| ARM64 lowering | `internal/jit/arm64/` |
| non-ARM64 JIT stub | `interp/jit_stub.go` |
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

## JIT Status

JIT status is per opcode and per backend. It describes whether a recorded trace can be lowered by that backend; every opcode remains available through the threaded interpreter.

| Status | Meaning |
|---|---|
| ✅ | native lowering exists for supported trace shapes |
| ◐ | partial support: guarded, shape-limited, terminal fallback, or framed fallback |
| ⬜ | threaded-only on that backend |
| 🔲 | backend unavailable |

AMD64 currently has no active JIT backend. `internal/asm/amd64` is a placeholder and `interp/jit_stub.go` disables compiler construction on non-ARM64 platforms, so every AMD64 entry is `🔲`.

ARM64 branches `MUST` be range-checked before encoding. A conditional branch outside signed imm19 range `MUST` be relaxed to an inverted conditional skip plus an imm26 branch when reachable. Otherwise JIT compilation `MUST` fall back to threaded execution.

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

| Family | Opcode | Mnemonic | ARM64 JIT | AMD64 JIT | Notes |
|---|---|---|---:|---:|---|
| Stack | `NOP` | `nop` | ✅ | 🔲 | — |
| Stack | `UNREACHABLE` | `unreachable` | ◐ | 🔲 | terminal trap exit |
| Stack | `DROP` | `drop` | ✅ | 🔲 | — |
| Stack | `DUP` | `dup` | ✅ | 🔲 | — |
| Stack | `SWAP` | `swap` | ✅ | 🔲 | — |
| Control | `BR` | `br` | ✅ | 🔲 | linear branch or loop back-edge |
| Control | `BR_IF` | `br_if` | ◐ | 🔲 | recorded branch guard or loop back-edge |
| Control | `BR_TABLE` | `br_table` | ◐ | 🔲 | recorded table branch with fallback |
| Stack | `SELECT` | `select` | ✅ | 🔲 | — |
| Control | `CALL` | `call` | ◐ | 🔲 | bytecode/closure calls lower, self-recursion included; host or unsupported callees fall back |
| Control | `RETURN` | `return` | ✅ | 🔲 | trace return or stitched continuation |
| Control | `RETURN_CALL` | `return_call` | ◐ | 🔲 | tail loop or tail morph when target shape is supported |
| Coroutines | `YIELD` | `yield` | ◐ | 🔲 | terminal fallback to coroutine suspension |
| Coroutines | `RESUME` | `resume` | ◐ | 🔲 | terminal fallback to coroutine resume |
| Coroutines | `CORO_DONE` | `coro.done` | ✅ | 🔲 | native coroutine field read |
| Coroutines | `CORO_VALUE` | `coro.value` | ✅ | 🔲 | native coroutine field read |
| Variables | `GLOBAL_GET` | `global.get` | ✅ | 🔲 | index must be within declared `.globals`; out-of-range traps (segmentation fault) |
| Variables | `GLOBAL_SET` | `global.set` | ✅ | 🔲 | index must be within declared `.globals`; out-of-range traps (segmentation fault) |
| Variables | `GLOBAL_TEE` | `global.tee` | ✅ | 🔲 | index must be within declared `.globals`; out-of-range traps (segmentation fault) |
| Variables | `LOCAL_GET` | `local.get` | ✅ | 🔲 | — |
| Variables | `LOCAL_SET` | `local.set` | ✅ | 🔲 | — |
| Variables | `LOCAL_TEE` | `local.tee` | ✅ | 🔲 | — |
| Variables | `CONST_GET` | `const.get` | ◐ | 🔲 | scalar constants and function refs feeding calls |
| Variables | `UPVAL_GET` | `upval.get` | ✅ | 🔲 | — |
| Variables | `UPVAL_SET` | `upval.set` | ✅ | 🔲 | — |
| References | `REF_NULL` | `ref.null` | ✅ | 🔲 | — |
| References | `REF_NEW` | `ref.new` | ⬜ | 🔲 | allocation stays interpreter-owned |
| References | `REF_GET` | `ref.get` | ✅ | 🔲 | native ref-cell read |
| References | `REF_SET` | `ref.set` | ⬜ | 🔲 | mutation stays interpreter-owned |
| References | `REF_TEST` | `ref.test` | ⬜ | 🔲 | runtime type test stays threaded |
| References | `REF_CAST` | `ref.cast` | ⬜ | 🔲 | runtime cast stays threaded |
| References | `REF_IS_NULL` | `ref.is_null` | ✅ | 🔲 | — |
| References | `REF_EQ` | `ref.eq` | ◐ | 🔲 | native boxed compare; two owned operands fall back |
| References | `REF_NE` | `ref.ne` | ◐ | 🔲 | native boxed compare; two owned operands fall back |
| Integers | `I32_CONST` | `i32.const` | ✅ | 🔲 | — |
| Integers | `I32_ADD` | `i32.add` | ✅ | 🔲 | — |
| Integers | `I32_SUB` | `i32.sub` | ✅ | 🔲 | — |
| Integers | `I32_MUL` | `i32.mul` | ✅ | 🔲 | — |
| Integers | `I32_DIV_S` | `i32.div_s` | ✅ | 🔲 | division guard for traps |
| Integers | `I32_DIV_U` | `i32.div_u` | ✅ | 🔲 | division guard for traps |
| Integers | `I32_REM_S` | `i32.rem_s` | ✅ | 🔲 | division guard for traps |
| Integers | `I32_REM_U` | `i32.rem_u` | ✅ | 🔲 | division guard for traps |
| Integers | `I32_SHL` | `i32.shl` | ✅ | 🔲 | — |
| Integers | `I32_SHR_S` | `i32.shr_s` | ✅ | 🔲 | — |
| Integers | `I32_SHR_U` | `i32.shr_u` | ✅ | 🔲 | — |
| Integers | `I32_XOR` | `i32.xor` | ✅ | 🔲 | — |
| Integers | `I32_AND` | `i32.and` | ✅ | 🔲 | — |
| Integers | `I32_OR` | `i32.or` | ✅ | 🔲 | — |
| Integers | `I32_CLZ` | `i32.clz` | ✅ | 🔲 | — |
| Integers | `I32_CTZ` | `i32.ctz` | ✅ | 🔲 | — |
| Integers | `I32_POPCNT` | `i32.popcnt` | ✅ | 🔲 | — |
| Integers | `I32_ROTL` | `i32.rotl` | ✅ | 🔲 | — |
| Integers | `I32_ROTR` | `i32.rotr` | ✅ | 🔲 | — |
| Integers | `I32_EXTEND8_S` | `i32.extend8_s` | ✅ | 🔲 | — |
| Integers | `I32_EXTEND16_S` | `i32.extend16_s` | ✅ | 🔲 | — |
| Integers | `I32_EQZ` | `i32.eqz` | ✅ | 🔲 | — |
| Integers | `I32_EQ` | `i32.eq` | ✅ | 🔲 | — |
| Integers | `I32_NE` | `i32.ne` | ✅ | 🔲 | — |
| Integers | `I32_LT_S` | `i32.lt_s` | ✅ | 🔲 | — |
| Integers | `I32_LT_U` | `i32.lt_u` | ✅ | 🔲 | — |
| Integers | `I32_GT_S` | `i32.gt_s` | ✅ | 🔲 | — |
| Integers | `I32_GT_U` | `i32.gt_u` | ✅ | 🔲 | — |
| Integers | `I32_LE_S` | `i32.le_s` | ✅ | 🔲 | — |
| Integers | `I32_LE_U` | `i32.le_u` | ✅ | 🔲 | — |
| Integers | `I32_GE_S` | `i32.ge_s` | ✅ | 🔲 | — |
| Integers | `I32_GE_U` | `i32.ge_u` | ✅ | 🔲 | — |
| Integers | `I32_TO_I64_S` | `i32.to_i64_s` | ✅ | 🔲 | — |
| Integers | `I32_TO_I64_U` | `i32.to_i64_u` | ✅ | 🔲 | — |
| Integers | `I32_TO_F32_U` | `i32.to_f32_u` | ✅ | 🔲 | — |
| Integers | `I32_TO_F32_S` | `i32.to_f32_s` | ✅ | 🔲 | — |
| Integers | `I32_TO_F64_U` | `i32.to_f64_u` | ✅ | 🔲 | — |
| Integers | `I32_TO_F64_S` | `i32.to_f64_s` | ✅ | 🔲 | — |
| Integers | `I32_REINTERPRET_F32` | `i32.reinterpret_f32` | ✅ | 🔲 | — |
| Integers | `I64_CONST` | `i64.const` | ◐ | 🔲 | boxable i64 immediates only |
| Integers | `I64_ADD` | `i64.add` | ✅ | 🔲 | boxability guard for arithmetic results |
| Integers | `I64_SUB` | `i64.sub` | ✅ | 🔲 | boxability guard for arithmetic results |
| Integers | `I64_MUL` | `i64.mul` | ✅ | 🔲 | boxability guard for arithmetic results |
| Integers | `I64_DIV_S` | `i64.div_s` | ✅ | 🔲 | division and boxability guards |
| Integers | `I64_DIV_U` | `i64.div_u` | ✅ | 🔲 | division and boxability guards |
| Integers | `I64_REM_S` | `i64.rem_s` | ✅ | 🔲 | division guard; result cannot overflow the boxed payload |
| Integers | `I64_REM_U` | `i64.rem_u` | ✅ | 🔲 | division guard; result cannot overflow the boxed payload |
| Integers | `I64_SHL` | `i64.shl` | ✅ | 🔲 | boxability guard where needed |
| Integers | `I64_SHR_S` | `i64.shr_s` | ✅ | 🔲 | — |
| Integers | `I64_SHR_U` | `i64.shr_u` | ✅ | 🔲 | boxability guard where needed |
| Integers | `I64_XOR` | `i64.xor` | ✅ | 🔲 | — |
| Integers | `I64_AND` | `i64.and` | ✅ | 🔲 | — |
| Integers | `I64_OR` | `i64.or` | ✅ | 🔲 | — |
| Integers | `I64_CLZ` | `i64.clz` | ✅ | 🔲 | — |
| Integers | `I64_CTZ` | `i64.ctz` | ✅ | 🔲 | — |
| Integers | `I64_POPCNT` | `i64.popcnt` | ✅ | 🔲 | — |
| Integers | `I64_ROTL` | `i64.rotl` | ✅ | 🔲 | — |
| Integers | `I64_ROTR` | `i64.rotr` | ✅ | 🔲 | — |
| Integers | `I64_EXTEND8_S` | `i64.extend8_s` | ✅ | 🔲 | — |
| Integers | `I64_EXTEND16_S` | `i64.extend16_s` | ✅ | 🔲 | — |
| Integers | `I64_EXTEND32_S` | `i64.extend32_s` | ✅ | 🔲 | — |
| Integers | `I64_EQZ` | `i64.eqz` | ✅ | 🔲 | — |
| Integers | `I64_EQ` | `i64.eq` | ✅ | 🔲 | — |
| Integers | `I64_NE` | `i64.ne` | ✅ | 🔲 | — |
| Integers | `I64_LT_S` | `i64.lt_s` | ✅ | 🔲 | — |
| Integers | `I64_LT_U` | `i64.lt_u` | ✅ | 🔲 | — |
| Integers | `I64_GT_S` | `i64.gt_s` | ✅ | 🔲 | — |
| Integers | `I64_GT_U` | `i64.gt_u` | ✅ | 🔲 | — |
| Integers | `I64_LE_S` | `i64.le_s` | ✅ | 🔲 | — |
| Integers | `I64_LE_U` | `i64.le_u` | ✅ | 🔲 | — |
| Integers | `I64_GE_S` | `i64.ge_s` | ✅ | 🔲 | — |
| Integers | `I64_GE_U` | `i64.ge_u` | ✅ | 🔲 | — |
| Integers | `I64_TO_I32` | `i64.to_i32` | ✅ | 🔲 | — |
| Integers | `I64_TO_F32_S` | `i64.to_f32_s` | ✅ | 🔲 | — |
| Integers | `I64_TO_F32_U` | `i64.to_f32_u` | ✅ | 🔲 | — |
| Integers | `I64_TO_F64_S` | `i64.to_f64_s` | ✅ | 🔲 | — |
| Integers | `I64_TO_F64_U` | `i64.to_f64_u` | ✅ | 🔲 | — |
| Integers | `I64_REINTERPRET_F64` | `i64.reinterpret_f64` | ✅ | 🔲 | — |
| Floating point | `F32_CONST` | `f32.const` | ✅ | 🔲 | — |
| Floating point | `F32_ADD` | `f32.add` | ✅ | 🔲 | — |
| Floating point | `F32_SUB` | `f32.sub` | ✅ | 🔲 | — |
| Floating point | `F32_MUL` | `f32.mul` | ✅ | 🔲 | — |
| Floating point | `F32_DIV` | `f32.div` | ✅ | 🔲 | — |
| Floating point | `F32_REM` | `f32.rem` | ◐ | 🔲 | terminal fallback |
| Floating point | `F32_MOD` | `f32.mod` | ◐ | 🔲 | terminal fallback |
| Floating point | `F32_ABS` | `f32.abs` | ✅ | 🔲 | — |
| Floating point | `F32_NEG` | `f32.neg` | ✅ | 🔲 | — |
| Floating point | `F32_SQRT` | `f32.sqrt` | ✅ | 🔲 | — |
| Floating point | `F32_CEIL` | `f32.ceil` | ✅ | 🔲 | — |
| Floating point | `F32_FLOOR` | `f32.floor` | ✅ | 🔲 | — |
| Floating point | `F32_TRUNC` | `f32.trunc` | ✅ | 🔲 | — |
| Floating point | `F32_NEAREST` | `f32.nearest` | ✅ | 🔲 | — |
| Floating point | `F32_MIN` | `f32.min` | ✅ | 🔲 | — |
| Floating point | `F32_MAX` | `f32.max` | ✅ | 🔲 | — |
| Floating point | `F32_COPYSIGN` | `f32.copysign` | ✅ | 🔲 | — |
| Floating point | `F32_EQ` | `f32.eq` | ✅ | 🔲 | — |
| Floating point | `F32_NE` | `f32.ne` | ✅ | 🔲 | — |
| Floating point | `F32_LT` | `f32.lt` | ✅ | 🔲 | — |
| Floating point | `F32_GT` | `f32.gt` | ✅ | 🔲 | — |
| Floating point | `F32_LE` | `f32.le` | ✅ | 🔲 | — |
| Floating point | `F32_GE` | `f32.ge` | ✅ | 🔲 | — |
| Floating point | `F32_TO_I32_S` | `f32.to_i32_s` | ✅ | 🔲 | — |
| Floating point | `F32_TO_I32_U` | `f32.to_i32_u` | ✅ | 🔲 | — |
| Floating point | `F32_TO_I64_S` | `f32.to_i64_s` | ✅ | 🔲 | boxability guard |
| Floating point | `F32_TO_I64_U` | `f32.to_i64_u` | ✅ | 🔲 | boxability guard |
| Floating point | `F32_TO_F64` | `f32.to_f64` | ✅ | 🔲 | — |
| Floating point | `F32_REINTERPRET_I32` | `f32.reinterpret_i32` | ✅ | 🔲 | — |
| Floating point | `F64_CONST` | `f64.const` | ✅ | 🔲 | — |
| Floating point | `F64_ADD` | `f64.add` | ✅ | 🔲 | — |
| Floating point | `F64_SUB` | `f64.sub` | ✅ | 🔲 | — |
| Floating point | `F64_MUL` | `f64.mul` | ✅ | 🔲 | — |
| Floating point | `F64_DIV` | `f64.div` | ✅ | 🔲 | — |
| Floating point | `F64_REM` | `f64.rem` | ◐ | 🔲 | terminal fallback |
| Floating point | `F64_MOD` | `f64.mod` | ◐ | 🔲 | terminal fallback |
| Floating point | `F64_ABS` | `f64.abs` | ✅ | 🔲 | — |
| Floating point | `F64_NEG` | `f64.neg` | ✅ | 🔲 | — |
| Floating point | `F64_SQRT` | `f64.sqrt` | ✅ | 🔲 | — |
| Floating point | `F64_CEIL` | `f64.ceil` | ✅ | 🔲 | — |
| Floating point | `F64_FLOOR` | `f64.floor` | ✅ | 🔲 | — |
| Floating point | `F64_TRUNC` | `f64.trunc` | ✅ | 🔲 | — |
| Floating point | `F64_NEAREST` | `f64.nearest` | ✅ | 🔲 | — |
| Floating point | `F64_MIN` | `f64.min` | ✅ | 🔲 | — |
| Floating point | `F64_MAX` | `f64.max` | ✅ | 🔲 | — |
| Floating point | `F64_COPYSIGN` | `f64.copysign` | ✅ | 🔲 | — |
| Floating point | `F64_EQ` | `f64.eq` | ✅ | 🔲 | — |
| Floating point | `F64_NE` | `f64.ne` | ✅ | 🔲 | — |
| Floating point | `F64_LT` | `f64.lt` | ✅ | 🔲 | — |
| Floating point | `F64_GT` | `f64.gt` | ✅ | 🔲 | — |
| Floating point | `F64_LE` | `f64.le` | ✅ | 🔲 | — |
| Floating point | `F64_GE` | `f64.ge` | ✅ | 🔲 | — |
| Floating point | `F64_TO_I32_S` | `f64.to_i32_s` | ✅ | 🔲 | — |
| Floating point | `F64_TO_I32_U` | `f64.to_i32_u` | ✅ | 🔲 | — |
| Floating point | `F64_TO_I64_S` | `f64.to_i64_s` | ✅ | 🔲 | boxability guard |
| Floating point | `F64_TO_I64_U` | `f64.to_i64_u` | ✅ | 🔲 | boxability guard |
| Floating point | `F64_TO_F32` | `f64.to_f32` | ✅ | 🔲 | — |
| Floating point | `F64_REINTERPRET_I64` | `f64.reinterpret_i64` | ✅ | 🔲 | — |
| Strings | `STRING_NEW_UTF32` | `string.new_utf32` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Strings | `STRING_LEN` | `string.len` | ✅ | 🔲 | native typed-array-length-style length read |
| Strings | `STRING_CONCAT` | `string.concat` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Strings | `STRING_EQ` | `string.eq` | ⬜ | 🔲 | content compare stays threaded |
| Strings | `STRING_NE` | `string.ne` | ⬜ | 🔲 | content compare stays threaded |
| Strings | `STRING_LT` | `string.lt` | ⬜ | 🔲 | string comparisons stay threaded |
| Strings | `STRING_GT` | `string.gt` | ⬜ | 🔲 | string comparisons stay threaded |
| Strings | `STRING_LE` | `string.le` | ⬜ | 🔲 | string comparisons stay threaded |
| Strings | `STRING_GE` | `string.ge` | ⬜ | 🔲 | string comparisons stay threaded |
| Strings | `STRING_ENCODE_UTF32` | `string.encode_utf32` | ◐ | 🔲 | terminal fallback |
| Arrays | `ARRAY_NEW` | `array.new` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Arrays | `ARRAY_NEW_DEFAULT` | `array.new_default` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Arrays | `ARRAY_LEN` | `array.len` | ✅ | 🔲 | native typed-array length fast path |
| Arrays | `ARRAY_GET` | `array.get` | ✅ | 🔲 | native typed-array get fast path |
| Arrays | `ARRAY_SET` | `array.set` | ◐ | 🔲 | guarded native store when the register budget permits; ref stores remain terminal |
| Arrays | `ARRAY_FILL` | `array.fill` | ◐ | 🔲 | terminal deopt boundary; trace prefix stays native |
| Arrays | `ARRAY_COPY` | `array.copy` | ◐ | 🔲 | terminal deopt boundary; trace prefix stays native |
| Arrays | `ARRAY_APPEND` | `array.append` | ◐ | 🔲 | terminal deopt boundary; trace prefix stays native |
| Arrays | `ARRAY_DELETE` | `array.delete` | ⬜ | 🔲 | mutation and removed-value ownership stay threaded |
| Arrays | `ARRAY_SLICE` | `array.slice` | ⬜ | 🔲 | allocation and ownership stay threaded |
| Structs | `STRUCT_NEW` | `struct.new` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Structs | `STRUCT_NEW_DEFAULT` | `struct.new_default` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Structs | `STRUCT_GET` | `struct.get` | ✅ | 🔲 | native field get fast path; static plans resolve constant field indexes; a `*HostStruct` field loads Go memory in place |
| Structs | `STRUCT_SET` | `struct.set` | ◐ | 🔲 | guarded native store when the register budget permits; ref-field writes remain terminal; a `*HostStruct` field stores Go memory in place when it is as wide as its slot |
| Maps | `MAP_NEW` | `map.new` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Maps | `MAP_NEW_DEFAULT` | `map.new_default` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Maps | `MAP_LEN` | `map.len` | ◐ | 🔲 | terminal fallback |
| Maps | `MAP_GET` | `map.get` | ◐ | 🔲 | terminal fallback |
| Maps | `MAP_LOOKUP` | `map.lookup` | ◐ | 🔲 | terminal fallback |
| Maps | `MAP_SET` | `map.set` | ◐ | 🔲 | terminal deopt boundary; trace prefix stays native |
| Maps | `MAP_DELETE` | `map.delete` | ⬜ | 🔲 | mutation stays threaded |
| Maps | `MAP_CLEAR` | `map.clear` | ⬜ | 🔲 | mutation stays threaded |
| Maps | `MAP_KEYS` | `map.keys` | ◐ | 🔲 | terminal fallback |
| Closures | `CLOSURE_NEW` | `closure.new` | ⬜ | 🔲 | allocation stays interpreter-owned |
| Maps | `MAP_ITER` | `map.iter` | ◐ | 🔲 | terminal fallback |
| Structured errors | `THROW` | `throw` | ◐ | 🔲 | terminal fallback to handler logic |
| Structured errors | `ERROR_NEW` | `error.new` | ◐ | 🔲 | terminal fallback allocation |
| Structured errors | `ERROR_GET` | `error.get` | ✅ | 🔲 | native error field read |
| Structured errors | `ERROR_CODE` | `error.code` | ◐ | 🔲 | terminal fallback |
| Strings | `STRING_ITER` | `string.iter` | ◐ | 🔲 | terminal fallback |

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
- preserve threaded/native semantic parity.

## Related

- `docs/guides/add-opcode.md` — checklist for adding or changing an opcode
- `docs/verification.md` — static validation and stack rules
- `docs/value-representation.md` — kinds, boxed layout, and boolean representation
- `docs/jit-internals.md` — native lowering and fallback behavior
- `docs/compatibility.md` — platform and backend availability
