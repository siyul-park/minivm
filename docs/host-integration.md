# Host Integration

Go host ↔ VM calls, values, heap refs, and reflection.

Heap ownership is defined in `memory-model.md`; boxed layout in `value-representation.md`.

## Layers

| Layer | APIs | Use |
|---|---|---|
| Direct | `Boxed`, `Value`, `HostFunction`, `Alloc`, `Load`, `Retain`, `Release` | hot paths / explicit ownership |
| Reflection | `Marshal`, `Unmarshal`, `Registry`, codecs | setup, tests, structs, maps, slices, functions |

## Host Functions

`HostFunction` bridges `CALL` to Go:

```go
func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error)
```

`NewHostFunction` is the normal constructor; `Typ` and `Fn` remain public.

Rules:

- `params` is valid only during the call;
- non-nil errors stop the current `Run`;
- host functions `MUST NOT` call `vm.Run` recursively.

## Boxed Values

`types.Boxed` is the VM stack word. The agent `MUST` check `Kind()` before unboxing unless the bytecode contract proves the kind.

Wrong-kind unboxing is invalid.

`PopBoxed` returns the raw stack word. For `KindRef`, it transfers stack ownership to the caller; `Load` does not change ownership, and the caller `MUST` release the transferred ref when finished. The caller `MUST` retain first when another ownership is required.

`Pop` returns `types.Value`; for refs it detaches the heap value and releases the stack ref.

## Heap API

```go
addr, err := vm.Alloc(types.String("hello"))
obj, err := vm.Load(addr)
err = vm.Store(addr, types.String("world"))
obj, err = vm.Retain(addr)
err = vm.Release(addr)
```

| API | Contract |
|---|---|
| `Alloc` | creates one owned ref |
| `Load` | reads without ownership change |
| `Store` | replaces value; releases refs owned by old value |
| `Retain` | creates one host-owned ref |
| `Release` | drops one host-owned ref |

Additional rules:

- Allocating an existing ref creates another ownership;
- storing the same concrete pointer or destination ref is a no-op;
- storing a different heap address returns `ErrTypeMismatch`; the agent `MUST` use `Alloc(ref)` to share;
- concrete pointers passed to `Alloc`, `Store`, or `Push` transfer unique ownership and `MUST NOT` already be VM-owned;
- owned refs `MUST` eventually transfer or release;
- leaked host ownership keeps objects alive.

`Store`/`Alloc` dynamically track crossed pointers to reject double ownership. Dynamic functions stored in the heap receive callable dispatch slots and follow normal heap lifetime.

External dynamic functions `MUST` be verified before storage.

## Globals and Locals

`SetGlobal` and `SetLocal` transfer a new valid ref and release the replaced ref.

Invalid heap addresses return `ErrSegmentationFault` and leave the slot unchanged. Assigning the current boxed value is a no-op and transfers no ownership.

The caller `MUST` retain before assignment when it must keep its ownership:

```go
if _, err := vm.Retain(addr); err != nil { return err }
if err := vm.SetGlobal(0, types.BoxRef(addr)); err != nil { return err }
```

## Limits

`WithHeap` sets initial capacity. `WithHeapLimit(n)` sets a hard entry limit; `n <= 0` means unlimited. `Alloc`, `Push`, and `Marshal` return `ErrHeapExhausted`; guest execution wraps it in `RuntimeError`.

## Reflection

`Marshal` and `Unmarshal` use cached per-type codecs. `Unmarshal` writes a VM value into a Go destination. Heap ownership is unchanged by conversion semantics.

| Go | VM |
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

## Related

- `memory-model.md`
- `value-representation.md`
- `verification.md`
