package transform

import (
	"fmt"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// GuardPass removes dominated duplicate guards.
type GuardPass struct{}

var _ pass.Pass[*ssa.Function] = (*GuardPass)(nil)

// NewGuardPass returns the pass.
func NewGuardPass() *GuardPass {
	return &GuardPass{}
}

// Run applies the pass to one SSA function.
func (p *GuardPass) Run(_ *pass.Manager, function *ssa.Function) (pass.Preserved, error) {
	next, changed := deduplicate(function, guardKey)
	if !changed {
		return pass.PreserveAll(), nil
	}
	*function = *next
	return pass.PreserveNone(), nil
}

func guardKey(function *ssa.Function, operation ssa.Operation) (string, bool) {
	switch operation.Op {
	case ssa.OpGuardKind:
		return fmt.Sprintf("kind %d %s", operation.Args[0], function.Type(operation.Results[0])), true
	case ssa.OpGuardShape:
		return fmt.Sprintf("shape %d %+v", operation.Args[0], operation.Shape), true
	case ssa.OpGuardBounds:
		return fmt.Sprintf("bounds %d %d", operation.Args[0], operation.Args[1]), true
	case ssa.OpGuardValue:
		return fmt.Sprintf("value %d %d", operation.Args[0], operation.Args[1]), true
	default:
		return "", false
	}
}
