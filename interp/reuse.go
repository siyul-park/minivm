package interp

import "slices"

// recycler keeps the values Reset cleared so the next run reuses them. live
// and peak track how many are in use, which sizes what survives a Reset.
type recycler[T any] struct {
	values []T
	live   int
	peak   int
}

func (p *recycler[T]) add() {
	p.live++
	p.peak = max(p.peak, p.live)
}

func (p *recycler[T]) remove() {
	p.live--
}

func (p *recycler[T]) get() (T, bool) {
	if len(p.values) == 0 {
		var zero T
		return zero, false
	}
	last := len(p.values) - 1
	value := p.values[last]
	var zero T
	p.values[last] = zero
	p.values = p.values[:last]
	return value, true
}

func (p *recycler[T]) put(value T) {
	p.values = append(p.values, value)
}

func (p *recycler[T]) trim(dynamic int) int {
	keep := max(p.peak, min(dynamic, len(p.values)))
	if keep < len(p.values) {
		clear(p.values[keep:])
		p.values = p.values[:keep]
	}
	p.values = slices.Clip(p.values)
	return keep
}

func (p *recycler[T]) reset() {
	p.live = 0
	p.peak = 0
}

func (p *recycler[T]) clear() {
	clear(p.values)
	p.values = nil
	p.reset()
}
