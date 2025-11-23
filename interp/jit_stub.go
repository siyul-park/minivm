//go:build !arm64

package interp

import "github.com/siyul-park/minivm/types"

func (f *CompiledFunction) Run(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
	return nil, ErrUnknownOpcode
}

func (c *jitCompiler) Compile(_ *types.Function) (*CompiledFunction, error) {
	return nil, ErrUnknownOpcode
}
