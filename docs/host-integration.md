# Host Integration

Go host to VM calls, values, and heap refs, for embedders choosing among the codec, host-function, and manual-ref APIs.

Heap ownership is defined in `memory-model.md`; boxed layout in `value-representation.md`.

## Layers

Use the first layer that fits; descend only for the stated need.

| Order | Layer | APIs | Use |
|---|---|---|---|
| 1 | Codec | `Registry`, `Interpreter.Marshal`, `Interpreter.Unmarshal` | Go values and funcs in either direction; the default path |
| 2 | Host function | `HostFunction`, `NewHostFunction`, `types.Boxed` | hot paths and calls that need no conversion |
| 3 | Manual refs | `Alloc`, `Retain`, `Release`, `Load`, `Store` | holding refs across calls |

Runnable examples for each layer are in `interp/example_test.go`.

## Codec

`Marshal` and `Unmarshal` use cached per-type codecs. A panic in a conversion returns as an error in both directions.

| Direction | Go | VM |
|---|---|---|
| `Marshal` | func | `HostFunction`; a `context.Context` first param is the active `Run` context and is not a VM param; a trailing `error` result is not a VM result |
| `Unmarshal` | VM function | typed Go func, called through `invoke` |

A `Marshal`ed func becomes callable by `Alloc` then `SetGlobal`, or as a program constant.

A call into a `Marshal`ed func goes through `reflect.Value.Call`. The per-call scratch (argument slots, argument list, result holders, decoder and encoder) is pooled and reused, and primitive params and results convert without boxing. A call still allocates for the reflected result, the result slice, and anything the conversion must build: string results, i64 results too wide to box, extra results, and slice, map, pointer, struct and function params. A `HostFunction` written directly skips the reflection and the conversion, so it is the cheaper layer on a path that calls per iteration; `benchmarks.md` owns the measured cost.

An unmarshalled Go func returns a failed call as its trailing `error`. A func type without a trailing `error` panics on failure, so typed callbacks `SHOULD` declare one.

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

## Ownership

| API | Contract |
|---|---|
| `Marshal` | transfers the heap refs the result holds to the caller |
| `Unmarshal` | borrows the VM value; the Go value holds no VM ref |
| `HostFunction.Fn` params | borrowed for the call |
| `HostFunction.Fn` results | transfer one owned ref per `KindRef` result to the VM |
| `Alloc` | transfers a concrete value to a new slot and returns one caller-owned ref; an existing ref gains an owner |
| `Retain` | adds one caller-owned ref |
| `Release` | drops one caller-owned ref |
| `Load` | borrows; the count is unchanged |
| `Store` | transfers a concrete value of the slot's representation (Go type and declared type; a scalar cell may change scalar kind) to the slot and releases the replaced value; any other value is `ErrTypeMismatch` |
| `Push` | transfers the value or its ref to the stack |
| `Pop` | releases the stack ref and returns the detached value |
| `PopBoxed` | transfers the stack ref to the caller |
| `SetGlobal` / `SetLocal` | transfer a different ref to the slot and release the replaced ref; `SetLocal` inside a host function or a finalizer is `ErrHostCall`, since the optimizer assumes only a call writes a caller local, and `SetGlobal` inside a finalizer is too |
| `Global` / `Local` / `Const` | borrow; the slot keeps its ref |

A value from `Marshal` owns the refs of its fields until the VM takes it. The caller `MUST` hand it to `Push`, `Alloc`, or `Store`, or drop it with `Alloc` then `Release`. Values with no refs, such as scalars, strings, slices, maps, and funcs, need nothing.

`Pop` frees the refs a value holds with its last reference, so read them before `Pop` or `Retain` them.

## Host Functions

`HostFunction` bridges `CALL` to Go:

```go
func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error)
```

`NewHostFunction` constructs it; `Typ` and `Fn` are public. `params` is call-scoped and borrowed; returning a param hands its ref on as a result. Host functions `MUST NOT` call `vm.Run`.

A value's `Close` (`io.Closer`) runs when the VM frees the value, from an instruction that releases, such as a dropped global or array element, from `Reset`, or from a collection, and so can run in the middle of such an instruction. It runs as host code: `SetLocal` inside it is `ErrHostCall`, as in a host function, and so are `SetGlobal` and a call of a VM function, which a host function may make. The optimizer keeps locals and globals across a release, so `Close` `MUST NOT` write them. A panic in `Close` leaves the interpreter's host state as it was.

A non-nil error ends the call and is raised as a VM trap with code `TrapCodeHostError`. A guest `try` region covering the call catches it as an `Error` value whose `Frames` are those an uncaught error would report. An uncaught error ends `Run` with a `*RuntimeError` that wraps it, so `errors.Is` finds the host error.

Calling a Go func bound to a VM function from inside a host function runs the VM function nested on the same interpreter. The nested call sees the host function's arguments untouched, uses the stack above them, and takes the context the Go func receives. A trap or cancellation in the nested call returns as the Go func's error, and the interpreter returns to the state the host function saw. The host function decides whether to return that error: if it does, the outer call raises it as above. A nested call cannot yield: coroutines do not cross the host boundary. Inside a finalizer the call is `ErrHostCall` (see `Close` above).

`ErrInterpreterBusy` is returned only when `Run` is active and no host function is running, for example from another goroutine or a hook.

## Boxed Values

`types.Boxed` is the VM stack word. Callers `MUST` check `Kind()` before unboxing unless the contract proves the kind.

Wrong-kind unboxing is invalid.

`PopBoxed` returns the raw stack word. For `KindRef` the caller `MUST` release the transferred ref when finished, or `Retain` first when another ownership is required. `Load` does not change ownership. A value from `Load` is borrowed: the caller `MUST NOT` change its type or storage (for an array, its length, capacity, or backing array) except through `Store`, since the optimizer reads arrays and guards shapes on that representation.

A hook's `Local` and `SetLocal` are exact only on threaded code. On optimized code (bytecode `optimize.O3` or native), locals are not program semantics: a pass or the JIT may hold or drop a local's value, so a hook's `Local` need not see the program's value and its `SetLocal` need not change the program's computation.

## Heap API

```go
addr, err := vm.Alloc(types.String("hello"))
obj, err := vm.Load(addr)
err = vm.Store(addr, types.String("world"))
obj, err = vm.Retain(addr)
err = vm.Release(addr)
```

Additional rules:

- Allocating an existing ref adds ownership; storing the same pointer/address is a no-op.
- Storing another heap address returns `ErrTypeMismatch`; share it with `Alloc(ref)`.
- Concrete pointers passed to `Alloc`, `Store`, or `Push` transfer unique ownership and `MUST NOT` already be VM-owned.
- Owned refs `MUST` transfer or release; leaked host ownership keeps objects alive.

`Store`/`Alloc` track crossed pointers to reject double ownership. Heap-stored dynamic functions receive callable dispatch slots and follow normal heap lifetime.

External dynamic functions `MUST` be verified before storage.

## Globals and Locals

Invalid heap addresses return `ErrSegmentationFault` and leave the slot unchanged. Assigning the current boxed value is a no-op and transfers no ownership.

The caller `MUST` retain before assignment when it must keep its ownership:

```go
if _, err := vm.Retain(addr); err != nil { return err }
if err := vm.SetGlobal(0, types.BoxRef(addr)); err != nil { return err }
```

## Limits

`WithHeap` sets initial capacity. `WithHeapLimit(n)` sets an entry limit; `n <= 0` means unlimited. Deoptimization boxes values native code held unboxed and may pass the limit to do so; the next allocation that needs a new heap entry then fails. `New`, `Pool.Get`, `Alloc`, `Push`, and `Marshal` return `ErrHeapExhausted`; guest execution wraps it in `RuntimeError`.

## Related

- `memory-model.md`
- `value-representation.md`
- `verification.md`
