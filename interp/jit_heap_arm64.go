package interp

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/types"
)

func (l arm64Lowerer) refNull(ctx *lowering) bool {
	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(boxed, uint64(types.BoxedNull))...)
	l.retainBox(ctx, boxed)
	ctx.push(value{reg: boxed, kind: types.KindRef, raw: false})
	return true
}

func (l arm64Lowerer) refIsNull(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	vNull := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(vNull, uint64(types.BoxedNull))...)
	ctx.assembler.Emit(arm64.CMP(ref, vNull))
	// Capture the flags before release clobbers them, then release the consumed
	// ref so the bool result leaves no leaked reference on the stack.
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, arm64.CondEQ))
	l.releaseBox(ctx, ref, pre, op.ip)
	ctx.pop()
	ctx.push(value{reg: flag, kind: types.KindI1, raw: true})
	return true
}

func (l arm64Lowerer) refGet(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	kind := op.seen.Kind()
	switch op.shape.itab {
	case heapI32:
		if kind != types.KindI32 {
			return false
		}
	case heapF32:
		if kind != types.KindF32 {
			return false
		}
	case heapF64:
		if kind != types.KindF64 {
			return false
		}
	default:
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, op.shape.itab, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if kind == types.KindF64 {
		ctx.assembler.Emit(arm64.LDR(result, data, 0))
	} else {
		ctx.assembler.Emit(arm64.LDRSW(result, data, 0))
	}
	l.releaseRef(ctx, addr, pre, op.ip)
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: kind, raw: true})
	return true
}

// stringLen mirrors the threaded STRING_LEN handler: a heap-boxed
// types.String has the same {data, len} header layout as a slice, so its
// length lives at the same sliceLen offset arrayLen reads. Unlike ARRAY_LEN,
// the opcode's target concrete type is always types.String, so there is no
// shape to pick among; guardItab below is the only check needed and it deopts
// at runtime instead of aborting the lowering at trace-build time.
func (l arm64Lowerer) stringLen(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapString, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(n, data, sliceLen))
	ctx.assembler.Emit(arm64.MOV(result, n))
	l.releaseRef(ctx, addr, pre, op.ip)
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) arrayLen(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	base := int16(0)
	switch op.shape.itab {
	case heapArrayI1, heapArrayI8, heapArrayI32, heapArrayI64, heapArrayF32, heapArrayF64:
	case heapArrayRef:
		base = int16(arrayElems)
	default:
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, op.shape.itab, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(n, data, base+sliceLen))
	ctx.assembler.Emit(arm64.MOV(result, n))
	l.releaseRef(ctx, addr, pre, op.ip)
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) arrayGet(ctx *lowering, op step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 || ctx.values[len(ctx.values)-2].kind != types.KindRef {
		return false
	}
	kind := op.seen.Kind()
	var want uintptr
	var base int16
	var scale uint8
	var raw bool
	switch kind {
	case types.KindI1:
		want = heapArrayI1
		raw = true
	case types.KindI8:
		want = heapArrayI8
		raw = true
	case types.KindI32:
		want = heapArrayI32
		scale = 2
		raw = true
	case types.KindI64:
		want = heapArrayI64
		scale = 3
		raw = true
	case types.KindF32:
		want = heapArrayF32
		scale = 2
		raw = true
	case types.KindF64:
		want = heapArrayF64
		scale = 3
		raw = true
	case types.KindRef:
		want = heapArrayRef
		base = int16(arrayElems)
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != want {
		return false
	}
	pre := ctx.pre()
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-2])
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, want, fail)

	dataPtr, n := l.sliceHeader(ctx, data, base)
	l.guardIndex(ctx, idx, n, fail)

	result := a.Reg(asm.RegTypeInt, asm.Width64)
	switch kind {
	case types.KindI1:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(result, elemAddr, 0))
	case types.KindI8:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		elem := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(elem, elemAddr, 0))
		a.Emit(arm64.SXTB(result, elem))
	case types.KindI32, types.KindF32:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		off := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(off, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, off))
		a.Emit(arm64.LDRSW(result, elemAddr, 0))
	case types.KindI64, types.KindF64, types.KindRef:
		if scale != 0 {
			off := a.Reg(asm.RegTypeInt, asm.Width64)
			elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LSLI(off, idx, scale))
			a.Emit(arm64.ADD(elemAddr, dataPtr, off))
			a.Emit(arm64.LDR(result, elemAddr, 0))
		} else {
			a.Emit(arm64.LDRR(result, dataPtr, idx))
		}
	}
	if kind == types.KindI64 {
		l.guardBoxable(ctx, result, fail)
	}
	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, fail)
	a.Emit(arm64.SUBI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, rcBase, addr))
	if kind == types.KindRef {
		l.retainBox(ctx, result)
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: kind, raw: raw})
	return true
}

func (l arm64Lowerer) arraySet(ctx *lowering, op step) bool {
	if ctx.count() < 3 || ctx.values[len(ctx.values)-2].kind != types.KindI32 || ctx.values[len(ctx.values)-3].kind != types.KindRef {
		return false
	}
	kind := ctx.values[len(ctx.values)-1].kind
	var want uintptr
	var base int16
	var scale uint8
	switch kind {
	case types.KindI1:
		want = heapArrayI1
	case types.KindI8:
		want = heapArrayI8
	case types.KindI32:
		want = heapArrayI32
		scale = 2
	case types.KindI64:
		want = heapArrayI64
		scale = 3
	case types.KindF32:
		want = heapArrayF32
		scale = 2
	case types.KindF64:
		want = heapArrayF64
		scale = 3
	case types.KindRef:
		want = heapArrayRef
		base = int16(arrayElems)
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != want {
		return false
	}
	pre := ctx.pre()
	val := ctx.values[len(ctx.values)-1]
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-2].reg)
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-3])
	if !ok {
		return false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, want, fail)

	dataPtr, n := l.sliceHeader(ctx, data, base)
	l.guardIndex(ctx, idx, n, fail)

	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, fail)

	if kind == types.KindRef {
		// The container's refcount deopt point (guardRC above) runs before the
		// overwritten element's release, which carries its own internal deopt
		// (releaseBoxUnlessEqual -> releaseRef -> guardRC). Neither refcount is
		// decremented until both checks pass, matching the release-old-ref-first
		// idiom used by globalSet.
		old := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(old, dataPtr, idx))
		l.releaseBoxUnlessEqual(ctx, old, val.reg, pre, op.ip)
	} else {
		switch kind {
		case types.KindI1, types.KindI8:
			elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
			a.Emit(arm64.STRB(val.reg, elemAddr, 0))
		case types.KindI32, types.KindF32:
			elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
			off := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LSLI(off, idx, scale))
			a.Emit(arm64.ADD(elemAddr, dataPtr, off))
			a.Emit(arm64.STRW(val.reg, elemAddr, 0))
		case types.KindI64, types.KindF64:
			a.Emit(arm64.STRR(val.reg, dataPtr, idx))
		}
	}
	a.Emit(arm64.SUBI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, rcBase, addr))
	if kind == types.KindRef {
		a.Emit(arm64.STRR(val.reg, dataPtr, idx))
	}

	ctx.values = pre[: len(pre)-3 : len(pre)-3]
	return l.exit(ctx, op.ip+1)
}

func (l arm64Lowerer) structGet(ctx *lowering, op step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 || ctx.values[len(ctx.values)-2].kind != types.KindRef {
		return false
	}
	out := op.seen.Kind()
	switch out {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != heapStruct {
		return false
	}
	pre := ctx.pre()
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-2])
	if !ok {
		return false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapStruct, fail)

	typ := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(typ, data, int16(structTyp)))
	if op.shape.typ != 0 {
		want := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(want, uint64(op.shape.typ))...)
		a.Emit(arm64.CMP(typ, want))
		a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	}
	fields, n := l.sliceHeader(ctx, typ, int16(fieldsSlice))
	l.guardIndex(ctx, idx, n, fail)

	fieldOff := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(fieldOff, uint64(fieldSize))...)
	a.Emit(arm64.MUL(fieldOff, idx, fieldOff))
	field := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(field, fields, fieldOff))
	fieldKindReg := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRB(fieldKindReg, field, int16(fieldKind)))
	a.Emit(arm64.CMPI(fieldKindReg, uint16(out)))
	a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))

	dataPtr, _ := l.sliceHeader(ctx, data, int16(structData))
	result := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(result, dataPtr, idx))
	if out == types.KindI64 {
		l.guardBoxable(ctx, result, fail)
	}
	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, fail)
	if out == types.KindRef {
		l.retainBox(ctx, result)
	}
	a.Emit(arm64.SUBI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, rcBase, addr))
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: out, raw: out != types.KindRef})
	return true
}

func (l arm64Lowerer) structSet(ctx *lowering, op step) bool {
	if ctx.count() < 3 || ctx.values[len(ctx.values)-2].kind != types.KindI32 || ctx.values[len(ctx.values)-3].kind != types.KindRef {
		return false
	}
	kind := ctx.values[len(ctx.values)-1].kind
	switch kind {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != heapStruct {
		return false
	}
	pre := ctx.pre()
	val := ctx.values[len(ctx.values)-1]
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-2].reg)
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-3])
	if !ok {
		return false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapStruct, fail)

	typ := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(typ, data, int16(structTyp)))
	if op.shape.typ != 0 {
		want := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(want, uint64(op.shape.typ))...)
		a.Emit(arm64.CMP(typ, want))
		a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	}
	fields, n := l.sliceHeader(ctx, typ, int16(fieldsSlice))
	l.guardIndex(ctx, idx, n, fail)

	fieldOff := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(fieldOff, uint64(fieldSize))...)
	a.Emit(arm64.MUL(fieldOff, idx, fieldOff))
	field := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(field, fields, fieldOff))
	fieldKindReg := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRB(fieldKindReg, field, int16(fieldKind)))
	a.Emit(arm64.CMPI(fieldKindReg, uint16(kind)))
	a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))

	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, fail)

	dataPtr, _ := l.sliceHeader(ctx, data, int16(structData))
	if kind == types.KindRef {
		// Mirrors arraySet's ref-element handling: the container's guardRC
		// above is the deopt point for the container's own refcount, and the
		// overwritten field's release (releaseBoxUnlessEqual) carries its own
		// internal deopt. Neither refcount is decremented until both checks
		// pass.
		old := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(old, dataPtr, idx))
		l.releaseBoxUnlessEqual(ctx, old, val.reg, pre, op.ip)
	} else {
		var stored asm.VReg
		switch kind {
		case types.KindI1, types.KindI8, types.KindI32, types.KindF32:
			stored = a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.ANDI(stored, val.reg, maskI32))
		case types.KindI64, types.KindF64:
			stored = val.reg
		}
		a.Emit(arm64.STRR(stored, dataPtr, idx))
	}
	a.Emit(arm64.SUBI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, rcBase, addr))
	if kind == types.KindRef {
		a.Emit(arm64.STRR(val.reg, dataPtr, idx))
	}

	ctx.values = pre[: len(pre)-3 : len(pre)-3]
	return l.exit(ctx, op.ip+1)
}

// errorGet reads a guest Error's payload. It mirrors the threaded handler:
// retain a ref payload first, then release the consumed error handle.
func (l arm64Lowerer) errorGet(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	if op.shape.itab != heapError {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapError, fail)

	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, data, int16(errorValue)))
	kind := op.seen.Kind()
	switch kind {
	case types.KindI64:
		if !l.guardI64(ctx, dst, op.ip) {
			return false
		}
		dst = l.sign64(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	case types.KindRef:
		base := l.rcBase(ctx)
		rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
		ctx.assembler.Emit(arm64.CMPI(rc, 1))
		shared := ctx.assembler.Label()
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBGT, shared))
		ctx.values = append(ctx.values[:0], pre...)
		if !l.exit(ctx, op.ip) {
			return false
		}
		ctx.assembler.Bind(shared)
		ctx.values = append(ctx.values[:0], pre...)
		l.retainBox(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: false})
	case types.KindI32, types.KindF32, types.KindF64:
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	default:
		return false
	}
	return true
}

// coroDone reads a coroutine handle's done flag and pushes it as an i32 (0 or
// 1). It mirrors the threaded handler: the handle ref stays owned by its stack
// slot, so no refcount changes. A constant coroutine handle is impossible, so
// a raw (unboxed constant) ref is rejected to avoid box's retain side effect.
func (l arm64Lowerer) coroDone(ctx *lowering, op step) bool {
	if ctx.count() < 1 {
		return false
	}
	if op.shape.itab != heapCoroutine {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef || v.raw {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, v)
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	_, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapCoroutine, fail)

	done := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDRB(done, data, int16(coroDone)))
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: done, kind: types.KindI1, raw: true})
	return true
}

// coroValue reads a coroutine handle's last yielded or returned value. It
// mirrors the threaded handler: retain the value, then release the handle.
// The stored field is a full Boxed, so its representation matches a global
// slot (see globalGet) — scalars push raw, refs stay boxed.
func (l arm64Lowerer) coroValue(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	if op.shape.itab != heapCoroutine {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip)
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapCoroutine, fail)

	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, data, int16(coroValue)))
	kind := op.seen.Kind()
	switch kind {
	case types.KindI64:
		if !l.guardI64(ctx, dst, op.ip) {
			return false
		}
		dst = l.sign64(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	case types.KindRef:
		base := l.rcBase(ctx)
		rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
		ctx.assembler.Emit(arm64.CMPI(rc, 1))
		shared := ctx.assembler.Label()
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBGT, shared))
		ctx.values = append(ctx.values[:0], pre...)
		if !l.exit(ctx, op.ip) {
			return false
		}
		ctx.assembler.Bind(shared)
		ctx.values = append(ctx.values[:0], pre...)
		l.retainBox(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: false})
	case types.KindI32, types.KindF32, types.KindF64:
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	default:
		return false
	}
	return true
}

// guardHeap loads a heap cell or branches to fail on a non-ref tag.
func (arm64Lowerer) guardHeap(ctx *lowering, ref asm.VReg, fail asm.Label) (asm.VReg, asm.VReg, asm.VReg) {
	a := ctx.assembler
	tag := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSRI(tag, ref, uint8(types.VBits)))
	wantRef := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(wantRef, tagRef>>types.VBits)...)
	a.Emit(arm64.CMP(tag, wantRef))
	a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))

	addr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ANDI(addr, ref, maskI32))
	heap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journalHeap*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(off, addr, 4))
	cell := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(cell, heap, off))
	itab := a.Reg(asm.RegTypeInt, asm.Width64)
	data := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(
		arm64.LDR(itab, cell, 0),
		arm64.LDR(data, cell, 8),
	)
	return addr, itab, data
}

func (arm64Lowerer) sliceHeader(ctx *lowering, data asm.VReg, base int16) (asm.VReg, asm.VReg) {
	ptr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(
		arm64.LDR(ptr, data, base+sliceData),
		arm64.LDR(n, data, base+sliceLen),
	)
	return ptr, n
}

// guardIndex uses one unsigned check: sign-extended negative i32 indexes are
// above any VM array or struct length.
func (arm64Lowerer) guardIndex(ctx *lowering, idx, n asm.VReg, fail asm.Label) {
	ctx.assembler.Emit(arm64.CMP(idx, n))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBCS, fail))
}

func (arm64Lowerer) matchItab(ctx *lowering, got asm.VReg, want uintptr, hit asm.Label) {
	v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(v, uint64(want))...)
	ctx.assembler.Emit(arm64.CMP(got, v))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, hit))
}

func (arm64Lowerer) guardItab(ctx *lowering, got asm.VReg, want uintptr, fail asm.Label) {
	v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(v, uint64(want))...)
	ctx.assembler.Emit(arm64.CMP(got, v))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
}

func (arm64Lowerer) matchKind(ctx *lowering, got asm.VReg, want types.Kind, hit asm.Label) {
	ctx.assembler.Emit(arm64.CMPI(got, uint16(want)))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, hit))
}

// unwind appends one journal frame record per live symbolic frame,
// innermost first so deopt rebuilds the chain in interpreter order. The
// innermost frame resumes at resume; outer frames resume past their calls.
func (l arm64Lowerer) unwind(ctx *lowering, vCtrl asm.VReg, resume int) {
	for k := len(ctx.frames) - 1; k >= 0; k-- {
		f := &ctx.frames[k]
		ip := f.resume
		if k == len(ctx.frames)-1 {
			ip = resume
		}
		l.save(ctx, vCtrl, f, ip)
	}
}

func (l arm64Lowerer) save(ctx *lowering, vCtrl asm.VReg, f *activation, ip int) {
	a := ctx.assembler
	depth := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(depth, vCtrl, int16(journalDepth*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(off, depth, 5))
	base := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(base, vCtrl, off))

	vAddr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vAddr, uint64(f.addr))...)
	bp := ctx.pin(scratchBP)
	if f.base != 0 {
		shifted := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(shifted, bp, uint16(f.base)))
		bp = shifted
	}
	a.Emit(arm64.STP(vAddr, bp, base, int16((journalHead+recordAddr)*8)))

	vIP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vIP, uint64(ip))...)
	vReturns := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vReturns, uint64(f.returns))...)
	a.Emit(arm64.STP(vIP, vReturns, base, int16((journalHead+recordIP)*8)))

	a.Emit(arm64.ADDI(depth, depth, 1))
	a.Emit(arm64.STR(depth, vCtrl, int16(journalDepth*8)))
}

func (l arm64Lowerer) report(ctx *lowering, vCtrl asm.VReg, trap, nextIP int) {
	a := ctx.assembler
	vTrap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vTrap, uint64(trap))...)
	a.Emit(arm64.STR(vTrap, vCtrl, int16(journalTrap*8)))
	vIP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vIP, uint64(nextIP))...)
	a.Emit(arm64.STR(vIP, vCtrl, int16(journalNextIP*8)))
}

// box produces the boxed form of v in a fresh register. A marker materializes
// its function ref and retains it, because the interpreter state being rebuilt
// will release it through the frame it pushes.
func (l arm64Lowerer) box(ctx *lowering, v value) (asm.VReg, bool) {
	a := ctx.assembler
	switch v.kind {
	case types.KindI32:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI32>>48), 48))
		return lo, true
	case types.KindI8:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI8>>48), 48))
		return lo, true
	case types.KindI1:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI1>>48), 48))
		return lo, true
	case types.KindF32:
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		tag := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(tag, tagF32)...)
		a.Emit(arm64.ORR(lo, lo, tag))
		return lo, true
	case types.KindI64:
		// Raw i64 is the full signed value and boxable by invariant; mask the
		// 49-bit lane and tag.
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI64))
		tag := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(tag, tagI64)...)
		a.Emit(arm64.ORR(lo, lo, tag))
		return lo, true
	case types.KindF64:
		return v.reg, true
	case types.KindRef:
		if !v.raw {
			return v.reg, true
		}
		ref := v.ref
		if ref == 0 {
			ref = v.fn
		}
		boxed := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(boxed, uint64(types.BoxRef(ref)))...)
		l.retain(ctx, ref)
		return boxed, true
	}
	return asm.VReg{}, false
}

// retain bumps the refcount of the heap cell at compile-time address fn.
func (l arm64Lowerer) retain(ctx *lowering, fn int) {
	a := ctx.assembler
	base := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalRC*8)))
	slot := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(slot, uint64(fn))...)
	rc := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(rc, base, slot))
	a.Emit(arm64.ADDI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, base, slot))
}

func (l arm64Lowerer) retainBox(ctx *lowering, v asm.VReg) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		l.retainRef(ctx, addr)
	})
}

func (l arm64Lowerer) releaseBox(ctx *lowering, v asm.VReg, pre []value, ip int) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		l.releaseRef(ctx, addr, pre, ip)
	})
}

func (l arm64Lowerer) releaseBoxUnlessEqual(ctx *lowering, old, val asm.VReg, pre []value, ip int) {
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CMP(old, val))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, done))
	l.releaseBox(ctx, old, pre, ip)
	ctx.assembler.Bind(done)
	ctx.values = append(ctx.values[:0], pre...)
}

func (l arm64Lowerer) retainBoxUnlessEqual(ctx *lowering, old, val asm.VReg) {
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CMP(old, val))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, done))
	l.retainBox(ctx, val)
	ctx.assembler.Bind(done)
}

func (l arm64Lowerer) retainRef(ctx *lowering, addr asm.VReg) {
	base := l.rcBase(ctx)
	rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
	ctx.assembler.Emit(arm64.ADDI(rc, rc, 1))
	ctx.assembler.Emit(arm64.STRR(rc, base, addr))
}

// guardRC keeps releases that could free objects in the interpreter.
func (arm64Lowerer) guardRC(ctx *lowering, addr, rcBase asm.VReg, fail asm.Label) asm.VReg {
	a := ctx.assembler
	rc := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(rc, rcBase, addr))
	a.Emit(arm64.CMPI(rc, 1))
	a.Emit(arm64.BCondLabel(arm64.OpBLE, fail))
	return rc
}

// releaseRef decrements addr after guardRC proves it will stay live.
func (l arm64Lowerer) releaseRef(ctx *lowering, addr asm.VReg, pre []value, ip int) {
	fail, ok := l.sideExit(ctx, pre, ip)
	if !ok {
		return
	}
	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, fail)
	ctx.assembler.Emit(arm64.SUBI(rc, rc, 1))
	ctx.assembler.Emit(arm64.STRR(rc, rcBase, addr))
}

func (l arm64Lowerer) refOnly(ctx *lowering, v asm.VReg, body func(asm.VReg)) {
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagRef>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))

	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, done))

	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(addr, v, maskI32))
	body(addr)

	ctx.assembler.Bind(done)
}

func (arm64Lowerer) rcBase(ctx *lowering) asm.VReg {
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalRC*8)))
	return base
}
