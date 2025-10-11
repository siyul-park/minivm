package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

var dispatch = [256]func(i *Interpreter) error{
	instr.NOP: func(i *Interpreter) error {
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.UNREACHABLE: func(i *Interpreter) error {
		i.frames[i.fp-1].ip++
		return ErrUnreachableExecuted
	},
	instr.DROP: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		val := i.stack[i.sp-1]
		if val.Kind() == types.KindRef {
			i.release(val.Ref())
		}
		i.sp--
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.DUP: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		val := i.stack[i.sp-1]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}
		i.stack[i.sp] = val
		i.sp++
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.SWAP: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		i.stack[i.sp-1], i.stack[i.sp-2] = i.stack[i.sp-2], i.stack[i.sp-1]
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.BR: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		offset := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		frame.ip += offset + 3
		return nil
	},
	instr.BR_IF: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		i.sp--
		cond := i.stack[i.sp].I32()
		if cond != 0 {
			code := frame.fn.Code
			offset := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
			frame.ip += offset
		}
		frame.ip += 3
		return nil
	},
	instr.BR_TABLE: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		count := int(code[frame.ip+1])
		i.sp--
		cond := int(i.stack[i.sp].I32())
		if cond > count {
			cond = count + 1
		}
		offset := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+cond*2+2])))
		frame.ip += offset + count*2 + 4
		return nil
	},
	instr.SELECT: func(i *Interpreter) error {
		if i.sp < 3 {
			return ErrStackUnderflow
		}
		cond := i.stack[i.sp-1].I32()
		var discard types.Boxed
		if cond == 0 {
			discard = i.stack[i.sp-3]
			i.stack[i.sp-3] = i.stack[i.sp-2]
		} else {
			discard = i.stack[i.sp-2]
		}
		if discard.Kind() == types.KindRef {
			i.release(discard.Ref())
		}
		i.sp -= 2
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.CALL: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		if i.fp == len(i.frames) {
			return ErrFrameOverflow
		}
		addr := i.stack[i.sp-1].Ref()
		fn, ok := i.heap[addr].(*types.Function)
		if !ok {
			return ErrTypeMismatch
		}
		if i.sp <= fn.Params {
			return ErrStackUnderflow
		}
		if i.sp+fn.Locals-fn.Params > len(i.stack) {
			return ErrStackOverflow
		}
		for idx := 0; idx < fn.Locals-fn.Params; idx++ {
			i.stack[i.sp+idx-1] = 0
		}
		frame := &i.frames[i.fp]
		frame.addr = addr
		frame.fn = fn
		frame.ip = 0
		frame.bp = i.sp - fn.Params - 1
		i.fp++
		i.sp = frame.bp + fn.Locals
		i.frames[i.fp-2].ip++
		return nil
	},
	instr.RETURN: func(i *Interpreter) error {
		if i.fp == 1 {
			return ErrFrameUnderflow
		}
		frame := &i.frames[i.fp-1]
		fn := frame.fn
		if i.sp < fn.Returns {
			return ErrStackUnderflow
		}
		copy(i.stack[frame.bp:frame.bp+fn.Returns], i.stack[i.sp-fn.Returns:i.sp])
		i.sp = frame.bp + fn.Returns
		if frame.addr > 0 {
			i.release(frame.addr)
		}
		frame.fn = nil
		i.fp--
		return nil
	},
	instr.GLOBAL_GET: func(i *Interpreter) error {
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.global) {
			return ErrSegmentationFault
		}
		val := i.global[idx]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}
		i.stack[i.sp] = val
		i.sp++
		frame.ip += 3
		return nil
	},
	instr.GLOBAL_SET: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 {
			return ErrSegmentationFault
		}
		val := i.stack[i.sp-1]
		if idx >= len(i.global) {
			if cap(i.global) > idx {
				i.global = i.global[:idx+1]
			} else {
				global := make([]types.Boxed, idx*2)
				copy(global, i.global)
				i.global = global[:idx+1]
			}
		}
		if old := i.global[idx]; old != val && old.Kind() == types.KindRef {
			i.release(old.Ref())
		}
		i.global[idx] = val
		i.sp--
		frame.ip += 3
		return nil
	},
	instr.LOCAL_GET: func(i *Interpreter) error {
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(code[frame.ip+1])
		addr := frame.bp + idx
		if addr < 0 || addr > i.sp {
			return ErrSegmentationFault
		}
		val := i.stack[addr]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}
		i.stack[i.sp] = val
		i.sp++
		frame.ip += 2
		return nil
	},
	instr.LOCAL_SET: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		fn := frame.fn
		code := fn.Code
		idx := int(code[frame.ip+1])
		addr := frame.bp + idx
		if addr < 0 || addr > i.sp {
			return ErrSegmentationFault
		}
		val := i.stack[i.sp-1]
		if old := i.stack[addr]; old != val && old.Kind() == types.KindRef {
			i.release(old.Ref())
		}
		i.stack[addr] = val
		i.sp--
		frame.ip += 2
		return nil
	},
	instr.CONST_GET: func(i *Interpreter) error {
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.constants) {
			return ErrSegmentationFault
		}
		var val types.Boxed
		switch v := i.constants[idx].(type) {
		case types.Boxed:
			val = v
		case types.I32:
			val = types.BoxI32(int32(v))
		case types.I64:
			val = i.boxI64(int64(v))
		case types.F32:
			val = types.BoxF32(float32(v))
		case types.F64:
			val = types.BoxF64(float64(v))
		default:
			val = types.BoxRef(i.alloc(v))
		}
		i.stack[i.sp] = val
		i.sp++
		frame.ip += 3
		return nil
	},
	instr.REF_TEST: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		fn := frame.fn
		code := fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			return ErrSegmentationFault
		}
		typ := i.types[idx]
		val := i.stack[i.sp-1]
		var cond types.Boxed
		switch kind := val.Kind(); kind {
		case types.KindRef:
			ref := i.heap[val.Ref()]
			cond = types.BoxBool(ref.Type().Equals(typ))
		default:
			cond = types.BoxBool(kind == typ.Kind())
		}
		i.stack[i.sp-1] = cond
		frame.ip += 3
		return nil
	},
	instr.REF_CAST: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		fn := frame.fn
		code := fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			return ErrSegmentationFault
		}
		typ := i.types[idx]
		val := i.stack[i.sp-1]
		switch kind := val.Kind(); kind {
		case types.KindRef:
			ref := i.heap[val.Ref()]
			if !ref.Type().Cast(typ) {
				return ErrTypeMismatch
			}
		default:
			if kind != typ.Kind() {
				return ErrTypeMismatch
			}
		}
		frame.ip += 3
		return nil
	},
	instr.REF_EQ: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1]
		v2 := i.stack[i.sp-2]
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_CONST: func(i *Interpreter) error {
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		val := types.BoxI32(*(*int32)(unsafe.Pointer(&code[frame.ip+1])))
		i.stack[i.sp] = val
		i.sp++
		frame.ip += 5
		return nil
	},
	instr.I32_ADD: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 + v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_SUB: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 - v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_MUL: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 * v2)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_DIV_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 / v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_DIV_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v2) / uint32(v1)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_REM_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 % v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_REM_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v2) % uint32(v1)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_SHL: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32() & 0x1F
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 << v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_SHR_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32() & 0x1F
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 >> v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_SHR_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32() & 0x1F
		v2 := uint32(i.stack[i.sp-2].I32())
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(int32(v2 >> v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_XOR: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 ^ v2)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_AND: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 & v2)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_OR: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 | v2)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_EQZ: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		val := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxBool(val == 0)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_EQ: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_NE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_LT_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_LT_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) < uint32(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_GT_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_GT_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) > uint32(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_LE_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_LE_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) <= uint32(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_GE_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_GE_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) >= uint32(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_I64_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_I64_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := uint32(i.stack[i.sp-1].I32())
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F32_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F32_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF32(float32(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F64_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F64_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF64(float64(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_CONST: func(i *Interpreter) error {
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		val := i.boxI64(int64(*(*uint64)(unsafe.Pointer(&code[frame.ip+1]))))
		i.stack[i.sp] = val
		i.sp++
		frame.ip += 9
		return nil
	},
	instr.I64_ADD: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 + v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_SUB: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 - v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_MUL: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v1 * v2)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_DIV_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 / v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_DIV_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) / uint64(v1)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_REM_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 % v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_REM_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) % uint64(v1)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_SHL: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(v2 << (v1 & 0x3F)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_SHR_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(v2 >> (v1 & 0x3F)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_SHR_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) >> (v1 & 0x3F)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_EQZ: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		val := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxBool(val == 0)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_EQ: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_NE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_LT_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_LT_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) < uint64(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_GT_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_GT_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) > uint64(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_LE_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_LE_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) <= uint64(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_GE_S: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_GE_U: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) >= uint64(v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_I32: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F32_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F32_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF32(float32(uint64(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F64_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F64_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF64(float64(uint64(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_CONST: func(i *Interpreter) error {
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		i.stack[i.sp] = types.BoxF32(*(*float32)(unsafe.Pointer(&code[frame.ip+1])))
		i.sp++
		frame.ip += 5
		return nil
	},
	instr.F32_ADD: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v2 + v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_SUB: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v2 - v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_MUL: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v1 * v2)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_DIV: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v2 / v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_EQ: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_NE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_LT: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_GT: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_LE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_GE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_I32_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_I32_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_I64_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_I64_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = i.boxI64(int64(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_F64: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I32_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I32_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I64_S: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I64_U: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_F32: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_CONST: func(i *Interpreter) error {
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		i.stack[i.sp] = types.BoxF64(*(*float64)(unsafe.Pointer(&code[frame.ip+1])))
		i.sp++
		frame.ip += 9
		return nil
	},
	instr.F64_ADD: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v2 + v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_SUB: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v2 - v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_MUL: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v1 * v2)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_DIV: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		if v1 == 0 {
			return ErrDivideByZero
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v2 / v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_EQ: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_NE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_LT: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_GT: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_LE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_GE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_LEN: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		v, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		i.stack[i.sp-1] = types.BoxI32(int32(len(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_CONCAT: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxRef(i.alloc(v2 + v1))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_EQ: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_NE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_LT: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_GT: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_LE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRING_GE: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.ARRAY_NEW: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			return ErrSegmentationFault
		}
		typ, ok := i.types[idx].(*types.ArrayType)
		if !ok {
			return ErrTypeMismatch
		}
		size := int(i.stack[i.sp-1].I32())
		if i.sp < size+1 {
			return ErrStackUnderflow
		}
		var arr types.Value
		switch typ.ElemKind {
		case types.KindI32:
			val := make(types.I32Array, size)
			for j := 0; j < size; j++ {
				val[j] = types.I32(i.stack[i.sp-size-j-1].I32())
			}
			arr = val
		case types.KindI64:
			val := make(types.I64Array, size)
			for j := 0; j < size; j++ {
				val[j] = types.I64(i.unboxI64(i.stack[i.sp-size-j-1]))
			}
			arr = val
		case types.KindF32:
			val := make(types.F32Array, size)
			for j := 0; j < size; j++ {
				val[j] = types.F32(i.stack[i.sp-size-j-1].F32())
			}
			arr = val
		case types.KindF64:
			val := make(types.F64Array, size)
			for j := 0; j < size; j++ {
				val[j] = types.F64(i.stack[i.sp-size-j-1].F64())
			}
			arr = val
		default:
			val := &types.Array{
				Typ:   typ,
				Elems: make([]types.Boxed, size),
			}
			copy(val.Elems, i.stack[i.sp-size-1:i.sp-1])
			arr = val
		}
		i.sp -= size
		i.stack[i.sp-1] = types.BoxRef(i.alloc(arr))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.ARRAY_NEW_DEFAULT: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			return ErrSegmentationFault
		}
		typ, ok := i.types[idx].(*types.ArrayType)
		if !ok {
			return ErrTypeMismatch
		}
		size := i.stack[i.sp-1].I32()
		var arr types.Value
		switch typ.ElemKind {
		case types.KindI32:
			arr = make(types.I32Array, size)
		case types.KindI64:
			arr = make(types.I64Array, size)
		case types.KindF32:
			arr = make(types.F32Array, size)
		case types.KindF64:
			arr = make(types.F64Array, size)
		default:
			arr = &types.Array{
				Typ:   typ,
				Elems: make([]types.Boxed, size),
			}
		}
		i.stack[i.sp-1] = types.BoxRef(i.alloc(arr))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.ARRAY_GET: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		idx := int(i.stack[i.sp-1].I32())
		ref := i.stack[i.sp-2]
		if ref.Kind() != types.KindRef {
			return ErrTypeMismatch
		}
		addr := ref.Ref()
		var val types.Boxed
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			val = types.BoxI32(int32(arr[idx]))
		case types.I64Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			val = i.boxI64(int64(arr[idx]))
		case types.F32Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			val = types.BoxF32(float32(arr[idx]))
		case types.F64Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			val = types.BoxF64(float64(arr[idx]))
		case *types.Array:
			if idx < 0 || idx >= len(arr.Elems) {
				return ErrIndexOutOfRange
			}
			elem := arr.Elems[idx]
			if elem.Kind() == types.KindRef {
				i.retain(elem.Ref())
			}
			val = elem
		default:
			return ErrTypeMismatch
		}
		i.release(addr)
		i.sp--
		i.stack[i.sp-1] = val
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.ARRAY_SET: func(i *Interpreter) error {
		if i.sp < 3 {
			return ErrStackUnderflow
		}
		val := i.stack[i.sp-1]
		idx := int(i.stack[i.sp-2].I32())
		ref := i.stack[i.sp-3]
		if ref.Kind() != types.KindRef {
			return ErrTypeMismatch
		}
		addr := ref.Ref()
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			arr[idx] = types.I32(val.I32())
		case types.I64Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			arr[idx] = types.I64(i.unboxI64(val))
		case types.F32Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			arr[idx] = types.F32(val.F32())
		case types.F64Array:
			if idx < 0 || idx >= len(arr) {
				return ErrIndexOutOfRange
			}
			arr[idx] = types.F64(val.F64())
		case *types.Array:
			if idx < 0 || idx >= len(arr.Elems) {
				return ErrIndexOutOfRange
			}
			elem := arr.Elems[idx]
			arr.Elems[idx] = val
			if elem.Kind() == types.KindRef {
				i.release(elem.Ref())
			}
		default:
			return ErrTypeMismatch
		}
		i.release(addr)
		i.sp -= 3
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.ARRAY_FILL: func(i *Interpreter) error {
		if i.sp < 4 {
			return ErrStackUnderflow
		}
		size := int(i.stack[i.sp-1].I32())
		val := i.stack[i.sp-2]
		idx := int(i.stack[i.sp-3].I32())
		ref := i.stack[i.sp-4]
		if ref.Kind() != types.KindRef {
			return ErrTypeMismatch
		}
		addr := ref.Ref()
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if idx < 0 || idx+size > len(arr) {
				return ErrIndexOutOfRange
			}
			v := types.I32(val.I32())
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case types.I64Array:
			if idx < 0 || idx+size > len(arr) {
				return ErrIndexOutOfRange
			}
			v := types.I64(i.unboxI64(val))
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case types.F32Array:
			if idx < 0 || idx+size > len(arr) {
				return ErrIndexOutOfRange
			}
			v := types.F32(val.F32())
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case types.F64Array:
			if idx < 0 || idx+size > len(arr) {
				return ErrIndexOutOfRange
			}
			v := types.F64(val.F64())
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case *types.Array:
			if idx < 0 || idx+size > len(arr.Elems) {
				return ErrIndexOutOfRange
			}
			elem := arr.Elems[idx]
			for i := idx; i < idx+size; i++ {
				arr.Elems[i] = val
			}
			if val.Kind() == types.KindRef {
				i.retains(val.Ref(), size-1)
			}
			if elem.Kind() == types.KindRef {
				i.release(elem.Ref())
			}
		default:
			return ErrTypeMismatch
		}
		i.release(addr)
		i.sp -= 4
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.ARRAY_COPY: func(i *Interpreter) error {
		if i.sp < 4 {
			return ErrStackUnderflow
		}
		size := int(i.stack[i.sp-1].I32())
		src := int(i.stack[i.sp-2].I32())
		dst := int(i.stack[i.sp-3].I32())
		ref := i.stack[i.sp-4]
		if ref.Kind() != types.KindRef {
			return ErrTypeMismatch
		}
		addr := ref.Ref()
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				return ErrIndexOutOfRange
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case types.I64Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				return ErrIndexOutOfRange
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case types.F32Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				return ErrIndexOutOfRange
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case types.F64Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				return ErrIndexOutOfRange
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case *types.Array:
			if src < 0 || dst < 0 || src+size > len(arr.Elems) || dst+size > len(arr.Elems) {
				return ErrIndexOutOfRange
			}
			elems := arr.Elems
			for _, v := range elems[src : src+size] {
				if v.Kind() == types.KindRef {
					i.retain(v.Ref())
				}
			}
			for _, v := range elems[dst : dst+size] {
				if v.Kind() == types.KindRef {
					i.release(v.Ref())
				}
			}
			copy(elems[dst:dst+size], elems[src:src+size])
		default:
			return ErrTypeMismatch
		}
		i.release(addr)
		i.sp -= 4
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRUCT_NEW: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			return ErrSegmentationFault
		}
		typ, ok := i.types[idx].(*types.StructType)
		if !ok {
			return ErrTypeMismatch
		}
		size := len(typ.Fields)
		if i.sp < size {
			return ErrStackUnderflow
		}
		s := types.NewStruct(typ)
		for j, f := range typ.Fields {
			offset := f.Offset
			val := i.stack[i.sp-size-j]
			switch f.Kind {
			case types.KindI32:
				*(*int32)(unsafe.Pointer(&s.Data[offset])) = val.I32()
			case types.KindI64:
				*(*int64)(unsafe.Pointer(&s.Data[offset])) = i.unboxI64(val)
			case types.KindF32:
				*(*float32)(unsafe.Pointer(&s.Data[offset])) = val.F32()
			case types.KindF64:
				*(*float64)(unsafe.Pointer(&s.Data[offset])) = val.F64()
			case types.KindRef:
				*(*uint64)(unsafe.Pointer(&s.Data[offset])) = uint64(val)
			default:
				return ErrTypeMismatch
			}
		}
		i.sp -= size - 1
		i.stack[i.sp-1] = types.BoxRef(i.alloc(s))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRUCT_NEW_DEFAULT: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(*(*uint16)(unsafe.Pointer(&code[frame.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			return ErrSegmentationFault
		}
		typ, ok := i.types[idx].(*types.StructType)
		if !ok {
			return ErrTypeMismatch
		}
		s := types.NewStruct(typ)
		i.sp++
		i.stack[i.sp-1] = types.BoxRef(i.alloc(s))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRUCT_GET: func(i *Interpreter) error {
		if i.sp < 2 {
			return ErrStackUnderflow
		}
		idx := int(i.stack[i.sp-1].I32())
		ref := i.stack[i.sp-2]
		if ref.Kind() != types.KindRef {
			return ErrTypeMismatch
		}
		addr := ref.Ref()
		s, ok := i.heap[addr].(*types.Struct)
		if !ok {
			return ErrTypeMismatch
		}
		typ := s.Typ
		if idx < 0 || idx >= len(typ.Fields) {
			return ErrSegmentationFault
		}
		f := typ.Fields[idx]
		offset := f.Offset
		var val types.Boxed
		switch f.Kind {
		case types.KindI32:
			val = types.BoxI32(*(*int32)(unsafe.Pointer(&s.Data[offset])))
		case types.KindI64:
			val = i.boxI64(*(*int64)(unsafe.Pointer(&s.Data[offset])))
		case types.KindF32:
			val = types.BoxF32(*(*float32)(unsafe.Pointer(&s.Data[offset])))
		case types.KindF64:
			val = types.BoxF64(*(*float64)(unsafe.Pointer(&s.Data[offset])))
		case types.KindRef:
			val = types.Boxed(*(*uint64)(unsafe.Pointer(&s.Data[offset])))
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
		default:
			return ErrTypeMismatch
		}
		i.release(addr)
		i.sp--
		i.stack[i.sp-1] = val
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.STRUCT_SET: func(i *Interpreter) error {
		if i.sp < 3 {
			return ErrStackUnderflow
		}
		val := i.stack[i.sp-1]
		idx := int(i.stack[i.sp-2].I32())
		ref := i.stack[i.sp-3]
		if ref.Kind() != types.KindRef {
			return ErrTypeMismatch
		}
		addr := ref.Ref()
		s, ok := i.heap[addr].(*types.Struct)
		if !ok {
			return ErrTypeMismatch
		}
		typ := s.Typ
		if idx < 0 || idx >= len(typ.Fields) {
			return ErrSegmentationFault
		}
		f := typ.Fields[idx]
		offset := f.Offset
		switch f.Kind {
		case types.KindI32:
			*(*int32)(unsafe.Pointer(&s.Data[offset])) = val.I32()
		case types.KindI64:
			*(*int64)(unsafe.Pointer(&s.Data[offset])) = i.unboxI64(val)
		case types.KindF32:
			*(*float32)(unsafe.Pointer(&s.Data[offset])) = val.F32()
		case types.KindF64:
			*(*float64)(unsafe.Pointer(&s.Data[offset])) = val.F64()
		case types.KindRef:
			ptr := (*uint64)(unsafe.Pointer(&s.Data[offset]))
			if old := types.Boxed(*ptr); old.Kind() == types.KindRef {
				i.release(old.Ref())
			}
			*ptr = uint64(val)
		default:
			return ErrTypeMismatch
		}
		i.release(addr)
		i.sp -= 3
		i.frames[i.fp-1].ip++
		return nil
	},
}

func init() {
	for i, fn := range dispatch {
		if fn == nil {
			dispatch[i] = func(i *Interpreter) error {
				return ErrUnknownOpcode
			}
		}
	}
}
