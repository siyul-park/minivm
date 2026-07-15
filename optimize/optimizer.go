package optimize

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/transform"
)

type Optimizer struct {
	pipeline *pass.Pipeline[*program.Program]
	manager  *pass.Manager

	level Level
}

type Level int

const (
	O0 Level = iota
	O1
	O2
	O3
)

func New(level Level) *Optimizer {
	o := &Optimizer{
		pipeline: pass.NewPipeline[*program.Program](),
		manager:  pass.NewManager(),
		level:    level,
	}

	pass.Register(o.manager, analysis.NewBlocksAnalysis())
	pass.Register(o.manager, analysis.NewGVNAnalysis())
	for _, p := range o.transforms() {
		o.pipeline.Add(p)
	}

	return o
}

func (o *Optimizer) Optimize(prog *program.Program) (*program.Program, error) {
	return o.pipeline.Run(o.manager, prog)
}

func (o *Optimizer) Level() Level {
	return o.level
}

// Add appends a custom transform to the optimizer pipeline.
func (o *Optimizer) Add(p pass.Pass[*program.Program]) {
	o.pipeline.Add(p)
}

// transforms returns the cumulative transform pipeline for the optimizer level:
// O1 runs cheap local rewrites, O2 adds CFG-based passes, O3 adds cross-block
// global value numbering (which subsumes block-local CSE) on top.
func (o *Optimizer) transforms() []pass.Pass[*program.Program] {
	switch o.level {
	case O1:
		return []pass.Pass[*program.Program]{
			transform.NewFoldPass(),
			transform.NewDedupPass(),
		}
	case O2:
		return []pass.Pass[*program.Program]{
			transform.NewFoldPass(),
			transform.NewAlgebraicPass(),
			transform.NewDedupPass(),
			transform.NewDCEPass(),
		}
	case O3:
		return []pass.Pass[*program.Program]{
			transform.NewFoldPass(),
			transform.NewAlgebraicPass(),
			transform.NewGVNPass(),
			transform.NewDedupPass(),
			transform.NewDCEPass(),
		}
	default:
		return nil
	}
}
