package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type Function struct {
	Signature *FunctionSignature
	Params    int
	Returns   int
	Locals    int
	Code      []byte
}

type FunctionSignature struct {
	FunctionType
	Locals []Type
}

type FunctionType struct {
	Params  []Type
	Returns []Type
}

var _ Value = (*Function)(nil)
var _ Type = (*FunctionType)(nil)

func WithParams(types ...Type) func(*FunctionSignature) {
	return func(s *FunctionSignature) {
		s.Params = append(s.Params, types...)
	}
}

func WithReturns(types ...Type) func(*FunctionSignature) {
	return func(s *FunctionSignature) {
		s.Returns = append(s.Returns, types...)
	}
}

func WithLocals(types ...Type) func(*FunctionSignature) {
	return func(s *FunctionSignature) {
		s.Locals = append(s.Locals, types...)
	}
}

func NewFunction(signature *FunctionSignature, instrs ...instr.Instruction) *Function {
	return &Function{
		Signature: signature,
		Params:    len(signature.Params),
		Returns:   len(signature.Returns),
		Locals:    len(signature.Params) + len(signature.Locals),
		Code:      instr.Marshal(instrs),
	}
}

func (f *Function) Type() Type {
	return f.Signature.Type()
}

func (f *Function) Kind() Kind {
	return KindRef
}

func (f *Function) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Signature.String()))
	sb.WriteString(instr.Disassemble(f.Code))
	return sb.String()
}

func NewFunctionSignature(opts ...func(*FunctionSignature)) *FunctionSignature {
	typ := &FunctionSignature{}
	for _, opt := range opts {
		opt(typ)
	}
	return typ
}

func (s *FunctionSignature) Type() *FunctionType {
	return &s.FunctionType
}

func (s *FunctionSignature) String() string {
	var sb strings.Builder
	sb.WriteString(s.FunctionType.String())
	for _, t := range s.Locals {
		sb.WriteString("\n")
		sb.WriteString(t.String())
	}
	return sb.String()
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
