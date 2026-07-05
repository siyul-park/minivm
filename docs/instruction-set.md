# Instruction Set Reference

Opcode reference for minivm bytecode.

## When to Read

Use this document when adding, changing, debugging, or testing bytecode instructions. This document owns opcode semantics; other docs should link here instead of repeating opcode behavior.

## Source of Truth

| Concern | File |
|---|---|
| opcode byte values | `instr/opcode.go` |
| mnemonic, operand widths, fixed stack effects | `instr/type.go` |
| dynamic verification rules | `program/verify.go` |
| runtime semantics | `interp/threaded.go` |
| ARM64 lowering | `interp/jit_arm64.go` |

## Core Rules

- All opcodes are one byte.
- Operands are fixed-width or length-prefixed.
- Operands are little-endian unless stated otherwise.
- Stack operands pop right-to-left.
- Boolean results use runtime kind `i1`.
- `i1`, `i8`, and `i32` share the same computational representation.
- Ref-exposing instructions retain refs.
- Ref-overwriting instructions release replaced refs.

## JIT Status

| Status | Meaning |
|---|---|
| ✅ | lowered to native ARM64 |
| ◐ | partially lowered, guarded, terminal, or framed fallback |
| ⬜ | threaded-only |

## Operand Widths

Operand widths are declared in `instr/type.go`.

| Notation | Meaning |
|---|---|
| `{}` | no operands |
| `{n}` | one fixed `n`-byte operand |
| `{-n, n}` | count byte plus `count × n`-byte operands |

Branch operands are signed 16-bit offsets relative to the end of the branch instruction.

```text
target = instruction_start + instruction_width + operand
```

## Operand Kinds

`i1`, `i8`, and `i32` share one representation class. An opcode that accepts the `i32` representation can also accept `i1` and `i8` when the verifier can prove representation compatibility.

Narrow result rules:

- comparisons, `eqz`, `ref.test`, `ref.eq`, and `ref.ne` produce `i1`
- `i32.and`, `i32.or`, and `i32.xor` preserve a shared narrow kind for `i1`/`i8`
- other arithmetic widens narrow operands to `i32`

## Opcode Families

| Family | Representative opcodes | Notes |
|---|---|---|
| Stack | `NOP`, `DROP`, `DUP`, `SWAP`, `SELECT`, `UNREACHABLE` | stack manipulation and deliberate unreachable marker |
| Control | `BR`, `BR_IF`, `BR_TABLE`, `CALL`, `RETURN`, `RETURN_CALL` | relative branches, calls, returns, and tail calls |
| Coroutines and iterators | `YIELD`, `RESUME`, `CORO_DONE`, `CORO_VALUE` | coroutine handles and custom `types.Iterator` values |
| Variables | `GLOBAL_*`, `LOCAL_*`, `CONST_GET`, `UPVAL_*` | global, local, constant, and closure upvalue access |
| References | `REF_NULL`, `REF_NEW`, `REF_GET`, `REF_SET`, `REF_TEST`, `REF_CAST`, `REF_IS_NULL`, `REF_EQ`, `REF_NE` | dynamic `ref`, mutable scalar cells, and runtime type checks |
| Closures | `CLOSURE_NEW` | function template plus captured values |
| Integers | `I32_*`, `I64_*` | constants, arithmetic, bitwise ops, comparisons, conversions, and reinterpret casts |
| Floating point | `F32_*`, `F64_*` | constants, arithmetic, comparisons, conversions, rounding, min/max, and reinterpret casts |
| Strings | `STRING_NEW_UTF32`, `STRING_LEN`, `STRING_CONCAT`, `STRING_*` comparisons, `STRING_ENCODE_UTF32`, `STRING_ITER` | heap strings and UTF-32/codepoint conversion |
| Arrays | `ARRAY_NEW`, `ARRAY_NEW_DEFAULT`, `ARRAY_LEN`, `ARRAY_GET`, `ARRAY_SET`, `ARRAY_FILL`, `ARRAY_COPY`, `ARRAY_APPEND`, `ARRAY_DELETE`, `ARRAY_SLICE` | growable typed arrays |
| Structs | `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `STRUCT_GET`, `STRUCT_SET` | VM structs and host-object fallback |
| Maps | `MAP_NEW`, `MAP_NEW_DEFAULT`, `MAP_LEN`, `MAP_GET`, `MAP_LOOKUP`, `MAP_SET`, `MAP_DELETE`, `MAP_CLEAR`, `MAP_KEYS`, `MAP_ITER` | typed maps and key iteration |
| Structured errors | `THROW`, `ERROR_NEW`, `ERROR_GET`, `ERROR_CODE` | exception tables and `types.Error` payloads |

## Family Rules

### Control

Branch offsets are relative to the end of the branch instruction. `RETURN_CALL` is the tail-call primitive. Function bodies must end with `RETURN`, `RETURN_CALL`, or `UNREACHABLE`; top-level code may fall through.

### References

A `ref` slot is the VM dynamic value type. It can hold an inline primitive or a heap reference. Use `REF_TEST` and `REF_CAST` to recover dynamic runtime types.

### Arrays

`ARRAY_APPEND` moves values into the array. `ARRAY_DELETE` moves the removed element to the stack. `ARRAY_GET` and `ARRAY_SLICE` retain copied ref elements. `ARRAY_SLICE` consumes the source array ref; use `DUP` first to keep it.

### Maps

Map keys use primitive value identity for `i1`, `i8`, `i32`, `i64`, `f32`, and `f64`. Ref keys use heap ref identity. Missing keys read as the element zero value. `MAP_LOOKUP` also returns an `i1` presence flag.

### Structured Errors

Exception handling uses per-function handler tables. `types.Error` is the canonical structured exception payload. Error code `0` is unclassified, VM traps use negative trap-code values, and source-language errors should use `types.ErrorCodeUserBase` and above.

## Fusion Notes

Threaded execution may fuse common opcode sequences in non-exact mode. Debugging with `WithTick(1)` preserves bytecode-level instruction boundaries.

Examples:

- primitive constants feeding primitive binary operations
- typed locals feeding primitive binary operations
- typed locals plus primitive constants feeding binary operations
- constant indexes feeding `array.get` or `struct.get`
- constant ref cell plus `ref.get`
- structured-error creation followed by a raise

## Maintenance Notes

When changing the instruction set:

- append opcodes only
- keep names short, standard, and consistent
- prefer one general opcode over several narrow variants
- do not add an opcode when existing opcodes compose cleanly
- update widths, verifier, threaded runtime, JIT status, tests, and this document together
- keep stack effects explicit
- keep ref ownership rules visible
- preserve interpreter/JIT semantic symmetry

## Related Docs

- `docs/guides/add-opcode.md` — checklist for adding or changing an opcode
- `docs/verification.md` — static validation and stack rules
- `docs/value-representation.md` — kinds, boxed layout, and boolean representation
- `docs/jit-internals.md` — native lowering and fallback behavior
