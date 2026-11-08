package interp

import (
	"errors"
	"slices"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"
)

// stock is the default structs and arrays of each type the interpreter built
// ahead of native code, which takes them without leaving it. An array type's
// stock holds arrays of one length. Each stocked ref is an object of count 1
// that no one but native code may take, so until it does the stock owns it:
// flush returns every ref still unused.
type stock struct {
	// table is what native code reads, one jit.Stock per type index; refs
	// are the arrays the table's pointers address, kept here so they
	// outlive native code's uintptr views of them.
	table []jit.Stock
	refs  [][]types.Boxed
	// step is how many objects each type's next refill builds; zero until
	// the type's first miss, which only arms it.
	step []int
	// built and left are how many objects each type's fills built in an
	// entry and how many of them the entry did not take; missed is how many
	// misses it served. Their sum, built - left + missed, is how many objects
	// the entry used, and learned is that sum for the last entry that built
	// any, which sizes the type's next first fill.
	built, left, missed, learned []int
	// shifts is how many times each type's stocked length changed in an
	// entry; a second change stops the type's stocking until flush.
	shifts []int8
	// dirty reports that a miss armed a step or a refill stocked something,
	// which flush has not yet reset or returned; held, that a refill stocked.
	dirty, held bool
	// filling reports that refill is allocating, whose own allocation
	// must not return what it has stocked.
	filling bool
}

const (
	// first is how many default structs a type's first refill builds.
	first = 16
	// last is the most a single refill builds.
	last = 1024
	// wide is the longest array a stock holds, so that a refill of long
	// arrays cannot outgrow the memory the heap limit counts in slots.
	wide = 1 << 12
	// unset is the Len of a type with no stocked length yet. Native code
	// compares a requested length with it, and no length is negative.
	unset = -1
)

// newStock returns an empty stock for n types.
func newStock(n int) stock {
	s := stock{table: make([]jit.Stock, n), refs: make([][]types.Boxed, n), step: make([]int, n), built: make([]int, n), left: make([]int, n), missed: make([]int, n), learned: make([]int, n), shifts: make([]int8, n)}
	for t := range s.table {
		s.table[t].Len = unset
	}
	return s
}

// refill serves type t's miss of an object of the given length, which a struct
// has none of. The first miss of an entry only arms the type, so a call that
// allocates once builds nothing ahead; the next builds what the last entry
// needed beyond the two misses served so far, at least 16, and each later miss
// in the entry doubles that, stopping early, without failing, when the heap is exhausted, after releasing
// a stock of arrays of another length. A type whose length changes a second
// time in an entry alternates, and builds nothing more until flush, so each of
// its allocations costs the callout alone. Native code takes the batch's lowest
// address first, so a batch is stored last first.
func (s *stock) refill(i *Interpreter, t, length int) {
	s.dirty = true
	s.missed[t]++
	if s.step[t] == 0 {
		// Two misses are served before the first fill, the arming one and
		// the one that fills.
		s.step[t] = min(max(s.learned[t]-2, first), last)
		return
	}
	if length > wide || s.shifts[t] > 1 {
		return
	}
	if was := s.table[t].Len; int64(length) != was {
		if was != unset {
			s.shifts[t]++
		}
		mark := len(i.free)
		s.drop(i, t)
		i.shrink(mark)
		if s.shifts[t] > 1 {
			return
		}
	}
	size := s.step[t]
	s.step[t] = min(2*size, last)

	n := int(s.table[t].N)
	refs := s.refs[t][:n]
	s.filling = true
	defer func() {
		s.filling = false
		if r := recover(); r != nil {
			if err, ok := r.(error); !ok || !errors.Is(err, ErrHeapExhausted) {
				panic(r)
			}
		}
		slices.Reverse(refs[n:])
		s.built[t] += len(refs) - n
		s.refs[t] = refs
		s.table[t] = jit.Stock{Refs: base(refs), N: int64(len(refs)), Len: int64(length)}
		s.held = s.held || len(refs) > 0
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

// flush releases every ref still stocked, so the heap reads as though native
// code had allocated only what it took, resets every refill size and stocked length, and reports
// whether it released any ref. Returning the stock at once frees its slots to
// the allocator, and the ones that end the heap are dropped from it.
func (s *stock) flush(i *Interpreter) bool {
	if !s.dirty || s.filling {
		return false
	}
	released := s.held
	s.dirty, s.held = false, false
	mark := len(i.free)
	for t := range s.table {
		s.drop(i, t)
		if s.built[t] > 0 {
			s.learned[t] = s.built[t] - s.left[t] + s.missed[t]
		}
		s.step[t], s.built[t], s.left[t], s.missed[t], s.shifts[t], s.table[t].Len = 0, 0, 0, 0, 0, unset
	}
	i.shrink(mark)
	return released
}

// drop releases every ref type t still has stocked, leaving the slots it
// frees to the caller to shrink.
func (s *stock) drop(i *Interpreter, t int) {
	s.left[t] += int(s.table[t].N)
	for e := &s.table[t]; e.N > 0; {
		e.N--
		i.undo(s.refs[t][e.N].Ref())
	}
}
