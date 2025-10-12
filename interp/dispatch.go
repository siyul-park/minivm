package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

var dispatch = [256]func(i *Interpreter){
	instr.NOP: func(i *Interpreter) {
		i.frames[i.fp-1].ip++
	},
	instr.UNREACHABLE: func(i *Interpreter) {
		i.frames[i.fp-1].ip++
		panic(ErrUnreachableExecuted)
	},
	instr.DROP: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		val := i.stack[i.sp-1]
		if val.Kind() == types.KindRef {
			i.release(val.Ref())
		}
		i.sp--
		i.frames[i.fp-1].ip++
	},
	instr.DUP: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		val := i.stack[i.sp-1]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}
		i.stack[i.sp] = val
		i.sp++
		i.frames[i.fp-1].ip++
	},
	instr.SWAP: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		i.stack[i.sp-1], i.stack[i.sp-2] = i.stack[i.sp-2], i.stack[i.sp-1]
		i.frames[i.fp-1].ip++
	},
	instr.BR: func(i *Interpreter) {
		f := &i.frames[i.fp-1]
		offset := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		f.ip += offset + 3
	},
	instr.BR_IF: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		i.sp--
		cond := i.stack[i.sp].I32()
		if cond != 0 {
			offset := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
			f.ip += offset
		}
		f.ip += 3
	},
	instr.BR_TABLE: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		count := int(f.code[f.ip+1])
		i.sp--
		cond := int(i.stack[i.sp].I32())
		if cond > count {
			cond = count + 1
		}
		offset := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+cond*2+2])))
		f.ip += offset + count*2 + 4
	},
	instr.SELECT: func(i *Interpreter) {
		if i.sp < 3 {
			panic(ErrStackUnderflow)
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
	},
	instr.CALL: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		if i.fp == len(i.frames) {
			panic(ErrFrameOverflow)
		}
		addr := i.stack[i.sp-1].Ref()
		switch fn := i.heap[addr].(type) {
		case *types.Function:
			if i.sp <= fn.Params {
				panic(ErrStackUnderflow)
			}
			if i.sp+fn.Locals-fn.Params-1 >= len(i.stack) {
				panic(ErrStackOverflow)
			}
			for idx := 0; idx < fn.Locals-fn.Params; idx++ {
				i.stack[i.sp+idx-1] = 0
			}
			f := &i.frames[i.fp]
			f.code = fn.Code
			f.addr = addr
			f.ip = 0
			f.bp = i.sp - fn.Params - 1
			i.sp = f.bp + fn.Locals
			i.fp++
			i.frames[i.fp-2].ip++
		case *NativeFunction:
			if i.sp <= fn.Params {
				panic(ErrStackUnderflow)
			}
			if i.sp+fn.Returns-fn.Params-1 >= len(i.stack) {
				panic(ErrStackOverflow)
			}
			params := i.stack[i.sp-fn.Params : i.sp]
			returns, err := fn.Fn(i, params)
			if err != nil {
				panic(err)
			}
			for _, val := range params {
				if val.Kind() != types.KindRef {
					continue
				}
				ok := false
				for _, r := range returns {
					if r == val {
						ok = true
						break
					}
				}
				if !ok {
					i.release(val.Ref())
				}
			}
			i.sp -= fn.Params
			copy(i.stack[i.sp-fn.Returns:i.sp], returns)
			i.sp += fn.Returns - 1
			i.frames[i.fp-1].ip++
		default:
			panic(ErrTypeMismatch)
		}
	},
	instr.RETURN: func(i *Interpreter) {
		if i.fp == 1 {
			panic(ErrFrameUnderflow)
		}
		f := &i.frames[i.fp-1]
		fn := i.heap[f.addr].(*types.Function)
		if i.sp < fn.Returns {
			panic(ErrStackUnderflow)
		}
		copy(i.stack[f.bp:f.bp+fn.Returns], i.stack[i.sp-fn.Returns:i.sp])
		i.sp = f.bp + fn.Returns
		i.release(f.addr)
		f.code = nil
		i.fp--
	},
	instr.GLOBAL_GET: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.globals) {
			panic(ErrSegmentationFault)
		}
		val := i.globals[idx]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}
		i.stack[i.sp] = val
		i.sp++
		f.ip += 3
	},
	instr.GLOBAL_SET: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 {
			panic(ErrSegmentationFault)
		}
		val := i.stack[i.sp-1]
		if idx >= len(i.globals) {
			if cap(i.globals) > idx {
				i.globals = i.globals[:idx+1]
			} else {
				global := make([]types.Boxed, idx*2)
				copy(global, i.globals)
				i.globals = global[:idx+1]
			}
		}
		if old := i.globals[idx]; old != val && old.Kind() == types.KindRef {
			i.release(old.Ref())
		}
		i.globals[idx] = val
		i.sp--
		f.ip += 3
	},
	instr.GLOBAL_TEE: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 {
			panic(ErrSegmentationFault)
		}
		val := i.stack[i.sp-1]
		if idx >= len(i.globals) {
			if cap(i.globals) > idx {
				i.globals = i.globals[:idx+1]
			} else {
				global := make([]types.Boxed, idx*2)
				copy(global, i.globals)
				i.globals = global[:idx+1]
			}
		}
		if old := i.globals[idx]; old != val && old.Kind() == types.KindRef {
			i.release(old.Ref())
		}
		i.globals[idx] = val
		f.ip += 3
	},
	instr.LOCAL_GET: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(f.code[f.ip+1])
		addr := f.bp + idx
		if addr < 0 || addr > i.sp {
			panic(ErrSegmentationFault)
		}
		val := i.stack[addr]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}
		i.stack[i.sp] = val
		i.sp++
		f.ip += 2
	},
	instr.LOCAL_SET: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(f.code[f.ip+1])
		addr := f.bp + idx
		if addr < 0 || addr > i.sp {
			panic(ErrSegmentationFault)
		}
		val := i.stack[i.sp-1]
		if old := i.stack[addr]; old != val && old.Kind() == types.KindRef {
			i.release(old.Ref())
		}
		i.stack[addr] = val
		i.sp--
		f.ip += 2
	},
	instr.LOCAL_TEE: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(f.code[f.ip+1])
		addr := f.bp + idx
		if addr < 0 || addr > i.sp {
			panic(ErrSegmentationFault)
		}
		val := i.stack[i.sp-1]
		if old := i.stack[addr]; old != val && old.Kind() == types.KindRef {
			i.release(old.Ref())
		}
		i.stack[addr] = val
		f.ip += 2
	},
	instr.CONST_GET: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		f := &i.frames[i.fp-1]

		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.constants) {
			panic(ErrSegmentationFault)
		}
		val := i.constants[idx]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}
		i.stack[i.sp] = val
		i.sp++
		f.ip += 3
	},
	instr.REF_NULL: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		i.retain(0)
		i.stack[i.sp] = types.BoxedNull
		i.sp++
		i.frames[i.fp-1].ip++
	},
	instr.REF_TEST: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			panic(ErrSegmentationFault)
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
		f.ip += 3
	},
	instr.REF_CAST: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			panic(ErrSegmentationFault)
		}
		typ := i.types[idx]
		val := i.stack[i.sp-1]
		switch kind := val.Kind(); kind {
		case types.KindRef:
			ref := i.heap[val.Ref()]
			if !ref.Type().Cast(typ) {
				panic(ErrTypeMismatch)
			}
		default:
			if kind != typ.Kind() {
				panic(ErrTypeMismatch)
			}
		}
		f.ip += 3
	},
	instr.REF_IS_NULL: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		val := i.stack[i.sp-1]
		i.stack[i.sp-1] = types.BoxBool(val.Ref() == 0)
		i.frames[i.fp-1].ip++
	},
	instr.REF_EQ: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1]
		v2 := i.stack[i.sp-2]
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
	},
	instr.REF_NE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1]
		v2 := i.stack[i.sp-2]
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_CONST: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		f := &i.frames[i.fp-1]
		val := types.BoxI32(*(*int32)(unsafe.Pointer(&f.code[f.ip+1])))
		i.stack[i.sp] = val
		i.sp++
		f.ip += 5
	},
	instr.I32_ADD: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 + v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_SUB: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 - v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_MUL: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 * v2)
		i.frames[i.fp-1].ip++
	},
	instr.I32_DIV_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 / v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_DIV_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v2) / uint32(v1)))
		i.frames[i.fp-1].ip++
	},
	instr.I32_REM_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 % v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_REM_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v2) % uint32(v1)))
		i.frames[i.fp-1].ip++
	},
	instr.I32_SHL: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32() & 0x1F
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 << v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_SHR_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32() & 0x1F
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v2 >> v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_SHR_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32() & 0x1F
		v2 := uint32(i.stack[i.sp-2].I32())
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(int32(v2 >> v1))
		i.frames[i.fp-1].ip++
	},
	instr.I32_XOR: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 ^ v2)
		i.frames[i.fp-1].ip++
	},
	instr.I32_AND: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 & v2)
		i.frames[i.fp-1].ip++
	},
	instr.I32_OR: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxI32(v1 | v2)
		i.frames[i.fp-1].ip++
	},
	instr.I32_EQZ: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		val := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxBool(val == 0)
		i.frames[i.fp-1].ip++
	},
	instr.I32_EQ: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_NE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_LT_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_LT_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) < uint32(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I32_GT_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_GT_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) > uint32(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I32_LE_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_LE_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) <= uint32(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I32_GE_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
	},
	instr.I32_GE_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].I32()
		v2 := i.stack[i.sp-2].I32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint32(v2) >= uint32(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I32_TO_I64_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
	},
	instr.I32_TO_I64_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := uint32(i.stack[i.sp-1].I32())
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
	},
	instr.I32_TO_F32_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
	},
	instr.I32_TO_F32_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF32(float32(uint32(v)))
		i.frames[i.fp-1].ip++
	},
	instr.I32_TO_F64_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
	},
	instr.I32_TO_F64_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF64(float64(uint32(v)))
		i.frames[i.fp-1].ip++
	},
	instr.I64_CONST: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		f := &i.frames[i.fp-1]

		val := i.boxI64(int64(*(*uint64)(unsafe.Pointer(&f.code[f.ip+1]))))
		i.stack[i.sp] = val
		i.sp++
		f.ip += 9
	},
	instr.I64_ADD: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 + v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_SUB: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 - v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_MUL: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v1 * v2)
		i.frames[i.fp-1].ip++
	},
	instr.I64_DIV_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 / v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_DIV_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) / uint64(v1)))
		i.frames[i.fp-1].ip++
	},
	instr.I64_REM_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(v2 % v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_REM_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) % uint64(v1)))
		i.frames[i.fp-1].ip++
	},
	instr.I64_SHL: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(v2 << (v1 & 0x3F)))
		i.frames[i.fp-1].ip++
	},
	instr.I64_SHR_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(v2 >> (v1 & 0x3F)))
		i.frames[i.fp-1].ip++
	},
	instr.I64_SHR_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) >> (v1 & 0x3F)))
		i.frames[i.fp-1].ip++
	},
	instr.I64_EQZ: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		val := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxBool(val == 0)
		i.frames[i.fp-1].ip++
	},
	instr.I64_EQ: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_NE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_LT_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_LT_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) < uint64(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I64_GT_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_GT_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) > uint64(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I64_LE_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_LE_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) <= uint64(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I64_GE_S: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
	},
	instr.I64_GE_U: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.unboxI64(i.stack[i.sp-1])
		v2 := i.unboxI64(i.stack[i.sp-2])
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(uint64(v2) >= uint64(v1))
		i.frames[i.fp-1].ip++
	},
	instr.I64_TO_I32: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
	},
	instr.I64_TO_F32_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
	},
	instr.I64_TO_F32_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF32(float32(uint64(v)))
		i.frames[i.fp-1].ip++
	},
	instr.I64_TO_F64_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
	},
	instr.I64_TO_F64_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF64(float64(uint64(v)))
		i.frames[i.fp-1].ip++
	},
	instr.F32_CONST: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		f := &i.frames[i.fp-1]
		i.stack[i.sp] = types.BoxF32(*(*float32)(unsafe.Pointer(&f.code[f.ip+1])))
		i.sp++
		f.ip += 5
	},
	instr.F32_ADD: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v2 + v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_SUB: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v2 - v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_MUL: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v1 * v2)
		i.frames[i.fp-1].ip++
	},
	instr.F32_DIV: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxF32(v2 / v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_EQ: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_NE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_LT: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_GT: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_LE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_GE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F32()
		v2 := i.stack[i.sp-2].F32()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
	},
	instr.F32_TO_I32_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
	},
	instr.F32_TO_I32_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
		i.frames[i.fp-1].ip++
	},
	instr.F32_TO_I64_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
	},
	instr.F32_TO_I64_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = i.boxI64(int64(uint32(v)))
		i.frames[i.fp-1].ip++
	},
	instr.F32_TO_F64: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
	},
	instr.F64_TO_I32_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
	},
	instr.F64_TO_I32_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
		i.frames[i.fp-1].ip++
	},
	instr.F64_TO_I64_S: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
	},
	instr.F64_TO_I64_U: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v)))
		i.frames[i.fp-1].ip++
	},
	instr.F64_TO_F32: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
	},
	instr.F64_CONST: func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		f := &i.frames[i.fp-1]
		i.stack[i.sp] = types.BoxF64(*(*float64)(unsafe.Pointer(&f.code[f.ip+1])))
		i.sp++
		f.ip += 9
	},
	instr.F64_ADD: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v2 + v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_SUB: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v2 - v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_MUL: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v1 * v2)
		i.frames[i.fp-1].ip++
	},
	instr.F64_DIV: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		if v1 == 0 {
			panic(ErrDivideByZero)
		}
		i.sp--
		i.stack[i.sp-1] = types.BoxF64(v2 / v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_EQ: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_NE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_LT: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_GT: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_LE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
	},
	instr.F64_GE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1 := i.stack[i.sp-1].F64()
		v2 := i.stack[i.sp-2].F64()
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
	},
	instr.STRING_NEW_UTF32: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		val, _ := i.unbox(i.stack[i.sp-1]).(types.I32Array)
		i.stack[i.sp-1] = types.BoxRef(i.alloc(types.String(val)))
		i.frames[i.fp-1].ip++
	},
	instr.STRING_LEN: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		v, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		i.stack[i.sp-1] = types.BoxI32(int32(len(v)))
		i.frames[i.fp-1].ip++
	},
	instr.STRING_CONCAT: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxRef(i.alloc(v2 + v1))
		i.frames[i.fp-1].ip++
	},
	instr.STRING_EQ: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 == v1)
		i.frames[i.fp-1].ip++
	},
	instr.STRING_NE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 != v1)
		i.frames[i.fp-1].ip++
	},
	instr.STRING_LT: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 < v1)
		i.frames[i.fp-1].ip++
	},
	instr.STRING_GT: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 > v1)
		i.frames[i.fp-1].ip++
	},
	instr.STRING_LE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
		i.frames[i.fp-1].ip++
	},
	instr.STRING_GE: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
		i.sp--
		i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
		i.frames[i.fp-1].ip++
	},
	instr.STRING_ENCODE_UTF32: func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		val, _ := i.unbox(i.stack[i.sp-1]).(types.String)
		i.stack[i.sp-1] = types.BoxRef(i.alloc(types.I32Array(val)))
		i.frames[i.fp-1].ip++
	},
	instr.ARRAY_NEW: func(i *Interpreter) {
		if i.sp < 1 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			panic(ErrSegmentationFault)
		}
		typ, ok := i.types[idx].(*types.ArrayType)
		if !ok {
			panic(ErrTypeMismatch)
		}
		size := int(i.stack[i.sp-1].I32())
		if i.sp < size+1 {
			panic(ErrStackUnderflow)
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
	},
	instr.ARRAY_NEW_DEFAULT: func(i *Interpreter) {
		if i.sp < 1 {
			panic(ErrStackUnderflow)
		}
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			panic(ErrSegmentationFault)
		}
		typ, ok := i.types[idx].(*types.ArrayType)
		if !ok {
			panic(ErrTypeMismatch)
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
	},
	instr.ARRAY_GET: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		idx := int(i.stack[i.sp-1].I32())
		ref := i.stack[i.sp-2]
		if ref.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		addr := ref.Ref()
		var val types.Boxed
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			val = types.BoxI32(int32(arr[idx]))
		case types.I64Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			val = i.boxI64(int64(arr[idx]))
		case types.F32Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			val = types.BoxF32(float32(arr[idx]))
		case types.F64Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			val = types.BoxF64(float64(arr[idx]))
		case *types.Array:
			if idx < 0 || idx >= len(arr.Elems) {
				panic(ErrIndexOutOfRange)
			}
			elem := arr.Elems[idx]
			if elem.Kind() == types.KindRef {
				i.retain(elem.Ref())
			}
			val = elem
		default:
			panic(ErrTypeMismatch)
		}
		i.release(addr)
		i.sp--
		i.stack[i.sp-1] = val
		i.frames[i.fp-1].ip++
	},
	instr.ARRAY_SET: func(i *Interpreter) {
		if i.sp < 3 {
			panic(ErrStackUnderflow)
		}
		val := i.stack[i.sp-1]
		idx := int(i.stack[i.sp-2].I32())
		ref := i.stack[i.sp-3]
		if ref.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		addr := ref.Ref()
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			arr[idx] = types.I32(val.I32())
		case types.I64Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			arr[idx] = types.I64(i.unboxI64(val))
		case types.F32Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			arr[idx] = types.F32(val.F32())
		case types.F64Array:
			if idx < 0 || idx >= len(arr) {
				panic(ErrIndexOutOfRange)
			}
			arr[idx] = types.F64(val.F64())
		case *types.Array:
			if idx < 0 || idx >= len(arr.Elems) {
				panic(ErrIndexOutOfRange)
			}
			elem := arr.Elems[idx]
			arr.Elems[idx] = val
			if elem.Kind() == types.KindRef {
				i.release(elem.Ref())
			}
		default:
			panic(ErrTypeMismatch)
		}
		i.release(addr)
		i.sp -= 3
		i.frames[i.fp-1].ip++
	},
	instr.ARRAY_FILL: func(i *Interpreter) {
		if i.sp < 4 {
			panic(ErrStackUnderflow)
		}
		size := int(i.stack[i.sp-1].I32())
		val := i.stack[i.sp-2]
		idx := int(i.stack[i.sp-3].I32())
		ref := i.stack[i.sp-4]
		if ref.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		addr := ref.Ref()
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if idx < 0 || idx+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			v := types.I32(val.I32())
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case types.I64Array:
			if idx < 0 || idx+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			v := types.I64(i.unboxI64(val))
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case types.F32Array:
			if idx < 0 || idx+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			v := types.F32(val.F32())
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case types.F64Array:
			if idx < 0 || idx+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			v := types.F64(val.F64())
			for i := idx; i < idx+size; i++ {
				arr[i] = v
			}
		case *types.Array:
			if idx < 0 || idx+size > len(arr.Elems) {
				panic(ErrIndexOutOfRange)
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
			panic(ErrTypeMismatch)
		}
		i.release(addr)
		i.sp -= 4
		i.frames[i.fp-1].ip++
	},
	instr.ARRAY_COPY: func(i *Interpreter) {
		if i.sp < 4 {
			panic(ErrStackUnderflow)
		}
		size := int(i.stack[i.sp-1].I32())
		src := int(i.stack[i.sp-2].I32())
		dst := int(i.stack[i.sp-3].I32())
		ref := i.stack[i.sp-4]
		if ref.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		addr := ref.Ref()
		switch arr := i.heap[addr].(type) {
		case types.I32Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case types.I64Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case types.F32Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case types.F64Array:
			if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
				panic(ErrIndexOutOfRange)
			}
			copy(arr[dst:dst+size], arr[src:src+size])
		case *types.Array:
			if src < 0 || dst < 0 || src+size > len(arr.Elems) || dst+size > len(arr.Elems) {
				panic(ErrIndexOutOfRange)
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
			panic(ErrTypeMismatch)
		}
		i.release(addr)
		i.sp -= 4
		i.frames[i.fp-1].ip++
	},
	instr.STRUCT_NEW: func(i *Interpreter) {
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			panic(ErrSegmentationFault)
		}
		typ, ok := i.types[idx].(*types.StructType)
		if !ok {
			panic(ErrTypeMismatch)
		}
		size := len(typ.Fields)
		if i.sp < size {
			panic(ErrStackUnderflow)
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
				panic(ErrTypeMismatch)
			}
		}
		i.sp -= size - 1
		i.stack[i.sp-1] = types.BoxRef(i.alloc(s))
		i.frames[i.fp-1].ip++
	},
	instr.STRUCT_NEW_DEFAULT: func(i *Interpreter) {
		f := &i.frames[i.fp-1]
		idx := int(*(*uint16)(unsafe.Pointer(&f.code[f.ip+1])))
		if idx < 0 || idx >= len(i.types) {
			panic(ErrSegmentationFault)
		}
		typ, ok := i.types[idx].(*types.StructType)
		if !ok {
			panic(ErrTypeMismatch)
		}
		s := types.NewStruct(typ)
		i.sp++
		i.stack[i.sp-1] = types.BoxRef(i.alloc(s))
		i.frames[i.fp-1].ip++
	},
	instr.STRUCT_GET: func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		idx := int(i.stack[i.sp-1].I32())
		ref := i.stack[i.sp-2]
		if ref.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		addr := ref.Ref()
		s, ok := i.heap[addr].(*types.Struct)
		if !ok {
			panic(ErrTypeMismatch)
		}
		typ := s.Typ
		if idx < 0 || idx >= len(typ.Fields) {
			panic(ErrSegmentationFault)
		}
		field := typ.Fields[idx]
		var val types.Boxed
		switch field.Kind {
		case types.KindI32:
			val = types.BoxI32(*(*int32)(unsafe.Pointer(&s.Data[field.Offset])))
		case types.KindI64:
			val = i.boxI64(*(*int64)(unsafe.Pointer(&s.Data[field.Offset])))
		case types.KindF32:
			val = types.BoxF32(*(*float32)(unsafe.Pointer(&s.Data[field.Offset])))
		case types.KindF64:
			val = types.BoxF64(*(*float64)(unsafe.Pointer(&s.Data[field.Offset])))
		case types.KindRef:
			val = types.Boxed(*(*uint64)(unsafe.Pointer(&s.Data[field.Offset])))
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
		default:
			panic(ErrTypeMismatch)
		}
		i.release(addr)
		i.sp--
		i.stack[i.sp-1] = val
		i.frames[i.fp-1].ip++
	},
	instr.STRUCT_SET: func(i *Interpreter) {
		if i.sp < 3 {
			panic(ErrStackUnderflow)
		}
		val := i.stack[i.sp-1]
		idx := int(i.stack[i.sp-2].I32())
		ref := i.stack[i.sp-3]
		if ref.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		addr := ref.Ref()
		s, ok := i.heap[addr].(*types.Struct)
		if !ok {
			panic(ErrTypeMismatch)
		}
		typ := s.Typ
		if idx < 0 || idx >= len(typ.Fields) {
			panic(ErrSegmentationFault)
		}
		field := typ.Fields[idx]
		switch field.Kind {
		case types.KindI32:
			*(*int32)(unsafe.Pointer(&s.Data[field.Offset])) = val.I32()
		case types.KindI64:
			*(*int64)(unsafe.Pointer(&s.Data[field.Offset])) = i.unboxI64(val)
		case types.KindF32:
			*(*float32)(unsafe.Pointer(&s.Data[field.Offset])) = val.F32()
		case types.KindF64:
			*(*float64)(unsafe.Pointer(&s.Data[field.Offset])) = val.F64()
		case types.KindRef:
			ptr := (*uint64)(unsafe.Pointer(&s.Data[field.Offset]))
			if old := types.Boxed(*ptr); old.Kind() == types.KindRef {
				i.release(old.Ref())
			}
			*ptr = uint64(val)
		default:
			panic(ErrTypeMismatch)
		}
		i.release(addr)
		i.sp -= 3
		i.frames[i.fp-1].ip++
	},
}

func init() {
	for i, fn := range dispatch {
		if fn == nil {
			dispatch[i] = func(i *Interpreter) {
				panic(ErrUnknownOpcode)
			}
		}
	}
}
