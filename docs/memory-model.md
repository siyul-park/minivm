# Memory Model

Heap storage, reference ownership, RC, cycle collection.

## Ownership

| Concern | Owner |
|---|---|
| Heap / RC | `interp/interp.go` |
| Threaded ownership | `interp/threaded.go` |
| Host heap API | `interp/host.go` |
| Heap object types | `types/array.go`, `struct.go`, `map.go` |
| Boxed values | `types/boxed.go` |

## Model

- Heap refs are stable indexes; `0` is permanent null.
- Only `KindRef` is reference-counted.
- Ref-containing heap values `MUST` implement `types.Traceable`.
- `release` is iterative.

```text
heap  []types.Value
rc    []int
free  []int
trial []int
work  []int
```

Native execution MUST preserve the threaded ownership totals.

## Transfers

| Operation | Result |
|---|---|
| `Alloc` | creates one owned ref |
| `Retain` / `DUP` | adds one owner |
| `Release` | removes one owner; zero triggers reclamation |
| stack push | takes ownership |
| stack load | borrows slot ownership |
| local/global/upvalue store | retains new, releases old |
| `CLOSURE_NEW` | transfers function/capture ownership |
| `RETURN` | releases frame-owned values |
| native call to a borrowed parameter | caller keeps ownership; materialization restores it |

Transfers `MUST` preserve counts exactly. Native execution `MAY` borrow backing storage, but deopt/exits `MUST` restore interpreter ownership.

## Reference Counting

`retain(addr)` adds one owner. `release(addr)` removes one; zero clears the slot, recycles the index, and iteratively releases child refs.

Counts cover heap edges plus frame, global, stack, temporary, coroutine, and host owners.

## Traceable

```go
Refs(dst []types.Ref) []types.Ref
```

Implementations `MUST` append child refs without mutating existing entries and `MUST` allocate nothing for no-child traversal.

## Cycle Collection

Trial deletion subtracts heap-to-heap edges from exact counts, marks from externally owned objects, reclaims unmarked allocated objects, then repairs surviving counts.

Heap indexes never move. Collection runs adaptively and before the hard heap limit.

## Strings

Published strings are immutable. Concatenation `MAY` reuse interpreter-local append storage; writes `MUST` occur beyond published lengths, and reallocation `MUST` preserve old storage.

## Reset and Host

`Reset` invalidates runtime objects and may reuse cleared generic-array storage; reused headers `MUST` have no prior type or contents.

Host refs use the same retain/release model. Host values `MUST NOT` implicitly own VM refs. Host API details belong to `host-integration.md`.

## Related

- `host-integration.md`
- `value-representation.md`
- `jit-internals.md`
