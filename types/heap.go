package types

import (
	"errors"
	"io"
	"math"
	"slices"
	"unsafe"
)

// Heap holds the values a runtime references by address, counting each slot's
// references and collecting unreachable cycles. It is not safe for concurrent
// use, and it MUST NOT be copied after first use.
//
// Address 0 is the runtime's null slot: Inc counts it like any slot, and
// callers of Dec skip it. The methods that name an address take it unchecked,
// for the runtime that owns the heap: a caller that cannot trust an address
// checks it with Alive first.
type Heap struct {
	objects []Value
	rc      []int
	free    []int
	base    int
	target  int
	limit   int
	trial   []int
	work    []int
	refbuf  []Ref
	arrays  freelist[*Array]
	structs freelist[*Struct]
	hooks   Hooks
	// reloaded reports a Reset since NewHeap, after which Recount paces from
	// the live set alone.
	reloaded bool
}

// Hooks are the runtime a heap serves, fixed at NewHeap. A nil Exhaust or
// Fault panics where it would be called; a nil Finalize or Collect does
// nothing.
type Hooks struct {
	// Exhaust is called when the limit is reached after a collection. It
	// returns only after freeing space, which the heap then retries, or panics.
	Exhaust func()
	// Fault is called when a collection finds corruption. It panics.
	Fault func()
	// Finalize is called when a *Function or an io.Closer leaves its slot.
	Finalize func(addr int, v Value)
	// Collect is called after each collection with the slots it walked and the
	// slots it freed.
	Collect func(slots, freed int)
}

const heapRunway = 64

var errHook = errors.New("types: heap hook is not set")

// NewHeap returns an empty heap with room for capacity values that holds at
// most limit slots, or any number when limit is 0.
func NewHeap(capacity, limit int, hooks Hooks) *Heap {
	return &Heap{
		objects: make([]Value, 0, capacity),
		rc:      make([]int, 0, capacity),
		free:    make([]int, 0, capacity),
		limit:   limit,
		hooks:   hooks,
	}
}

// At returns the value at addr, for the runtime that owns the heap.
func (h *Heap) At(addr int) Value {
	return h.objects[addr]
}

// Set replaces the value at addr without changing its count, for the runtime
// that owns the heap.
func (h *Heap) Set(addr int, v Value) {
	h.objects[addr] = v
}

// Add stores v in a new slot with one reference and returns its address, for
// the runtime that owns the heap. It collects when the heap reaches its pace,
// and calls Hooks.Exhaust when the limit is reached.
func (h *Heap) Add(v Value) int {
	collected := h.target > 0 && h.Live() >= h.target
	if collected {
		h.gc()
	}
	if addr, ok := h.reuse(v); ok {
		h.track(v)
		return addr
	}

	full := len(h.objects) == cap(h.objects)
	limited := h.limit > 0 && len(h.objects) >= h.limit
	if !collected && (full || limited) {
		h.gc()
		if addr, ok := h.reuse(v); ok {
			h.track(v)
			return addr
		}
	}
	if limited {
		if h.hooks.Exhaust == nil {
			panic(errHook)
		}
		h.hooks.Exhaust()
		return h.Add(v)
	}

	if full {
		c := 2 * cap(h.objects)
		if c == 0 {
			c = 1
		}
		objects := make([]Value, len(h.objects), c)
		copy(objects, h.objects)
		h.objects = objects

		rc := make([]int, len(h.rc), c)
		copy(rc, h.rc)
		h.rc = rc
	}

	h.objects = append(h.objects, v)
	h.rc = append(h.rc, 1)
	h.track(v)
	return len(h.objects) - 1
}

// Inc adds n references to addr, for the runtime that owns the heap.
func (h *Heap) Inc(addr, n int) {
	h.rc[addr] += n
}

// Dec removes one reference from addr and reclaims the value with its last,
// releasing what it references, for the runtime that owns the heap.
func (h *Heap) Dec(addr int) {
	if h.rc[addr] > 1 {
		h.rc[addr]--
		return
	}

	base := len(h.work)
	h.work = append(h.work, addr)
	for len(h.work) > base {
		next := h.work[len(h.work)-1]
		h.work = h.work[:len(h.work)-1]

		h.rc[next]--
		if h.rc[next] == 0 {
			v := h.objects[next]
			for _, r := range h.refs(v) {
				h.work = append(h.work, int(r))
			}
			h.reclaim(next, v)
		}
	}
}

// Retain adds one reference to the value v refers to, null included, and
// ignores a v that is not a reference, for the runtime that owns the heap.
func (h *Heap) Retain(v Boxed) {
	if v.Kind() == KindRef {
		h.Inc(v.Ref(), 1)
	}
}

// Release removes one reference from the value v refers to, as Dec, and
// ignores null and a v that is not a reference, for the runtime that owns the
// heap.
func (h *Heap) Release(v Boxed) {
	if v.Kind() == KindRef && v.Ref() != 0 {
		h.Dec(v.Ref())
	}
}

// Count returns the reference count of addr, for the runtime that owns the
// heap.
func (h *Heap) Count(addr int) int {
	return h.rc[addr]
}

// Alive reports whether addr names a value with a reference.
func (h *Heap) Alive(addr int) bool {
	return addr >= 0 && addr < len(h.objects) && h.rc[addr] > 0
}

// Len returns one past the highest address the heap has allocated.
func (h *Heap) Len() int {
	return len(h.objects)
}

// Live returns the number of slots in use.
func (h *Heap) Live() int {
	return len(h.objects) - len(h.free)
}

// Dispose releases what old references and finalizes it, leaving the slot at
// addr allocated, so a replaced value keeps its address and count. It is for
// the runtime that owns the heap.
func (h *Heap) Dispose(addr int, old Value) {
	var local [8]int
	children := local[:0]
	for _, ref := range h.refs(old) {
		children = append(children, int(ref))
	}
	for _, child := range children {
		h.Dec(child)
	}
	h.finalize(addr, old)
}

// Recount takes the heap as the baseline every Reset returns to and rebuilds
// its counts from roots, one reference each, and from the edges between
// values, for the runtime that owns the heap. The heap MUST already hold the
// null slot at address 0. It sets the collection pace: a
// fresh heap first collects once its capacity is in use, a reloaded one once
// the live set has grown by the pace's runway.
func (h *Heap) Recount(roots []Boxed) {
	h.base = len(h.objects)
	clear(h.rc)
	h.rc[0] = 1
	for _, val := range roots {
		if val.Kind() != KindRef {
			continue
		}
		addr := val.Ref()
		if addr >= 0 && addr < len(h.rc) {
			h.rc[addr]++
		}
	}
	for addr := 1; addr < len(h.objects); addr++ {
		for _, ref := range h.refs(h.objects[addr]) {
			child := int(ref)
			if child >= 0 && child < len(h.rc) {
				h.rc[child]++
			}
		}
	}
	h.pace()
	if !h.reloaded {
		// A fresh heap's first collection waits for the capacity it holds.
		target := max(h.target, cap(h.objects))
		if h.limit > 0 {
			target = min(target, h.limit)
		}
		h.target = max(target, h.base)
	}
}

// Reset finalizes every value above the baseline and empties the heap, so the
// runtime can reload it. It keeps the freed structs and arrays for reuse and
// stops collecting until the next Recount.
func (h *Heap) Reset() {
	// Keep what the ending run peaked at.
	dynamic := len(h.objects) - h.base
	h.structs.trim(dynamic)
	h.arrays.trim(dynamic)
	for addr := h.base; addr < len(h.objects); addr++ {
		value := h.objects[addr]
		if h.rc[addr] <= 0 {
			continue
		}
		h.finalize(addr, value)
		h.recycle(value, false)
	}
	h.structs.reset()
	h.arrays.reset()

	clear(h.objects[:cap(h.objects)])
	h.objects = h.objects[:0]
	clear(h.rc[:cap(h.rc)])
	h.rc = h.rc[:0]
	h.free = h.free[:0]
	h.target = 0
	h.reloaded = true
}

// Drain drops the structs and arrays the heap kept for reuse.
func (h *Heap) Drain() {
	h.arrays.clear()
	h.structs.clear()
}

// Undo frees the value at addr that no one ever published, which holds the
// only reference, without finalizing it or walking its references. It keeps
// the value for reuse. A generic array holds only nulls, whose references go
// with it. Undo is for the runtime that owns the heap.
func (h *Heap) Undo(addr int) {
	v := h.objects[addr]
	if array, ok := v.(*Array); ok {
		h.rc[0] -= len(array.Elems)
	}
	h.recycle(v, false)
	h.objects[addr] = nil
	h.rc[addr] = 0
	h.free = append(h.free, addr)
}

// Mark returns a position for Shrink.
func (h *Heap) Mark() int {
	return len(h.free)
}

// Shrink drops from the end of the heap the slots freed since mark that
// end it, which only grew to hold them.
func (h *Heap) Shrink(mark int) {
	tail := h.free[mark:]
	slices.Sort(tail)
	for len(tail) > 0 && tail[len(tail)-1] == len(h.objects)-1 {
		h.objects, h.rc = h.objects[:len(h.objects)-1], h.rc[:len(h.rc)-1]
		tail = tail[:len(tail)-1]
	}
	h.free = h.free[:mark+len(tail)]
}

// Room returns how many more slots the heap takes before it collects, or
// math.MaxInt when it does not.
func (h *Heap) Room() int {
	if h.target == 0 {
		return math.MaxInt
	}
	return h.target - h.Live()
}

// Limit returns the most slots the heap holds, or 0 for any number.
func (h *Heap) Limit() int {
	return h.limit
}

// SetLimit sets the most slots the heap holds; 0 lifts it.
func (h *Heap) SetLimit(n int) {
	h.limit = n
}

// Bases returns the addresses of the arrays holding the values and their
// counts, for native code that reads them in place. They move when the heap
// grows.
func (h *Heap) Bases() (objects, rc uintptr) {
	return uintptr(unsafe.Pointer(unsafe.SliceData(h.objects))), uintptr(unsafe.Pointer(unsafe.SliceData(h.rc)))
}

func (h *Heap) reuse(v Value) (int, bool) {
	if len(h.free) == 0 {
		return 0, false
	}
	addr := h.free[len(h.free)-1]
	h.free = h.free[:len(h.free)-1]
	h.objects[addr] = v
	h.rc[addr] = 1
	return addr, true
}

// track counts a newly allocated value the free lists may later keep.
func (h *Heap) track(v Value) {
	switch v := v.(type) {
	case *Struct:
		if len(v.Typ.Fields) <= 4 {
			h.structs.add()
		}
	case *Array:
		h.arrays.add()
	}
}

// gc collects one cycle. Every pass walks the whole heap.
func (h *Heap) gc() {
	slots := len(h.objects)
	h.scan()
	h.mark()
	freed := h.sweep()
	h.pace()
	if h.hooks.Collect != nil {
		h.hooks.Collect(slots, freed)
	}
}

// scan derives each object's external incoming count. Exact rc includes both
// heap edges and owners outside the heap; subtracting every heap-to-heap edge
// leaves a positive value only for objects owned by the stack, constants,
// globals, frames, temporary construction state, or the host.
func (h *Heap) scan() {
	n := len(h.rc)
	if cap(h.trial) < n {
		h.trial = make([]int, n)
	} else {
		h.trial = h.trial[:n]
	}
	copy(h.trial, h.rc)

	for addr := 1; addr < n; addr++ {
		if h.rc[addr] <= 0 {
			continue
		}
		for _, ref := range h.refs(h.objects[addr]) {
			child := int(ref)
			if child == 0 {
				continue
			}
			if child < 0 || child >= n || h.rc[child] <= 0 {
				h.fault()
			}
			h.trial[child]--
		}
	}
	for addr := 1; addr < n; addr++ {
		if h.rc[addr] > 0 && h.trial[addr] < 0 {
			h.fault()
		}
	}
}

// mark traces from every positive external count. A negative trial value marks
// a survivor; zero remains an unreachable cycle candidate.
func (h *Heap) mark() {
	var work []int
	push := func(addr int) {
		if addr <= 0 || addr >= len(h.rc) || h.rc[addr] <= 0 || h.trial[addr] < 0 {
			return
		}
		h.trial[addr] = -h.trial[addr] - 1
		work = append(work, addr)
	}

	for addr := 1; addr < len(h.rc); addr++ {
		if h.rc[addr] > 0 && h.trial[addr] > 0 {
			push(addr)
		}
	}
	for len(work) > 0 {
		addr := work[len(work)-1]
		work = work[:len(work)-1]
		for _, ref := range h.refs(h.objects[addr]) {
			push(int(ref))
		}
	}
}

// sweep reclaims every allocated unmarked slot and returns how many. Edges
// from dead objects to survivors are removed from exact rc; dead-to-dead edges
// need no adjustment because both slots are discarded by this pass.
func (h *Heap) sweep() int {
	freed := 0
	for addr := 1; addr < len(h.objects); addr++ {
		if h.rc[addr] <= 0 || h.trial[addr] < 0 {
			continue
		}
		v := h.objects[addr]
		for _, ref := range h.refs(v) {
			child := int(ref)
			if child > 0 && child < len(h.rc) && h.rc[child] > 0 && h.trial[child] < 0 {
				h.rc[child]--
			}
		}
		h.rc[addr] = 0
		h.reclaim(addr, v)
		freed++
	}
	return freed
}

func (h *Heap) pace() {
	live := h.Live()
	target := live + max(live-h.base, heapRunway)
	if h.limit > 0 {
		target = min(target, h.limit)
	}
	h.target = max(target, live)
}

func (h *Heap) fault() {
	if h.hooks.Fault != nil {
		h.hooks.Fault()
	}
	panic(errHook)
}

// reclaim finalizes slot addr holding v, clears it, and returns the stable
// address to the free list. The caller has already settled its referents.
func (h *Heap) reclaim(addr int, v Value) {
	h.finalize(addr, v)
	h.recycle(v, true)
	h.objects[addr] = nil
	h.free = append(h.free, addr)
}

// recycle keeps a freed value for reuse. A struct of up to four fields is
// kept whenever it is freed; an array only once no one can hold it, because a
// host may hold an *Array it loaded. escaped reports that someone may still
// hold v, which Reset and an allocation that was never published know not.
func (h *Heap) recycle(v Value, escaped bool) {
	switch v := v.(type) {
	case *Struct:
		if len(v.Typ.Fields) > 4 {
			return
		}
		h.structs.remove()
		if escaped {
			h.structs.put(v)
		} else if h.structs.room() {
			*v = Struct{}
			h.structs.put(v)
		}
	case *Array:
		h.arrays.remove()
		if !escaped && h.arrays.room() {
			clear(v.Elems[:cap(v.Elems)])
			v.Typ = nil
			v.Elems = v.Elems[:0]
			h.arrays.put(v)
		}
	}
}

func (h *Heap) finalize(addr int, v Value) {
	if h.hooks.Finalize == nil {
		return
	}
	switch v.(type) {
	case *Function, io.Closer:
		h.hooks.Finalize(addr, v)
	}
}

// refs returns v's nested refs using the heap's reused scratch buffer, or nil
// if v is not Traceable. The result is only valid until the next call.
func (h *Heap) refs(v Value) []Ref {
	t, ok := v.(Traceable)
	if !ok {
		return nil
	}
	h.refbuf = t.Refs(h.refbuf[:0])
	return h.refbuf
}
