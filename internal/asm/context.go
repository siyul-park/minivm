package asm

import (
	"fmt"
	"unsafe"
)

// Context owns the state shared by Go and native activations. Native code
// accesses its layout through the offsets below; Go code uses the state methods.
type Context struct {
	stub  uintptr
	stack []uint64

	sp, fp, lr uintptr
	pc         uintptr
	nsp        uintptr
	trap       Trap
	exit       uint64
	regs       [32]uint64
	fregs      [32]uint64
}

// Trap is how native code left the last time control returned to Go.
type Trap uint64

const (
	// TrapReturn is a native activation returning to the trampoline: no
	// native frame of that activation survives.
	TrapReturn Trap = iota
	// TrapDeopt abandons the native activation; Go rebuilds its interpreter
	// state and never resumes it.
	TrapDeopt
	// TrapBridge suspends the native activation for Go to do something on
	// its behalf; Resume continues it.
	TrapBridge
)

// Offsets of the Context fields native code addresses off the context
// register. The trampoline reads the same offsets through go_asm.h, so this
// is the one layout both sides share.
const (
	OffsetStub  = unsafe.Offsetof(Context{}.stub)
	OffsetPC    = unsafe.Offsetof(Context{}.pc)
	OffsetNSP   = unsafe.Offsetof(Context{}.nsp)
	OffsetTrap  = unsafe.Offsetof(Context{}.trap)
	OffsetExit  = unsafe.Offsetof(Context{}.exit)
	OffsetRegs  = unsafe.Offsetof(Context{}.regs)
	OffsetFregs = unsafe.Offsetof(Context{}.fregs)
)

// NewContext returns a context owning a native stack of size bytes, with NSP
// at the stack's 16-byte-aligned top.
func NewContext(size int) (*Context, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: stack size %d", ErrInvalidArgs, size)
	}
	stack := make([]uint64, size/8+2)
	top := uintptr(unsafe.Pointer(&stack[0])) + uintptr(len(stack))*8
	return &Context{stub: exitPC(), nsp: top &^ 15, stack: stack}, nil
}

// Exit reports the exit identifier native code last wrote before leaving.
func (c *Context) Exit() uint64 {
	return c.exit
}

// Reg reports the saved value of r. r must be saved by the native exit protocol.
func (c *Context) Reg(r PReg) uint64 {
	return *c.slot(r)
}

// SetReg stores v in the saved register file for r. r must be saved by the native exit protocol.
func (c *Context) SetReg(r PReg, v uint64) {
	*c.slot(r) = v
}

// String reports how native code last left.
func (t Trap) String() string {
	switch t {
	case TrapReturn:
		return "return"
	case TrapDeopt:
		return "deopt"
	case TrapBridge:
		return "bridge"
	default:
		return "invalid"
	}
}

func (c *Context) slot(r PReg) *uint64 {
	if r.id >= uint8(len(c.regs)) {
		panic("asm: invalid register")
	}
	switch r.typ {
	case RegTypeInt:
		return &c.regs[r.id]
	case RegTypeFloat:
		return &c.fregs[r.id]
	default:
		panic("asm: invalid register type")
	}
}
