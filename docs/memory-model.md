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

While native code runs, the tcache (`jit-internals.md`, Reserve) owns the structs and arrays it built ahead: each is a live slot of count 1 that no program value references. `HeapLen` and `RefCount` can read higher than threaded execution would until the tcache is returned. It is returned at every return to the interpreter at native depth 0, so after `Run` the heap holds only what the program owns, and an array it returns is reused at once, not only at Reset, since no host could have loaded it; a heap limit counts cached slots, and reaching it returns the tcache before it fails.

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

Constants load through one path at construction and at `Reset`. Each interpreter `MUST` own a copy of every mutable constant (arrays, structs, maps), so mutation never reaches the program or another interpreter, and `Reset` `MUST` reload them at unchanged heap addresses with initial contents and no ref into the dynamic region.

Host-backed views (`HostArray`, `HostStruct`, `HostMap`) are the exception: they address Go memory and are not cloned, so a host view used as a constant stays shared with the host and with other interpreters, and `Reset` `MUST NOT` be expected to restore what writes through it changed.

Host refs use the same retain/release model. Host values `MUST NOT` implicitly own VM refs; a value returned by `Marshal` owns the refs it holds until the VM takes it. Per-API ownership belongs to `host-integration.md`.

## Related

- `host-integration.md`
- `value-representation.md`
- `jit-internals.md`
