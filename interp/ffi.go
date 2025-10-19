package interp

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/types"
)

type NativeFunction struct {
	Signature *types.FunctionSignature
	Params    int
	Returns   int
	Fn        func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)
}

var _ types.Value = (*NativeFunction)(nil)

func NewNativeFunction(signature *types.FunctionSignature, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *NativeFunction {
	return &NativeFunction{
		Signature: signature,
		Params:    len(signature.Params),
		Returns:   len(signature.Returns),
		Fn:        fn,
	}
}

func (f *NativeFunction) Kind() types.Kind {
	return types.KindRef
}

func (f *NativeFunction) Type() types.Type {
	return f.Signature.Type()
}

func (f *NativeFunction) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Signature.String()))
	sb.WriteString("<native>")
	return sb.String()
}
