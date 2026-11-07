package transform

import (
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// O1 folds constants and removes dead code. It is the JIT's Baseline tier.
func O1() []pass.Pass[*ssa.Function] {
	return []pass.Pass[*ssa.Function]{NewFoldPass(), NewDCEPass()}
}

// O2 adds common subexpression and redundant guard elimination to O1.
func O2() []pass.Pass[*ssa.Function] {
	return []pass.Pass[*ssa.Function]{NewFoldPass(), NewCSEPass(), NewGuardPass(), NewDCEPass()}
}

// O3 adds slot forwarding, promotion and hoisting to O2. late runs before the
// last dead-code removal, for a caller's own passes. It is the JIT's Optimized
// tier, whose late pass reads profiles.
func O3(late ...pass.Pass[*ssa.Function]) []pass.Pass[*ssa.Function] {
	passes := []pass.Pass[*ssa.Function]{
		NewFoldPass(), NewForwardPass(), NewCSEPass(), NewGuardPass(),
		NewDCEPass(), NewPromotePass(), NewHoistPass(),
	}
	passes = append(passes, late...)
	return append(passes, NewDCEPass())
}
