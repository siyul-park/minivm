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
	Locals  []Type
}

var _ Value = (*Function)(nil)
var _ Type = (*FunctionType)(nil)

func WithParams(types ...Type) func(*FunctionType) {
	return func(typ *FunctionType) {
		typ.Params = append(typ.Params, types...)
	}
}

func WithReturns(types ...Type) func(*FunctionType) {
	return func(typ *FunctionType) {
		typ.Returns = append(typ.Returns, types...)
	}
}

func WithLocals(types ...Type) func(*FunctionType) {
	return func(typ *FunctionType) {
		typ.Locals = append(typ.Locals, types...)
	}
}

func NewFunction(typ *FunctionType, instrs ...instr.Instruction) *Function {
	return &Function{
		Typ:     typ,
		Params:  len(typ.Params),
		Returns: len(typ.Returns),
		Locals:  len(typ.Params) + len(typ.Locals),
		Code:    instr.Marshal(instrs),
	}
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

func NewFunctionType(opts ...func(*FunctionType)) *FunctionType {
	typ := &FunctionType{}
	for _, opt := range opts {
		opt(typ)
	}
	return typ
}

func (t *FunctionType) Kind() Kind {
	return KindRef
}

func (t *FunctionType) String() string {
	var params []string
	for _, p := range t.Params {
		params = append(params, p.String())
	}
	var returns []string
	for _, r := range t.Returns {
		returns = append(returns, r.String())
	}
	return fmt.Sprintf("func(%s) (%s)", strings.Join(params, ", "), strings.Join(returns, ", "))
}

func (t *FunctionType) Cast(other Type) bool {
	return t.Equals(other)
}

func (t *FunctionType) Equals(other Type) bool {
	if other == nil {
		return false
	}
	o, ok := other.(*FunctionType)
	if !ok {
		return false
	}
	if len(t.Params) != len(o.Params) || len(t.Returns) != len(o.Returns) {
		return false
	}
	for i := range t.Params {
		if !t.Params[i].Equals(o.Params[i]) {
			return false
		}
	}
	for i := range t.Returns {
		if !t.Returns[i].Equals(o.Returns[i]) {
			return false
		}
	}
	return true
}
