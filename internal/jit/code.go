package jit

import "github.com/siyul-park/minivm/internal/asm"

// Tier is how far a function's native code is optimized; the zero Tier is none.
type Tier uint8

const (
	Baseline Tier = iota + 1
	Optimized
)

// Code is the native code of one function at one tier, in executable memory of its own.
type Code struct {
	Address int
	Tier    Tier
	Exits   []Exit

	entry  uintptr
	size   int
	buffer *asm.Buffer
}

// NewCode links code into a new Buffer sized len(code) and returns the
// native code of address at tier, whose exits it takes are exits.
func NewCode(address int, tier Tier, code []byte, exits []Exit) (*Code, error) {
	buffer, err := asm.NewBuffer(len(code))
	if err != nil {
		return nil, err
	}
	entry, err := asm.Link(buffer, code)
	if err != nil {
		_ = buffer.Free()
		return nil, err
	}
	return &Code{Address: address, Tier: tier, Exits: exits, entry: entry, size: len(code), buffer: buffer}, nil
}

// Entry returns c's native entry address.
func (c *Code) Entry() uintptr {
	return c.entry
}

// Free unmaps c's executable memory; a second call is a no-op. Nothing may
// run c's code after either call.
func (c *Code) Free() error {
	return c.buffer.Free()
}
