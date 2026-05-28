package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type FunctionBuilder struct {
	typ      *FunctionType
	locals   []Type
	captures []Type
	instrs   []instr.Instruction
}

type Function struct {
	Typ      *FunctionType
	Locals   []Type
	Captures []Type
	Code     []byte
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

func (b *FunctionBuilder) WithCaptures(cs ...Type) *FunctionBuilder {
	b.captures = append(b.captures, cs...)
	return b
}

func (b *FunctionBuilder) Emit(instrs ...instr.Instruction) *FunctionBuilder {
	b.instrs = append(b.instrs, instrs...)
	return b
}

func (b *FunctionBuilder) Build() *Function {
	return &Function{
		Typ:      b.typ,
		Locals:   b.locals,
		Captures: b.captures,
		Code:     instr.Marshal(b.instrs),
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
	for _, t := range f.Captures {
		sb.WriteString("capture ")
		sb.WriteString(t.String())
		sb.WriteString("\n")
	}
	for i, t := range f.Locals {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(t.String())
	}
	if len(f.Locals) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(instr.Format(f.Code))
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
	if len(returns) == 0 {
		return fmt.Sprintf("func(%s)", strings.Join(params, ", "))
	}
	if len(returns) == 1 {
		return fmt.Sprintf("func(%s) %s", strings.Join(params, ", "), returns[0])
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
