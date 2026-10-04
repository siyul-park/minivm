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

// limit lowers guard.bounds: it deopts unless the limit is at most the
// length, signed.
func limit(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 0) {
		return false
	}
	a.Emit(target.CMP(s.Reg(op.Args[0]), s.Reg(op.Args[1])), target.BCondLabel(target.OpBGT, s.Deopt()))
	return true
}

// row is the access at p: imm's form for an offset, reg's for an index.
func (p address) row(imm func(r, base asm.Reg, offset int16) asm.Instruction, reg func(r, base, index asm.Reg) asm.Instruction, r asm.Reg) asm.Instruction {
	if p.index == nil {
		return imm(r, p.base, p.offset)
	}
	return reg(r, p.base, p.index)
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
