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

func cseKey(function *ssa.Function, operation ssa.Operation) (string, bool) {
	switch operation.Op {
	case ssa.OpConst:
		return fmt.Sprintf("const %s %d", function.Type(operation.Results[0]), operation.Const), true
	default:
		if operation.Op == ssa.OpExec || operation.Op.Data() {
			return pure(function, operation)
		}
		return "", false
	}
}

// pure keys a pure computation by what decides its results: its operation,
// condition, operands, and the types it yields, since a conversion differs from
// another by its result type alone.
func pure(function *ssa.Function, operation ssa.Operation) (string, bool) {
	if !operation.Pure() {
		return "", false
	}
	args := operation.Args
	if commutative(operation) && len(args) == 2 && args[0] > args[1] {
		args = []ssa.Value{args[1], args[0]}
	}
	results := make([]ssa.Type, len(operation.Results))
	for i, v := range operation.Results {
		results[i] = function.Type(v)
	}
	return fmt.Sprintf("%s %d %s %v %v", operation.Op, operation.Code, operation.Cond, args, results), true
}
