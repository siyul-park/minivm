package jit

import "sync"

// Layout describes the consumer-side memory layout that the JIT must access
// via raw native instructions. The consumer registers its layout once via
// Bind; jit reads the offsets back when emitting prologues, epilogues, and
// stack/frame accesses.
//
// All offsets are byte offsets from the start of the containing struct.
type Layout struct {
	// SPOffset is the byte offset of the stack pointer (int) inside the
	// interpreter struct.
	SPOffset uintptr

	// FPOffset is the byte offset of the frame pointer (int).
	FPOffset uintptr

	// FROffset is the byte offset of the current-frame pointer (*frame).
	FROffset uintptr

	// FramesOffset is the byte offset of the frames slice header in the
	// interpreter struct.
	FramesOffset uintptr

	// FrameSize is the size in bytes of one frame element.
	FrameSize uintptr

	// FrameAddrOff is the byte offset of the addr (int) field in a frame.
	FrameAddrOff uintptr

	// FrameIPOff is the byte offset of the ip (int) field in a frame.
	FrameIPOff uintptr

	// FrameBPOff is the byte offset of the bp (int) field in a frame.
	FrameBPOff uintptr

	// FrameRetsOff is the byte offset of the returns (int) field in a
	// frame.
	FrameRetsOff uintptr

	// FrameRefOff is the byte offset of the ref (int) field in a frame.
	FrameRefOff uintptr
}

var (
	layoutMu sync.RWMutex
	layout   Layout
	bound    bool
)

// Bind installs the consumer-side struct layout. Subsequent calls overwrite
// the previously bound layout.
func Bind(l Layout) {
	layoutMu.Lock()
	defer layoutMu.Unlock()
	layout = l
	bound = true
}

// RuntimeLayout returns the currently bound layout.
func RuntimeLayout() Layout {
	layoutMu.RLock()
	defer layoutMu.RUnlock()
	return layout
}

// Bound reports whether Bind has been called.
func Bound() bool {
	layoutMu.RLock()
	defer layoutMu.RUnlock()
	return bound
}
