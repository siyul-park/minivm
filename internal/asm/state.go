package asm

import (
	"fmt"
	"unsafe"
)

// State is the native machine state shared with Go. Native code reaches it
// through the pinned register and exits through OffsetStub. It must lead any
// embedding runtime context. Its native stack is noscan; native code stores no
// Go pointers there. It is the same for every architecture: each one's
// trampoline keeps what it needs of Go's stack in link and saves its register
// banks in a prefix of regs and fregs.
type State struct {
	stub  uintptr
	stack []uint64

	link     [3]uintptr
	pc       uintptr
	nsp, top unsafe.Pointer
	// base is the native stack pointer the latest Enter started from: below
	// any activation still suspended when it ran.
	base   unsafe.Pointer
	exited uint64
	regs   [32]uint64
	fregs  [32]uint64
}

// Field offsets for native code and each architecture's trampoline.
const (
	OffsetStub   = unsafe.Offsetof(State{}.stub)
	OffsetLink   = unsafe.Offsetof(State{}.link)
	OffsetPC     = unsafe.Offsetof(State{}.pc)
	OffsetNSP    = unsafe.Offsetof(State{}.nsp)
	OffsetBase   = unsafe.Offsetof(State{}.base)
	OffsetExited = unsafe.Offsetof(State{}.exited)
	OffsetRegs   = unsafe.Offsetof(State{}.regs)
	OffsetFRegs  = unsafe.Offsetof(State{}.fregs)
)

// NewState returns a state exiting through arch's stub, with a 16-byte-aligned
// native stack top.
func NewState(arch Arch, size int) (State, error) {
	if size <= 0 {
		return State{}, fmt.Errorf("%w: stack size %d", ErrInvalidArgs, size)
	}
	stack := make([]uint64, size/8+4)
	base := unsafe.Pointer(&stack[0])
	index := len(stack) - 1
	if (uintptr(base)+uintptr(index*8))&15 != 0 {
		index--
	}
	top := unsafe.Add(base, index*8)
	return State{stub: arch.Stub(), nsp: top, top: top, base: top, stack: stack}, nil
}

// PC reports where the last exit left: the exit stub's return address, which
// lies in the exiting activation's code.
func (s *State) PC() uintptr {
	return s.pc
}

// Exited reports whether native code left the state suspended.
func (s *State) Exited() bool {
	return s.exited != 0
}

// Abandon discards the activations the latest Enter started, so the next
// Enter runs from where that one did: the stack's top, or below an
// activation suspended before it, which stays intact.
func (s *State) Abandon() {
	s.nsp = s.base
	s.exited = 0
}

// Reg reports the saved value of r. r must be saved by the exit protocol.
func (s *State) Reg(r PReg) uint64 {
	return *s.cell(r)
}

// SetReg stores v in the saved register file for r. r must be saved by the
// exit protocol.
func (s *State) SetReg(r PReg, v uint64) {
	*s.cell(r) = v
}

// Slot reads spill slot n of the suspended native activation.
func (s *State) Slot(n int) uint64 {
	if n < 0 {
		panic("asm: invalid spill slot")
	}
	return *(*uint64)(unsafe.Add(s.nsp, n*8))
}

// Word reads the native stack word at addr, an address a native activation
// recorded, such as its stack pointer plus a spill slot's offset.
func (s *State) Word(addr uintptr) uint64 {
	offset := addr - uintptr(unsafe.Pointer(&s.stack[0]))
	if addr < uintptr(unsafe.Pointer(&s.stack[0])) || offset%8 != 0 || offset/8 >= uintptr(len(s.stack)) {
		panic("asm: address outside the native stack")
	}
	return s.stack[offset/8]
}

func (s *State) cell(r PReg) *uint64 {
	if r.ID() >= uint8(len(s.regs)) {
		panic("asm: invalid register")
	}
	switch r.Type() {
	case RegTypeInt:
		return &s.regs[r.ID()]
	case RegTypeFloat:
		return &s.fregs[r.ID()]
	default:
		panic("asm: invalid register type")
	}
}
