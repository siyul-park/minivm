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

	jit[_PROLOGUE] = func(c *jitCompiler) (bool, bool) {
		c.assembler.Emits(arm64.LDI(c.scratch[rNext], uint64(c.end))...)
		return true, false
	}

	jit[_EPILOGUE] = func(c *jitCompiler) (bool, bool) {
		c.assembler.Emits(arm64.LDI(c.scratch[rNext], uint64(c.end))...)
		c.assembler.Emit(arm64.RET())
		return true, false
	}

	jit[instr.BR] = func(c *jitCompiler) (bool, bool) {
		if len(c.assembler.Returns()) > 0 {
			inst := instr.Instruction(c.code[c.ip:])
			c.ip += inst.Width()
			return false, false
		}

		offset := int(uint16(c.code[c.ip+1]) | uint16(c.code[c.ip+2])<<8)
		targetIP := c.ip + 3 + offset
		c.ip += 3

		if c.compilable[targetIP] && c.linkable(targetIP) {
			c.assembler.Emit(arm64.BLabel(c.labels[targetIP]))
		} else {
			c.assembler.Emits(arm64.LDI(c.scratch[rNext], uint64(targetIP))...)
			c.assembler.Emit(arm64.RET())
		}
		return true, true
	}

	jit[instr.BR_IF] = func(c *jitCompiler) (bool, bool) {
		if len(c.assembler.Returns()) > 1 {
			inst := instr.Instruction(c.code[c.ip:])
			c.ip += inst.Width()
			return false, false
		}
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}

		offset := int(uint16(c.code[c.ip+1]) | uint16(c.code[c.ip+2])<<8)
		targetIP := c.ip + 3 + offset
		fallIP := c.ip + 3
		c.ip += 3

		targetLink := c.compilable[targetIP] && c.linkable(targetIP)
		fallLink := c.compilable[fallIP] && c.linkable(fallIP)

		if targetLink && fallLink {
			c.assembler.Emit(arm64.CBNZLabel(r0, c.labels[targetIP]))
			c.assembler.Emit(arm64.BLabel(c.labels[fallIP]))
			return true, true
		}

		if targetLink {
			fallStubLabel := c.assembler.NewLabel()
			c.assembler.Emit(arm64.CBZLabel(r0, fallStubLabel))
			c.assembler.Emit(arm64.BLabel(c.labels[targetIP]))
			c.assembler.Bind(fallStubLabel)
			c.assembler.Emit(arm64.RET())
			return true, true
		}

		if fallLink {
			takenStubLabel := c.assembler.NewLabel()
			c.assembler.Emit(arm64.CBNZLabel(r0, takenStubLabel))
			c.assembler.Emit(arm64.BLabel(c.labels[fallIP]))
			c.assembler.Bind(takenStubLabel)
			c.assembler.Emits(arm64.LDI(c.scratch[rNext], uint64(targetIP))...)
			c.assembler.Emit(arm64.RET())
			return true, true
		}

		takenStubLabel := c.assembler.NewLabel()
		c.assembler.Emit(arm64.CBNZLabel(r0, takenStubLabel))
		c.assembler.Emit(arm64.RET())
		c.assembler.Bind(takenStubLabel)
		c.assembler.Emits(arm64.LDI(c.scratch[rNext], uint64(targetIP))...)
		c.assembler.Emit(arm64.RET())
		return true, true
	}

	jit[instr.NOP] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		return true, false
	}

	jit[instr.UNREACHABLE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		return false, false
	}

	jit[instr.DROP] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		_, ok := c.assembler.Pop()
		return ok, false
	}

	jit[instr.DUP] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Top(0)
		if !ok {
			return false, false
		}
		c.assembler.Push(r0)
		return true, false
	}

	jit[instr.SWAP] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Pop()
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Pop()
		if !ok {
			return false, false
		}
		c.assembler.Push(r0)
		c.assembler.Push(r1)
		return true, false
	}

	jit[instr.SELECT] = func(c *jitCompiler) (bool, bool) {
		c.ip++

		cond, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		val2, ok2 := c.assembler.Pop()
		val1, ok1 := c.assembler.Pop()
		if !ok1 || !ok2 {
			return false, false
		}
		if val1.Type() != val2.Type() || val1.Width() != val2.Width() {
			return false, false
		}

		result := c.assembler.NewVReg(val1.Type(), val1.Width())
		c.assembler.Push(result)

		isFloat := val1.Type() == asm.RegTypeFloat
		lTrue := c.assembler.NewLabel()
		lDone := c.assembler.NewLabel()

		c.assembler.Emit(arm64.CMPI(cond, 0))
		c.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, lTrue))

		if isFloat {
			c.assembler.Emit(arm64.FMOV(result, val2))
		} else {
			c.assembler.Emit(arm64.ADDI(result, val2, 0))
		}
		c.assembler.Emit(arm64.BLabel(lDone))

		c.assembler.Bind(lTrue)
		if isFloat {
			c.assembler.Emit(arm64.FMOV(result, val1))
		} else {
			c.assembler.Emit(arm64.ADDI(result, val1, 0))
		}
		c.assembler.Bind(lDone)

		return true, false
	}

	jit[instr.BR_TABLE] = func(c *jitCompiler) (bool, bool) {
		if len(c.assembler.Returns()) > 1 {
			inst := instr.Instruction(c.code[c.ip:])
			c.ip += inst.Width()
			return false, false
		}

		count := int(c.code[c.ip+1])
		offsets := make([]int, count+1)
		for j := 0; j <= count; j++ {
			offsets[j] = int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+j*2+2])))
		}
		c.ip += count*2 + 4

		targetIPs := make([]int, count+1)
		for j, off := range offsets {
			targetIPs[j] = c.ip + off
		}

		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}

		stubLabels := make([]int, count+1)
		for j := range stubLabels {
			stubLabels[j] = c.assembler.NewLabel()
		}

		for j := 0; j < count; j++ {
			c.assembler.Emit(arm64.CMPI(r0, uint16(j)))
			c.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, stubLabels[j]))
		}
		c.assembler.Emit(arm64.BLabel(stubLabels[count]))

		for j := 0; j <= count; j++ {
			c.assembler.Bind(stubLabels[j])
			targetIP := targetIPs[j]
			if c.compilable[targetIP] && c.linkable(targetIP) {
				c.assembler.Emit(arm64.BLabel(c.labels[targetIP]))
			} else {
				c.assembler.Emits(arm64.LDI(c.scratch[rNext], uint64(targetIP))...)
				c.assembler.Emit(arm64.RET())
			}
		}
		return true, true
	}

	jit[instr.LOCAL_GET] = func(c *jitCompiler) (bool, bool) {
		idx := int(c.code[c.ip+1])
		c.ip += 2

		typ, ok := c.local(idx)
		if !ok {
			return false, false
		}

		offset := int16(idx * 8)
		boxed := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Emit(arm64.LDR(boxed, c.scratch[rStack], offset))
		switch typ.Kind() {
		case types.KindI32:
			r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			c.assembler.Emit(arm64.UXTW(r0, boxed))
			c.assembler.Push(r0)
		case types.KindI64:
			r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			c.assembler.Emit(arm64.LSLI(r0, boxed, 64-types.VBits))
			c.assembler.Emit(arm64.ASRI(r0, r0, 64-types.VBits))
			c.assembler.Push(r0)
		case types.KindF32:
			ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			rf := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
			c.assembler.Emit(arm64.UXTW(ri, boxed))
			c.assembler.Emit(arm64.FMOV(rf, ri))
			c.assembler.Push(rf)
		case types.KindF64:
			rf := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
			c.assembler.Emit(arm64.FMOV(rf, boxed))
			c.assembler.Push(rf)
		default:
			return false, false
		}
		return true, false
	}

	jit[instr.LOCAL_SET] = func(c *jitCompiler) (bool, bool) {
		idx := int(c.code[c.ip+1])
		c.ip += 2

		typ, ok := c.local(idx)
		if !ok {
			return false, false
		}

		offset := int16(idx * 8)
		var boxed asm.VReg
		switch typ.Kind() {
		case types.KindI32:
			r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = c.boxI32(r0)
		case types.KindI64:
			r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = c.boxI64(r0)
		case types.KindF32:
			r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = c.boxF32(r0)
		case types.KindF64:
			r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = c.boxF64(r0)
		default:
			return false, false
		}
		c.assembler.Emit(arm64.STR(boxed, c.scratch[rStack], offset))
		return true, false
	}

	jit[instr.LOCAL_TEE] = func(c *jitCompiler) (bool, bool) {
		idx := int(c.code[c.ip+1])
		c.ip += 2

		typ, ok := c.local(idx)
		if !ok {
			return false, false
		}

		offset := int16(idx * 8)
		var boxed asm.VReg
		switch typ.Kind() {
		case types.KindI32:
			r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = c.boxI32(r0)
			c.assembler.Push(r0)
		case types.KindI64:
			r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = c.boxI64(r0)
			c.assembler.Push(r0)
		case types.KindF32:
			r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = c.boxF32(r0)
			c.assembler.Push(r0)
		case types.KindF64:
			r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = c.boxF64(r0)
			c.assembler.Push(r0)
		default:
			return false, false
		}
		c.assembler.Emit(arm64.STR(boxed, c.scratch[rStack], offset))
		return true, false
	}

	jit[instr.CONST_GET] = func(c *jitCompiler) (bool, bool) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.constants) {
			return false, false
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
			return false, false
		}
		return true, false
	}

	jit[instr.I32_CONST] = func(c *jitCompiler) (bool, bool) {
		val := uint32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 5
		r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r0)
		c.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		return true, false
	}

	jit[instr.I32_ADD] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ADD(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_SUB] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SUB(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_MUL] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.MUL(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_DIV_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_DIV_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_REM_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true, false
	}

	jit[instr.I32_REM_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true, false
	}

	jit[instr.I32_AND] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.AND(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_OR] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ORR(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_XOR] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.EOR(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_SHL] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSL(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_SHR_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ASR(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_SHR_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSR(r2, r1, r0))
		return true, false
	}

	jit[instr.I32_EQZ] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.CMPI(r0, 0))
		c.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		return true, false
	}

	jit[instr.I32_EQ] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true, false
	}

	jit[instr.I32_NE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true, false
	}

	jit[instr.I32_LT_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLT))
		return true, false
	}

	jit[instr.I32_LT_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true, false
	}

	jit[instr.I32_GT_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true, false
	}

	jit[instr.I32_GT_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondHI))
		return true, false
	}

	jit[instr.I32_LE_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLE))
		return true, false
	}

	jit[instr.I32_LE_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true, false
	}

	jit[instr.I32_GE_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true, false
	}

	jit[instr.I32_GE_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCS))
		return true, false
	}

	jit[instr.I32_TO_I64_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SXTW(r1, r0))
		return true, false
	}

	jit[instr.I32_TO_I64_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UXTW(r1, r0))
		return true, false
	}

	jit[instr.I32_TO_F32_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true, false
	}

	jit[instr.I32_TO_F32_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true, false
	}

	jit[instr.I32_TO_F64_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true, false
	}

	jit[instr.I32_TO_F64_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true, false
	}

	jit[instr.I64_CONST] = func(c *jitCompiler) (bool, bool) {
		val := uint64(*(*int64)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 9
		r0 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r0)
		c.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>32)&0xFFFF), 32))
		c.assembler.Emit(arm64.MOVK(r0, uint16((val>>48)&0xFFFF), 48))
		return true, false
	}

	jit[instr.I64_ADD] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ADD(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_SUB] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SUB(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_MUL] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.MUL(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_DIV_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_DIV_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_REM_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.SDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true, false
	}

	jit[instr.I64_REM_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.UDIV(r3, r1, r0))
		c.assembler.Emit(arm64.MUL(r4, r3, r0))
		c.assembler.Emit(arm64.SUB(r2, r1, r4))
		return true, false
	}

	jit[instr.I64_SHL] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSL(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_SHR_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.ASR(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_SHR_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.LSR(r2, r1, r0))
		return true, false
	}

	jit[instr.I64_EQZ] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.CMPI(r0, 0))
		c.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		return true, false
	}

	jit[instr.I64_EQ] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true, false
	}

	jit[instr.I64_NE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true, false
	}

	jit[instr.I64_LT_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLT))
		return true, false
	}

	jit[instr.I64_LT_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true, false
	}

	jit[instr.I64_GT_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true, false
	}

	jit[instr.I64_GT_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondHI))
		return true, false
	}

	jit[instr.I64_LE_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLE))
		return true, false
	}

	jit[instr.I64_LE_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true, false
	}

	jit[instr.I64_GE_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true, false
	}

	jit[instr.I64_GE_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.CMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCS))
		return true, false
	}

	jit[instr.I64_TO_I32] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UXTW(r1, r0))
		return true, false
	}

	jit[instr.I64_TO_F32_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true, false
	}

	jit[instr.I64_TO_F32_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true, false
	}

	jit[instr.I64_TO_F64_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.SCVTF(r1, r0))
		return true, false
	}

	jit[instr.I64_TO_F64_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.UCVTF(r1, r0))
		return true, false
	}

	jit[instr.F32_CONST] = func(c *jitCompiler) (bool, bool) {
		bits := *(*uint32)(unsafe.Pointer(&c.code[c.ip+1]))
		c.ip += 5
		ri := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		rf := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Emit(arm64.MOVZ(ri, uint16(bits&0xFFFF), 0))
		c.assembler.Emit(arm64.MOVK(ri, uint16((bits>>16)&0xFFFF), 16))
		c.assembler.Emit(arm64.FMOV(rf, ri))
		c.assembler.Push(rf)
		return true, false
	}

	jit[instr.F32_ADD] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FADD(r2, r1, r0))
		return true, false
	}

	jit[instr.F32_SUB] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FSUB(r2, r1, r0))
		return true, false
	}

	jit[instr.F32_MUL] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FMUL(r2, r1, r0))
		return true, false
	}

	jit[instr.F32_DIV] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FDIV(r2, r1, r0))
		return true, false
	}

	jit[instr.F32_EQ] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true, false
	}

	jit[instr.F32_NE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true, false
	}

	jit[instr.F32_LT] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true, false
	}

	jit[instr.F32_GT] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true, false
	}

	jit[instr.F32_LE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true, false
	}

	jit[instr.F32_GE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true, false
	}

	jit[instr.F32_TO_I32_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true, false
	}

	jit[instr.F32_TO_I32_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true, false
	}

	jit[instr.F32_TO_I64_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true, false
	}

	jit[instr.F32_TO_I64_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true, false
	}

	jit[instr.F32_TO_F64] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVT(r1, r0))
		return true, false
	}

	jit[instr.F64_CONST] = func(c *jitCompiler) (bool, bool) {
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
		return true, false
	}

	jit[instr.F64_ADD] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FADD(r2, r1, r0))
		return true, false
	}

	jit[instr.F64_SUB] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FSUB(r2, r1, r0))
		return true, false
	}

	jit[instr.F64_MUL] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FMUL(r2, r1, r0))
		return true, false
	}

	jit[instr.F64_DIV] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FDIV(r2, r1, r0))
		return true, false
	}

	jit[instr.F64_EQ] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		return true, false
	}

	jit[instr.F64_NE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		return true, false
	}

	jit[instr.F64_LT] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		return true, false
	}

	jit[instr.F64_GT] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		return true, false
	}

	jit[instr.F64_LE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		return true, false
	}

	jit[instr.F64_GE] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r2)
		c.assembler.Emit(arm64.FCMP(r1, r0))
		c.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		return true, false
	}

	jit[instr.F64_TO_I32_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true, false
	}

	jit[instr.F64_TO_I32_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true, false
	}

	jit[instr.F64_TO_I64_S] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZS(r1, r0))
		return true, false
	}

	jit[instr.F64_TO_I64_U] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVTZU(r1, r0))
		return true, false
	}

	jit[instr.F64_TO_F32] = func(c *jitCompiler) (bool, bool) {
		c.ip++
		r0, ok := c.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := c.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		c.assembler.Push(r1)
		c.assembler.Emit(arm64.FCVT(r1, r0))
		return true, false
	}
}

func (c *jitCompiler) boxI32(r0 asm.VReg) asm.VReg {
	payload := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	tag := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	boxed := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	c.assembler.Emit(arm64.UXTW(payload, r0))
	c.assembler.Emits(arm64.LDI(tag, types.Tag(types.KindI32))...)
	c.assembler.Emit(arm64.ORR(boxed, tag, payload))

	return boxed
}

func (c *jitCompiler) boxI64(r0 asm.VReg) asm.VReg {
	payload := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	c.assembler.Emit(arm64.LSLI(payload, r0, 64-types.VBits))
	c.assembler.Emit(arm64.ASRI(payload, payload, 64-types.VBits))

	slow := c.assembler.NewLabel()
	done := c.assembler.NewLabel()

	c.assembler.Emit(arm64.CMP(payload, r0))
	c.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, slow))

	tag := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	boxed := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	c.assembler.Emit(arm64.LSRI(payload, payload, 64-types.VBits))
	c.assembler.Emits(arm64.LDI(tag, types.Tag(types.KindI64))...)
	c.assembler.Emit(arm64.ORR(boxed, tag, payload))
	c.assembler.Emit(arm64.BLabel(done))

	c.assembler.Bind(slow)
	c.assembler.Emits(arm64.LDI(c.scratch[rNext], uint64(c.ip-2))...)
	c.assembler.Emit(arm64.RET())

	c.assembler.Bind(done)
	return boxed
}

func (c *jitCompiler) boxF32(r0 asm.VReg) asm.VReg {
	bits := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
	payload := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	tag := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	boxed := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	c.assembler.Emit(arm64.FMOV(bits, r0))
	c.assembler.Emit(arm64.UXTW(payload, bits))
	c.assembler.Emits(arm64.LDI(tag, types.Tag(types.KindF32))...)
	c.assembler.Emit(arm64.ORR(boxed, tag, payload))

	return boxed
}

func (c *jitCompiler) boxF64(r0 asm.VReg) asm.VReg {
	boxed := c.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	c.assembler.Emit(arm64.FMOV(boxed, r0))
	return boxed
}
