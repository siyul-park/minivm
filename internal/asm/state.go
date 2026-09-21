package asm

import (
	"fmt"
	"unsafe"
)

// State is the machine state one native execution and the Go code around it
// share: the native stack, the Go registers saved while native code runs,
// and where and with which registers the last exit left. Native code holds
// its address in the pinned state register and leaves through the stub at
// OffsetStub. Whoever needs more than the machine embeds State as its first
// field, so the one address names both.
//
// It is a heap object Go never moves, and native code writes only its scalar
// fields, so no write barrier is ever owed. The native stack it owns is
// noscan memory: a Go pointer stored there is invisible to the collector,
// which is why native code stores none.
type State struct {
	stub  uintptr
	stack []uint64

	sp, fp, lr uintptr
	pc         uintptr
	nsp, top   unsafe.Pointer
	exited     uint64
	regs       [32]uint64
	fregs      [32]uint64
}

// OffsetStub is where native code finds the exit stub off the state
// register. The trampoline reads every other field through go_asm.h.
const OffsetStub = unsafe.Offsetof(State{}.stub)

// NewState returns a state owning a native stack of size bytes, with the
// native stack pointer at the stack's 16-byte-aligned top. Nothing in it
// depends on its own address until Enter, which takes it by pointer and so
// keeps it off the goroutine stack.
func NewState(size int) (State, error) {
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
	return State{stub: exitPC(), nsp: top, top: top, stack: stack}, nil
}

// PC reports where the last exit left: the exit stub's return address, which
// lies in the exiting activation's code.
func (s *State) PC() uintptr {
	return s.pc
}

// Abandon discards a suspended native stack, so the next Enter runs a fresh
// activation from the stack's top rather than nesting below the abandoned
// one.
func (s *State) Abandon() {
	s.nsp = s.top
	s.exited = 0
}

// Reg reports the saved value of r. r must be saved by the exit protocol.
func (s *State) Reg(r PReg) uint64 {
	return *s.slot(r)
}

// SetReg stores v in the saved register file for r. r must be saved by the
// exit protocol.
func (s *State) SetReg(r PReg, v uint64) {
	*s.slot(r) = v
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

func (s *State) slot(r PReg) *uint64 {
	if r.id >= uint8(len(s.regs)) {
		panic("asm: invalid register")
	}
	switch r.typ {
	case RegTypeInt:
		return &s.regs[r.id]
	case RegTypeFloat:
		return &s.fregs[r.id]
	default:
		panic("asm: invalid register type")
	}
}
