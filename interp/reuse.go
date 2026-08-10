package interp

import "slices"

type pool[T any] struct {
	values []T
	live   int
	peak   int
}

func (p *pool[T]) add() {
	p.live++
	p.peak = max(p.peak, p.live)
}

func (p *pool[T]) remove() {
	p.live--
}

func (p *pool[T]) get() (T, bool) {
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

func (p *pool[T]) put(value T) {
	p.values = append(p.values, value)
}

func (p *pool[T]) trim(dynamic int) int {
	keep := max(p.peak, min(dynamic, len(p.values)))
	if keep < len(p.values) {
		clear(p.values[keep:])
		p.values = p.values[:keep]
	}
	p.values = slices.Clip(p.values)
	return keep
}

func (p *pool[T]) reset() {
	p.live = 0
	p.peak = 0
}

func (p *pool[T]) clear() {
	clear(p.values)
	p.values = nil
	p.reset()
}
