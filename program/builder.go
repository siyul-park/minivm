package program

import (
	"reflect"

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
	locals    []types.Type
	globals   []types.Type
}

func NewBuilder() *Builder {
	return &Builder{code: instr.NewBuilder()}
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

// Try declares a protected region [start, end) landing on catch, with depth the
// operand-stack height at the region's entry. Declare inner regions before the
// outer ones that enclose them. Build resolves the labels into the program's
// top-level exception table.
func (b *Builder) Try(start, end, catch instr.Label, depth int) *Builder {
	b.code.Try(start, end, catch, depth)
	return b
}

// ConstGet interns v and emits CONST_GET for its index.
func (b *Builder) ConstGet(v types.Value) *Builder {
	return b.Emit(instr.CONST_GET, uint64(b.Const(v)))
}

// Emit appends one instruction built from op and operands.
func (b *Builder) Emit(op instr.Opcode, operands ...uint64) *Builder {
	b.code.Emit(op, operands...)
	return b
}

// Const interns v into the constant pool and returns its index. Comparable
// values reuse equal slots; pointer values therefore use identity. Values that
// cannot be compared are appended because their String form is diagnostic, not
// an equality contract.
func (b *Builder) Const(v types.Value) int {
	typ := reflect.TypeOf(v)
	if typ == nil {
		return -1
	}
	comparable := typ.Comparable()
	if comparable {
		for idx, existing := range b.constants {
			typ := reflect.TypeOf(existing)
			if typ == nil || !typ.Comparable() {
				continue
			}
			if existing == v {
				return idx
			}
		}
	}
	idx := len(b.constants)
	b.constants = append(b.constants, v)
	return idx
}

// Type interns t into the type pool and returns its index, reusing an existing
// slot when an equal type is already present.
func (b *Builder) Type(t types.Type) int {
	if t == nil {
		return -1
	}
	for idx, existing := range b.typs {
		if existing.Equals(t) {
			return idx
		}
	}
	idx := len(b.typs)
	b.typs = append(b.typs, t)
	return idx
}

// Locals declares the entry frame's local scratch slots in order; their
// positions are the indices used by LOCAL_* at the top level.
func (b *Builder) Locals(ts ...types.Type) *Builder {
	b.locals = append(b.locals, ts...)
	return b
}

// Globals declares the module's global slots and their types; their positions
// are the indices used by GLOBAL_* at the top level, forming the module's
// fixed global table. GLOBAL_* past the declared count traps. Declaring
// globals gives each interpreter a pre-sized, kind-stable global table so
// GLOBAL_GET/SET emit native traces. Seed per-run input into a declared slot
// with Interpreter.SetGlobal before Run.
func (b *Builder) Globals(ts ...types.Type) *Builder {
	b.globals = append(b.globals, ts...)
	return b
}

// Build resolves every branch and returns the assembled program with its
// constant and type pools.
func (b *Builder) Build() (*Program, error) {
	instrs, err := b.code.Assemble()
	if err != nil {
		return nil, err
	}
	return &Program{
		Code:      instr.Marshal(instrs),
		Locals:    b.locals,
		Globals:   b.globals,
		Constants: b.constants,
		Types:     b.typs,
		Handlers:  b.code.Handlers(),
	}, nil
}
