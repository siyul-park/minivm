package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// read lowers a guard and the array access it admits as one shape: they are
// one bytecode operation, so the bounds test leaves through the state the
// guard carries. Element stride lives in the shape table, not here.
//
// An i64 element may be heap-promoted and a reference element is owned by
// whoever receives it; neither has a lane or a retain here, so both decline.
//
// The guard emits and reserves a stub before the bounds exit and the element
// kind can still decline. Sound only because a false anywhere in lowering
// abandons the whole Compile and its assembler unpublished.
func (e *emitter) read(guard, get ssa.Operation) bool {
	if !e.fused(guard, get) {
		return false
	}
	shape, ok := jit.ElemShapeByItab(guard.Shape.Itab)
	if !ok {
		return false
	}
	dst := e.c.Reg(get.Results[0])
	typ := e.c.Func().Type(get.Results[0])
	if typ != ssa.TypeOf(shape.Kind) || lane(typ) == 0 {
		return false
	}
	data, _, ok := e.guard(guard)
	if !ok {
		return false
	}
	fail, ok := e.exit(guard.State, prof.ExitGuardBounds)
	if !ok {
		return false
	}

	ptr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	length := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(ptr, data, shape.Base+sliceData),
		arm64.LDR(length, data, shape.Base+sliceLen),
	)
	// A sign-extended negative index is above any length, so one unsigned
	// test covers both ends.
	idx := e.sign32(e.c.Reg(get.Args[1]))
	e.a.Emit(arm64.CMP(idx, length), arm64.BCondLabel(arm64.OpBCS, fail))

	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	if shape.Scale == 0 {
		e.a.Emit(arm64.ADD(addr, ptr, idx))
	} else {
		off := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LSLI(off, idx, shape.Scale), arm64.ADD(addr, ptr, off))
	}
	// Unreachable while the shape table, ssa.TypeOf, and lane agree on these
	// five kinds; a kind that stops agreeing declines instead of loading at
	// the wrong width.
	switch shape.Kind {
	case types.KindI1:
		e.a.Emit(arm64.LDRB(dst, addr, 0))
	case types.KindI8:
		raw := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDRB(raw, addr, 0), arm64.SXTB(dst, raw))
	case types.KindI32:
		// A plain 32-bit LDR, not LDRSW: dst is the W-lane raw representation
		// (see backend.bank), which wants the upper 32 bits zero-extended, not
		// sign-extended - the low 32 bits it reads are the element's own
		// two's-complement bit pattern either way.
		e.a.Emit(arm64.LDR(dst, addr, 0))
	case types.KindF32:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDRSW(bits, addr, 0), arm64.FMOV(dst, narrow32(bits)))
	case types.KindF64:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDR(bits, addr, 0), arm64.FMOV(dst, bits))
	default:
		return false
	}
	return true
}
