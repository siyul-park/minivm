//go:build arm64

package interp

import (
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"unsafe"
)

const (
	regBase  = arm64.X8
	regLimit = arm64.X15
	regCount = regLimit - regBase + 1
)

func init() {
	jit[instr.I32_CONST] = func(c *jitCompiler) error {
		val := types.BoxI32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 5

		imm0 := uint16(val & 0xFFFF)
		imm1 := uint16((val >> 16) & 0xFFFF)

		if c.rp <= regLimit {
			c.emitter.Emit32(arm64.MOVZ(c.rp, imm0, 0))
			if imm1 != 0 {
				c.emitter.Emit32(arm64.MOVK(c.rp, imm1, 16))
			}
		} else {
			spill := regBase + ((c.rp - regBase) % regCount)
			c.emitter.Emit32(arm64.STR(spill, arm64.SP, 0))
			c.emitter.Emit32(arm64.ADDI(arm64.SP, arm64.SP, 8))
		}
		c.rp++
		return nil
	}
	jit[instr.RETURN] = func(c *jitCompiler) error {
		c.ip++

		reg := regCount
		if c.returns < regCount {
			reg = c.returns
		}

		for i := 0; i < reg; i++ {
			src := c.rp - c.returns + i
			if src >= 0 {
				if src != i {
					c.emitter.Emit32(arm64.ORRW(i, src))
				}
			} else {
				offset := (i - c.returns) * 8
				c.emitter.Emit32(arm64.LDR(i, arm64.SP, offset))
			}
		}
		for i := reg; i < c.returns; i++ {
			offset := (i - reg) * 8
			c.emitter.Emit32(arm64.LDR(i, arm64.SP, offset))
		}
		if c.rp > regLimit {
			spillCount := c.rp - regLimit
			c.emitter.Emit32(arm64.ADDI(arm64.SP, arm64.SP, -8*spillCount))
		}

		c.emitter.Emit32(arm64.RET())
		return nil
	}
}
