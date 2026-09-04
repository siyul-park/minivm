package opt

import (
	"fmt"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// GuardPass collapses a guard into an equal one a dominating guard already
// established: two guards of the same kind, over the same operand and the
// same admitted fact, related by dominance, become one, exactly as CSEPass
// does for a pure computation. It has no bytecode counterpart - a guard is a
// fact this IR invents once it starts speculating, never a bytecode opcode -
// and is the pass docs/jit-internals.md's "Heap Reads and Mutations" section
// asks for: today a repeated heap access re-emits and re-guards
// independently, even when an earlier access on every path already proved
// the same fact. Running CSEPass first is what makes a later, textually
// distinct guard's operand equal a numeric Value already established: once
// two heap reads of the same slot collapse into one value, the guards built
// on each read are guards over that same value too, and this pass sees them.
//
// A dominating guard licenses skipping the later one only because the
// dominating guard would already have deoptimized on every path that reaches
// the later site if the fact did not hold - the later guard could never
// itself observe anything the earlier one did not. Removing it costs nothing
// a JIT function depends on: OpGuardBounds carries no result to preserve, and
// OpGuardKind, OpGuardShape, and OpGuardValue each refine their operand into
// their own result, so an eliminated guard's result is aliased to the
// dominating guard's own result, which already is that same refinement.
type GuardPass struct{}

var _ pass.Pass[*ssa.Function] = (*GuardPass)(nil)

func NewGuardPass() *GuardPass {
	return &GuardPass{}
}

func (p *GuardPass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	next, changed := dedup(fn, guardKey)
	if !changed {
		return pass.PreserveAll(), nil
	}
	*fn = *next
	return pass.PreserveNone(), nil
}

// guardKey identifies a guard by what it admits: OpGuardKind by its operand
// and the kind it narrows to (its result type, since ssa.Verify does not
// require the result to share the operand's type the way it does for the
// other three); OpGuardShape by its operand and Shape; OpGuardBounds by its
// index and length operands; OpGuardValue by its operand and the specialized
// value it compares against. Every operand named here is already translated
// to the rebuild's current value numbers.
func guardKey(fn *ssa.Function, op ssa.Operation) (string, bool) {
	switch op.Op {
	case ssa.OpGuardKind:
		return fmt.Sprintf("kind %d %s", op.Args[0], fn.Type(op.Results[0])), true
	case ssa.OpGuardShape:
		return fmt.Sprintf("shape %d %+v", op.Args[0], op.Shape), true
	case ssa.OpGuardBounds:
		return fmt.Sprintf("bounds %d %d", op.Args[0], op.Args[1]), true
	case ssa.OpGuardValue:
		return fmt.Sprintf("value %d %d", op.Args[0], op.Args[1]), true
	default:
		return "", false
	}
}
