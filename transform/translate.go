package transform

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// Module is the read-only, module-wide evidence a bytecode translation
// resolves value kinds, container shapes, and call targets against. It is
// everything a translation reads outside the function being translated.
type Module struct {
	// Constants is the module's constant pool, as the values CONST_GET
	// pushes.
	Constants []types.Boxed
	// Globals is the declared kind of each global slot.
	Globals []types.Kind
	Objects Objects
	// Decl is the program's declared-type table, indexed by the type operand
	// of STRUCT_NEW and REF_CAST.
	Decl []types.Type
}

// Objects resolves the reference a constant carries into the facts about the
// cell it names. The identity a reference carries is the constant's own pool
// slot, because a translation only ever hands it straight back to Objects.
type Objects map[int]Object

// Object is one constant cell's facts a translation resolves a reference
// against. Every field is zero for a cell that carries no such fact.
type Object struct {
	// Fn is the function published at this address.
	Fn *types.Function
	// Typ is the type a struct cell carries.
	Typ *types.StructType
}

// Translate returns the SSA for the whole of fn, published at addr. Address
// zero is module code, which ends by advancing past its last instruction
// rather than by returning. It returns (nil, nil) when fn holds an operation
// no translation from bytecode alone can resolve, or when fn suspends: a
// suspension ends execution at its own opcode while the threaded
// continuation runs past it, so a translation covering only the prefix up to
// it is not the whole function this returns.
func Translate(m Module, addr int, fn *types.Function) (*ssa.Function, error) {
	if fn == nil {
		return nil, nil
	}
	f, err := translate(m, addr, fn)
	if err != nil || f == nil {
		return nil, err
	}
	for id := 0; id < f.Len(); id++ {
		if f.Block(id).Term.Op == ssa.OpSuspend {
			return nil, nil
		}
	}
	return f, nil
}

// translate lays out fn's spans, resolves the operand facts every one of them
// is entered with, and emits the blocks reachable from the entry. A span
// nothing reaches is dead code and is simply left out, which is what a
// caller optimizing a whole function wants.
func translate(m Module, addr int, fn *types.Function) (*ssa.Function, error) {
	if len(fn.Code) == 0 {
		return nil, nil
	}
	f := facts{
		constants: m.Constants,
		globals:   m.Globals,
		objects:   m.Objects,
		decl:      m.Decl,
		declared:  !calls(fn.Code),
	}
	blocks, err := analysis.Blocks(fn)
	if err != nil {
		return nil, err
	}
	spans := split(fn.Code, blocks)
	if spans[0].start != 0 {
		return nil, nil
	}
	entry := frame{fn: fn, addr: addr, slots: fn.Declared()}
	states, ok := f.resolve(entry, spans)
	if !ok {
		return nil, nil
	}
	return f.build(entry, spans, states), nil
}

// calls reports whether code enters another function.
func calls(code []byte) bool {
	for ip := 0; ip < len(code); {
		inst := instr.Instruction(code[ip:])
		if inst.Opcode().Writes(instr.Frame) {
			return true
		}
		ip += inst.Width()
	}
	return false
}

// function returns the function published at addr, or nil when addr names no
// function.
func (o Objects) function(addr int) *types.Function {
	return o[addr].Fn
}
