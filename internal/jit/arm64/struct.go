package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// structRead lowers a guard and the field read it admits as one shape,
// mirroring read: the guard's Shape.Typ narrows the container to the one
// concrete *types.StructType the access was compiled against, and the field
// at the runtime index is proven against the kind the frontend resolved for
// it before admitting the load. A struct's fields differ in kind by index,
// unlike an array's elements, so that per-field kind guard is what this adds
// beyond read's bounds test (see docs/jit-internals.md, Speculation).
//
// A struct field's storage slot is a full 8-byte word already holding the
// value in its VM raw register form (types.Struct.SetField), so one LDRR
// reads any kind; only a float result needs the extra FMOV that moves it
// into the float bank this backend's register allocator gives it.
//
// The guard emits and reserves a stub before the bounds and kind exits, and
// either can still decline. Sound only because a false anywhere in lowering
// abandons the whole Compile and its assembler unpublished.
func (e *emitter) structRead(guard, get ssa.Operation) bool {
	if !e.fused(guard, get) {
		return false
	}
	if guard.Shape.Itab != jit.HeapStruct || guard.Shape.Typ == 0 || guard.Shape.Host != 0 {
		return false
	}
	kind, ok := resultKind(e.c.Func().Type(get.Results[0]))
	if !ok {
		return false
	}
	dst := e.c.Reg(get.Results[0])
	data, fail, ok := e.guard(guard)
	if !ok {
		return false
	}
	bounds, ok := e.exit(guard.State, prof.ExitGuardBounds)
	if !ok {
		return false
	}
	kindFail, ok := e.exit(guard.State, prof.ExitGuardKind)
	if !ok {
		return false
	}

	typ := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(typ, data, int16(structTyp)))
	e.admit(typ, uint64(guard.Shape.Typ), fail)
	fields := e.a.Reg(asm.RegTypeInt, asm.Width64)
	n := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(fields, typ, int16(fieldsSlice+sliceData)),
		arm64.LDR(n, typ, int16(fieldsSlice+sliceLen)),
	)
	idx := e.sign32(e.c.Reg(get.Args[1]))
	field := e.fieldEntry(idx, n, fields, fieldSize, bounds)
	got := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDRB(got, field, int16(fieldKind)))
	e.a.Emit(arm64.CMPI(got, uint16(kind)), arm64.BCondLabel(arm64.OpBNE, kindFail))

	dataPtr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(dataPtr, data, int16(structData+sliceData)))
	switch kind {
	case types.KindF32:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDRR(bits, dataPtr, idx), arm64.FMOV(dst, narrow32(bits)))
	case types.KindF64:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDRR(bits, dataPtr, idx), arm64.FMOV(dst, bits))
	default:
		e.a.Emit(arm64.LDRR(dst, dataPtr, idx))
	}
	return true
}

// hostRead lowers a guard and the *HostStruct field read it admits as one
// shape. A host field holds Go memory reached through jit.Layout offsets
// rather than a VM word, and its width, signedness, and Go kind are not
// implied by the VM kind alone - int16, int32, and uint32 all reach the
// guest as i32 - so the per-field checks below are what a plain shape guard
// cannot express (see ssa.Shape.Host).
func (e *emitter) hostRead(guard, get ssa.Operation) bool {
	if !e.fused(guard, get) {
		return false
	}
	layout := e.c.Input().Layout
	if guard.Shape.Itab != layout.HostStructItab || guard.Shape.Typ != 0 || guard.Shape.Host == 0 {
		return false
	}
	shape, ok := jit.HostShapeByKind(guard.Shape.Host)
	if !ok {
		return false
	}
	kind, ok := resultKind(e.c.Func().Type(get.Results[0]))
	if !ok || kind != shape.Kind {
		return false
	}
	size, signed := shape.Read()
	dst := e.c.Reg(get.Results[0])
	data, _, ok := e.guard(guard)
	if !ok {
		return false
	}
	bounds, ok := e.exit(guard.State, prof.ExitGuardBounds)
	if !ok {
		return false
	}
	kindFail, ok := e.exit(guard.State, prof.ExitGuardKind)
	if !ok {
		return false
	}

	fields := e.a.Reg(asm.RegTypeInt, asm.Width64)
	n := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(fields, data, int16(layout.HostFields+sliceData)),
		arm64.LDR(n, data, int16(layout.HostFields+sliceLen)),
	)
	idx := e.sign32(e.c.Reg(get.Args[1]))
	entry := e.fieldEntry(idx, n, fields, layout.HostFieldSize, bounds)

	conv := e.a.Reg(asm.RegTypeInt, asm.Width64)
	got := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(conv, entry, int16(layout.HostFieldConv)),
		arm64.LDRB(got, conv, int16(layout.HostConvKind)),
	)
	e.a.Emit(arm64.CMPI(got, uint16(guard.Shape.Host)), arm64.BCondLabel(arm64.OpBNE, kindFail))

	offset := e.a.Reg(asm.RegTypeInt, asm.Width64)
	base := e.a.Reg(asm.RegTypeInt, asm.Width64)
	target := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(offset, entry, int16(layout.HostFieldOffset)),
		arm64.LDR(base, data, int16(layout.HostPtr)),
		arm64.ADD(target, base, offset),
	)

	// The extension mirrors heap.go's hostLoad; a float result additionally
	// FMOVs into the float bank this backend's register allocator gave dst.
	switch {
	case size == 1 && signed:
		raw := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDRB(raw, target, 0), arm64.SXTB(dst, raw))
	case size == 1:
		e.a.Emit(arm64.LDRB(dst, target, 0))
	case size == 2 && signed:
		e.a.Emit(arm64.LDRSH(dst, target, 0))
	case size == 2:
		e.a.Emit(arm64.LDRH(dst, target, 0))
	case kind == types.KindF32:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDRSW(bits, target, 0), arm64.FMOV(dst, narrow32(bits)))
	case kind == types.KindF64:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDR(bits, target, 0), arm64.FMOV(dst, bits))
	case size == 4:
		e.a.Emit(arm64.LDRSW(dst, target, 0))
	}
	return true
}

// resultKind resolves the types.Kind a STRUCT_GET's SSA result type names, or
// false for one this machine has no lane for: an i64 result may be
// heap-promoted and a ref result is owned by whoever receives it, and this
// machine emits the boxability guard behind neither (see lane).
func resultKind(t ssa.Type) (types.Kind, bool) {
	switch t {
	case ssa.TypeI1:
		return types.KindI1, true
	case ssa.TypeI8:
		return types.KindI8, true
	case ssa.TypeI32:
		return types.KindI32, true
	case ssa.TypeF32:
		return types.KindF32, true
	case ssa.TypeF64:
		return types.KindF64, true
	default:
		return 0, false
	}
}

// fieldEntry bounds-checks idx against a fields table of length n, then
// returns the address of the idx'th row of that many stride bytes in the
// table starting at fields. structRead's StructField row and hostRead's host
// field descriptor share this addressing shape even though what each row
// holds afterward is unrelated - one names a VM kind, the other a Go one.
func (e *emitter) fieldEntry(idx, n, fields asm.VReg, stride int, bounds asm.Label) asm.VReg {
	e.a.Emit(arm64.CMP(idx, n), arm64.BCondLabel(arm64.OpBCS, bounds))
	off := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDI(off, uint64(stride))...)
	e.a.Emit(arm64.MUL(off, idx, off))
	entry := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.ADD(entry, fields, off))
	return entry
}
