# Host Integration

Pass values and calls between Go host code and the VM.

## Overview

The VM and Go host share a process but use different value representations.

| Layer | Use when | API |
|---|---|---|
| Direct | hot host calls, manual heap/ref control | `types.Value`, `types.Boxed`, `HostFunction` |
| Reflect | setup code, ordinary Go structs/maps/functions | `Marshal`, `Unmarshal` |

Both layers can be mixed in one interpreter.

---

## Direct Layer

### `HostFunction`

`HostFunction` is the direct call bridge. Any Go closure with signature `func(*Interpreter, []Boxed) ([]Boxed, error)` becomes callable from bytecode via `CONST_GET` + `CALL`.

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

`HostFunction` is a `types.Value` (`KindRef`). Register it as a constant.

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

Always `Release` what you `Retain`; leaked refs prevent GC collection.

---

## Reflect Layer

### `Marshal`

`Marshal` converts ordinary Go values to `types.Value` using reflection-backed type plans. The default marshaler compiles metadata once per Go type, then reuses that plan for later conversions. Use it for setup data, functions, and config; keep hot calls on the direct layer.

```go
v, err := vm.Marshal(myGoValue)
```

**Type mapping:**

| Go type | VM type | Notes |
|---|---|---|
| `bool` | `I32` | `false=0`, `true=1` |
| `int8`, `int16`, `int32` | `I32` | |
| `int`, `int64` | `I64` | large values may heap-spill |
| `uint8`, `uint16`, `uint32` | `I32` | preserves raw unsigned bits |
| `uint`, `uint64`, `uintptr` | `I64` | preserves raw unsigned bits |
| `float32` | `F32` | |
| `float64` | `F64` | |
| `string` | `String` (ref) | heap-allocated |
| `[]int8`, `[]int16`, `[]int32` | `I32Array` | no heap allocation |
| `[]uint8`, `[]uint16`, `[]uint32` | `I32Array` | preserves raw unsigned bits |
| `[]int`, `[]int64` | `I64Array` | no heap allocation |
| `[]uint`, `[]uint64`, `[]uintptr` | `I64Array` | preserves raw unsigned bits |
| `[]float32` | `F32Array` | no heap allocation |
| `[]float64` | `F64Array` | no heap allocation |
| `[]T` (other) | `*Array` (ref) | elements heap-allocated if ref-typed |
| `map[K]V` | `*Map` (ref) | heap-allocated |
| `struct` (exported fields only, no methods) | `*Struct` (ref) | data-only snapshot |
| `struct` with methods or unexported fields | `*HostObject` (ref) | see Host Objects below |
| defined scalar with methods (e.g. `type Celsius float64`) | underlying scalar | keeps primitive opcode/JIT path |
| `*T` | same as `T`, `*HostObject`, or `Null` if nil | pointer dereferenced; defined scalar pointers with methods become host objects |
| `func(...)` | `*HostFunction` (ref) | see below |
| `types.Value` | passthrough | returned as-is |
| `types.Boxed` | unboxed | `KindRef` resolved via `Load` |

**Nil / null:**

```go
var p *MyStruct = nil
v, _ := vm.Marshal(p) // → types.Null
```

Pointer cycles return `ErrMarshalCycle`. Shared non-cycle pointers are allowed.

### Marshaling Go functions

A Go `func` marshals to `*HostFunction`. A final `error` return is host-only; non-nil values surface as call errors.

```go
add := func(a, b int32) (int32, error) { return a + b, nil }
fn, err := vm.Marshal(add)
// fn is *HostFunction with Params=[I32,I32], Returns=[I32]
```

VM-native types (`types.I32`, `types.F32`, etc.) in Go signatures avoid boxing overhead:

```go
add := func(a, b types.I32) types.I32 { return a + b }
fn, err := vm.Marshal(add)
```

### Host Objects

`*HostObject` exposes Go values with data fields and bound methods through the same indexed-field protocol as `*Struct`. `STRUCT_GET` / `STRUCT_SET` handle native structs first, then use `HostObject` for host values.

```go
type Counter struct{ Count int32 }

func (c *Counter) Bump(n int32) int32 {
    c.Count += n
    return c.Count
}

v, _ := vm.Marshal(Counter{Count: 1})
ho := v.(*interp.HostObject)
// ho.Typ.Fields = [Count: I32, Bump: func(I32) I32]
```

**Routing rules:** `Marshal` creates `*HostObject` when any condition holds:

- A struct type has methods on `T` or `*T`.
- A struct type has unexported fields (would lose info via `*Struct`).
- A pointer to a defined scalar type has methods on `T` or `*T`.

Non-pointer defined scalars with methods marshal as their underlying scalar so numeric and string opcodes keep their normal fast path. Marshal a pointer when VM code needs method access or pointer-receiver mutation.

**Field layout:**

- Exported data fields first, in declaration order. Only fields whose Go kind maps to a VM primitive (`bool`, `int*`, `uint*`, `float*`, `string`) or implements `types.Value` are exposed; others are skipped.
- Defined scalar host objects reserve field `0` as `Value`, the current underlying scalar. Use `STRUCT_GET 0` before primitive opcodes and `STRUCT_SET 0` to update it directly.
- Methods second. Methods whose name collides with an exported data field are skipped.

**Receiver semantics:**

- `Receiver` is an **addressable copy** of the marshaled Go value, owned by the `HostObject`. Pointer-receiver method calls mutate this copy.
- The caller's original Go value is not mutated by VM-side writes. Round-trip via `Unmarshal(ho, &dst)` to recover the current state into a new Go value.

**Field access:** `Field` / `SetField` use compiled field metadata and unsafe offsets against `Receiver`. Methods are pre-bound as `*HostFunction` values on the VM heap and retained via `Refs`. Arbitrary Go function and method calls still use `reflect.Call` because their signatures are not known statically.

### `Unmarshal`

`Unmarshal` converts `types.Value` back to Go. Destination must be a non-nil pointer. The default marshaler reuses the same per-type plans used by `Marshal`.

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

Unsigned Go integers use the same `I32` / `I64` VM types as signed integers. Values above signed max preserve raw bits, so `uint32(math.MaxUint32)` appears as `I32(-1)` and `uint64(math.MaxUint64)` appears as `I64(-1)` inside the VM. Signedness comes from the Go destination type during `Unmarshal`, or from `_S` / `_U` opcode suffixes in bytecode.

---

## Marshaled Value Lifetime

Marshaled refs (strings, arrays, maps, structs, host functions) live on the VM heap and are collected when unreachable from stack, constants, or globals.

Consume marshaled refs before next `Run`, or register them as constants/globals:

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

Override conversion for custom types, schema registries, or reflection-free integration:

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

`WithMarshaler` replaces the default reflection converter for all `Marshal` / `Unmarshal` calls on that interpreter.

---

## Errors

| Error | When |
|---|---|
| `ErrMarshalCycle` | pointer graph contains a cycle |
| `ErrUnsupportedMarshalType` | Go type cannot be converted (e.g. `chan`, `complex`) |
| `ErrInvalidUnmarshalTarget` | destination is not a non-nil pointer |
| `ErrValueOverflow` | numeric value doesn't fit destination type |
| `ErrTypeMismatch` | source and destination kinds are incompatible |
