package interp

import (
	"errors"
	"slices"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"
)

// stock is the default structs of each type the interpreter built ahead of
// native code, which takes them without leaving it. Each stocked ref is an
// object of count 1 that no one but native code may take, so until it does
// the stock owns it: flush returns every ref still unused.
type stock struct {
	// table is what native code reads, one jit.Stock per type index; refs
	// are the arrays the table's pointers address, kept here so they
	// outlive native code's uintptr views of them.
	table []jit.Stock
	refs  [][]types.Boxed
	// step is how many structs each type's next refill builds; zero until
	// the type's first miss, which only arms it.
	step []int
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
)

// newStock returns an empty stock for n types.
func newStock(n int) stock {
	return stock{table: make([]jit.Stock, n), refs: make([][]types.Boxed, n), step: make([]int, n)}
}

// refill serves type t's miss. The first miss of an entry only arms the
// type, so a call that allocates once builds nothing ahead; each later miss
// builds the next batch of its structs, stopping early, without failing, when
// the heap is exhausted. Native code takes the batch's lowest address first,
// so a batch is stored last first.
func (s *stock) refill(i *Interpreter, t int) {
	s.dirty = true
	if s.step[t] == 0 {
		s.step[t] = first
		return
	}
	typ := i.types[t].(*types.StructType)
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
		s.refs[t] = refs
		s.table[t] = jit.Stock{Refs: base(refs), N: int64(len(refs))}
		s.held = s.held || len(refs) > 0
	}()
	for range size {
		refs = append(refs, types.BoxRef(i.alloc(i.newStruct(typ))))
	}
}

// flush releases every ref still stocked, so the heap reads as though native
// code had allocated only what it took, resets every refill size, and reports
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
		for e := &s.table[t]; e.N > 0; {
			e.N--
			i.release(s.refs[t][e.N].Ref())
		}
		s.step[t] = 0
	}
	i.shrink(mark)
	return released
}
