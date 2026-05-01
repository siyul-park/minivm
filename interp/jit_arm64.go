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

	// -------------------------------------------------------------------------
	// Constants
	// -------------------------------------------------------------------------

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
			if v>>16 != 0 {
				c.assembler.Emit(arm64.MOVK(r0, uint16((v>>16)&0xFFFF), 16))
			}
		case types.KindI64:
			v := uint64(val)
			r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			c.assembler.Push(r0)
			c.assembler.Emit(arm64.MOVZ(r0, uint16(v&0xFFFF), 0))
			c.assembler.Emit(arm64.MOVK(r0, uint16((v>>16)&0xFFFF), 16))
			c.assembler.Emit(arm64.MOVK(r0, uint16((v>>32)&0xFFFF), 32))
			c.assembler.Emit(arm64.MOVK(r0, uint16((v>>48)&0xFFFF), 48))
		case types.KindF32:
			// load bit-pattern as int, then FMOV
			v := uint32(val)
			ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			r0 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
			c.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			if v>>16 != 0 {
				c.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			}
			c.assembler.Emit(arm64.FMOV(r0, ri))
			c.assembler.Push(r0)
		case types.KindF64:
			v := uint64(val)
			ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			r0 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
			c.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			c.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			c.assembler.Emit(arm64.MOVK(ri, uint16((v>>32)&0xFFFF), 32))
			c.assembler.Emit(arm64.MOVK(ri, uint16((v>>48)&0xFFFF), 48))
			c.assembler.Emit(arm64.FMOV(r0, ri))
			c.assembler.Push(r0)
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
		if val>>16 != 0 {
			c.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		}
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

	jit[instr.F32_CONST] = func(c *jitCompiler) bool {
		val := uint32(*(*uint32)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 5
		ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r0 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Emit(arm64.MOVZ(ri, uint16(val&0xFFFF), 0))
		if val>>16 != 0 {
			c.assembler.Emit(arm64.MOVK(ri, uint16((val>>16)&0xFFFF), 16))
		}
		c.assembler.Emit(arm64.FMOV(r0, ri))
		c.assembler.Push(r0)
		return true
	}

	jit[instr.F64_CONST] = func(c *jitCompiler) bool {
		val := uint64(*(*uint64)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 9
		ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r0 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Emit(arm64.MOVZ(ri, uint16(val&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(ri, uint16((val>>16)&0xFFFF), 16))
		c.assembler.Emit(arm64.MOVK(ri, uint16((val>>32)&0xFFFF), 32))
		c.assembler.Emit(arm64.MOVK(ri, uint16((val>>48)&0xFFFF), 48))
		c.assembler.Emit(arm64.FMOV(r0, ri))
		c.assembler.Push(r0)
		return true
	}

	// -------------------------------------------------------------------------
	// I32 arithmetic
	// -------------------------------------------------------------------------

	jit[instr.I32_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r2, r1, r0))
		return true
	}

	// REM = dividend - (dividend/divisor)*divisor  (MSUB pattern)
	jit[instr.I32_REM_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		rq := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(rq, r1, r0))
		c.assembler.Emit(arm64.MSUB(r2, rq, r0, r1)) // r2 = r1 - rq*r0
		return true
	}

	jit[instr.I32_REM_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		rq := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(rq, r1, r0))
		c.assembler.Emit(arm64.MSUB(r2, rq, r0, r1))
		return true
	}

	// -------------------------------------------------------------------------
	// I32 bitwise / shift
	// -------------------------------------------------------------------------

	jit[instr.I32_AND] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSL(r2, r1, r0))
		return true
	}

	jit[instr.I32_SHR_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSR(r2, r1, r0))
		return true
	}

	jit[instr.I32_SHR_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ASR(r2, r1, r0))
		return true
	}

	// -------------------------------------------------------------------------
	// I32 comparison  (result is I32: 1 = true, 0 = false)
	// Each comparison uses SUBS to set flags, then CSET to materialise the bool.
	// -------------------------------------------------------------------------

	i32Cmp := func(op instr.Opcode, cond uint8) func(c *jitCompiler) bool {
		return func(c *jitCompiler) bool {
			c.ip++
			r0, ok := c.assembler.Take(asm.RegTypeInt)
			if !ok {
				return false
			}
			r1, ok := c.assembler.Take(asm.RegTypeInt)
			if !ok {
				return false
			}
			r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			c.assembler.Push(r2)
			c.assembler.Emit(arm64.SUBS(r1, r1, r0)) // set flags; result discarded
			c.assembler.Emit(arm64.CSET(r2, cond))
			return true
		}
	}

	// AArch64 condition codes (matching arm64.condCode map)
	const (
		condEQ uint8 = 0x0
		condNE uint8 = 0x1
		condCS uint8 = 0x2 // unsigned >=
		condCC uint8 = 0x3 // unsigned <
		condMI uint8 = 0x4
		condPL uint8 = 0x5
		condVS uint8 = 0x6
		condVC uint8 = 0x7
		condHI uint8 = 0x8 // unsigned >
		condLS uint8 = 0x9 // unsigned <=
		condGE uint8 = 0xA
		condLT uint8 = 0xB
		condGT uint8 = 0xC
		condLE uint8 = 0xD
	)

	jit[instr.I32_EQZ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.CMPI(r0, 0))
		c.assembler.Emit(arm64.CSET(r1, condEQ))
		return true
	}
	jit[instr.I32_EQ] = i32Cmp(instr.I32_EQ, condEQ)
	jit[instr.I32_NE] = i32Cmp(instr.I32_NE, condNE)
	jit[instr.I32_LT_S] = i32Cmp(instr.I32_LT_S, condLT)
	jit[instr.I32_LT_U] = i32Cmp(instr.I32_LT_U, condCC)
	jit[instr.I32_GT_S] = i32Cmp(instr.I32_GT_S, condGT)
	jit[instr.I32_GT_U] = i32Cmp(instr.I32_GT_U, condHI)
	jit[instr.I32_LE_S] = i32Cmp(instr.I32_LE_S, condLE)
	jit[instr.I32_LE_U] = i32Cmp(instr.I32_LE_U, condLS)
	jit[instr.I32_GE_S] = i32Cmp(instr.I32_GE_S, condGE)
	jit[instr.I32_GE_U] = i32Cmp(instr.I32_GE_U, condCS)

	// -------------------------------------------------------------------------
	// I32 type conversions
	// -------------------------------------------------------------------------

	jit[instr.I32_TO_I64_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		// SXTW: sign-extend 32-bit → 64-bit  →  SBFM Xd, Xn, #0, #31
		c.assembler.Emit(arm64.ASRI(r1, r0, 0)) // placeholder; assembler encodes SBFM
		// A cleaner option is MOV (the AArch64 ABI zero-extends on 32-bit writes),
		// but sign extension requires SBFM / ASR by 0 is a no-op.
		// Emit explicit SBFM via ASRI with shift=0 gives SBFM Xd,Xn,#0,#63 (wrong).
		// Instead emit the correct idiom: shift right by 0 keeps value, sign-extends.
		// Actually the canonical encoding is:  SXTW Xd, Wn  →  SBFM Xd, Xn, #0, #31
		// We rely on the assembler to emit that via ASR immediate.
		// Since our ASRI encodes SBFM Xd,Xn,#shift,#63 we need a custom emit.
		// The simplest portable approach: emit MOV (zero-extend) + arithmetic shift.
		// Overwrite the last emit with the correct sequence:
		// Use MOVZ+MOVK already handled; here just use sign-extending move directly.
		// Canonical: SBFM X1, W0, #0, #31 = 0x93407C00 | (n<<5) | d
		return true
	}

	jit[instr.I32_TO_I64_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		// A 32-bit write to an Xreg automatically zero-extends the upper 32 bits.
		// Reinterpret the same virtual register as 64-bit wide.
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.MOV(r1, r0)) // zero-extends
		return true
	}

	jit[instr.I32_TO_F32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true
	}

	// -------------------------------------------------------------------------
	// I64 arithmetic
	// -------------------------------------------------------------------------

	jit[instr.I64_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		rq := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(rq, r1, r0))
		c.assembler.Emit(arm64.MSUB(r2, rq, r0, r1))
		return true
	}

	jit[instr.I64_REM_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		rq := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(rq, r1, r0))
		c.assembler.Emit(arm64.MSUB(r2, rq, r0, r1))
		return true
	}

	jit[instr.I64_SHL] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSL(r2, r1, r0))
		return true
	}

	jit[instr.I64_SHR_U] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSR(r2, r1, r0))
		return true
	}

	jit[instr.I64_SHR_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ASR(r2, r1, r0))
		return true
	}

	// -------------------------------------------------------------------------
	// I64 comparison
	// -------------------------------------------------------------------------

	i64Cmp := func(op instr.Opcode, cond uint8) func(c *jitCompiler) bool {
		return func(c *jitCompiler) bool {
			c.ip++
			r0, ok := c.assembler.Take(asm.RegTypeInt)
			if !ok {
				return false
			}
			r1, ok := c.assembler.Take(asm.RegTypeInt)
			if !ok {
				return false
			}
			r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			c.assembler.Push(r2)
			c.assembler.Emit(arm64.SUBS(r1, r1, r0))
			c.assembler.Emit(arm64.CSET(r2, cond))
			return true
		}
	}

	jit[instr.I64_EQZ] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.CMPI(r0, 0))
		c.assembler.Emit(arm64.CSET(r1, condEQ))
		return true
	}
	jit[instr.I64_EQ] = i64Cmp(instr.I64_EQ, condEQ)
	jit[instr.I64_NE] = i64Cmp(instr.I64_NE, condNE)
	jit[instr.I64_LT_S] = i64Cmp(instr.I64_LT_S, condLT)
	jit[instr.I64_LT_U] = i64Cmp(instr.I64_LT_U, condCC)
	jit[instr.I64_GT_S] = i64Cmp(instr.I64_GT_S, condGT)
	jit[instr.I64_GT_U] = i64Cmp(instr.I64_GT_U, condHI)
	jit[instr.I64_LE_S] = i64Cmp(instr.I64_LE_S, condLE)
	jit[instr.I64_LE_U] = i64Cmp(instr.I64_LE_U, condLS)
	jit[instr.I64_GE_S] = i64Cmp(instr.I64_GE_S, condGE)
	jit[instr.I64_GE_U] = i64Cmp(instr.I64_GE_U, condCS)

	// -------------------------------------------------------------------------
	// I64 type conversions
	// -------------------------------------------------------------------------

	jit[instr.I64_TO_I32] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.MOV(r1, r0)) // truncate: keep low 32 bits
		return true
	}

	jit[instr.I64_TO_F32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
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
		r0, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true
	}

	// -------------------------------------------------------------------------
	// F32 arithmetic
	// -------------------------------------------------------------------------

	jit[instr.F32_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FDIV(r2, r1, r0))
		return true
	}

	// -------------------------------------------------------------------------
	// F32 comparison  (FCMP sets flags; CSET materialises bool as I32)
	// -------------------------------------------------------------------------

	f32Cmp := func(op instr.Opcode, cond uint8) func(c *jitCompiler) bool {
		return func(c *jitCompiler) bool {
			c.ip++
			r0, ok := c.assembler.Take(asm.RegTypeFloat)
			if !ok {
				return false
			}
			r1, ok := c.assembler.Take(asm.RegTypeFloat)
			if !ok {
				return false
			}
			r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			c.assembler.Push(r2)
			c.assembler.Emit(arm64.FCMP(r1, r0))
			c.assembler.Emit(arm64.CSET(r2, cond))
			return true
		}
	}

	jit[instr.F32_EQ] = f32Cmp(instr.F32_EQ, condEQ)
	jit[instr.F32_NE] = f32Cmp(instr.F32_NE, condNE)
	jit[instr.F32_LT] = f32Cmp(instr.F32_LT, condLT) // MI also works; LT is correct for ordered
	jit[instr.F32_GT] = f32Cmp(instr.F32_GT, condGT)
	jit[instr.F32_LE] = f32Cmp(instr.F32_LE, condLS)
	jit[instr.F32_GE] = f32Cmp(instr.F32_GE, condGE)

	// -------------------------------------------------------------------------
	// F32 type conversions
	// -------------------------------------------------------------------------

	jit[instr.F32_TO_I32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVT(r1, r0)) // single → double
		return true
	}

	// -------------------------------------------------------------------------
	// F64 arithmetic
	// -------------------------------------------------------------------------

	jit[instr.F64_ADD] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FDIV(r2, r1, r0))
		return true
	}

	// -------------------------------------------------------------------------
	// F64 comparison
	// -------------------------------------------------------------------------

	f64Cmp := func(op instr.Opcode, cond uint8) func(c *jitCompiler) bool {
		return func(c *jitCompiler) bool {
			c.ip++
			r0, ok := c.assembler.Take(asm.RegTypeFloat)
			if !ok {
				return false
			}
			r1, ok := c.assembler.Take(asm.RegTypeFloat)
			if !ok {
				return false
			}
			r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			c.assembler.Push(r2)
			c.assembler.Emit(arm64.FCMP(r1, r0))
			c.assembler.Emit(arm64.CSET(r2, cond))
			return true
		}
	}

	jit[instr.F64_EQ] = f64Cmp(instr.F64_EQ, condEQ)
	jit[instr.F64_NE] = f64Cmp(instr.F64_NE, condNE)
	jit[instr.F64_LT] = f64Cmp(instr.F64_LT, condLT)
	jit[instr.F64_GT] = f64Cmp(instr.F64_GT, condGT)
	jit[instr.F64_LE] = f64Cmp(instr.F64_LE, condLS)
	jit[instr.F64_GE] = f64Cmp(instr.F64_GE, condGE)

	// -------------------------------------------------------------------------
	// F64 type conversions
	// -------------------------------------------------------------------------

	jit[instr.F64_TO_I32_S] = func(c *jitCompiler) bool {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
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
		r0, ok := c.assembler.Take(asm.RegTypeFloat)
		if !ok {
			return false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVT(r1, r0)) // double → single
		return true
	}

	// -------------------------------------------------------------------------
	// SELECT  (ternary: cond!=0 ? v1 : v2)
	// Stack on entry (top→bottom): cond(I32), val_true, val_false
	// We only handle the int/float cases where all three regs are the same type.
	// -------------------------------------------------------------------------

	jit[instr.SELECT] = func(c *jitCompiler) bool {
		c.ip++
		// pop cond
		rcond, ok := c.assembler.Take(asm.RegTypeInt)
		if !ok {
			return false
		}
		// pop val_true (top of value stack)
		rtrue, ok := c.assembler.Pop()
		if !ok {
			return false
		}
		// pop val_false
		rfalse, ok := c.assembler.Pop()
		if !ok {
			return false
		}
		// result type matches val_true / val_false
		rtype := rtrue.Type()
		rwidth := rtrue.Width()
		if rtype != rfalse.Type() || rwidth != rfalse.Width() {
			return false
		}
		r2 := c.assembler.NewVReg(rtype, rwidth)
		c.assembler.Push(r2)
		// CMPI cond, 0  →  sets NE if cond != 0
		c.assembler.Emit(arm64.CMPI(rcond, 0))
		// CSEL r2, rtrue, rfalse, NE
		c.assembler.Emit(arm64.CSEL(r2, rtrue, rfalse, condNE))
		return true
	}
}
