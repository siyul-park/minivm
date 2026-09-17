# Value Representation

Runtime stack/global values use one 64-bit `types.Boxed` word. Native code may use static-type representations internally.

## Ownership

| Concern | Owner |
|---|---|
| Boxed layout | `types/boxed.go` |
| Kinds | `instr/kind.go` |
| Runtime types | `types/type.go` |
| Host conversion | `interp/codec.go`, `encode.go`, `decode.go` |
| Native representation | `internal/jit/arm64/` |

## Boxed Layout

minivm uses NaN boxing.

```text
63      52 51 49 48                               0
┌─────────┬───────┬────────────────────────────────┐
│  0x7FF  │  Kind │            payload              │
└─────────┴───────┴────────────────────────────────┘
           3 bits              49 bits
```text

- non-NaN values are `f64`;
- non-`f64` values use quiet-NaN tags;
- `KindRef` stores a heap index;
- tag `111` is reserved;
- `KindAny` is verifier-only.

| Kind | Payload |
|---|---|
| `KindF64` | IEEE-754 `float64` |
| `KindF32` | low 32 bits |
| `KindI64` | inline signed 49-bit integer |
| `KindRef` | heap index |
| `KindI32` | signed 32-bit integer |
| `KindI8` | signed 8-bit integer |
| `KindI1` | `0` or `1` |

## Computational Types

`i1`, `i8`, and `i32` share one 32-bit computational representation while retaining distinct runtime kinds.

```text
i8 & i8 → i8
i1 ^ i1 → i1
i8 + i8 → i32
comparison / eqz → i1
```text

Constant folding preserves result kinds.

## I64

`KindI64` covers:

```text
-2^48 <= v <= 2^48 - 1
```text

Larger signed values use heap-backed `types.I64` objects and `KindRef`.

`BoxI64` accepts only inline values. The interpreter performs heap promotion; native ARM64 may compute raw `i64` values until a boxing boundary.

## Boxing API

| Function | Result |
|---|---|
| `BoxI1` | `KindI1` |
| `BoxI8` | `KindI8` |
| `BoxI32` | `KindI32` |
| `BoxI64` | inline `KindI64` |
| `BoxF32` | `KindF32` |
| `BoxF64` | `KindF64` |
| `BoxRef` | `KindRef` |

Unboxing methods: `I32`, `I8`, `I64`, `F32`, `F64`, `Ref`, `Bool`. Check `Kind()` unless the contract proves the kind.

## Native Representation

| Static type | Native representation |
|---|---|
| `i1`, `i8`, `i32` | 32-bit integer lane |
| `i64` | 64-bit integer lane |
| `f32` | 32-bit float lane |
| `f64` | 64-bit float lane |
| `ref` | boxed 64-bit value |

Every interpreter, container, storage, or host boundary restores the exact boxed representation and ownership.

## Related

- `memory-model.md`
- `jit-internals.md`
- `host-integration.md`
