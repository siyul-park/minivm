package opt

import (
	"fmt"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// CSEPass collapses a pure computation into an equal one a dominating
// operation already performed: two operations of the same opcode over the
// same arguments, related by dominance, become one. It is the SSA
// counterpart of analysis.GVNAnalysis plus transform.GVNPass, and is
// dramatically smaller than either, because most of what that pair computes
// - within-block value numbering, an available-expression dataflow to carry
// numbers across block boundaries, and a conservative story for which
// mutable loads are even nameable across blocks - is not this pass's problem
// to solve. It is the IR's own: a value here is its definition, so identity
// is free, and dominance alone (dedup's dominator-tree-scoped table) decides
// what one definition may stand in for, with no dataflow fixpoint needed for
// merges a dominator already covers.
//
// Eligibility is exactly instr's own purity, as OpExec states it, plus
// OpConst, which the frontend never expresses as an OpExec: an OpLoad reads
// mutable interpreter storage and never collapses across even a straight-line
// run of code without knowing no store to that storage intervened, a
// dataflow problem this phase leaves for the eventual redundant-load pass;
// nothing that can deoptimize is ever eligible, since a pure OpExec or
// OpConst never carries state to begin with (ssa.Verify rejects one that
// does), so this pass never has to reason about a frame that might reference
// the value it is about to elide.
type CSEPass struct{}

var _ pass.Pass[*ssa.Function] = (*CSEPass)(nil)

func NewCSEPass() *CSEPass {
	return &CSEPass{}
}

func (p *CSEPass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	next, changed := dedup(fn, cseKey)
	if !changed {
		return pass.PreserveAll(), nil
	}
	*fn = *next
	return pass.PreserveNone(), nil
}

// cseKey identifies a CSE-eligible operation by what it computes: an
// OpConst by its boxed value, a pure OpExec by its opcode and its arguments
// (already translated to the rebuild's current value numbers). Anything else
// is not eligible.
func cseKey(_ *ssa.Function, op ssa.Operation) (string, bool) {
	switch op.Op {
	case ssa.OpConst:
		return fmt.Sprintf("const %d", op.Const), true
	case ssa.OpExec:
		if !op.Code.IsPure() {
			return "", false
		}
		return fmt.Sprintf("exec %d %v", op.Code, op.Args), true
	default:
		return "", false
	}
}
