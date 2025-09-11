package interp

import (
	"encoding/binary"
	"math"

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
		offset := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		frame.ip += offset + 5
		return nil
	},
	instr.BR_IF: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		i.sp--
		cond := i.stack[i.sp].I32()
		if cond != 0 {
			offset := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			frame.ip += offset
		}
		frame.ip += 5
		return nil
	},
	instr.CALL: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		i.sp--
		if i.fp == len(i.frames) {
			return ErrFrameOverflow
		}

		addr := i.stack[i.sp].Ref()
		fn, ok := i.heap[addr].(*types.Function)
		if !ok {
			return ErrTypeMismatch
		}
		params := len(fn.Params)
		locals := len(fn.Locals)

		frame := &i.frames[i.fp]
		frame.addr = addr
		frame.fn = fn
		frame.ip = 0
		frame.bp = i.sp - params

		i.fp++
		for idx := 0; idx < locals; idx++ {
			i.stack[frame.bp+params+idx] = 0
		}
		sp := frame.bp + params + locals
		if sp == len(i.stack) {
			return ErrStackOverflow
		}
		i.sp = sp
		i.frames[i.fp-2].ip++
		return nil
	},
	instr.RETURN: func(i *Interpreter) error {
		if i.fp == 1 {
			return ErrFrameUnderflow
		}

		frame := &i.frames[i.fp-1]
		fn := frame.fn
		returns := len(fn.Returns)
		if i.sp < returns {
			return ErrStackUnderflow
		}

		copy(i.stack[frame.bp:frame.bp+returns], i.stack[i.sp-returns:i.sp])
		i.sp = frame.bp + returns

		if frame.addr > 0 {
			i.release(frame.addr)
		}
		frame.fn = nil
		i.fp--
		return nil
	},
	instr.GLOBAL_GET: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 || idx >= len(i.global) {
			return ErrSegmentationFault
		}

		val := i.global[idx]
		if val.Kind() == types.KindRef {
			i.retain(val.Ref())
		}

		i.stack[i.sp] = val
		i.sp++
		frame.ip += 5
		return nil
	},
	instr.GLOBAL_SET: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
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
		frame.ip += 5
		return nil
	},
	instr.LOCAL_GET: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
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
		frame.ip += 5
		return nil
	},
	instr.LOCAL_SET: func(i *Interpreter) error {
		if i.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &i.frames[i.fp-1]
		fn := frame.fn
		code := fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 || idx >= len(fn.Params)+len(fn.Locals) {
			return ErrSegmentationFault
		}
		val := i.stack[i.sp-1]
		addr := frame.bp + idx
		if addr < 0 || addr > i.sp {
			return ErrSegmentationFault
		}
		if old := i.stack[addr]; old != val && old.Kind() == types.KindRef {
			i.release(old.Ref())
		}
		i.stack[addr] = val
		i.sp--
		frame.ip += 5
		return nil
	},
	instr.FN_CONST: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 || idx >= len(i.constants) {
			return ErrSegmentationFault
		}
		fn, ok := i.constants[idx].(*types.Function)
		if !ok {
			return ErrTypeMismatch
		}
		addr := i.alloc(fn)
		i.stack[i.sp] = types.BoxRef(addr)
		i.sp++
		frame.ip += 5
		return nil
	},
	instr.I32_CONST: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		val := types.BoxI32(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
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
		i.stack[i.sp-1] = types.BoxI32(v1 + v2)
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
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_I64_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := uint32(i.stack[i.sp-1].I32())
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F32_S: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F32_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF32(float32(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F64_S: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I32_TO_F64_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = types.BoxF64(float64(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_CONST: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		val := i.boxI64(int64(binary.BigEndian.Uint64(code[frame.ip+1:])))
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
		i.stack[i.sp-1] = i.boxI64(v1 + v2)
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
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F32_S: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F32_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF32(float32(uint64(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F64_S: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.I64_TO_F64_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = types.BoxF64(float64(uint64(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_CONST: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		val := types.BoxF32(math.Float32frombits(binary.BigEndian.Uint32(code[frame.ip+1:])))
		i.stack[i.sp] = val
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
		i.stack[i.sp-1] = types.BoxF32(v1 + v2)
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
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_I32_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_I64_S: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_I64_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = i.boxI64(int64(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F32_TO_F64: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = types.BoxF64(float64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I32_S: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxI32(int32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I32_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I64_S: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = i.boxI64(int64(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_I64_U: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = i.boxI64(int64(uint64(v)))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_TO_F32: func(i *Interpreter) error {
		if i.sp < 1 {
			return ErrStackUnderflow
		}
		v := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = types.BoxF32(float32(v))
		i.frames[i.fp-1].ip++
		return nil
	},
	instr.F64_CONST: func(i *Interpreter) error {
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		val := types.BoxF64(math.Float64frombits(binary.BigEndian.Uint64(code[frame.ip+1:])))
		i.stack[i.sp] = val
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
		i.stack[i.sp-1] = types.BoxF64(v1 + v2)
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
}
