# Value Representation

How minivm represents runtime values as one `uint64`.

## Summary

`types.Boxed` is the VM’s stack and global value representation. Every runtime value is carried as one `uint64`.

minivm uses NaN boxing:

* ordinary `f64` values use raw IEEE-754 bits
* all other values use quiet-NaN tagged payloads
* heap values are represented by `KindRef`, which stores a heap index
* large `i64` values that do not fit the inline range spill to the heap

Default design rule:

* keep value representation simple and explicit
* check `Kind()` before unboxing
* keep primitive values inline when possible
* use heap refs only when needed
* keep interpreter and JIT boxing rules identical
* prefer short, standard names such as `value`, `boxed`, `kind`, `ref`, `addr`, `bits`, and `payload`

## Agent Fast Path

Read this before editing:

* `types/boxed.go`
* numeric opcode handlers
* verifier kind logic
* JIT value passing
* marshal/unmarshal conversion
* code converting between `types.Value` and `types.Boxed`

Checklist:

* `Boxed` is the VM stack/global currency
* `KindF64` is raw IEEE-754 unless the bits are tagged NaN
* `i1`, `i8`, and `i32` share one representation class
* large `i64` values may become `KindRef`
* unbox methods do not validate kind
* always check `Kind()` first unless the bytecode contract proves it
* use `Interpreter.Load(ref)` to read the heap object behind `KindRef`

## NaN Boxing

`types.Boxed` is a `uint64`.

For `f64`, minivm stores the raw IEEE-754 bits.

For non-`f64` values, minivm uses quiet-NaN space:

```text id="munow6"
63      52 51 50 49 48                               0
┌─────────┬──────────┬────────────────────────────────┐
│  0x7FF  │   Kind   │        payload (49 bits)       │
└─────────┴──────────┴────────────────────────────────┘
  12 bits    3 bits              49 bits
```

Rules:

* if exponent bits are not `0x7FF`, the value is `KindF64`
* if exponent bits are `0x7FF` and mantissa is zero, the value is also `KindF64`
* if exponent bits are `0x7FF` and mantissa is non-zero, bits `51–49` store `Kind`
* bits `48–0` store the payload

## Kind Encoding

Kinds are defined in `instr/kind.go` and re-exported as `types.Kind`.

The numeric tag values are optimized for fast classification, not long-term serialization stability.

| Kind      | Tag   | Payload                     |
| --------- | ----- | --------------------------- |
| `KindF64` | `000` | raw IEEE-754 `float64` bits |
| `KindF32` | `001` | `float32` bits in `31–0`    |
| `KindI64` | `010` | 49-bit signed integer       |
| `KindRef` | `011` | heap index in `31–0`        |
| `KindI32` | `100` | sign-extended `int32`       |
| `KindI8`  | `101` | sign-extended `int8`        |
| `KindI1`  | `110` | boolean bit, `0` or `1`     |

Tag `111` is reserved.

`KindAny` is verifier-only. It is not a real boxed runtime kind.

## Computational Types

`i1`, `i8`, and `i32` share one representation class.

They use:

* the same 32-bit payload slot
* the same broad computational representation
* the same `i32.*` operator family

This mirrors the JVM model where boolean, byte, and int compute as int.

`Kind.Repr()` folds `i1` and `i8` into the `i32` representation class. The verifier and JIT use representation equality to decide whether operands can compute together.

The narrow kinds still keep their own `Kind`, `Type`, and `String` behavior, so values remain distinguishable as `i1` or `i8` across:

* stack operations
* locals
* globals
* struct fields
* arrays
* boxing
* marshaling

### Narrow Kind Results

Bitwise operations are width-closed when both operands share the same narrow kind:

```text id="p2ayyl"
i8 & i8 → i8
i1 ^ i1 → i1
i8 & i32 → i32
```

The following produce `i1`:

* comparisons
* `eqz`
* `ref.test`
* `ref.eq`
* `ref.ne`

Other arithmetic widens narrow operands to `i32`.

```text id="2x4tzk"
i8 + i8 → i32
i1 + i1 → i32
```

Constant folding must preserve this behavior. A folded comparison produces an `i1` constant, not an `i32`.

## Boxable I64 Range

`KindI64` stores only 49-bit signed values inline.

Approximate inline range:

```text id="2yg92u"
-2^48 <= v <= 2^48 - 1
```

Values outside this range are heap-allocated as `types.I64` and represented as `KindRef`.

This is bytecode-transparent, but it costs heap allocation and reference-counting work.

JIT only handles inline `KindI64`. If an `i64` slot contains a heap-promoted ref, JIT exits to threaded execution.

## Boxing

| Function                   | Result                               |
| -------------------------- | ------------------------------------ |
| `BoxI32(v int32)`          | `KindI32`                            |
| `BoxI8(v int8)`            | `KindI8`                             |
| `BoxI1(b bool)`            | `KindI1`; returns boolean singletons |
| `BoxI64(v int64)`          | `KindI64`; value must be boxable     |
| `BoxF32(v float32)`        | `KindF32`                            |
| `BoxF64(v float64)`        | `KindF64`                            |
| `BoxRef(v int)`            | `KindRef`                            |
| `Box(v uint64, kind Kind)` | raw non-`f64` payload with kind tag  |

There is no `BoxBool`. Use `BoxI1`.

## Unboxing

`Boxed` exposes typed unbox methods:

```go id="lm0tpq"
v.I32() int32
v.I8()  int8
v.I64() int64
v.F32() float32
v.F64() float64
v.Ref() int
v.Bool() bool
```

Valid use:

| Method   | Expected kind                                        |
| -------- | ---------------------------------------------------- |
| `I32()`  | `KindI32`; also reads `KindI8` and `KindI1` payloads |
| `I8()`   | `KindI8`                                             |
| `I64()`  | `KindI64`                                            |
| `F32()`  | `KindF32`                                            |
| `F64()`  | `KindF64`                                            |
| `Ref()`  | `KindRef`                                            |
| `Bool()` | `KindI1`                                             |

Wrong-kind unboxing returns garbage. Check `Kind()` first unless the instruction contract already proves the kind.

## Sentinels

```go id="7f03hq"
var BoxedNull  = BoxRef(0)
var BoxedFalse = Box(0, KindI1)
var BoxedTrue  = Box(1, KindI1)
```

Rules:

* heap index `0` is always `Null`
* `BoxedNull` is `BoxRef(0)`
* `BoxI1(false)` returns `BoxedFalse`
* `BoxI1(true)` returns `BoxedTrue`
* use `types.IsNull(v)` when accepting either `types.Null` or `types.BoxedNull`

## Runtime Values

Heap objects implement `types.Value`.

| Go type                | Kind      | Type            | Notes                |
| ---------------------- | --------- | --------------- | -------------------- |
| `types.I1`             | `KindI1`  | `TypeI1`        | scalar               |
| `types.I8`             | `KindI8`  | `TypeI8`        | scalar               |
| `types.I32`            | `KindI32` | `TypeI32`       | scalar               |
| `types.I64`            | `KindI64` | `TypeI64`       | scalar or heap cell  |
| `types.F32`            | `KindF32` | `TypeF32`       | scalar               |
| `types.F64`            | `KindF64` | `TypeF64`       | scalar               |
| `types.Ref`            | `KindRef` | `TypeRef`       | heap index wrapper   |
| `types.String`         | `KindRef` | `TypeString`    | heap value           |
| `*types.Array`         | `KindRef` | `*ArrayType`    | traceable            |
| `*types.Struct`        | `KindRef` | `*StructType`   | traceable            |
| `*types.Map`           | `KindRef` | `*MapType`      | traceable            |
| `*types.Function`      | `KindRef` | `*FunctionType` | callable             |
| `*types.Closure`       | `KindRef` | `*FunctionType` | callable, traceable  |
| `*interp.HostFunction` | `KindRef` | `*FunctionType` | callable host bridge |
| `*interp.HostObject`   | `KindRef` | `*StructType`   | host-backed struct   |

User-defined heap types do not add new `Kind` values. They use `KindRef` like other heap objects.

If a heap value contains refs, it must implement `Traceable`.

If a heap value owns native resources, it may implement `io.Closer`.

## Closures

A closure has the same `*FunctionType` as its underlying function.

Captures live on `*types.Function` as `Captures []Type`, not on `*FunctionType`. Therefore captures do not affect function type equality.

A closure keeps alive:

* its function template
* its captured upvalues

It reports these through `Traceable`.

## Traceable Values

`Traceable` exposes nested heap refs for GC and release traversal.

```go id="56mk7a"
Refs(dst []Ref) []Ref
```

Rules:

* append child refs to `dst`
* return `dst` unchanged if there are no child refs
* avoid allocation until the first child ref is found
* keep the append-only contract

Arrays, structs, maps, closures, host objects, and iterators that hold refs must report them.

## Structs and Host Objects

`STRUCT_GET` and `STRUCT_SET` first handle VM-native `*types.Struct`.

If the value is not a native struct, they fall back to `*interp.HostObject`.

Defined scalar values with methods marshal as their underlying primitive unless passed by pointer. Pointer form becomes a `HostObject`. Field `0` is reserved as `Value` and exposes the current primitive for ordinary opcodes.

## Iterators

`Iterator` marks heap values consumable by:

* `RESUME`
* `CORO_DONE`
* `CORO_VALUE`

Iterator contract:

| Method      | Meaning                 |
| ----------- | ----------------------- |
| `Next()`    | advance one item        |
| `Current()` | return current VM value |
| `Done()`    | report exhaustion       |

Iterators that hold refs must also implement `Traceable`.

`MapIterator` yields keys only. Use `MAP_GET` to read values.

`StringIterator` yields UTF-8 codepoints as `i32` without allocating a UTF-32 array.

## Strings

Strings created by interpreter opcodes and the marshaler are interned per `Interpreter`.

Equal string contents share one heap ref while live. The intern table drops the entry when the last ref is released.

## Arrays and Maps

`TypeI1` and `TypeI8` are first-class stack kinds, not element-only types.

`[]i8` arrays store raw bytes as `int8`.

Rules:

* `ARRAY_GET` on `[]i8` returns signed `BoxI8`
* `ARRAY_SET` and `ARRAY_FILL` on `[]i8` truncate to the low byte
* no overflow trap occurs on narrowing
* mask with `& 0xFF` when unsigned `0..255` interpretation is needed

`[]i1` arrays use raw one-byte boolean cells.

Rules:

* `ARRAY_GET` returns `BoxI1`
* `ARRAY_SET` and `ARRAY_FILL` store `val.Bool()`

Use constructors for compound runtime types because they initialize cached metadata and storage used by hot paths.

Common constructors:

```text id="qu4oei"
NewStructType
NewStruct
NewMapType
NewMap
NewMapWithCapacity
NewMapForType
NewIteratorType
```

`NewMapForType` returns primitive-key specializations for:

```text id="l7xppz"
i1
i8
i32
i64
f32
f64
```

Strings and other ref-typed keys use generic `*types.Map` with heap ref identity.

## Dynamic Values

`ref` (`types.TypeRef`) is the VM dynamic “any” type.

There is no separate boxed-any representation. A `Boxed` value is already a self-describing tagged union, so a `ref`-typed slot stores the full `Boxed` value verbatim.

A `ref` slot can hold:

* inline primitives such as `i32`, `i64`, `f32`, `f64`
* heap refs such as strings, arrays, maps, structs, functions, and host objects

Rules:

* `refType.Cast` accepts every type
* `REF_TEST` and `REF_CAST` recover runtime type
* primitive operands use `Boxed.Type()`
* `KindRef` operands use the heap object’s `Type()`
* reference counting is based on the actual runtime `Kind`, not the declared slot type

Storing a primitive into a `ref` slot does not retain. Overwriting a `ref` slot releases only when the displaced runtime value is actually `KindRef`.

This applies uniformly to:

* locals
* globals
* struct fields
* array elements
* map keys
* map values

### Dynamic Containers

Only generic containers can hold dynamic `ref` elements.

| Container               | Dynamic support               |
| ----------------------- | ----------------------------- |
| `*Array`                | can hold `[]ref`              |
| `*Map`                  | can hold `ref` keys or values |
| specialized arrays/maps | cannot hold dynamic values    |

Specialized containers pack raw bits and do not store kind tags.

Generic maps key primitives by value and heap refs by identity.

The ARM64 JIT can move `ref` slots through locals, globals, and upvalues with runtime kind checks and RC updates. Unsupported heap shapes exit to threaded execution.

Go `interface{}` maps to VM `ref` through the marshaler.

## Unbox to Value

`types.Unbox(v Boxed)` converts inline boxed values to concrete `types.Value`.

| Boxed kind | Result      |
| ---------- | ----------- |
| `KindI32`  | `types.I32` |
| `KindI8`   | `types.I8`  |
| `KindI1`   | `types.I1`  |
| `KindI64`  | `types.I64` |
| `KindF32`  | `types.F32` |
| `KindF64`  | `types.F64` |
| `KindRef`  | `types.Ref` |

For `KindRef`, `Unbox` returns only the heap index wrapper. It does not load the heap object.

Use:

```go id="k4c49k"
obj, err := vm.Load(v.Ref())
```

to access the actual heap value.

## Agent Notes

When changing value representation:

* keep `Boxed` as the single stack/global currency
* avoid adding new runtime kinds unless necessary
* preserve `i1`/`i8`/`i32` representation compatibility
* keep boolean results as `i1`
* keep wrong-kind unboxing explicitly unsafe
* make heap spill behavior transparent to bytecode
* keep JIT tag checks aligned with interpreter behavior
* update verifier, interpreter, JIT, marshal, and docs together when kind rules change
* prefer one simple representation over special-case encodings

The best value representation change keeps values self-describing, cheap to move, and easy for both the interpreter and JIT to reason about.
