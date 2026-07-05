# Instruction Set Reference

Complete opcode reference for minivm.

Use this document as the semantic source of truth when adding, changing, debugging, or testing bytecode instructions.

## Agent Fast Path

| Task                     | Check                                                                            |
| ------------------------ | -------------------------------------------------------------------------------- |
| Add an opcode            | append to `instr/opcode.go`; never insert between existing opcodes               |
| Change operands          | update `instr/type.go`; handler widths must match exactly                        |
| Change threaded behavior | update `interp/threaded.go`                                                      |
| Change JIT behavior      | update `interp/jit_arm64.go`                                                     |
| Change semantics         | update this document, verifier/tests in `instr/`, and runtime tests in `interp/` |
| Add optimized behavior   | keep threaded, verifier, and JIT semantics aligned                               |

Default design rule: if two opcode designs provide the same behavior, choose the simpler one. Prefer fewer opcodes, fewer special cases, and short standard names. Add a new opcode only when composition is meaningfully worse.

## Core Rules

All opcodes are one byte. Operands are fixed-width or length-prefixed.

Unless stated otherwise:

* operands are little-endian
* stack operands pop right-to-left
* comparisons push boolean values
* boolean runtime kind is `i1`
* tables write boolean results as `i32` for readability
* ref-exposing instructions retain refs
* ref-overwriting instructions release replaced refs
* runtime traps panic internally and are recovered by `interp.Run`

## JIT Status

| Status | Meaning                                                  |
| ------ | -------------------------------------------------------- |
| ✅      | lowered to native ARM64                                  |
| ◐      | partially lowered, guarded, terminal, or framed fallback |
| ⬜      | threaded-only                                            |

## Operand Widths

Operand widths are declared in `instr/type.go`.

| Notation  | Meaning                                   |
| --------- | ----------------------------------------- |
| `{}`      | no operands                               |
| `{n}`     | one fixed `n`-byte operand                |
| `{-n, n}` | count byte plus `count × n`-byte operands |

Examples:

* `{2}` means one `u16` operand
* branch opcodes interpret `{2}` as signed `i16`
* `{-2, 2}` means `count` followed by repeated `u16` values

## Operand Kinds

`i32`, `i8`, and `i1` share the same 32-bit stack representation. This follows the JVM-style computational type model.

Consequences:

* an opcode accepting `i32` also accepts `i8` and `i1` by representation
* comparisons and boolean-producing opcodes are listed as `→ i32`, but box through `i1`
* `i32.and`, `i32.or`, and `i32.xor` preserve narrow width when both operands share it
* other arithmetic widens narrow operands to `i32`

## Branch Offsets

Branch operands are relative to the end of the branch instruction.

```text id="pp2jcv"
target = instruction_start + instruction_width + operand
```

Offsets are signed 16-bit little-endian values.

Examples:

* `BR 5` skips 5 bytes after the 3-byte branch instruction
* `BR 0` falls through
* `BR -3` jumps back to the branch instruction itself

## Stack Manipulation

| Opcode        | Widths | Stack          | JIT | Description                                                                                     |
| ------------- | ------ | -------------- | --- | ----------------------------------------------------------------------------------------------- |
| `NOP`         | `{}`   | `→`            | ✅   | No-op. Threaded execution may collapse NOP runs; `WithTick(1)` preserves per-instruction hooks. |
| `DROP`        | `{}`   | `x →`          | ✅   | Discard the top value.                                                                          |
| `DUP`         | `{}`   | `x → x x`      | ✅   | Duplicate the top value.                                                                        |
| `SWAP`        | `{}`   | `a b → b a`    | ✅   | Swap the top two values.                                                                        |
| `SELECT`      | `{}`   | `a b cond → x` | ✅   | Push `a` if `cond != 0`, otherwise `b`.                                                         |
| `UNREACHABLE` | `{}`   | `→`            | ⬜   | Trap with `ErrUnreachableExecuted`. Useful as dead-code filler before DCE.                      |

## Control Flow

| Opcode        | Widths    | Stack        | JIT | Description                                                                                                                                 |
| ------------- | --------- | ------------ | --- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `BR`          | `{2}`     | `→`          | ◐   | Unconditional relative jump.                                                                                                                |
| `BR_IF`       | `{2}`     | `cond →`     | ◐   | Jump if `cond != 0`; otherwise fall through.                                                                                                |
| `BR_TABLE`    | `{-2, 2}` | `index →`    | ◐   | Jump table. Negative or out-of-range index uses the default target.                                                                         |
| `CALL`        | `{}`      | `fn →`       | ◐   | Call `*Function`, `*HostFunction`, or `*Closure`. JIT supports observed direct calls, selected indirect calls, and eligible closure calls.  |
| `RETURN`      | `{}`      | `→`          | ◐   | Return from the current frame.                                                                                                              |
| `RETURN_CALL` | `{}`      | `args… fn →` | ◐   | Tail call. Reuses the current frame above the entry frame; entry-frame calls push a normal callee frame. Host targets are invoked in place. |

`RETURN_CALL` is the tail-call primitive. Prefer it over adding separate recursion-specific opcodes.

## Variables

`LOCAL_*` access slots in the current frame by `u8` index. The top-level program may also have locals through `Program.Locals`, `program.WithLocals`, or `Builder.Locals`.

| Opcode       | Widths | Stack   | JIT | Description                                                |
| ------------ | ------ | ------- | --- | ---------------------------------------------------------- |
| `GLOBAL_GET` | `{2}`  | `→ x`   | ◐   | Push global at `u16` index.                                |
| `GLOBAL_SET` | `{2}`  | `x →`   | ◐   | Store global. Replaces and releases the old ref if needed. |
| `GLOBAL_TEE` | `{2}`  | `x → x` | ◐   | Store global and keep the value on stack.                  |
| `LOCAL_GET`  | `{1}`  | `→ x`   | ◐   | Push local at `u8` index.                                  |
| `LOCAL_SET`  | `{1}`  | `x →`   | ◐   | Store local. Replaces and releases the old ref if needed.  |
| `LOCAL_TEE`  | `{1}`  | `x → x` | ◐   | Store local and keep the value on stack.                   |
| `CONST_GET`  | `{2}`  | `→ x`   | ◐   | Push constant at `u16` index.                              |

JIT supports common numeric, dynamic-ref, and guarded ref-counted paths. Heap-promoted `i64` values and ordinary heap ref constants may fall back.

## References

A `ref` slot is the VM dynamic value type. It can hold either an inline primitive or a heap reference.

| Opcode        | Widths | Stack       | JIT | Description                                                                  |
| ------------- | ------ | ----------- | --- | ---------------------------------------------------------------------------- |
| `REF_NULL`    | `{}`   | `→ ref`     | ✅   | Push null ref, heap index `0`.                                               |
| `REF_IS_NULL` | `{}`   | `ref → i32` | ✅   | Push true if the ref is null.                                                |
| `REF_EQ`      | `{}`   | `a b → i32` | ✅   | Push true if refs point to the same heap index.                              |
| `REF_NE`      | `{}`   | `a b → i32` | ✅   | Push true if refs differ.                                                    |
| `REF_TEST`    | `{2}`  | `any → i32` | ⬜   | Push true if value matches type at `u16` index. Accepts refs and primitives. |
| `REF_CAST`    | `{2}`  | `any → any` | ⬜   | Trap with `ErrTypeMismatch` if value cannot cast to type at `u16` index.     |
| `REF_NEW`     | `{}`   | `x → ref`   | ◐   | Box a non-ref scalar as a mutable heap cell. Traps on ref operands.          |
| `REF_GET`     | `{}`   | `ref → x`   | ◐   | Load scalar from a cell and release the ref.                                 |
| `REF_SET`     | `{}`   | `ref x →`   | ◐   | Store scalar into a cell and release the ref. Traps if `x` is a ref.         |

Use `REF_TEST` and `REF_CAST` to recover dynamic runtime types.

## Closures

| Opcode        | Widths | Stack                          | JIT | Description                                                                                          |
| ------------- | ------ | ------------------------------ | --- | ---------------------------------------------------------------------------------------------------- |
| `CLOSURE_NEW` | `{}`   | `upval1 … upvalN fn → closure` | ◐   | Create a closure from a function template and captured values. Ownership transfers into the closure. |
| `UPVAL_GET`   | `{1}`  | `→ x`                          | ◐   | Push closure upvalue at `u8` index. Traps outside closure frames or out of range.                    |
| `UPVAL_SET`   | `{1}`  | `x →`                          | ◐   | Store closure upvalue at `u8` index. Traps outside closure frames or out of range.                   |

The number of captured values comes from `len(fn.Captures)`.

## Integer Operations

Integer operations are grouped by width. Most are fully JIT-supported.

### i32

| Opcode                         | Widths | Stack       | JIT | Description                                                    |
| ------------------------------ | ------ | ----------- | --- | -------------------------------------------------------------- |
| `I32_CONST`                    | `{4}`  | `→ i32`     | ✅   | Push immediate `i32`.                                          |
| `I32_ADD` / `SUB` / `MUL`      | `{}`   | `a b → i32` | ✅   | Signed arithmetic.                                             |
| `I32_DIV_S` / `DIV_U`          | `{}`   | `a b → i32` | ✅   | Signed or unsigned division. Traps on zero divisor.            |
| `I32_REM_S` / `REM_U`          | `{}`   | `a b → i32` | ✅   | Signed or unsigned remainder. Traps on zero divisor.           |
| `I32_SHL`                      | `{}`   | `a b → i32` | ✅   | Left shift; amount uses low 5 bits.                            |
| `I32_SHR_S` / `SHR_U`          | `{}`   | `a b → i32` | ✅   | Arithmetic or logical right shift.                             |
| `I32_AND` / `OR` / `XOR`       | `{}`   | `a b → i32` | ✅   | Bitwise operation. Preserves shared narrow kind for `i8`/`i1`. |
| `I32_CLZ` / `CTZ`              | `{}`   | `x → i32`   | ✅   | Count leading or trailing zero bits; returns `32` for zero.    |
| `I32_POPCNT`                   | `{}`   | `x → i32`   | ✅   | Count set bits.                                                |
| `I32_ROTL` / `ROTR`            | `{}`   | `a b → i32` | ✅   | Rotate modulo 32.                                              |
| `I32_EXTEND8_S` / `EXTEND16_S` | `{}`   | `x → i32`   | ✅   | Sign-extend low 8 or 16 bits.                                  |
| `I32_EQZ`                      | `{}`   | `x → i32`   | ✅   | Push true if zero.                                             |
| `I32_EQ` / `NE`                | `{}`   | `a b → i32` | ✅   | Equality or inequality.                                        |
| `I32_LT_S` / `LT_U`            | `{}`   | `a b → i32` | ✅   | Signed or unsigned less-than.                                  |
| `I32_GT_S` / `GT_U`            | `{}`   | `a b → i32` | ✅   | Signed or unsigned greater-than.                               |
| `I32_LE_S` / `LE_U`            | `{}`   | `a b → i32` | ✅   | Signed or unsigned less-or-equal.                              |
| `I32_GE_S` / `GE_U`            | `{}`   | `a b → i32` | ✅   | Signed or unsigned greater-or-equal.                           |
| `I32_TO_I64_S` / `TO_I64_U`    | `{}`   | `i32 → i64` | ✅   | Sign-extend or zero-extend to `i64`.                           |
| `I32_TO_F32_S` / `TO_F32_U`    | `{}`   | `i32 → f32` | ✅   | Convert signed or unsigned `i32` to `f32`.                     |
| `I32_TO_F64_S` / `TO_F64_U`    | `{}`   | `i32 → f64` | ✅   | Convert signed or unsigned `i32` to `f64`.                     |
| `I32_REINTERPRET_F32`          | `{}`   | `f32 → i32` | ✅   | Reinterpret bits without conversion.                           |

### i64

| Opcode                                        | Widths | Stack       | JIT | Description                                                        |
| --------------------------------------------- | ------ | ----------- | --- | ------------------------------------------------------------------ |
| `I64_CONST`                                   | `{8}`  | `→ i64`     | ✅   | Push immediate `i64`; non-inline values may spill to heap storage. |
| `I64_ADD` / `SUB` / `MUL`                     | `{}`   | `a b → i64` | ✅   | Signed arithmetic.                                                 |
| `I64_DIV_S` / `DIV_U`                         | `{}`   | `a b → i64` | ✅   | Signed or unsigned division. Traps on zero divisor.                |
| `I64_REM_S` / `REM_U`                         | `{}`   | `a b → i64` | ✅   | Signed or unsigned remainder. Traps on zero divisor.               |
| `I64_SHL`                                     | `{}`   | `a b → i64` | ✅   | Left shift; amount uses low 6 bits.                                |
| `I64_SHR_S` / `SHR_U`                         | `{}`   | `a b → i64` | ✅   | Arithmetic or logical right shift.                                 |
| `I64_AND` / `OR` / `XOR`                      | `{}`   | `a b → i64` | ✅   | Bitwise operation.                                                 |
| `I64_CLZ` / `CTZ`                             | `{}`   | `x → i64`   | ✅   | Count leading or trailing zero bits; returns `64` for zero.        |
| `I64_POPCNT`                                  | `{}`   | `x → i64`   | ✅   | Count set bits.                                                    |
| `I64_ROTL` / `ROTR`                           | `{}`   | `a b → i64` | ✅   | Rotate modulo 64.                                                  |
| `I64_EXTEND8_S` / `EXTEND16_S` / `EXTEND32_S` | `{}`   | `x → i64`   | ✅   | Sign-extend low 8, 16, or 32 bits.                                 |
| `I64_EQZ`                                     | `{}`   | `x → i32`   | ✅   | Push true if zero.                                                 |
| `I64_EQ` / `NE`                               | `{}`   | `a b → i32` | ✅   | Equality or inequality.                                            |
| `I64_LT_S` / `LT_U`                           | `{}`   | `a b → i32` | ✅   | Signed or unsigned less-than.                                      |
| `I64_GT_S` / `GT_U`                           | `{}`   | `a b → i32` | ✅   | Signed or unsigned greater-than.                                   |
| `I64_LE_S` / `LE_U`                           | `{}`   | `a b → i32` | ✅   | Signed or unsigned less-or-equal.                                  |
| `I64_GE_S` / `GE_U`                           | `{}`   | `a b → i32` | ✅   | Signed or unsigned greater-or-equal.                               |
| `I64_TO_I32`                                  | `{}`   | `i64 → i32` | ✅   | Truncate to low 32 bits.                                           |
| `I64_TO_F32_S` / `TO_F32_U`                   | `{}`   | `i64 → f32` | ✅   | Convert signed or unsigned `i64` to `f32`.                         |
| `I64_TO_F64_S` / `TO_F64_U`                   | `{}`   | `i64 → f64` | ✅   | Convert signed or unsigned `i64` to `f64`.                         |
| `I64_REINTERPRET_F64`                         | `{}`   | `f64 → i64` | ✅   | Reinterpret bits without conversion.                               |

## Floating-Point Operations

Floating-point values use IEEE-754 semantics unless stated otherwise.

### f32

| Opcode                                      | Widths | Stack       | JIT | Description                                                    |
| ------------------------------------------- | ------ | ----------- | --- | -------------------------------------------------------------- |
| `F32_CONST`                                 | `{4}`  | `→ f32`     | ✅   | Push immediate `f32`.                                          |
| `F32_ADD` / `SUB` / `MUL` / `DIV`           | `{}`   | `a b → f32` | ✅   | Floating-point arithmetic.                                     |
| `F32_REM`                                   | `{}`   | `a b → f32` | ◐   | Truncated remainder; sign follows `a`. Traps on zero divisor.  |
| `F32_MOD`                                   | `{}`   | `a b → f32` | ◐   | Floored modulo; sign follows `b`. Traps on zero divisor.       |
| `F32_ABS`                                   | `{}`   | `x → f32`   | ✅   | Absolute value.                                                |
| `F32_NEG`                                   | `{}`   | `x → f32`   | ✅   | Negate by flipping sign bit, including NaN.                    |
| `F32_SQRT`                                  | `{}`   | `x → f32`   | ✅   | Square root.                                                   |
| `F32_CEIL` / `FLOOR` / `TRUNC` / `NEAREST`  | `{}`   | `x → f32`   | ✅   | Round toward +∞, −∞, zero, or nearest ties-to-even.            |
| `F32_MIN` / `MAX`                           | `{}`   | `a b → f32` | ✅   | Min/max. NaN propagates; signed zero follows IEEE-style rules. |
| `F32_COPYSIGN`                              | `{}`   | `a b → f32` | ✅   | Magnitude of `a` with sign of `b`.                             |
| `F32_EQ` / `NE` / `LT` / `GT` / `LE` / `GE` | `{}`   | `a b → i32` | ✅   | Floating-point comparison.                                     |
| `F32_TO_I32_S` / `TO_I32_U`                 | `{}`   | `f32 → i32` | ✅   | Truncate toward zero, saturating.                              |
| `F32_TO_I64_S` / `TO_I64_U`                 | `{}`   | `f32 → i64` | ✅   | Truncate toward zero, saturating.                              |
| `F32_TO_F64`                                | `{}`   | `f32 → f64` | ✅   | Widen to `f64`.                                                |
| `F32_REINTERPRET_I32`                       | `{}`   | `i32 → f32` | ✅   | Reinterpret bits without conversion.                           |

Saturating float-to-int conversions map NaN to zero. Signed conversions clamp to nearest signed bound; unsigned conversions clamp negative values to zero and overflow to the unsigned max.

### f64

| Opcode                                      | Widths | Stack       | JIT | Description                                                    |
| ------------------------------------------- | ------ | ----------- | --- | -------------------------------------------------------------- |
| `F64_CONST`                                 | `{8}`  | `→ f64`     | ✅   | Push immediate `f64`.                                          |
| `F64_ADD` / `SUB` / `MUL` / `DIV`           | `{}`   | `a b → f64` | ✅   | Floating-point arithmetic.                                     |
| `F64_REM`                                   | `{}`   | `a b → f64` | ◐   | Truncated remainder; sign follows `a`. Traps on zero divisor.  |
| `F64_MOD`                                   | `{}`   | `a b → f64` | ◐   | Floored modulo; sign follows `b`. Traps on zero divisor.       |
| `F64_ABS`                                   | `{}`   | `x → f64`   | ✅   | Absolute value.                                                |
| `F64_NEG`                                   | `{}`   | `x → f64`   | ✅   | Negate by flipping sign bit, including NaN.                    |
| `F64_SQRT`                                  | `{}`   | `x → f64`   | ✅   | Square root.                                                   |
| `F64_CEIL` / `FLOOR` / `TRUNC` / `NEAREST`  | `{}`   | `x → f64`   | ✅   | Round toward +∞, −∞, zero, or nearest ties-to-even.            |
| `F64_MIN` / `MAX`                           | `{}`   | `a b → f64` | ✅   | Min/max. NaN propagates; signed zero follows IEEE-style rules. |
| `F64_COPYSIGN`                              | `{}`   | `a b → f64` | ✅   | Magnitude of `a` with sign of `b`.                             |
| `F64_EQ` / `NE` / `LT` / `GT` / `LE` / `GE` | `{}`   | `a b → i32` | ✅   | Floating-point comparison.                                     |
| `F64_TO_I32_S` / `TO_I32_U`                 | `{}`   | `f64 → i32` | ✅   | Truncate toward zero, saturating.                              |
| `F64_TO_I64_S` / `TO_I64_U`                 | `{}`   | `f64 → i64` | ✅   | Truncate toward zero, saturating.                              |
| `F64_TO_F32`                                | `{}`   | `f64 → f32` | ✅   | Narrow to `f32`.                                               |
| `F64_REINTERPRET_I64`                       | `{}`   | `i64 → f64` | ⬜   | Reinterpret bits without conversion.                           |

## Strings

Strings are heap references. String operations consume and retain refs according to the general ref rules.

| Opcode                                         | Widths | Stack                    | JIT | Description                                                                                       |
| ---------------------------------------------- | ------ | ------------------------ | --- | ------------------------------------------------------------------------------------------------- |
| `STRING_NEW_UTF32`                             | `{}`   | `array → string`         | ⬜   | Build string from UTF-32 codepoints.                                                              |
| `STRING_LEN`                                   | `{}`   | `string → i32`           | ◐   | Push string length in codepoints.                                                                 |
| `STRING_CONCAT`                                | `{}`   | `a b → string`           | ⬜   | Concatenate strings.                                                                              |
| `STRING_EQ` / `NE` / `LT` / `GT` / `LE` / `GE` | `{}`   | `a b → i32`              | ⬜   | Lexicographic comparison.                                                                         |
| `STRING_ENCODE_UTF32`                          | `{}`   | `string → array`         | ◐   | Encode string as UTF-32 codepoints.                                                               |
| `STRING_ITER`                                  | `{}`   | `string → iterator[i32]` | ◐   | Create a lazy codepoint iterator and advance it to the first rune. Invalid UTF-8 yields `U+FFFD`. |

## Arrays

Arrays are growable heap values.

| Opcode              | Widths | Stack                                 | JIT | Description                                                                     |
| ------------------- | ------ | ------------------------------------- | --- | ------------------------------------------------------------------------------- |
| `ARRAY_NEW`         | `{2}`  | `value count → array`                 | ◐   | Create typed array filled with `value`.                                         |
| `ARRAY_NEW_DEFAULT` | `{2}`  | `count → array`                       | ◐   | Create zero-initialized typed array.                                            |
| `ARRAY_LEN`         | `{}`   | `array → i32`                         | ◐   | Push element count.                                                             |
| `ARRAY_GET`         | `{}`   | `array index → value`                 | ◐   | Load element. Traps on invalid index.                                           |
| `ARRAY_SET`         | `{}`   | `array index value →`                 | ◐   | Store element. Traps on invalid index.                                          |
| `ARRAY_FILL`        | `{}`   | `array offset count value →`          | ◐   | Fill range with repeated value.                                                 |
| `ARRAY_COPY`        | `{}`   | `dst dstOffset src srcOffset count →` | ◐   | Copy elements between arrays.                                                   |
| `ARRAY_APPEND`      | `{}`   | `array v1 … vn count → array`         | ◐   | Append `count` values and re-push the array for chaining.                       |
| `ARRAY_DELETE`      | `{}`   | `array index → value`                 | ◐   | Remove one element, shift tail left, shrink by one, and push the removed value. |
| `ARRAY_SLICE`       | `{}`   | `array start end → array`             | ◐   | Allocate a new array containing a copy of `[start, end)`.                       |

Rules:

* `ARRAY_APPEND` moves values into the array; it does not retain extra refs.
* `ARRAY_DELETE` moves the removed element to the stack.
* `ARRAY_SLICE` retains copied ref elements.
* `ARRAY_GET` copies and retains ref elements.
* `ARRAY_SLICE` consumes the source array ref; use `DUP` first to keep it.
* there is no separate `ARRAY_POP`; use `ARRAY_DELETE`.
* arrays need no iterator opcode because `ARRAY_LEN` + `ARRAY_GET` is sufficient.

Typed array notes:

* `[]i8` stores raw bytes; `ARRAY_GET` returns signed `i8`
* `ARRAY_SET` and `ARRAY_FILL` for `[]i8` truncate to the low byte
* `[]i1` stores raw one-byte booleans
* `ARRAY_SET` and `ARRAY_FILL` for `[]i1` use `val.Bool()`

## Structs

| Opcode               | Widths | Stack                  | JIT | Description                                                 |
| -------------------- | ------ | ---------------------- | --- | ----------------------------------------------------------- |
| `STRUCT_NEW`         | `{2}`  | `fields → struct`      | ◐   | Create struct from field values.                            |
| `STRUCT_NEW_DEFAULT` | `{2}`  | `→ struct`             | ◐   | Create zero-initialized struct.                             |
| `STRUCT_GET`         | `{}`   | `struct index → value` | ◐   | Load field. Traps on invalid index or incompatible target.  |
| `STRUCT_SET`         | `{}`   | `struct index value →` | ◐   | Store field. Traps on invalid index or incompatible target. |

JIT has fast paths for VM `*types.Struct` fields with common scalar/ref kinds. Host objects and heap-promoted `i64` values may fall back.

## Maps

Map keys use primitive value identity for `i1`, `i8`, `i32`, `i64`, `f32`, and `f64`. Ref keys use heap ref identity.

Missing keys read as the element zero value. `MAP_LOOKUP` also returns a presence flag.

| Opcode            | Widths | Stack                       | JIT | Description                                                                           |
| ----------------- | ------ | --------------------------- | --- | ------------------------------------------------------------------------------------- |
| `MAP_NEW`         | `{2}`  | `k1 v1 … kn vn count → map` | ◐   | Create typed map from key/value pairs. Later duplicate keys overwrite earlier values. |
| `MAP_NEW_DEFAULT` | `{2}`  | `capacity → map`            | ◐   | Create empty typed map with capacity hint.                                            |
| `MAP_LEN`         | `{}`   | `map → i32`                 | ◐   | Push entry count.                                                                     |
| `MAP_GET`         | `{}`   | `map key → value`           | ◐   | Load value or zero value if missing.                                                  |
| `MAP_LOOKUP`      | `{}`   | `map key → value ok`        | ◐   | Load value and presence flag.                                                         |
| `MAP_SET`         | `{}`   | `map key value →`           | ◐   | Insert or replace entry.                                                              |
| `MAP_DELETE`      | `{}`   | `map key →`                 | ◐   | Delete entry; missing key is a no-op.                                                 |
| `MAP_CLEAR`       | `{}`   | `map →`                     | ◐   | Delete all entries.                                                                   |
| `MAP_KEYS`        | `{}`   | `map → array`               | ◐   | Snapshot keys into a new array in unspecified order.                                  |
| `MAP_ITER`        | `{}`   | `map[K]V → iterator[K]`     | ◐   | Create lazy key iterator. Use `MAP_GET` to read values.                               |

Map iteration order and mutation visibility are unspecified, matching Go map range behavior.

## Coroutines and Iterators

A function containing `YIELD` is a coroutine-function. Calling it creates a coroutine handle and runs until the first `YIELD` or `RETURN`.

Entry-frame `YIELD` is different: it escapes to the host through `ErrYield`, and the next `Run` resumes after the yield.

`RESUME`, `CORO_DONE`, and `CORO_VALUE` also support custom `types.Iterator` heap values.

| Opcode       | Widths | Stack            | JIT | Description                                                                                         |
| ------------ | ------ | ---------------- | --- | --------------------------------------------------------------------------------------------------- |
| `YIELD`      | `{}`   | `value → result` | ◐   | Suspend current coroutine. In entry frame, escape through `ErrYield`.                               |
| `RESUME`     | `{}`   | `coro in → coro` | ◐   | Resume coroutine with input value, or advance iterator while ignoring input. Traps if already done. |
| `CORO_DONE`  | `{}`   | `coro → i32`     | ◐   | Push true if coroutine or iterator is finished. Does not release handle.                            |
| `CORO_VALUE` | `{}`   | `coro → value`   | ◐   | Push last yielded/returned value or iterator current value, then release handle.                    |

JIT treats anchor-frame yield/resume as terminal deopt points so the threaded handler performs the actual suspend/resume.

## Exceptions

Exception handling uses per-function exception tables instead of block-bracket opcodes.

This is a zero-cost model: code that never throws pays no runtime overhead. Tables are consulted only during unwinding.

### Handler Tables

Each handler is an `instr.Handler` with:

| Field   | Meaning                                    |
| ------- | ------------------------------------------ |
| `Start` | protected range start byte offset          |
| `End`   | protected range end byte offset, exclusive |
| `Catch` | catch target byte offset                   |
| `Depth` | stack height at protected-region entry     |

Use `Builder.Try(start, end, catch, depth)` to declare regions.

Declare inner regions before outer enclosing regions so the table remains innermost-first.

### Throw Semantics

`THROW` pops a value and unwinds to the nearest enclosing handler.

During unwind, the interpreter:

1. walks frames outward from the throw site
2. discards frames and operands above the handler depth
3. pushes the thrown value
4. resumes at the catch target

Any value can be thrown. `types.Error` is the canonical exception payload.

Runtime traps and host-function Go errors are caught the same way: `Run` wraps them in `types.Error` while preserving the original cause for `errors.Is` and `errors.As`.

If no handler catches the value:

* thrown `types.Error` surfaces through `*RuntimeError`
* other thrown values are wrapped under `ErrUncaughtException`

| Opcode       | Widths | Stack                  | JIT | Description                                                             |
| ------------ | ------ | ---------------------- | --- | ----------------------------------------------------------------------- |
| `THROW`      | `{}`   | `value → …`            | ◐   | Raise value to nearest handler or escape `Run` as an error. Terminator. |
| `ERROR_NEW`  | `{}`   | `payload code → error` | ◐   | Create `types.Error` with numeric `i32` code and payload.               |
| `ERROR_GET`  | `{}`   | `error → payload`      | ◐   | Push error payload and release error. Traps if operand is not an error. |
| `ERROR_CODE` | `{}`   | `error → i32`          | ◐   | Push error code and release error. Traps if operand is not an error.    |

Error code rules:

* code `0` is unclassified
* VM traps use negative `interp.TrapCode*` values
* source-language errors should use `types.ErrorCodeUserBase` and above

## Fusion Notes

Threaded execution may fuse common opcode sequences in non-precise mode.

Examples:

* primitive constants feeding primitive binary operations
* typed locals feeding primitive binary operations
* typed locals plus primitive constants feeding binary operations
* constant indexes feeding `array.get` or `struct.get`
* constant ref cell plus `ref.get`
* constant string plus `i32.const` plus `ERROR_NEW`
* `ERROR_NEW` plus `THROW`

Debugging with `WithTick(1)` preserves bytecode-level instruction boundaries.

## Agent Notes

When changing the instruction set:

* append opcodes only
* keep opcode names short, standard, and consistent
* prefer one general opcode over several narrow variants
* do not add an opcode when existing opcodes compose cleanly
* update widths, verifier, threaded runtime, JIT status, tests, and this document together
* keep stack effects explicit
* keep ref ownership rules visible
* preserve interpreter/JIT semantic symmetry
* prefer simple fallback over fragile partial lowering

The best opcode is usually the smallest one that composes well, has clear stack behavior, and does not force special cases across the verifier, interpreter, JIT, and docs.
