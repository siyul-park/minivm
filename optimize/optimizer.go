// Package optimize composes the configured optimization pipeline.
package optimize

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/transform"
)

// Optimizer runs the selected optimization level.
type Optimizer struct {
	pipeline *pass.Pipeline[*program.Program]
	manager  *pass.Manager

	level Level
}

// Level selects the optimization pipeline.
type Level int

// Optimization levels.
const (
	O0 Level = iota
	O1
	O2
	O3
)

// New returns an optimizer at level.
func New(level Level) *Optimizer {
	o := &Optimizer{
		pipeline: pass.NewPipeline[*program.Program](),
		manager:  pass.NewManager(),
		level:    level,
	}

	pass.Register(o.manager, analysis.NewBlocksAnalysis())
	for _, p := range o.passes() {
		o.pipeline.Add(p)
	}

	return o
}

// Optimize runs the configured passes in place and returns prog.
func (o *Optimizer) Optimize(prog *program.Program) (*program.Program, error) {
	return o.pipeline.Run(o.manager, prog)
}

// Level returns the configured optimization level.
func (o *Optimizer) Level() Level {
	return o.level
}

// Add appends a program-level pass.
func (o *Optimizer) Add(p pass.Pass[*program.Program]) {
	o.pipeline.Add(p)
}

func (o *Optimizer) passes() []pass.Pass[*program.Program] {
	switch o.level {
	case O1:
		return compose(transform.NewFoldPass(), transform.NewDCEPass())
	case O2:
		return compose(transform.NewFoldPass(), transform.NewCSEPass(), transform.NewGuardPass(), transform.NewDCEPass())
	case O3:
		return compose(transform.NewFoldPass(), transform.NewPromotePass(), transform.NewForwardPass(),
			transform.NewCSEPass(), transform.NewGuardPass(), transform.NewHoistPass(), transform.NewDCEPass())
	default:
		return nil
	}
}

func compose(passes ...pass.Pass[*ssa.Function]) []pass.Pass[*program.Program] {
	pipeline := pass.NewPipeline[*ssa.Function]()
	for _, p := range passes {
		pipeline.Add(p)
	}
	return []pass.Pass[*program.Program]{transform.NewSSAPass(pipeline), transform.NewCompactPass()}
}
