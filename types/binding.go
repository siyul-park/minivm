package types

// binding ties a container to the heap that holds the references it owns.
// A nil heap makes the container unbound: it can be read, filled, and built,
// but not released into.
type binding struct {
	heap *Heap
}

// bound returns the heap the container's references live in.
func (b *binding) bound() *Heap {
	if b.heap == nil {
		panic(ErrDetached)
	}
	return b.heap
}

// adopt binds an unbound container to h and reports whether the container is
// bound to h afterwards.
func (b *binding) adopt(h *Heap) bool {
	if b.heap == nil {
		b.heap = h
	}
	return b.heap == h
}
