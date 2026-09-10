package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// read lowers one guarded array read: the guard that admits the container,
// and the access that loads through it. The two lower as one shape because
// they are one bytecode operation - they resume into a single interpreter
// state, and the bounds test below leaves through the state the guard in
// front of it carries.
//
// With the guard behind it, the read is the index test the plan pipeline's
// guardIndex performs and the one load the element's own storage needs. The
// shape table states the stride, so a new element kind stays one row there
// rather than a rule here.
//
// It reads only an element that fits a register on its own. An i64 may be
// heap-promoted, which needs the boxability guard and the register lane this
// machine has neither of, and a reference read out of a container is owned by
// whoever receives it, which needs a retain the IR does not carry.
//
// It emits before it has finished refusing: the guard commits instructions
// and reserves a stub, and the bounds exit and the element kind can still
// decline behind them. That is sound only because a false anywhere in
// lowering abandons the whole Compile, whose assembler is per-attempt and
// discarded unpublished - not because the emitted work is unwound.
func (e *emitter) read(guard, get ssa.Operation) bool {
	if len(get.Args) != 2 || len(get.Results) != 1 {
		return false
	}
	if len(guard.Results) != 1 || guard.Results[0] != get.Args[0] {
		return false
	}
	shape, ok := jit.ElemShapeByItab(guard.Shape.Itab)
	if !ok || !e.lanes(ssa.TypeI32, get.Args[1]) {
		return false
	}
	dst := e.c.Reg(get.Results[0])
	typ := e.c.Func().Type(get.Results[0])
	if typ != ssa.TypeOf(shape.Kind) || lane(typ) == 0 {
		return false
	}
	data, ok := e.guard(guard)
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
	// One unsigned test covers both ends: a sign-extended negative index is
	// above any length a VM container can have.
	idx := e.sign32(e.c.Reg(get.Args[1]))
	e.a.Emit(arm64.CMP(idx, length), arm64.BCondLabel(arm64.OpBCS, fail))

	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	if shape.Scale == 0 {
		e.a.Emit(arm64.ADD(addr, ptr, idx))
	} else {
		off := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LSLI(off, idx, shape.Scale), arm64.ADD(addr, ptr, off))
	}
	// The lane test above already admits exactly these five kinds, so the
	// refusal below cannot fire today. It is kept because what pins the five
	// is three tables in three packages agreeing - the shape table's element
	// kinds, ssa.TypeOf, and lane - and a kind that stops agreeing declines
	// here rather than loading at some other width.
	switch shape.Kind {
	case types.KindI1:
		e.a.Emit(arm64.LDRB(dst, addr, 0))
	case types.KindI8:
		raw := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDRB(raw, addr, 0), arm64.SXTB(dst, raw))
	case types.KindI32:
		e.a.Emit(arm64.LDRSW(dst, addr, 0))
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
