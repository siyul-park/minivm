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

- Heap refs are stable indexes.
- Index `0` is permanent null.
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

Future native paths MUST preserve the threaded ownership totals.

## Ownership

| Operation | Ownership |
|---|---|
| `Alloc` | one owned ref |
| `Retain` | +1 ownership |
| `Release` | -1 ownership |
| stack push | owns pushed ref |
| stack load | borrows slot ownership |
| local/global/upvalue store | retain new, release old |
| `DUP` | +1 ownership |
| `CLOSURE_NEW` | transfers function/capture ownership |
| `RETURN` | releases retiring frame ownership |

Each transfer above `MUST` be implemented exactly as stated. Deferred native refs `MAY` borrow backing ownership only until an interpreter-visible transfer; deopt and bridges `MUST` restore interpreter ownership.

## Reference Counting

`retain(addr)` increments `rc[addr]`. `release(addr)` decrements; zero clears the slot, returns the index to `free`, and releases child refs through an explicit work stack.

The count includes heap objects, frames, globals, stack values, temporaries, coroutines, and host references.

## Traceable

```go
Refs(dst []types.Ref) []types.Ref
```

Implementations `MUST` append child refs without mutating existing entries and `MUST` allocate nothing for no-child traversal.

## Cycle Collection

Trial deletion:

1. copy exact counts to `trial`;
2. subtract heap-to-heap edges;
3. treat positive residuals as external ownership;
4. mark from externally owned objects;
5. reclaim allocated unmarked objects;
6. repair surviving counts.

Heap indexes never move. Collection runs adaptively and before enforcing the hard heap limit.

## Strings

Published strings are immutable. Concatenation `MAY` reuse interpreter-local append storage; writes `MUST` occur beyond published lengths, and reallocation `MUST` preserve old storage.

## Reset

`Reset` invalidates runtime objects, recomputes collection goals, and `MAY` reuse cleared generic-array headers/storage. Reused headers `MUST` carry no prior type or contents.

## Host

Host refs use the same retain/release model. Host values `MUST NOT` implicitly own VM refs.

See `host-integration.md` for host API behavior.

## Related

- `host-integration.md`
- `value-representation.md`
- `jit-internals.md`
