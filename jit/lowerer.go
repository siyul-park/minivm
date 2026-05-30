package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Lowerer is the arch-specific opcode emitter. Arch returns the asm.Arch
// the compiler should use to encode and link emitted code. Prologue/
// Epilogue are explicit roles for whole-function compilation. Lower
// handles a single bytecode opcode; the driver advances IP only when
// Lower returns true. Exit emits the segment's terminator (write next IP
// to the agreed scratch slot and return from native).
type Lowerer interface {
	Arch() asm.Arch
	Prologue(c *Context, fn *types.Function)
	Epilogue(c *Context)
	Lower(c *Context, op instr.Opcode) bool
	Exit(c *Context, nextIP int)
}

// Context is the per-segment state the Lowerer reads and mutates while
// emitting native code. The driver sets up Context once per segment;
// architectures see it as a read-mostly bag of pointers plus a VM-stack
// shadow.
//
// A Lowerer must return false from Lower without mutating any field in
// Context when an opcode is unsupported.
type Context struct {
	Assembler *asm.Assembler

	// Code is the bytecode of the function being compiled.
	Code []byte

	// Start is the segment's first opcode IP.
	Start int

	// IP is the current decode position. Lowerers advance IP exactly by
	// the lowered opcode's width on success and leave it untouched on
	// reject.
	IP int

	// End is the IP one past the last opcode the segment is allowed to
	// lower. Terminator opcodes (BR, RETURN, …) may emit code that exits
	// before End.
	End int

	// Snap is the consumer-side state available to lowerers at compile
	// time.
	Snap Snapshot

	// Stack tracks values pushed onto the VM stack within this segment.
	// Top of the slice is top of stack.
	Stack []asm.VReg

	// Scratch holds the physical registers assigned to each segment-wide
	// scratch slot.
	Scratch []asm.PReg

	// Slots is the indirection table for direct-BL CALL lowering.
	Slots *Slots

	// Layout is a snapshot of the consumer's struct layout.
	Layout Layout
}

// Snapshot is the consumer-side state the JIT may inspect at compile time
// for opcodes whose lowering depends on runtime kinds (CONST_GET, GLOBAL_*,
// LOCAL_*). Each field is a read-only view into the consumer's tables.
type Snapshot struct {
	Constants []types.Boxed
	Globals   []types.Boxed
	Locals    []types.Kind
}
