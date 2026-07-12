# Host Integration

Passing values and calls between Go host code and the VM.

## When to Read

Use this document when embedding minivm in Go code, exposing host functions, moving heap references across the host boundary, or using `Marshal` and `Unmarshal`.

For heap ownership, see `docs/memory-model.md`. For boxed value layout, see `docs/value-representation.md`.

## Overview

The Go host and the VM run in the same process, but they use different value representations.

| Layer | Main APIs | Best for |
|---|---|---|
| Direct | `types.Boxed`, `types.Value`, `HostFunction`, `Alloc`, `Load`, `Retain`, `Release` | hot paths and explicit heap control |
| Reflection | `Marshal`, `Unmarshal`, `WithConverter`, `WithMarshaler` | setup data, tests, structs, maps, slices, and functions |

Both layers can be used with the same interpreter.

## Direct Layer

### Host Functions

`HostFunction` is the direct call bridge from bytecode to Go.

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

prog := program.New(instrs, program.WithConstants(fn))
```

Bytecode calls the function with `CONST_GET` and `CALL`.

Rules:

- `params` is valid only during the call
- returning a non-nil error stops the current `Run`
- do not call `vm.Run` recursively from a host function

### Boxed Values

`types.Boxed` is the VM stack word. Check `Kind()` before unboxing unless the bytecode contract already proves the kind.

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
    _ = obj
    _ = err
}
```

Wrong-kind unboxing is invalid and may return garbage.

### Reading Results

Use `PopBoxed` when the caller wants the raw stack word.

```go
v, err := vm.PopBoxed()
if err != nil {
    return err
}
score := v.F64()
```

For scalar values, `PopBoxed` is allocation-free.

For `KindRef`, `PopBoxed` transfers the stack reference to the caller. Resolve it with `Load`, then `Release` it when done. Use `Retain` first if the host needs another independent reference.

Use `Pop` when the caller wants a `types.Value`. For heap values, `Pop` detaches the heap value and releases the stack reference.

### Heap Access

Host code can allocate, load, replace, retain, and release VM heap values.

```go
addr, err := vm.Alloc(types.String("hello"))
obj, err := vm.Load(addr)
err = vm.Store(addr, types.String("world"))
obj, err = vm.Retain(addr)
err = vm.Release(addr)
```

Ownership rules:

- `Alloc` creates an owned heap reference
- `Load` reads without changing ownership
- `Store` replaces the value at an address
- `Retain` creates another host-owned reference
- `Release` drops a host-owned reference
- every successful `Retain` must be matched by `Release`

Leaked host references keep heap objects alive.

### Globals and Locals

`SetGlobal(idx, val)` and `SetLocal(idx, val)` overwrite VM slots.

If `val` is `KindRef`, ownership transfers into the slot. The caller must not release that same reference afterward.

To keep another reference, retain first.

```go
ref, err := vm.Retain(addr)
if err != nil {
    return err
}
err = vm.SetGlobal(0, types.BoxRef(ref))
```

This mirrors `GLOBAL_SET` and `LOCAL_SET`, which consume stack references into slots.

### Dynamic Functions

`Alloc` and `Store` can accept `*types.Function`.

When a function is stored in the heap, the interpreter keeps a callable dispatch slot for that heap address. Bytecode can call the reference with `CALL` or `RETURN_CALL`.

Dynamic functions follow normal heap ownership rules. When no stack, global, closure, object, or host reference keeps the function alive, the heap slot is reclaimed and the callable dispatch slot is removed.

`Alloc` and `Store` do not verify function bytecode. If a dynamic function comes from outside the trusted program builder, verify the bytecode before storing it.

### Resource Limits

`WithHeap(n)` sets the initial heap capacity.

`WithMaxHeap(n)` sets a hard heap-entry limit. Values `n <= 0` mean unlimited.

Allocation order is described in `docs/memory-model.md`; this document only covers host-facing API behavior.

`Alloc`, `Push`, and `Marshal` return heap exhaustion as normal errors. Guest execution wraps heap exhaustion in `RuntimeError`, which unwraps to `ErrHeapExhausted`.

## Reflection Layer

The reflection layer converts ordinary Go values to and from VM values. It is convenient, but it is not the preferred hot path.

Use it for setup data, configuration, tests, ordinary structs, maps, slices, and functions.

### Marshal

`Marshal` converts a Go value into a `types.Value`.

```go
v, err := vm.Marshal(myGoValue)
```

The default marshaler builds and caches one conversion plan per Go type.

### Type Mapping

| Go type | VM type | Notes |
|---|---|---|
| `bool` | `I1` | `false=0`, `true=1` |
| `int8` | `I8` | |
| `int16`, `int32` | `I32` | |
| `int`, `int64` | `I64` | large values may heap-spill |
| `uint8`, `uint16`, `uint32` | `I32` | raw bits preserved |
| `uint`, `uint64`, `uintptr` | `I64` | raw bits preserved |
| `float32` | `F32` | |
| `float64` | `F64` | |
| `string` | `String` ref | heap-allocated and interned by the interpreter |
| `[]bool` | `I1Array` | one byte per element |
| `[]int8`, `[]uint8`, `[]byte` | `I8Array` | raw bits preserved |
| `[]int16`, `[]int32`, `[]uint16`, `[]uint32` | `I32Array` | raw bits preserved for unsigned values |
| `[]int`, `[]int64`, `[]uint`, `[]uint64`, `[]uintptr` | `I64Array` | raw bits preserved for unsigned values |
| `[]float32` | `F32Array` | |
| `[]float64` | `F64Array` | |
| other `[]T` | `*Array` ref | generic fallback |
| `map[K]V` | `*Map` ref | heap-allocated |
| data-only struct | `*Struct` ref | exported fields only |
| struct with methods or unexported fields | `*HostObject` ref | preserves methods or hidden state |
| defined scalar with methods | underlying scalar | keeps primitive fast path |
| `*T` | `T`, `*HostObject`, or `Null` | nil pointer becomes `Null` |
| `func(...)` | `*HostFunction` ref | final `error` return is host-only |
| `interface{}` / `any` | `ref` | dynamic value |
| `types.Value` | passthrough | returned as-is |
| `types.Boxed` | unboxed | `KindRef` resolved with `Load` |
| `time.Time` | `I64` | Unix nanoseconds |
| `time.Duration` | `I64` | normal scalar path |
| `complex64` | `*Struct{Real, Imag F32}` | heap-allocated |
| `complex128` | `*Struct{Real, Imag F64}` | heap-allocated |
| `ValueMarshaler` | custom | `MarshalVM` decides representation |

Nil pointers marshal to `types.Null`. Pointer cycles return `ErrMarshalCycle`. Shared non-cycle pointers are allowed.

### Go Functions

A Go function marshals to `*HostFunction`.

A final `error` return is treated as a host error. If it returns non-nil, the VM call fails with that error.

An exact first `context.Context` parameter is host-only and omitted from the VM function signature. When guest code calls the marshaled function, minivm passes the active `Interpreter.Context`; a nil active context is normalized to `context.Background()`. The same rule applies to exported host-object methods. A `context.Context` in any other position is converted as an ordinary VM argument.

```go
add := func(a, b int32) (int32, error) {
    return a + b, nil
}
fn, err := vm.Marshal(add)
```

A VM `*types.Function`, `*types.Closure`, or live function ref can be unmarshaled into a matching Go function type. Each call marshals arguments, runs the VM function on the same interpreter, then unmarshals results.

```go
var add func(int32, int32) (int32, error)
err := vm.Unmarshal(vmFunction, &add)
result, err := add(2, 3)
```

The Go and VM signatures must map to equal VM function types. A final Go `error` return is host-only: VM traps and bridge errors are returned there. Without a final `error`, those failures panic. Calls must not overlap another `Run` or callable-wrapper call on the same interpreter. Wrappers use the interpreter heap and become invalid when their referenced function no longer survives normal `Release`, `Reset`, or `Close` ownership rules.

When a VM function, closure, or live function ref is unmarshaled into a Go function wrapper, an exact first `context.Context` parameter is host-only and omitted from the VM signature. The wrapper passes it to VM execution, so cancellation and `Interpreter.Context` use the caller's context. A nil caller context and wrappers without a context parameter use `context.Background()`. A `context.Context` in any other position is converted as an ordinary VM argument.

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

- `Marshal` uses the concrete dynamic value
- nil interface values become `Null`
- interface-typed fields, slice elements, and map values become `ref`
- primitive values inside interface-typed slots are heap-boxed because the slot stores `ref`
- `Unmarshal` into `interface{}` returns VM-native `types.Value` values
- use a concrete Go destination type to recover Go-native values
- bytecode can recover dynamic type with `REF_TEST` and `REF_CAST`

Maps with `interface{}` keys heap-box primitive keys during marshal, so equal primitive keys are not deduplicated by value. Prefer concrete key types, or build the map inside the VM.

## Host Objects

`*HostObject` exposes selected Go fields and methods through the same indexed-field protocol used by `*Struct`.

`STRUCT_GET` and `STRUCT_SET` first handle native VM structs, then fall back to host objects.

`Marshal` creates `*HostObject` when a struct has methods, a struct has unexported fields, or a pointer to a defined scalar has methods. Non-pointer defined scalars with methods marshal as their underlying scalar. Use a pointer when VM code needs method access or pointer-receiver mutation.

Host object fields are ordered as exported data fields first, then methods. Fields that cannot map to VM primitives or `types.Value` are skipped. Defined scalar host objects reserve field `0` as `Value`.

A `HostObject` owns an addressable copy of the marshaled Go value. Pointer-receiver methods mutate that copy. To recover the VM-mutated state, unmarshal the host object into a new Go value.

## Unmarshal

`Unmarshal` converts a VM value back into Go.

The destination must be a non-nil pointer.

```go
var n int32
err := vm.Unmarshal(types.I32(42), &n)

var s string
err = vm.Unmarshal(types.String("hello"), &s)
```

Struct fields are matched by name first. Unmatched destination fields fall back to the first unused VM field by position. VM function-typed fields are skipped.

Errors:

- overflow returns `ErrValueOverflow`
- incompatible source/destination kinds return `ErrTypeMismatch`
- invalid destination returns `ErrInvalidUnmarshalTarget`

Unsigned integers use the same VM types as signed integers. Values above the signed maximum preserve raw bits. Signedness is recovered from the Go destination type during `Unmarshal`, or from `_S` / `_U` opcode suffixes in bytecode.

## Value Lifetime

Marshaled references live on the VM heap. This includes strings, arrays, maps, structs, host objects, and host functions.

They remain alive while reachable from the stack, constants, globals, closures, heap objects, or retained host references. They do not survive `vm.Close()` or `vm.Reset()`.

Use marshaled refs before the next reset, or register them as constants or globals.

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

For a type you own, implement `ValueMarshaler` and/or `ValueUnmarshaler`.

For a type you do not own, register a converter with `WithConverter`.

Use `WithMarshaler` to replace the default reflection converter entirely.

Converters apply wherever the registered type appears, including nested fields, slices, and maps. Converters take precedence over built-in conversions. If `Marshal` or `Unmarshal` is nil, that direction returns `ErrUnsupportedMarshalType`.

`WithConverter` has no effect when `WithMarshaler` supplies a custom marshaler.

## Errors

| Error | Meaning |
|---|---|
| `RuntimeError` | guest execution failed; unwraps to the cause and carries frames |
| `types.Error` | guest exception value with code, message, payload, and optional wrapped Go cause |
| `ErrHeapExhausted` | heap allocation exceeded `WithMaxHeap` |
| `ErrMarshalCycle` | pointer graph contains a cycle |
| `ErrUnsupportedMarshalType` | Go type or conversion direction is unsupported |
| `ErrInvalidUnmarshalTarget` | destination is not a non-nil pointer |
| `ErrValueOverflow` | numeric value does not fit destination type |
| `ErrTypeMismatch` | source and destination kinds are incompatible |

Use `errors.Is` for error categories and `errors.As` to inspect structured errors.

Use `interp.TrapCode(err)` or `types.Error.Code()` to map VM traps to source-language exceptions without matching rendered error messages. Code `0` is unclassified. Source-language errors should use `types.ErrorCodeUserBase` and above.

## Maintenance Notes

When changing host integration code:

- keep hot paths on `types.Boxed`
- keep reflection out of performance-sensitive loops
- make ownership transfer explicit
- release every retained reference
- avoid retaining temporary slices or VM stack views
- prefer one clear conversion path over multiple partial paths
- keep custom conversion APIs small
- do not expose implementation details through public errors

## Related Docs

- `docs/memory-model.md` — heap ownership, RC, GC, and host refs
- `docs/value-representation.md` — `types.Boxed`, kinds, and dynamic `ref`
- `docs/instruction-set.md` — host-callable opcodes and dynamic type checks
