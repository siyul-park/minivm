# Value Representation

How minivm represents all runtime values as one `uint64`.

## Agent Checklist

Read before editing `types/boxed.go`, numeric opcode handlers, JIT value passing, or conversions between `types.Value` and `types.Boxed`.

- `Boxed` is VM stack/global currency.
- `KindF64` is raw IEEE-754 unless bits are in tagged NaN space.
- Large `I64` values can spill to heap as `types.I64` refs.
- Unbox methods do not validate kind; check `Kind()` first.
- Use `Interpreter.Load(ref)` to access heap object behind `KindRef`.

## NaN Boxing

`types.Boxed` is `uint64` using IEEE-754 quiet-NaN space.

- **F64**: if bits `63–52` are not `0x7FF`, or mantissa is `0`, value is raw `float64`; `BoxF64(v)` stores `math.Float64bits(v)`.
- **Non-F64**: if bits `63–52 == 0x7FF` and mantissa is non-zero, bits `51–49` store 3-bit `Kind`; bits `48–0` store 49-bit payload.

```text
63      52 51 50 49 48                               0
┌─────────┬──┬──┬──┬───────────────────────────────────┐
│  0x7FF  │   Kind   │        payload (49 bits)        │
└─────────┴──┴──┴──┴───────────────────────────────────┘
 12 bits    3 bits              49 bits
```

## Kind Encoding

Kinds are `iota` values in `types/value.go`. Non-F64 kinds use bits `51–49`.

| Kind | iota | Tag | Payload |
|---|---:|---|---|
| `KindF64` | `0` | — | raw IEEE-754 `float64` bits |
| `KindF32` | `1` | `001` | `float32` bits in `31–0` |
| `KindI64` | `2` | `010` | 49-bit signed integer, sign-extended from bit `48` |
| `KindI32` | `3` | `011` | sign-extended `int32` in `31–0` |
| `KindRef` | `4` | `100` | heap index as `int32` in `31–0` |

`Boxed.Kind()` detects tagged NaN values:

```go
func (v Boxed) Kind() Kind {
    u := uint64(v)
    if u>>52 == 0x7FF && u&0x000FFFFFFFFFFFFF != 0 {
        return Kind((u >> vBits) & tMask) // vBits=49, tMask=0b111
    }
    return KindF64
}
```

## Boxable I64 Range

`KindI64` stores only 49-bit signed values.

```go
func IsBoxable(v int64) bool {
    return uint64(v+vMask) <= 2*vMask // vMask = (1<<49)-1
}
```

Approximate range: `-2^48 ≤ v ≤ 2^48 - 1`.

When `!IsBoxable(v)`, interpreter heap-allocates `types.I64` and returns `KindRef`. Bytecode-transparent but costs heap allocation and RC work per out-of-range integer operation. Avoid tight loops over large I64 values when possible.

## Boxing Functions

| Function | Input | Boxed Kind |
|---|---|---|
| `BoxI32(v int32)` | any `int32` | `KindI32` |
| `BoxI64(v int64)` | 49-bit range only | `KindI64` |
| `BoxF32(v float32)` | any `float32` | `KindF32` |
| `BoxF64(v float64)` | any `float64` | `KindF64` |
| `BoxRef(v int)` | heap index in int32 range | `KindRef` |
| `BoxBool(b bool)` | `false`→`BoxI32(0)`, `true`→`BoxI32(1)` | `KindI32` |
| `Box(v uint64, kind Kind)` | raw payload | any non-F64 kind |

## Unboxing Methods

```go
v.I32() int32
v.I64() int64
v.F32() float32
v.F64() float64
v.Ref() int
v.Bool() bool
```

Valid kinds:

- `I32()` → `KindI32`
- `I64()` → `KindI64`, sign-extends 49 bits
- `F32()` → `KindF32`
- `F64()` → `KindF64`
- `Ref()` → `KindRef`, returns heap index
- `Bool()` → non-zero payload is true

Wrong-kind unboxing returns garbage; always check `Kind()` first.

## Sentinel Values

```go
var BoxedNull  = BoxRef(0) // heap index 0; Null object, never freed
var BoxedFalse = BoxI32(0)
var BoxedTrue  = BoxI32(1)
```

Use `types.IsNull(v)` when accepting either `types.Null` or `types.BoxedNull`.

## Type System Values

Heap objects implement `types.Value`.

| Go type | `Kind()` | `Type()` | Implements |
|---|---|---|---|
| `types.I32` | `KindI32` | `TypeI32` | `Value` |
| `types.I64` | `KindI64` | `TypeI64` | `Value` |
| `types.F32` | `KindF32` | `TypeF32` | `Value` |
| `types.F64` | `KindF64` | `TypeF64` | `Value` |
| `types.Ref` | `KindRef` | `TypeRef` | `Value` |
| `types.String` | `KindRef` | `TypeString` | `Value` |
| `*types.Array` | `KindRef` | `*ArrayType` | `Value`, `Traceable` |
| `*types.Struct` | `KindRef` | `*StructType` | `Value`, `Traceable` |
| `*types.Map` | `KindRef` | `*MapType` | `Value`, `Traceable` |
| `*types.MapI32`, `*types.MapI64`, `*types.MapF32`, `*types.MapF64` | `KindRef` | `*MapType` | `Value`, `Traceable` |
| `*types.Function` | `KindRef` | `*FunctionType` | `Value` |
| `*interp.HostFunction` | `KindRef` | `*FunctionType` | `Value` |
| `*interp.HostObject` | `KindRef` | `*StructType` | `Value`, `Traceable` |

`Traceable` exposes `Refs() []Ref` for GC graph traversal. Any heap object containing refs must implement `Traceable`.
`Array`, `Struct`, `Map` variants, and `HostObject` defer their `Refs()` result allocation until
the first nested ref is found, so release of values with no children stays
allocation-free while ref-containing values keep one pre-sized result slice.

`STRUCT_GET` and `STRUCT_SET` handle VM-native `*types.Struct` directly and fall back to `*interp.HostObject` for host-supplied values. See [host-integration.md](host-integration.md) for HostObject semantics.

Defined scalar values with methods marshal as their underlying primitive unless passed by pointer. Pointer form becomes a `HostObject`; field `0` is reserved as `Value` and exposes the current primitive for ordinary opcodes.

Strings created by interpreter opcodes and the marshaler are interned per `Interpreter`. Equal string contents share one heap ref while live; the intern table drops an entry when the last ref is released.

Use constructors for compound runtime types (`NewStructType`, `NewStruct`, `NewMapType`, `NewMap`, `NewMapWithCapacity`, `NewMapForType`). These constructors initialize cached metadata and internal storage used by interpreter hot paths. `NewMapForType` returns primitive-key specializations for `i32`, `i64`, `f32`, and `f64`; strings and all other ref-typed keys use generic `*types.Map` with heap ref identity keys.

## Unbox to Value

`types.Unbox(v Boxed) Value` converts boxed values to concrete `types.Value`:

- `KindI32` → `I32`
- `KindI64` → `I64`
- `KindF32` → `F32`
- `KindF64` → `F64`
- `KindRef` → `Ref`, only the index, not the heap object

Use `Interpreter.Load(addr)` to retrieve actual heap object from `KindRef`.
