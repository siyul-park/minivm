# Value Representation

How minivm represents runtime values as one stack word.

## When to Read

Use this document when changing `types.Boxed`, kind encoding, scalar boxing, dynamic `ref`, marshaling, verifier kind rules, or JIT value passing.

For reference ownership and heap lifecycle, see `docs/memory-model.md`.

## Source of Truth

| Concern | File |
|---|---|
| boxed layout and helpers | `types/boxed.go` |
| kind definitions | `instr/kind.go` |
| runtime type descriptors | `types/type.go` |
| marshal/unmarshal conversion | `interp/marshal.go` |
| JIT value passing | `interp/jit_arm64.go` |

## Summary

`types.Boxed` is the VM's stack and global value representation. Every runtime value is carried as one `uint64`.

minivm uses NaN boxing:

- ordinary `f64` values use raw IEEE-754 bits
- all other values use quiet-NaN tagged payloads
- heap values use `KindRef`, which stores a heap index
- large `i64` values that do not fit the inline range spill to the heap

Design rules:

- keep `Boxed` as the single stack/global currency
- check `Kind()` before unboxing unless the bytecode contract proves the kind
- keep primitive values inline when possible
- use heap refs only when needed
- keep interpreter and JIT boxing rules identical

## NaN Boxing

`types.Boxed` is a `uint64`.

For `f64`, minivm stores the raw IEEE-754 bits.

For non-`f64` values, minivm uses quiet-NaN space:

```text
63      52 51 50 49 48                               0
┌─────────┬──────────┬────────────────────────────────┐
│  0x7FF  │   Kind   │        payload (49 bits)       │
└─────────┴──────────┴────────────────────────────────┘
  12 bits    3 bits              49 bits
```

Rules:

- if exponent bits are not `0x7FF`, the value is `KindF64`
- if exponent bits are `0x7FF` and mantissa is zero, the value is also `KindF64`
- if exponent bits are `0x7FF` and mantissa is non-zero, bits `51–49` store `Kind`
- bits `48–0` store the payload

## Kind Encoding

Kinds are defined in `instr/kind.go` and re-exported as `types.Kind`.

The numeric tag values are optimized for fast classification, not long-term serialization stability.

| Kind | Tag | Payload |
|---|---|---|
| `KindF64` | `000` | raw IEEE-754 `float64` bits |
| `KindF32` | `001` | `float32` bits in `31–0` |
| `KindI64` | `010` | 49-bit signed integer |
| `KindRef` | `011` | heap index in `31–0` |
| `KindI32` | `100` | sign-extended `int32` |
| `KindI8` | `101` | sign-extended `int8` |
| `KindI1` | `110` | boolean bit, `0` or `1` |

Tag `111` is reserved. `KindAny` is verifier-only and is not a real boxed runtime kind.

## Computational Types

`i1`, `i8`, and `i32` share one representation class.

They use the same 32-bit payload slot, the same broad computational representation, and the same `i32.*` operator family.

`Kind.Repr()` folds `i1` and `i8` into the `i32` representation class. The verifier and JIT use representation equality to decide whether operands can compute together.

The narrow kinds still keep their own `Kind`, `Type`, and `String` behavior. Values remain distinguishable as `i1` or `i8` across stack operations, locals, globals, struct fields, arrays, boxing, and marshaling.

### Narrow Results

Bitwise operations are width-closed when both operands share the same narrow kind.

```text
i8 & i8 → i8
i1 ^ i1 → i1
i8 & i32 → i32
```

The following produce `i1`:

- comparisons
- `eqz`
- `ref.test`
- `ref.eq`
- `ref.ne`

Other arithmetic widens narrow operands to `i32`.

```text
i8 + i8 → i32
i1 + i1 → i32
```

Constant folding must preserve this behavior. A folded comparison produces an `i1` value.

## Boxable I64 Range

`KindI64` stores only 49-bit signed values inline.

Approximate inline range:

```text
-2^48 <= v <= 2^48 - 1
```

Values outside this range are heap-allocated as `types.I64` and represented as `KindRef`.

This is bytecode-transparent, but it costs heap allocation and reference-counting work.

The ARM64 JIT only handles inline `KindI64`. If an `i64` slot contains a heap-promoted ref, JIT exits to threaded execution.

## Boxing and Unboxing

| Function | Result |
|---|---|
| `BoxI32(v int32)` | `KindI32` |
| `BoxI8(v int8)` | `KindI8` |
| `BoxI1(b bool)` | `KindI1`; returns boolean singletons |
| `BoxI64(v int64)` | `KindI64`; value must be boxable |
| `BoxF32(v float32)` | `KindF32` |
| `BoxF64(v float64)` | `KindF64` |
| `BoxRef(v int)` | `KindRef` |
| `Box(v uint64, kind Kind)` | raw non-`f64` payload with kind tag |

There is no `BoxBool`. Use `BoxI1`.

`Boxed` exposes typed unbox methods: `I32`, `I8`, `I64`, `F32`, `F64`, `Ref`, and `Bool`.

Valid use:

| Method | Expected kind |
|---|---|
| `I32()` | `KindI32`; also reads `KindI8` and `KindI1` payloads |
| `I8()` | `KindI8` |
| `I64()` | `KindI64` |
| `F32()` | `KindF32` |
| `F64()` | `KindF64` |
| `Ref()` | `KindRef` |
| `Bool()` | `KindI1` |

Incorrect-kind unboxing is invalid. Check `Kind()` first unless the instruction contract already proves the kind.

## Sentinels

```go
var BoxedNull  = BoxRef(0)
var BoxedFalse = Box(0, KindI1)
var BoxedTrue  = Box(1, KindI1)
```

Rules:

- heap index `0` is always `Null`
- `BoxedNull` is `BoxRef(0)`
- `BoxI1(false)` returns `BoxedFalse`
- `BoxI1(true)` returns `BoxedTrue`
- use `types.IsNull(v)` when accepting either `types.Null` or `types.BoxedNull`

## Runtime Values

User-defined heap types do not add new `Kind` values. They use `KindRef` like other heap objects.

| Go type | Kind | Type | Notes |
|---|---|---|---|
| `types.I1` | `KindI1` | `TypeI1` | scalar |
| `types.I8` | `KindI8` | `TypeI8` | scalar |
| `types.I32` | `KindI32` | `TypeI32` | scalar |
| `types.I64` | `KindI64` | `TypeI64` | scalar or heap cell |
| `types.F32` | `KindF32` | `TypeF32` | scalar |
| `types.F64` | `KindF64` | `TypeF64` | scalar |
| `types.Ref` | `KindRef` | `TypeRef` | heap index wrapper |
| `types.String` | `KindRef` | `TypeString` | heap value |
| arrays, structs, maps, functions, closures, host values | `KindRef` | type-specific | heap values |

If a heap value contains refs, it must implement `Traceable`. If a heap value owns external resources, it may implement `io.Closer`.

## Traceable Values

`Traceable` exposes nested heap refs for GC and release traversal.

```go
Refs(dst []Ref) []Ref
```

Rules:

- append child refs to `dst`
- return `dst` unchanged if there are no child refs
- avoid allocation until the first child ref is found
- keep the append-only contract

Arrays, structs, maps, closures, host objects, and iterators that hold refs must report them.

## Strings, Arrays, Maps, and Iterators

Strings created by interpreter opcodes and the marshaler are interned per `Interpreter`. Equal string contents share one heap ref while live.

`TypeI1` and `TypeI8` are first-class stack kinds, not element-only types.

Typed container rules:

- `[]i8` arrays store raw bytes as `int8`
- `ARRAY_GET` on `[]i8` returns signed `BoxI8`
- `ARRAY_SET` and `ARRAY_FILL` on `[]i8` truncate to the low byte
- `[]i1` arrays use raw one-byte boolean cells
- `ARRAY_GET` on `[]i1` returns `BoxI1`
- `ARRAY_SET` and `ARRAY_FILL` on `[]i1` store `val.Bool()`

`MapIterator` yields keys only. Use `MAP_GET` to read values. `StringIterator` yields UTF-8 codepoints as `i32` without allocating a UTF-32 array.

## Dynamic Values

`ref` (`types.TypeRef`) is the VM dynamic value type.

There is no separate boxed-any representation. A `Boxed` value is already a self-describing tagged union, so a `ref`-typed slot stores the full `Boxed` value verbatim.

A `ref` slot can hold inline primitives and heap refs.

Rules:

- `refType.Cast` accepts every type
- `REF_TEST` and `REF_CAST` recover runtime type
- primitive operands use `Boxed.Type()`
- `KindRef` operands use the heap object's `Type()`
- reference counting is based on the actual runtime `Kind`, not the declared slot type

Storing a primitive into a `ref` slot does not retain. Overwriting a `ref` slot releases only when the displaced runtime value is actually `KindRef`.

Only generic arrays and maps can hold dynamic `ref` elements. Specialized containers pack raw bits and do not store kind tags.

Go `interface{}` maps to VM `ref` through the marshaler.

## Unbox to Value

`types.Unbox(v Boxed)` converts inline boxed values to concrete `types.Value`.

For `KindRef`, `Unbox` returns only the heap index wrapper. It does not load the heap object. Use `vm.Load(v.Ref())` to access the actual heap value.

## Maintenance Notes

When changing value representation:

- keep `Boxed` as the single stack/global currency
- avoid adding new runtime kinds unless necessary
- preserve `i1`/`i8`/`i32` representation compatibility
- keep boolean results as `i1`
- make heap spill behavior transparent to bytecode
- keep JIT tag checks aligned with interpreter behavior
- update verifier, interpreter, JIT, marshal, and docs together when kind rules change

## Related Docs

- `docs/memory-model.md` — heap ownership, RC, and GC
- `docs/instruction-set.md` — opcode kind rules and stack effects
- `docs/host-integration.md` — marshal/unmarshal type mapping
- `docs/jit-internals.md` — native representation and guards
