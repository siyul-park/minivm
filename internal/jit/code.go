package jit

import "github.com/siyul-park/minivm/internal/asm"

// Tier is how far a function's native code is optimized; the zero Tier is none.
type Tier uint8

const (
	Baseline Tier = iota + 1
	Optimized
)

// Promote is the entry count, written by a Baseline prologue on every
// interpreted or native entry, at which the interpreter tiers an address to
// Optimized.
const Promote = 1024

func (t Tier) String() string {
	switch t {
	case Baseline:
		return "baseline"
	case Optimized:
		return "optimized"
	default:
		return "none"
	}
}

// Code is the native code of one function at one tier, in executable memory of its own.
type Code struct {
	Address int
	// IP is the bytecode offset this code is rooted at.
	IP int
	// OSR reports whether c is rooted at a loop header instead of Address's
	// own entry: never a call target, so Store keys it by (Address, IP)
	// instead of installing it into Context.Natives.
	OSR  bool
	Tier Tier
	// Results is the value count a TrapReturn leaves: module code's own
	// OpComplete carries it on the operand stack past the locals, since it
	// has no Typ.Returns to read it from otherwise.
	Results int
	Exits   []Exit

	entry  uintptr
	size   int
	buffer *asm.Buffer
}

// NewCode links code into a new Buffer sized len(code) and returns the
// native code of address rooted at ip (an OSR unit when osr) at tier, whose
// exits it takes are exits and TrapReturn value count is results.
func NewCode(address, ip int, osr bool, tier Tier, results int, code []byte, exits []Exit) (*Code, error) {
	buffer, err := asm.NewBuffer(len(code))
	if err != nil {
		return nil, err
	}
	entry, err := asm.Link(buffer, code)
	if err != nil {
		_ = buffer.Free()
		return nil, err
	}
	return &Code{Address: address, IP: ip, OSR: osr, Tier: tier, Results: results, Exits: exits, entry: entry, size: len(code), buffer: buffer}, nil
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
