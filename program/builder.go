package program

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Builder assembles a whole program: a main code stream with symbolic branch
// targets plus interned constant and type pools. Emit and branch like
// instr.Builder, reference constants and types by value (ConstGet/Type), then
// Build to resolve branches and collect the pools. Builders are mutable;
// discard after Build.
type Builder struct {
	code      *instr.Builder
	constants []types.Value
	typs      []types.Type

	constIndex map[string]int
	typeIndex  map[string]int
}

func NewBuilder() *Builder {
	return &Builder{
		code:       instr.NewBuilder(),
		constIndex: map[string]int{},
		typeIndex:  map[string]int{},
	}
}

// Emit appends one instruction built from op and operands.
func (b *Builder) Emit(op instr.Opcode, operands ...uint64) *Builder {
	b.code.Emit(op, operands...)
	return b
}

// Label allocates an unbound branch target.
func (b *Builder) Label() instr.Label {
	return b.code.Label()
}

// Bind fixes l to the next instruction emitted.
func (b *Builder) Bind(l instr.Label) *Builder {
	b.code.Bind(l)
	return b
}

// Br emits an unconditional branch to l.
func (b *Builder) Br(l instr.Label) *Builder {
	b.code.Br(l)
	return b
}

// BrIf emits a conditional branch to l.
func (b *Builder) BrIf(l instr.Label) *Builder {
	b.code.BrIf(l)
	return b
}

// BrTable emits a jump table to targets with def as the out-of-range case.
func (b *Builder) BrTable(def instr.Label, targets ...instr.Label) *Builder {
	b.code.BrTable(def, targets...)
	return b
}

// ConstGet interns v and emits CONST_GET for its index.
func (b *Builder) ConstGet(v types.Value) *Builder {
	return b.Emit(instr.CONST_GET, uint64(b.Const(v)))
}

// Const interns v into the constant pool and returns its index, reusing an
// existing slot when an equal value is already present.
func (b *Builder) Const(v types.Value) int {
	key := v.String()
	if idx, ok := b.constIndex[key]; ok {
		return idx
	}
	idx := len(b.constants)
	b.constants = append(b.constants, v)
	b.constIndex[key] = idx
	return idx
}

// Type interns t into the type pool and returns its index, reusing an existing
// slot when an equal type is already present.
func (b *Builder) Type(t types.Type) int {
	key := t.String()
	if idx, ok := b.typeIndex[key]; ok {
		return idx
	}
	idx := len(b.typs)
	b.typs = append(b.typs, t)
	b.typeIndex[key] = idx
	return idx
}

// Build resolves every branch and returns the assembled program with its
// constant and type pools.
func (b *Builder) Build() (*Program, error) {
	instrs, err := b.code.Assemble()
	if err != nil {
		return nil, err
	}

	var opts []func(*Program)
	if len(b.constants) > 0 {
		opts = append(opts, WithConstants(b.constants...))
	}
	if len(b.typs) > 0 {
		opts = append(opts, WithTypes(b.typs...))
	}
	return New(instrs, opts...), nil
}
