//go:build !arm64

package interp

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/types"
)

type CompiledFunction struct{}

type jitCompiler struct {
	types     []types.Type
	constants []types.Boxed
}

var _ types.Value = (*CompiledFunction)(nil)

func (f *CompiledFunction) Run(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
	return nil, ErrUnknownOpcode
}

func (f *CompiledFunction) Kind() types.Kind {
	return types.KindRef
}

func (f *CompiledFunction) Type() types.Type {
	return f.Signature.Type()
}

func (f *CompiledFunction) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Signature.String()))
	sb.WriteString("<compiled>")
	return sb.String()
}

func (c *jitCompiler) Compile(_ *types.Function) (*CompiledFunction, error) {
	return nil, ErrUnknownOpcode
}
