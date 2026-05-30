// Package amd64 is a stub Lowerer for x86_64. It rejects every opcode so
// the threaded interpreter handles all execution.
package amd64

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/amd64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	"github.com/siyul-park/minivm/types"
)

type Lowerer struct{}

var theArch = amd64.New()

func (Lowerer) Arch() asm.Arch                             { return theArch }
func (Lowerer) Prologue(_ *jit.Context, _ *types.Function) {}
func (Lowerer) Epilogue(_ *jit.Context)                    {}
func (Lowerer) Lower(_ *jit.Context, _ instr.Opcode) bool  { return false }
func (Lowerer) Exit(_ *jit.Context, _ int)                 {}
