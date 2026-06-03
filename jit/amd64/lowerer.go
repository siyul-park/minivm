// Package amd64 carries a skeletal Lowerer for x86_64. The skeleton
// rejects every opcode and is not registered with jit; see register.go
// for why. The type remains so an actual codegen pass can drop in later
// without restructuring callers.
package amd64

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/amd64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	"github.com/siyul-park/minivm/types"
)

// Lowerer is the stub x86_64 emitter.
type Lowerer struct{}

var (
	_       jit.Lowerer = Lowerer{}
	theArch             = amd64.New()
)

func (Lowerer) Arch() asm.Arch                             { return theArch }
func (Lowerer) Prologue(_ *jit.Context, _ *types.Function) {}
func (Lowerer) Epilogue(_ *jit.Context)                    {}
func (Lowerer) Lower(_ *jit.Context, _ instr.Opcode) bool  { return false }
func (Lowerer) Exit(_ *jit.Context, _ int)                 {}
