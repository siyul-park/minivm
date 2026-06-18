package interp

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Extension is a user-registered family of custom instructions dispatched
// through the EXT opcode. Types reports the ops it provides (each an instr.Type
// of mnemonic + operand widths); the slice index is the op's local id, carried
// in the low byte of the EXT operand. Compile builds the threaded handler for an
// occurrence and Lower optionally emits native ARM64; both receive the current
// instruction and self-resolve the op via Types()[inst.Operand(0)&0xFF]. If
// Lower returns false, Emitter stack changes are rolled back and emitted
// instructions are dropped.
type Extension interface {
	Types() []instr.Type
	Compile(inst instr.Instruction) func(i *Interpreter) error
	Lower(inst instr.Instruction, e *Emitter) bool
}

// Registry is an ordered collection of Extensions. Register assigns each
// extension a sequential id (its slot) and the program addresses it by that id
// in the high byte of an EXT operand, so ids cannot collide. Pass a Registry to
// the interpreter with WithRegistry.
type Registry struct {
	exts []Extension
}

// Emitter is the JIT lowering façade handed to Extension.Lower. It exposes only
// the operand-stack and emission primitives a custom op needs, wrapping the
// internal trace-lowering state. Popped values are raw-unboxed (an i32 keeps its
// value in the low 32 bits, an f64 its IEEE bits); the kind tells the extension
// how to treat each register.
type Emitter struct {
	ctx   *lowering
	insts []asm.Instruction
}

func NewRegistry() *Registry {
	return &Registry{}
}

// Register appends ext and returns its auto-assigned extension id. It panics if
// more than 256 extensions are registered, since the id must fit one byte.
func (r *Registry) Register(ext Extension) uint8 {
	if len(r.exts) >= 256 {
		panic("interp: registry holds more than 256 extensions")
	}
	id := uint8(len(r.exts))
	r.exts = append(r.exts, ext)
	return id
}

// Kinds reports the runtime kinds of the innermost frame's operands, top last.
func (e *Emitter) Kinds() []types.Kind {
	n := e.ctx.count()
	base := len(e.ctx.values) - n
	kinds := make([]types.Kind, n)
	for k := 0; k < n; k++ {
		kinds[k] = e.ctx.values[base+k].kind
	}
	return kinds
}

// Pop removes the top operand and returns its register, kind, and whether it is
// raw-unboxed.
func (e *Emitter) Pop() (asm.VReg, types.Kind, bool) {
	v := e.ctx.pop()
	return v.reg, v.kind, v.raw
}

// Push records a freshly produced raw operand of kind k held in reg.
func (e *Emitter) Push(reg asm.VReg, k types.Kind) {
	e.ctx.push(value{reg: reg, kind: k, raw: true})
}

// Reg allocates a fresh virtual register.
func (e *Emitter) Reg(t asm.RegType, w asm.RegWidth) asm.VReg {
	return e.ctx.assembler.Reg(t, w)
}

// Emit buffers native instructions until Lower returns true.
func (e *Emitter) Emit(insts ...asm.Instruction) {
	e.insts = append(e.insts, insts...)
}
