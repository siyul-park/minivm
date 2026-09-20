package asm

import (
	"fmt"
	"unsafe"
)

// Context is the state one native execution and the Go code around it share.
// Native code holds its address in the pinned context register for as long
// as it runs, and leaves through it: every way out of native code lands in
// the trampoline with Trap saying which way, and Regs, Fregs, PC, and NSP
// holding enough of the machine state for Resume to continue exactly there.
//
// It is a heap object Go never moves, and native code writes only its scalar
// fields, so no write barrier is ever owed. The native stack it owns is
// noscan memory: a Go pointer stored there is invisible to the collector,
// which is why native code stores none.
type Context struct {
	stub  uintptr
	stack []uint64

	sp, fp, lr uintptr
	// PC is where the last exit left native code, and where Resume
	// continues.
	PC uintptr
	// NSP is the native stack pointer: where Enter starts, where the last
	// exit stopped, and, after a native activation returns, where that
	// activation was entered.
	NSP uintptr
	// Trap is how native code last left. The trampoline writes TrapReturn;
	// native code writes any other value before it exits.
	Trap Trap
	// Exit identifies the exit native code last left through. Native code
	// writes it before exiting; what it names is the code publisher's to
	// say.
	Exit uint64
	// Regs holds X0-X29 as they were at the last exit, indexed by register
	// number, and is what Resume loads them from. The slots of the scratch,
	// platform, Go-reserved, and context registers (X16, X17, X18, X28, X26)
	// are never written or read: Resume takes the context from its argument.
	Regs [32]uint64
	// Fregs holds D0-D31 as they were at the last exit and is what Resume
	// loads them from.
	Fregs [32]uint64
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
	OffsetPC    = unsafe.Offsetof(Context{}.PC)
	OffsetNSP   = unsafe.Offsetof(Context{}.NSP)
	OffsetTrap  = unsafe.Offsetof(Context{}.Trap)
	OffsetExit  = unsafe.Offsetof(Context{}.Exit)
	OffsetRegs  = unsafe.Offsetof(Context{}.Regs)
	OffsetFregs = unsafe.Offsetof(Context{}.Fregs)
)

// NewContext returns a context owning a native stack of size bytes, with NSP
// at the stack's 16-byte-aligned top.
func NewContext(size int) (*Context, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: stack size %d", ErrInvalidArgs, size)
	}
	stack := make([]uint64, (size+7)/8)
	top := uintptr(unsafe.Pointer(&stack[0])) + uintptr(len(stack))*8
	return &Context{stub: exitPC(), NSP: top &^ 15, stack: stack}, nil
}

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
