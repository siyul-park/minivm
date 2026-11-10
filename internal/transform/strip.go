package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// StripPass removes every deopt state: the state operations, and the state
// each operation and terminator holds. Only an executor that resumes the
// interpreter from a state reads them; bytecode emission does not, so the
// values a state alone keeps alive become dead.
type StripPass struct{}

var _ pass.Pass[*ssa.Function] = (*StripPass)(nil)

// NewStripPass returns the pass.
func NewStripPass() *StripPass {
	return &StripPass{}
}

// Run applies the pass to one SSA function.
func (p *StripPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	changed := false
	r := newRebuilder(function)
	for _, block := range graph.Order(function) {
		id := r.open(block)
		b := function.Block(block)
		for _, operation := range b.Operations {
			if operation.Op == ssa.OpState {
				changed = true
				continue
			}
			operation = r.operation(operation)
			changed = changed || operation.State != ssa.NoValue
			operation.State, operation.Frames = ssa.NoValue, nil
			r.builder.Add(id, r.define(operation))
		}
		term := r.terminator(b.Terminator)
		changed = changed || term.State != ssa.NoValue
		term.State = ssa.NoValue
		r.builder.Term(id, term)
	}

	if !changed {
		return true, nil
	}
	*function = *r.builder.Build()
	return false, nil
}
