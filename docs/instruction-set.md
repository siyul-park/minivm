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
| ◐ | partially JIT compiled, guarded, or kept as a framed fallback boundary |
| ⬜ | threaded-only |

## Operand Width Notation

Declared in `instr/type.go`.

| Notation | Meaning |
|---|---|
| `{n}` | fixed `n`-byte operand |
| `{-n, n}` | count byte + `count × n`-byte values |

Examples: `{2}` = one u16; branch opcodes interpret it as i16. `{-2, 2}` = count byte + repeated u16 operands.

## Branch Offsets

Branch operands are relative to instruction end:

```text
target = instruction_start + instruction_width + operand
```

Offsets are signed 16-bit values encoded little-endian. `BR 5` skips 5 bytes past the 3-byte `BR`; `BR 0` is fall-through; `BR -3` jumps back to the branch instruction.

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
| `BR` | `{2}` | `→` | ◐ | Unconditional relative jump. Trace JIT records the observed path and exits or continues through learned branch traces. |
| `BR_IF` | `{2}` | `cond →` | ◐ | Jump if `cond ≠ 0`, else fall through. JIT only for simple stack shapes. |
| `BR_TABLE` | `{-2, 2}` | `index →` | ◐ | Jump table; negative or out-of-range index uses default target. JIT only for simple stack shapes. |
| `CALL` | `{}` | `fn →` | ◐ | Call `*Function`, `*HostFunction`, or `*Closure`; trace JIT lowers observed direct calls, small same-arity function-value indirect dispatches, and eligible closure-body calls to native `BL`. Host calls and misses fall back. |
| `RETURN` | `{}` | `→` | ◐ | Return from current frame; trace JIT lowers entry returns and stitches inlined callee returns. |
| `RETURN_CALL` | `{}` | `args… fn →` | ◐ | Tail call: pops args + funcref like `CALL`, but reuses the current frame so tail recursion runs in constant frame depth. Above the entry frame the frame is replaced in place; at the entry frame a new frame is pushed (callee returns to the entry frame normally). Target must be a `*Function` or `*Closure`; a host-function target is invoked in place and its results returned. Result arity should match the current function's. Trace JIT lowers plain-function targets: a tail call back to the trace anchor becomes a native loop back-edge (self/mutual recursion in constant depth), a tail call to another function morphs the frame in place. Host and closure targets fall back. |

## Variables

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `GLOBAL_GET` | `{2}` | `→ x` | ◐ | Push global at u16 index. JIT supports in-range numeric, dynamic `ref`, and guarded ref-counted heap values; heap-promoted `i64` exits to threaded execution. |
| `GLOBAL_SET` | `{2}` | `x →` | ◐ | Store global at u16 index. JIT supports numeric, dynamic `ref`, and guarded ref-counted heap values. |
| `GLOBAL_TEE` | `{2}` | `x → x` | ◐ | Store global and keep value. JIT supports the same guarded paths as `GLOBAL_SET`. |
| `LOCAL_GET` | `{1}` | `→ x` | ◐ | Push u8 local relative to frame base. JIT supports params/locals including dynamic `ref` slots; heap-promoted `i64` exits to threaded execution. |
| `LOCAL_SET` | `{1}` | `x →` | ◐ | Store u8 local. JIT supports numeric, dynamic `ref`, and guarded ref-counted heap values. |
| `LOCAL_TEE` | `{1}` | `x → x` | ◐ | Store local and keep value. JIT supports the same guarded paths as `LOCAL_SET`. |
| `CONST_GET` | `{2}` | `→ x` | ◐ | Push u16 constant. JIT supports boxed numeric constants and function constants used by direct/indirect calls; ordinary heap ref constants stay threaded. |

## References

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `REF_NULL` | `{}` | `→ ref` | ✅ | Push `BoxedNull`, heap index `0`. |
| `REF_IS_NULL` | `{}` | `ref → i32` | ✅ | Push `I32(1)` if null, else `I32(0)`. |
| `REF_EQ` | `{}` | `a b → i32` | ✅ | Push `I32(1)` if refs point to same heap index. |
| `REF_NE` | `{}` | `a b → i32` | ✅ | Push `I32(1)` if refs differ. |
| `REF_TEST` | `{2}` | `any → i32` | ⬜ | Push `I32(1)` if the value matches the type at u16 index. Accepts any operand: a `KindRef` is matched against the heap object's `Type()`; a primitive is matched against its own kind. |
| `REF_CAST` | `{2}` | `any → any` | ⬜ | Trap with `ErrTypeMismatch` if the value does not cast to the type at u16 index. Accepts both `KindRef` and primitive operands (primitives use `Boxed.Type()`). |
| `REF_NEW` | `{}` | `x → ref` | ◐ | Box a non-ref scalar (`I32/I64/F32/F64`) onto the heap as a mutable cell; trap `ErrTypeMismatch` on a ref operand. JIT keeps framed entries by exiting locally to the threaded handler. |
| `REF_GET` | `{}` | `ref → x` | ◐ | Load the scalar held by a cell; trap `ErrTypeMismatch` if the target is not a scalar. Consumes (releases) the ref. JIT has native fast paths for `I32`, `F32`, and `F64`; `I64` cells fall back. |
| `REF_SET` | `{}` | `ref x →` | ◐ | Overwrite a cell's scalar; trap `ErrTypeMismatch` if `x` is a ref. Consumes (releases) the ref. JIT keeps framed entries by exiting locally to the threaded handler. |

A `ref`-typed slot is the VM's dynamic ("any") type: it holds any `Boxed` — an inline primitive or a `KindRef` — and `REF_TEST`/`REF_CAST` recover the runtime type. See [value-representation.md](value-representation.md#dynamic-any-values).

## Closures

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `CLOSURE_NEW` | `{}` | `upval1 … upvalN fn → closure` | ◐ | Pop the `*Function` template (top of stack, like `call`), read `N = len(fn.Captures)`, pop N upvalues below it, and push a `*Closure` capturing them. Ownership of `fn` and the upvalues transfers into the closure. JIT keeps framed entries by exiting locally to the threaded handler. |
| `UPVAL_GET` | `{1}` | `→ x` | ◐ | Push the closure upvalue at u8 index; traps `ErrSegmentationFault` outside a closure frame or out of range. Closure-body JIT supports guarded upvalue loads. |
| `UPVAL_SET` | `{1}` | `x →` | ◐ | Store into the closure upvalue at u8 index (persists across calls to the same closure); same trap conditions as `UPVAL_GET`. Closure-body JIT supports guarded ref-counted stores. |

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
| `I32_CLZ` | `{}` | `x → i32` | ✅ | Count leading zero bits (`32` if `x == 0`). |
| `I32_CTZ` | `{}` | `x → i32` | ✅ | Count trailing zero bits (`32` if `x == 0`). |
| `I32_POPCNT` | `{}` | `x → i32` | ✅ | Count set bits. |
| `I32_ROTL` | `{}` | `a b → i32` | ✅ | Rotate `a` left by `b` (modulo 32). |
| `I32_ROTR` | `{}` | `a b → i32` | ✅ | Rotate `a` right by `b` (modulo 32). |
| `I32_EXTEND8_S` | `{}` | `x → i32` | ✅ | Sign-extend low 8 bits to i32. |
| `I32_EXTEND16_S` | `{}` | `x → i32` | ✅ | Sign-extend low 16 bits to i32. |
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
| `I32_REINTERPRET_F32` | `{}` | `f32 → i32` | ✅ | Reinterpret f32 bit pattern as i32 (no conversion). |

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
| `I64_XOR` | `{}` | `a b → i64` | ✅ | Bitwise XOR. |
| `I64_AND` | `{}` | `a b → i64` | ✅ | Bitwise AND. |
| `I64_OR` | `{}` | `a b → i64` | ✅ | Bitwise OR. |
| `I64_CLZ` | `{}` | `x → i64` | ✅ | Count leading zero bits (`64` if `x == 0`). |
| `I64_CTZ` | `{}` | `x → i64` | ✅ | Count trailing zero bits (`64` if `x == 0`). |
| `I64_POPCNT` | `{}` | `x → i64` | ✅ | Count set bits. |
| `I64_ROTL` | `{}` | `a b → i64` | ✅ | Rotate `a` left by `b` (modulo 64). |
| `I64_ROTR` | `{}` | `a b → i64` | ✅ | Rotate `a` right by `b` (modulo 64). |
| `I64_EXTEND8_S` | `{}` | `x → i64` | ✅ | Sign-extend low 8 bits to i64. |
| `I64_EXTEND16_S` | `{}` | `x → i64` | ✅ | Sign-extend low 16 bits to i64. |
| `I64_EXTEND32_S` | `{}` | `x → i64` | ✅ | Sign-extend low 32 bits to i64. |
| `I64_EQZ` | `{}` | `x → i32` | ✅ | Push `I32(1)` if zero. |
| `I64_EQ` … `I64_GE_U` | `{}` | `a b → i32` | ✅ | Same semantics as i32 comparisons. |
| `I64_TO_I32` | `{}` | `i64 → i32` | ✅ | Truncate to low 32 bits. |
| `I64_TO_F32_S` | `{}` | `i64 → f32` | ✅ | Convert signed i64 to f32. |
| `I64_TO_F32_U` | `{}` | `i64 → f32` | ✅ | Convert unsigned i64 to f32. |
| `I64_TO_F64_S` | `{}` | `i64 → f64` | ✅ | Convert signed i64 to f64. |
| `I64_TO_F64_U` | `{}` | `i64 → f64` | ✅ | Convert unsigned i64 to f64. |
| `I64_REINTERPRET_F64` | `{}` | `f64 → i64` | ✅ | Reinterpret f64 bit pattern as i64 (no conversion). |

## f32 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `F32_CONST` | `{4}` | `→ f32` | ✅ | Push immediate IEEE-754 f32. |
| `F32_ADD` | `{}` | `a b → f32` | ✅ | Floating-point addition. |
| `F32_SUB` | `{}` | `a b → f32` | ✅ | Floating-point subtraction. |
| `F32_MUL` | `{}` | `a b → f32` | ✅ | Floating-point multiplication. |
| `F32_DIV` | `{}` | `a b → f32` | ✅ | Floating-point division. |
| `F32_ABS` | `{}` | `x → f32` | ✅ | Absolute value (clears sign bit). |
| `F32_NEG` | `{}` | `x → f32` | ✅ | Negate (flips sign bit, incl. NaN). |
| `F32_SQRT` | `{}` | `x → f32` | ✅ | Square root. |
| `F32_CEIL` | `{}` | `x → f32` | ✅ | Round toward +∞. |
| `F32_FLOOR` | `{}` | `x → f32` | ✅ | Round toward −∞. |
| `F32_TRUNC` | `{}` | `x → f32` | ✅ | Round toward zero. |
| `F32_NEAREST` | `{}` | `x → f32` | ✅ | Round to nearest, ties to even. |
| `F32_MIN` | `{}` | `a b → f32` | ✅ | Minimum; NaN propagates, `min(-0,+0)=-0`. |
| `F32_MAX` | `{}` | `a b → f32` | ✅ | Maximum; NaN propagates, `max(-0,+0)=+0`. |
| `F32_COPYSIGN` | `{}` | `a b → f32` | ✅ | Magnitude of `a` with sign of `b`. |
| `F32_EQ` … `F32_GE` | `{}` | `a b → i32` | ✅ | Floating-point comparisons. |
| `F32_TO_I32_S` | `{}` | `f32 → i32` | ✅ | Truncate toward zero to signed i32, saturating (NaN→0, out-of-range→nearest bound). |
| `F32_TO_I32_U` | `{}` | `f32 → i32` | ✅ | Truncate toward zero to unsigned i32, saturating (NaN/negative→0, overflow→`u32` max). |
| `F32_TO_I64_S` | `{}` | `f32 → i64` | ✅ | Truncate toward zero to signed i64, saturating (NaN→0, out-of-range→nearest bound). |
| `F32_TO_I64_U` | `{}` | `f32 → i64` | ✅ | Truncate toward zero to unsigned i64, saturating (NaN/negative→0, overflow→`u64` max). |
| `F32_TO_F64` | `{}` | `f32 → f64` | ✅ | Widen f32 to f64. |
| `F32_REINTERPRET_I32` | `{}` | `i32 → f32` | ✅ | Reinterpret i32 bit pattern as f32 (no conversion). |

## f64 Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `F64_CONST` | `{8}` | `→ f64` | ✅ | Push immediate IEEE-754 f64. |
| `F64_ADD` | `{}` | `a b → f64` | ✅ | Floating-point addition. |
| `F64_SUB` | `{}` | `a b → f64` | ✅ | Floating-point subtraction. |
| `F64_MUL` | `{}` | `a b → f64` | ✅ | Floating-point multiplication. |
| `F64_DIV` | `{}` | `a b → f64` | ✅ | Floating-point division. |
| `F64_ABS` | `{}` | `x → f64` | ✅ | Absolute value (clears sign bit). |
| `F64_NEG` | `{}` | `x → f64` | ✅ | Negate (flips sign bit, incl. NaN). |
| `F64_SQRT` | `{}` | `x → f64` | ✅ | Square root. |
| `F64_CEIL` | `{}` | `x → f64` | ✅ | Round toward +∞. |
| `F64_FLOOR` | `{}` | `x → f64` | ✅ | Round toward −∞. |
| `F64_TRUNC` | `{}` | `x → f64` | ✅ | Round toward zero. |
| `F64_NEAREST` | `{}` | `x → f64` | ✅ | Round to nearest, ties to even. |
| `F64_MIN` | `{}` | `a b → f64` | ✅ | Minimum; NaN propagates, `min(-0,+0)=-0`. |
| `F64_MAX` | `{}` | `a b → f64` | ✅ | Maximum; NaN propagates, `max(-0,+0)=+0`. |
| `F64_COPYSIGN` | `{}` | `a b → f64` | ✅ | Magnitude of `a` with sign of `b`. |
| `F64_EQ` … `F64_GE` | `{}` | `a b → i32` | ✅ | Floating-point comparisons. |
| `F64_TO_I32_S` | `{}` | `f64 → i32` | ✅ | Truncate toward zero to signed i32, saturating (NaN→0, out-of-range→nearest bound). |
| `F64_TO_I32_U` | `{}` | `f64 → i32` | ✅ | Truncate toward zero to unsigned i32, saturating (NaN/negative→0, overflow→`u32` max). |
| `F64_TO_I64_S` | `{}` | `f64 → i64` | ✅ | Truncate toward zero to signed i64, saturating (NaN→0, out-of-range→nearest bound). |
| `F64_TO_I64_U` | `{}` | `f64 → i64` | ✅ | Truncate toward zero to unsigned i64, saturating (NaN/negative→0, overflow→`u64` max). |
| `F64_TO_F32` | `{}` | `f64 → f32` | ✅ | Narrow f64 to f32. |
| `F64_REINTERPRET_I64` | `{}` | `i64 → f64` | ⬜ | Reinterpret i64 bit pattern as f64 (no conversion). |

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
| `ARRAY_NEW` | `{2}` | `value count → array` | ◐ | Create typed array filled with `value`. JIT keeps framed entries by exiting locally to the threaded handler. |
| `ARRAY_NEW_DEFAULT` | `{2}` | `count → array` | ◐ | Create zero-initialized typed array. JIT keeps framed entries by exiting locally to the threaded handler. |
| `ARRAY_LEN` | `{}` | `array → i32` | ◐ | Push element count. JIT has native fast paths for VM arrays. |
| `ARRAY_GET` | `{}` | `array index → value` | ◐ | Load element; trap `ErrIndexOutOfRange` on invalid index. JIT has native fast paths for `[]i8`, `[]i32`, `[]f32`, `[]f64`, and generic `[]ref`; `[]i64` falls back when boxing would be needed. |
| `ARRAY_SET` | `{}` | `array index value →` | ◐ | Store element. JIT keeps framed entries by exiting locally to the threaded handler. |
| `ARRAY_FILL` | `{}` | `array offset count value →` | ◐ | Fill range with repeated value. JIT keeps framed entries by exiting locally to the threaded handler. |
| `ARRAY_COPY` | `{}` | `dst dstOffset src srcOffset count →` | ◐ | Copy elements between arrays. JIT keeps framed entries by exiting locally to the threaded handler. |

`[]i8` arrays (binary blobs) share these opcodes. Stack values are `i32`; `ARRAY_GET` zero-extends a byte to `BoxI32(0..255)`, and `ARRAY_SET`/`ARRAY_FILL` narrow via low-byte truncation (`int8(val.I32())`). No overflow trap on narrowing — the storage cell holds raw bits.

## Struct Operations

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `STRUCT_NEW` | `{2}` | `fields → struct` | ◐ | Create struct from field values. JIT keeps framed entries by exiting locally to the threaded handler. |
| `STRUCT_NEW_DEFAULT` | `{2}` | `→ struct` | ◐ | Create zero-initialized struct. JIT keeps framed entries by exiting locally to the threaded handler. |
| `STRUCT_GET` | `{}` | `struct index → value` | ◐ | Load struct field. JIT has native fast paths for VM `*types.Struct` fields that are `i32`, `f32`, `f64`, or `ref`; `i64` boxing and `HostObject` fall back. |
| `STRUCT_SET` | `{}` | `struct index value →` | ◐ | Store struct field. JIT keeps framed entries by exiting locally to the threaded handler. |

## Map Operations

Map keys use primitive value identity for `i32`, `i64`, `f32`, and `f64`; all ref-typed keys use heap ref identity. Missing keys read as element zero value. `MAP_LOOKUP` also returns `I32(1)` for present and `I32(0)` for missing.

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `MAP_NEW` | `{2}` | `k1 v1 ... kn vn count → map` | ◐ | Create typed map from key/value pairs; later duplicate keys overwrite earlier values. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_NEW_DEFAULT` | `{2}` | `capacity → map` | ◐ | Create empty typed map with capacity hint. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_LEN` | `{}` | `map → i32` | ◐ | Push entry count. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_GET` | `{}` | `map key → value` | ◐ | Load value or element zero value when key is missing. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_LOOKUP` | `{}` | `map key → value ok` | ◐ | Load value plus presence flag. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_SET` | `{}` | `map key value →` | ◐ | Insert or replace entry. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_DELETE` | `{}` | `map key →` | ◐ | Delete entry; missing key is a no-op. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_CLEAR` | `{}` | `map →` | ◐ | Delete all entries. JIT keeps framed entries by exiting locally to the threaded handler. |
| `MAP_KEYS` | `{}` | `map → array` | ⬜ | Snapshot keys into a new `[]K` array (`K` = map key type), in unspecified order. Enables guest map iteration with `ARRAY_LEN`/`ARRAY_GET` + `MAP_GET`. |

## Coroutines

A function whose body contains `YIELD` is a coroutine-function: its `CALL` allocates a `Coroutine` handle and runs the body until the first `YIELD` (suspend) or `RETURN` (finish), yielding the handle instead of plain returns. `RESUME` re-enters a suspended handle. A `YIELD` in the entry frame (`fp == 1`) is an interpreter escape: it panics the private `errYield`, `Run` returns the exported `ErrYield` without losing state, and the next `Run` resumes after the `YIELD`.

| Opcode | Widths | Stack | JIT | Description |
|---|---|---|---|---|
| `YIELD` | `{}` | `value → result` | ◐ | Suspend the current coroutine frame, capturing `value` into the handle and unwinding to the caller; `RESUME` later delivers its in-value as `result`. In the entry frame it escapes via `ErrYield`. JIT records a `YIELD` reached in the trace's anchor frame as a terminal and lowers it to an unconditional deopt that runs the threaded suspend; a `YIELD` inside an inlined callee frame aborts the trace. |
| `RESUME` | `{}` | `coro in → coro` | ◐ | Resume a suspended handle, delivering `in` as the pending `YIELD`'s result, running to the next `YIELD`/`RETURN`, and pushing the handle back; traps `ErrCoroutineDone` if already finished. JIT lowers an anchor-frame `RESUME` to a terminal deopt that runs the threaded resume. |
| `CORO_DONE` | `{}` | `coro → i32` | ◐ | Push `I32(1)` if the handle has finished, else `I32(0)`; does not release the handle. JIT has a native fast path behind an itab guard (reads the `done` byte). |
| `CORO_VALUE` | `{}` | `coro → value` | ◐ | Push the handle's last yielded or returned value and release the handle. JIT has a native fast path behind an itab guard (loads the boxed `value`, retains it, releases the handle). |
