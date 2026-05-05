# Value Representation

How minivm represents all runtime values as a single `uint64`.

## NaN Boxing

`types.Boxed` is a `uint64`. The encoding exploits the IEEE-754 quiet-NaN space:

- **F64**: Any bit pattern where bits 63–52 are NOT `0x7FF`, or the mantissa is 0, is a raw `float64`. `BoxF64(v)` stores `math.Float64bits(v)` directly.
- **Non-F64**: When bits 63–52 equal `0x7FF` (quiet-NaN prefix) and the mantissa is non-zero, bits 51–49 hold the 3-bit `Kind` tag and bits 48–0 hold the 49-bit payload.

```
 63      52 51 50 49 48                               0
 ┌─────────┬──┬──┬──┬───────────────────────────────────┐
 │  0x7FF  │   Kind   │        payload (49 bits)        │
 └─────────┴──┴──┴──┴───────────────────────────────────┘
  12 bits    3 bits              49 bits
```

## Kind Encoding

Kind values are defined as `iota` in `types/value.go`. Non-F64 kinds are stored in bits 51–49.

| Kind | iota value | Tag bits 51–49 | Payload (bits 48–0) |
|------|-----------|-----------------|---------------------|
| `KindF64` | 0 | — (not a NaN) | raw IEEE-754 `float64` bits |
| `KindF32` | 1 | `001` | `float32` bits in bits 31–0 |
| `KindI64` | 2 | `010` | 49-bit signed integer, sign-extended from bit 48 |
| `KindI32` | 3 | `011` | sign-extended `int32` in bits 31–0 |
| `KindRef` | 4 | `100` | heap index as `int32` in bits 31–0 |

`Kind` is detected by `Boxed.Kind()`:
```go
func (v Boxed) Kind() Kind {
    u := uint64(v)
    // top 12 bits == 0x7FF and mantissa != 0 → tagged NaN
    if u>>52 == 0x7FF && u&0x000FFFFFFFFFFFFF != 0 {
        return Kind((u >> vBits) & tMask)  // vBits=49, tMask=0b111 → extracts bits 51–49
    }
    return KindF64
}
```

## Boxable Range for I64

`KindI64` can only represent 49-bit signed values. `IsBoxable(v int64) bool` returns true when the value fits:

```go
func IsBoxable(v int64) bool {
    return uint64(v+vMask) <= 2*vMask  // vMask = (1<<49)-1
}
```

Approximately: `-2^48 ≤ v ≤ 2^48 - 1`.

When `!IsBoxable(v)`, the interpreter heap-allocates a `types.I64` value and returns a `KindRef` pointing to it. This is transparent to bytecode but causes a heap allocation and RC increment per operation on out-of-range integers. Avoid tight loops over large I64 values if possible.

## Boxing Functions

| Function | Input | Boxed Kind |
|---|---|---|
| `BoxI32(v int32)` | any `int32` | `KindI32` |
| `BoxI64(v int64)` | 49-bit range only | `KindI64` |
| `BoxF32(v float32)` | any `float32` | `KindF32` |
| `BoxF64(v float64)` | any `float64` | `KindF64` |
| `BoxRef(v int)` | heap index (int32 range) | `KindRef` |
| `BoxBool(b bool)` | `false`→`BoxI32(0)`, `true`→`BoxI32(1)` | `KindI32` |
| `Box(v uint64, kind Kind)` | raw payload | any non-F64 Kind |

## Unboxing Methods

```go
v.I32() int32     // valid when Kind == KindI32
v.I64() int64     // valid when Kind == KindI64 (sign-extends 49 bits)
v.F32() float32   // valid when Kind == KindF32
v.F64() float64   // valid when Kind == KindF64
v.Ref() int       // valid when Kind == KindRef; returns heap index
v.Bool() bool     // non-zero payload → true
```

Calling an unbox method for the wrong Kind returns garbage — always check `Kind()` first.

## Sentinel Values

```go
var BoxedNull  = BoxRef(0)    // heap index 0 (Null object, never freed)
var BoxedFalse = BoxI32(0)
var BoxedTrue  = BoxI32(1)
```

## Type System Values

Heap-allocated objects implement `types.Value`:

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
| `*types.Function` | `KindRef` | `*FunctionType` | `Value` |
| `*interp.HostFunction` | `KindRef` | `*FunctionType` | `Value` |

`Traceable` objects expose `Refs() []Ref` so the GC can walk their reference graph. Any heap object that contains other heap references must implement `Traceable`.

## Unbox to Value

`types.Unbox(v Boxed) Value` converts a `Boxed` to a concrete `types.Value`:
- `KindI32` → `I32`
- `KindI64` → `I64`
- `KindF32` → `F32`
- `KindF64` → `F64`
- `KindRef` → `Ref` (just the index, not the heap object)

Use `Interpreter.Load(addr)` to retrieve the actual heap object from a `KindRef`.
