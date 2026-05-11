# Instruction Set Reference

Complete opcode reference for minivm.

## Agent Usage

Use this as the semantic source of truth when adding or debugging opcodes.

- Opcode byte order lives in `instr/opcode.go`; append new opcodes, do not insert between existing ones.
- Operand widths live in `instr/type.go`; handler widths must match exactly.
- Threaded behavior lives in `interp/threaded.go`; JIT status comes from `interp/jit_arm64.go`.
- When changing semantics, update this table plus tests in `instr/` and `interp/`.

All opcodes are 1 byte.
Operands are fixed-width or length-prefixed.

---

## Execution Semantics

Unless otherwise specified:

- operands are little-endian
- stack operands are popped right-to-left
- comparison instructions push `I32(0)` or `I32(1)`
- instructions that expose refs retain them
- instructions that overwrite refs release replaced refs
- runtime traps panic and are recovered by `interp.Run`

---

## JIT Status

| Status | Meaning |
|---|---|
| ✅ | fully compiled to native ARM64 |
| ◐ | partially JIT compiled |
| ⬜ | threaded-only execution |

---

## Operand Width Notation

Operand widths are declared in `instr/type.go`.

| Notation | Meaning |
|---|---|
| `{n}` | fixed `n`-byte operand |
| `{-n, n}` | one count byte followed by `count × n`-byte values |

Examples:

- `{2}` → one u16 operand
- `{-2, 2}` → count byte + repeated u16 operands

---

## Branch Offsets

Branch operands are relative to the end of the instruction.

```text
target = instruction_start + instruction_width + operand
```

Example:

```text
BR 5
```

skips 5 bytes past the end of the 3-byte `BR` instruction.

`BR 0` is effectively a fall-through.

---

## Stack Manipulation

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `NOP` | `{}` | `→` | ✅ | No-op. Threaded execution collapses consecutive NOP runs into a single dispatch step. JIT emits no native instruction. |
| `DROP` | `{}` | `x →` | ✅ | Pop and discard the top value. |
| `DUP` | `{}` | `x → x x` | ✅ | Duplicate the top value. |
| `SWAP` | `{}` | `a b → b a` | ✅ | Swap the top two stack values. |
| `SELECT` | `{}` | `a b cond → x` | ✅ | Push `a` if `cond ≠ 0`, otherwise `b`. |
| `UNREACHABLE` | `{}` | `→` | ⬜ | Trap with `ErrUnreachableExecuted`. Used as dead-code filler before DCE compaction removes unreachable bytes. |

---

## Control Flow

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `BR` | `{2}` | `→` | ◐ | Unconditional relative jump. JIT compiles only when the current segment has no pending return values. |
| `BR_IF` | `{2}` | `cond →` | ◐ | Jump if `cond ≠ 0`, otherwise fall through. JIT compiles only simple stack shapes. |
| `BR_TABLE` | `{-2, 2}` | `index →` | ◐ | Jump through a jump table. Out-of-range indices use the default target. JIT compiles only simple stack shapes. |
| `CALL` | `{}` | `fn →` | ⬜ | Call `*Function` or `*HostFunction`. Pushes a new frame. |
| `RETURN` | `{}` | `→` | ⬜ | Return from the current frame. |

---

## Variables

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `GLOBAL_GET` | `{2}` | `→ x` | ⬜ | Push global at u16 index. |
| `GLOBAL_SET` | `{2}` | `x →` | ⬜ | Store value to global at u16 index. |
| `GLOBAL_TEE` | `{2}` | `x → x` | ⬜ | Store value to global and leave it on the stack. |
| `LOCAL_GET` | `{1}` | `→ x` | ◐ | Push local at u8 index relative to the frame base pointer. JIT supports numeric function params/locals. |
| `LOCAL_SET` | `{1}` | `x →` | ◐ | Store value to local at u8 index. JIT supports numeric function params/locals. |
| `LOCAL_TEE` | `{1}` | `x → x` | ◐ | Store value to local and leave it on the stack. JIT supports numeric function params/locals. |
| `CONST_GET` | `{2}` | `→ x` | ◐ | Push constant at u16 index. JIT supports boxed numeric constants. |

---

## References

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `REF_NULL` | `{}` | `→ ref` | ⬜ | Push `BoxedNull` (heap index 0). |
| `REF_IS_NULL` | `{}` | `ref → i32` | ⬜ | Push `I32(1)` if ref is null, otherwise `I32(0)`. |
| `REF_EQ` | `{}` | `a b → i32` | ⬜ | Push `I32(1)` if refs point to the same heap index. |
| `REF_NE` | `{}` | `a b → i32` | ⬜ | Push `I32(1)` if refs differ. |
| `REF_TEST` | `{2}` | `ref → i32` | ⬜ | Push `I32(1)` if ref matches the type at u16 index. |
| `REF_CAST` | `{2}` | `ref → ref` | ⬜ | Trap with `ErrTypeMismatch` if the ref type does not match. |

---

## i32 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `I32_CONST` | `{4}` | `→ i32` | ✅ | Push immediate i32. |
| `I32_ADD` | `{}` | `a b → i32` | ✅ | Signed addition. |
| `I32_SUB` | `{}` | `a b → i32` | ✅ | Signed subtraction. |
| `I32_MUL` | `{}` | `a b → i32` | ✅ | Signed multiplication. |
| `I32_DIV_S` | `{}` | `a b → i32` | ✅ | Signed division. Traps with `ErrDivideByZero` if divisor is zero. |
| `I32_DIV_U` | `{}` | `a b → i32` | ✅ | Unsigned division. Traps with `ErrDivideByZero` if divisor is zero. |
| `I32_REM_S` | `{}` | `a b → i32` | ✅ | Signed remainder. Traps with `ErrDivideByZero` if divisor is zero. |
| `I32_REM_U` | `{}` | `a b → i32` | ✅ | Unsigned remainder. Traps with `ErrDivideByZero` if divisor is zero. |
| `I32_SHL` | `{}` | `a b → i32` | ✅ | Left shift. Shift amount uses the low 5 bits. |
| `I32_SHR_S` | `{}` | `a b → i32` | ✅ | Arithmetic right shift. |
| `I32_SHR_U` | `{}` | `a b → i32` | ✅ | Logical right shift. |
| `I32_AND` | `{}` | `a b → i32` | ✅ | Bitwise AND. |
| `I32_OR` | `{}` | `a b → i32` | ✅ | Bitwise OR. |
| `I32_XOR` | `{}` | `a b → i32` | ✅ | Bitwise XOR. |
| `I32_EQZ` | `{}` | `x → i32` | ✅ | Push `I32(1)` if value is zero. |
| `I32_EQ` | `{}` | `a b → i32` | ✅ | Equality comparison. |
| `I32_NE` | `{}` | `a b → i32` | ✅ | Inequality comparison. |
| `I32_LT_S` | `{}` | `a b → i32` | ✅ | Signed less-than comparison. |
| `I32_LT_U` | `{}` | `a b → i32` | ✅ | Unsigned less-than comparison. |
| `I32_GT_S` | `{}` | `a b → i32` | ✅ | Signed greater-than comparison. |
| `I32_GT_U` | `{}` | `a b → i32` | ✅ | Unsigned greater-than comparison. |
| `I32_LE_S` | `{}` | `a b → i32` | ✅ | Signed less-or-equal comparison. |
| `I32_LE_U` | `{}` | `a b → i32` | ✅ | Unsigned less-or-equal comparison. |
| `I32_GE_S` | `{}` | `a b → i32` | ✅ | Signed greater-or-equal comparison. |
| `I32_GE_U` | `{}` | `a b → i32` | ✅ | Unsigned greater-or-equal comparison. |
| `I32_TO_I64_S` | `{}` | `i32 → i64` | ✅ | Sign-extend i32 to i64. |
| `I32_TO_I64_U` | `{}` | `i32 → i64` | ✅ | Zero-extend i32 to i64. |
| `I32_TO_F32_S` | `{}` | `i32 → f32` | ✅ | Convert signed i32 to f32. |
| `I32_TO_F32_U` | `{}` | `i32 → f32` | ✅ | Convert unsigned i32 to f32. |
| `I32_TO_F64_S` | `{}` | `i32 → f64` | ✅ | Convert signed i32 to f64. |
| `I32_TO_F64_U` | `{}` | `i32 → f64` | ✅ | Convert unsigned i32 to f64. |

---

## i64 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `I64_CONST` | `{8}` | `→ i64` | ✅ | Push immediate i64. Values outside the inline range may spill to heap storage. |
| `I64_ADD` | `{}` | `a b → i64` | ✅ | Signed addition. |
| `I64_SUB` | `{}` | `a b → i64` | ✅ | Signed subtraction. |
| `I64_MUL` | `{}` | `a b → i64` | ✅ | Signed multiplication. |
| `I64_DIV_S` | `{}` | `a b → i64` | ✅ | Signed division. Traps with `ErrDivideByZero` if divisor is zero. |
| `I64_DIV_U` | `{}` | `a b → i64` | ✅ | Unsigned division. Traps with `ErrDivideByZero` if divisor is zero. |
| `I64_REM_S` | `{}` | `a b → i64` | ✅ | Signed remainder. |
| `I64_REM_U` | `{}` | `a b → i64` | ✅ | Unsigned remainder. |
| `I64_SHL` | `{}` | `a b → i64` | ✅ | Left shift. Shift amount uses the low 6 bits. |
| `I64_SHR_S` | `{}` | `a b → i64` | ✅ | Arithmetic right shift. |
| `I64_SHR_U` | `{}` | `a b → i64` | ✅ | Logical right shift. |
| `I64_EQZ` | `{}` | `x → i32` | ✅ | Push `I32(1)` if value is zero. |
| `I64_EQ` … `I64_GE_U` | `{}` | `a b → i32` | ✅ | Same semantics as i32 comparisons. |
| `I64_TO_I32` | `{}` | `i64 → i32` | ✅ | Truncate to the low 32 bits. |
| `I64_TO_F32_S` | `{}` | `i64 → f32` | ✅ | Convert signed i64 to f32. |
| `I64_TO_F32_U` | `{}` | `i64 → f32` | ✅ | Convert unsigned i64 to f32. |
| `I64_TO_F64_S` | `{}` | `i64 → f64` | ✅ | Convert signed i64 to f64. |
| `I64_TO_F64_U` | `{}` | `i64 → f64` | ✅ | Convert unsigned i64 to f64. |

---

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

---

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

---

## String Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `STRING_NEW_UTF32` | `{}` | `array → string` | ⬜ | Create a `types.String` from UTF-32 codepoints. |
| `STRING_LEN` | `{}` | `string → i32` | ⬜ | Push string length in codepoints. |
| `STRING_CONCAT` | `{}` | `a b → string` | ⬜ | Concatenate two strings. |
| `STRING_EQ` … `STRING_GE` | `{}` | `a b → i32` | ⬜ | Lexicographic string comparisons. |
| `STRING_ENCODE_UTF32` | `{}` | `string → array` | ⬜ | Encode a string as UTF-32 codepoints. |

---

## Array Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `ARRAY_NEW` | `{2}` | `value count → array` | ⬜ | Create a typed array filled with `value`. |
| `ARRAY_NEW_DEFAULT` | `{2}` | `count → array` | ⬜ | Create a zero-initialized typed array. |
| `ARRAY_LEN` | `{}` | `array → i32` | ⬜ | Push element count. |
| `ARRAY_GET` | `{}` | `array index → value` | ⬜ | Load array element. Traps with `ErrIndexOutOfRange` on invalid index. |
| `ARRAY_SET` | `{}` | `array index value →` | ⬜ | Store array element. |
| `ARRAY_FILL` | `{}` | `array offset count value →` | ⬜ | Fill a range with a repeated value. |
| `ARRAY_COPY` | `{}` | `dst dstOffset src srcOffset count →` | ⬜ | Copy array elements between arrays. |

---

## Struct Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `STRUCT_NEW` | `{2}` | `fields → struct` | ⬜ | Create a struct from field values. |
| `STRUCT_NEW_DEFAULT` | `{2}` | `→ struct` | ⬜ | Create a zero-initialized struct. |
| `STRUCT_GET` | `{}` | `struct index → value` | ⬜ | Load struct field. |
| `STRUCT_SET` | `{}` | `struct index value →` | ⬜ | Store struct field. |
