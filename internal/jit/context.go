// Package jit owns the native tier's runtime contract: what native code and
// the interpreter share while native code runs, and how native code reports
// back.
package jit

import (
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
)

// Record identifies one native activation.
type Record struct {
	FB uintptr
	// SP is the spill base used by values live across a native call.
	SP uintptr
	// PC is the caller return address.
	PC   uintptr
	Exit uint64
}

// Context is the runtime state shared by native code and the interpreter.
// asm.State leads the layout; the remaining fields are native exit state.
type Context struct {
	asm.State
	trap Trap
	exit uint64

	// Bases are written by the interpreter before every Enter and Resume.
	Stack   uintptr
	Heap    uintptr
	Globals uintptr
	RC      uintptr
	Natives uintptr
	// Entries is the base of a per-address int64 table the prologue
	// increments on every entry, interpreted-CALL or native-to-native, so
	// tiering sees a callee reached only from native code too.
	Entries uintptr
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
	OffsetHeap    = unsafe.Offsetof(Context{}.Heap)
	OffsetGlobals = unsafe.Offsetof(Context{}.Globals)
	OffsetRC      = unsafe.Offsetof(Context{}.RC)
	OffsetNatives = unsafe.Offsetof(Context{}.Natives)
	OffsetEntries = unsafe.Offsetof(Context{}.Entries)
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

// Read returns the raw value named by v. Outer activations use their saved
// spill base because registers are clobbered across calls.
func (c *Context) Read(record int, v Value) uint64 {
	if record == int(c.Depth)-1 {
		if v.Loc.Spilled {
			return c.Slot(v.Loc.Slot)
		}
		return c.Reg(v.Loc.Reg)
	}
	if !v.Loc.Spilled {
		panic("jit: an outer activation's value must be spilled")
	}
	return c.Word(c.Records[record].SP + uintptr(8*v.Loc.Slot))
}

// String returns the trap name.
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
