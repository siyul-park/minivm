package interp

import (
	"errors"
	"slices"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"
)

// tcache is the default structs and arrays of each type the interpreter built
// ahead of native code, which takes them without leaving it: the fast path. An
// array type has jit.Classes bins: class c below the last holds arrays of
// length c, and the last holds arrays of one exact length at or past it. A
// struct type uses class 0. Each cached ref is an object of
// count 1 that no one but native code may take, so until it does the tcache
// owns it: flush returns every ref still unused. A miss takes the slow path,
// which allocates in Go and fills the bin.
type tcache struct {
	// table is what native code reads, one jit.Bin per bin; refs
	// are the arrays the table's pointers address, kept here so they
	// outlive native code's uintptr views of them.
	table []jit.Bin
	refs  [][]types.Boxed
	// batch is how many objects each bin's next fill builds; zero until
	// the bin's first miss, which only arms it.
	batch []int
	// built and left are how many objects each bin's fills built in an
	// entry and how many of them the entry did not take; missed is how many
	// misses it served. Their sum, built - left + missed, is how many objects
	// the entry used, and learned is that sum for the last entry that built
	// any, which sizes the bin's next first fill.
	built, left, missed, learned []int
	// changes is how many times each bin's length changed in an
	// entry, which only the last class of an array type can; a second change
	// stops the bin's caching until flush.
	changes []int8
	// dirty reports that a miss armed a batch or a fill cached something,
	// which flush has not yet reset or returned; held, that a fill cached.
	dirty, held bool
	// filling reports that fill is allocating, whose own allocation
	// must not return what it has cached.
	filling bool
}

const (
	// minBatch is the fewest objects a fill builds.
	minBatch = 16
	// maxBatch is the most a single fill builds.
	maxBatch = 1024
	// wide is the longest array a bin holds, so that a fill of long
	// arrays cannot outgrow the memory the heap limit counts in slots.
	wide = 1 << 12
	// covered is the batch of a type whose last entry needed no fill.
	covered = -1
	// unset is the Len of a bin with no length yet. Native code
	// compares a requested length with it, and no length is negative.
	unset = -1
)

// newTcache returns an empty tcache for n types.
func newTcache(n int) tcache {
	n *= jit.Classes
	c := tcache{table: make([]jit.Bin, n), refs: make([][]types.Boxed, n), batch: make([]int, n), built: make([]int, n), left: make([]int, n), missed: make([]int, n), learned: make([]int, n), changes: make([]int8, n)}
	for b := range c.table {
		c.table[b].Len = unset
	}
	return c
}

// fill serves type t's slow path: a miss of an object of the given length,
// which a struct has none of, in the bin of its class. The first miss of an
// entry only arms the bin, so a call that allocates once builds nothing ahead; the next builds what the
// last entry needed beyond the two misses served so far, at least minBatch, and
// each later miss in the entry doubles that. A fill stops early, without
// failing, when the heap is exhausted, after returning a bin of arrays of
// another length. A bin whose length changes a second time in an entry
// alternates, and builds nothing more until flush, so each of its allocations
// costs the callout alone. Native code takes the batch's lowest address first,
// so a batch is stored last first.
func (c *tcache) fill(i *Interpreter, t, length int) {
	b := t*jit.Classes + min(length, jit.Classes-1)
	c.dirty = true
	c.missed[b]++
	if c.batch[b] == 0 {
		// Two misses are served before the first fill, the arming one and
		// the one that fills. An entry that learned it needs no more than
		// those two builds nothing.
		switch want := c.learned[b] - 2; {
		case c.learned[b] == 0:
			c.batch[b] = minBatch
		case want <= 0:
			c.batch[b] = covered
		default:
			c.batch[b] = min(want, maxBatch)
		}
		return
	}
	if c.batch[b] == covered {
		// The entry needs more than it learned: fall back to doubling.
		c.batch[b] = minBatch
		return
	}
	if length > wide || c.changes[b] > 1 {
		return
	}
	if was := c.table[b].Len; int64(length) != was {
		if was != unset {
			c.changes[b]++
		}
		mark := len(i.free)
		c.drop(i, b)
		i.shrink(mark)
		if c.changes[b] > 1 {
			return
		}
	}
	size := c.batch[b]
	c.batch[b] = min(2*size, maxBatch)
	// A fill must not collect: it builds at most half of what the heap holds
	// before the collection threaded code would run at this live count, and
	// leaves the rest to the program's own allocations.
	if i.target > 0 {
		size = min(size, (i.target-(len(i.heap)-len(i.free)))/2)
	}
	if size <= 0 {
		return
	}

	n := int(c.table[b].N)
	refs := c.refs[b][:n]
	c.filling = true
	defer func() {
		c.filling = false
		if r := recover(); r != nil {
			if err, ok := r.(error); !ok || !errors.Is(err, ErrHeapExhausted) {
				panic(r)
			}
		}
		slices.Reverse(refs[n:])
		c.built[b] += len(refs) - n
		c.refs[b] = refs
		c.table[b] = jit.Bin{Refs: base(refs), N: int64(len(refs)), Len: int64(length)}
		c.held = c.held || len(refs) > 0
	}()
	array, _ := i.types[t].(*types.ArrayType)
	for range size {
		var v types.Value
		if array != nil {
			v = i.newArrayDefault(array, types.BoxI32(int32(length)))
		} else {
			v = i.newStruct(i.types[t].(*types.StructType))
		}
		refs = append(refs, types.BoxRef(i.alloc(v)))
	}
}

// flush returns every ref still cached, so the heap reads as though native
// code had allocated only what it took, resets every batch and bin length, and
// reports whether it returned any ref. Returning the tcache at once frees its
// slots to the allocator, and the ones that end the heap are dropped from it.
func (c *tcache) flush(i *Interpreter) bool {
	if !c.dirty || c.filling {
		return false
	}
	returned := c.held
	c.dirty, c.held = false, false
	mark := len(i.free)
	for b := range c.table {
		c.drop(i, b)
		if c.built[b] > 0 {
			c.learned[b] = c.built[b] - c.left[b] + c.missed[b]
		}
		c.batch[b], c.built[b], c.left[b], c.missed[b], c.changes[b], c.table[b].Len = 0, 0, 0, 0, 0, unset
	}
	i.shrink(mark)
	return returned
}

// drop returns every ref bin b still has cached, leaving the slots it
// frees to the caller to shrink.
func (c *tcache) drop(i *Interpreter, b int) {
	c.left[b] += int(c.table[b].N)
	for e := &c.table[b]; e.N > 0; {
		e.N--
		i.undo(c.refs[b][e.N].Ref())
	}
}
