package bench

import (
	"testing"

	"github.com/siyul-park/minivm/interp"
)

// BenchmarkJITIssue101 tracks a LightGBM-style batch path: many tiny tree-score
// functions called from one top-level accumulator over a stable feature row.
func BenchmarkJITIssue101(b *testing.B) {
	prog, want := batchTreeEvaluation(30)

	b.Run("interp", func(b *testing.B) {
		vm := interp.New(prog, interp.WithThreshold(-1))
		runMiniVMProgram(b, vm, want)
	})
}
