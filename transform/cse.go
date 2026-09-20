package transform

import (
	"fmt"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// CSEPass removes dominating duplicate pure computations.
type CSEPass struct{}

var _ pass.Pass[*ssa.Function] = (*CSEPass)(nil)

// NewCSEPass returns the pass.
func NewCSEPass() *CSEPass {
	return &CSEPass{}
}

// Run applies the pass to one SSA function.
func (p *CSEPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	next, changed := deduplicate(function, cseKey)
	if !changed {
		return true, nil
	}
	*function = *next
	return false, nil
}

func cseKey(_ *ssa.Function, operation ssa.Operation) (string, bool) {
	switch operation.Op {
	case ssa.OpConst:
		return fmt.Sprintf("const %d", operation.Const), true
	case ssa.OpExec:
		if !operation.Code.IsPure() {
			return "", false
		}
		return fmt.Sprintf("exec %d %v", operation.Code, operation.Args), true
	default:
		return "", false
	}
}
