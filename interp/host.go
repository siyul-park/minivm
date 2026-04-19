package interp

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/types"
)

type HostFunction struct {
	Typ *types.FunctionType
	Fn  func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)
}

var _ types.Value = (*HostFunction)(nil)

func NewHostFunction(typ *types.FunctionType, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *HostFunction {
	return &HostFunction{
		Typ: typ,
		Fn:  fn,
	}
}

func (f *HostFunction) Kind() types.Kind {
	return types.KindRef
}

func (f *HostFunction) Type() types.Type {
	return f.Typ
}

func (f *HostFunction) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Typ.String()))
	sb.WriteString("<native>")
	return sb.String()
}
