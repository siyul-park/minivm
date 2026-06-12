package interp

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/types"
)

func (l arm64JIT) retainBox(ctx *jitContext, v asm.VReg) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		base := l.rcBase(ctx)
		rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		l.load(ctx, rc, base, addr)
		ctx.assembler.Emit(arm64.ADDI(rc, rc, 1))
		l.store(ctx, rc, base, addr)
	})
}

func (l arm64JIT) releaseBox(ctx *jitContext, v asm.VReg, pre []asm.VReg) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		l.releaseRef(ctx, addr, pre)
	})
}

func (l arm64JIT) releaseBoxUnlessEqual(ctx *jitContext, old, val asm.VReg, pre []asm.VReg) {
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CMP(old, val))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, done))
	l.releaseBox(ctx, old, pre)
	ctx.assembler.Emit(arm64.BLabel(done))
	ctx.assembler.Bind(done)
	ctx.stack = append(ctx.stack[:0], pre...)
}

func (l arm64JIT) releaseRef(ctx *jitContext, addr asm.VReg, pre []asm.VReg) {
	base := l.rcBase(ctx)
	rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	l.load(ctx, rc, base, addr)
	ctx.assembler.Emit(arm64.CMPI(rc, 1))

	ok := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBGT, ok))
	ctx.stack = append(ctx.stack[:0], pre...)
	l.exitFallback(ctx, ctx.ip)

	ctx.assembler.Bind(ok)
	ctx.stack = append(ctx.stack[:0], pre...)
	ctx.assembler.Emit(arm64.SUBI(rc, rc, 1))
	l.store(ctx, rc, base, addr)
}

func (l arm64JIT) refOnly(ctx *jitContext, v asm.VReg, body func(asm.VReg)) {
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

func (arm64JIT) rcBase(ctx *jitContext) asm.VReg {
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalRC*8)))
	return base
}

func (l arm64JIT) upvalGet(ctx *jitContext) bool {
	idx := int(ctx.code[ctx.ip+1])
	if idx >= len(ctx.captures) {
		return false
	}

	base := l.upvalBase(ctx)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, base, int16(idx*8)))
	l.retainBox(ctx, dst)
	if ctx.captures[idx] == types.KindI64 {
		l.guardI64(ctx, dst)
	}
	ctx.stack = append(ctx.stack, dst)
	return true
}

func (l arm64JIT) upvalPut(ctx *jitContext) bool {
	idx := int(ctx.code[ctx.ip+1])
	if idx >= len(ctx.captures) {
		return false
	}
	if !l.need(ctx, 1) {
		return false
	}

	pre := append([]asm.VReg(nil), ctx.stack...)
	src := ctx.stack[len(ctx.stack)-1]
	base := l.upvalBase(ctx)
	old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(old, base, int16(idx*8)))
	l.releaseBoxUnlessEqual(ctx, old, src, pre)
	if ctx.captures[idx] == types.KindI64 {
		l.guardI64(ctx, old)
	}
	ctx.assembler.Emit(arm64.STR(src, base, int16(idx*8)))

	ctx.stack = ctx.stack[:len(ctx.stack)-1]
	return true
}

func (arm64JIT) upvalBase(ctx *jitContext) asm.VReg {
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalUpvals*8)))
	return base
}
