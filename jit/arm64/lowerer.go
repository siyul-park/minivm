// Package arm64 implements the JIT Lowerer for AArch64. Blank-import this
// package to register the backend with jit.
package arm64

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	"github.com/siyul-park/minivm/types"
)

// Lowerer is the AArch64 opcode emitter. Phase A opcodes will be filled in
// incrementally; until then every Lower call returns false so callers fall
// back to threaded dispatch.
type Lowerer struct{}

// Prologue is a no-op until whole-function Entry lowering lands.
func (Lowerer) Prologue(_ *jit.Context, _ *types.Function) {}

// Epilogue is a no-op until whole-function Entry lowering lands.
func (Lowerer) Epilogue(_ *jit.Context) {}

// Lower rejects every opcode. Real lowering follows in later milestones.
func (Lowerer) Lower(_ *jit.Context, _ instr.Opcode) bool { return false }
