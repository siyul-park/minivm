# Value Representation

Runtime stack/global values use one 64-bit `types.Boxed` word. Native code may use static-type representations internally.

## Ownership

| Concern | Owner |
|---|---|
| Boxed layout | `types/boxed.go` |
| Kinds | `instr/kind.go` |
| Runtime types | `types/value.go` (`Type`) |
| Host conversion | `interp/codec.go`, `encode.go`, `decode.go` |
| Native representation: type to register class | `internal/jit/compile` (`vreg`) |
| Native word of a boxed value | `types/boxed.go` (`Boxed.Word`, `BoxWord`) |
| Native representation: lowering | `internal/jit/arm64` |

## Boxed Layout

minivm uses NaN boxing.

```text
63      52 51 49 48                               0
┌─────────┬───────┬────────────────────────────────┐
│  0x7FF  │  Kind │            payload              │
└─────────┴───────┴────────────────────────────────┘
           3 bits              49 bits
```

- Non-NaN values are `f64`;
- non-`f64` values use quiet-NaN tags;
- a positive `f64` NaN boxes as `BoxedNaN` (`0x7FF0000000000001`): a positive quiet NaN, such as ARM64's default NaN or Go's `math.NaN()`, would read as a tagged kind, and `BoxedNaN`'s tag is `KindF64`. A negative NaN never reads as a tag and keeps its bits. A boxed NaN therefore keeps its sign, so `abs`, `neg`, and `copysign` agree across tiers, but not its payload: a NaN's payload is unspecified. `BoxF64` and `BoxWord` canonicalize; native code canonicalizes wherever it boxes an `f64` register;
- `KindRef` stores a heap index;
- tag `111` is reserved;
- `KindAny` is never a tag: the verifier uses it for an operand of any kind, and `MapType.KeyKind` for an `any` key.

| Kind | Payload |
|---|---|
| `KindF64` | IEEE-754 `float64` |
| `KindF32` | low 32 bits |
| `KindI64` | inline signed 49-bit integer |
| `KindRef` | heap index |
| `KindI32` | signed 32-bit integer |
| `KindI8` | signed 8-bit integer |
| `KindI1` | `0` or `1` |

## Zero

A non-parameter local or a global starts as `types.Zero` of its declared kind, so a `ref` or `any` slot is null. Every tier observes the same words.

## Computational Types

`i1`, `i8`, and `i32` share one 32-bit computational lane while retaining distinct runtime kinds.

```text
i8 & i8 → i8
i1 ^ i1 → i1
i8 + i8 → i32
comparison / eqz → i1
```

Constant folding `MUST` preserve the declared result kind.

## I64

`KindI64` covers:

```text
-2^48 <= v <= 2^48 - 1
```

Larger signed values `MUST` use heap-backed `types.I64` objects and `KindRef`.

`BoxI64` accepts only inline values. The interpreter performs heap promotion; native ARM64 `MAY` compute raw `i64` values until a boxing boundary.

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

`Boxed.Word` returns a value's native word and `BoxWord` boxes one of a given kind; they own the kind-to-word rule for SSA constants, bridged results, and exit-map values. A wide `i64` word is promoted by the caller before `BoxWord`.

Unboxing methods: `I32`, `I8`, `I64`, `F32`, `F64`, `Ref`, `Bool`. Callers `MUST` check `Kind()` unless the contract proves the kind.

## Map Key Words

A map key is one table word whose form depends on the map's key kind (`memory-model.md`, Map Storage, owns the table that holds it):

| Key kind | Table word |
|---|---|
| `i1`, `i8`, `i32` | the value zero-extended to its own width: `i1` 0 or 1, `i8` the low byte, `i32` the low 32 bits |
| `i64` | all 64 bits when the value fits the inline range, else the `Boxed` ref of a map-owned `I64` cell |
| `f32`, `f64` | the raw bits (`f32` in the low 32); `-0` is stored as `+0` |
| `string`, other reference types | the `Boxed` word |
| `any` (`KindAny`) | the `Boxed` word; `i8` and `i32` retag to `i32`, `i1` to `i32` 0 or 1 (any nonzero payload is 1), and floats follow the float rule |

Keys compare as their Go values: a float `-0` equals `+0`, and a `NaN` key never matches, so each store adds an entry that only `map.clear` removes. The hash of a key is its base hash: the table word, except the content hash `String.Hash` (which mixes the process `Hashkey`) for a `String` and the value for a wide `I64` cell. A reference key compares by its value, in every map: a `String` by content, an `I64` cell by its integer, any other reference by identity.

## Native Representation

| Static type | Native representation |
|---|---|
| `i1`, `i8`, `i32` | 32-bit integer lane |
| `i64` | 64-bit integer lane |
| `f32` | 32-bit float lane |
| `f64` | 64-bit float lane |
| `ref` | boxed 64-bit value |

Native code boxes only at VM-slot boundaries. Narrow and `f32` values use the low 32 bits; `f64` and `ref` use the full word. An `i64` slot is guarded before unboxing because a promoted value is a `KindRef`; a wide native `i64` boxes through `ExitBox` (see `jit-internals.md`'s Exits section) rather than deopting.

An SSA constant (`ssa.Operation.Const`) is its result type's native word, `Boxed.Word` of the constant. For a non-ref kind that word is unboxed; `types.Boxed` appears only at the pool boundary and in the materializer. A `ref` constant's native word is already the boxed 64-bit value, since refs travel as boxed words.

Every interpreter, container, storage, or host boundary `MUST` restore the exact boxed representation and ownership.

## Related

- `memory-model.md`
- `jit-internals.md`
- `host-integration.md`
