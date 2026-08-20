package interp

import (
	"fmt"

	"github.com/siyul-park/minivm/types"
)

// HostFunction exposes a Go function to the VM. The codec produces one for a
// marshaled Go function and for every exported method it binds onto a struct,
// and host code may build one directly with NewHostFunction.
type HostFunction struct {
	Typ *types.FunctionType
	Fn  func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)
}

var _ types.Value = (*HostFunction)(nil)

func NewHostFunction(typ *types.FunctionType, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *HostFunction {
	return &HostFunction{Typ: typ, Fn: fn}
}

func (f *HostFunction) Kind() types.Kind { return types.KindRef }
func (f *HostFunction) Type() types.Type { return f.Typ }

func (f *HostFunction) String() string {
	return fmt.Sprintf("%s\n<native>", f.Typ)
}
