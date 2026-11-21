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

`types/map.go` owns map storage. A `TypedMap` with a numeric key (`i1`, `i8`, `i32`, `i64`, `f32`, `f64`) keeps its entries in an open-addressing table of four flat arrays that hold no Go pointers. A `StringMap` and the generic `Map` keep a Go map. Native code `MUST NOT` touch a Go map, so only the table is reachable from machine code.

| Part | Contract |
|---|---|
| Capacity | a power of two, at least 8 for a map built by `NewTypedMap`; a zero-value `TypedMap` is not usable |
| Hash | the key word xor a random per-table seed, times a fixed constant; the top bits pick the home slot and the seven bits below them the tag. The seed survives rehash and `Clone`, so keys crafted to collide for the bare constant spread out; it never affects iteration order |
| Slot | key word, then `Boxed` value word, in `slots`; control byte `empty` (0), `dead` (1, a tombstone), or `full` (`0x80` with the tag) in `ctrl`; probing is linear and passes every slot whose control byte is not the key's tag. An insert takes the first tombstone on its probe, else the empty slot that ended it |
| Order | `order[:used]` lists the slot of every insert since the last rehash, the rehash's own re-inserts first, and `pos[slot]` is the position of the slot's latest insert; position `p` is live while slot `order[p]` is full and `pos[order[p]]` is `p`. A zero key is stored as `+0` |
| Load | `used`, which bounds live entries plus tombstones, stays at or below three quarters of capacity; an insert past that rehashes the live entries, in order, into fresh arrays, doubling them when live entries exceed three eighths and keeping the size otherwise |

Native code reads and writes the table of a `TypedMap` with an integer key (`i1`, `i8`, `i32`, `i64`) through `jit.OffsetMap*` (`jit-internals.md`, Maps). It `MUST` choose the same slot, write the same control byte, append the same `order` entry, write the same `pos` word, and make the same `count` and `used` updates as `set`, `remove`, and `get`, and `MUST NOT` rehash: an insert past the load takes the Go `set`. It `MUST NOT` write `gen`, `epoch`, a slice header, or `seed`.

A numeric map iterates (`Range`, `map.keys`, `map.iter`) in insertion order: an update keeps the entry's place, and a key deleted and set again goes last. A `StringMap` or `Map` iterates in an unspecified order; map constants and host maps are such maps. Under mutation, `map.iter` `MUST` produce every entry present for the whole walk exactly once and `MUST NOT` produce a key deleted before the walk reaches it. Over a numeric table it also produces every entry inserted during the walk, once: a rehash ends the table's `epoch` with the order positions it dropped, and the walk follows the epochs to its place in the new order, so it never reads an entry the table gave up. A walk that inserts a new key at every step therefore never ends. A table has no epoch until its first walk; after that every rehash records one, and a walk left unfinished keeps the epochs after its own reachable. `map.clear` ends a walk over a numeric table. Entries inserted during a walk over a `StringMap` or `Map` `MAY` be produced; such a map is cleared in place, so a walk over it produces nothing already cleared, but entries added after the clear `MAY` still appear. Float keys follow Go map keys, except that `-0` and `+0` are one key stored as `+0`; a `NaN` key never matches, so each set adds an entry that only `map.clear` removes.

## Representation

A heap object keeps its Go representation type for its lifetime and, for a struct, array, or map, its declared type (`Type().Equals`). The one exception is a `REF_NEW` cell, whose scalar kind `REF_SET` may change. `Interpreter.Store` returns `ErrTypeMismatch` for a value that would change the slot's representation; a scalar replacing a scalar cell is allowed, as with `REF_SET`. Optimizer guards on a shape rely on this. A value borrowed by `Load` keeps it too: the host `MUST NOT` change its type or storage except through `Store`.

A finalizer (`io.Closer.Close`) runs as host code with the VM mid-instruction: it `MUST NOT` write a local or a global (`SetLocal`, `SetGlobal`, and a call of a VM function return `ErrHostCall`), since optimized code holds both across a release.

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

Constants load through one path at construction and at `Reset`. Each interpreter `MUST` own a copy of every mutable constant (arrays, structs, maps, errors), so mutation never reaches the program or another interpreter, and `Reset` `MUST` reload them at unchanged heap addresses with initial contents and no ref into the dynamic region.

Host-backed views (`HostArray`, `HostStruct`, `HostMap`) are the exception: they address Go memory and are not cloned, so a host view used as a constant stays shared with the host and with other interpreters, and `Reset` `MUST NOT` be expected to restore what writes through it changed.

Host refs use the same retain/release model. Host values `MUST NOT` implicitly own VM refs; a value returned by `Marshal` owns the refs it holds until the VM takes it. Per-API ownership belongs to `host-integration.md`.

## Related

- `host-integration.md`
- `value-representation.md`
- `jit-internals.md`
