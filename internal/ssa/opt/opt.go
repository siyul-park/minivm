// Package opt optimizes internal/ssa's IR: a pass.Pipeline over *ssa.Function
// and the passes it runs. It knows only ssa, graph, pass, instr, and types -
// never the JIT itself - so every pass here is correct and useful whether
// fn came from a bytecode frontend with no guard or deopt state at all, or
// from a trace frontend whose speculation this package's own GuardPass exists
// to clean up after.
package opt

import (
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// Optimizer runs a fixed pipeline of passes over one *ssa.Function.
type Optimizer struct {
	pipeline *pass.Pipeline[*ssa.Function]
	manager  *pass.Manager
}

// New returns an Optimizer running, in order, constant folding, common
// subexpression elimination, redundant guard elimination, and dead code
// elimination. CSEPass runs before GuardPass because a guard's operand is
// only recognizably equal to an earlier guard's once CSEPass has unified the
// values they read; DCEPass runs last because every earlier pass can leave
// behind an operation - a folded computation's now-unused operands, a
// deduplicated guard's now-unread OpState - that only liveness can tell is
// safe to drop.
func New() *Optimizer {
	o := &Optimizer{
		pipeline: pass.NewPipeline[*ssa.Function](),
		manager:  pass.NewManager(),
	}
	o.pipeline.Add(NewFoldPass())
	o.pipeline.Add(NewCSEPass())
	o.pipeline.Add(NewGuardPass())
	o.pipeline.Add(NewDCEPass())
	return o
}

// Optimize runs the pipeline over fn, mutating it in place, and returns it.
func (o *Optimizer) Optimize(fn *ssa.Function) (*ssa.Function, error) {
	return o.pipeline.Run(o.manager, fn)
}

// Add appends a custom pass to the pipeline.
func (o *Optimizer) Add(p pass.Pass[*ssa.Function]) {
	o.pipeline.Add(p)
}
