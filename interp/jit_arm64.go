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

	jitPrologue = func(s *jitSeg) {
		s.assembler.Emits(arm64.LDI(s.scratch[rNext], uint64(s.end))...)
	}

	jitEpilogue = func(s *jitSeg) {
		s.ret(s.end)
	}

	jit[instr.NOP] = func(s *jitSeg) bool {
		s.ip++
		return true
	}
	jit[instr.UNREACHABLE] = func(s *jitSeg) bool {
		return false
	}
	jit[instr.DROP] = func(s *jitSeg) bool {
		if len(s.stack) == 0 {
			return false
		}
		s.ip++
		s.Pop()
		return true
	}
	jit[instr.DUP] = func(s *jitSeg) bool {
		r0, ok := s.Top(0)
		if !ok {
			return false
		}
		s.ip++
		dst := s.assembler.NewVReg(r0.Type(), r0.Width())
		s.assembler.Emit(arm64.MOV(dst, r0))
		s.Push(dst)
		return true
	}
	jit[instr.SWAP] = func(s *jitSeg) bool {
		if len(s.stack) < 2 {
			return false
		}
		s.ip++
		r0, _ := s.Pop()
		r1, _ := s.Pop()
		s.Push(r0)
		s.Push(r1)
		return true
	}
	jit[instr.BR] = func(s *jitSeg) bool {
		offset := instr.ParseI16(s.code, s.ip+1)
		targetIP := s.ip + 3 + offset
		s.ip += 3

		_ = s.pinReturn()
		edge, fallback := s.edge(targetIP)
		s.assembler.Emit(arm64.BLabel(edge))
		s.assembler.Bind(fallback)
		s.ret(targetIP)
		return true
	}
	jit[instr.BR_IF] = func(s *jitSeg) bool {
		if !s.accepts(asm.NewPReg(0, asm.RegTypeInt, asm.Width32)) {
			return false
		}
		offset := instr.ParseI16(s.code, s.ip+1)
		targetIP := s.ip + 3 + offset
		fallIP := s.ip + 3
		s.ip += 3

		cond, _ := s.Take(asm.RegTypeInt, asm.Width32)

		_ = s.pinReturn()
		targetEdge, targetFallback := s.edge(targetIP)
		fallEdge, fallFallback := s.edge(fallIP)
		s.assembler.Emit(arm64.CBNZLabel(cond, targetEdge))
		s.assembler.Emit(arm64.BLabel(fallEdge))
		s.assembler.Bind(targetFallback)
		s.ret(targetIP)
		s.assembler.Bind(fallFallback)
		s.ret(fallIP)
		return true
	}
	jit[instr.BR_TABLE] = func(s *jitSeg) bool {
		if !s.accepts(asm.NewPReg(0, asm.RegTypeInt, asm.Width32)) {
			return false
		}
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

		r0, _ := s.Take(asm.RegTypeInt, asm.Width32)

		_ = s.pinReturn()
		edges := make([]int, count+1)
		fallbacks := make([]int, count+1)
		for j := range edges {
			edges[j], fallbacks[j] = s.edge(targetIPs[j])
		}

		for j := 0; j < count; j++ {
			s.assembler.Emit(arm64.CMPI(r0, uint16(j)))
			s.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, edges[j]))
		}
		s.assembler.Emit(arm64.BLabel(edges[count]))

		for j := 0; j <= count; j++ {
			s.assembler.Bind(fallbacks[j])
			s.ret(targetIPs[j])
		}
		return true
	}
	jit[instr.SELECT] = func(s *jitSeg) bool {
		cond, ok := s.Top(0)
		v2, ok2 := s.Top(1)
		v1, ok1 := s.Top(2)
		if !ok || !ok1 || !ok2 || cond.Type() != asm.RegTypeInt || cond.Width() != asm.Width32 {
			return false
		}
		if v1.Type() != v2.Type() || v1.Width() != v2.Width() {
			return false
		}
		s.ip++
		s.Pop()
		s.Pop()
		s.Pop()

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
		return true
	}
	jit[instr.GLOBAL_GET] = func(s *jitSeg) bool {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		offset, ok := s.global(idx)
		if !ok {
			return false
		}
		kind, ok := s.facts[idx]
		if !ok || !kind.IsNumeric() {
			return false
		}
		s.ip += 3
		s.loadSlot(s.scratch[rGlobals], offset, kind)
		return true
	}
	jit[instr.GLOBAL_SET] = func(s *jitSeg) bool {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		offset, ok := s.global(idx)
		if !ok {
			return false
		}
		r0, ok := s.Top(0)
		if !ok {
			return false
		}
		kind, ok := s.regKind(r0)
		if !ok {
			return false
		}
		s.ip += 3
		s.Pop()
		s.storeSlot(r0, kind, s.scratch[rGlobals], offset, s.ip-3)
		s.facts[idx] = kind
		return true
	}
	jit[instr.GLOBAL_TEE] = func(s *jitSeg) bool {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		offset, ok := s.global(idx)
		if !ok {
			return false
		}
		r0, ok := s.Top(0)
		if !ok {
			return false
		}
		kind, ok := s.regKind(r0)
		if !ok {
			return false
		}
		s.ip += 3
		s.storeSlot(r0, kind, s.scratch[rGlobals], offset, s.ip-3)
		s.facts[idx] = kind
		return true
	}
	jit[instr.LOCAL_GET] = func(s *jitSeg) bool {
		idx := int(s.code[s.ip+1])

		typ, ok := s.local(idx)
		if !ok {
			return false
		}
		if !typ.Kind().IsNumeric() {
			return false
		}

		s.ip += 2
		s.loadSlot(s.scratch[rStack], int16(idx*8), typ.Kind())
		return true
	}
	jit[instr.LOCAL_SET] = func(s *jitSeg) bool {
		idx := int(s.code[s.ip+1])

		typ, ok := s.local(idx)
		if !ok {
			return false
		}
		reg, ok := s.regOf(typ.Kind())
		if !ok || !s.accepts(reg) {
			return false
		}
		s.ip += 2
		r0, _ := s.Take(reg.Type(), reg.Width())
		s.storeSlot(r0, typ.Kind(), s.scratch[rStack], int16(idx*8), s.ip-2)
		return true
	}
	jit[instr.LOCAL_TEE] = func(s *jitSeg) bool {
		idx := int(s.code[s.ip+1])

		typ, ok := s.local(idx)
		if !ok {
			return false
		}
		reg, ok := s.regOf(typ.Kind())
		if !ok || !s.accepts(reg) {
			return false
		}
		s.ip += 2
		r0, _ := s.Take(reg.Type(), reg.Width())
		s.Push(r0)
		s.storeSlot(r0, typ.Kind(), s.scratch[rStack], int16(idx*8), s.ip-2)
		return true
	}
	jit[instr.CONST_GET] = func(s *jitSeg) bool {
		idx := int(*(*uint16)(unsafe.Pointer(&s.code[s.ip+1])))
		if idx < 0 || idx >= len(s.constants) {
			return false
		}
		val := s.constants[idx]
		if !val.Kind().IsNumeric() {
			return false
		}
		s.ip += 3
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
		}
		return true
	}
	jit[instr.I32_CONST] = func(s *jitSeg) bool {
		val := uint32(*(*int32)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 5
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.Push(r0)
		s.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		return true
	}
	jit[instr.I32_ADD] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.ADD)
	}
	jit[instr.I32_SUB] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.SUB)
	}
	jit[instr.I32_MUL] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.MUL)
	}
	jit[instr.I32_DIV_S] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.SDIV)
	}
	jit[instr.I32_DIV_U] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.UDIV)
	}
	jit[instr.I32_REM_S] = func(s *jitSeg) bool {
		return s.remainder(asm.Width32, arm64.SDIV)
	}
	jit[instr.I32_REM_U] = func(s *jitSeg) bool {
		return s.remainder(asm.Width32, arm64.UDIV)
	}
	jit[instr.I32_SHL] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.LSL)
	}
	jit[instr.I32_SHR_S] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.ASR)
	}
	jit[instr.I32_SHR_U] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.LSR)
	}
	jit[instr.I32_XOR] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.EOR)
	}
	jit[instr.I32_AND] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.AND)
	}
	jit[instr.I32_OR] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width32, arm64.ORR)
	}
	jit[instr.I32_EQZ] = func(s *jitSeg) bool {
		if !s.accepts(asm.NewPReg(0, asm.RegTypeInt, asm.Width32)) {
			return false
		}
		s.ip++
		r0, _ := s.Take(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMPI(r0, 0))
		s.assembler.Emit(arm64.CSET(r0, arm64.CondEQ))
		s.Push(r0)
		return true
	}
	jit[instr.I32_EQ] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondEQ)
	}
	jit[instr.I32_NE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondNE)
	}
	jit[instr.I32_LT_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondLT)
	}
	jit[instr.I32_LT_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondCC)
	}
	jit[instr.I32_GT_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondGT)
	}
	jit[instr.I32_GT_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondHI)
	}
	jit[instr.I32_LE_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondLE)
	}
	jit[instr.I32_LE_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondLS)
	}
	jit[instr.I32_GE_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondGE)
	}
	jit[instr.I32_GE_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width32, arm64.CMP, arm64.CondCS)
	}
	jit[instr.I32_TO_I64_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width32, asm.RegTypeInt, asm.Width64, arm64.SXTW)
	}
	jit[instr.I32_TO_I64_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width32, asm.RegTypeInt, asm.Width64, arm64.UXTW)
	}
	jit[instr.I32_TO_F32_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width32, asm.RegTypeFloat, asm.Width32, arm64.UCVTF)
	}
	jit[instr.I32_TO_F32_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width32, asm.RegTypeFloat, asm.Width32, arm64.SCVTF)
	}
	jit[instr.I32_TO_F64_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width32, asm.RegTypeFloat, asm.Width64, arm64.UCVTF)
	}
	jit[instr.I32_TO_F64_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width32, asm.RegTypeFloat, asm.Width64, arm64.SCVTF)
	}
	jit[instr.I64_CONST] = func(s *jitSeg) bool {
		val := uint64(*(*int64)(unsafe.Pointer(&s.code[s.ip+1])))
		s.ip += 9
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.MOVZ(r0, uint16(val&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>16)&0xFFFF), 16))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>32)&0xFFFF), 32))
		s.assembler.Emit(arm64.MOVK(r0, uint16((val>>48)&0xFFFF), 48))
		s.Push(r0)
		return true
	}
	jit[instr.I64_ADD] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.ADD)
	}
	jit[instr.I64_SUB] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.SUB)
	}
	jit[instr.I64_MUL] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.MUL)
	}
	jit[instr.I64_DIV_S] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.SDIV)
	}
	jit[instr.I64_DIV_U] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.UDIV)
	}
	jit[instr.I64_REM_S] = func(s *jitSeg) bool {
		return s.remainder(asm.Width64, arm64.SDIV)
	}
	jit[instr.I64_REM_U] = func(s *jitSeg) bool {
		return s.remainder(asm.Width64, arm64.UDIV)
	}
	jit[instr.I64_SHL] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.LSL)
	}
	jit[instr.I64_SHR_S] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.ASR)
	}
	jit[instr.I64_SHR_U] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeInt, asm.Width64, arm64.LSR)
	}
	jit[instr.I64_EQZ] = func(s *jitSeg) bool {
		if !s.accepts(asm.NewPReg(0, asm.RegTypeInt, asm.Width64)) {
			return false
		}
		s.ip++
		r0, _ := s.Take(asm.RegTypeInt, asm.Width64)
		r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.CMPI(r0, 0))
		s.assembler.Emit(arm64.CSET(r1, arm64.CondEQ))
		s.Push(r1)
		return true
	}
	jit[instr.I64_EQ] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondEQ)
	}
	jit[instr.I64_NE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondNE)
	}
	jit[instr.I64_LT_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondLT)
	}
	jit[instr.I64_LT_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondCC)
	}
	jit[instr.I64_GT_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondGT)
	}
	jit[instr.I64_GT_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondHI)
	}
	jit[instr.I64_LE_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondLE)
	}
	jit[instr.I64_LE_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondLS)
	}
	jit[instr.I64_GE_S] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondGE)
	}
	jit[instr.I64_GE_U] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeInt, asm.Width64, arm64.CMP, arm64.CondCS)
	}
	jit[instr.I64_TO_I32] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width64, asm.RegTypeInt, asm.Width32, arm64.UXTW)
	}
	jit[instr.I64_TO_F32_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width64, asm.RegTypeFloat, asm.Width32, arm64.SCVTF)
	}
	jit[instr.I64_TO_F32_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width64, asm.RegTypeFloat, asm.Width32, arm64.UCVTF)
	}
	jit[instr.I64_TO_F64_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width64, asm.RegTypeFloat, asm.Width64, arm64.SCVTF)
	}
	jit[instr.I64_TO_F64_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeInt, asm.Width64, asm.RegTypeFloat, asm.Width64, arm64.UCVTF)
	}
	jit[instr.F32_CONST] = func(s *jitSeg) bool {
		bits := *(*uint32)(unsafe.Pointer(&s.code[s.ip+1]))
		s.ip += 5
		ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.MOVZ(ri, uint16(bits&0xFFFF), 0))
		s.assembler.Emit(arm64.MOVK(ri, uint16((bits>>16)&0xFFFF), 16))
		s.assembler.Emit(arm64.FMOV(rf, ri))
		s.Push(rf)
		return true
	}
	jit[instr.F32_ADD] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width32, arm64.FADD)
	}
	jit[instr.F32_SUB] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width32, arm64.FSUB)
	}
	jit[instr.F32_MUL] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width32, arm64.FMUL)
	}
	jit[instr.F32_DIV] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width32, arm64.FDIV)
	}
	jit[instr.F32_EQ] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width32, arm64.FCMP, arm64.CondEQ)
	}
	jit[instr.F32_NE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width32, arm64.FCMP, arm64.CondNE)
	}
	jit[instr.F32_LT] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width32, arm64.FCMP, arm64.CondCC)
	}
	jit[instr.F32_GT] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width32, arm64.FCMP, arm64.CondGT)
	}
	jit[instr.F32_LE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width32, arm64.FCMP, arm64.CondLS)
	}
	jit[instr.F32_GE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width32, arm64.FCMP, arm64.CondGE)
	}
	jit[instr.F32_TO_I32_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width32, asm.RegTypeInt, asm.Width32, arm64.FCVTZS)
	}
	jit[instr.F32_TO_I32_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width32, asm.RegTypeInt, asm.Width32, arm64.FCVTZU)
	}
	jit[instr.F32_TO_I64_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width32, asm.RegTypeInt, asm.Width64, arm64.FCVTZS)
	}
	jit[instr.F32_TO_I64_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width32, asm.RegTypeInt, asm.Width64, arm64.FCVTZU)
	}
	jit[instr.F32_TO_F64] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width32, asm.RegTypeFloat, asm.Width64, arm64.FCVT)
	}
	jit[instr.F64_CONST] = func(s *jitSeg) bool {
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
		return true
	}
	jit[instr.F64_ADD] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width64, arm64.FADD)
	}
	jit[instr.F64_SUB] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width64, arm64.FSUB)
	}
	jit[instr.F64_MUL] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width64, arm64.FMUL)
	}
	jit[instr.F64_DIV] = func(s *jitSeg) bool {
		return s.binary(asm.RegTypeFloat, asm.Width64, arm64.FDIV)
	}
	jit[instr.F64_EQ] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width64, arm64.FCMP, arm64.CondEQ)
	}
	jit[instr.F64_NE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width64, arm64.FCMP, arm64.CondNE)
	}
	jit[instr.F64_LT] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width64, arm64.FCMP, arm64.CondCC)
	}
	jit[instr.F64_GT] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width64, arm64.FCMP, arm64.CondGT)
	}
	jit[instr.F64_LE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width64, arm64.FCMP, arm64.CondLS)
	}
	jit[instr.F64_GE] = func(s *jitSeg) bool {
		return s.compare(asm.RegTypeFloat, asm.Width64, arm64.FCMP, arm64.CondGE)
	}
	jit[instr.F64_TO_I32_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width64, asm.RegTypeInt, asm.Width32, arm64.FCVTZS)
	}
	jit[instr.F64_TO_I32_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width64, asm.RegTypeInt, asm.Width32, arm64.FCVTZU)
	}
	jit[instr.F64_TO_I64_S] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width64, asm.RegTypeInt, asm.Width64, arm64.FCVTZS)
	}
	jit[instr.F64_TO_I64_U] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width64, asm.RegTypeInt, asm.Width64, arm64.FCVTZU)
	}
	jit[instr.F64_TO_F32] = func(s *jitSeg) bool {
		return s.convert(asm.RegTypeFloat, asm.Width64, asm.RegTypeFloat, asm.Width32, arm64.FCVT)
	}
}

func (s *jitSeg) binary(typ asm.RegType, width asm.RegWidth, emit func(dst, left, right asm.Reg) asm.Instruction) bool {
	reg := asm.NewPReg(0, typ, width)
	if !s.accepts(reg, reg) {
		return false
	}
	s.ip++
	right, _ := s.Take(typ, width)
	left, _ := s.Take(typ, width)
	s.assembler.Emit(emit(left, left, right))
	s.Push(left)
	return true
}

func (s *jitSeg) remainder(width asm.RegWidth, divide func(dst, left, right asm.Reg) asm.Instruction) bool {
	reg := asm.NewPReg(0, asm.RegTypeInt, width)
	if !s.accepts(reg, reg) {
		return false
	}
	s.ip++
	right, _ := s.Take(asm.RegTypeInt, width)
	left, _ := s.Take(asm.RegTypeInt, width)
	result := s.assembler.NewVReg(asm.RegTypeInt, width)
	quotient := s.assembler.NewVReg(asm.RegTypeInt, width)
	product := s.assembler.NewVReg(asm.RegTypeInt, width)
	s.assembler.Emit(divide(quotient, left, right))
	s.assembler.Emit(arm64.MUL(product, quotient, right))
	s.assembler.Emit(arm64.SUB(result, left, product))
	s.Push(result)
	return true
}

func (s *jitSeg) compare(typ asm.RegType, width asm.RegWidth, compare func(left, right asm.Reg) asm.Instruction, cond uint8) bool {
	reg := asm.NewPReg(0, typ, width)
	if !s.accepts(reg, reg) {
		return false
	}
	s.ip++
	right, _ := s.Take(typ, width)
	left, _ := s.Take(typ, width)
	result := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
	s.assembler.Emit(compare(left, right))
	s.assembler.Emit(arm64.CSET(result, cond))
	s.Push(result)
	return true
}

func (s *jitSeg) convert(srcType asm.RegType, srcWidth asm.RegWidth, dstType asm.RegType, dstWidth asm.RegWidth, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if !s.accepts(asm.NewPReg(0, srcType, srcWidth)) {
		return false
	}
	s.ip++
	src, _ := s.Take(srcType, srcWidth)
	dst := s.assembler.NewVReg(dstType, dstWidth)
	s.assembler.Emit(emit(dst, src))
	s.Push(dst)
	return true
}

func (s *jitSeg) unbox64(boxed asm.VReg, kind types.Kind) asm.VReg {
	switch kind {
	case types.KindI32:
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		s.assembler.Emit(arm64.UXTW(r0, boxed))
		return r0
	case types.KindI64:
		r0 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
		s.assembler.Emit(arm64.LSLI(r0, boxed, 64-types.VBits))
		s.assembler.Emit(arm64.ASRI(r0, r0, 64-types.VBits))
		return r0
	case types.KindF32:
		ri := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
		rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width32)
		s.assembler.Emit(arm64.UXTW(ri, boxed))
		s.assembler.Emit(arm64.FMOV(rf, ri))
		return rf
	case types.KindF64:
		rf := s.assembler.NewVReg(asm.RegTypeFloat, asm.Width64)
		s.assembler.Emit(arm64.FMOV(rf, boxed))
		return rf
	default:
		return asm.VReg{}
	}
}

func (s *jitSeg) box64(r0 asm.VReg, kind types.Kind, fallbackIP int) asm.VReg {
	switch kind {
	case types.KindI32:
		return s.boxI32(r0)
	case types.KindI64:
		return s.boxI64(r0, fallbackIP)
	case types.KindF32:
		return s.boxF32(r0)
	case types.KindF64:
		return s.boxF64(r0)
	default:
		return asm.VReg{}
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
	_ = s.pinReturn()

	regs := make([]asm.PReg, len(s.stack))
	for i, v := range s.stack {
		regs[i] = asm.NewPReg(uint8(i), v.Type(), v.Width())
	}

	s.assembler.Emits(arm64.LDI(s.scratch[rNext], uint64(nextIP))...)
	s.assembler.Emits(arm64.LDI(arm64.X15, arm64.Header(nil, regs, len(s.scratch)))...)
	s.assembler.Emit(arm64.RET())
}

// loadSlot emits a boxed 64-bit load from base+offset, unboxes it as kind, and
// pushes the result. Shared by LOCAL_GET and GLOBAL_GET.
func (s *jitSeg) loadSlot(base asm.PReg, offset int16, kind types.Kind) {
	boxed := s.assembler.NewVReg(asm.RegTypeInt, asm.Width64)
	s.assembler.Emit(arm64.LDR(boxed, base, offset))
	s.Push(s.unbox64(boxed, kind))
}

// storeSlot boxes r0 as kind and emits a 64-bit store to base+offset. Shared by
// LOCAL_SET/TEE and GLOBAL_SET/TEE.
func (s *jitSeg) storeSlot(r0 asm.VReg, kind types.Kind, base asm.PReg, offset int16, fallbackIP int) {
	boxed := s.box64(r0, kind, fallbackIP)
	s.assembler.Emit(arm64.STR(boxed, base, offset))
}

func (s *jitSeg) regOf(kind types.Kind) (asm.PReg, bool) {
	switch kind {
	case types.KindI32:
		return asm.NewPReg(0, asm.RegTypeInt, asm.Width32), true
	case types.KindI64:
		return asm.NewPReg(0, asm.RegTypeInt, asm.Width64), true
	case types.KindF32:
		return asm.NewPReg(0, asm.RegTypeFloat, asm.Width32), true
	case types.KindF64:
		return asm.NewPReg(0, asm.RegTypeFloat, asm.Width64), true
	default:
		return asm.PReg{}, false
	}
}
