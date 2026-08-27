package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"
)

func (l lowerer) zero32(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(out, v, maskI32))
	return out
}

func (l lowerer) i32Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(dst, a.reg, b.reg))
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

// i32Bitwise lowers a width-closed bitwise op (and/or/xor). Operands are
// accepted by representation, so i1/i8 flow in like i32; the result keeps a
// shared narrow kind (i8&i8 → i8, i1^i1 → i1) and widens to i32 only for a
// mixed pair. The op runs on the full register; the low 32 bits carry the value
// and box masks the rest.
func (l lowerer) i32Bitwise(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(dst, a.reg, b.reg))
	ctx.push(value{reg: dst, kind: a.kind & b.kind, raw: true})
	return true
}

func (l lowerer) i32Divide(
	ctx *lowering,
	op jit.Step,
	div func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*lowering, asm.VReg) asm.VReg,
	rem bool,
) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI32, 2) {
		return false
	}
	b := prep(ctx, ctx.values[len(ctx.values)-1].reg)
	a := prep(ctx, ctx.values[len(ctx.values)-2].reg)

	top := ctx.values[len(ctx.values)-1]
	observed := uint64(0)
	if op.Arg.Kind().Repr() == types.KindI32 {
		observed = uint64(uint32(op.Arg.I32()))
	}
	if !l.guardDivisor(ctx, top, l.narrow32(b), observed, op.IP) {
		return false
	}

	ctx.pop()
	ctx.pop()
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(div(dst, a, b))
	if rem {
		quotient := dst
		dst = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.MSUB(dst, quotient, b, a))
	}
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

func (l lowerer) i32Shift(
	ctx *lowering,
	shiftOp func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*lowering, asm.VReg) asm.VReg,
) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI32, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1]
	a := ctx.values[len(ctx.values)-2]
	shift := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if b.known {
		ctx.assembler.Emit(arm64.LDI(shift, uint64(uint32(b.imm)&0x1F))...)
	} else {
		ctx.assembler.Emit(arm64.ANDI(shift, b.reg, 0x1F))
	}
	val := prep(ctx, a.reg)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(shiftOp(dst, val, shift))
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

func (l lowerer) i32Eqz(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	ctx.assembler.Emit(arm64.CMPI(l.narrow32(a.reg), 0))
	l.setBool(ctx, arm64.CondEQ)
	return true
}

// i32Cmp compares the 32-bit lanes through W-register views: raw upper
// bits never participate, so signed and unsigned conditions both read correct
// flags from the 32-bit subtraction.
func (l lowerer) i32Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMP(l.narrow32(a.reg), l.narrow32(b.reg)))
	l.setBool(ctx, cond)
	return true
}

func (l lowerer) f64Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindF64)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, a.reg),
		arm64.FMOV(fb, b.reg),
		op(fr, fa, fb),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l lowerer) f64Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindF64)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, a.reg),
		arm64.FMOV(fb, b.reg),
		arm64.FCMP(fa, fb),
	)
	l.setBool(ctx, cond)
	return true
}

func (l lowerer) i32ToF64(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	val := prep(ctx, a.reg)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.SCVTF(fr, val))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l lowerer) f64ToI32(ctx *lowering, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(fa, a.reg))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(dst, fa))
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

// f32Binary lowers an f32 arithmetic opcode. A raw f32 keeps its float
// bits in the low 32 of an int register, so both inputs unbox with a 32-bit
// FMOV and the result moves back untagged — box tags it at a boundary.
func (l lowerer) f32Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindF32)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		arm64.FMOV(fb, l.narrow32(b.reg)),
		op(fr, fa, fb),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

func (l lowerer) f32Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindF32)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		arm64.FMOV(fb, l.narrow32(b.reg)),
		arm64.FCMP(fa, fb),
	)
	l.setBool(ctx, cond)
	return true
}

// i32ToF32 converts a raw i32 to a raw f32. prep sign- or zero-extends
// the value lane; SCVTF over the extended 64-bit value is correct for both
// signed and (non-negative, zero-extended) unsigned sources.
func (l lowerer) i32ToF32(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	val := prep(ctx, a.reg)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.SCVTF(fr, val))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

func (l lowerer) f32ToI32(ctx *lowering, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.FMOV(fa, l.narrow32(a.reg)))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(dst, fa))
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

func (l lowerer) f32ToF64(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fd := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		arm64.FCVT(fd, fa),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fd))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l lowerer) f64ToF32(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.pop()
	fd := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fs := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fd, a.reg),
		arm64.FCVT(fs, fd),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fs))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

// i64Binary lowers an i64 arithmetic opcode. Raw i64 is the full signed
// value, so the op runs directly on 64-bit registers; checked ops guard that
// the result still fits the boxable range and deopt with the operands intact
// when it overflows, so the interpreter handles the heap promotion.
func (l lowerer) i64Binary(ctx *lowering, op jit.Step, opfn func(dst, src1, src2 asm.Reg) asm.Instruction, checked bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(opfn(raw, a, b))
	if checked && !l.boxableI64(ctx, raw, op.IP) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l lowerer) i64Divide(ctx *lowering, op jit.Step, div func(dst, src1, src2 asm.Reg) asm.Instruction, rem bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg

	top := ctx.values[len(ctx.values)-1]
	observed := uint64(0)
	if op.Arg.Kind() == types.KindI64 {
		observed = uint64(op.Arg.I64())
	}
	if !l.guardDivisor(ctx, top, b, observed, op.IP) {
		return false
	}

	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(div(raw, a, b))
	if rem {
		quotient := raw
		raw = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.MSUB(raw, quotient, b, a))
	}
	if !l.boxableI64(ctx, raw, op.IP) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l lowerer) i64Shift(ctx *lowering, op jit.Step, opfn func(dst, src1, src2 asm.Reg) asm.Instruction, checked bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg
	shift := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if ctx.values[len(ctx.values)-1].known {
		ctx.assembler.Emit(arm64.LDI(shift, uint64(ctx.values[len(ctx.values)-1].imm)&0x3F)...)
	} else {
		ctx.assembler.Emit(arm64.ANDI(shift, b, 0x3F))
	}
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(opfn(raw, a, shift))
	if checked && !l.boxableI64(ctx, raw, op.IP) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l lowerer) i64Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindI64)
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMP(a.reg, b.reg))
	l.setBool(ctx, cond)
	return true
}

// operands pops a typed binary-op pair after checking both kinds.
func (l lowerer) operands(ctx *lowering, kind types.Kind) (value, value, bool) {
	if ctx.count() < 2 || !l.kinds(ctx, kind, 2) {
		return value{}, value{}, false
	}
	b := ctx.pop()
	a := ctx.pop()
	return b, a, true
}

func (l lowerer) i64Eqz(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	ctx.assembler.Emit(arm64.CMPI(a.reg, 0))
	l.setBool(ctx, arm64.CondEQ)
	return true
}

// setBool pushes a comparison/test result as i1: every caller is an
// eqz/eq/lt/.../is_null/test whose result kind is i1 (matching the interpreter,
// which boxes these through BoxI1). The 0/1 flag still satisfies any later
// i32 operand because kinds compares by representation.
func (l lowerer) setBool(ctx *lowering, cond uint8) {
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))
	ctx.push(value{reg: flag, kind: types.KindI1, raw: true})
}

// i32ToI64 widens a raw i32 to a raw i64; the i32 range is within the
// boxable i64 range so no guard is needed.
func (l lowerer) i32ToI64(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	ext := prep(ctx, a.reg)
	ctx.push(value{reg: ext, kind: types.KindI64, raw: true})
	return true
}

func (l lowerer) i64ToI32(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, a.reg, maskI32))
	ctx.push(value{reg: lo, kind: types.KindI32, raw: true})
	return true
}

func (l lowerer) i64ToF64(ctx *lowering, cvtf func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(cvtf(fr, a.reg))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l lowerer) i64ToF32(ctx *lowering, cvtf func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(cvtf(fr, a.reg))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

func (l lowerer) f32ToI64(ctx *lowering, op jit.Step, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.values[len(ctx.values)-1]
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.FMOV(fa, l.narrow32(a.reg)))
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(raw, fa))
	if !l.boxableI64(ctx, raw, op.IP) {
		return false
	}
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l lowerer) f64ToI64(ctx *lowering, op jit.Step, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.values[len(ctx.values)-1]
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(fa, a.reg))
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(raw, fa))
	if !l.boxableI64(ctx, raw, op.IP) {
		return false
	}
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

// countZeros lowers CLZ (reverse=false) or CTZ (reverse=true, via RBIT then
// CLZ) for an integer kind. The count is always in [0, width] so the result is
// boxable without a guard. i32 operates on the W view so the upper lane is
// ignored.
func (l lowerer) countZeros(ctx *lowering, kind types.Kind, reverse bool) bool {
	if ctx.count() < 1 || !l.kinds(ctx, kind, 1) {
		return false
	}
	a := ctx.pop()
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	src, out := a.reg, dst
	if kind == types.KindI32 {
		src, out = l.narrow32(a.reg), l.narrow32(dst)
	}
	if reverse {
		ctx.assembler.Emit(arm64.RBIT(out, src))
		ctx.assembler.Emit(arm64.CLZ(out, out))
	} else {
		ctx.assembler.Emit(arm64.CLZ(out, src))
	}
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// popcnt lowers a population count through the SIMD pipe (FMOV → CNT → ADDV →
// FMOV); ARMv8.0 has no scalar GPR popcount. The result is small and boxable.
// i32 masks the upper lane so CNT counts only the 32-bit value.
func (l lowerer) popcnt(ctx *lowering, kind types.Kind) bool {
	if ctx.count() < 1 || !l.kinds(ctx, kind, 1) {
		return false
	}
	a := ctx.pop()
	src := a.reg
	if kind == types.KindI32 {
		src = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ANDI(src, a.reg, maskI32))
	}
	v := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(v, src),
		arm64.CNT(v, v),
		arm64.ADDV(v, v),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, v))
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// rotate lowers ROTL (left=true) or ROTR for an integer kind via ROR. ROTL is
// ROR by the negated amount; the rotate width follows the register view (W for
// i32, X for i64). An i64 rotate of the full 64-bit value can leave the boxable
// range, so it guards before pushing; i32 always fits.
func (l lowerer) rotate(ctx *lowering, op jit.Step, kind types.Kind, left bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, kind, 2) {
		return false
	}
	src := ctx.values[len(ctx.values)-2].reg
	amount := ctx.values[len(ctx.values)-1].reg
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	out := raw
	if kind == types.KindI32 {
		src, amount, out = l.narrow32(src), l.narrow32(amount), l.narrow32(raw)
	}
	if left {
		neg := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		if kind == types.KindI32 {
			neg = l.narrow32(neg)
		}
		ctx.assembler.Emit(arm64.NEG(neg, amount))
		amount = neg
	}
	ctx.assembler.Emit(arm64.ROR(out, src, amount))
	if kind == types.KindI64 && !l.boxableI64(ctx, raw, op.IP) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: kind, raw: true})
	return true
}

// extend lowers a sign-extend op (SXTB/SXTH/SXTW). The 64-bit form is correct
// for both kinds: it reads only the low byte/half/word and the sign-extended
// result stays within the boxable i64 range, so no guard is needed.
func (l lowerer) extend(ctx *lowering, kind types.Kind, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, kind, 1) {
		return false
	}
	a := ctx.pop()
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(emit(dst, a.reg))
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// reinterpret reinterprets the raw bits of the top value as another kind. The
// i32/f32 pair and the i64→f64 direction share their register representation,
// so only the kind changes. Reading an f64 bit pattern as i64 can leave the
// boxable range, so that direction guards first.
func (l lowerer) reinterpret(ctx *lowering, op jit.Step, from, to types.Kind) bool {
	if ctx.count() < 1 || !l.kinds(ctx, from, 1) {
		return false
	}
	if to == types.KindI64 {
		if !l.boxableI64(ctx, ctx.values[len(ctx.values)-1].reg, op.IP) {
			return false
		}
	}
	a := ctx.pop()
	ctx.push(value{reg: a.reg, kind: to, raw: true})
	return true
}

// f32Unary lowers a single-operand f32 op. The raw f32 keeps its bits in the
// low 32 of an int register, so it unboxes with a 32-bit FMOV and the result
// moves back untagged.
func (l lowerer) f32Unary(ctx *lowering, op func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		op(fr, fa),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

// f64Unary lowers a single-operand f64 op. A raw f64 is its own bit pattern.
func (l lowerer) f64Unary(ctx *lowering, op func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, a.reg),
		op(fr, fa),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

// copysign splices the sign bit of the top operand onto the magnitude of the
// one below it with GPR bit ops, matching math.Copysign(magnitude, sign). The
// raw float bits already live in int registers, so no FP move is needed.
func (l lowerer) copysign(ctx *lowering, kind types.Kind) bool {
	if ctx.count() < 2 || !l.kinds(ctx, kind, 2) {
		return false
	}
	sign := ctx.pop()
	magnitude := ctx.pop()
	mask := uint64(0x8000_0000_0000_0000)
	if kind == types.KindF32 {
		mask = 0x8000_0000
	}
	signbit := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(signbit, mask)...)
	notSign := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(notSign, ^mask)...)
	s := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.AND(s, sign.reg, signbit))
	m := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.AND(m, magnitude.reg, notSign))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ORR(dst, m, s))
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// kinds reports whether the top n operands are all raw and computable as kind.
// The match is by representation, so the narrow integer kinds (i1, i8) satisfy
// an i32 operand exactly as they do in the interpreter; for every other kind
// Repr is the identity, so the check stays exact.
func (l lowerer) kinds(ctx *lowering, kind types.Kind, n int) bool {
	for k := 0; k < n; k++ {
		v := ctx.values[len(ctx.values)-1-k]
		if v.kind.Repr() != kind.Repr() || !v.raw {
			return false
		}
	}
	return true
}

func (lowerer) narrow32(v asm.VReg) asm.VReg {
	return asm.NewVReg(v.ID(), v.Type(), asm.Width32)
}

func (l lowerer) sign32(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SXTW(out, v))
	return out
}
