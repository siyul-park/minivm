# Instruction Set Reference

Complete opcode table for minivm. All opcodes are 1 byte. Operands are fixed-width or length-prefixed.

**JIT column**: ✅ compiled to native ARM64 | ⬜ always runs in threaded tier.

## Operand Width Notation

Operand widths are declared in `instr/type.go`:
- `{n}` — fixed `n`-byte operand (little-endian)
- `{-n, n}` — one count byte, then `count × n`-byte values

## Stack Manipulation

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `NOP` | `{}` | ✅ | No-op. Threaded: the closure at the first NOP of a run jumps over all consecutive NOPs at once. JIT: returns `true` without emitting any native instruction (contributes to sub-block `count`). |
| `DROP` | `{}` | ✅ | Pop and discard the top value. Releases ref if `KindRef`. |
| `DUP` | `{}` | ✅ | Duplicate the top value. Retains ref if `KindRef`. |
| `SWAP` | `{}` | ✅ | Swap the top two values. No RC change. |
| `SELECT` | `{}` | ⬜ | Pop condition (I32), then two values. Push first if condition≠0, else second. **Not yet implemented.** |
| `UNREACHABLE` | `{}` | ⬜ | Panics with `ErrUnreachableExecuted`. The JIT handler returns `false`, ending the current sub-block. Used as dead-code filler by DCE. |

## Control Flow

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `BR` | `{2}` | ⬜ | Unconditional jump. Operand: u16 byte offset past the end of this instruction. |
| `BR_IF` | `{2}` | ⬜ | Pop I32 condition. Jump by operand if non-zero, otherwise fall through. |
| `BR_TABLE` | `{-2, 2}` | ⬜ | Pop I32 index. One count byte, then `count` u16 case offsets, then one u16 default offset. Clamps out-of-range index to default. |
| `CALL` | `{}` | ⬜ | Pop a `KindRef` pointing to a `*Function` or `*HostFunction`. Push a new frame. |
| `RETURN` | `{}` | ⬜ | Pop the current frame. Execution resumes in the caller. |

**BR offset semantics**: target = `instruction_start + instruction_width + operand`. `BR 0` is effectively a fall-through. Example: `BR 5` skips 5 bytes past the end of the 3-byte BR instruction.

## Variables

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `GLOBAL_GET` | `{2}` | ⬜ | Push global at u16 index. Retains if `KindRef`. |
| `GLOBAL_SET` | `{2}` | ⬜ | Pop and store to global at u16 index. Releases old value if ref, retains new. |
| `GLOBAL_TEE` | `{2}` | ⬜ | Like `GLOBAL_SET` but leaves a copy on the stack. |
| `LOCAL_GET` | `{1}` | ⬜ | Push local at u8 index (relative to frame base pointer). Retains if `KindRef`. |
| `LOCAL_SET` | `{1}` | ⬜ | Pop and store to local at u8 index. |
| `LOCAL_TEE` | `{1}` | ⬜ | Like `LOCAL_SET` but leaves a copy on the stack. |
| `CONST_GET` | `{2}` | ✅ | Push constant at u16 index. Retains if `KindRef`. |

## References

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `REF_NULL` | `{}` | ⬜ | Push `BoxedNull` (heap index 0). |
| `REF_IS_NULL` | `{}` | ⬜ | Pop ref. Push `I32(1)` if it is null, else `I32(0)`. |
| `REF_EQ` | `{}` | ⬜ | Pop two refs. Push `I32(1)` if same heap index. |
| `REF_NE` | `{}` | ⬜ | Pop two refs. Push `I32(1)` if different heap index. |
| `REF_TEST` | `{2}` | ⬜ | Pop ref. Push `I32(1)` if its type matches type at u16 index. |
| `REF_CAST` | `{2}` | ⬜ | Pop ref. Push it if type matches, else panic `ErrTypeMismatch`. |

## i32 Operations

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `I32_CONST` | `{4}` | ✅ | Push immediate i32 (4-byte operand). |
| `I32_ADD` | `{}` | ✅ | Pop two i32, push sum. |
| `I32_SUB` | `{}` | ✅ | Pop two i32, push difference (second − top). |
| `I32_MUL` | `{}` | ✅ | Pop two i32, push product. |
| `I32_DIV_S` | `{}` | ✅ | Signed division. Panics `ErrDivideByZero` if divisor is 0. |
| `I32_DIV_U` | `{}` | ✅ | Unsigned division. Panics on zero. |
| `I32_REM_S` | `{}` | ✅ | Signed remainder. Panics on zero. |
| `I32_REM_U` | `{}` | ✅ | Unsigned remainder. Panics on zero. |
| `I32_SHL` | `{}` | ✅ | Left shift. Shift amount masked to low 5 bits. |
| `I32_SHR_S` | `{}` | ✅ | Arithmetic right shift. |
| `I32_SHR_U` | `{}` | ✅ | Logical right shift. |
| `I32_AND` | `{}` | ✅ | Bitwise AND. |
| `I32_OR` | `{}` | ✅ | Bitwise OR. |
| `I32_XOR` | `{}` | ✅ | Bitwise XOR. |
| `I32_EQZ` | `{}` | ✅ | Pop i32. Push `I32(1)` if zero, else `I32(0)`. |
| `I32_EQ` | `{}` | ✅ | Pop two i32. Push `I32(1)` if equal. |
| `I32_NE` | `{}` | ✅ | Push `I32(1)` if not equal. |
| `I32_LT_S` | `{}` | ✅ | Signed less-than. |
| `I32_LT_U` | `{}` | ✅ | Unsigned less-than. |
| `I32_GT_S` | `{}` | ✅ | Signed greater-than. |
| `I32_GT_U` | `{}` | ✅ | Unsigned greater-than. |
| `I32_LE_S` | `{}` | ✅ | Signed less-or-equal. |
| `I32_LE_U` | `{}` | ✅ | Unsigned less-or-equal. |
| `I32_GE_S` | `{}` | ✅ | Signed greater-or-equal. |
| `I32_GE_U` | `{}` | ✅ | Unsigned greater-or-equal. |
| `I32_TO_I64_S` | `{}` | ✅ | Sign-extend i32 to i64. |
| `I32_TO_I64_U` | `{}` | ✅ | Zero-extend i32 to i64. |
| `I32_TO_F32_S` | `{}` | ✅ | Convert signed i32 to f32. |
| `I32_TO_F32_U` | `{}` | ✅ | Convert unsigned i32 to f32. |
| `I32_TO_F64_S` | `{}` | ✅ | Convert signed i32 to f64. |
| `I32_TO_F64_U` | `{}` | ✅ | Convert unsigned i32 to f64. |

## i64 Operations

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `I64_CONST` | `{8}` | ✅ | Push immediate i64 (8-byte operand). May spill to heap if out of 49-bit range. |
| `I64_ADD` | `{}` | ✅ | Pop two i64, push sum. |
| `I64_SUB` | `{}` | ✅ | Pop two i64, push difference. |
| `I64_MUL` | `{}` | ✅ | Pop two i64, push product. |
| `I64_DIV_S` | `{}` | ✅ | Signed division. Panics on zero. |
| `I64_DIV_U` | `{}` | ✅ | Unsigned division. Panics on zero. |
| `I64_REM_S` | `{}` | ✅ | Signed remainder. |
| `I64_REM_U` | `{}` | ✅ | Unsigned remainder. |
| `I64_SHL` | `{}` | ✅ | Left shift, amount masked to low 6 bits. |
| `I64_SHR_S` | `{}` | ✅ | Arithmetic right shift. |
| `I64_SHR_U` | `{}` | ✅ | Logical right shift. |
| `I64_EQZ` | `{}` | ✅ | Push `I32(1)` if i64 value is zero. |
| `I64_EQ` … `I64_GE_U` | `{}` | ✅ | Same as i32 comparisons but for i64. Result is always `I32`. |
| `I64_TO_I32` | `{}` | ✅ | Truncate i64 to i32 (low 32 bits). |
| `I64_TO_F32_S` | `{}` | ✅ | Signed i64 → f32. |
| `I64_TO_F32_U` | `{}` | ✅ | Unsigned i64 → f32. |
| `I64_TO_F64_S` | `{}` | ✅ | Signed i64 → f64. |
| `I64_TO_F64_U` | `{}` | ✅ | Unsigned i64 → f64. |

## f32 Operations

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `F32_CONST` | `{4}` | ✅ | Push immediate f32 (4-byte IEEE-754 bits). |
| `F32_ADD` `F32_SUB` `F32_MUL` `F32_DIV` | `{}` | ✅ | Standard float arithmetic. |
| `F32_EQ` `F32_NE` `F32_LT` `F32_GT` `F32_LE` `F32_GE` | `{}` | ✅ | Float comparisons. Result is `I32(0)` or `I32(1)`. |
| `F32_TO_I32_S` `F32_TO_I32_U` | `{}` | ✅ | Truncate f32 to signed/unsigned i32. |
| `F32_TO_I64_S` `F32_TO_I64_U` | `{}` | ✅ | Truncate f32 to signed/unsigned i64. |
| `F32_TO_F64` | `{}` | ✅ | Widen f32 to f64. |

## f64 Operations

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `F64_CONST` | `{8}` | ✅ | Push immediate f64 (8-byte IEEE-754 bits). |
| `F64_ADD` `F64_SUB` `F64_MUL` `F64_DIV` | `{}` | ✅ | Standard float arithmetic. |
| `F64_EQ` `F64_NE` `F64_LT` `F64_GT` `F64_LE` `F64_GE` | `{}` | ✅ | Float comparisons. Result is `I32`. |
| `F64_TO_I32_S` `F64_TO_I32_U` | `{}` | ✅ | Truncate f64 to i32. |
| `F64_TO_I64_S` `F64_TO_I64_U` | `{}` | ✅ | Truncate f64 to i64. |
| `F64_TO_F32` | `{}` | ✅ | Narrow f64 to f32. |

## String Operations

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `STRING_NEW_UTF32` | `{}` | ⬜ | Pop `KindRef` → `types.I32Array`. Create a `types.String` from UTF-32 codepoints. |
| `STRING_LEN` | `{}` | ⬜ | Pop string ref. Push `I32` length (number of codepoints). |
| `STRING_CONCAT` | `{}` | ⬜ | Pop two string refs. Push new concatenated string. |
| `STRING_EQ` … `STRING_GE` | `{}` | ⬜ | Compare two string refs lexicographically. Push `I32(0/1)`. |
| `STRING_ENCODE_UTF32` | `{}` | ⬜ | Pop string ref. Push `KindRef` → `types.I32Array` of UTF-32 codepoints. |

## Array Operations

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `ARRAY_NEW` | `{2}` | ⬜ | Pop element value and I32 count. Create typed array of `count` elements. u16 operand is type index. |
| `ARRAY_NEW_DEFAULT` | `{2}` | ⬜ | Pop I32 count. Create zero-initialized typed array. |
| `ARRAY_LEN` | `{}` | ⬜ | Pop array ref. Push `I32` element count. |
| `ARRAY_GET` | `{}` | ⬜ | Pop index (I32) and array ref. Push element at index. Panics `ErrIndexOutOfRange`. |
| `ARRAY_SET` | `{}` | ⬜ | Pop value, index, array ref. Store element. |
| `ARRAY_FILL` | `{}` | ⬜ | Pop fill-value, count (I32), offset (I32), array ref. Fill `count` elements starting at `offset`. |
| `ARRAY_COPY` | `{}` | ⬜ | Pop count, src-offset, src-array, dst-offset, dst-array. Copy elements. |

## Struct Operations

| Opcode | Widths | JIT | Description |
|---|---|---|---|
| `STRUCT_NEW` | `{2}` | ⬜ | Pop one value per field. Create struct of type at u16 index. |
| `STRUCT_NEW_DEFAULT` | `{2}` | ⬜ | Create zero-initialized struct. |
| `STRUCT_GET` | `{}` | ⬜ | Pop field index (I32) and struct ref. Push field value. |
| `STRUCT_SET` | `{}` | ⬜ | Pop value, field index, struct ref. Store field. |
