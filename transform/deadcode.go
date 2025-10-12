package transform

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
)

type DeadCodeEliminationPass struct{}

var _ pass.Pass[*program.Program] = (*DeadCodeEliminationPass)(nil)

func NewDeadCodeEliminationPass() *DeadCodeEliminationPass {
	return &DeadCodeEliminationPass{}
}

func (p *DeadCodeEliminationPass) Run(m *pass.Manager) (*program.Program, error) {
	var prog *program.Program
	var module *analysis.Module
	if err := m.Load(&prog); err != nil {
		return nil, err
	}
	if err := m.Load(&module); err != nil {
		return nil, err
	}

	var fns []*analysis.Function
	fns = append(fns, module.EntryPoint)
	fns = append(fns, module.Functions...)

	for _, fn := range fns {
		for i := 1; i < len(fn.Blocks); i++ {
			blk := fn.Blocks[i]
			if len(blk.Preds) == 0 {
				for j := blk.Start; j < blk.End; j++ {
					fn.Code[j] = byte(instr.UNREACHABLE)
				}
			}
		}
	}
	return prog, nil
}
