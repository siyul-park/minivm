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
// A typed array's i64 element is stored raw, unlike a VM slot's boxed word,
// so there is no tag to admit the way guardI64 admits one: the element loads
// unconditionally and guardBoxable proves it fits the boxed payload
// afterward, exiting through the same state the bounds test already resumes
// through. A reference element is owned by whoever receives it, which this
// machine has not learned to account for, so the switch below still declines
// it by naming no case for KindRef.
//
// The guard emits and reserves a stub before the bounds exit, the element
// kind, and (for KindI64) the boxability guard can still decline. Sound only
// because a false anywhere in lowering abandons the whole Compile and its
// assembler unpublished.
func (e *emitter) read(guard, get ssa.Operation) bool {
	if !e.fused(guard, get, 2, 1) {
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
	// six kinds; a kind that stops agreeing declines instead of loading at
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
	case types.KindI64:
		e.a.Emit(arm64.LDR(dst, addr, 0))
		if !e.guardBoxable(guard.State, dst) {
			return false
		}
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

// write lowers a guard and the array store it admits as one shape, mirroring
// read: the bounds test leaves through the state the guard carries, and
// leaves before the store, since a store that already ran cannot be undone
// by a later deopt the way a read's own boxability guard can still discard
// its result. The element kind is the value's own SSA type, so ElemShapeByItab
// recovers the same row frontend/walk.go resolved it from - the write knows
// no more about the container than the guard already proved.
//
// The element is written raw, at the same address arithmetic read computes;
// element stride lives in the shape table, not here. A reference element is
// owned by whoever the array's overwritten slot released it to, which this
// machine has not learned to account for, so the switch below still declines
// it by naming no case for KindRef.
func (e *emitter) write(guard, set ssa.Operation) bool {
	if !e.fused(guard, set, 3, 0) {
		return false
	}
	val := set.Args[2]
	typ := e.c.Func().Type(val)
	shape, ok := jit.ElemShapeByItab(guard.Shape.Itab)
	if !ok || typ != ssa.TypeOf(shape.Kind) || lane(typ) == 0 {
		return false
	}
	raw, ok := e.raw(val)
	if !ok {
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
	idx := e.sign32(e.c.Reg(set.Args[1]))
	e.a.Emit(arm64.CMP(idx, length), arm64.BCondLabel(arm64.OpBCS, fail))

	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	if shape.Scale == 0 {
		e.a.Emit(arm64.ADD(addr, ptr, idx))
	} else {
		off := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LSLI(off, idx, shape.Scale), arm64.ADD(addr, ptr, off))
	}
	// Unreachable while the shape table, ssa.TypeOf, and lane agree on these
	// six kinds; a kind that stops agreeing declines instead of storing at
	// the wrong width.
	switch shape.Kind {
	case types.KindI1, types.KindI8:
		e.a.Emit(arm64.STRB(raw, addr, 0))
	case types.KindI32, types.KindF32:
		e.a.Emit(arm64.STRW(raw, addr, 0))
	case types.KindI64, types.KindF64:
		e.a.Emit(arm64.STR(raw, addr, 0))
	default:
		return false
	}
	return true
}
