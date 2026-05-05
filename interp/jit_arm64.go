//go:build arm64

package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

func init() {
	arch = arm64.Arch

	jit[_PROLOGUE] = func(c *jitCompiler) bool {
		return true
	}

	jit[_EPILOGUE] = func(c *jitCompiler) bool {
		c.assembler.Emit(arm64.RET())
		return true
	}

	jit[instr.NOP] = func(c *jitCompiler) bool {
		c.ip++
		return true
	}

	jit[instr.UNREACHABLE] = func(c *jitCompiler) bool {
		c.ip++
		return false
	}

	jit[instr.DROP] = func(c *jitCompiler) bool {
		c.ip++
		_, ok := c.assembler.Pop()
		return ok
	}

	jit[instr.DUP] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Top(0)
		if !ok {
			return false
		}
		c.assembler.Push(r0)
		return true
	}

	jit[instr.SWAP] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Pop()
		if !ok {
			return false
		}
		r1, ok := c.assembler.Pop()
		if !ok {
			return false
		}
		c.assembler.Push(r0)
		c.assembler.Push(r1)
		return true
	}

	jit[instr.CONST_GET] = func(c *jitCompiler) bool {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.constants) {
			return false
		}
		val := c.constants[idx]
		switch val.Kind() {
		case types.KindI32:
			v := uint32(val.I32())
			r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			c.assembler.Push(r0)
			c.assembler.Emit(arm64.MOVZ(r0, uint16(v&0xFFFF), 0))
			c.assembler.Emit(arm64.MOVK(r0, uint16((v>>16)&0xFFFF), 16))
		case types.KindI64:
			v := uint64(val.I64())
			r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			c.assembler.Push(r0)
			c.assembler.Emit(arm64.MOVZ(r0, uint16(v&0xFFFF), 0))
			c.assembler.Emit(arm64.MOVK(r0, uint16((v>>16)&0xFFFF), 16))
			c.assembler.Emit(arm64.MOVK(r0, uint16((v>>32)&0xFFFF), 32))
			c.assembler.Emit(arm64.MOVK(r0, uint16((v>>48)&0xFFFF), 48))
		case types.KindF32:
			v := *(*uint32)(unsafe.Pointer(&val))
			ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			rf := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
			c.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			c.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			c.assembler.Emit(arm64.FMOV(rf, ri))
			c.assembler.Push(rf)
		case types.KindF64:
			v := *(*uint64)(unsafe.Pointer(&val))
			ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			rf := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
			c.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			c.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			c.assembler.Emit(arm64.MOVK(ri, uint16((v>>32)&0xFFFF), 32))
			c.assembler.Emit(arm64.MOVK(ri, uint16((v>>48)&0xFFFF), 48))
			c.assembler.Emit(arm64.FMOV(rf, ri))
			c.assembler.Push(rf)
		default:
			return false
		}
		return true
	}

	jit[instr.I32_CONST] = func(c *jitCompiler) bool {
		val := uint32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 5
		r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r0)
		c.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		return true
	}

	jit[instr.I32_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ADD(r2, r1, r0))
		return true
	}

	jit[instr.I32_SUB] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SUB(r2, r1, r0))
		return true
	}

	jit[instr.I32_MUL] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.MUL(r2, r1, r0))
		return true
	}

	jit[instr.I32_DIV_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r2, r1, r0))
		return true
	}

	jit[instr.I32_DIV_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r2, r1, r0))
		return true
	}

	jit[instr.I32_REM_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true
	}

	jit[instr.I32_REM_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true
	}

	jit[instr.I32_AND] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.AND(r2, r1, r0))
		return true
	}

	jit[instr.I32_OR] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ORR(r2, r1, r0))
		return true
	}

	jit[instr.I32_XOR] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.EOR(r2, r1, r0))
		return true
	}

	jit[instr.I32_SHL] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSL(r2, r1, r0))
		return true
	}

	jit[instr.I32_SHR_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ASR(r2, r1, r0))
		return true
	}

	jit[instr.I32_SHR_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSR(r2, r1, r0))
		return true
	}

	jit[instr.I32_EQZ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.CMPI(r0, 0))
		c.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		return true
	}

	jit[instr.I32_EQ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true
	}

	jit[instr.I32_NE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true
	}

	jit[instr.I32_LT_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLT))
		return true
	}

	jit[instr.I32_LT_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true
	}

	jit[instr.I32_GT_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true
	}

	jit[instr.I32_GT_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondHI))
		return true
	}

	jit[instr.I32_LE_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLE))
		return true
	}

	jit[instr.I32_LE_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true
	}

	jit[instr.I32_GE_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true
	}

	jit[instr.I32_GE_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCS))
		return true
	}

	jit[instr.I32_TO_I64_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SXTW(r1, r0))
		return true
	}

	jit[instr.I32_TO_I64_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.MOV(r1, r0))
		return true
	}

	jit[instr.I32_TO_F32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true
	}

	jit[instr.I32_TO_F32_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true
	}

	jit[instr.I32_TO_F64_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true
	}

	jit[instr.I32_TO_F64_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true
	}

	jit[instr.I64_CONST] = func(c *jitCompiler) bool {
		val := uint64(*(*int64)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 9
		r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r0)
		c.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>32)&0xFFFF), 32))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>48)&0xFFFF), 48))
		return true
	}

	jit[instr.I64_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ADD(r2, r1, r0))
		return true
	}

	jit[instr.I64_SUB] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SUB(r2, r1, r0))
		return true
	}

	jit[instr.I64_MUL] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.MUL(r2, r1, r0))
		return true
	}

	jit[instr.I64_DIV_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r2, r1, r0))
		return true
	}

	jit[instr.I64_DIV_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r2, r1, r0))
		return true
	}

	jit[instr.I64_REM_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true
	}

	jit[instr.I64_REM_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true
	}

	jit[instr.I64_SHL] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSL(r2, r1, r0))
		return true
	}

	jit[instr.I64_SHR_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ASR(r2, r1, r0))
		return true
	}

	jit[instr.I64_SHR_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSR(r2, r1, r0))
		return true
	}

	jit[instr.I64_EQZ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.CMPI(r0, 0))
		c.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		return true
	}

	jit[instr.I64_EQ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true
	}

	jit[instr.I64_NE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true
	}

	jit[instr.I64_LT_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLT))
		return true
	}

	jit[instr.I64_LT_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true
	}

	jit[instr.I64_GT_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true
	}

	jit[instr.I64_GT_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondHI))
		return true
	}

	jit[instr.I64_LE_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLE))
		return true
	}

	jit[instr.I64_LE_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true
	}

	jit[instr.I64_GE_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true
	}

	jit[instr.I64_GE_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCS))
		return true
	}

	jit[instr.I64_TO_I32] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.MOV(r1, r0))
		return true
	}

	jit[instr.I64_TO_F32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true
	}

	jit[instr.I64_TO_F32_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true
	}

	jit[instr.I64_TO_F64_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true
	}

	jit[instr.I64_TO_F64_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true
	}

	jit[instr.F32_CONST] = func(c *jitCompiler) bool {
		bits := *(*uint32)(unsafe.Pointer(&c.code[c.ip+1]))
		c.ip += 5
		ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		rf := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Emit(arm64.MOVZ(ri, uint16(bits&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(ri, uint16((bits>>16)&0xFFFF), 16))
		c.assembler.Emit(arm64.FMOV(rf, ri))
		c.assembler.Push(rf)
		return true
	}

	jit[instr.F32_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FADD(r2, r1, r0))
		return true
	}

	jit[instr.F32_SUB] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FSUB(r2, r1, r0))
		return true
	}

	jit[instr.F32_MUL] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FMUL(r2, r1, r0))
		return true
	}

	jit[instr.F32_DIV] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FDIV(r2, r1, r0))
		return true
	}

	jit[instr.F32_EQ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true
	}

	jit[instr.F32_NE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true
	}

	jit[instr.F32_LT] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true
	}

	jit[instr.F32_GT] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true
	}

	jit[instr.F32_LE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true
	}

	jit[instr.F32_GE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true
	}

	jit[instr.F32_TO_I32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true
	}

	jit[instr.F32_TO_I32_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true
	}

	jit[instr.F32_TO_I64_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true
	}

	jit[instr.F32_TO_I64_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true
	}

	jit[instr.F32_TO_F64] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVT(r1, r0))
		return true
	}

	jit[instr.F64_CONST] = func(c *jitCompiler) bool {
		bits := *(*uint64)(unsafe.Pointer(&c.code[c.ip+1]))
		c.ip += 9
		ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		rf := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Emit(arm64.MOVZ(ri, uint16(bits&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(ri, uint16((bits>>16)&0xFFFF), 16))
		c.assembler.Emit(arm64.MOVK(ri, uint16((bits>>32)&0xFFFF), 32))
		c.assembler.Emit(arm64.MOVK(ri, uint16((bits>>48)&0xFFFF), 48))
		c.assembler.Emit(arm64.FMOV(rf, ri))
		c.assembler.Push(rf)
		return true
	}

	jit[instr.F64_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FADD(r2, r1, r0))
		return true
	}

	jit[instr.F64_SUB] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FSUB(r2, r1, r0))
		return true
	}

	jit[instr.F64_MUL] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FMUL(r2, r1, r0))
		return true
	}

	jit[instr.F64_DIV] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FDIV(r2, r1, r0))
		return true
	}

	jit[instr.F64_EQ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true
	}

	jit[instr.F64_NE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true
	}

	jit[instr.F64_LT] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true
	}

	jit[instr.F64_GT] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true
	}

	jit[instr.F64_LE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true
	}

	jit[instr.F64_GE] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true
	}

	jit[instr.F64_TO_I32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true
	}

	jit[instr.F64_TO_I32_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true
	}

	jit[instr.F64_TO_I64_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true
	}

	jit[instr.F64_TO_I64_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true
	}

	jit[instr.F64_TO_F32] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVT(r1, r0))
		return true
	}
}
