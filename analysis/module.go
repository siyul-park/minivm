package analysis

import (
	"errors"

	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type ModulePass struct{}

type Module struct {
	EntryPoint *Function
	Functions  []*Function
	Constants  []types.Value
	Types      []types.Type
}

type Function struct {
	*types.Function
	Blocks []*BasicBlock
}

var _ pass.Pass[*Module] = (*ModulePass)(nil)

var ErrInvalidJump = errors.New("invalid jump")

func NewModulePass() pass.Pass[*Module] {
	return &ModulePass{}
}

func (p *ModulePass) Run(m *pass.Manager) (*Module, error) {
	var prog *program.Program
	if err := m.Load(&prog); err != nil {
		return nil, err
	}

	var fns []*Function
	fns = append(fns, &Function{
		Function: &types.Function{
			Signature: types.NewFunctionSignature(),
			Code:      prog.Code,
		},
	})
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, &Function{Function: fn})
		}
	}

	for _, fn := range fns {
		err := m.Convert(fn.Function, &fn.Blocks)
		if err != nil {
			return nil, err
		}
	}

	mdl := &Module{
		EntryPoint: fns[0],
		Constants:  prog.Constants,
		Types:      prog.Types,
	}
	if len(fns) > 1 {
		mdl.Functions = fns[1:]
	}
	return mdl, nil
}

func (m *Module) AllFunctions() []*Function {
	fns := make([]*Function, 0, len(m.Functions)+1)
	if m.EntryPoint != nil {
		fns = append(fns, m.EntryPoint)
	}
	fns = append(fns, m.Functions...)
	return fns
}
