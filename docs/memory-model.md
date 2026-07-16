# Memory Model

How minivm heap storage, reference counting, GC, and reference ownership work.

## When to Read

Use this document when changing code that allocates, retains, releases, loads, stores, or moves refs.

For boxed value layout and kind rules, see `docs/value-representation.md`.

## Source of Truth

| Concern | File |
|---|---|
| interpreter heap and RC | `interp/interp.go` |
| threaded ref ownership | `interp/threaded.go` |
| host heap APIs | `interp/host.go` |
| arrays, structs, maps | `types/array.go`, `types/struct.go`, `types/map.go` |
| boxed kind layout | `types/boxed.go` |

## Summary

minivm uses stable heap indices, exact reference counting, and a trial-deletion mark-and-sweep collector for cycles.

Only `KindRef` values participate in reference counting. Primitive boxed values are copied by value and ignored by RC.

Design rules:

- keep ownership explicit
- keep retain and release symmetric
- keep heap indices stable
- keep `release` iterative
- prefer local ownership rules over hidden lifetime behavior

## Heap Structure

`Interpreter` stores heap state in parallel slices.

```go
heap  []types.Value
rc    []int
free  []int
trial []int
work  []int
```

Allocation returns a stable integer heap index. Heap indices never move, because `KindRef` values store heap indices in stack slots, constants, globals, closures, and heap objects.

## Reference Counting

Reference counting is handled by threaded handlers and host APIs.

| Operation | RC behavior |
|---|---|
| push ref to stack | retain ref |
| pop or consume ref from stack | release ref |
| `DUP` ref | retain ref |
| `DROP` ref | release ref |
| store ref to local/global/upvalue | retain new ref, release old ref |
| overwrite ref field/element | retain or transfer new ref, release old ref |
| map replace/delete/clear | release map-owned refs |
| `CLOSURE_NEW` | transfer popped function and upvalues into closure |

`retain(addr)` increments `rc[addr]`.

`release(addr)` decrements `rc[addr]`. When the count reaches zero, it collects nested refs from `Traceable.Refs`, closes the value when needed, clears the heap slot, appends the address to `free`, and repeats for nested refs with an explicit work stack.

`release` must remain iterative to avoid deep-recursion failures on large object graphs.

Counts include every ownership edge, whether it comes from another heap object,
the VM stack and frames, a global or constant, temporary construction state, or
the host. The cycle collector relies on this exactness.

The ARM64 JIT may compile a stack copy of a ref as *deferred*: the operand
borrows the retain already held by its backing storage (a local, global, upval,
or constant) instead of taking its own, which removes the per-element
retain/release pair from primitive container loops. The invariant is unchanged:
every path that hands the value to interpreter-visible state — an ownership
transfer, a guard exit stub, or a trap-fallback/module-completion redeem — takes
exactly one retain first, so net counts stay exact and symmetric on every path,
including deopts. See `docs/jit-internals.md` (Reference Ownership).

## Traceable Values

Heap objects that can contain refs implement `types.Traceable`.

```go
Refs(dst []types.Ref) []types.Ref
```

Contract:

- append nested refs to `dst`
- return `dst` unchanged when there are no nested refs
- do not allocate unless a child ref exists
- preserve append-only behavior

This contract lets `release` and GC reuse caller-owned scratch storage.

## Allocation

`alloc(val types.Value)` creates one owned heap reference.

Allocation order:

1. run GC when occupied slots reach the adaptive goal
2. reuse an index from `free`
3. run GC if backing storage or the hard limit is reached and this
   allocation has not collected yet
4. reuse a slot freed by GC if available
5. return `ErrHeapExhausted` if the hard limit still applies
6. otherwise append or grow heap storage

An allocation attempt runs GC at most once.

`WithHeap(n)` sets initial heap capacity. Subject to the hard limit, the initial
GC goal is at least that capacity and at least 64 slots beyond the baseline heap.

`WithHeapLimit(n)` sets a hard heap entry limit. Values `n <= 0` mean
unlimited. It also clamps the adaptive goal.

The max limit is checked after GC and free-list reuse, so collectable objects
do not block future allocations.

Public host APIs that allocate, such as `Alloc`, `Push`, and `Marshal`, return `ErrHeapExhausted` as ordinary errors.

## GC

GC uses trial deletion to derive roots from exact reference counts instead of
maintaining a second root registry.

GC runs when occupied slots reach an adaptive goal. Backing-storage
exhaustion and the hard heap limit remain forced collection points even when
the goal is higher.

After collection, the next goal is derived from the live set:

```text
live = len(heap) - len(free)
dynamic = max(live - base, 0)
goal = live + max(dynamic, 64)
```

The hard heap limit clamps `goal`, and `goal` never falls below `live`. The
64-slot minimum avoids repeated collection for small heaps; larger dynamic live
sets receive roughly their current size as allocation runway. `Reset` recomputes
the goal from the baseline heap instead of inheriting the previous run's target.

High-level flow:

1. copy each allocated slot's exact `rc` into the reused `trial` table
2. subtract every heap-to-heap edge reported by `Traceable.Refs`
3. treat positive residual counts as owners outside the heap graph
4. mark transitively from those externally owned objects
5. reclaim every allocated unmarked slot as cyclic garbage
6. subtract dead-to-live edges from surviving exact counts

A residual external count covers stack, constant, global, frame, coroutine,
temporary construction, and host ownership uniformly. Host-held addresses
therefore survive collection without a separate pin table.

Properties:

- handles self-cycles, multi-object cycles, and duplicate edges
- preserves objects with any external ownership
- reuses O(heap slots) trial and work buffers after their first growth
- does not compact
- keeps heap indices stable
- pause cost is proportional to allocated slots plus traced edges

## Invariants

### Heap index 0 is always null

`heap[0]` is reserved for `Null`. `interp.New` initializes index `0` with RC `1` before user code runs.

Rules:

- never free it
- never put it in `free`
- `BoxedNull` is `BoxRef(0)`

### Only refs use RC

Primitive boxed values do not participate in reference counting. Only `KindRef` values are tracked.

### RC must be exact and symmetric

Every ownership edge contributes exactly one count. Every retained ref must be
released exactly once. Missing counts can collect a value too early; excess
counts can turn unreachable garbage into a false external root.

### Heap indices are stable

Do not keep addresses into the heap slice across any operation that may allocate. Keep integer heap indexes instead.

### Ref ownership must be explicit

When an operation moves a ref, define whether it copies and retains, consumes and releases, transfers ownership, or overwrites and releases the old value.

Prefer one clear ownership rule per opcode or API.

### Closure frames track two refs

| Field | Meaning |
|---|---|
| `addr` | function/template heap index used for code, profiling, and JIT |
| `ref` | callable heap ref released on return |

For plain functions, `addr == ref`. For closures, `addr` points to the function template and `ref` points to the closure instance.

Profiling and JIT use `addr`. Lifetime release uses `ref`.

## Host Access

Host functions use `Interpreter` APIs to work with heap values.

```go
addr, err := vm.Alloc(val)
obj, err := vm.Load(addr)
err = vm.Store(addr, val)
obj, err = vm.Retain(addr)
err = vm.Release(addr)
```

Rules:

- `Alloc` creates an owned heap ref
- `Alloc` of an existing ref creates another ownership of the same address
- `Load` reads an object without changing ownership
- `Store` overwrites an existing heap slot and finalizes the old value
- `Store` accepts the destination's own ref as a no-op but rejects a different
  heap address; share objects through `Alloc(existingRef)` instead
- concrete pointer values transferred into heap or stack slots must have unique
  ownership; use refs to share one object
- `Retain` creates an additional host-owned ref
- `Release` drops a host-owned ref
- every ownership created by `Alloc` or `Retain` must be transferred or released

Leaked host refs keep objects alive. `SetGlobal` and `SetLocal` validate and
transfer a different ref into the destination, but assigning the destination's
current boxed value is a no-op and leaves the caller's ownership unchanged.

`Marshal` may allocate nested heap refs while converting Go values. Those refs belong to the interpreter heap. Consume them through VM APIs such as `Push` or `Alloc`, or let `Close` / `Reset` discard temporary allocations.

## Pool Lifetime

Each `Interpreter` owns its heap.

A pooled interpreter is reset when returned to the pool. After `Pool.Put`, all refs from that interpreter are invalid.

Do not store heap refs from a borrowed interpreter beyond its borrow lifetime.

## I64 Heap Spilling

Most `i64` values are stored inline in `types.Boxed`.

Large `int64` values outside the NaN-boxable range spill to the heap as `types.I64`.

Approximate inline range:

```text
[-2^48, 2^48 - 1]
```

Spilled `i64` values preserve bytecode semantics, but they cost heap allocation and RC work. Tight loops with non-boxable `i64` values can be significantly slower.

## Maintenance Notes

When changing memory behavior:

- keep ownership visible at the operation boundary
- avoid hidden retains or releases
- update old and new refs in the same local block
- keep `release` iterative
- use `Refs(dst)` scratch instead of allocating traversal slices eagerly
- keep heap-edge counts exact so trial deletion can derive external roots
- never cache heap element indexes as slice addresses across allocation
- keep `heap[0]` special and simple
- prefer transfer semantics when values are already being consumed
- prefer retain semantics when values are being copied or exposed
- keep interpreter and JIT ref behavior symmetrical

## Related Docs

- `docs/value-representation.md` — `KindRef`, boxing, and heap-spilled `i64`
- `docs/host-integration.md` — host-facing heap APIs
- `docs/jit-internals.md` — native ref updates and fallback rules
- `docs/architecture.md` — frame ownership and runtime state
