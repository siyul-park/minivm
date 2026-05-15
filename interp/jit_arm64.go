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

	jit[opPrologue] = func(s *jitSeg) (bool, bool) {
		s.assembler.Emits(arm64.LDI(s.scratch[rNext], uint64(s.end))...)
		return true, false
	}

	jit[opEpilogue] = func(s *jitSeg) (bool, bool) {
		s.ret(s.end)
		return true, false
	}

	jit[instr.BR] = func(s *jitSeg) (bool, bool) {
		offset := instr.ParseI16(s.code, s.ip+1)
		targetIP := s.ip + 3 + offset
		s.ip += 3

		if s.linkable(targetIP, false) {
			s.assembler.Emit(arm64.BLabel(s.labels[targetIP]))
		} else {
			s.ret(targetIP)
		}
		return true, true
	}

	jit[instr.BR_IF] = func(s *jitSeg) (bool, bool) {
		offset := instr.ParseI16(s.code, s.ip+1)
		targetIP := s.ip + 3 + offset
		fallIP := s.ip + 3
		s.ip += 3

		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}

		targetLink := s.linkable(targetIP, false)
		fallLink := s.linkable(fallIP, false)

		if targetLink && fallLink {
			s.assembler.Emit(arm64.CBNZLabel(r0, s.labels[targetIP]))
			s.assembler.Emit(arm64.BLabel(s.labels[fallIP]))
			return true, true
		}

		if targetLink {
			fallStubLabel := s.assembler.NewLabel()
			s.assembler.Emit(arm64.CBZLabel(r0, fallStubLabel))
			s.assembler.Emit(arm64.BLabel(s.labels[targetIP]))
			s.assembler.Bind(fallStubLabel)
			s.ret(fallIP)
			return true, true
		}

		if fallLink {
			takenStubLabel := s.assembler.NewLabel()
			s.assembler.Emit(arm64.CBNZLabel(r0, takenStubLabel))
			s.assembler.Emit(arm64.BLabel(s.labels[fallIP]))
			s.assembler.Bind(takenStubLabel)
			s.ret(targetIP)
			return true, true
		}

		takenStubLabel := s.assembler.NewLabel()
		s.assembler.Emit(arm64.CBNZLabel(r0, takenStubLabel))
		s.ret(fallIP)
		s.assembler.Bind(takenStubLabel)
		s.ret(targetIP)
		return true, true
	}

	jit[instr.NOP] = func(s *jitSeg) (bool, bool) {
		s.ip++
		return true, false
	}

	jit[instr.UNREACHABLE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		return false, false
	}

	jit[instr.DROP] = func(s *jitSeg) (bool, bool) {
		s.ip++
		_, ok := s.assembler.Pop()
		return ok, false
	}

	jit[instr.DUP] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Top(0)
		if !ok {
			return false, false
		}
		dst := s.assembler.NewVReg(r0.Type(), r0.Width())
		s.assembler.Emit(arm64.MOV(dst, r0))
		s.assembler.Push(dst)
		return true, false
	}

	jit[instr.SWAP] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Pop()
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Pop()
		if !ok {
			return false, false
		}
		s.assembler.Push(r0)
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.SELECT] = func(s *jitSeg) (bool, bool) {
		s.ip++

		cond, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		v2, ok2 := s.assembler.Pop()
		v1, ok1 := s.assembler.Pop()
		if !ok1 || !ok2 {
			return false, false
		}
		if v1.Type() != v2.Type() || v1.Width() != v2.Width() {
			return false, false
		}

		result := s.assembler.NewVReg(v1.Type(), v1.Width())

		s.assembler.Emit(arm64.CMPI(cond, 0))
		if v1.Type() == asm.RegTypeFloat {
			xi1 := s.assembler.NewVReg(asm.RegTypeInt, v1.Width())
			xi2 := s.assembler.NewVReg(asm.RegTypeInt, v2.Width())
			xr := s.assembler.NewVReg(asm.RegTypeInt, v1.Width())

			s.assembler.Emit(arm64.FMOV(xi1, v1))
			s.assembler.Emit(arm64.FMOV(xi2, v2))
			s.assembler.Emit(arm64.CSEL(xr, xi1, xi2, arm64.CondNE))
			s.assembler.Emit(arm64.FMOV(result, xr))
		} else {
			s.assembler.Emit(arm64.CSEL(result, v1, v2, arm64.CondNE))
		}
		s.assembler.Push(result)
		return true, false
	}
	jit[instr.BR_TABLE] = func(s *jitSeg) (bool, bool) {
		count := int(s.code[s.ip+1])
		offsets := make([]int, count+1)
		for j := 0; j <= count; j++ {
			at := s.ip + j*2 + 2
			offsets[j] = instr.ParseI16(s.code, at)
		}
		s.ip += count*2 + 4

		targetIPs := make([]int, count+1)
		for j, off := range offsets {
			targetIPs[j] = s.ip + off
		}

		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}

		stubLabels := make([]int, count+1)
		for j := range stubLabels {
			stubLabels[j] = s.assembler.NewLabel()
		}

		for j := 0; j < count; j++ {
			s.assembler.Emit(arm64.CMPI(r0, uint16(j)))
			s.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, stubLabels[j]))
		}
		s.assembler.Emit(arm64.BLabel(stubLabels[count]))

		for j := 0; j <= count; j++ {
			s.assembler.Bind(stubLabels[j])
			targetIP := targetIPs[j]
			if s.linkable(targetIP, false) {
				s.assembler.Emit(arm64.BLabel(s.labels[targetIP]))
			} else {
				s.ret(targetIP)
			}
		}
		return true, true
	}

	jit[instr.GLOBAL_GET] = func(s *jitSeg) (bool, bool) {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 3
		offset, ok := s.global(idx)
		if !ok {
			return false, false
		}
		kind, ok := s.facts[idx]
		if !ok {
			return false, false
		}
		boxed := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.LDR(boxed, s.scratch[rGlobals], offset))
		r0, ok := s.unbox64(boxed, kind)
		if !ok {
			return false, false
		}
		s.assembler.Push(r0)
		return true, false
	}

	jit[instr.GLOBAL_SET] = func(s *jitSeg) (bool, bool) {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 3
		offset, ok := s.global(idx)
		if !ok {
			return false, false
		}
		r0, ok := s.assembler.Pop()
		if !ok {
			return false, false
		}
		kind, ok := s.kind(r0)
		if !ok {
			return false, false
		}
		boxed, ok := s.box64(r0, kind, s.ip-3)
		if !ok {
			return false, false
		}

		s.assembler.Emit(arm64.STR(boxed, s.scratch[rGlobals], offset))
		s.facts[idx] = kind
		return true, false
	}

	jit[instr.GLOBAL_TEE] = func(s *jitSeg) (bool, bool) {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 3
		offset, ok := s.global(idx)
		if !ok {
			return false, false
		}
		r0, ok := s.assembler.Top(0)
		if !ok {
			return false, false
		}
		kind, ok := s.kind(r0)
		if !ok {
			return false, false
		}
		boxed, ok := s.box64(r0, kind, s.ip-3)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.STR(boxed, s.scratch[rGlobals], offset))
		s.facts[idx] = kind
		return true, false
	}

	jit[instr.LOCAL_GET] = func(s *jitSeg) (bool, bool) {
		idx := int(s.code[s.ip+1])
		s.ip += 2

		typ, ok := s.local(idx)
		if !ok {
			return false, false
		}

		offset := int16(idx * 8)
		boxed := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.LDR(boxed, s.scratch[rStack], offset))
		switch typ.Kind() {
		case types.KindI32:
			r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			s.assembler.Emit(arm64.UXTW(r0, boxed))
			s.assembler.Push(r0)
		case types.KindI64:
			r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			s.assembler.Emit(arm64.LSLI(r0, boxed, 64-types.VBits))
			s.assembler.Emit(arm64.ASRI(r0, r0, 64-types.VBits))
			s.assembler.Push(r0)
		case types.KindF32:
			ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
			s.assembler.Emit(arm64.UXTW(ri, boxed))
			s.assembler.Emit(arm64.FMOV(rf, ri))
			s.assembler.Push(rf)
		case types.KindF64:
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
			s.assembler.Emit(arm64.FMOV(rf, boxed))
			s.assembler.Push(rf)
		default:
			return false, false
		}
		return true, false
	}

	jit[instr.LOCAL_SET] = func(s *jitSeg) (bool, bool) {
		idx := int(s.code[s.ip+1])
		s.ip += 2

		typ, ok := s.local(idx)
		if !ok {
			return false, false
		}

		offset := int16(idx * 8)
		var boxed asm.VReg
		switch typ.Kind() {
		case types.KindI32:
			r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxI32(r0)
		case types.KindI64:
			r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = s.boxI64(r0, s.ip-2)
		case types.KindF32:
			r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxF32(r0)
		case types.KindF64:
			r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = s.boxF64(r0)
		default:
			return false, false
		}
		s.assembler.Emit(arm64.STR(boxed, s.scratch[rStack], offset))
		return true, false
	}

	jit[instr.LOCAL_TEE] = func(s *jitSeg) (bool, bool) {
		idx := int(s.code[s.ip+1])
		s.ip += 2

		typ, ok := s.local(idx)
		if !ok {
			return false, false
		}

		offset := int16(idx * 8)
		var boxed asm.VReg
		switch typ.Kind() {
		case types.KindI32:
			r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxI32(r0)
			s.assembler.Push(r0)
		case types.KindI64:
			r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = s.boxI64(r0, s.ip-2)
			s.assembler.Push(r0)
		case types.KindF32:
			r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxF32(r0)
			s.assembler.Push(r0)
		case types.KindF64:
			r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = s.boxF64(r0)
			s.assembler.Push(r0)
		default:
			return false, false
		}
		s.assembler.Emit(arm64.STR(boxed, s.scratch[rStack], offset))
		return true, false
	}

	jit[instr.CONST_GET] = func(s *jitSeg) (bool, bool) {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 3
		if idx < 0 || idx >= len(s.constants) {
			return false, false
		}
		val := s.constants[idx]
		switch val.Kind() {
		case types.KindI32:
			v := uint32(val.I32())
			r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			s.assembler.Emit(arm64.MOVZ(r0, uint16(v&0xFFFF), 0))
			s.assembler.Emit(arm64.MOVK(r0, uint16((v>>16)&0xFFFF), 16))
			s.assembler.Push(r0)
		case types.KindI64:
			v := uint64(val.I64())
			r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			s.assembler.Emit(arm64.MOVZ(r0, uint16(v&0xFFFF), 0))
			s.assembler.Emit(arm64.MOVK(r0, uint16((v>>16)&0xFFFF), 16))
			s.assembler.Emit(arm64.MOVK(r0, uint16((v>>32)&0xFFFF), 32))
			s.assembler.Emit(arm64.MOVK(r0, uint16((v>>48)&0xFFFF), 48))
			s.assembler.Push(r0)
		case types.KindF32:
			v := *(*uint32)(unsafe.Pointer(&val))
			ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
			s.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			s.assembler.Emit(arm64.FMOV(rf, ri))
			s.assembler.Push(rf)
		case types.KindF64:
			v := *(*uint64)(unsafe.Pointer(&val))
			ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
			s.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>32)&0xFFFF), 32))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>48)&0xFFFF), 48))
			s.assembler.Emit(arm64.FMOV(rf, ri))
			s.assembler.Push(rf)
		default:
			return false, false
		}
		return true, false
	}

	jit[instr.I32_CONST] = func(s *jitSeg) (bool, bool) {
		val := uint32(*(*int32)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 5
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Push(r0)
		s.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		return true, false
	}

	jit[instr.I32_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ADD(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SUB(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.MUL(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_DIV_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SDIV(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_DIV_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.UDIV(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_REM_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.SDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I32_REM_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.UDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I32_AND] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.AND(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_OR] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ORR(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_XOR] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.EOR(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_SHL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSL(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_SHR_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ASR(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_SHR_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSR(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_EQZ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMPI(r0, 0))
		s.assembler.Emit(arm64.CSET(r0, arm64.CondEQ))
		s.assembler.Push(r0)
		return true, false
	}

	jit[instr.I32_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondNE))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_LT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondLT))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_LT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondCC))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_GT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondGT))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_GT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondHI))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_LE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondLE))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_LE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondLS))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_GE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondGE))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_GE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondCS))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_I64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.SXTW(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_I64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.UXTW(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_CONST] = func(s *jitSeg) (bool, bool) {
		val := uint64(*(*int64)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 9
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>32)&0xFFFF), 32))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>48)&0xFFFF), 48))
		s.assembler.Push(r0)
		return true, false
	}

	jit[instr.I64_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ADD(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SUB(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.MUL(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_DIV_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SDIV(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_DIV_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.UDIV(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_REM_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.SDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_REM_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.UDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_SHL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSL(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_SHR_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ASR(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_SHR_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSR(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_EQZ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMPI(r0, 0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_LT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLT))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_LT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_GT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_GT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondHI))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_LE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLE))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_LE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_GE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_GE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCS))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.I64_TO_I32] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.UXTW(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_CONST] = func(s *jitSeg) (bool, bool) {
		bits := *(*uint32)(unsafe.Pointer(&s.code[s.ip+1]))
		s.ip += 5
		ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.MOVZ(ri, uint16(bits&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(ri, uint16((bits>>16)&0xFFFF), 16))
		s.assembler.Emit(arm64.FMOV(rf, ri))
		s.assembler.Push(rf)
		return true, false
	}

	jit[instr.F32_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FADD(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FSUB(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FMUL(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_DIV] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FDIV(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F32_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F32_LT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F32_GT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F32_LE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F32_GE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F32_TO_I32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_I32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_I64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_I64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_F64] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.FCVT(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_CONST] = func(s *jitSeg) (bool, bool) {
		bits := *(*uint64)(unsafe.Pointer(&s.code[s.ip+1]))
		s.ip += 9
		ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.MOVZ(ri, uint16(bits&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(ri, uint16((bits>>16)&0xFFFF), 16))
		s.assembler.Emit(arm64.MOVK(ri, uint16((bits>>32)&0xFFFF), 32))
		s.assembler.Emit(arm64.MOVK(ri, uint16((bits>>48)&0xFFFF), 48))
		s.assembler.Emit(arm64.FMOV(rf, ri))
		s.assembler.Push(rf)
		return true, false
	}

	jit[instr.F64_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FADD(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FSUB(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FMUL(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_DIV] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FDIV(r1, r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F64_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F64_LT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F64_GT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F64_LE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F64_GE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		s.assembler.Push(r2)
		return true, false
	}

	jit[instr.F64_TO_I32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_I32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_I64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_I64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_F32] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.assembler.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.FCVT(r1, r0))
		s.assembler.Push(r1)
		return true, false
	}
}

func (s *jitSeg) unbox64(boxed asm.VReg, kind types.Kind) (asm.VReg, bool) {
	switch kind {
	case types.KindI32:
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.UXTW(r0, boxed))
		return r0, true
	case types.KindI64:
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.LSLI(r0, boxed, 64-types.VBits))
		s.assembler.Emit(arm64.ASRI(r0, r0, 64-types.VBits))
		return r0, true
	case types.KindF32:
		ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.UXTW(ri, boxed))
		s.assembler.Emit(arm64.FMOV(rf, ri))
		return rf, true
	case types.KindF64:
		rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.FMOV(rf, boxed))
		return rf, true
	default:
		return asm.VReg{}, false
	}
}

func (s *jitSeg) box64(r0 asm.VReg, kind types.Kind, fallbackIP int) (asm.VReg, bool) {
	switch kind {
	case types.KindI32:
		if r0.Type() != asm.RegTypeInt || r0.Width() != asm.Width32 {
			return asm.VReg{}, false
		}
		return s.boxI32(r0), true
	case types.KindI64:
		if r0.Type() != asm.RegTypeInt || r0.Width() != asm.Width64 {
			return asm.VReg{}, false
		}
		return s.boxI64(r0, fallbackIP), true
	case types.KindF32:
		if r0.Type() != asm.RegTypeFloat || r0.Width() != asm.Width32 {
			return asm.VReg{}, false
		}
		return s.boxF32(r0), true
	case types.KindF64:
		if r0.Type() != asm.RegTypeFloat || r0.Width() != asm.Width64 {
			return asm.VReg{}, false
		}
		return s.boxF64(r0), true
	default:
		return asm.VReg{}, false
	}
}

func (s *jitSeg) boxI32(r0 asm.VReg) asm.VReg {
	payload := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	tag := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	boxed := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	s.assembler.Emit(arm64.UXTW(payload, r0))
	s.assembler.Emits(arm64.LDI(tag, types.Tag(types.KindI32))...)
	s.assembler.Emit(arm64.ORR(boxed, tag, payload))

	return boxed
}

func (s *jitSeg) boxI64(r0 asm.VReg, fallbackIP int) asm.VReg {
	payload := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	s.assembler.Emit(arm64.LSLI(payload, r0, 64-types.VBits))
	s.assembler.Emit(arm64.ASRI(payload, payload, 64-types.VBits))

	slow := s.assembler.NewLabel()
	done := s.assembler.NewLabel()

	s.assembler.Emit(arm64.CMP(payload, r0))
	s.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, slow))

	tag := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	boxed := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	s.assembler.Emit(arm64.LSRI(payload, payload, 64-types.VBits))
	s.assembler.Emits(arm64.LDI(tag, types.Tag(types.KindI64))...)
	s.assembler.Emit(arm64.ORR(boxed, tag, payload))
	s.assembler.Emit(arm64.BLabel(done))

	s.assembler.Bind(slow)
	s.assembler.Push(r0)
	s.ret(fallbackIP)
	s.assembler.Pop()

	s.assembler.Bind(done)
	return boxed
}

func (s *jitSeg) boxF32(r0 asm.VReg) asm.VReg {
	bits := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
	payload := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	tag := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	boxed := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)

	s.assembler.Emit(arm64.FMOV(bits, r0))
	s.assembler.Emit(arm64.UXTW(payload, bits))
	s.assembler.Emits(arm64.LDI(tag, types.Tag(types.KindF32))...)
	s.assembler.Emit(arm64.ORR(boxed, tag, payload))

	return boxed
}

func (s *jitSeg) boxF64(r0 asm.VReg) asm.VReg {
	boxed := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	s.assembler.Emit(arm64.FMOV(boxed, r0))
	return boxed
}

func (s *jitSeg) ret(nextIP int) {
	idx := s.assembler.Mark()
	stack := s.assembler.Returns(idx)

	regs := make([]asm.PReg, len(stack))
	for i, v := range stack {
		regs[i] = asm.NewPReg(uint8(i), v.Type(), v.Width())
	}

	s.assembler.Emits(arm64.LDI(s.scratch[rNext], uint64(nextIP))...)
	s.assembler.Emits(arm64.LDI(arm64.X15, arm64.Header(nil, regs, len(s.scratch)))...)
	s.assembler.Emit(arm64.RET())
}
