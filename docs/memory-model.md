# Memory Model

Heap storage, reference ownership, reference counting, and cycle collection.

## When to Read

Read when changing allocation, `retain`, `release`, heap objects, refs, GC, or ownership across interpreter/JIT/host boundaries.

## Source of Truth

| Concern | Owner |
|---|---|
| Heap and RC | `interp/interp.go` |
| Threaded ownership | `interp/threaded.go` |
| Host heap APIs | `interp/host.go` |
| Heap object types | `types/array.go`, `types/struct.go`, `types/map.go` |
| Boxed values | `types/boxed.go` |

## Model

- Heap references are stable integer indexes.
- Heap index `0` is permanent null.
- Only `KindRef` participates in reference counting.
- Heap values that contain refs implement `types.Traceable`.
- `release` is iterative.
- Native/JIT paths must preserve the same ownership totals as threaded execution.

```text
heap  []types.Value
rc    []int
free  []int
trial []int
work  []int
```

## Ownership

| Operation | Ownership |
|---|---|
| `Alloc` | returns one owned reference |
| `Retain` | creates one additional ownership |
| `Release` | drops one ownership |
| stack push | owns a pushed `KindRef` |
| stack load | borrows the slot's ownership |
| local/global/upvalue store | retains new value and releases old value |
| `DUP` | creates one additional ownership |
| `CLOSURE_NEW` | transfers function/capture ownership into closure |
| `RETURN` | releases the retiring frame's remaining owned slots |

JIT deferred refs may borrow the retain held by backing storage. Before a deopt, bridge, or other interpreter-visible transfer, the native path must restore the ownership the interpreter expects.

## Reference Counting

`retain(addr)` increments `rc[addr]`.

`release(addr)` decrements it. A zero count clears the slot, returns the index to `free`, and releases nested references using an explicit work stack.

The count includes every ownership edge, including heap objects, frames, globals, stack values, temporaries, coroutines, and host references.

## `Traceable`

```go
Refs(dst []types.Ref) []types.Ref
```

Implementations append child references to `dst` without mutating existing entries and avoid allocation when there are no children.

## Cycle Collection

GC uses trial deletion over the current heap graph:

1. copy exact counts into `trial`;
2. subtract heap-to-heap edges;
3. treat positive residual counts as external ownership;
4. mark from externally owned objects;
5. reclaim allocated unmarked objects;
6. repair surviving exact counts.

It handles self-cycles and multi-object cycles without moving heap indexes.

Collection runs at an adaptive goal and may also run when storage or the hard heap limit requires it. A hard limit is checked after collection and free-list reuse.

## Strings

String concatenation may reuse an interpreter-local append buffer, but published strings are immutable. Growth only writes beyond every published length; reallocation leaves older strings attached to their old storage.

## Reset and Reuse

`Reset` invalidates runtime objects, recomputes the collection goal, and may reuse cleared generic-array headers and backing storage. Pooled headers never retain their previous type or contents.

## Host Rules

Host-owned references use the same `Retain`/`Release` model as guest ownership. Host values do not implicitly own VM refs.

See `host-integration.md` for public API details.

## Related Docs

- `value-representation.md`
- `host-integration.md`
- `jit-internals.md`
