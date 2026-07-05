# Memory Model

How minivm heap storage, reference counting, GC, and reference ownership work.

## Summary

minivm uses stable heap indices, manual reference counting, and a cycle-collecting mark-and-sweep GC.

Only `KindRef` values participate in reference counting. Primitive boxed values are copied by value and ignored by RC.

Default design rule:

* keep ownership explicit
* keep retain/release symmetric
* keep heap indices stable
* keep `release` iterative
* prefer simple local ownership rules over hidden lifetime behavior
* use short, standard names such as `addr`, `ref`, `heap`, `rc`, `free`, `root`, and `mark`

## Agent Fast Path

Read this before editing code that allocates, retains, releases, loads, stores, or moves refs.

Important files include:

* `interp/threaded.go`
* `interp/host.go`
* `interp/interp.go`
* `types/array.go`
* `types/struct.go`
* `types/map.go`

Checklist:

* only `KindRef` values use RC
* `heap[0]` is always `Null`
* every `retain` must have a matching `release`
* stack, global, local, and upvalue ownership changes must be symmetric
* ref-overwriting operations must release the old ref
* ref-exposing operations must retain the exposed ref
* `release` must stay iterative, not recursive
* never keep pointers into `heap` across allocation
* keep integer heap indices instead
* refs from a pooled interpreter are invalid after `Pool.Put`, because `Reset` clears the heap

## Heap Structure

`Interpreter` stores heap state in parallel slices.

```go id="4qcsws"
heap []types.Value // object at index
rc   []int         // reference count; 0 means free
free []int         // reusable indices
```

Allocation returns a stable integer heap index.

Heap indices never move. This is required because `KindRef` values store raw heap indices in stack slots, constants, globals, closures, and heap objects.

## Reference Counting

Reference counting is handled manually by threaded handlers and host APIs.

| Operation                         | RC behavior                                        |
| --------------------------------- | -------------------------------------------------- |
| push ref to stack                 | retain ref                                         |
| pop or consume ref from stack     | release ref                                        |
| `DUP` ref                         | retain ref                                         |
| `DROP` ref                        | release ref                                        |
| store ref to local/global/upvalue | retain new ref, release old ref                    |
| overwrite ref field/element       | retain or transfer new ref, release old ref        |
| map replace/delete/clear          | release map-owned refs                             |
| `CLOSURE_NEW`                     | transfer popped function and upvalues into closure |

`retain(addr)` increments `rc[addr]`.

`release(addr)` decrements `rc[addr]`. When the count reaches zero, it:

1. collects nested refs from `Traceable.Refs`
2. calls `Close` if the value implements `io.Closer`
3. clears the heap slot
4. appends the address to `free`
5. repeats for nested refs with an explicit work stack

`release` must remain iterative to avoid Go stack overflow on deep object graphs.

## Traceable Values

Heap objects that can contain refs implement `types.Traceable`.

```go id="dzvnvz"
Refs(dst []types.Ref) []types.Ref
```

Contract:

* append nested refs to `dst`
* return `dst` unchanged when there are no nested refs
* do not allocate unless a child ref exists
* preserve append-only behavior

This contract lets `release` and GC reuse caller-owned scratch storage.

## Allocation

`alloc(val types.Value)` creates one owned heap reference.

Allocation order:

1. reuse an index from `free`
2. if `WithMaxHeap(n)` is set and heap length reached `n`, run GC
3. reuse a slot freed by GC if available
4. if still at the hard limit, raise `ErrHeapExhausted`
5. append into existing capacity if possible
6. if full, run GC
7. reuse a slot freed by GC if available
8. otherwise grow `heap` and `rc`, then append

`WithHeap(n)` sets initial heap capacity only.

`WithMaxHeap(n)` sets a hard heap entry limit. Values `n <= 0` mean unlimited.

The max limit is checked after free-list reuse and GC, so collectable objects do not block future allocations.

Public host APIs that allocate, such as `Alloc`, `Push`, and `Marshal`, return `ErrHeapExhausted` instead of leaking the allocator panic.

## GC

GC is mark-and-sweep with the sign bit of `rc` used as the mark state. There is no separate mark array.

GC runs when the heap is full and `free` is empty.

### 1. Mark live slots as unreachable

```text id="0fmabs"
for each heap slot except 0:
    if rc[j] != 0:
        rc[j] = -abs(rc[j])
```

### 2. Trace roots

Roots include:

* stack values
* constants
* globals
* active frames
* coroutine and closure refs reachable from frames

For each root ref:

```text id="o1cfp3"
if rc[addr] < 0:
    rc[addr] = -rc[addr]
    trace nested refs through Traceable.Refs
```

### 3. Sweep

```text id="klostk"
for each heap slot:
    if rc[j] < 0:
        heap[j] = nil
        rc[j] = 0
        free.append(j)
```

Properties:

* handles reference cycles
* uses O(1) extra mark storage
* does not compact
* keeps heap indices stable
* pause cost is proportional to heap size

## Invariants

### Heap index 0 is always null

`heap[0]` is reserved for `Null`.

`interp.New` initializes index `0` with RC `1` before user code runs.

Rules:

* never free it
* never put it in `free`
* `BoxedNull` is `BoxRef(0)`

### Only refs use RC

Primitive boxed values do not participate in reference counting.

Ignored by RC:

* `KindI32`
* `KindI64`
* `KindF32`
* `KindF64`

Tracked by RC:

* `KindRef`

### RC must be symmetric

Every retained ref must be released exactly once.

Asymmetry causes:

* premature collection if released too much
* memory leaks if released too little

### Heap indices are stable

Do not keep pointers into `heap` across any operation that may allocate.

Allocation can grow the backing slice and invalidate pointers.

Keep integer heap addresses instead.

### Ref ownership must be explicit

When an operation moves a ref, document and implement whether it:

* copies and retains
* consumes and releases
* transfers ownership without retain/release
* overwrites and releases the old value

Prefer one clear ownership rule per opcode or API.

### Closure frames track two refs

A frame stores both:

| Field  | Meaning                                                        |
| ------ | -------------------------------------------------------------- |
| `addr` | function/template heap index used for code, profiling, and JIT |
| `ref`  | callable heap ref released on return                           |

For plain functions, `addr == ref`.

For closures, they differ:

* `addr` points to the function template
* `ref` points to the closure instance

Profiling and JIT use `addr`. Lifetime release uses `ref`.

A closure keeps its function template and captured upvalues alive until the closure RC reaches zero.

## Host Access

Host functions use `Interpreter` APIs to work with heap values.

```go id="hrhzvk"
addr, err := vm.Alloc(val)

obj, err := vm.Load(addr)

err = vm.Store(addr, val)

obj, err = vm.Retain(addr)

err = vm.Release(addr)
```

Rules:

* `Alloc` creates an owned heap ref
* `Load` reads an object without changing ownership
* `Store` overwrites an existing heap slot
* `Retain` creates an additional host-owned ref
* `Release` drops a host-owned ref
* every successful `Retain` must be matched by `Release`

Leaked host refs keep objects alive.

`Marshal` may allocate nested heap refs while converting Go values. Those refs belong to the interpreter heap. Consume them through VM APIs such as `Push` or `Alloc`, or let `Close` / `Reset` discard temporary allocations.

## Pool Lifetime

Each `Interpreter` owns its heap.

A pooled interpreter is reset when returned to the pool. After `Pool.Put`, all refs from that interpreter are invalid.

Do not store heap refs from a borrowed interpreter beyond its borrow lifetime.

## I64 Heap Spilling

Most `i64` values are stored inline in `types.Boxed`.

Large `int64` values outside the NaN-boxable range spill to the heap as `types.I64`.

```go id="v4r4gq"
func (i *Interpreter) boxI64(val int64) types.Boxed {
    if types.IsBoxable(val) {
        return types.BoxI64(val)
    }

    addr := i.alloc(types.I64(val))
    return types.BoxRef(addr)
}
```

Approximate inline range:

```text id="zrk1m9"
[-2^48, 2^48 - 1]
```

Spilled `i64` values preserve bytecode semantics, but they cost heap allocation and RC work. Tight loops with non-boxable `i64` values can be significantly slower.

## Agent Notes

When changing memory behavior:

* keep ownership visible at the operation boundary
* avoid hidden retains or releases
* update old and new refs in the same local block
* keep `release` iterative
* use `Refs(dst)` scratch instead of allocating traversal slices eagerly
* never cache heap element pointers across allocation
* keep `heap[0]` special and simple
* prefer transfer semantics when values are already being consumed
* prefer retain semantics when values are being copied or exposed
* keep interpreter and JIT ref behavior symmetrical

The best memory-model change is small, local, and explicit about who owns each ref before and after the operation.
