# Value Representation

Runtime values use one 64-bit `types.Boxed` word at interpreter stack and global boundaries.

## When to Read

Read when changing boxing, kind encoding, scalar representation, dynamic values, or JIT value passing.

## Source of Truth

| Concern | Owner |
|---|---|
| Boxed layout | `types/boxed.go` |
| Kinds | `instr/kind.go` |
| Runtime types | `types/type.go` |
| Host conversion | `interp/codec.go`, `interp/encode.go`, `interp/decode.go` |
| Native representation | `internal/jit/arm64/` |

## Boxed Layout

minivm uses NaN boxing.

```text
63      52 51 49 48                               0
┌─────────┬───────┬────────────────────────────────┐
│  0x7FF  │  Kind │            payload              │
└─────────┴───────┴────────────────────────────────┘
           3 bits              49 bits
```

- non-NaN values are `f64`
- non-`f64` values use quiet-NaN tags
- `KindRef` stores a heap index
- tag `111` is reserved
- `KindAny` is verifier-only

| Kind | Payload |
|---|---|
| `KindF64` | IEEE-754 `float64` |
| `KindF32` | low 32 bits |
| `KindI64` | 49-bit signed integer |
| `KindRef` | heap index |
| `KindI32` | signed 32-bit value |
| `KindI8` | signed 8-bit value |
| `KindI1` | `0` or `1` |

## Computational Types

`i1`, `i8`, and `i32` use the same 32-bit computational representation. Their runtime kinds remain distinct.

```text
i8 & i8 → i8
i1 ^ i1 → i1
i8 + i8 → i32
comparison / eqz → i1
```

Constant folding must preserve these result kinds.

## I64

Only signed values in the inline range are represented as `KindI64`:

```text
-2^48 <= v <= 2^48 - 1
```

Larger values are heap-backed `types.I64` values represented as `KindRef`.

`BoxI64` requires an inline value. The interpreter handles heap promotion; native ARM64 computation may keep an `i64` raw and cross back to boxed form only at a representation boundary.

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

Typed unboxing methods are `I32`, `I8`, `I64`, `F32`, `F64`, `Ref`, and `Bool`. Check `Kind()` unless the instruction contract already proves the kind.

## Boundaries

`Boxed` is the interpreter/global/storage currency. Native code may use narrower raw representations internally, but every interpreter-visible boundary must restore the exact boxed representation and ownership semantics.

## Related Docs

- `memory-model.md`
- `jit-internals.md`
- `host-integration.md`
