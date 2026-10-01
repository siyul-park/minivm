package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	target "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// shape lowers guard.shape. It admits only the representation and optional
// concrete type encoded by Shape; null, host, or mismatched containers deopt.
// A closure guard first deopts a word that is no reference at all, since a
// dynamic callee may hold any value.
func (m *Machine) shape(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	expected := itab(op.Shape)
	if expected == 0 {
		return false
	}
	ref, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if op.Shape.Function != 0 {
		expectRef(a, ref, s.Deopt())
	}
	addr := m.heap(a, ref)
	a.Emit(target.LDR(target.X16, addr, 0))
	expect(a, uint64(expected), s.Deopt())
	if op.Shape.Struct && op.Shape.Type != 0 {
		a.Emit(target.LDR(target.X16, addr, int16(jit.OffsetData)))
		a.Emit(target.LDR(target.X16, target.X16, int16(jit.OffsetStructTyp)))
		expect(a, uint64(op.Shape.Type), s.Deopt())
	}
	if op.Shape.Function != 0 {
		m.closure(a, addr, op.Shape, s)
	}
	m.Move(a, dst, ref)
	m.guards[op.Results[0]] = op.Shape
	return true
}

// closure deopts unless the *types.Closure at heap slot addr calls
// shape.Function, has type shape.Type, and holds at least shape.Captures
// upvals.
func (m *Machine) closure(a *asm.Assembler, addr asm.VReg, shape ssa.Shape, s compile.Site) {
	data := m.vreg()
	a.Emit(target.LDR(data, addr, int16(jit.OffsetData)))
	a.Emit(target.LDR(target.W16, data, int16(jit.OffsetClosureFn)))
	expect(a, uint64(shape.Function), s.Deopt())
	a.Emit(target.LDR(target.X16, data, int16(jit.OffsetClosureTyp)))
	expect(a, uint64(shape.Type), s.Deopt())
	if shape.Captures == 0 {
		return
	}
	a.Emit(target.LDR(target.X16, data, int16(jit.OffsetClosureUpvals+jit.OffsetSliceLen)))
	if shape.Captures <= imm12 {
		a.Emit(target.CMPI(target.X16, uint16(shape.Captures)))
	} else {
		a.Emit(target.LDI(target.X17, uint64(shape.Captures))...)
		a.Emit(target.CMP(target.X16, target.X17))
	}
	a.Emit(target.BCondLabel(target.OpBCC, s.Deopt()))
}

// itab is the itab of the concrete representation shape admits: TypedArray[T]
// for a scalar Kind, *types.Array for KindRef, *types.Struct when Struct,
// *types.Closure when Function.
func itab(shape ssa.Shape) uintptr {
	if shape.Function != 0 {
		return jit.Itab((*types.Closure)(nil))
	}
	if shape.Struct {
		return jit.Itab((*types.Struct)(nil))
	}
	switch shape.Kind {
	case types.KindI1:
		return jit.Itab(types.TypedArray[bool](nil))
	case types.KindI8:
		return jit.Itab(types.TypedArray[int8](nil))
	case types.KindI32:
		return jit.Itab(types.TypedArray[int32](nil))
	case types.KindI64:
		return jit.Itab(types.TypedArray[int64](nil))
	case types.KindF32:
		return jit.Itab(types.TypedArray[float32](nil))
	case types.KindF64:
		return jit.Itab(types.TypedArray[float64](nil))
	case types.KindRef:
		return jit.Itab((*types.Array)(nil))
	default:
		return 0
	}
}

// refIsNull tests the low word of a boxed ref for a null heap index; no
// container guard applies since it never dereferences the heap.
func refIsNull(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	src, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if src.Type() != asm.RegTypeInt || src.Width() != asm.Width64 || dst.Type() != asm.RegTypeInt || dst.Width() != asm.Width32 {
		return false
	}
	a.Emit(target.SBFX(target.X16, src, 0, 32), target.CMPI(target.X16, 0), target.CSET(dst, target.CondEQ))
	return true
}

// arrayLen loads the element count of a guarded array.
func (m *Machine) arrayLen(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok {
		return false
	}
	_, ln := m.slice(a, s.Reg(op.Args[0]), shape)
	a.Emit(target.MOVW(s.Reg(op.Results[0]), ln))
	return true
}

// arrayGet loads the element at a bounds-checked index off a guarded array.
// A ref element is retained, matching interp.(*Interpreter).arrayGet.
func (m *Machine) arrayGet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 1) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok {
		return false
	}
	ptr, ln := m.slice(a, s.Reg(op.Args[0]), shape)
	idx := m.index(a, s, s.Reg(op.Args[1]), ln)
	dst := s.Reg(op.Results[0])
	switch width(shape.Kind) {
	case 1:
		addr := m.vreg()
		a.Emit(target.ADD(addr, ptr, idx))
		if shape.Kind == types.KindI8 {
			a.Emit(target.LDRSB(dst, addr, 0))
		} else {
			a.Emit(target.LDRB(dst, addr, 0))
		}
	case 4:
		off := m.offset(a, ptr, idx, 2)
		a.Emit(target.LDR(dst, off, 0))
	default:
		off := m.offset(a, ptr, idx, 3)
		a.Emit(target.LDR(dst, off, 0))
		if shape.Kind == types.KindRef {
			m.retain(a, dst)
		}
	}
	return true
}

// arraySet writes val at a bounds-checked index into a guarded array. A ref
// element releases the old one and adopts val, matching
// interp.(*Interpreter).arraySet.
func (m *Machine) arraySet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 3, 0) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok {
		return false
	}
	ptr, ln := m.slice(a, s.Reg(op.Args[0]), shape)
	idx := m.index(a, s, s.Reg(op.Args[1]), ln)
	val := s.Reg(op.Args[2])
	switch width(shape.Kind) {
	case 1:
		addr := m.vreg()
		a.Emit(target.ADD(addr, ptr, idx))
		if shape.Kind == types.KindI1 && s.Type(op.Args[2]).Kind() != types.KindI1 {
			// A bool byte is 0 or 1: store val != 0, as threaded does.
			bit := m.vreg()
			a.Emit(target.CMPI(val, 0), target.CSET(bit, target.CondNE), target.STRB(bit, addr, 0))
		} else {
			a.Emit(target.STRB(val, addr, 0))
		}
	case 4:
		// int32 elements use STRW: array stride is 4 bytes, while generic
		// integer STR writes 8 bytes. Float32 STR is already width-correct.
		off := m.offset(a, ptr, idx, 2)
		if shape.Kind == types.KindI32 {
			a.Emit(target.STRW(val, off, 0))
		} else {
			a.Emit(target.STR(val, off, 0))
		}
	default:
		off := m.offset(a, ptr, idx, 3)
		if shape.Kind == types.KindRef {
			// A []any element is a Boxed word; box is a no-op for a ref.
			m.replace(a, s, off, box(a, s, op.Args[2]))
		} else {
			a.Emit(target.STR(val, off, 0))
		}
	}
	return true
}

// structGet loads the field at a constant index off a guarded struct. The
// shape guard pinned the struct type, so the field kind is Results[0]'s own
// static type and no bounds check applies. A ref field is retained, matching
// threaded STRUCT_GET.
func (m *Machine) structGet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 1) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok || !shape.Struct {
		return false
	}
	data := m.container(a, s.Reg(op.Args[0]))
	base := m.vreg()
	a.Emit(target.LDR(base, data, int16(jit.OffsetStructData)))
	idx := m.vreg()
	a.Emit(target.SXTW(idx, s.Reg(op.Args[1])))
	off := m.offset(a, base, idx, 3)

	dst := s.Reg(op.Results[0])
	switch s.Type(op.Results[0]).Kind() {
	case types.KindI64, types.KindF64, types.KindF32, types.KindI32:
		a.Emit(target.LDR(dst, off, 0))
	case types.KindRef:
		a.Emit(target.LDR(dst, off, 0))
		m.retain(a, dst)
	case types.KindI8:
		a.Emit(target.LDRSB(dst, off, 0))
	case types.KindI1:
		a.Emit(target.LDRB(dst, off, 0))
	default:
		return false
	}
	return true
}

// structSet writes a statically typed value to a dynamically indexed guarded
// struct. Bounds use the runtime field count; a field whose declared kind
// differs from the value's deopts, since threaded SetField converts by the
// declared kind; ref fields release the old value and adopt the new one.
func (m *Machine) structSet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 3, 0) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok || !shape.Struct {
		return false
	}
	kind := s.Type(op.Args[2]).Kind()
	if !kind.IsNumeric() && kind != types.KindRef {
		return false
	}

	data := m.container(a, s.Reg(op.Args[0]))
	typ := m.vreg()
	a.Emit(target.LDR(typ, data, int16(jit.OffsetStructTyp)))
	length := m.vreg()
	a.Emit(target.LDR(length, typ, int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)))
	idx := m.index(a, s, s.Reg(op.Args[1]), length)
	fields, row := m.vreg(), m.vreg()
	a.Emit(target.LDR(fields, typ, int16(jit.OffsetStructTypeFields)))
	a.Emit(target.LDI(target.X16, uint64(jit.SizeofStructField))...)
	a.Emit(target.MUL(row, idx, target.X16), target.ADD(row, fields, row))
	a.Emit(target.LDRB(target.X16, row, int16(jit.OffsetStructFieldKind)))
	a.Emit(target.CMPI(target.X16, uint16(kind)), target.BCondLabel(target.OpBNE, s.Deopt()))

	base := m.vreg()
	a.Emit(target.LDR(base, data, int16(jit.OffsetStructData)))
	off := m.offset(a, base, idx, 3)

	val := field(a, s, op.Args[2])
	if kind == types.KindRef {
		m.replace(a, s, off, val)
	} else {
		a.Emit(target.STR(val, off, 0))
	}
	return true
}

// container is the heap slot's data word for a guarded ref: *types.Array or
// *types.Struct, the heap's own pointer-shaped representation.
func (m *Machine) container(a *asm.Assembler, ref asm.Reg) asm.VReg {
	addr := m.heap(a, ref)
	data := m.vreg()
	a.Emit(target.LDR(data, addr, int16(jit.OffsetData)))
	return data
}

// heap is the address of ref's own heap slot: Context.Heap plus its index
// scaled by SizeofValue (a shift, since SizeofValue is a power of two).
func (m *Machine) heap(a *asm.Assembler, ref asm.Reg) asm.VReg {
	addr := m.vreg()
	a.Emit(target.SBFX(addr, ref, 0, 32), target.LSLI(addr, addr, 4))
	a.Emit(target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)))
	a.Emit(target.ADD(addr, target.X16, addr))
	return addr
}

// slice is the element pointer and length of ref's guarded array: a boxed
// slice header for a scalar Kind's TypedArray[T], or the Elems header
// embedded in *types.Array for KindRef.
func (m *Machine) slice(a *asm.Assembler, ref asm.Reg, shape ssa.Shape) (ptr, ln asm.VReg) {
	header := m.container(a, ref)
	if shape.Kind == types.KindRef {
		a.Emit(target.ADDI(header, header, uint16(jit.OffsetArrayElems)))
	}
	ptr, ln = m.vreg(), m.vreg()
	a.Emit(target.LDR(ptr, header, 0))
	a.Emit(target.LDR(ln, header, int16(jit.OffsetSliceLen)))
	return ptr, ln
}

// index sign-extends a native i32 index and deopts unless it is within
// [0, length), matching an array or struct bounds guard: one unsigned
// compare, since a negative index extends to above any length.
func (m *Machine) index(a *asm.Assembler, s compile.Site, at, length asm.Reg) asm.VReg {
	idx := m.vreg()
	a.Emit(target.SXTW(idx, at))
	a.Emit(target.CMP(idx, length), target.BCondLabel(target.OpBCS, s.Trap()))
	return idx
}

// offset is ptr advanced by idx elements of 1<<shift bytes.
func (m *Machine) offset(a *asm.Assembler, ptr, idx asm.Reg, shift uint8) asm.VReg {
	off := m.vreg()
	a.Emit(target.LSLI(off, idx, shift))
	a.Emit(target.ADD(off, ptr, off))
	return off
}

// replace stores ref word at off, then releases the word it overwrote.
func (m *Machine) replace(a *asm.Assembler, s compile.Site, off asm.Reg, word asm.Reg) {
	old := m.vreg()
	a.Emit(target.LDR(old, off, 0))
	a.Emit(target.STR(word, off, 0))
	m.release(a, old, s)
}

// field returns v widened to a struct.Data slot's 64 bits. Unlike box, an i64
// or f64 stores its raw bits, since struct.Data is not NaN-boxed.
func field(a *asm.Assembler, s compile.Site, v ssa.Value) asm.Reg {
	src, k := s.Reg(v), s.Type(v).Kind()
	switch k {
	case types.KindI64, types.KindF64, types.KindRef:
		return src
	case types.KindF32:
		a.Emit(target.FMOV(target.W16, src))
	case types.KindI8:
		// SetField stores uint64(uint32(int32(val.I8()))): SBFX into W16
		// sign-extends to 32 bits and zeroes the upper 32.
		a.Emit(target.SBFX(target.W16, src, 0, 8))
	default: // KindI1, KindI32
		a.Emit(target.UXTW(target.X16, src))
	}
	return target.X16
}

// width is the element storage width of an array admitting Kind: bool and
// int8 one byte, int32 and float32 four, int64, float64, and a ref's own
// Boxed word eight.
func width(kind types.Kind) int {
	switch kind {
	case types.KindI1, types.KindI8:
		return 1
	case types.KindI32, types.KindF32:
		return 4
	default:
		return 8
	}
}
