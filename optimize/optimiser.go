package optimize

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/transform"
)

type Optimizer struct {
	level   Level
	manager *pass.Manager
}

type Level int

const (
	O0 Level = iota
	O1
)

func NewOptimizer(level Level) *Optimizer {
	opt := &Optimizer{level: level, manager: pass.NewManager()}

	_ = opt.manager.Register(analysis.NewBasicBlocksPass())

	switch level {
	case O0:
	case O1:
		_ = opt.manager.Register(transform.NewConstantFoldingPass())
		_ = opt.manager.Register(transform.NewConstantDeduplicationPass())
		_ = opt.manager.Register(transform.NewDeadCodeEliminationPass())
	}

	return opt
}

func (o *Optimizer) Level() Level {
	return o.level
}

func (o *Optimizer) Register(pass any) error {
	return o.manager.Register(pass)
}

func (o *Optimizer) Optimize(prog *program.Program) (*program.Program, error) {
	if err := o.manager.Run(prog); err != nil {
		return nil, err
	}
	return prog, o.manager.Load(&prog)
}
