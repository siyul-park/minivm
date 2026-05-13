# Instruction Set Reference

Complete opcode reference for minivm.

## Agent Usage

Semantic source of truth for adding/debugging opcodes.

- Opcode byte order: `instr/opcode.go`; append new opcodes, never insert between existing ones.
- Operand widths: `instr/type.go`; handler widths must match exactly.
- Threaded behavior: `interp/threaded.go`.
- JIT status: `interp/jit_arm64.go`.
- Semantics changes require this table plus `instr/` and `interp/` tests.

All opcodes are 1 byte. Operands are fixed-width or length-prefixed.

## Execution Semantics

Unless noted:

- operands are little-endian
- stack operands pop right-to-left
- comparisons push `I32(0)` or `I32(1)`
- ref-exposing instructions retain refs
- ref-overwriting instructions release replaced refs
- runtime traps panic and are recovered by `interp.Run`

## JIT Status

| Status | Meaning |
|---|---|
| ✅ | native ARM64 |
| ◐ | partially JIT compiled |
| ⬜ | threaded-only |

## Operand Width Notation

Declared in `instr/type.go`.

| Notation | Meaning |
|---|---|
| `{n}` | fixed `n`-byte operand |
| `{-n, n}` | count byte + `count × n`-byte values |

Examples: `{2}` = one u16; `{-2, 2}` = count byte + repeated u16 operands.

## Branch Offsets

Branch operands are relative to instruction end:

```text
target = instruction_start + instruction_width + operand
```

`BR 5` skips 5 bytes past the 3-byte `BR`; `BR 0` is fall-through.

## Stack Manipulation

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `NOP` | `{}` | `→` | ✅ | No-op. Normal threaded execution collapses NOP runs into one dispatch; `WithTick(1)` preserves per-instruction hooks. JIT emits nothing. |
| `DROP` | `{}` | `x →` | ✅ | Pop/discard top value. |
| `DUP` | `{}` | `x → x x` | ✅ | Duplicate top value. |
| `SWAP` | `{}` | `a b → b a` | ✅ | Swap top two values. |
| `SELECT` | `{}` | `a b cond → x` | ✅ | Push `a` if `cond ≠ 0`, else `b`. |
| `UNREACHABLE` | `{}` | `→` | ⬜ | Trap with `ErrUnreachableExecuted`; used as dead-code filler before DCE compaction. |

## Control Flow

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `BR` | `{2}` | `→` | ◐ | Unconditional relative jump. JIT only when current segment has no pending return values. |
| `BR_IF` | `{2}` | `cond →` | ◐ | Jump if `cond ≠ 0`, else fall through. JIT only for simple stack shapes. |
| `BR_TABLE` | `{-2, 2}` | `index →` | ◐ | Jump table; out-of-range uses default target. JIT only for simple stack shapes. |
| `CALL` | `{}` | `fn →` | ⬜ | Call `*Function` or `*HostFunction`; pushes a frame. |
| `RETURN` | `{}` | `→` | ⬜ | Return from current frame. |

## Variables

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `GLOBAL_GET` | `{2}` | `→ x` | ◐ | Push global at u16 index. JIT supports same-segment proven numeric globals. |
| `GLOBAL_SET` | `{2}` | `x →` | ◐ | Store global at u16 index. JIT supports numeric globals. |
| `GLOBAL_TEE` | `{2}` | `x → x` | ◐ | Store global and keep value. JIT supports numeric globals. |
| `LOCAL_GET` | `{1}` | `→ x` | ◐ | Push u8 local relative to frame base. JIT supports numeric params/locals. |
| `LOCAL_SET` | `{1}` | `x →` | ◐ | Store u8 local. JIT supports numeric params/locals. |
| `LOCAL_TEE` | `{1}` | `x → x` | ◐ | Store local and keep value. JIT supports numeric params/locals. |
| `CONST_GET` | `{2}` | `→ x` | ◐ | Push u16 constant. JIT supports boxed numeric constants. |

## References

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `REF_NULL` | `{}` | `→ ref` | ⬜ | Push `BoxedNull`, heap index `0`. |
| `REF_IS_NULL` | `{}` | `ref → i32` | ⬜ | Push `I32(1)` if null, else `I32(0)`. |
| `REF_EQ` | `{}` | `a b → i32` | ⬜ | Push `I32(1)` if refs point to same heap index. |
| `REF_NE` | `{}` | `a b → i32` | ⬜ | Push `I32(1)` if refs differ. |
| `REF_TEST` | `{2}` | `ref → i32` | ⬜ | Push `I32(1)` if ref matches type at u16 index. |
| `REF_CAST` | `{2}` | `ref → ref` | ⬜ | Trap with `ErrTypeMismatch` if ref type mismatches. |

## i32 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `I32_CONST` | `{4}` | `→ i32` | ✅ | Push immediate i32. |
| `I32_ADD` | `{}` | `a b → i32` | ✅ | Signed addition. |
| `I32_SUB` | `{}` | `a b → i32` | ✅ | Signed subtraction. |
| `I32_MUL` | `{}` | `a b → i32` | ✅ | Signed multiplication. |
| `I32_DIV_S` | `{}` | `a b → i32` | ✅ | Signed division; trap `ErrDivideByZero` if divisor is zero. |
| `I32_DIV_U` | `{}` | `a b → i32` | ✅ | Unsigned division; trap `ErrDivideByZero` if divisor is zero. |
| `I32_REM_S` | `{}` | `a b → i32` | ✅ | Signed remainder; trap `ErrDivideByZero` if divisor is zero. |
| `I32_REM_U` | `{}` | `a b → i32` | ✅ | Unsigned remainder; trap `ErrDivideByZero` if divisor is zero. |
| `I32_SHL` | `{}` | `a b → i32` | ✅ | Left shift; amount uses low 5 bits. |
| `I32_SHR_S` | `{}` | `a b → i32` | ✅ | Arithmetic right shift. |
| `I32_SHR_U` | `{}` | `a b → i32` | ✅ | Logical right shift. |
| `I32_AND` | `{}` | `a b → i32` | ✅ | Bitwise AND. |
| `I32_OR` | `{}` | `a b → i32` | ✅ | Bitwise OR. |
| `I32_XOR` | `{}` | `a b → i32` | ✅ | Bitwise XOR. |
| `I32_EQZ` | `{}` | `x → i32` | ✅ | Push `I32(1)` if zero. |
| `I32_EQ` | `{}` | `a b → i32` | ✅ | Equality comparison. |
| `I32_NE` | `{}` | `a b → i32` | ✅ | Inequality comparison. |
| `I32_LT_S` | `{}` | `a b → i32` | ✅ | Signed less-than. |
| `I32_LT_U` | `{}` | `a b → i32` | ✅ | Unsigned less-than. |
| `I32_GT_S` | `{}` | `a b → i32` | ✅ | Signed greater-than. |
| `I32_GT_U` | `{}` | `a b → i32` | ✅ | Unsigned greater-than. |
| `I32_LE_S` | `{}` | `a b → i32` | ✅ | Signed less-or-equal. |
| `I32_LE_U` | `{}` | `a b → i32` | ✅ | Unsigned less-or-equal. |
| `I32_GE_S` | `{}` | `a b → i32` | ✅ | Signed greater-or-equal. |
| `I32_GE_U` | `{}` | `a b → i32` | ✅ | Unsigned greater-or-equal. |
| `I32_TO_I64_S` | `{}` | `i32 → i64` | ✅ | Sign-extend i32 to i64. |
| `I32_TO_I64_U` | `{}` | `i32 → i64` | ✅ | Zero-extend i32 to i64. |
| `I32_TO_F32_S` | `{}` | `i32 → f32` | ✅ | Convert signed i32 to f32. |
| `I32_TO_F32_U` | `{}` | `i32 → f32` | ✅ | Convert unsigned i32 to f32. |
| `I32_TO_F64_S` | `{}` | `i32 → f64` | ✅ | Convert signed i32 to f64. |
| `I32_TO_F64_U` | `{}` | `i32 → f64` | ✅ | Convert unsigned i32 to f64. |

## i64 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `I64_CONST` | `{8}` | `→ i64` | ✅ | Push immediate i64; values outside inline range may spill to heap storage. |
| `I64_ADD` | `{}` | `a b → i64` | ✅ | Signed addition. |
| `I64_SUB` | `{}` | `a b → i64` | ✅ | Signed subtraction. |
| `I64_MUL` | `{}` | `a b → i64` | ✅ | Signed multiplication. |
| `I64_DIV_S` | `{}` | `a b → i64` | ✅ | Signed division; trap `ErrDivideByZero` if divisor is zero. |
| `I64_DIV_U` | `{}` | `a b → i64` | ✅ | Unsigned division; trap `ErrDivideByZero` if divisor is zero. |
| `I64_REM_S` | `{}` | `a b → i64` | ✅ | Signed remainder. |
| `I64_REM_U` | `{}` | `a b → i64` | ✅ | Unsigned remainder. |
| `I64_SHL` | `{}` | `a b → i64` | ✅ | Left shift; amount uses low 6 bits. |
| `I64_SHR_S` | `{}` | `a b → i64` | ✅ | Arithmetic right shift. |
| `I64_SHR_U` | `{}` | `a b → i64` | ✅ | Logical right shift. |
| `I64_EQZ` | `{}` | `x → i32` | ✅ | Push `I32(1)` if zero. |
| `I64_EQ` … `I64_GE_U` | `{}` | `a b → i32` | ✅ | Same semantics as i32 comparisons. |
| `I64_TO_I32` | `{}` | `i64 → i32` | ✅ | Truncate to low 32 bits. |
| `I64_TO_F32_S` | `{}` | `i64 → f32` | ✅ | Convert signed i64 to f32. |
| `I64_TO_F32_U` | `{}` | `i64 → f32` | ✅ | Convert unsigned i64 to f32. |
| `I64_TO_F64_S` | `{}` | `i64 → f64` | ✅ | Convert signed i64 to f64. |
| `I64_TO_F64_U` | `{}` | `i64 → f64` | ✅ | Convert unsigned i64 to f64. |

## f32 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `F32_CONST` | `{4}` | `→ f32` | ✅ | Push immediate IEEE-754 f32. |
| `F32_ADD` | `{}` | `a b → f32` | ✅ | Floating-point addition. |
| `F32_SUB` | `{}` | `a b → f32` | ✅ | Floating-point subtraction. |
| `F32_MUL` | `{}` | `a b → f32` | ✅ | Floating-point multiplication. |
| `F32_DIV` | `{}` | `a b → f32` | ✅ | Floating-point division. |
| `F32_EQ` … `F32_GE` | `{}` | `a b → i32` | ✅ | Floating-point comparisons. |
| `F32_TO_I32_S` | `{}` | `f32 → i32` | ✅ | Truncate to signed i32. |
| `F32_TO_I32_U` | `{}` | `f32 → i32` | ✅ | Truncate to unsigned i32. |
| `F32_TO_I64_S` | `{}` | `f32 → i64` | ✅ | Truncate to signed i64. |
| `F32_TO_I64_U` | `{}` | `f32 → i64` | ✅ | Truncate to unsigned i64. |
| `F32_TO_F64` | `{}` | `f32 → f64` | ✅ | Widen f32 to f64. |

## f64 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `F64_CONST` | `{8}` | `→ f64` | ✅ | Push immediate IEEE-754 f64. |
| `F64_ADD` | `{}` | `a b → f64` | ✅ | Floating-point addition. |
| `F64_SUB` | `{}` | `a b → f64` | ✅ | Floating-point subtraction. |
| `F64_MUL` | `{}` | `a b → f64` | ✅ | Floating-point multiplication. |
| `F64_DIV` | `{}` | `a b → f64` | ✅ | Floating-point division. |
| `F64_EQ` … `F64_GE` | `{}` | `a b → i32` | ✅ | Floating-point comparisons. |
| `F64_TO_I32_S` | `{}` | `f64 → i32` | ✅ | Truncate to signed i32. |
| `F64_TO_I32_U` | `{}` | `f64 → i32` | ✅ | Truncate to unsigned i32. |
| `F64_TO_I64_S` | `{}` | `f64 → i64` | ✅ | Truncate to signed i64. |
| `F64_TO_I64_U` | `{}` | `f64 → i64` | ✅ | Truncate to unsigned i64. |
| `F64_TO_F32` | `{}` | `f64 → f32` | ✅ | Narrow f64 to f32. |

## String Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `STRING_NEW_UTF32` | `{}` | `array → string` | ⬜ | Create `types.String` from UTF-32 codepoints. |
| `STRING_LEN` | `{}` | `string → i32` | ⬜ | Push string length in codepoints. |
| `STRING_CONCAT` | `{}` | `a b → string` | ⬜ | Concatenate strings. |
| `STRING_EQ` … `STRING_GE` | `{}` | `a b → i32` | ⬜ | Lexicographic comparisons. |
| `STRING_ENCODE_UTF32` | `{}` | `string → array` | ⬜ | Encode string as UTF-32 codepoints. |

## Array Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `ARRAY_NEW` | `{2}` | `value count → array` | ⬜ | Create typed array filled with `value`. |
| `ARRAY_NEW_DEFAULT` | `{2}` | `count → array` | ⬜ | Create zero-initialized typed array. |
| `ARRAY_LEN` | `{}` | `array → i32` | ⬜ | Push element count. |
| `ARRAY_GET` | `{}` | `array index → value` | ⬜ | Load element; trap `ErrIndexOutOfRange` on invalid index. |
| `ARRAY_SET` | `{}` | `array index value →` | ⬜ | Store element. |
| `ARRAY_FILL` | `{}` | `array offset count value →` | ⬜ | Fill range with repeated value. |
| `ARRAY_COPY` | `{}` | `dst dstOffset src srcOffset count →` | ⬜ | Copy elements between arrays. |

## Struct Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `STRUCT_NEW` | `{2}` | `fields → struct` | ⬜ | Create struct from field values. |
| `STRUCT_NEW_DEFAULT` | `{2}` | `→ struct` | ⬜ | Create zero-initialized struct. |
| `STRUCT_GET` | `{}` | `struct index → value` | ⬜ | Load struct field. |
| `STRUCT_SET` | `{}` | `struct index value →` | ⬜ | Store struct field. |
