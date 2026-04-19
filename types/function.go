package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type FunctionBuilder struct {
	typ    *FunctionType
	locals []Type
	instrs []instr.Instruction
}

type Function struct {
	Typ    *FunctionType
	Locals []Type
	Code   []byte
}

type FunctionType struct {
	Params  []Type
	Returns []Type
}

var _ Value = (*Function)(nil)
var _ Type = (*FunctionType)(nil)

func NewFunctionBuilder(typ *FunctionType) *FunctionBuilder {
	if typ == nil {
		typ = &FunctionType{}
	}
	return &FunctionBuilder{typ: typ}
}

func (b *FunctionBuilder) WithParams(ps ...Type) *FunctionBuilder {
	b.typ.Params = append(b.typ.Params, ps...)
	return b
}

func (b *FunctionBuilder) WithReturns(rs ...Type) *FunctionBuilder {
	b.typ.Returns = append(b.typ.Returns, rs...)
	return b
}

func (b *FunctionBuilder) WithLocals(ls ...Type) *FunctionBuilder {
	b.locals = append(b.locals, ls...)
	return b
}

func (b *FunctionBuilder) Emit(instrs ...instr.Instruction) *FunctionBuilder {
	b.instrs = append(b.instrs, instrs...)
	return b
}

func (b *FunctionBuilder) Build() *Function {
	return &Function{
		Typ:    b.typ,
		Locals: b.locals,
		Code:   instr.Marshal(b.instrs),
	}
}

func NewFunction(typ *FunctionType, locals []Type, instrs []instr.Instruction) *Function {
	if typ == nil {
		typ = &FunctionType{}
	}
	return &Function{
		Typ:    typ,
		Locals: locals,
		Code:   instr.Marshal(instrs),
	}
}

func (f *Function) Kind() Kind {
	return KindRef
}

func (f *Function) Type() Type {
	return f.Typ
}

func (f *Function) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Type().String()))
	for _, t := range f.Locals {
		sb.WriteString("\n")
		sb.WriteString(t.String())
	}
	sb.WriteString(instr.Disassemble(f.Code))
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
	if t == other {
		return true
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
