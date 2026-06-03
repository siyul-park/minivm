package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Lowerer is the arch-specific opcode emitter. Arch returns the asm.Arch
// the compiler should use to encode and link emitted code. Prologue binds
// segment entry state; Lower handles one opcode; Exit emits the terminator,
// writes next IP through scratch, and maps stack values to ABI returns.
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
	// Whole is true while compiling a whole-function native entry.
	// Partial segments leave it false and must reject RETURN so the
	// threaded handler keeps frame teardown ownership.
	Whole bool

	Assembler *asm.Assembler

	// Slots is the indirection table for direct CALL lowering. May be nil.
	Slots *Slots

	// Layout is a snapshot of the consumer's struct layout. Empty until
	// the consumer calls Bind during init.
	Layout Layout

	// Code is the bytecode of the function being compiled.
	Code []byte

	// Start is the segment's first opcode IP.
	Start int

	// End is the IP one past the last opcode the segment is allowed to
	// lower. Terminator opcodes may emit code that exits before End.
	End int

	// Snap is the consumer-side state available to lowerers at compile
	// time.
	Snap Snapshot

	// IP is the current decode position. Lowerers advance IP exactly by
	// the lowered opcode's width on success and leave it untouched on
	// reject.
	IP int

	// Stop tells the compiler that the last successfully lowered opcode
	// ended the segment.
	Stop bool

	// Closed tells the compiler that the lowerer already emitted every
	// native exit path for the segment.
	Closed bool

	// Successor is a forced follow-up entry IP discovered by a terminal
	// opcode. -1 means no forced successor.
	Successor int

	// Stack tracks values currently visible on the VM stack within this
	// segment. Top of the slice is top of stack.
	Stack []asm.VReg

	// Inputs records VM stack values that existed before segment entry.
	// Order is bottom-to-top, matching the interpreter stack slice.
	Inputs []asm.VReg

	// Args records ABI argument registers populated by Exit from Inputs.
	Args []asm.PReg

	// Scratch holds the physical registers assigned to each segment-wide
	// scratch slot.
	Scratch []asm.PReg

	// Returns records ABI return registers populated by Exit. The compiler
	// copies it into the Code signature after lowering completes.
	Returns []asm.PReg

	// Target is the heap address of the callee when the immediately
	// preceding opcode was CONST_GET of a function reference. The arm64
	// lowerer sets this in constGet and reads it in call. Lower resets
	// it to -1 before each dispatch so stale values never cross opcodes.
	Target int
}

// Snapshot is the consumer-side state the JIT may inspect at compile time
// for opcodes whose lowering depends on runtime kinds (CONST_GET, GLOBAL_*,
// LOCAL_*). Each field is a read-only view into the consumer's tables.
type Snapshot struct {
	Hot []int

	Functions map[int]*types.Function // heap addr -> *Function for direct-BL CALL lowering
	Constants []types.Boxed
	Globals   []types.Boxed
	Locals    []types.Kind
}
