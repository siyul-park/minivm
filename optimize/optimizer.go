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

	// No pass a level composes asks for an analysis, since every rewrite of a
	// function's own code happens over SSA. A transform added through Add can,
	// and this manager is the only one it ever sees.
	pass.Register(o.manager, analysis.NewBlocksAnalysis())
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

// transforms returns the cumulative transform pipeline for the optimizer
// level. Every rewrite of a function's own code is one of internal/ssa's
// transformation policies, run over each function through the
// bytecode-to-SSA-to-bytecode route: O1 folds and sweeps what folding leaves
// behind, O2 adds the dominance-scoped common-subexpression elimination and
// the guard elimination that rides on it, and O3 adds the local promotion that
// turns a slot read and written only through itself into values, the
// redundant-load forwarding that makes a repeated read of what is left one
// value, and the loop-invariant code motion that only reads well once the rest
// has canonicalized the function.
// DedupPass follows the route at every level: a constant pool is a
// whole-program concern no per-function IR has a counterpart for, and it has
// the folded constants the route interned to collect.
func (o *Optimizer) transforms() []pass.Pass[*program.Program] {
	switch o.level {
	case O1:
		return route(ssapass.NewFoldPass(), ssapass.NewDCEPass())
	case O2:
		return route(ssapass.NewFoldPass(), ssapass.NewCSEPass(), ssapass.NewGuardPass(), ssapass.NewDCEPass())
	case O3:
		return route(ssapass.NewFoldPass(), ssapass.NewPromotePass(), ssapass.NewForwardPass(),
			ssapass.NewCSEPass(), ssapass.NewGuardPass(), ssapass.NewHoistPass(), ssapass.NewDCEPass())
	default:
		return nil
	}
}

// route composes passes into the one pass that runs them over every function a
// program holds, followed by the constant-pool deduplication that collects
// what they interned.
func route(passes ...pass.Pass[*ssa.Function]) []pass.Pass[*program.Program] {
	pipeline := pass.NewPipeline[*ssa.Function]()
	for _, p := range passes {
		pipeline.Add(p)
	}
	return []pass.Pass[*program.Program]{transform.NewSSAPass(pipeline), transform.NewDedupPass()}
}
