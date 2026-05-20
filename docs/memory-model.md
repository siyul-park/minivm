# Memory Model

How minivm heap, reference counting, GC, and invariants work.

## Agent Checklist

Read before editing `interp/threaded.go`, `interp/host.go`, `types/array.go`, `types/struct.go`, or code that retains, releases, allocates, loads, or stores refs.

- only `KindRef` values participate in RC
- stack/global/local ownership changes need symmetric retain/release
- keep `release()` iterative
- heap index `0` is always `Null`
- never keep pointers into `heap`; keep integer refs because allocation may grow slices

## Heap Structure

`Interpreter` stores heap state in parallel slices:

```go
heap []types.Value // object at index
rc   []int         // ref count; 0 = free/collected
free []int         // reusable indices
```

Allocation returns stable integer indices. Indices never move, because `KindRef` values in constants and stack slots hold raw heap indices that must survive GC.

## Reference Counting Protocol

RC is manually handled in every threaded closure touching refs.

| Operation | RC change |
|---|---|
| push ref to stack | `retain(addr)` |
| pop/use ref from stack | `release(addr)` |
| `DUP` ref | `retain(addr)` |
| `DROP` ref | `release(addr)` |
| store ref to local/global | `retain(new)`, `release(old)` |
| map insert/replace/delete/clear | transfer or release map-owned ref keys/values |

`retain(addr)` increments `rc[addr]`.

`release(addr)` decrements `rc[addr]`. When RC reaches `0`, it:

1. gets nested refs from `Refs()` if object is `Traceable`
2. calls `Close()` if object is `io.Closer`
3. clears `heap[addr]` and appends `addr` to `free`
4. repeats for nested refs using an explicit work stack

`release` must stay iterative, not recursive, to avoid stack overflow on deep object graphs.

## Allocation

`alloc(val types.Value) int`:

1. reuse from `free` if available
2. append if heap has capacity, with `rc[addr] = 1`
3. if full, run `gc()`
4. if GC freed slots, reuse one
5. otherwise double `heap`/`rc` capacity and append

## GC Algorithm: Mark-and-Sweep via Sign Flip

`gc()` runs when the heap is full and `free` is empty. It uses the sign of `rc` as the mark bit, avoiding a separate mark array.

### 1. Mark all live slots unreachable

```text
for each heap slot j, except 0:
    if rc[j] != 0:
        rc[j] = -abs(rc[j])
```

### 2. Trace roots and mark reachable

```text
roots = stack values + constants + globals

for each root KindRef addr:
    if rc[addr] < 0:
        rc[addr] = -rc[addr]
    recursively trace Traceable.Refs()
```

### 3. Sweep

```text
for each heap slot j:
    if rc[j] < 0:
        heap[j] = nil
        rc[j] = 0
        free.append(j)
```

Properties:

- O(1) extra space; no mark array
- handles reference cycles
- no compaction, so heap indices stay stable
- pause cost is proportional to heap size, not live-set size

## Key Invariants

### `heap[0]` is always `Null`

`interp.New()` allocates heap index `0` with RC `1` before user code. It is never freed and never enters `free`. `BoxedNull = BoxRef(0)`.

### RC must be symmetric

Every `retain` needs a matching `release`. Asymmetry causes premature collection or leaks.

### Primitive `Boxed` values do not use RC

Only `KindRef` values need RC. `KindI32`, `KindI64`, `KindF32`, and `KindF64` are value types and ignored by RC logic.

### Heap indices are stable

Never cache a pointer into `heap` across potential `alloc`; the slice may reallocate. Keep integer indices.

## Host Function Memory Access

Host functions use the `Interpreter` API:

```go
addr, err := vm.Alloc(val)  // allocate object; BoxedRef returns existing index
obj, err := vm.Load(addr)   // read object; validates RC > 0
err = vm.Store(addr, val)   // write existing slot

obj, err := vm.Retain(addr) // manual long-lived retain
err = vm.Release(addr)      // matching release
```

Always `Release` retained refs when done, or objects leak.

## I64 Heap Spilling

Large `int64` values outside the 49-bit NaN-boxable range are heap-allocated as `types.I64`.

```go
func (i *Interpreter) boxI64(val int64) types.Boxed {
    if types.IsBoxable(val) {
        return types.BoxI64(val)
    }
    addr := i.alloc(types.I64(val))
    return types.BoxRef(addr)
}
```

Spilled I64 arithmetic costs one heap allocation plus RC work per operation. Bytecode behavior is unchanged, but tight-loop throughput can drop. Roughly, `[-2^48, 2^48-1]` stays stack-allocated; outside that range spills to heap.
