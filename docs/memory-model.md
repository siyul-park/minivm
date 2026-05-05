# Memory Model

How the minivm heap works, including reference counting, GC, and key invariants.

## Heap Structure

The heap is three parallel slices inside `Interpreter`:

```go
heap []types.Value  // the object at this index
rc   []int          // reference count for this index (0 = free/collected)
free []int          // stack of free indices available for reuse
```

Allocation always returns a stable integer index. Indices never move — this is critical because `KindRef` values embedded in bytecode constants and stack slots hold raw heap indices that remain valid across GC.

## Reference Counting Protocol

RC is managed manually in every threaded closure that touches references. The rule:

| Operation | RC change |
|---|---|
| Value enters the stack (push) | `retain(addr)` — increment RC |
| Value is consumed from the stack (pop/use) | `release(addr)` — decrement RC |
| Value is duplicated (`DUP`) | `retain(addr)` — one more reference |
| Value is dropped (`DROP`) | `release(addr)` — one fewer reference |
| Value stored to local/global | `retain(addr)` for the new slot, `release(addr)` for the old slot |

**`retain(addr)`**: increments `rc[addr]`.

**`release(addr)`**: decrements `rc[addr]`. If RC reaches 0:
1. Calls `Refs()` on the object (if `Traceable`) and adds each referenced addr to a work stack.
2. Calls `Close()` on the object (if `io.Closer`).
3. Sets `heap[addr] = nil` and appends `addr` to the `free` list.
4. Repeats for every addr on the work stack (cascading release of nested references).

`release` is **iterative** using an explicit `[]int` work stack — it does not recurse. This prevents stack overflow on deep object graphs.

## Allocation

`alloc(val types.Value) int`:
1. If `free` is non-empty: pop an index, store `val`, return.
2. If `heap` has capacity: append `val`, set `rc[addr] = 1`, return.
3. If at capacity: run `gc()`. If `gc` freed slots, pop one and return. Otherwise, double the heap/rc slices and append.

## GC Algorithm (Mark-and-Sweep via Sign Flip)

`gc()` runs when the heap is full and the free list is empty. It avoids allocating a separate mark array by using the sign of `rc` values as the mark bit:

**Phase 1 — Mark all as unreachable:**
```
for each heap slot j (skip j=0):
    if rc[j] != 0: rc[j] = -|rc[j]|   (negate, making it negative)
```

**Phase 2 — Trace roots and mark reachable:**
```
roots: stack values, constants, globals (any KindRef)
for each root KindRef addr:
    if rc[addr] < 0: rc[addr] = -rc[addr]  (restore positive = "reachable")
    recurse into Traceable.Refs()
```

**Phase 3 — Sweep:**
```
for each heap slot j:
    if rc[j] < 0:   (still negative = unreachable)
        heap[j] = nil
        rc[j] = 0
        free.append(j)
```

**Properties:**
- No separate mark array — O(1) extra space.
- Handles reference cycles (mark-and-sweep vs. pure RC).
- No compaction — heap indices remain stable.
- GC pauses are proportional to heap size, not live-set size.

## Key Invariants

**`heap[0]` is always `Null`** — allocated in `interp.New()` with RC=1 before any user code runs. It is never freed and never appears in the free list. `BoxedNull = BoxRef(0)`.

**RC must be symmetric** — every `retain` must have a matching `release`. Asymmetry causes either premature collection (RC=0 too early) or permanent leaks (RC never reaches 0).

**No RC tracking for primitive `Boxed` values** — only `KindRef` values have heap objects that need RC management. Non-ref kinds (`KindI32`, `KindI64`, `KindF32`, `KindF64`) are value types; ignore them in RC logic.

**Heap indices are stable across GC** — never cache a pointer into `heap` slice across a potential `alloc` call (slice may be reallocated). Always use the integer index.

## Host Function Memory Access

Host functions interact with the heap through the `Interpreter` API:

```go
// Allocate a new object and get its index
addr, err := vm.Alloc(val)    // if val is BoxedRef, returns existing index

// Read an object (validates RC > 0)
obj, err := vm.Load(addr)

// Write to an existing slot
err = vm.Store(addr, val)

// Manual RC control (for long-lived references)
obj, err := vm.Retain(addr)   // increments RC, returns object
err = vm.Release(addr)        // decrements RC
```

Always call `Release` when a retained reference is no longer needed, otherwise objects leak.

## I64 Heap Spilling

Large `int64` values that don't fit in the 49-bit NaN-boxable range are heap-allocated as `types.I64`:

```go
func (i *Interpreter) boxI64(val int64) types.Boxed {
    if types.IsBoxable(val) {
        return types.BoxI64(val)     // fits in Boxed, no allocation
    }
    addr := i.alloc(types.I64(val)) // spill to heap
    return types.BoxRef(addr)
}
```

Each I64 arithmetic operation on spilled values costs one heap alloc + RC operations. This is transparent to bytecode but affects throughput in tight loops. Approximately: values in `[-2^48, 2^48-1]` are stack-allocated; outside this range, heap-allocated.
