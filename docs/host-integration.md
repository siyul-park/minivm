# Host Integration

Passing values and calls between Go host code and the VM.

## Summary

The Go host and the VM run in the same process, but they use different value representations.

Use two integration layers:

| Layer   | Use for                                                                | Main APIs                                                                          |
| ------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Direct  | hot paths, manual ownership, precise heap/ref control                  | `types.Boxed`, `types.Value`, `HostFunction`, `Alloc`, `Load`, `Retain`, `Release` |
| Reflect | setup data, ordinary Go structs/maps/functions, convenience conversion | `Marshal`, `Unmarshal`                                                             |

Both layers can be used in the same interpreter.

Default rule:

* use the direct layer for hot code
* use the reflect layer for setup and convenience
* keep ownership explicit
* prefer simple value flow over hidden conversion
* use short, standard names when adding host integration APIs

## Direct Layer

The direct layer works with VM-native values and ownership rules. It is the preferred path for performance-sensitive host integration.

### Host Functions

`HostFunction` is the direct call bridge from bytecode to Go.

Any Go function with this shape can be called from VM bytecode:

```go
func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error)
```

Example:

```go
fn := interp.NewHostFunction(
    &types.FunctionType{
        Params:  []types.Type{types.TypeI32, types.TypeI32},
        Returns: []types.Type{types.TypeI32},
    },
    func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
        a := params[0].I32()
        b := params[1].I32()
        return []types.Boxed{types.BoxI32(a + b)}, nil
    },
)
```

`HostFunction` is a `types.Value` with `KindRef`. Register it as a constant and call it from bytecode with `CONST_GET` + `CALL`.

```go
prog := program.New(instrs, program.WithConstants(fn))
```

Rules:

* `params` is valid only during the call; do not retain it
* returning a non-nil error stops the current `Run`
* do not call `vm.Run` recursively from inside a host function

### Boxed Values

`types.Boxed` is the VM stack word. It is represented as `uint64`.

Check `Kind()` before unboxing.

```go
switch v.Kind() {
case types.KindI32:
    n := v.I32()
case types.KindI64:
    n := v.I64()
case types.KindF32:
    f := v.F32()
case types.KindF64:
    f := v.F64()
case types.KindRef:
    obj, err := vm.Load(v.Ref())
}
```

Wrong-kind unboxing is invalid and may return garbage. Always check the kind first unless the bytecode contract already proves it.

### Reading Results

Use `Pop` when you want a `types.Value`.

Use `PopBoxed` when you want the raw stack word without scalar allocation.

```go
v, err := vm.PopBoxed()
if err != nil {
    return err
}

score := v.F64()
```

For scalar values, `PopBoxed` is allocation-free.

For `KindRef`, `PopBoxed` transfers the stack reference to the caller unchanged. Resolve it with `Load`, then `Release` it when done. Use `Retain` first if you need an additional independent reference.

`Pop` instead detaches the heap value and releases the stack reference itself.

### Heap Access

Host code can allocate, load, replace, retain, and release VM heap values directly.

```go
addr, err := vm.Alloc(types.String("hello"))

obj, err := vm.Load(addr)

err = vm.Store(addr, types.String("world"))

obj, err = vm.Retain(addr)
err = vm.Release(addr)
```

Ownership rules:

* `Alloc` creates a heap reference
* `Load` reads without changing ownership
* `Store` replaces the value at an address
* `Retain` increments the reference count
* `Release` decrements the reference count
* every retained reference must be released

Leaked references keep heap objects alive and prevent collection.

### Globals and Locals

`SetGlobal(idx, val)` and `SetLocal(idx, val)` overwrite VM slots.

If `val` is `KindRef`, ownership transfers into the slot. The caller must not release it afterward.

To keep another reference, retain it before storing.

```go
ref, err := vm.Retain(addr)
if err != nil {
    return err
}

err = vm.SetGlobal(0, types.BoxRef(ref))
```

This mirrors the VM opcodes `GLOBAL_SET` and `LOCAL_SET`, which consume stack references into slots.

### Dynamic Functions

`Alloc` and `Store` can accept `*types.Function`.

When a function is stored in the heap, the interpreter keeps a callable dispatch slot for that heap address. Bytecode can then call the reference with `CALL` or `RETURN_CALL`.

Dynamic functions follow normal heap ownership rules. When no stack, global, closure, object, or host reference keeps the function alive, the heap slot is reclaimed and the callable dispatch slot is removed.

With `WithVerify(true)`, dynamic functions are verified before `Alloc` or `Store` mutates heap state.

### Resource Limits

`WithHeap(n)` sets the initial heap capacity.

`WithMaxHeap(n)` sets a hard heap-entry limit. Values `n <= 0` mean unlimited.

Allocation order:

1. reuse free heap slots
2. run GC if needed
3. return `ErrHeapExhausted` if the live heap still exceeds the limit

`Alloc`, `Push`, and `Marshal` return heap exhaustion as normal errors.

Guest execution wraps heap exhaustion in `RuntimeError`, which unwraps to `ErrHeapExhausted`.

## Reflect Layer

The reflect layer converts ordinary Go values to and from VM values. It is convenient, but it is not the preferred hot path.

Use it for:

* setup data
* configuration
* tests
* ordinary Go structs, maps, slices, and functions
* integration code where reflection overhead is acceptable

### Marshal

`Marshal` converts a Go value into a `types.Value`.

```go
v, err := vm.Marshal(myGoValue)
```

The default marshaler builds and caches a conversion plan per Go type, then reuses that plan for later conversions.

### Type Mapping

| Go type                                               | VM type                       | Notes                                  |
| ----------------------------------------------------- | ----------------------------- | -------------------------------------- |
| `bool`                                                | `I32`                         | `false=0`, `true=1`                    |
| `int8`, `int16`, `int32`                              | `I32`                         |                                        |
| `int`, `int64`                                        | `I64`                         | large values may heap-spill            |
| `uint8`, `uint16`, `uint32`                           | `I32`                         | raw bits preserved                     |
| `uint`, `uint64`, `uintptr`                           | `I64`                         | raw bits preserved                     |
| `float32`                                             | `F32`                         |                                        |
| `float64`                                             | `F64`                         |                                        |
| `string`                                              | `String` ref                  | heap-allocated                         |
| `[]bool`                                              | `I1Array`                     | one byte per element                   |
| `[]int8`, `[]uint8`, `[]byte`                         | `I8Array`                     | raw bits preserved                     |
| `[]int16`, `[]int32`, `[]uint16`, `[]uint32`          | `I32Array`                    | raw bits preserved for unsigned values |
| `[]int`, `[]int64`, `[]uint`, `[]uint64`, `[]uintptr` | `I64Array`                    | raw bits preserved for unsigned values |
| `[]float32`                                           | `F32Array`                    |                                        |
| `[]float64`                                           | `F64Array`                    |                                        |
| `[]T`                                                 | `*Array` ref                  | fallback for other element types       |
| `map[K]V`                                             | `*Map` ref                    | heap-allocated                         |
| data-only struct                                      | `*Struct` ref                 | exported fields only                   |
| struct with methods or unexported fields              | `*HostObject` ref             | preserves behavior or hidden state     |
| defined scalar with methods                           | underlying scalar             | keeps primitive fast path              |
| `*T`                                                  | `T`, `*HostObject`, or `Null` | nil pointer becomes `Null`             |
| `func(...)`                                           | `*HostFunction` ref           | final `error` return is host-only      |
| `interface{}` / `any`                                 | `ref`                         | dynamic value                          |
| `types.Value`                                         | passthrough                   | returned as-is                         |
| `types.Boxed`                                         | unboxed                       | `KindRef` resolved with `Load`         |
| `time.Time`                                           | `I64`                         | Unix nanoseconds                       |
| `time.Duration`                                       | `I64`                         | normal scalar path                     |
| `complex64`                                           | `*Struct{Real, Imag F32}`     | heap-allocated                         |
| `complex128`                                          | `*Struct{Real, Imag F64}`     | heap-allocated                         |
| `ValueMarshaler`                                      | custom                        | `MarshalVM` decides representation     |

Nil pointers marshal to `types.Null`.

```go
var p *MyStruct
v, err := vm.Marshal(p) // types.Null
```

Pointer cycles return `ErrMarshalCycle`. Shared non-cycle pointers are allowed.

### Go Functions

A Go function marshals to `*HostFunction`.

A final `error` return is treated as a host error. If it returns non-nil, the VM call fails with that error.

```go
add := func(a, b int32) (int32, error) {
    return a + b, nil
}

fn, err := vm.Marshal(add)
```

VM-native scalar types can reduce conversion overhead.

```go
add := func(a, b types.I32) types.I32 {
    return a + b
}
```

### Dynamic Interface Values

`interface{}` and named interfaces map to the VM dynamic `ref` type.

```go
got, err := vm.Marshal([]any{int32(1), "x", 2.5})
```

Rules:

* `Marshal` uses the concrete dynamic value
* nil interface values become `Null`
* interface-typed fields, slice elements, and map values become `ref`
* primitive values inside interfaces are heap-boxed
* `Unmarshal` into `interface{}` returns VM-native `types.Value` values
* use a concrete Go destination type to recover Go-native values
* bytecode can recover dynamic type with `REF_TEST` and `REF_CAST`

Limitation: maps with `interface{}` keys heap-box primitive keys during marshal, so equal primitive keys are not deduplicated by value. Prefer concrete key types, or build the map inside the VM.

## Host Objects

`*HostObject` exposes selected Go fields and methods through the same indexed-field protocol used by `*Struct`.

`STRUCT_GET` and `STRUCT_SET` first handle native VM structs, then fall back to host objects.

```go
type Counter struct {
    Count int32
}

func (c *Counter) Bump(n int32) int32 {
    c.Count += n
    return c.Count
}

v, err := vm.Marshal(Counter{Count: 1})
ho := v.(*interp.HostObject)
```

### Routing

`Marshal` creates `*HostObject` when:

* a struct has methods on `T` or `*T`
* a struct has unexported fields
* a pointer to a defined scalar has methods

Non-pointer defined scalars with methods marshal as their underlying scalar. This keeps numeric and string opcodes on their normal fast path.

Use a pointer when VM code needs method access or pointer-receiver mutation.

### Layout

Host object fields are ordered as:

1. exported data fields, in declaration order
2. methods

Only fields that map to VM primitives or implement `types.Value` are exposed. Other fields are skipped.

Defined scalar host objects reserve field `0` as `Value`, which stores the current underlying scalar.

Methods with names that collide with exported fields are skipped.

### Receiver Semantics

A `HostObject` owns an addressable copy of the marshaled Go value.

Pointer-receiver methods mutate that copy, not the caller’s original Go value.

To recover the VM-mutated state, unmarshal the host object into a new Go value.

```go
var out Counter
err := vm.Unmarshal(ho, &out)
```

### Field Access

`Field` and `SetField` use compiled field metadata and unsafe offsets against the host object receiver.

Methods are pre-bound as `*HostFunction` values and retained through `Refs`.

Arbitrary Go function and method calls still use `reflect.Call`.

## Unmarshal

`Unmarshal` converts a VM value back into Go.

The destination must be a non-nil pointer.

```go
var n int32
err := vm.Unmarshal(types.I32(42), &n)

var s string
err = vm.Unmarshal(types.String("hello"), &s)

var out MyStruct
err = vm.Unmarshal(vmStruct, &out)
```

Struct fields are matched by name first. Unmatched destination fields fall back to the first unused VM field by position. VM function-typed fields are skipped.

Errors:

* overflow returns `ErrValueOverflow`
* incompatible source/destination kinds return `ErrTypeMismatch`
* invalid destination returns `ErrInvalidUnmarshalTarget`

Unsigned integers use the same VM types as signed integers. Values above the signed maximum preserve raw bits. Signedness is recovered from the Go destination type during `Unmarshal`, or from `_S` / `_U` opcode suffixes in bytecode.

## Value Lifetime

Marshaled references live on the VM heap.

This includes strings, arrays, maps, structs, host objects, and host functions.

They remain alive while reachable from:

* stack
* constants
* globals
* closures
* heap objects
* retained host references

They do not survive `vm.Close()` or `vm.Reset()`.

Use marshaled refs before the next reset, or register them as constants/globals.

```go
v, err := vm.Marshal(myStruct)
if err != nil {
    return err
}

addr, err := vm.Alloc(v)
if err != nil {
    return err
}

err = vm.Push(types.BoxRef(addr))
if err != nil {
    return err
}

err = vm.Run(ctx)
```

Or register values before creating the interpreter:

```go
prog := program.New(instrs, program.WithConstants(marshaledValue))
```

## Custom Conversion

Use custom conversion when the default reflection mapping is not the desired VM representation.

### Owned Types

For a type you own, implement `ValueMarshaler` and/or `ValueUnmarshaler`.

```go
type Point struct {
    X int32
    Y int32
}

func (p Point) MarshalVM(vm *interp.Interpreter) (types.Value, error) {
    return types.String(fmt.Sprintf("%d,%d", p.X, p.Y)), nil
}

func (p *Point) UnmarshalVM(vm *interp.Interpreter, v types.Value) error {
    s, ok := v.(types.String)
    if !ok {
        return fmt.Errorf("want string, got %T", v)
    }

    _, err := fmt.Sscanf(string(s), "%d,%d", &p.X, &p.Y)
    return err
}
```

Nested fields, slice elements, and map values use the same custom conversion.

Implement both directions for round-trip support. If a direction is missing, that direction returns `ErrUnsupportedMarshalType`.

Custom producer types may marshal to heap values that implement `types.Iterator`. Bytecode consumes iterators with `RESUME`, `CORO_DONE`, and `CORO_VALUE`, the same opcodes used for coroutine handles.

If an iterator retains heap references, it must implement `types.Traceable`.

### External Types

For a type you do not own, register a converter with `WithConverter`.

```go
ipType := reflect.TypeOf(net.IP{})

vm := interp.New(prog, interp.WithConverter(ipType, interp.Converter{
    VMType: types.TypeString,
    Marshal: func(_ *interp.Interpreter, v any) (types.Value, error) {
        return types.String(v.(net.IP).String()), nil
    },
    Unmarshal: func(_ *interp.Interpreter, val types.Value, dst any) error {
        s, ok := val.(types.String)
        if !ok {
            return fmt.Errorf("want string, got %T", val)
        }

        *dst.(*net.IP) = net.ParseIP(string(s))
        return nil
    },
}))
```

Converters apply wherever the registered type appears, including nested fields, slices, and maps.

Converters take precedence over built-in conversions. They can override default behavior, such as mapping `time.Time` to seconds instead of nanoseconds.

If `Marshal` or `Unmarshal` is nil, that direction returns `ErrUnsupportedMarshalType`.

`WithConverter` has no effect when `WithMarshaler` supplies a custom marshaler.

### Custom Marshaler

Use `WithMarshaler` to replace the default reflection converter entirely.

```go
type marshaler struct{}

func (m *marshaler) MarshalValue(vm *interp.Interpreter, v any) (types.Value, error) {
    // custom logic
}

func (m *marshaler) UnmarshalValue(vm *interp.Interpreter, v types.Value, dst any) error {
    // custom logic
}

vm := interp.New(prog, interp.WithMarshaler(&marshaler{}))
```

Use a custom marshaler for schema registries, reflection-free integration, or complete conversion control.

## Errors

| Error                       | Meaning                                                                          |
| --------------------------- | -------------------------------------------------------------------------------- |
| `RuntimeError`              | guest execution failed; unwraps to the cause and carries frames                  |
| `types.Error`               | guest exception value with code, message, payload, and optional wrapped Go cause |
| `ErrHeapExhausted`          | heap allocation exceeded `WithMaxHeap`                                           |
| `ErrMarshalCycle`           | pointer graph contains a cycle                                                   |
| `ErrUnsupportedMarshalType` | Go type or conversion direction is unsupported                                   |
| `ErrInvalidUnmarshalTarget` | destination is not a non-nil pointer                                             |
| `ErrValueOverflow`          | numeric value does not fit destination type                                      |
| `ErrTypeMismatch`           | source and destination kinds are incompatible                                    |

Use `errors.Is` for error categories:

```go
errors.Is(err, interp.ErrHeapExhausted)
errors.Is(err, interp.ErrDivideByZero)
```

Use `errors.As` to inspect structured errors:

```go
var runtimeErr *interp.RuntimeError
if errors.As(err, &runtimeErr) {
    frames := runtimeErr.Frames
    _ = frames
}
```

Use `interp.TrapCode(err)` or `types.Error.Code()` to map VM traps to source-language exceptions without matching rendered error messages.

Code `0` is unclassified. Source-language errors should use `types.ErrorCodeUserBase` and above.

## Agent Notes

When changing host integration code:

* keep hot paths on `types.Boxed`
* keep reflection out of performance-sensitive loops
* make ownership transfer explicit
* release every retained reference
* avoid retaining temporary slices or VM stack views
* prefer one clear conversion path over multiple partial paths
* keep custom conversion APIs small
* do not expose implementation details through public errors
* prefer short, standard names such as `value`, `boxed`, `addr`, `ref`, `field`, `method`, `marshal`, and `convert`

The best host integration is explicit at the boundary, simple in the public API, and predictable about ownership.
