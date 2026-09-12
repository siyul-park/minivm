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
| Reflection | `Marshal`, `Unmarshal`, `Registry`, `WithCodec`, `WithMarshaler`, `WithUnmarshaler` | setup data, tests, structs, maps, slices, and functions |

Both layers can be used with the same interpreter.

## Direct Layer

### Host Functions

`HostFunction` is the direct call bridge from bytecode to Go.

Its `Typ` and `Fn` fields are public. A struct literal and `NewHostFunction`
establish the same value; the constructor is the concise common path.

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
- `Alloc` of an existing `types.Ref` or `KindRef` creates another ownership of the same address
- `Load` reads without changing ownership
- `Store` replaces the value at an address, releases refs owned by the old value, and finalizes its external resources
- function, closure, and coroutine slots are immutable because runtime frames
  borrow their code, captures, and suspension state
- storing the same concrete pointer or the destination's own `types.Ref` /
  `KindRef` is a no-op
- storing a different heap address returns `ErrTypeMismatch`; use
  `Alloc(existingRef)` to create another ownership of one object
- concrete pointer values passed to `Alloc`, `Store`, or `Push` transfer unique
  ownership and must not already be owned by the interpreter; use an existing
  ref when sharing one object. The interpreter answers this from an index of the
  pointers that have crossed `Alloc`, `Store`, `Push`, `Load`, `Retain`, or
  `Pop`, so the check costs one lookup no matter how large the heap is
- `Retain` creates another host-owned reference
- `Release` drops a host-owned reference
- every owned reference from `Alloc` or `Retain` must eventually be transferred or released

Leaked host references keep heap objects alive. Releasing an address does not
invalidate another ownership created by `Alloc` or `Retain`.

### Globals and Locals

`SetGlobal(idx, val)` and `SetLocal(idx, val)` overwrite VM slots.

If `val` is a different valid `KindRef`, ownership transfers into the slot. The
caller must not release that same ownership afterward. Invalid heap addresses
return `ErrSegmentationFault` without changing the slot. Assigning the slot's
current boxed value is a no-op; in that case no ownership transfers, and the
caller remains responsible for any ownership it already holds.

To keep another reference after a different-value assignment, retain first.

```go
_, err := vm.Retain(addr)
if err != nil {
    return err
}
err = vm.SetGlobal(0, types.BoxRef(addr))
```

This mirrors `GLOBAL_SET` and `LOCAL_SET`, which consume stack references into slots.

### Dynamic Functions

`Alloc` and `Store` can accept `*types.Function`.

When a function is stored in the heap, the interpreter keeps a callable dispatch slot for that heap address. Bytecode can call the reference with `CALL` or `RETURN_CALL`.

Dynamic functions follow normal heap ownership rules. When no stack, global, closure, object, or host reference keeps the function alive, the heap slot is reclaimed and the callable dispatch slot is removed.

`Alloc` and `Store` do not verify function bytecode. If a dynamic function comes from outside the trusted program builder, verify the bytecode before storing it.

### Resource Limits

`WithHeap(n)` sets the initial heap capacity.

`WithHeapLimit(n)` sets a hard heap-entry limit. Values `n <= 0` mean unlimited.

Allocation order is described in `docs/memory-model.md`; this document only covers host-facing API behavior.

`Alloc`, `Push`, and `Marshal` return heap exhaustion as normal errors. Guest execution wraps heap exhaustion in `RuntimeError`, which unwraps to `ErrHeapExhausted`.

## Reflection Layer

`Marshal` and `Unmarshal` convert ordinary Go values through cached per-type codecs. Use them for setup data, tests, structs, maps, slices, and functions; use direct APIs for hot paths.

| Go value | VM representation |
|---|---|
| `bool` | `I1` |
| `int8` | `I8` |
| `int16` / `int32` / `uint8` / `uint16` / `uint32` | `I32` |
| `int` / `int64` / `uint` / `uint64` / `uintptr` | `I64` |
| `float32` / `float64` | `F32` / `F64` |
| `string` | string ref |
| `[]T` | live host-array view |
| `map[K]V` | live host-map view |
| exported struct | VM struct |
| struct with unexported fields | host-struct view |
| function | host-function ref |
| `any` | ref |
| `types.Value` | passthrough |
| `types.Boxed` | unboxed value |

`Unmarshal` writes a VM value into a Go destination using the same codec cache. `Marshal` and `Unmarshal` preserve the declared conversion contract; they do not change heap ownership rules.

## Limits

`WithHeap` sets initial capacity. `WithHeapLimit` sets a hard entry limit; `<= 0` means unlimited. Allocation errors are returned as `ErrHeapExhausted`. Guest execution wraps the same cause in `RuntimeError`.

## Related Docs

- `memory-model.md`
- `value-representation.md`
- `verification.md`
