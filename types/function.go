package types

import (
	"fmt"
	"slices"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

// FunctionBuilder assembles a Function: its signature, locals, captures, and code.
type FunctionBuilder struct {
	code *instr.Builder

	typ      *FunctionType
	locals   []Type
	captures []Type
}

// Function is compiled code with its signature, local and capture slots, and exception handlers.
type Function struct {
	Typ      *FunctionType
	Locals   []Type
	Captures []Type
	Code     []byte
	Handlers []instr.Handler
}

// FunctionType is a function signature.
type FunctionType struct {
	Params  []Type
	Returns []Type
}

var _ Value = (*Function)(nil)

var _ Type = (*FunctionType)(nil)

// NewFunctionBuilder returns a builder for typ; a nil typ becomes the empty signature.
func NewFunctionBuilder(typ *FunctionType) *FunctionBuilder {
	if typ == nil {
		typ = &FunctionType{}
	}
	return &FunctionBuilder{code: instr.NewBuilder(), typ: typ}
}

// NewFunction returns a function of type typ with locals and instrs; a nil typ becomes the empty signature.
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

// Params appends parameter types.
func (b *FunctionBuilder) Params(ps ...Type) *FunctionBuilder {
	b.typ.Params = append(b.typ.Params, ps...)
	return b
}

// Returns appends result types.
func (b *FunctionBuilder) Returns(rs ...Type) *FunctionBuilder {
	b.typ.Returns = append(b.typ.Returns, rs...)
	return b
}

// Locals appends local slot types.
func (b *FunctionBuilder) Locals(ls ...Type) *FunctionBuilder {
	b.locals = append(b.locals, ls...)
	return b
}

// Captures appends captured slot types.
func (b *FunctionBuilder) Captures(cs ...Type) *FunctionBuilder {
	b.captures = append(b.captures, cs...)
	return b
}

// Emit appends one instruction built from op and operands to the function body.
func (b *FunctionBuilder) Emit(op instr.Opcode, operands ...uint64) *FunctionBuilder {
	b.code.Emit(op, operands...)
	return b
}

// Append appends pre-built instructions to the function body.
func (b *FunctionBuilder) Append(instrs ...instr.Instruction) *FunctionBuilder {
	b.code.Append(instrs...)
	return b
}

// Label allocates an unbound branch target for the function body.
func (b *FunctionBuilder) Label() instr.Label {
	return b.code.Label()
}

// Bind fixes l to the next instruction emitted.
func (b *FunctionBuilder) Bind(l instr.Label) *FunctionBuilder {
	b.code.Bind(l)
	return b
}

// Br emits an unconditional branch to l.
func (b *FunctionBuilder) Br(l instr.Label) *FunctionBuilder {
	b.code.Br(l)
	return b
}

// BrIf emits a conditional branch to l.
func (b *FunctionBuilder) BrIf(l instr.Label) *FunctionBuilder {
	b.code.BrIf(l)
	return b
}

// BrTable emits a jump table to targets with def as the out-of-range case.
func (b *FunctionBuilder) BrTable(def instr.Label, targets ...instr.Label) *FunctionBuilder {
	b.code.BrTable(def, targets...)
	return b
}

// Try declares a protected region [start, end) landing on catch, with depth the
// operand-stack height at the region's entry. Declare inner regions before the
// outer ones that enclose them. Build resolves the labels into the function's
// exception table.
func (b *FunctionBuilder) Try(start, end, catch instr.Label, depth int) *FunctionBuilder {
	b.code.Try(start, end, catch, depth)
	return b
}

// MustBuild is like Build but panics on failure. Use it only with statically
// known-good bodies, such as in tests and fixtures.
func (b *FunctionBuilder) MustBuild() *Function {
	fn, err := b.Build()
	if err != nil {
		panic(err)
	}
	return fn
}

// Build resolves the body's branches and returns the function. It fails when a
// branch references an unbound label or overflows the offset operand.
func (b *FunctionBuilder) Build() (*Function, error) {
	instrs, err := b.code.Assemble()
	if err != nil {
		return nil, err
	}
	return &Function{
		Typ:      b.typ,
		Locals:   b.locals,
		Captures: b.captures,
		Code:     instr.Marshal(instrs),
		Handlers: b.code.Handlers(),
	}, nil
}

// Slots returns the kind of each stack slot addressable by LOCAL_*
// opcodes: first the function's params (in order), then its declared
// locals. The kinds are the slots' declared kinds (i1/i8 stay narrow); a
// consumer that computes through a slot reduces it with Kind.Repr at the point
// of use. Returns nil when the function has neither.
func (f *Function) Slots() []Kind {
	return Kinds(f.Declared())
}

// Declared returns the declared Type of each stack slot addressable by
// LOCAL_* opcodes, in the same params-then-locals order as Slots. Callers
// that need more than a slot's reduced Kind, such as proving a local is a
// concrete typed array, index this by the same slot number. Returns nil when
// the function has neither.
func (f *Function) Declared() []Type {
	if f.Typ == nil || len(f.Typ.Params) == 0 {
		return f.Locals
	}
	if len(f.Locals) == 0 {
		return f.Typ.Params
	}
	out := make([]Type, 0, len(f.Typ.Params)+len(f.Locals))
	out = append(out, f.Typ.Params...)
	out = append(out, f.Locals...)
	return out
}

// Clone returns a copy of f owning its Locals, Captures, Code, and Handlers.
// The signature is shared.
func (f *Function) Clone() *Function {
	return &Function{
		Typ:      f.Typ,
		Locals:   slices.Clone(f.Locals),
		Captures: slices.Clone(f.Captures),
		Code:     slices.Clone(f.Code),
		Handlers: slices.Clone(f.Handlers),
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
