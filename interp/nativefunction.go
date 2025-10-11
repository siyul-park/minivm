package interp

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/types"
)

type NativeFunction struct {
	Typ     *types.FunctionType
	Params  int
	Returns int
	Fn      func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)
}

var _ types.Value = (*NativeFunction)(nil)

func NewNativeFunction(typ *types.FunctionType, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *NativeFunction {
	return &NativeFunction{
		Typ:     typ,
		Params:  len(typ.Params),
		Returns: len(typ.Returns),
		Fn:      fn,
	}
}

func (f *NativeFunction) Kind() types.Kind {
	return types.KindRef
}

func (f *NativeFunction) Type() types.Type {
	return f.Typ
}

func (f *NativeFunction) Interface() any {
	return f.Fn
}

func (f *NativeFunction) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Typ.String()))
	sb.WriteString("<native>")
	return sb.String()
}
