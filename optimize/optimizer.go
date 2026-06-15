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
)

func NewOptimizer(level Level) *Optimizer {
	o := &Optimizer{
		pipeline: pass.NewPipeline[*program.Program](),
		manager:  pass.NewManager(),
		level:    level,
	}

	pass.Register(o.manager, analysis.NewBasicBlocksAnalysis())
	for _, p := range o.transforms() {
		o.pipeline.AddPass(p)
	}

	return o
}

func (o *Optimizer) Optimize(prog *program.Program) (*program.Program, error) {
	return o.pipeline.Run(o.manager, prog)
}

func (o *Optimizer) Level() Level {
	return o.level
}

// AddPass appends a custom transform to the optimizer pipeline.
func (o *Optimizer) AddPass(p pass.Pass[*program.Program]) {
	o.pipeline.AddPass(p)
}

// transforms returns the cumulative transform pipeline for the optimizer level:
// O1 runs cheap local rewrites, O2 adds CFG-based passes.
func (o *Optimizer) transforms() []pass.Pass[*program.Program] {
	switch o.level {
	case O1:
		return []pass.Pass[*program.Program]{
			transform.NewConstantFoldingPass(),
			transform.NewConstantDeduplicationPass(),
		}
	case O2:
		return []pass.Pass[*program.Program]{
			transform.NewConstantFoldingPass(),
			transform.NewAlgebraicSimplificationPass(),
			transform.NewConstantDeduplicationPass(),
			transform.NewDeadCodeEliminationPass(),
		}
	default:
		return nil
	}
}
