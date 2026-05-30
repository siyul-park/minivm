package jit

import "sync"

// Layout exposes the consumer's frame/interpreter struct field offsets so
// the JIT can emit native code that touches those fields without importing
// the consumer package. The consumer registers its layout once at init via
// Bind; jit reads it back when emitting prologues, epilogues, and frame
// pushes/pops.
//
// All offsets are byte offsets from the start of the struct.
type Layout struct {
	FpOffset       uintptr
	FramesOffset   uintptr
	FrameSize      uintptr
	FrameAddrOff   uintptr
	FrameIPOff     uintptr
	FrameBPOff     uintptr
	FrameRetsOff   uintptr
	FrameRefOff    uintptr
	FrameUpvalsOff uintptr
}

var (
	layoutOnce sync.Once
	layout     Layout
)

// Bind installs the consumer-side struct layout. It is safe to call from any
// init function; only the first call has effect.
func Bind(l Layout) {
	layoutOnce.Do(func() {
		layout = l
	})
}

// RuntimeLayout returns the currently bound layout. Lowerers consult this
// when emitting frame/interpreter accesses.
func RuntimeLayout() Layout { return layout }
