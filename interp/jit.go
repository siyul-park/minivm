package interp

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/types"
)

type CompiledFunction struct {
	Signature *types.FunctionSignature
	Params    []types.Kind
	Returns   []types.Kind
	Code      *asm.Code
}

type jitCompiler struct {
	types     []types.Type
	constants []types.Boxed
	code      []byte
	params    int
	returns   int
	ip        int
	sp        int
}

var _ types.Value = (*CompiledFunction)(nil)

func NewCompiledFunction(signature *types.FunctionSignature, code *asm.Code) *CompiledFunction {
	fn := &CompiledFunction{
		Signature: signature,
		Code:      code,
	}
	fn.Params = make([]types.Kind, len(signature.Params))
	for i, t := range signature.Params {
		fn.Params[i] = t.Kind()
	}
	fn.Returns = make([]types.Kind, len(signature.Returns))
	for i, t := range signature.Returns {
		fn.Returns[i] = t.Kind()
	}
	return fn
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

func (f *CompiledFunction) Close() error {
	return f.Code.Close()
}
