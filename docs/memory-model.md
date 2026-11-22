# Memory Model

Heap storage, reference ownership, RC, cycle collection.

## Ownership

| Concern | Owner |
|---|---|
| Heap / RC | `types/heap.go` |
| Threaded ownership | `interp/threaded.go` |
| Host heap API | `interp/host.go` |
| Heap object types | `types/array.go`, `struct.go`, `map.go` |
| Boxed values | `types/boxed.go` |

## Model

- Heap refs are stable indexes; `0` is permanent null.
- Only `KindRef` is reference-counted.
- Ref-containing heap values `MUST` implement `types.Traceable`.
- `Heap.Dec` is iterative.
- `types.Heap` holds the slots, their counts, the free list, and the struct and array free lists; `interp.Interpreter` embeds it by value and reaches its runtime through `Hooks` (`Exhaust`, `Fault`, `Finalize`, `Collect`).

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

`types/map.go` owns map storage. One `Map` serves every key type: it keeps its entries in an open-addressing table of four flat arrays that hold no Go pointers, so native code reads them without touching a Go map. A `Map` `MUST` come from `NewMap`; its zero value is not usable.

The key kind (`MapType.KeyKind`, which is `KindAny` for an `any` key) fixes how a key is stored and compared. `value-representation.md`, Map Key Words, owns the table word each key kind stores and how it matches.

An `i64` key outside the inline range is a map-owned `I64` heap cell: `Store` and `Swap` adopt the incoming cell when the caller holds the only reference, else copy it, and release a cell whose value fits inline. `Range` lends the cell; `map.keys` and `map.iter` hand out fresh copies, so a guest never holds the map's own cell. `Accepts` is the check before a key enters: a scalar fits unless the map is keyed by a reference type, and a reference fits a `string` map when it is a `String`, an `i64` map when it is an `I64`, and any other reference-keyed map always; an `any` map also takes a scalar.

| Part | Contract |
|---|---|
| Capacity | a power of two, at least 8 |
| Hash | the key's base hash (its word, else the content hash of a `String`) xor a random per-table seed, times a fixed constant; the top bits pick the home slot and the seven bits below them the tag. The seed survives rehash and `Clone`, so keys crafted to collide for the bare constant spread out; it never affects iteration order |
| Slot | key word, then `Boxed` value word, in `slots`; control byte `empty` (0), `dead` (1, a tombstone), or `full` (`0x80` with the tag) in `ctrl`; probing is linear and passes every slot whose control byte is not the key's tag. An insert takes the first tombstone on its probe, else the empty slot that ended it |
| Order | `order[:used]` lists the slot of every insert since the last rehash, the rehash's own re-inserts first, and `pos[slot]` is the position of the slot's latest insert; position `p` is live while slot `order[p]` is full and `pos[order[p]]` is `p` |
| Load | `used`, which bounds live entries plus tombstones, stays at or below three quarters of capacity; an insert past that rehashes the live entries, in order, into fresh arrays, doubling them when live entries exceed three eighths and keeping the size otherwise |

Native code serves maps keyed by `i1`, `i8`, `i32`, or `i64` only; every other key kind bridges or deopts (`jit-internals.md`, Maps). It reads and writes the table through `jit.OffsetMap*`, and `MUST` choose the same slot, write the same control byte, append the same `order` entry, write the same `pos` word, and make the same `count` and `used` updates as `Store`, `Delete`, and `Load`, and `MUST NOT` rehash: an insert past the load takes the Go `Store`. It `MUST NOT` write `gen`, `epoch`, a slice header, or `seed`.

## Map Ownership

A `Map` owns the references its keys and values hold and releases them through its heap. A map built with a nil heap is unbound: the first reference it must read, retain, or release panics with `ErrDetached`.

| Method | Key | Value |
|---|---|---|
| `Load` | borrowed | borrowed |
| `Store` | taken; a hit keeps the first key and releases the incoming one | taken; a hit releases the replaced value |
| `Swap` | taken as `Store` | taken; a hit returns the replaced value to the caller unreleased |
| `LoadAndDelete` | borrowed; the stored key is released | moves to the caller |
| `Delete` | borrowed; the stored key is released | released |
| `Clear` | every key released once | every value released once |
| `Clone` | a bound copy retains every ref, `null` included | same |

`Heap.Retain` counts `null`; `Heap.Release` skips it.

A walk over a map (`Range`, `map.keys`, `map.iter`) yields entries in insertion order for every map, constants and host-map copies included: an update keeps the entry's place, and a key deleted and stored again goes last. A host-map copy takes the order Go's map range gives. Under mutation, `map.iter` `MUST` produce every entry present for the whole walk exactly once and `MUST NOT` produce a key deleted before the walk reaches it. It also produces every entry inserted during the walk, once: a rehash ends the table's `epoch` with the order positions it dropped, and the walk follows the epochs to its place in the new order, so it never reads an entry the table gave up. A walk that inserts a new key at every step therefore never ends. A table has no epoch until its first walk; after that every rehash records one, and a walk left unfinished keeps the epochs after its own reachable. `map.clear` ends every walk.

A map owns its refs; arrays and structs keep handler reference counting until `types.Heap` owns theirs too (#367).

## Representation

A heap object keeps its Go representation type for its lifetime and, for a struct, array, or map, its declared type (`Type().Equals`). The one exception is a `REF_NEW` cell, whose scalar kind `REF_SET` may change. `Interpreter.Store` returns `ErrTypeMismatch` for a value that would change the slot's representation; a scalar replacing a scalar cell is allowed, as with `REF_SET`. Optimizer guards on a shape rely on this. A value borrowed by `Load` keeps it too: the host `MUST NOT` change its type or storage except through `Store`.

A finalizer (`io.Closer.Close`) runs as host code with the VM mid-instruction: it `MUST NOT` write a local or a global (`SetLocal`, `SetGlobal`, and a call of a VM function return `ErrHostCall`), since optimized code holds both across a release.

## Reference Counting

`types.Heap` owns the slots, counts, free lists, and collection (`Interpreter.Heap`). `Inc(addr, n)` adds owners. `Dec(addr)` removes one; zero clears the slot, recycles the index, and iteratively releases child refs. The `Interpreter` host API validates an address, then calls these unchecked methods; the interpreter supplies `types.Hooks` for what the heap cannot own: finalizing a function or closer, flushing the native tcache when the limit is reached, faulting on corruption, and the collection metrics.

Counts cover heap edges plus frame, global, stack, temporary, coroutine, and host owners.

## Traceable

```go
Refs(dst []types.Ref) []types.Ref
```

Implementations `MUST` append child refs without mutating existing entries and `MUST` allocate nothing for no-child traversal.

## Cycle Collection

Trial deletion subtracts heap-to-heap edges from exact counts, marks from externally owned objects, reclaims unmarked allocated objects, then repairs surviving counts.

Heap indexes never move. Collection runs adaptively and before the hard heap limit. After construction, the heap first collects when it holds `max(capacity, baseline + 64)` slots, capped at the limit. After `Reset`, and after every collection, it collects when it holds the live slots plus `max(live - baseline, 64)`, capped at the limit.

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
