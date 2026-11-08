package interp

import "slices"

// freelist keeps the values Reset cleared so the next run reuses them. live
// and peak track how many are in use, which sizes what survives a Reset.
type freelist[T any] struct {
	values []T
	live   int
	peak   int
	// keep is how many values a trim let survive; trimmed reports that the
	// bound holds until reset.
	keep    int
	trimmed bool
}

func (p *freelist[T]) add() {
	p.live++
	p.peak = max(p.peak, p.live)
}

func (p *freelist[T]) remove() {
	p.live--
}

func (p *freelist[T]) get() (T, bool) {
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

func (p *freelist[T]) put(value T) {
	p.values = append(p.values, value)
}

func (p *freelist[T]) trim(dynamic int) {
	keep := max(p.peak, min(dynamic, len(p.values)))
	if keep < len(p.values) {
		clear(p.values[keep:])
		p.values = p.values[:keep]
	}
	p.values = slices.Clip(p.values)
	p.keep, p.trimmed = keep, true
}

func (p *freelist[T]) room() bool {
	return !p.trimmed || len(p.values) < p.keep
}

func (p *freelist[T]) clear() {
	clear(p.values)
	p.values = nil
	p.reset()
}

func (p *freelist[T]) reset() {
	p.live = 0
	p.peak = 0
	p.trimmed = false
}
