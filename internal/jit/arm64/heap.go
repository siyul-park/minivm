package arm64

import (
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

const (
	sliceData   = 0
	sliceLen    = 8
	structTyp   = int(unsafe.Offsetof(types.Struct{}.Typ))
	structData  = int(unsafe.Offsetof(types.Struct{}.Data))
	fieldsSlice = int(unsafe.Offsetof(types.StructType{}.Fields))
	fieldKind   = int(unsafe.Offsetof(types.StructField{}.Kind))
	fieldSize   = int(unsafe.Sizeof(types.StructField{}))
	errorValue  = types.ErrorValueOffset
)

func (l lowerer) arrayGetKnown(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 {
		return false
	}
	marker := ctx.values[len(ctx.values)-2]
	constant := marker.ref
	if marker.backing != jit.BackingConst || constant <= 0 || constant >= len(ctx.heap) {
		return false
	}

	var kind types.Kind
	var want uintptr
	var scale uint8
	switch value := ctx.heap[constant].(type) {
	case types.TypedArray[bool]:
		kind, want = types.KindI1, jit.Itab(value)
	case types.TypedArray[int8]:
		kind, want = types.KindI8, jit.Itab(value)
	case types.TypedArray[int32]:
		kind, want, scale = types.KindI32, jit.Itab(value), 2
	case types.TypedArray[float32]:
		kind, want, scale = types.KindF32, jit.Itab(value), 2
	case types.TypedArray[float64]:
		kind, want, scale = types.KindF64, jit.Itab(value), 3
	default:
		return false
	}

	pre := ctx.pre()
	if !l.flush(ctx, flushSnapshot) {
		return false
	}
	l.clearLocals(ctx)
	fail := ctx.queueExit(nil, op.IP, prof.ExitGuardValue, int(op.Op))

	a := ctx.assembler
	heap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journal.CellHeap*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(off, uint64(constant))...)
	a.Emit(arm64.LSLI(off, off, 4))
	cell := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(cell, heap, off))
	actual := a.Reg(asm.RegTypeInt, asm.Width64)
	data := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(actual, cell, 0), arm64.LDR(data, cell, 8))
	l.guardItab(ctx, actual, want, fail)

	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	dataPtr, n := l.sliceHeader(ctx, data, 0)
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
		elemOff := a.Reg(asm.RegTypeInt, asm.Width64)
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(elemOff, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, elemOff))
		a.Emit(arm64.LDRSW(result, elemAddr, 0))
	case types.KindF64:
		elemOff := a.Reg(asm.RegTypeInt, asm.Width64)
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(elemOff, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, elemOff))
		a.Emit(arm64.LDR(result, elemAddr, 0))
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: kind, raw: true})
	return true
}

func (l lowerer) refGet(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	kind := op.Seen.Kind()
	switch op.Shape.Itab {
	case jit.HeapI32:
		if kind != types.KindI32 {
			return false
		}
	case jit.HeapF32:
		if kind != types.KindF32 {
			return false
		}
	case jit.HeapF64:
		if kind != types.KindF64 {
			return false
		}
	default:
		return false
	}
	owned := ctx.values[len(ctx.values)-1].backing == jit.BackingStack
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, op.Shape.Itab, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if kind == types.KindF64 {
		ctx.assembler.Emit(arm64.LDR(result, data, 0))
	} else {
		ctx.assembler.Emit(arm64.LDRSW(result, data, 0))
	}
	if owned {
		l.releaseRef(ctx, addr, pre, op.IP)
	}
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: kind, raw: true})
	return true
}

// stringLen mirrors the threaded STRING_LEN handler: a heap-boxed
// types.String has the same {data, len} header layout as a slice, so its
// length lives at the same sliceLen offset arrayLen reads. Unlike ARRAY_LEN,
// the opcode's target concrete type is always types.String, so there is no
// shape to pick among; guardItab below is the only check needed and it deopts
// at runtime instead of aborting the lowering at trace-build time.
func (l lowerer) stringLen(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	owned := ctx.values[len(ctx.values)-1].backing == jit.BackingStack
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, jit.HeapString, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(n, data, sliceLen))
	ctx.assembler.Emit(arm64.MOV(result, n))
	if owned {
		l.releaseRef(ctx, addr, pre, op.IP)
	}
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: types.KindI32, raw: true})
	return true
}

func (l lowerer) arrayLen(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	shape, ok := jit.ElemShapeByItab(op.Shape.Itab)
	if !ok {
		return false
	}
	base := shape.Base
	owned := ctx.values[len(ctx.values)-1].backing == jit.BackingStack
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, op.Shape.Itab, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(n, data, base+sliceLen))
	ctx.assembler.Emit(arm64.MOV(result, n))
	if owned {
		l.releaseRef(ctx, addr, pre, op.IP)
	}
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: types.KindI32, raw: true})
	return true
}

func (l lowerer) arrayGet(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 || ctx.values[len(ctx.values)-2].kind != types.KindRef {
		return false
	}
	kind := op.Seen.Kind()
	shape, ok := jit.ElemShapeByKind(kind)
	if !ok {
		return false
	}
	want, base, scale, raw := shape.Itab, shape.Base, shape.Scale, shape.Raw
	if op.Shape.Itab != 0 && op.Shape.Itab != want {
		return false
	}
	container := ctx.values[len(ctx.values)-2]
	owned := container.backing == jit.BackingStack
	hoisted := ctx.hoist.live && container.backing == jit.BackingLocal && container.slot == ctx.hoist.slot && want == ctx.hoist.want
	pre := ctx.pre()
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	a := ctx.assembler
	bounds, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardBounds, int(op.Op))
	if !ok {
		return false
	}
	var fail, valueFail asm.Label
	if !hoisted || kind == types.KindI64 {
		valueFail, ok = l.sideExit(ctx, pre, op.IP, prof.ExitGuardValue, int(op.Op))
		if !ok {
			return false
		}
	}
	var addr, dataPtr, n asm.VReg
	if hoisted {
		dataPtr, n = ctx.hoist.dataPtr, ctx.hoist.n
	} else {
		fail, ok = l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
		if !ok {
			return false
		}
		ref, ok := l.box(ctx, container)
		if !ok {
			return false
		}
		var itab, data asm.VReg
		addr, itab, data = l.guardHeap(ctx, ref, fail)
		l.guardItab(ctx, itab, want, fail)
		dataPtr, n = l.sliceHeader(ctx, data, base)
	}
	l.guardIndex(ctx, idx, n, bounds)

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
		l.guardBoxable(ctx, result, valueFail)
	}
	if owned {
		rcBase := l.rcBase(ctx)
		rc := l.guardRC(ctx, addr, rcBase, valueFail)
		a.Emit(arm64.SUBI(rc, rc, 1))
		a.Emit(arm64.STRR(rc, rcBase, addr))
	}
	if kind == types.KindRef {
		l.retainBox(ctx, result)
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: kind, raw: raw})
	return true
}

func (l lowerer) arraySet(ctx *lowering, op jit.Step) (bool, bool) {
	if ctx.count() < 3 || ctx.values[len(ctx.values)-2].kind != types.KindI32 || ctx.values[len(ctx.values)-3].kind != types.KindRef {
		return false, false
	}
	container := ctx.values[len(ctx.values)-3]
	kind := ctx.values[len(ctx.values)-1].kind
	shape, ok := jit.ElemShapeByKind(kind)
	if !ok {
		return false, false
	}
	want, base, scale := shape.Itab, shape.Base, shape.Scale
	if op.Shape.Itab != 0 && op.Shape.Itab != want {
		return false, false
	}
	deferred := kind == types.KindRef && ctx.values[len(ctx.values)-1].backing != jit.BackingStack
	owned := container.backing == jit.BackingStack
	pre := ctx.pre()
	val := ctx.values[len(ctx.values)-1]
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-2].reg)
	a := ctx.assembler
	if ctx.hoist.live && kind != types.KindRef && !op.Terminal &&
		container.backing == jit.BackingLocal && container.slot == ctx.hoist.slot && want == ctx.hoist.want {
		bounds, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardBounds, int(op.Op))
		if !ok {
			return false, false
		}
		l.guardIndex(ctx, idx, ctx.hoist.n, bounds)
		switch kind {
		case types.KindI1, types.KindI8:
			target := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.ADD(target, ctx.hoist.dataPtr, idx))
			a.Emit(arm64.STRB(val.reg, target, 0))
		case types.KindI32, types.KindF32:
			target := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LSLI(target, idx, scale))
			a.Emit(arm64.ADD(target, ctx.hoist.dataPtr, target))
			a.Emit(arm64.STRW(val.reg, target, 0))
		case types.KindI64, types.KindF64:
			a.Emit(arm64.STRR(val.reg, ctx.hoist.dataPtr, idx))
		}
		ctx.values = ctx.values[:len(ctx.values)-3]
		return true, op.Terminal
	}
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false, false
	}
	bounds, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardBounds, int(op.Op))
	if !ok {
		return false, false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardValue, int(op.Op))
	if !ok {
		return false, false
	}
	ref, ok := l.box(ctx, container)
	if !ok {
		return false, false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, want, fail)
	dataPtr, n := l.sliceHeader(ctx, data, base)
	l.guardIndex(ctx, idx, n, bounds)
	var rcBase, rc asm.VReg
	if owned {
		rcBase = l.rcBase(ctx)
		rc = l.guardRC(ctx, addr, rcBase, valueFail)
	}
	if kind == types.KindRef {
		old := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(old, dataPtr, idx))
		l.releaseOverwritten(ctx, old, val.reg, deferred, pre, op.IP)
		if _, ok := l.own(ctx, &ctx.values[len(ctx.values)-1]); !ok {
			return false, false
		}
		if owned {
			a.Emit(arm64.SUBI(rc, rc, 1), arm64.STRR(rc, rcBase, addr))
		}
		a.Emit(arm64.STRR(val.reg, dataPtr, idx))
	} else {
		if owned {
			a.Emit(arm64.SUBI(rc, rc, 1), arm64.STRR(rc, rcBase, addr))
		}
		switch kind {
		case types.KindI1, types.KindI8:
			target := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.ADD(target, dataPtr, idx), arm64.STRB(val.reg, target, 0))
		case types.KindI32, types.KindF32:
			off := a.Reg(asm.RegTypeInt, asm.Width64)
			target := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LSLI(off, idx, scale), arm64.ADD(target, dataPtr, off), arm64.STRW(val.reg, target, 0))
		case types.KindI64, types.KindF64:
			a.Emit(arm64.STRR(val.reg, dataPtr, idx))
		}
	}
	ctx.values = ctx.values[:len(ctx.values)-3]
	if op.Terminal {
		return l.exit(ctx, op.IP+1, prof.ExitTerminalOp, int(op.Op)), true
	}
	return true, false
}

func (l lowerer) structGet(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 || ctx.values[len(ctx.values)-2].kind != types.KindRef {
		return false
	}
	if op.Shape.Itab == ctx.layout.HostStructItab {
		return l.hostGet(ctx, op)
	}
	out := op.Seen.Kind()
	switch out {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
	default:
		return false
	}
	if op.Shape.Itab != 0 && op.Shape.Itab != jit.HeapStruct {
		return false
	}
	owned := ctx.values[len(ctx.values)-2].backing == jit.BackingStack
	pre := ctx.pre()
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-2])
	if !ok {
		return false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false
	}
	bounds, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardBounds, int(op.Op))
	if !ok {
		return false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardValue, int(op.Op))
	if !ok {
		return false
	}
	kindFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardKind, int(op.Op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, jit.HeapStruct, fail)

	typ := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(typ, data, int16(structTyp)))
	if op.Shape.Typ != 0 {
		want := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(want, uint64(op.Shape.Typ))...)
		a.Emit(arm64.CMP(typ, want))
		a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	}
	fields, n := l.sliceHeader(ctx, typ, int16(fieldsSlice))
	l.guardIndex(ctx, idx, n, bounds)

	fieldOff := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(fieldOff, uint64(fieldSize))...)
	a.Emit(arm64.MUL(fieldOff, idx, fieldOff))
	field := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(field, fields, fieldOff))
	fieldKindReg := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRB(fieldKindReg, field, int16(fieldKind)))
	a.Emit(arm64.CMPI(fieldKindReg, uint16(out)))
	a.Emit(arm64.BCondLabel(arm64.OpBNE, kindFail))

	dataPtr, _ := l.sliceHeader(ctx, data, int16(structData))
	result := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(result, dataPtr, idx))
	if out == types.KindI64 {
		l.guardBoxable(ctx, result, valueFail)
	}
	if owned {
		rcBase := l.rcBase(ctx)
		rc := l.guardRC(ctx, addr, rcBase, valueFail)
		if out == types.KindRef {
			l.retainBox(ctx, result)
		}
		a.Emit(arm64.SUBI(rc, rc, 1))
		a.Emit(arm64.STRR(rc, rcBase, addr))
	} else if out == types.KindRef {
		l.retainBox(ctx, result)
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: out, raw: out != types.KindRef})
	return true
}

// hostEntry walks the compiled layout of the *HostStruct at data to the field
// the index in idx names, and returns the address of that field inside the Go
// struct. The index is bounds-guarded against the layout the view carries and
// the field's Go kind is guarded against the one the trace recorded, so the
// caller's load or store is the one that kind compiled to.
func (l lowerer) hostEntry(ctx *lowering, data, idx asm.VReg, kind reflect.Kind, bounds, kindFail asm.Label) asm.VReg {
	a := ctx.assembler
	fields, n := l.sliceHeader(ctx, data, int16(ctx.layout.HostFields))
	l.guardIndex(ctx, idx, n, bounds)

	stride := a.Reg(asm.RegTypeInt, asm.Width64)
	entry := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(stride, uint64(ctx.layout.HostFieldSize))...)
	a.Emit(arm64.MUL(stride, idx, stride), arm64.ADD(entry, fields, stride))

	conv := a.Reg(asm.RegTypeInt, asm.Width64)
	got := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(conv, entry, int16(ctx.layout.HostFieldConv)), arm64.LDRB(got, conv, int16(ctx.layout.HostConvKind)))
	a.Emit(arm64.CMPI(got, uint16(kind)), arm64.BCondLabel(arm64.OpBNE, kindFail))

	offset := a.Reg(asm.RegTypeInt, asm.Width64)
	base := a.Reg(asm.RegTypeInt, asm.Width64)
	target := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(offset, entry, int16(ctx.layout.HostFieldOffset)), arm64.LDR(base, data, int16(ctx.layout.HostPtr)), arm64.ADD(target, base, offset))
	return target
}

// hostLoad reads size bytes at target into an integer register, extending them
// as signed says, which leaves the register holding what a VM slot of that
// field's kind holds raw. hostGet settles which widths reach here.
func (l lowerer) hostLoad(ctx *lowering, size uintptr, signed bool, target asm.VReg) asm.VReg {
	a := ctx.assembler
	out := a.Reg(asm.RegTypeInt, asm.Width64)
	switch {
	case size == 1 && signed:
		raw := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRB(raw, target, 0), arm64.SXTB(out, raw))
	case size == 1:
		a.Emit(arm64.LDRB(out, target, 0))
	case size == 2 && signed:
		a.Emit(arm64.LDRSH(out, target, 0))
	case size == 2:
		a.Emit(arm64.LDRH(out, target, 0))
	case size == 4:
		a.Emit(arm64.LDRSW(out, target, 0))
	default:
		a.Emit(arm64.LDR(out, target, 0))
	}
	return out
}

// hostGet lowers STRUCT_GET against a *HostStruct. A host field holds Go memory
// rather than a VM word, so the read loads that memory in the form the field's
// conversion produces instead of reading a struct data slot.
func (l lowerer) hostGet(ctx *lowering, op jit.Step) bool {
	s, ok := jit.HostShapeByKind(op.Shape.Field)
	if !ok || s.Kind != op.Seen.Kind() {
		return false
	}
	size, signed := s.Read()
	if size == 4 && !signed {
		// Four unsigned bytes widening into an eight-byte slot would need a
		// zero-extending word load that no target this backend lowers for
		// asks for, since int and uint are eight bytes on all of them.
		return false
	}
	container := ctx.values[len(ctx.values)-2]
	owned := container.backing == jit.BackingStack
	pre := ctx.pre()
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	ref, ok := l.box(ctx, container)
	if !ok {
		return false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false
	}
	bounds, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardBounds, int(op.Op))
	if !ok {
		return false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardValue, int(op.Op))
	if !ok {
		return false
	}
	kindFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardKind, int(op.Op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, ctx.layout.HostStructItab, fail)
	target := l.hostEntry(ctx, data, idx, op.Shape.Field, bounds, kindFail)
	result := l.hostLoad(ctx, size, signed, target)
	if s.Kind == types.KindI64 {
		l.guardBoxable(ctx, result, valueFail)
	}
	if owned {
		rcBase := l.rcBase(ctx)
		rc := l.guardRC(ctx, addr, rcBase, valueFail)
		a.Emit(arm64.SUBI(rc, rc, 1), arm64.STRR(rc, rcBase, addr))
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: s.Kind, raw: true})
	return true
}

// hostSet lowers STRUCT_SET against a *HostStruct. It lowers only a field that
// is the exact image of its VM slot, because every other field decodes through
// the range check setSigned and setUnsigned perform, and a check that can fail
// belongs with the interpreter that reports it.
func (l lowerer) hostSet(ctx *lowering, op jit.Step) (bool, bool) {
	s, ok := jit.HostShapeByKind(op.Shape.Field)
	if !ok || !s.Exact() || s.Kind != ctx.values[len(ctx.values)-1].kind {
		return false, false
	}
	container := ctx.values[len(ctx.values)-3]
	owned := container.backing == jit.BackingStack
	pre := ctx.pre()
	val := ctx.values[len(ctx.values)-1]
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-2].reg)
	ref, ok := l.box(ctx, container)
	if !ok {
		return false, false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false, false
	}
	bounds, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardBounds, int(op.Op))
	if !ok {
		return false, false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardValue, int(op.Op))
	if !ok {
		return false, false
	}
	kindFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardKind, int(op.Op))
	if !ok {
		return false, false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, ctx.layout.HostStructItab, fail)
	target := l.hostEntry(ctx, data, idx, op.Shape.Field, bounds, kindFail)
	if owned {
		rcBase := l.rcBase(ctx)
		rc := l.guardRC(ctx, addr, rcBase, valueFail)
		a.Emit(arm64.SUBI(rc, rc, 1), arm64.STRR(rc, rcBase, addr))
	}
	// exact leaves three widths, so the store is total over them.
	switch s.Size {
	case 1:
		a.Emit(arm64.STRB(val.reg, target, 0))
	case 4:
		a.Emit(arm64.STRW(val.reg, target, 0))
	default:
		a.Emit(arm64.STR(val.reg, target, 0))
	}
	ctx.values = ctx.values[:len(ctx.values)-3]
	if op.Terminal {
		return l.exit(ctx, op.IP+1, prof.ExitTerminalOp, int(op.Op)), true
	}
	return true, false
}

func (l lowerer) guardBoxable(ctx *lowering, v asm.VReg, fail asm.Label) {
	ext := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SBFX(ext, v, 0, boxableWidth))
	ctx.assembler.Emit(arm64.CMP(ext, v))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
}

func (l lowerer) structSet(ctx *lowering, op jit.Step) (bool, bool) {
	if ctx.count() < 3 || ctx.values[len(ctx.values)-2].kind != types.KindI32 || ctx.values[len(ctx.values)-3].kind != types.KindRef {
		return false, false
	}
	if op.Shape.Itab == ctx.layout.HostStructItab {
		return l.hostSet(ctx, op)
	}
	kind := ctx.values[len(ctx.values)-1].kind
	switch kind {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
	default:
		return false, false
	}
	if op.Shape.Itab != 0 && op.Shape.Itab != jit.HeapStruct {
		return false, false
	}
	deferred := kind == types.KindRef && ctx.values[len(ctx.values)-1].backing != jit.BackingStack
	container := ctx.values[len(ctx.values)-3]
	owned := container.backing == jit.BackingStack
	pre := ctx.pre()
	val := ctx.values[len(ctx.values)-1]
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-2].reg)
	ref, ok := l.box(ctx, container)
	if !ok {
		return false, false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false, false
	}
	bounds, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardBounds, int(op.Op))
	if !ok {
		return false, false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardValue, int(op.Op))
	if !ok {
		return false, false
	}
	kindFail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardKind, int(op.Op))
	if !ok {
		return false, false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, jit.HeapStruct, fail)
	typ := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(typ, data, int16(structTyp)))
	if op.Shape.Typ != 0 {
		want := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(want, uint64(op.Shape.Typ))...)
		a.Emit(arm64.CMP(typ, want), arm64.BCondLabel(arm64.OpBNE, fail))
	}
	fields, n := l.sliceHeader(ctx, typ, int16(fieldsSlice))
	l.guardIndex(ctx, idx, n, bounds)
	fieldOff := a.Reg(asm.RegTypeInt, asm.Width64)
	field := a.Reg(asm.RegTypeInt, asm.Width64)
	fieldKindReg := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(fieldOff, uint64(fieldSize))...)
	a.Emit(arm64.MUL(fieldOff, idx, fieldOff), arm64.ADD(field, fields, fieldOff), arm64.LDRB(fieldKindReg, field, int16(fieldKind)), arm64.CMPI(fieldKindReg, uint16(kind)), arm64.BCondLabel(arm64.OpBNE, kindFail))
	var rcBase, rc asm.VReg
	if owned {
		rcBase = l.rcBase(ctx)
		rc = l.guardRC(ctx, addr, rcBase, valueFail)
	}
	dataPtr, _ := l.sliceHeader(ctx, data, int16(structData))
	if kind == types.KindRef {
		old := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(old, dataPtr, idx))
		l.releaseOverwritten(ctx, old, val.reg, deferred, pre, op.IP)
		if _, ok := l.own(ctx, &ctx.values[len(ctx.values)-1]); !ok {
			return false, false
		}
		if owned {
			a.Emit(arm64.SUBI(rc, rc, 1), arm64.STRR(rc, rcBase, addr))
		}
		a.Emit(arm64.STRR(val.reg, dataPtr, idx))
	} else {
		if owned {
			a.Emit(arm64.SUBI(rc, rc, 1), arm64.STRR(rc, rcBase, addr))
		}
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
	ctx.values = ctx.values[:len(ctx.values)-3]
	if op.Terminal {
		return l.exit(ctx, op.IP+1, prof.ExitTerminalOp, int(op.Op)), true
	}
	return true, false
}

// payloadGet reads a single payload word out of a guarded heap object and
// pushes it, mirroring the threaded handlers: retain a ref payload first, then
// release the consumed handle. ERROR_GET and CORO_VALUE differ only in which
// itab they guard and which word they read, so they share this.
//
// The exclusive-owner guard on the ref case exists only to protect a
// release-triggered free: with no release to follow, the container cannot
// reach zero here, so a borrowed handle skips it.
func (l lowerer) payloadGet(ctx *lowering, op jit.Step, want uintptr, offset int16) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	if op.Shape.Itab != want {
		return false
	}
	owned := ctx.values[len(ctx.values)-1].backing == jit.BackingStack
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, want, fail)

	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, data, offset))
	kind := op.Seen.Kind()
	switch kind {
	case types.KindI64:
		if !l.guardI64(ctx, dst, op.IP) {
			return false
		}
		dst = l.sign64(ctx, dst)
		if owned {
			l.releaseRef(ctx, addr, pre, op.IP)
		}
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	case types.KindRef:
		if !owned {
			l.retainBox(ctx, dst)
			ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: false})
			break
		}
		base := l.rcBase(ctx)
		rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
		ctx.assembler.Emit(arm64.CMPI(rc, 1))
		shared := ctx.assembler.Label()
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBGT, shared))
		ctx.values = append(ctx.values[:0], pre...)
		if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
			return false
		}
		ctx.assembler.Bind(shared)
		ctx.values = append(ctx.values[:0], pre...)
		l.retainBox(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.IP)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: false})
	case types.KindI32, types.KindF32, types.KindF64:
		if owned {
			l.releaseRef(ctx, addr, pre, op.IP)
		}
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	default:
		return false
	}
	return true
}

func (l lowerer) errorGet(ctx *lowering, op jit.Step) bool {
	return l.payloadGet(ctx, op, jit.HeapError, int16(errorValue))
}

// coroDone reads a coroutine handle's done flag and pushes it as an i32 (0 or
// 1). It mirrors the threaded handler: the handle ref stays owned by its stack
// slot, so no refcount changes. A constant coroutine handle is impossible, so
// a raw (unboxed constant) ref is rejected to avoid a retain side effect.
func (l lowerer) coroDone(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 1 {
		return false
	}
	if op.Shape.Itab != ctx.layout.CoroutineItab {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef || v.backing == jit.BackingConst {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, v)
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardShape, int(op.Op))
	if !ok {
		return false
	}
	_, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, ctx.layout.CoroutineItab, fail)

	done := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDRB(done, data, int16(ctx.layout.CoroutineDone)))
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: done, kind: types.KindI1, raw: true})
	return true
}

// coroValue reads a coroutine handle's last yielded or returned value. It
// mirrors the threaded handler: retain the value, then release the handle.
// The stored field is a full Boxed, so its representation matches a global
// slot (see globalGet) — scalars push raw, refs stay boxed.
func (l lowerer) coroValue(ctx *lowering, op jit.Step) bool {
	return l.payloadGet(ctx, op, ctx.layout.CoroutineItab, int16(ctx.layout.CoroutineValue))
}

// guardHeap loads a heap cell or branches to fail on a non-ref tag. Unlike
// it preserves ref because queued side exits may still need the boxed
// operand.
func (lowerer) guardHeap(ctx *lowering, ref asm.VReg, fail asm.Label) (asm.VReg, asm.VReg, asm.VReg) {
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
	a.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journal.CellHeap*8)))
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

func (l lowerer) sliceHeader(ctx *lowering, data asm.VReg, base int16) (asm.VReg, asm.VReg) {
	ptr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(ptr, data, base+sliceData), arm64.LDR(n, data, base+sliceLen))
	return ptr, n
}

// guardIndex uses one unsigned check: sign-extended negative i32 indexes are
// above any VM array or struct length.
func (lowerer) guardIndex(ctx *lowering, idx, n asm.VReg, fail asm.Label) {
	ctx.assembler.Emit(arm64.CMP(idx, n))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBCS, fail))
}

func (l lowerer) guardItab(ctx *lowering, got asm.VReg, want uintptr, fail asm.Label) {
	v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(v, uint64(want))...)
	ctx.assembler.Emit(arm64.CMP(got, v), arm64.BCondLabel(arm64.OpBNE, fail))
}
