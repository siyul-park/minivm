package optimize

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/internal/ssa"
	ssapass "github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/transform"
)

type Optimizer struct {
	pipeline *pass.Pipeline[*program.Program]
	manager  *pass.Manager

	level Level
	ssa   bool
}

type Level int

const (
	O0 Level = iota
	O1
	O2
	O3
)

// WithSSA adds the level's SSA passes, run over each function through the
// bytecode-to-SSA-to-bytecode route, to the pipeline. They are the same
// folding, common-subexpression elimination, and dead-code elimination the
// bytecode passes perform, over the IR a compile uses, and the route exists so
// one implementation of each can serve both.
//
// It is off by default only while transform still owns those bytecode passes:
// running both would do the work twice, and the SSA passes do not yet subsume
// them - nothing there forwards a redundant load, which is most of what
// transform.GVNPass eliminates. The option goes away with the bytecode passes
// it defers to.
func WithSSA() func(*Optimizer) {
	return func(o *Optimizer) {
		o.ssa = true
	}
}

func New(level Level, options ...func(*Optimizer)) *Optimizer {
	o := &Optimizer{
		pipeline: pass.NewPipeline[*program.Program](),
		manager:  pass.NewManager(),
		level:    level,
	}
	for _, opt := range options {
		opt(o)
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
		return append([]pass.Pass[*program.Program]{
			transform.NewFoldPass(),
			transform.NewDedupPass(),
		}, o.route(ssapass.NewFoldPass(), ssapass.NewDCEPass())...)
	case O2:
		return append([]pass.Pass[*program.Program]{
			transform.NewFoldPass(),
			transform.NewAlgebraicPass(),
			transform.NewDedupPass(),
			transform.NewDCEPass(),
		}, o.route(ssapass.NewFoldPass(), ssapass.NewCSEPass(), ssapass.NewGuardPass(), ssapass.NewDCEPass())...)
	case O3:
		return append([]pass.Pass[*program.Program]{
			transform.NewFoldPass(),
			transform.NewAlgebraicPass(),
			transform.NewGVNPass(),
			transform.NewDedupPass(),
			transform.NewDCEPass(),
		}, o.route(ssapass.NewFoldPass(), ssapass.NewCSEPass(), ssapass.NewGuardPass(), ssapass.NewHoistPass(), ssapass.NewDCEPass())...)
	default:
		return nil
	}
}

// route composes passes into the one pass that runs them over every function a
// program holds, or nothing when the optimizer was not asked for them.
func (o *Optimizer) route(passes ...pass.Pass[*ssa.Function]) []pass.Pass[*program.Program] {
	if !o.ssa {
		return nil
	}
	pipeline := pass.NewPipeline[*ssa.Function]()
	for _, p := range passes {
		pipeline.Add(p)
	}
	return []pass.Pass[*program.Program]{transform.NewSSAPass(pipeline)}
}
