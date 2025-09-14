package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type Function struct {
	Typ     *FunctionType
	Params  int
	Returns int
	Locals  int
	Code    []byte
}

type FunctionType struct {
	Params  []Type
	Returns []Type
}

var _ Value = (*Function)(nil)
var _ Type = (*FunctionType)(nil)

func FunctionWithParams(types ...Type) func(*Function) {
	return func(fn *Function) {
		fn.Typ.Params = append(fn.Typ.Params, types...)
		fn.Params += len(types)
		fn.Locals += len(types)
	}
}

func FunctionWithReturns(types ...Type) func(*Function) {
	return func(fn *Function) {
		fn.Typ.Returns = append(fn.Typ.Returns, types...)
		fn.Returns += len(types)
	}
}

func FunctionWithLocals(types ...Type) func(*Function) {
	return func(function *Function) {
		function.Locals += len(types)
	}
}

func NewFunction(instrs []instr.Instruction, opts ...func(*Function)) *Function {
	fn := &Function{
		Typ:  &FunctionType{},
		Code: instr.Marshal(instrs),
	}
	for _, opt := range opts {
		opt(fn)
	}
	return fn
}

func (f *Function) Type() Type {
	return f.Typ
}

func (f *Function) Kind() Kind {
	return KindRef
}

func (f *Function) Interface() any {
	return f
}

func (f *Function) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Typ.String()))
	sb.WriteString(instr.Disassemble(f.Code))
	return sb.String()
}

func (f *FunctionType) Kind() Kind {
	return KindRef
}

func (f *FunctionType) String() string {
	var params []string
	for _, p := range f.Params {
		params = append(params, p.String())
	}

	var returns []string
	for _, r := range f.Returns {
		returns = append(returns, r.String())
	}

	return fmt.Sprintf("func(%s) (%s)", strings.Join(params, ", "), strings.Join(returns, ", "))
}

func (f *FunctionType) Equals(other Type) bool {
	if other == nil {
		return false
	}

	o, ok := other.(*FunctionType)
	if !ok {
		return false
	}

	if len(f.Params) != len(o.Params) || len(f.Returns) != len(o.Returns) {
		return false
	}

	for i := range f.Params {
		if !f.Params[i].Equals(o.Params[i]) {
			return false
		}
	}
	for i := range f.Returns {
		if !f.Returns[i].Equals(o.Returns[i]) {
			return false
		}
	}
	return true
}
