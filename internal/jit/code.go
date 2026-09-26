package jit

import (
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/types"
)

// Tier is how far a function's native code is optimized; the zero Tier is none.
type Tier uint8

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
	// Registers is the function's register-convention result kinds, nil
	// when Return boxes to the frame. An i64 one reaches its frame slot as
	// a raw word the caller must box.
	Registers []types.Kind
	// Arguments is the function's register-convention parameter kinds, nil
	// when Prologue reads only slots. A caller whose i64 argument slot holds
	// a heap ref (KindRef) must decline Entry: the Go entry stub unboxes an
	// i64 parameter inline (SBFX), never through the heap.
	Arguments []types.Kind
	Exits     []Exit

	// native is the body's own address: offset 0, what natives[Address]
	// holds for a native-to-native call. entry is the Go entry stub's
	// address, native plus stub's byte offset: what Go crosses into native
	// code through (jit.Enter/interp's fresh calls and OSR entries).
	native uintptr
	entry  uintptr
	size   int
	buffer *asm.Buffer
	// retired is set by Store under its lock before c joins the retired list.
	retired atomic.Bool
}

// Tier values published by the native runtime.
const (
	Baseline Tier = iota + 1
	Optimized
)

// NewCode links code into a new Buffer and returns one native code value with
// its identity, signature metadata, exits, and Go entry stub.
func NewCode(address, ip int, osr bool, tier Tier, results int, registers, arguments []types.Kind, code []byte, exits []Exit, stub int) (*Code, error) {
	if stub < 0 || stub > len(code) {
		return nil, fmt.Errorf("%w: entry stub offset %d", asm.ErrInvalidArgs, stub)
	}
	buffer, err := asm.NewBuffer(len(code))
	if err != nil {
		return nil, err
	}
	native, err := asm.Link(buffer, code)
	if err != nil {
		_ = buffer.Free()
		return nil, err
	}
	return &Code{
		Address: address, IP: ip, OSR: osr, Tier: tier, Results: results,
		Registers: slices.Clone(registers), Arguments: slices.Clone(arguments), Exits: slices.Clone(exits),
		native: native, entry: native + uintptr(stub), size: len(code), buffer: buffer,
	}, nil
}

// String returns the tier name.
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

// Native returns c's body address, offset 0: the address a native-to-native
// call dispatches to.
func (c *Code) Native() uintptr {
	return c.native
}

// Entry returns c's Go entry stub address: where a fresh call from Go, or an
// OSR entry, crosses into native code.
func (c *Code) Entry() uintptr {
	return c.entry
}

// holds reports whether pc lies inside c's native code.
// Retired reports whether Store has retired c: an interpreter that cached c
// and called Store.Enter may still run it until then without a lookup.
func (c *Code) Retired() bool {
	return c.retired.Load()
}

func (c *Code) holds(pc uintptr) bool {
	return pc >= c.native && pc < c.native+uintptr(c.size)
}

// Free unmaps c's executable memory; a second call is a no-op. Nothing may
// run c's code after either call.
func (c *Code) Free() error {
	return c.buffer.Free()
}
