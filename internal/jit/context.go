// Package jit owns the native tier's runtime contract: what native code and
// the interpreter share while native code runs, and how native code reports
// back.
package jit

import (
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
)

// Record identifies one suspended native activation.
type Record struct {
	FB   uintptr
	SP   uintptr
	PC   uintptr
	Exit uint64
}

// Context is the state one native execution and the interpreter share. Its
// machine half is the asm.State native code is entered with; the rest is
// what native code writes before it exits, addressed off the same pinned
// register through the offsets below.
type Context struct {
	asm.State
	trap Trap
	exit uint64

	// Bases are written by the interpreter before every Enter and Resume.
	Stack   uintptr
	Globals uintptr
	RC      uintptr
	Natives uintptr
	Top     uintptr
	FB      uintptr

	Depth   uint64
	Limit   uint64
	Budget  int64
	Results [2]uint64
	Records [256]Record
}

// Trap is how native code last left.
type Trap uint64

const (
	// TrapReturn is a native activation returning: no native frame of that
	// activation survives.
	TrapReturn Trap = iota
	// TrapDeopt abandons the native activation; the interpreter rebuilds its
	// state and never resumes it.
	TrapDeopt
	// TrapBridge suspends the native activation for the interpreter to do
	// something on its behalf; Resume continues it.
	TrapBridge
)

// Offsets of the Context fields native code writes before an exit.
const (
	OffsetTrap    = unsafe.Offsetof(Context{}.trap)
	OffsetExit    = unsafe.Offsetof(Context{}.exit)
	OffsetStack   = unsafe.Offsetof(Context{}.Stack)
	OffsetGlobals = unsafe.Offsetof(Context{}.Globals)
	OffsetRC      = unsafe.Offsetof(Context{}.RC)
	OffsetNatives = unsafe.Offsetof(Context{}.Natives)
	OffsetTop     = unsafe.Offsetof(Context{}.Top)
	OffsetFB      = unsafe.Offsetof(Context{}.FB)
	OffsetDepth   = unsafe.Offsetof(Context{}.Depth)
	OffsetLimit   = unsafe.Offsetof(Context{}.Limit)
	OffsetBudget  = unsafe.Offsetof(Context{}.Budget)
	OffsetResults = unsafe.Offsetof(Context{}.Results)
	OffsetRecords = unsafe.Offsetof(Context{}.Records)

	RecordFB   = unsafe.Offsetof(Record{}.FB)
	RecordSP   = unsafe.Offsetof(Record{}.SP)
	RecordPC   = unsafe.Offsetof(Record{}.PC)
	RecordExit = unsafe.Offsetof(Record{}.Exit)
)

// The pinned register names the Context and, through asm.OffsetStub, its
// State, so State must lead.
var _ [0]struct{} = [unsafe.Offsetof(Context{}.State)]struct{}{}

// NewContext returns a context owning a native stack of size bytes.
func NewContext(size int) (*Context, error) {
	s, err := asm.NewState(size)
	if err != nil {
		return nil, err
	}
	return &Context{State: s}, nil
}

// Enter runs the native code at code until that activation returns or exits,
// and reports how it left. Anything but TrapReturn leaves it suspended for
// Resume.
func Enter(code uintptr, ctx *Context) Trap {
	if !asm.Enter(code, &ctx.State) {
		return TrapReturn
	}
	return ctx.trap
}

// Resume continues the suspended activation and reports how it left next.
func Resume(ctx *Context) Trap {
	if !asm.Resume(&ctx.State) {
		return TrapReturn
	}
	return ctx.trap
}

// Exit reports the exit identifier native code last wrote before leaving;
// what it names is the code publisher's to say.
func (c *Context) Exit() uint64 {
	return c.exit
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
