package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	target "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// address is a memory operand: base plus offset bytes, or, when index is
// set, base plus index scaled by the access size.
type address struct {
	base, index asm.Reg
	offset      int16
}

// cells are the itabs of the heap cells of each scalar kind: the value a
// ref.new or a wide i64 promotion stores.
var cells = map[types.Kind]uintptr{
	types.KindI1:  jit.Itab(types.I1(false)),
	types.KindI8:  jit.Itab(types.I8(0)),
	types.KindI32: jit.Itab(types.I32(0)),
	types.KindI64: jit.Itab(types.I64(0)),
	types.KindF32: jit.Itab(types.F32(0)),
	types.KindF64: jit.Itab(types.F64(0)),
}

// row is the access at p: imm's form for an offset, reg's for an index.
func (p address) row(imm func(r, base asm.Reg, offset int16) asm.Instruction, reg func(r, base, index asm.Reg) asm.Instruction, r asm.Reg) asm.Instruction {
	if p.index == nil {
		return imm(r, p.base, p.offset)
	}
	return reg(r, p.base, p.index)
}

// itab is the itab of the concrete representation shape admits: TypedArray[T]
// for a scalar Kind, *types.Array for KindRef, *types.Struct when Struct,
// *types.Closure when Function, *types.Map when Map.
func itab(shape ssa.Shape) uintptr {
	if shape.Form == ssa.FormClosure {
		return jit.Itab((*types.Closure)(nil))
	}
	if shape.Form == ssa.FormStruct {
		return jit.Itab((*types.Struct)(nil))
	}
	if shape.Form == ssa.FormMap {
		return jit.Itab((*types.Map)(nil))
	}
	switch shape.Elem {
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

// refType lowers ref.test and ref.cast of a scalar type: ref.test yields
// whether the boxed word is of the type's kind, as threaded does; ref.cast
// traps, leaving the word unchanged, when it is not. Any other type, or an
// operand that is no boxed word, is left to a bridge.
func (m *Machine) refType(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) || s.Type(op.Args[0]) != ssa.TypeRef {
		return false
	}
	if _, ok := cells[op.Shape.Elem]; !ok {
		return false
	}
	src, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if op.Op == ssa.OpRefCast {
		m.kind(a, src, op.Shape.Elem, s.Trap())
		m.Move(a, dst, src)
		return true
	}
	if dst.Type() != asm.RegTypeInt || dst.Width() != asm.Width32 {
		return false
	}
	miss, done := a.Label(), a.Label()
	m.kind(a, src, op.Shape.Elem, miss)
	a.Emit(target.LDI(dst, 1)...)
	a.Emit(target.BLabel(done))
	a.Bind(miss)
	a.Emit(target.LDI(dst, 0)...)
	a.Bind(done)
	return true
}

// kind falls through when the boxed word src is of scalar kind k as threaded
// ref.test decides it, and branches to miss otherwise: the word holds k
// inline, as types.Boxed.Kind reads it, or refers to a heap cell of k. X16
// and X17 are scratch.
func (m *Machine) kind(a *asm.Assembler, src asm.Reg, k types.Kind, miss asm.Label) {
	match := a.Label()
	a.Emit(target.LSRI(target.X16, src, types.VBits))
	if k == types.KindF64 {
		// Tops 0x3FF9..0x3FFF are the seven tagged positive NaNs; every
		// other word, BoxedNaN and the infinities included, is an f64.
		a.Emit(target.LDI(target.X17, types.Tag(types.KindF32)>>types.VBits)...)
		a.Emit(target.SUB(target.X16, target.X16, target.X17), target.CMPI(target.X16, 6), target.BCondLabel(target.OpBHI, match))
		a.Emit(target.ADD(target.X16, target.X16, target.X17))
	} else {
		a.Emit(target.LDI(target.X17, types.Tag(k)>>types.VBits)...)
		a.Emit(target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBEQ, match))
	}
	expect(a, types.Tag(types.KindRef)>>types.VBits, miss)
	heap := m.heap(a, src)
	a.Emit(target.LDR(target.X16, heap, 0))
	expect(a, uint64(cells[k]), miss)
	a.Bind(match)
}

// limit lowers guard.bounds: it deopts unless the limit is at most the
// length, signed.
func limit(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 0) {
		return false
	}
	a.Emit(target.CMP(s.Reg(op.Args[0]), s.Reg(op.Args[1])), target.BCondLabel(target.OpBGT, s.Deopt()))
	return true
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

// stride is the element storage width of an array admitting Kind: bool and
// int8 one byte, int32 and float32 four, int64, float64, and a ref's own
// Boxed word eight.
func stride(kind types.Kind) int {
	switch kind {
	case types.KindI1, types.KindI8:
		return 1
	case types.KindI32, types.KindF32:
		return 4
	default:
		return 8
	}
}
