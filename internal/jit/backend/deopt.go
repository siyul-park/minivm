package backend

import (
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
)

// Deopt is one deoptimization resolved out of the frame chain an OpState
// carries: where the interpreter picks execution up, which VM stack slot each
// live operand must be boxed into, and the frame records the journal is handed
// so Go can rebuild the call chain native inlining hid. It states what native
// code writes, never how - the stores are the machine's.
//
// Ownership is deliberately absent. ssa.Frame lists the stack a deopt resumes
// with but not which of those values carry a reference count, so a machine
// still takes the cold-path retains itself. Stating it here is a change to the
// IR, not to this type: Flush is where the answer lands.
type Deopt struct {
	// ID is the descriptor journal.CellExitID reports this exit under, or -1
	// for an exit with none. Native code writes ID+1, so -1 writes the zero
	// that means "no descriptor" - which is what a bridge and a yield take,
	// being productive continuation rather than a give-up.
	ID int
	// Resume is the IP journal.CellNextIP carries: where threaded dispatch
	// picks the innermost frame up. It is that frame's own IP, so a guard
	// resumes at the opcode it refused rather than past it.
	Resume int
	// SP is the interpreter stack pointer journal.CellSP carries, as a delta
	// from the entry frame's base: the top of the innermost frame's operands.
	SP int
	// Stack is every live operand of every frame with the slot it belongs in,
	// outermost frame first and each frame's stack bottom first, which is
	// ascending slot order.
	Stack []Flush
	// Frames are the journal frame records, innermost first, which is the
	// order journal.At indexes them in and the interpreter reads them back in.
	Frames []Record
}

// Flush is one live operand and the VM stack slot, as a delta from the entry
// frame's base, native code must box it into before returning.
type Flush struct {
	Value ssa.Value
	Slot  int
}

// Record is one journal frame record: the values journal.RecordAddr,
// RecordBP, RecordIP, and RecordReturns are written from. BP is a delta from
// the entry frame's base, like every other coordinate here.
type Record struct {
	Addr    int
	BP      int
	IP      int
	Returns int
}

// Exit resolves the interpreter state v carries into the journal words a
// deoptimization writes, and registers the descriptor the Go wrapper counts
// this exit under. A reason of prof.ExitNone registers none, leaving ID at -1.
// v must be an OpState result; anything else yields the zero Deopt, which
// names no frame and cannot be emitted.
func (c *Compiler) Exit(v ssa.Value, reason prof.ExitReason, opcode int) Deopt {
	op, ok := c.Def(v)
	if !ok || op.Op != ssa.OpState || len(op.Frames) == 0 {
		return Deopt{}
	}

	d := Deopt{ID: -1, Resume: op.Frames[len(op.Frames)-1].IP, Frames: make([]Record, 0, len(op.Frames))}
	for _, frame := range op.Frames {
		floor := frame.Base + c.slots[frame.Addr]
		for i, operand := range frame.Stack {
			d.Stack = append(d.Stack, Flush{Value: operand, Slot: floor + i})
		}
		d.SP = floor + len(frame.Stack)
	}
	for i := len(op.Frames) - 1; i >= 0; i-- {
		frame := op.Frames[i]
		d.Frames = append(d.Frames, Record{Addr: frame.Addr, BP: frame.Base, IP: frame.IP, Returns: frame.Returns})
	}
	if reason != prof.ExitNone {
		d.ID = len(c.exits)
		c.exits = append(c.exits, jit.Exit{Reason: reason, Opcode: opcode})
	}
	return d
}
