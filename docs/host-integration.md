# Host Integration

How to pass values between Go host code and the VM.

## Overview

VM and Go host share same process but use different value representations. Two layers:

- **Direct** — use `types.Value` / `types.Boxed` / `HostFunction` directly, no reflection. Fast path for hot code.
- **Reflect** — use `Marshal` / `Unmarshal` to convert ordinary Go types automatically. Convenient for setup code.

Both layers are available simultaneously. Use direct for host functions called in tight loops; use marshal for one-off conversions of complex Go data.

---

## Direct Layer

### `HostFunction`

`HostFunction` is the primary bridge. Any Go closure with signature `func(*Interpreter, []Boxed) ([]Boxed, error)` becomes callable from bytecode via `CONST_GET` + `CALL`.

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

`HostFunction` is a `types.Value` (`KindRef`). Register it as a constant:

```go
prog := program.New(instrs, program.WithConstants(fn))
```

Rules:

- `params` slice valid only for duration of call; do not retain it.
- Returning non-nil `error` stops current `Run` call and surfaces as that error.
- Do not call `vm.Run` recursively from inside a host function.

### Working with `Boxed` directly

`types.Boxed` is `uint64`. Unbox with typed accessors after checking `Kind()`:

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

Wrong-kind unboxing returns garbage. Always check `Kind()` first.

### Heap access from host functions

```go
// allocate a value on the VM heap
addr, err := vm.Alloc(types.String("hello"))

// load an object by heap address
obj, err := vm.Load(addr)

// overwrite in place
err = vm.Store(addr, types.String("world"))

// manual retain / release for long-lived references
obj, err = vm.Retain(addr)   // increments RC
err = vm.Release(addr)        // decrements RC; free when 0
```

Always `Release` what you `Retain`. Leaked refs prevent GC collection.

---

## Reflect Layer

### `Marshal`

`Marshal` converts an ordinary Go value to `types.Value` using reflection. Use for setup code — marshaling data structures, functions, configs — not in hot call paths.

```go
v, err := vm.Marshal(myGoValue)
```

**Type mapping:**

| Go type | VM type | Notes |
|---|---|---|
| `bool` | `I32` | `false=0`, `true=1` |
| `int8`, `int16`, `int32` | `I32` | |
| `int`, `int64` | `I64` | large values may heap-spill |
| `uint8`, `uint16` | `I32` | |
| `uint`, `uint32`, `uint64` | `I64` | error if > `MaxInt64` |
| `float32` | `F32` | |
| `float64` | `F64` | |
| `string` | `String` (ref) | heap-allocated |
| `[]int32` | `I32Array` | no heap allocation |
| `[]int64` | `I64Array` | no heap allocation |
| `[]float32` | `F32Array` | no heap allocation |
| `[]float64` | `F64Array` | no heap allocation |
| `[]T` (other) | `*Array` (ref) | elements heap-allocated if ref-typed |
| `map[K]V` | `*Map` (ref) | heap-allocated |
| `struct` | `*Struct` (ref) | exported fields only |
| `*T` | same as `T`, or `Null` if nil | pointer dereferenced |
| `func(...)` | `*HostFunction` (ref) | see below |
| `types.Value` | passthrough | returned as-is |
| `types.Boxed` | unboxed | `KindRef` resolved via `Load` |

**Nil / null:**

```go
var p *MyStruct = nil
v, _ := vm.Marshal(p) // → types.Null
```

**Pointer cycles** return `ErrMarshalCycle`. Shared non-cycle pointers are allowed.

### Marshaling Go functions

A Go `func` value marshals to `*HostFunction`. Final return may be `error`; non-nil surfaces as host-call error, not passed to VM.

```go
add := func(a, b int32) (int32, error) { return a + b, nil }
fn, err := vm.Marshal(add)
// fn is *HostFunction with Params=[I32,I32], Returns=[I32]
```

VM-native types (`types.I32`, `types.F32`, etc.) used directly in Go func signatures are recognized without boxing overhead:

```go
add := func(a, b types.I32) types.I32 { return a + b }
fn, err := vm.Marshal(add)
```

### `Unmarshal`

`Unmarshal` converts a `types.Value` back to a Go value. Destination must be a non-nil pointer.

```go
var n int32
err := vm.Unmarshal(types.I32(42), &n) // n = 42

var s string
err = vm.Unmarshal(types.String("hello"), &s) // s = "hello"

var out MyStruct
err = vm.Unmarshal(vmStruct, &out) // struct fields matched by name
```

**Struct field matching:** fields matched by name first; unmatched destination fields fall back to first unused VM field by position. VM function-typed fields are skipped.

**Overflow and mismatch** return `ErrValueOverflow` or `ErrTypeMismatch`.

---

## Marshaled Value Lifetime

Values from `Marshal` that contain refs (strings, arrays, maps, structs, host functions) are heap-allocated on the VM heap. GC'd when unreachable from stack, constants, or globals.

Consume marshaled values before next `Run` or register them as constants/globals:

```go
// push to stack before running
v, _ := vm.Marshal(myStruct)
addr, _ := vm.Alloc(v)
vm.Push(types.BoxRef(addr))
vm.Run(ctx)

// or register as constant before creating interpreter
prog := program.New(instrs, program.WithConstants(marshaledValue))
```

Marshaled refs do not survive `vm.Close()` or `vm.Reset()`.

---

## Custom Marshaler

Override conversion logic — for custom types, schema registry, or to replace reflection entirely:

```go
type myMarshaler struct{}

func (m *myMarshaler) MarshalValue(vm *interp.Interpreter, v any) (types.Value, error) {
    // custom logic
}

func (m *myMarshaler) UnmarshalValue(vm *interp.Interpreter, v types.Value, dst any) error {
    // custom logic
}

vm := interp.New(prog, interp.WithMarshaler(&myMarshaler{}))
```

`WithMarshaler` replaces default reflection-based converter for all `Marshal` / `Unmarshal` calls on that interpreter.

---

## Errors

| Error | When |
|---|---|
| `ErrMarshalCycle` | pointer graph contains a cycle |
| `ErrUnsupportedMarshalType` | Go type cannot be converted (e.g. `chan`, `complex`) |
| `ErrInvalidUnmarshalTarget` | destination is not a non-nil pointer |
| `ErrValueOverflow` | numeric value doesn't fit destination type |
| `ErrTypeMismatch` | source and destination kinds are incompatible |
