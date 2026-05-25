//go:build arm64

package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Layout offsets for native CALL/RETURN. Resolved once at init from the
// live struct definitions so a future field reorder cannot silently
// corrupt frame state.
var (
	offInterpFP      uintptr
	offInterpSP      uintptr
	offInterpFR      uintptr
	offInterpFrames  uintptr
	offInterpStack   uintptr
	offInterpHeap    uintptr
	offInterpGlobals uintptr
	offInterpCode    uintptr
	sizeofFrame      uintptr
	offFrameCode     uintptr
	offFrameAddr     uintptr
	offFrameIP       uintptr
	offFrameBP       uintptr
	offFrameReturns  uintptr
	offFrameRelease  uintptr
)

func init() {
	arch = arm64.Arch
	initLayout()

	jitPrologue = func(s *jitSeg) {
		s.assembler.Emits(arm64.LDI(s.scratch[rNext], uint64(s.end))...)
	}

	jitEpilogue = func(s *jitSeg) {
		s.ret(s.end)
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

		cond, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}

		targetLink := s.linkable(targetIP, false)
		fallLink := s.linkable(fallIP, false)

		switch {
		case targetLink && fallLink:
			s.assembler.Emit(arm64.CBNZLabel(cond, s.labels[targetIP]))
			s.assembler.Emit(arm64.BLabel(s.labels[fallIP]))
		case targetLink:
			stub := s.assembler.NewLabel()
			s.assembler.Emit(arm64.CBZLabel(cond, stub))
			s.assembler.Emit(arm64.BLabel(s.labels[targetIP]))
			s.assembler.Bind(stub)
			s.ret(fallIP)
		case fallLink:
			stub := s.assembler.NewLabel()
			s.assembler.Emit(arm64.CBNZLabel(cond, stub))
			s.assembler.Emit(arm64.BLabel(s.labels[fallIP]))
			s.assembler.Bind(stub)
			s.ret(targetIP)
		default:
			stub := s.assembler.NewLabel()
			s.assembler.Emit(arm64.CBNZLabel(cond, stub))
			s.ret(fallIP)
			s.assembler.Bind(stub)
			s.ret(targetIP)
		}
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

	jit[instr.CALL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		return s.emitCall()
	}

	jit[instr.RETURN] = func(s *jitSeg) (bool, bool) {
		s.ip++
		return s.emitReturn()
	}

	jit[instr.DROP] = func(s *jitSeg) (bool, bool) {
		s.ip++
		_, ok := s.Pop()
		return ok, false
	}

	jit[instr.DUP] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Top(0)
		if !ok {
			return false, false
		}
		dst := s.assembler.NewVReg(r0.Type(), r0.Width())
		s.assembler.Emit(arm64.MOV(dst, r0))
		s.Push(dst)
		return true, false
	}

	jit[instr.SWAP] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Pop()
		if !ok {
			return false, false
		}
		r1, ok := s.Pop()
		if !ok {
			return false, false
		}
		s.Push(r0)
		s.Push(r1)
		return true, false
	}

	jit[instr.SELECT] = func(s *jitSeg) (bool, bool) {
		s.ip++

		cond, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		v2, ok2 := s.Pop()
		v1, ok1 := s.Pop()
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
		s.Push(result)
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

		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
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
			if s.linkable(targetIPs[j], false) {
				s.assembler.Emit(arm64.BLabel(s.labels[targetIPs[j]]))
			} else {
				s.ret(targetIPs[j])
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
		s.Push(r0)
		return true, false
	}

	jit[instr.GLOBAL_SET] = func(s *jitSeg) (bool, bool) {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 3
		offset, ok := s.global(idx)
		if !ok {
			return false, false
		}
		r0, ok := s.Pop()
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
		r0, ok := s.Top(0)
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
			s.Push(r0)
		case types.KindI64:
			r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			s.assembler.Emit(arm64.LSLI(r0, boxed, 64-types.VBits))
			s.assembler.Emit(arm64.ASRI(r0, r0, 64-types.VBits))
			s.Push(r0)
		case types.KindF32:
			ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
			s.assembler.Emit(arm64.UXTW(ri, boxed))
			s.assembler.Emit(arm64.FMOV(rf, ri))
			s.Push(rf)
		case types.KindF64:
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
			s.assembler.Emit(arm64.FMOV(rf, boxed))
			s.Push(rf)
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
			r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxI32(r0)
		case types.KindI64:
			r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = s.boxI64(r0, s.ip-2)
		case types.KindF32:
			r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxF32(r0)
		case types.KindF64:
			r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
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
			r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxI32(r0)
			s.Push(r0)
		case types.KindI64:
			r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = s.boxI64(r0, s.ip-2)
			s.Push(r0)
		case types.KindF32:
			r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
			if !ok {
				return false, false
			}
			boxed = s.boxF32(r0)
			s.Push(r0)
		case types.KindF64:
			r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
			if !ok {
				return false, false
			}
			boxed = s.boxF64(r0)
			s.Push(r0)
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
			s.Push(r0)
		case types.KindI64:
			v := uint64(val.I64())
			r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			s.assembler.Emit(arm64.MOVZ(r0, uint16(v&0xFFFF), 0))
			s.assembler.Emit(arm64.MOVK(r0, uint16((v>>16)&0xFFFF), 16))
			s.assembler.Emit(arm64.MOVK(r0, uint16((v>>32)&0xFFFF), 32))
			s.assembler.Emit(arm64.MOVK(r0, uint16((v>>48)&0xFFFF), 48))
			s.Push(r0)
		case types.KindF32:
			v := *(*uint32)(unsafe.Pointer(&val))
			ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
			s.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			s.assembler.Emit(arm64.FMOV(rf, ri))
			s.Push(rf)
		case types.KindF64:
			v := *(*uint64)(unsafe.Pointer(&val))
			ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
			rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
			s.assembler.Emit(arm64.MOVZ(ri, uint16(v&0xFFFF), 0))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>16)&0xFFFF), 16))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>32)&0xFFFF), 32))
			s.assembler.Emit(arm64.MOVK(ri, uint16((v>>48)&0xFFFF), 48))
			s.assembler.Emit(arm64.FMOV(rf, ri))
			s.Push(rf)
		case types.KindRef:
			// Fused CONST_GET+CALL: emit nothing for the CONST_GET
			// itself when the very next opcode is CALL and the fused
			// pair is guaranteed to compile cleanly. Materializing a
			// Ref on the eval stack would corrupt mid-segment exits
			// (closure() reboxes Width64 ints as I64, not Ref), so we
			// must reject (returning false,false BEFORE the s.ip
			// advance counts) when CALL would itself reject.
			if !s.callFusable(val) {
				return false, false
			}
			s.pendingFuncRef = val.Ref()
		default:
			return false, false
		}
		return true, false
	}

	jit[instr.I32_CONST] = func(s *jitSeg) (bool, bool) {
		val := uint32(*(*int32)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 5
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.Push(r0)
		s.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		return true, false
	}

	jit[instr.I32_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ADD(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SUB(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.MUL(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_DIV_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SDIV(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_DIV_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.UDIV(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_REM_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.SDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.Push(r2)
		return true, false
	}

	jit[instr.I32_REM_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.UDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.Push(r2)
		return true, false
	}

	jit[instr.I32_AND] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.AND(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_OR] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ORR(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_XOR] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.EOR(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_SHL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSL(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_SHR_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ASR(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_SHR_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSR(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_EQZ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMPI(r0, 0))
		s.assembler.Emit(arm64.CSET(r0, arm64.CondEQ))
		s.Push(r0)
		return true, false
	}

	jit[instr.I32_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondNE))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_LT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondLT))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_LT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondCC))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_GT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondGT))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_GT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondHI))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_LE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondLE))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_LE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondLS))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_GE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondGE))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_GE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondCS))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_I64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.SXTW(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_I64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.UXTW(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I32_TO_F64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.Push(r1)
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
		s.Push(r0)
		return true, false
	}

	jit[instr.I64_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ADD(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SUB(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.MUL(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_DIV_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.SDIV(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_DIV_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.UDIV(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_REM_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.SDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_REM_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r3 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		r4 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.UDIV(r3, r1, r0))
		s.assembler.Emit(arm64.MUL(r4, r3, r0))
		s.assembler.Emit(arm64.SUB(r2, r1, r4))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_SHL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSL(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_SHR_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.ASR(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_SHR_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.LSR(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_EQZ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMPI(r0, 0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_LT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLT))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_LT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_GT_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_GT_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondHI))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_LE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLE))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_LE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_GE_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_GE_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCS))
		s.Push(r2)
		return true, false
	}

	jit[instr.I64_TO_I32] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.UXTW(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.SCVTF(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.I64_TO_F64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeInt, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.UCVTF(r1, r0))
		s.Push(r1)
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
		s.Push(rf)
		return true, false
	}

	jit[instr.F32_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FADD(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FSUB(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FMUL(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_DIV] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FDIV(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		s.Push(r2)
		return true, false
	}

	jit[instr.F32_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		s.Push(r2)
		return true, false
	}

	jit[instr.F32_LT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		s.Push(r2)
		return true, false
	}

	jit[instr.F32_GT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		s.Push(r2)
		return true, false
	}

	jit[instr.F32_LE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		s.Push(r2)
		return true, false
	}

	jit[instr.F32_GE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		s.Push(r2)
		return true, false
	}

	jit[instr.F32_TO_I32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_I32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_I64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_I64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F32_TO_F64] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width32)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.FCVT(r1, r0))
		s.Push(r1)
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
		s.Push(rf)
		return true, false
	}

	jit[instr.F64_ADD] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FADD(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_SUB] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FSUB(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_MUL] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FMUL(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_DIV] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		s.assembler.Emit(arm64.FDIV(r1, r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_EQ] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondEQ))
		s.Push(r2)
		return true, false
	}

	jit[instr.F64_NE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondNE))
		s.Push(r2)
		return true, false
	}

	jit[instr.F64_LT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondCC))
		s.Push(r2)
		return true, false
	}

	jit[instr.F64_GT] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGT))
		s.Push(r2)
		return true, false
	}

	jit[instr.F64_LE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondLS))
		s.Push(r2)
		return true, false
	}

	jit[instr.F64_GE] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r2 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCMP(r1, r0))
		s.assembler.Emit(arm64.CSET(r2, arm64.CondGE))
		s.Push(r2)
		return true, false
	}

	jit[instr.F64_TO_I32_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_I32_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_I64_S] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZS(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_I64_U] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.FCVTZU(r1, r0))
		s.Push(r1)
		return true, false
	}

	jit[instr.F64_TO_F32] = func(s *jitSeg) (bool, bool) {
		s.ip++
		r0, ok := s.Take(asm.RegTypeFloat, asm.Width64)
		if !ok {
			return false, false
		}
		r1 := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.FCVT(r1, r0))
		s.Push(r1)
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
	s.Push(r0)
	s.ret(fallbackIP)
	s.Pop()

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
	_ = s.PinReturn()

	regs := make([]asm.PReg, len(s.stack))
	for i, v := range s.stack {
		regs[i] = asm.NewPReg(uint8(i), v.Type(), v.Width())
	}

	s.assembler.Emits(arm64.LDI(s.scratch[rNext], uint64(nextIP))...)
	s.assembler.Emits(arm64.LDI(arm64.X15, arm64.Header(nil, regs, len(s.scratch)))...)
	s.assembler.Emit(arm64.RET())
}

// kindRegV1 returns the (type, width, ok) triple for a v1 native
// CALL/RETURN kind. i64 is excluded because boxI64 has a slow-path
// deopt that would emit s.ret() mid-sequence.
func kindRegV1(k types.Kind) (asm.RegType, asm.RegWidth, bool) {
	switch k {
	case types.KindI32:
		return asm.RegTypeInt, asm.Width32, true
	case types.KindF32:
		return asm.RegTypeFloat, asm.Width32, true
	case types.KindF64:
		return asm.RegTypeFloat, asm.Width64, true
	}
	return 0, 0, false
}

// boxByKind boxes a numeric VReg using the existing box helpers.
// Accepts only the v1 numeric kinds.
func (s *jitSeg) boxByKind(r asm.VReg, kind types.Kind) (asm.VReg, bool) {
	typ, width, ok := kindRegV1(kind)
	if !ok || r.Type() != typ || r.Width() != width {
		return asm.VReg{}, false
	}
	switch kind {
	case types.KindI32:
		return s.boxI32(r), true
	case types.KindF32:
		return s.boxF32(r), true
	case types.KindF64:
		return s.boxF64(r), true
	}
	return asm.VReg{}, false
}

// numericEntryV1 reports whether every kind in a jitEntry signature is
// supported by kindRegV1.
func numericEntryV1(e *jitEntry) bool {
	for _, k := range e.params {
		if _, _, ok := kindRegV1(k); !ok {
			return false
		}
	}
	for _, k := range e.locals {
		if _, _, ok := kindRegV1(k); !ok {
			return false
		}
	}
	for _, k := range e.rets {
		if _, _, ok := kindRegV1(k); !ok {
			return false
		}
	}
	return true
}

// emitInterpReload re-materializes rInterp via LDI of the captured
// immortal pointer. Interpreter is allocated once at New() and never
// relocated, so embedding the pointer as immediate is safe.
func (s *jitSeg) emitInterpReload() {
	s.assembler.Emits(arm64.LDI(s.scratch[rInterp], uint64(uintptr(unsafe.Pointer(s.r.c.ip))))...)
}

// emitScratchReload reloads rStack/rHeap/rGlobals from the Interpreter
// struct after a BLR clobbered the caller-save scratch registers.
// rInterp must already be valid.
func (s *jitSeg) emitScratchReload() {
	a := s.assembler
	a.Emit(arm64.LDR(s.scratch[rHeap], s.scratch[rInterp], int16(offInterpHeap)))
	a.Emit(arm64.LDR(s.scratch[rGlobals], s.scratch[rInterp], int16(offInterpGlobals)))
	stackData := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(stackData, s.scratch[rInterp], int16(offInterpStack)))
	frPtr := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(frPtr, s.scratch[rInterp], int16(offInterpFR)))
	bpVal := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(bpVal, frPtr, int16(offFrameBP)))
	bpBytes := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(bpBytes, bpVal, 3))
	a.Emit(arm64.ADD(s.scratch[rStack], stackData, bpBytes))
}

// callFusable reports whether a CONST_GET of the given Boxed value is
// safe to fuse with an immediately following CALL — i.e., the fused
// pair would compile cleanly through emitCall. The CONST_GET handler
// uses this BEFORE setting pendingFuncRef so a rejection happens
// before s.ip would advance past the CONST_GET, letting partial()
// rewind and threaded resume from CONST_GET.
func (s *jitSeg) callFusable(val types.Boxed) bool {
	// Next opcode must be CALL.
	if s.ip >= s.end || s.code[s.ip] != byte(instr.CALL) {
		return false
	}
	addr := val.Ref()
	if addr <= 0 || addr >= len(s.r.c.heap) {
		return false
	}
	if _, ok := s.r.c.heap[addr].(*types.Function); !ok {
		return false
	}
	// Resolve entry: self uses entryFromHeap (chunk filled at Link),
	// cross-fn requires entries[addr] to be already registered.
	var entry *jitEntry
	if addr == s.r.c.addr {
		if !s.r.c.entryReturned {
			return false
		}
		if _, ok := s.labels[0]; !ok {
			return false
		}
		entry = newJitEntry(s.r.c.heap, addr, nil)
	} else {
		entry = s.r.c.entries[addr]
	}
	if entry == nil || !numericEntryV1(entry) {
		return false
	}
	nParams := len(entry.params)
	if len(s.stack) < nParams {
		return false
	}
	top := len(s.stack) - 1
	for i := 0; i < nParams; i++ {
		v := s.stack[top-i]
		typ, width, ok := kindRegV1(entry.params[nParams-1-i])
		if !ok || v.Type() != typ || v.Width() != width {
			return false
		}
	}
	return true
}

// emitCall is the body of jit[instr.CALL]. Returns (ok, stop). When
// emit succeeds, the segment continues. Failure returns (false, false)
// without mutating the assembler beyond what prior handlers committed.
//
// CALL is the consumer side of a CONST_GET+CALL fused pair: the
// preceding CONST_GET set s.pendingFuncRef when it recognized a
// JIT-eligible *types.Function constant, and emitted nothing. emitCall
// uses that addr and clears the field. Without a pending ref, the
// handler rejects (the fused pair is the only direct-call shape v1
// supports).
func (s *jitSeg) emitCall() (bool, bool) {
	addr := s.pendingFuncRef
	if addr == 0 {
		return false, false
	}
	s.pendingFuncRef = 0

	// Resolve entry. callFusable already validated all conditions, so
	// non-nil and numeric-only are guaranteed for the path that set
	// pendingFuncRef; the self/cross split picks the BL target shape.
	isSelf := addr == s.r.c.addr
	var entry *jitEntry
	var selfLabel int
	if isSelf {
		selfLabel = s.labels[0]
		entry = newJitEntry(s.r.c.heap, addr, nil)
	} else {
		entry = s.r.c.entries[addr]
	}

	nParams := len(entry.params)
	nLocalsOnly := len(entry.locals)
	nReturns := len(entry.rets)

	// Caller frame layout. The CONST_GET that produced pendingFuncRef
	// did not push a VReg, so the bytecode-logical eval-stack depth is
	// len(s.stack) + 1. Threaded CALL computes callee_bp = sp -
	// nParams - 1, which simplifies to:
	//   callee_bp_from_caller_bp = callerSlots + len(s.stack) - nParams
	fn := s.r.c.heap[s.r.c.addr].(*types.Function)
	callerSlots := len(fn.Typ.Params) + len(fn.Locals)
	calleeBPSlotOffset := callerSlots + len(s.stack) - nParams
	calleeBPByteOffset := calleeBPSlotOffset * 8

	// Pop nParams in declared order (last param popped first).
	pregs := make([]asm.VReg, nParams)
	for i := nParams - 1; i >= 0; i-- {
		r, ok := s.Pop()
		if !ok {
			return false, false
		}
		pregs[i] = r
	}

	a := s.assembler

	// Spill params into i.stack[callee_bp + i] via box helpers. rStack
	// currently points at &i.stack[caller_bp]; offsets are relative to
	// caller_bp.
	for i, p := range pregs {
		boxed, ok := s.boxByKind(p, entry.params[i])
		if !ok {
			return false, false
		}
		a.Emit(arm64.STR(boxed, s.scratch[rStack], int16(calleeBPByteOffset+i*8)))
	}

	// Zero the fn-ref slot (now first local) and remaining callee
	// locals so threaded LOCAL_GET sees cleared slots.
	zeroBase := calleeBPByteOffset + nParams*8
	for i := 0; i < nLocalsOnly; i++ {
		a.Emit(arm64.STR(arm64.XZR, s.scratch[rStack], int16(zeroBase+i*8)))
	}

	// Frame push.
	s.emitInterpReload()

	fpReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(fpReg, s.scratch[rInterp], int16(offInterpFP)))

	framesPtr := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(framesPtr, s.scratch[rInterp], int16(offInterpFrames)))

	// frameAddr = framesPtr + fpReg * sizeofFrame
	frameAddr := a.NewVReg(asm.RegTypeInt, asm.Width64)
	if sz := uint8(log2(sizeofFrame)); sz != 0 {
		shifted := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(shifted, fpReg, sz))
		a.Emit(arm64.ADD(frameAddr, framesPtr, shifted))
	} else {
		sizeReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emits(arm64.LDI(sizeReg, uint64(sizeofFrame))...)
		a.Emit(arm64.MADD(frameAddr, fpReg, sizeReg, framesPtr))
	}

	// f.code = i.code[addr] (24-byte slice header copy).
	codeBase := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(codeBase, s.scratch[rInterp], int16(offInterpCode)))
	codeEntry := a.NewVReg(asm.RegTypeInt, asm.Width64)
	offsetReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emits(arm64.LDI(offsetReg, uint64(addr*24))...)
	a.Emit(arm64.ADD(codeEntry, codeBase, offsetReg))
	for w := 0; w < 3; w++ {
		tmp := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDR(tmp, codeEntry, int16(w*8)))
		a.Emit(arm64.STR(tmp, frameAddr, int16(int(offFrameCode)+w*8)))
	}

	// f.addr = addr (constant).
	addrReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emits(arm64.LDI(addrReg, uint64(addr))...)
	a.Emit(arm64.STR(addrReg, frameAddr, int16(offFrameAddr)))

	// f.ip = 0.
	a.Emit(arm64.STR(arm64.XZR, frameAddr, int16(offFrameIP)))

	// f.bp = caller_bp + calleeBPSlotOffset.
	callerFr := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(callerFr, s.scratch[rInterp], int16(offInterpFR)))
	callerBP := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(callerBP, callerFr, int16(offFrameBP)))
	calleeBP := a.NewVReg(asm.RegTypeInt, asm.Width64)
	if calleeBPSlotOffset <= 0xFFFF {
		a.Emit(arm64.ADDI(calleeBP, callerBP, uint16(calleeBPSlotOffset)))
	} else {
		offReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emits(arm64.LDI(offReg, uint64(calleeBPSlotOffset))...)
		a.Emit(arm64.ADD(calleeBP, callerBP, offReg))
	}
	a.Emit(arm64.STR(calleeBP, frameAddr, int16(offFrameBP)))

	// f.returns = nReturns.
	retReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emits(arm64.LDI(retReg, uint64(nReturns))...)
	a.Emit(arm64.STR(retReg, frameAddr, int16(offFrameReturns)))

	// f.release = 1 (byte).
	oneReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emits(arm64.LDI(oneReg, 1)...)
	a.Emit(arm64.STRB(oneReg, frameAddr, int16(offFrameRelease)))

	// caller_frame.ip = postCallIP — for threaded fallback resume.
	postIPReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emits(arm64.LDI(postIPReg, uint64(s.ip))...)
	a.Emit(arm64.STR(postIPReg, callerFr, int16(offFrameIP)))

	// i.fp++.
	newFp := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(newFp, fpReg, 1))
	a.Emit(arm64.STR(newFp, s.scratch[rInterp], int16(offInterpFP)))

	// i.fr = frameAddr.
	a.Emit(arm64.STR(frameAddr, s.scratch[rInterp], int16(offInterpFR)))

	// i.sp = callee_bp + nParams + nLocalsOnly.
	spDelta := nParams + nLocalsOnly
	newSp := a.NewVReg(asm.RegTypeInt, asm.Width64)
	if spDelta <= 0xFFFF {
		a.Emit(arm64.ADDI(newSp, calleeBP, uint16(spDelta)))
	} else {
		offReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emits(arm64.LDI(offReg, uint64(spDelta))...)
		a.Emit(arm64.ADD(newSp, calleeBP, offReg))
	}
	a.Emit(arm64.STR(newSp, s.scratch[rInterp], int16(offInterpSP)))

	// Rebase rStack for callee: rStack = i.stack.data + calleeBP * 8.
	stackData := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(stackData, s.scratch[rInterp], int16(offInterpStack)))
	bpBytes := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(bpBytes, calleeBP, 3))
	a.Emit(arm64.ADD(s.scratch[rStack], stackData, bpBytes))

	// Call.
	if isSelf {
		a.Emit(arm64.BLLabel(selfLabel))
	} else {
		target := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emits(arm64.LDI(target, uint64(uintptr(entry.chunk.Ptr())))...)
		a.Emit(arm64.BLR(target))
	}

	// Post-BLR: scratch regs clobbered. Reload rInterp from immortal
	// pointer, then derive the rest from i.
	s.emitInterpReload()
	s.emitScratchReload()

	// Load nReturns boxed values from i.stack[callee_bp + i] and push
	// the unboxed VRegs onto s.stack. rStack now = &i.stack[caller_bp];
	// callee_bp_byte_offset relative to caller_bp is unchanged.
	for i := 0; i < nReturns; i++ {
		boxed := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDR(boxed, s.scratch[rStack], int16(calleeBPByteOffset+i*8)))
		unboxed, ok := s.unbox64(boxed, entry.rets[i])
		if !ok {
			return false, false
		}
		s.Push(unboxed)
	}
	return true, false
}

// emitReturn is the body of jit[instr.RETURN]. Only valid when the
// segment is the function-entry segment (s.start == 0) so the chunk
// was entered via BLR (or trampoline that expects RET back). The
// emitted code spills return values to i.stack[f.bp..], pops the
// frame, and RETs.
func (s *jitSeg) emitReturn() (bool, bool) {
	if s.start != 0 {
		return false, false
	}
	entry := newJitEntry(s.r.c.heap, s.r.c.addr, nil)
	if entry == nil || !numericEntryV1(entry) {
		return false, false
	}
	rets := entry.rets
	if len(s.stack) < len(rets) {
		return false, false
	}
	// Validate return kinds against current s.stack top.
	for i, k := range rets {
		v := s.stack[len(s.stack)-len(rets)+i]
		typ, width, ok := kindRegV1(k)
		if !ok || v.Type() != typ || v.Width() != width {
			return false, false
		}
	}

	a := s.assembler

	// Spill returns to i.stack[f.bp + i] via boxByKind. rStack
	// currently = &i.stack[f.bp] for this (callee) frame.
	for i, k := range rets {
		v := s.stack[len(s.stack)-len(rets)+i]
		boxed, ok := s.boxByKind(v, k)
		if !ok {
			return false, false
		}
		a.Emit(arm64.STR(boxed, s.scratch[rStack], int16(i*8)))
	}

	// Pop frame: rInterp may already be valid (it was loaded at chunk
	// entry by the trampoline and not yet clobbered), but reload for
	// safety to keep this helper independent of upstream state.
	s.emitInterpReload()

	// i.sp = f.bp + len(rets). Use rStack-derived f.bp? Easier: load
	// from i.fr.bp + len(rets).
	frPtr := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(frPtr, s.scratch[rInterp], int16(offInterpFR)))
	bpVal := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(bpVal, frPtr, int16(offFrameBP)))
	newSp := a.NewVReg(asm.RegTypeInt, asm.Width64)
	if len(rets) <= 0xFFFF {
		a.Emit(arm64.ADDI(newSp, bpVal, uint16(len(rets))))
	} else {
		offReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emits(arm64.LDI(offReg, uint64(len(rets)))...)
		a.Emit(arm64.ADD(newSp, bpVal, offReg))
	}
	a.Emit(arm64.STR(newSp, s.scratch[rInterp], int16(offInterpSP)))

	// f.code = nil (zero 3 words of slice header).
	for w := 0; w < 3; w++ {
		a.Emit(arm64.STR(arm64.XZR, frPtr, int16(int(offFrameCode)+w*8)))
	}

	// i.fp--.
	fpReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(fpReg, s.scratch[rInterp], int16(offInterpFP)))
	newFp := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.SUBI(newFp, fpReg, 1))
	a.Emit(arm64.STR(newFp, s.scratch[rInterp], int16(offInterpFP)))

	// i.fr = &i.frames[newFp - 1] = framesPtr + (newFp - 1) * sizeofFrame.
	framesPtr := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(framesPtr, s.scratch[rInterp], int16(offInterpFrames)))
	idxReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.SUBI(idxReg, newFp, 1))
	newFrAddr := a.NewVReg(asm.RegTypeInt, asm.Width64)
	if sz := uint8(log2(sizeofFrame)); sz != 0 {
		shifted := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(shifted, idxReg, sz))
		a.Emit(arm64.ADD(newFrAddr, framesPtr, shifted))
	} else {
		sizeReg := a.NewVReg(asm.RegTypeInt, asm.Width64)
		a.Emits(arm64.LDI(sizeReg, uint64(sizeofFrame))...)
		a.Emit(arm64.MADD(newFrAddr, idxReg, sizeReg, framesPtr))
	}
	a.Emit(arm64.STR(newFrAddr, s.scratch[rInterp], int16(offInterpFR)))

	// Mark entry-segment native return so link() registers this
	// function in jitEntries for subsequent cross-function callers.
	s.r.c.entryReturned = true

	a.Emit(arm64.RET())
	return true, true
}

// log2 returns the integer base-2 log of n if n is a power of two,
// else 0. Used to pick LSL shift count over MUL when sizeofFrame is
// a power of two.
func log2(n uintptr) int {
	if n == 0 || n&(n-1) != 0 {
		return 0
	}
	r := 0
	for n > 1 {
		n >>= 1
		r++
	}
	return r
}

func initLayout() {
	var i Interpreter
	var f frame
	offInterpFP = unsafe.Offsetof(i.fp)
	offInterpSP = unsafe.Offsetof(i.sp)
	offInterpFR = unsafe.Offsetof(i.fr)
	offInterpFrames = unsafe.Offsetof(i.frames)
	offInterpStack = unsafe.Offsetof(i.stack)
	offInterpHeap = unsafe.Offsetof(i.heap)
	offInterpGlobals = unsafe.Offsetof(i.globals)
	offInterpCode = unsafe.Offsetof(i.code)
	sizeofFrame = unsafe.Sizeof(f)
	offFrameCode = unsafe.Offsetof(f.code)
	offFrameAddr = unsafe.Offsetof(f.addr)
	offFrameIP = unsafe.Offsetof(f.ip)
	offFrameBP = unsafe.Offsetof(f.bp)
	offFrameReturns = unsafe.Offsetof(f.returns)
	offFrameRelease = unsafe.Offsetof(f.release)
}
