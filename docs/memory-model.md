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

## Map Storage

`types/map.go` owns map storage. A `TypedMap` with a numeric key (`i1`, `i8`, `i32`, `i64`, `f32`, `f64`) keeps its entries in an open-addressing table of two flat arrays that hold no Go pointers. A `StringMap` and the generic `Map` keep a Go map. Native code `MUST NOT` touch a Go map, so only the table is reachable from machine code.

| Part | Contract |
|---|---|
| Capacity | a power of two, at least 8 for a map built by `NewTypedMap`; a zero-value `TypedMap` is not usable |
| Hash | the key word xor a random per-table seed, times a fixed constant, shifted to the capacity; the seed survives rehash and `Clone`, so keys crafted to collide for the bare constant spread out, and slot order differs between tables |
| Slot | key word, then `Boxed` value word, in `slots`; state `empty`, `full`, or `dead` (tombstone) in `ctrl`; probing is linear and an insert reuses the first tombstone on its path |
| Load | live entries plus tombstones stay at or below three quarters of capacity; an insert past that rehashes into fresh arrays, doubling them when live entries exceed three eighths and keeping the size otherwise |

Iteration order (`Range`, `map.keys`, `map.iter`) is unspecified; for numeric keys it follows slot order. Under mutation, `map.iter` `MUST` produce every entry present for the whole walk exactly once and `MUST NOT` produce a key deleted before the walk reaches it. Entries inserted during the walk `MAY` be produced. A rehash does not restart or skip the walk, and `map.clear` ends a walk over a numeric table. A `StringMap` or `Map` is cleared in place, so a walk over it produces nothing already cleared, but entries added after the clear `MAY` still appear. Float keys follow Go map keys: `-0` and `+0` are one key whose stored bits are the latest written, and a `NaN` key never matches, so each set adds an entry that only `map.clear` removes.

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
