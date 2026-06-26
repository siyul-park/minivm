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

Kinds are `iota` values in `instr/kind.go` (re-exported as `types.Kind`). Non-F64
kinds use bits `51–49`. The tags are laid out for fast classification, not
stability: constants round-trip as text, so the numeric values are never
serialized.

| Kind | iota | Tag | Payload |
|---|---:|---|---|
| `KindF64` | `0` | `000` | raw IEEE-754 `float64` bits |
| `KindF32` | `1` | `001` | `float32` bits in `31–0` |
| `KindI64` | `2` | `010` | 49-bit signed integer, sign-extended from bit `48` |
| `KindRef` | `3` | `011` | heap index as `int32` in `31–0` |
| `KindI32` | `4` | `100` | sign-extended `int32` in `31–0` |
| `KindI8` | `5` | `101` | `int8` value sign-extended into `31–0` |
| `KindI1` | `6` | `110` | `0` or `1` in bit `0` |

Tag `111` is reserved. `KindAny` (`0xFF`) is the verifier's top element, not a
real boxed kind.

### Computational types: i1, i8

`i32`, `i8`, and `i1` form one **representation class**: all three carry tag bit
`0b100` (`reprI32`), share the 32-bit slot, the same boxed payload encoding, and
the `i32.*` operators. This mirrors the JVM "computational type" — `boolean`,
`byte`, and `int` all compute as `int`. `Kind.Repr()` is a single mask that
folds `i1`/`i8` to `i32`; `Kind.Repr()` equality is how the verifier and JIT
decide whether two operands are computable together.

The narrow kinds keep their own `Kind`/`Type`/`String` so a value stays
distinguishable as `i1` or `i8` across push/pop, locals, globals, struct fields,
boxing, and marshaling — at no extra runtime cost, because the box payload is
identical to `i32`.

Through arithmetic the kind follows Rust/Swift, not pure JVM:

- **Width-closed bitwise** `i32.and`/`i32.or`/`i32.xor` (and `~x` spelled as
  `x ^ -1`) **preserve a shared narrow kind**: `i8 & i8 → i8`, `i1 ^ i1 → i1`. A
  mixed pair (e.g. `i8 & i32`) widens to `i32`. The verifier (`bitwise`) and both
  engines agree; the interpreter's `bitwiseKind` and the JIT's `i32Bitwise`
  compute the result kind from the two operand kinds.
- **Comparisons / `eqz` / `ref.test` / `ref.eq`** produce `i1` (their declared
  `Push` kind). The threaded engine boxes through `BoxI1`; the JIT's `setBool`
  pushes `KindI1`. Constant folding of a comparison interns an `i1` constant and
  emits `CONST_GET`, so a folded `5 < 3` keeps the `i1` kind a dynamic compare
  would have produced.
- **Other arithmetic** (`add`/`sub`/`mul`/`shift`/…) is not width-closed and
  widens to `i32`.

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
    const (
        minI64 = -1 << 48
        maxI64 = 1<<48 - 1
    )
    return minI64 <= v && v <= maxI64
}
```

Approximate range: `-2^48 ≤ v ≤ 2^48 - 1`.

When `!IsBoxable(v)`, interpreter heap-allocates `types.I64` and returns `KindRef`. Bytecode-transparent but costs heap allocation and RC work per out-of-range integer operation. Avoid tight loops over large I64 values when possible.

The JIT only handles inline `KindI64`. Because an i64-typed local/global can hold a promoted `KindRef` at runtime, JIT i64 slot loads and stores tag-check the value and fall back to the interpreter on a ref (see `jit-internals.md`).

## Boxing Functions

| Function | Input | Boxed Kind |
|---|---|---|
| `BoxI32(v int32)` | any `int32` | `KindI32` |
| `BoxI8(v int8)` | any `int8` (sign-extended payload) | `KindI8` |
| `BoxI1(b bool)` | returns the `BoxedFalse`/`BoxedTrue` singleton | `KindI1` |
| `BoxI64(v int64)` | 49-bit range only | `KindI64` |
| `BoxF32(v float32)` | any `float32` | `KindF32` |
| `BoxF64(v float64)` | any `float64` | `KindF64` |
| `BoxRef(v int)` | heap index in int32 range | `KindRef` |
| `Box(v uint64, kind Kind)` | raw payload | any non-F64 kind |

There is no `BoxBool` — `BoxI1` is the single boolean boxer (it returns the
shared `i1` singletons).

## Unboxing Methods

```go
v.I32() int32
v.I8()  int8
v.I64() int64
v.F32() float32
v.F64() float64
v.Ref() int
v.Bool() bool
```

Valid kinds:

- `I32()` → `KindI32` (also reads `KindI8`/`KindI1`, since they share the payload)
- `I8()` → `KindI8`, low byte as `int8`
- `I64()` → `KindI64`, sign-extends 49 bits
- `F32()` → `KindF32`
- `F64()` → `KindF64`
- `Ref()` → `KindRef`, returns heap index
- `Bool()` → `KindI1`; non-zero payload is true

Wrong-kind unboxing returns garbage; always check `Kind()` first.

## Sentinel Values

```go
var BoxedNull  = BoxRef(0)   // heap index 0; Null object, never freed
var BoxedFalse = Box(0, KindI1) // i1 false singleton; BoxI1(false) returns it
var BoxedTrue  = Box(1, KindI1) // i1 true singleton;  BoxI1(true) returns it
```

Use `types.IsNull(v)` when accepting either `types.Null` or `types.BoxedNull`.

## Type System Values

Heap objects implement `types.Value`.

| Go type | `Kind()` | `Type()` | Implements |
|---|---|---|---|
| `types.I1` | `KindI1` | `TypeI1` | `Value` |
| `types.I8` | `KindI8` | `TypeI8` | `Value` |
| `types.I32` | `KindI32` | `TypeI32` | `Value` |
| `types.I64` | `KindI64` | `TypeI64` | `Value` |
| `types.F32` | `KindF32` | `TypeF32` | `Value` |
| `types.F64` | `KindF64` | `TypeF64` | `Value` |
| `types.Ref` | `KindRef` | `TypeRef` | `Value` |
| `types.String` | `KindRef` | `TypeString` | `Value` |
| `types.TypedArray[bool]` | `KindRef` | `TypeI1Array` | `Value` |
| `types.TypedArray[int8]` | `KindRef` | `TypeI8Array` | `Value` |
| `*types.Array` | `KindRef` | `*ArrayType` | `Value`, `Traceable` |
| `*types.Struct` | `KindRef` | `*StructType` | `Value`, `Traceable` |
| `*types.Map` | `KindRef` | `*MapType` | `Value`, `Traceable` |
| `*types.MapI32`, `*types.MapI64`, `*types.MapF32`, `*types.MapF64` | `KindRef` | `*MapType` | `Value`, `Traceable` |
| `*types.MapIterator` | `KindRef` | `TypeRef` | `Value`, `Traceable`, `Iterator` |
| `*types.Function` | `KindRef` | `*FunctionType` | `Value` |
| `*types.Closure` | `KindRef` | `*FunctionType` | `Value`, `Traceable` |
| `*interp.HostFunction` | `KindRef` | `*FunctionType` | `Value` |
| `*interp.HostObject` | `KindRef` | `*StructType` | `Value`, `Traceable` |

`*types.Closure` **shares** `*FunctionType` with its underlying function: a closure and a
function with the same signature are type-equal. Captures live on `*types.Function`
(`Captures []Type`, parallel to `Locals`), never on `*FunctionType`, so they never affect
type equality. `REF_NEW`/`REF_GET`/`REF_SET` reuse the `types.I32/I64/F32/F64` heap rows as
mutable scalar cells.

`Traceable` exposes `Refs() []Ref` for GC graph traversal. Any heap object containing refs must implement `Traceable`.
`Array`, `Struct`, `Map` variants, and `HostObject` defer their `Refs()` result allocation until
the first nested ref is found, so release of values with no children stays
allocation-free while ref-containing values keep one pre-sized result slice.
`*types.Closure` always reports at least its template (`Fn`), so it pre-sizes its `Refs()`
slice to `1 + len(Upvals)` and never takes the lazy-nil path.

`STRUCT_GET` and `STRUCT_SET` handle VM-native `*types.Struct` directly and fall back to `*interp.HostObject` for host-supplied values. See [host-integration.md](host-integration.md) for HostObject semantics.

`Iterator` marks heap values that can be consumed by `RESUME`, `CORO_DONE`, and
`CORO_VALUE`. `Next()` advances one item, `Current()` returns the single current
VM value, and `Done()` reports exhaustion. Iterators that hold refs must also
implement `Traceable` and report retained backing refs plus any current ref.
`MapIterator` keeps its source map and current ref key live while it traverses
without building a key array; it yields keys only.

User-defined heap types add no new `Kind`: a custom value implements `types.Value` / `types.Type` and is `KindRef` like every heap object (implement `Traceable` if it holds refs, `io.Closer` to release native resources). `Zero` returns `BoxedNull` for `KindRef`, so a custom type's default is null until an opcode constructs it. Reference the type programmatically via `b.Type(t)` and marshal Go values with `WithConverter` / `ValueMarshaler`; reconstructing a custom type from a textual type string is not yet supported.

Defined scalar values with methods marshal as their underlying primitive unless passed by pointer. Pointer form becomes a `HostObject`; field `0` is reserved as `Value` and exposes the current primitive for ordinary opcodes.

Strings created by interpreter opcodes and the marshaler are interned per `Interpreter`. Equal string contents share one heap ref while live; the intern table drops an entry when the last ref is released.

`TypeI8` (and `TypeI1`) are first-class stack kinds, not element-only — see
[Computational types](#computational-types-i1-i8). `[]i8` arrays store raw bytes
(`int8`); `ARRAY_GET` returns a signed `BoxI8` (`-128..127`), and `ARRAY_SET` /
`ARRAY_FILL` narrow via low-byte truncation (`int8(val.I32())`, no overflow
trap). Use `[]i8` for binary blobs; mask `& 0xFF` if you need the unsigned
`0..255` reading, because the load is now sign-extended. `[]i1` arrays use
`TypedArray[bool]` (raw 1-byte cells); `ARRAY_GET` returns `BoxI1`, and
`ARRAY_SET` / `ARRAY_FILL` store `val.Bool()` (non-zero ⇒ true).

Use constructors for compound runtime types (`NewStructType`, `NewStruct`, `NewMapType`, `NewMap`, `NewMapWithCapacity`, `NewMapForType`). These constructors initialize cached metadata and internal storage used by interpreter hot paths. `NewMapForType` returns primitive-key specializations for `i1`, `i8`, `i32`, `i64`, `f32`, and `f64` (`i1`/`i8` use `TypedMap[bool]`/`TypedMap[int8]`); strings and all other ref-typed keys use generic `*types.Map` with heap ref identity keys.

## Dynamic (any) values

`ref` (`types.TypeRef`) is the VM's dynamic "any" type — the equivalent of Go's
`interface{}`. No separate boxed representation exists: a `Boxed` is already a
self-describing tagged union, so a `ref`-typed slot can hold any value.

- A `ref` slot stores the full `Boxed` verbatim. An inline primitive (`KindI32`,
  `KindF32`, …) stays inline; a `KindRef` indexes a heap object whose `Type()`
  gives the concrete type.
- `refType.Cast` accepts every type, so any value is assignable to a `ref` slot.
- Recover the runtime type with `REF_TEST` / `REF_CAST` (they accept both
  primitive and ref operands). For a primitive operand they compare against
  `Boxed.Type()`; for a `KindRef` they compare against the heap object's `Type()`.
- Reference counting is always guarded on the value's **actual** runtime `Kind`,
  never the declared slot kind: storing a primitive into a `ref` slot does not
  retain, and overwriting releases only when the displaced value is a real ref.
  This holds uniformly for locals, globals, struct fields, array elements, and
  map keys/values.

Constraints:

- Only the **generic** containers carry `any` elements: `[]ref` uses `*Array`
  (not `I32Array` etc.); a `ref`-keyed or `ref`-valued map uses `*Map` (not the
  `MapI32`/… specializations). Specialized containers pack raw bits with no kind
  tag and cannot hold `any`.
- A generic `*Map` keys primitives by value (`MapKey{Kind, Bits}`) and heap refs
  by identity, so primitive `any` keys compare correctly.
- The ARM64 JIT can move `ref` slots through locals, globals, and upvalues with
  runtime kind checks and ref-count updates. Unsupported heap shapes still exit
  to threaded execution.

Go `interface{}` bridges to `ref` through the marshaler — see
[host-integration.md](host-integration.md#dynamic-interface-values).

## Unbox to Value

`types.Unbox(v Boxed) Value` converts boxed values to concrete `types.Value`:

- `KindI32` → `I32`
- `KindI8` → `I8`
- `KindI1` → `I1`
- `KindI64` → `I64`
- `KindF32` → `F32`
- `KindF64` → `F64`
- `KindRef` → `Ref`, only the index, not the heap object

Use `Interpreter.Load(addr)` to retrieve actual heap object from `KindRef`.
