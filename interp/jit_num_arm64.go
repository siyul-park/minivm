package interp

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/types"
)

func (l arm64Lowerer) i32Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
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
func (l arm64Lowerer) i32Bitwise(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(dst, a.reg, b.reg))
	ctx.push(value{reg: dst, kind: a.kind & b.kind, raw: true})
	return true
}

func (l arm64Lowerer) i32Divide(
	ctx *lowering,
	op step,
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
	if op.arg.Kind().Repr() == types.KindI32 {
		observed = uint64(uint32(op.arg.I32()))
	}
	if !l.guardDivisor(ctx, top, l.narrow32(b), observed, op.ip) {
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

func (l arm64Lowerer) i32Shift(
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

func (l arm64Lowerer) i32Eqz(ctx *lowering) bool {
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
func (l arm64Lowerer) i32Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMP(l.narrow32(a.reg), l.narrow32(b.reg)))
	l.setBool(ctx, cond)
	return true
}

func (l arm64Lowerer) f64Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
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

func (l arm64Lowerer) f64Cmp(ctx *lowering, cond uint8) bool {
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

func (l arm64Lowerer) i32ToF64(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
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

func (l arm64Lowerer) f64ToI32(ctx *lowering, cvt func(dst, src asm.Reg) asm.Instruction) bool {
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
func (l arm64Lowerer) f32Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
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

func (l arm64Lowerer) f32Cmp(ctx *lowering, cond uint8) bool {
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
func (l arm64Lowerer) i32ToF32(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
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

func (l arm64Lowerer) f32ToI32(ctx *lowering, cvt func(dst, src asm.Reg) asm.Instruction) bool {
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

func (l arm64Lowerer) f32ToF64(ctx *lowering) bool {
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

func (l arm64Lowerer) f64ToF32(ctx *lowering) bool {
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
func (l arm64Lowerer) i64Binary(ctx *lowering, op step, opfn func(dst, src1, src2 asm.Reg) asm.Instruction, checked bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(opfn(raw, a, b))
	if checked && !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64Divide(ctx *lowering, op step, div func(dst, src1, src2 asm.Reg) asm.Instruction, rem bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg

	top := ctx.values[len(ctx.values)-1]
	observed := uint64(0)
	if op.arg.Kind() == types.KindI64 {
		observed = uint64(op.arg.I64())
	}
	if !l.guardDivisor(ctx, top, b, observed, op.ip) {
		return false
	}

	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(div(raw, a, b))
	if rem {
		quotient := raw
		raw = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.MSUB(raw, quotient, b, a))
	}
	if !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64Shift(ctx *lowering, op step, opfn func(dst, src1, src2 asm.Reg) asm.Instruction, checked bool) bool {
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
	if checked && !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindI64)
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMP(a.reg, b.reg))
	l.setBool(ctx, cond)
	return true
}

func (l arm64Lowerer) i64Eqz(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	ctx.assembler.Emit(arm64.CMPI(a.reg, 0))
	l.setBool(ctx, arm64.CondEQ)
	return true
}

// i32ToI64 widens a raw i32 to a raw i64; the i32 range is within the
// boxable i64 range so no guard is needed.
func (l arm64Lowerer) i32ToI64(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	ext := prep(ctx, a.reg)
	ctx.push(value{reg: ext, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64ToI32(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, a.reg, maskI32))
	ctx.push(value{reg: lo, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) i64ToF64(ctx *lowering, cvtf func(dst, src asm.Reg) asm.Instruction) bool {
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

func (l arm64Lowerer) i64ToF32(ctx *lowering, cvtf func(dst, src asm.Reg) asm.Instruction) bool {
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

func (l arm64Lowerer) f32ToI64(ctx *lowering, op step, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.values[len(ctx.values)-1]
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.FMOV(fa, l.narrow32(a.reg)))
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(raw, fa))
	if !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) f64ToI64(ctx *lowering, op step, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.values[len(ctx.values)-1]
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(fa, a.reg))
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(raw, fa))
	if !l.boxableI64(ctx, raw, op.ip) {
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
func (l arm64Lowerer) countZeros(ctx *lowering, kind types.Kind, reverse bool) bool {
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
func (l arm64Lowerer) popcnt(ctx *lowering, kind types.Kind) bool {
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
func (l arm64Lowerer) rotate(ctx *lowering, op step, kind types.Kind, left bool) bool {
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
	if kind == types.KindI64 && !l.boxableI64(ctx, raw, op.ip) {
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
func (l arm64Lowerer) extend(ctx *lowering, kind types.Kind, emit func(dst, src asm.Reg) asm.Instruction) bool {
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
func (l arm64Lowerer) reinterpret(ctx *lowering, op step, from, to types.Kind) bool {
	if ctx.count() < 1 || !l.kinds(ctx, from, 1) {
		return false
	}
	if to == types.KindI64 {
		if !l.boxableI64(ctx, ctx.values[len(ctx.values)-1].reg, op.ip) {
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
func (l arm64Lowerer) f32Unary(ctx *lowering, op func(dst, src asm.Reg) asm.Instruction) bool {
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
func (l arm64Lowerer) f64Unary(ctx *lowering, op func(dst, src asm.Reg) asm.Instruction) bool {
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
func (l arm64Lowerer) copysign(ctx *lowering, kind types.Kind) bool {
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

// guardI64 deopts when v is a heap-promoted i64.
func (l arm64Lowerer) guardI64(ctx *lowering, v asm.VReg, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip)
	if !ok {
		return false
	}
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagI64>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	return true
}

// guardDivisor deopts before a divide by zero. When trace recorded a non-zero
// divisor, guardRaw owns the mismatch exit; otherwise the zero check protects
// the native divide itself.
func (l arm64Lowerer) guardDivisor(ctx *lowering, divisor value, reg asm.VReg, observed uint64, ip int) bool {
	guarded := false
	if !divisor.known && observed != 0 {
		if !l.guardRaw(ctx, reg, observed, ip) {
			return false
		}
		guarded = true
	}
	if divisor.known && divisor.imm != 0 || guarded {
		return true
	}
	fail, ok := l.sideExit(ctx, ctx.values, ip)
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMPI(reg, 0))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, fail))
	return true
}

// boxableI64 keeps raw i64 values within the boxed 49-bit lane.
func (l arm64Lowerer) boxableI64(ctx *lowering, raw asm.VReg, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip)
	if !ok {
		return false
	}
	l.guardBoxable(ctx, raw, fail)
	return true
}

func (l arm64Lowerer) guardBoxable(ctx *lowering, v asm.VReg, fail asm.Label) {
	ext := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SBFX(ext, v, 0, boxableWidth))
	ctx.assembler.Emit(arm64.CMP(ext, v))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
}

// guardRaw keeps observed narrow inputs speculative: a different runtime value
// exits before the opcode, so the threaded handler owns the general case.
func (l arm64Lowerer) guardRaw(ctx *lowering, got asm.VReg, val uint64, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip)
	if !ok {
		return false
	}
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, val)...)
	if got.Width() == asm.Width32 {
		want = l.narrow32(want)
	}
	ctx.assembler.Emit(arm64.CMP(got, want))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	return true
}

// sign64 sign-extends the 49-bit value lane of a boxed i64 to a full raw
// i64 value.
func (l arm64Lowerer) sign64(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SBFX(out, v, 0, boxableWidth))
	return out
}
