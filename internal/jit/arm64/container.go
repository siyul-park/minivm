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
func (m *Machine) shape(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	expected := itab(op.Shape)
	if expected == 0 {
		return false
	}
	ref, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	heap := m.vreg()
	a.Emit(target.SBFX(heap, ref, 0, 32), target.LSLI(heap, heap, 4))
	a.Emit(target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)))
	a.Emit(target.ADD(heap, target.X16, heap))
	a.Emit(target.LDR(target.X16, heap, 0))
	a.Emit(target.LDI(target.X17, uint64(expected))...)
	a.Emit(target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, s.Deopt()))
	if op.Shape.Struct && op.Shape.Type != 0 {
		a.Emit(target.LDR(target.X16, heap, int16(jit.OffsetData)))
		a.Emit(target.LDR(target.X16, target.X16, int16(jit.OffsetStructTyp)))
		a.Emit(target.LDI(target.X17, uint64(op.Shape.Type))...)
		a.Emit(target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, s.Deopt()))
	}
	m.Move(a, dst, ref)
	m.guards[op.Results[0]] = op.Shape
	return true
}

// itab is the itab of the concrete representation shape admits: TypedArray[T]
// for a scalar Kind, *types.Array for KindRef, *types.Struct when Struct.
func itab(shape ssa.Shape) uintptr {
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
func (m *Machine) refIsNull(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
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
	if len(op.Args) != 1 || len(op.Results) != 1 {
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
	if len(op.Args) != 2 || len(op.Results) != 1 {
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
// element release the old element and adopts val, matching
// interp.(*Interpreter).arraySet.
func (m *Machine) arraySet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 3 || len(op.Results) != 0 {
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
			boxed := m.box(a, s, op.Args[2])
			old := m.vreg()
			a.Emit(target.LDR(old, off, 0))
			a.Emit(target.STR(boxed, off, 0))
			m.release(a, old, s)
		} else {
			a.Emit(target.STR(val, off, 0))
		}
	}
	return true
}

// structGet loads the field at a translate-time-constant index off a
// guarded struct: the shape guard already pinned the exact struct type, so
// the field's kind — and index bounds — are the ones the frontend resolved,
// carried here as Results[0]'s own static type. No further guard or bounds
// check applies. A ref field is retained, matching threaded STRUCT_GET.
func (m *Machine) structGet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
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
// struct. Bounds use the runtime field count; ref fields release the old
// value and adopt the new one.
func (m *Machine) structSet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 3 || len(op.Results) != 0 {
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

	base := m.vreg()
	a.Emit(target.LDR(base, data, int16(jit.OffsetStructData)))
	off := m.offset(a, base, idx, 3)

	val := m.field(a, s, op.Args[2])
	if kind == types.KindRef {
		old := m.vreg()
		a.Emit(target.LDR(old, off, 0))
		a.Emit(target.STR(val, off, 0))
		m.release(a, old, s)
	} else {
		a.Emit(target.STR(val, off, 0))
	}
	return true
}

// container is the heap slot's data word for a guarded ref: *types.Array or
// *types.Struct, the heap's own pointer-shaped representation.
func (m *Machine) container(a *asm.Assembler, ref asm.Reg) asm.VReg {
	addr := m.vreg()
	a.Emit(target.SBFX(addr, ref, 0, 32), target.LSLI(addr, addr, 4))
	a.Emit(target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)))
	a.Emit(target.ADD(addr, target.X16, addr))
	data := m.vreg()
	a.Emit(target.LDR(data, addr, int16(jit.OffsetData)))
	return data
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
// [0, length), matching an array or struct bounds guard.
func (m *Machine) index(a *asm.Assembler, s compile.Site, at, length asm.Reg) asm.VReg {
	idx := m.vreg()
	a.Emit(target.SXTW(idx, at))
	a.Emit(target.CMPI(idx, 0), target.BCondLabel(target.OpBLT, s.Deopt()))
	a.Emit(target.CMP(idx, length), target.BCondLabel(target.OpBGE, s.Deopt()))
	return idx
}

// offset is ptr advanced by idx elements of 1<<shift bytes.
func (m *Machine) offset(a *asm.Assembler, ptr, idx asm.Reg, shift uint8) asm.VReg {
	off := m.vreg()
	a.Emit(target.LSLI(off, idx, shift))
	a.Emit(target.ADD(off, ptr, off))
	return off
}

// field returns v's own kind boxed to a struct.Data slot's full 64-bit
// width: the same widening m.box applies, except an i64 or f64 lane stores
// its raw bits unmodified — struct.Data is plain 64-bit storage, not
// NaN-boxed, so a value outside the boxed inline range is still exact.
func (m *Machine) field(a *asm.Assembler, s compile.Site, v ssa.Value) asm.Reg {
	src, k := s.Reg(v), s.Type(v).Kind()
	switch k {
	case types.KindI64, types.KindF64, types.KindRef:
		return src
	case types.KindF32:
		a.Emit(target.FMOV(target.W16, src))
	case types.KindI8:
		// types.(*Struct).SetField's KindI8 case computes
		// uint64(uint32(int32(val.I8()))): sign-extend to 32 bits, then
		// zero-extend to 64. SBFX into the 32-bit W16 does exactly that in
		// one step — bits[8:32) of X16 come out sign-extended from bit 7,
		// and writing W16 zeroes X16's upper 32 bits per the ARM64 register
		// convention, matching the zero-extend without a further mask.
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
