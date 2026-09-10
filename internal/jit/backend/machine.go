// Package backend lowers minivm's SSA into native code: the
// architecture-neutral half of a JIT backend, and the Machine seam an
// architecture implements below it. It chooses the order blocks are laid out
// in, binds one virtual register to every SSA value, sequences the register
// copies a block-parameter edge needs, and resolves the frame chain an
// OpState carries into the journal words a deoptimization writes. A Machine
// decides only what instructions say those things on its target: it never
// reorders blocks, rewrites the IR, or invents interpreter state.
//
// Root is the whole compile behind one call - the frontend the anchor implies,
// then that lowering - so a jit.Compiler reaches the SSA pipeline without
// importing the frontend that would import it back.
package backend

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
)

// Machine is one target's native code. It answers what it can lower before
// anything is planned, and opens one Lowering per compile, so one Machine is
// shared by concurrent compiles and holds none of their state.
type Machine interface {
	// Lowers reports whether the machine emits native code for code at all.
	// It is the capability question a frontend asks while planning: an opcode
	// the machine declines is bridged to the interpreter rather than lowered
	// (see docs/jit-internals.md, Bridge). Compile asks it too, over the
	// blocks it lays out, and refuses a function holding an operation the
	// machine declined, so a Lowering never sees one.
	Lowers(code instr.Opcode) bool
	// Traps reports whether lowering code ends the block by handing control
	// back to the interpreter - the unconditional terminal exit a machine
	// emits for an opcode it runs rather than computes. It is the other
	// capability question, and the opposite answer to Lowers: a trapped
	// opcode is lowered, as an exit, so Traps is asked only of one Lowers
	// admits. Compile lowers it last in its block, calls no Term after it,
	// and lays out no block that only its abandoned successors reach.
	Traps(code instr.Opcode) bool
	// Open begins one compile and returns the Lowering that emits it. c
	// stays valid until that compile ends.
	Open(c *Compiler) Lowering
}

// Lowering emits one function's native code. The Compiler drives it: prologue,
// then every block in layout order, then whatever the machine deferred.
// Reporting false abandons the compile at that point, which leaves threaded
// execution installed; the assembler is discarded whole, so a machine that
// gives up mid-block need not undo what it emitted.
type Lowering interface {
	// Enter emits the callable's prologue, before the first block's label is
	// bound. Every block already has its label, so a prologue that dispatches
	// on an external re-entry point may branch to one.
	Enter() bool
	// Lower emits ops[0] and reports how many of ops it consumed. Consuming
	// more than one is how a machine fuses an adjacent run into a single
	// lowering; the Compiler calls Lower again from the first operation left.
	// A count that is not positive, or that runs past ops, abandons the
	// compile. ops ends at the block's trapping operation, so a fusion can
	// never reach past the point control leaves.
	Lower(block int, ops []ssa.Operation) (int, bool)
	// Term ends block with t, unless the block trapped, which ends it
	// already. The Compiler binds no label after it: a machine that wants to
	// fall through asks Compiler.Next which block follows this one in the
	// layout.
	Term(block int, t ssa.Terminator) bool
	// Leave emits what lowering deferred - the cold stub behind every guard,
	// any continuation it scheduled - after the last block.
	Leave() bool
}
