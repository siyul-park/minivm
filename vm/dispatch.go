package vm

import (
	"encoding/binary"
	"math"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

var dispatch = [256]func(vm *VM) error{
	instr.NOP: func(vm *VM) error {
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.UNREACHABLE: func(vm *VM) error {
		vm.frames[vm.fp-1].ip++
		return ErrUnreachableExecuted
	},
	instr.DROP: func(vm *VM) error {
		val := vm.stack[vm.sp-1]
		if val.Kind() == types.KindRef {
			vm.release(val.Ref())
		}
		vm.sp--
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.DUP: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}
		val := vm.stack[vm.sp-1]
		if val.Kind() == types.KindRef {
			vm.retain(val.Ref())
		}
		vm.stack[vm.sp] = val
		vm.sp++
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.SWAP: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		vm.stack[vm.sp-1], vm.stack[vm.sp-2] = vm.stack[vm.sp-2], vm.stack[vm.sp-1]
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.BR: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		offset := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		frame.ip += offset + 5
		return nil
	},
	instr.BR_IF: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		if vm.sp == 0 {
			return ErrStackUnderflow
		}
		vm.sp--
		cond := vm.stack[vm.sp].I32()
		if cond != 0 {
			offset := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			frame.ip += offset
		}
		frame.ip += 5
		return nil
	},
	instr.CALL: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}
		vm.sp--
		if vm.fp == len(vm.frames) {
			return ErrFrameOverflow
		}

		addr := vm.stack[vm.sp].Ref()
		fn, ok := vm.heap[addr].(*types.Function)
		if !ok {
			return ErrTypeMismatch
		}
		params := len(fn.Params)
		locals := len(fn.Locals)

		frame := &vm.frames[vm.fp]
		frame.addr = addr
		frame.fn = fn
		frame.ip = 0
		frame.bp = vm.sp - params

		vm.fp++
		for i := 0; i < locals; i++ {
			vm.stack[frame.bp+params+i] = 0
		}
		sp := frame.bp + params + locals
		if sp == len(vm.stack) {
			return ErrStackOverflow
		}
		vm.sp = sp
		vm.frames[vm.fp-2].ip++
		return nil
	},
	instr.RETURN: func(vm *VM) error {
		if vm.fp == 1 {
			return ErrFrameUnderflow
		}

		frame := &vm.frames[vm.fp-1]
		fn := frame.fn
		returns := len(fn.Returns)
		if vm.sp < returns {
			return ErrStackUnderflow
		}

		copy(vm.stack[frame.bp:frame.bp+returns], vm.stack[vm.sp-returns:vm.sp])
		vm.sp = frame.bp + returns

		if frame.addr > 0 {
			vm.release(frame.addr)
		}
		frame.fn = nil
		vm.fp--
		return nil
	},
	instr.GLOBAL_GET: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 || idx >= len(vm.global) {
			return ErrSegmentationFault
		}

		val := vm.global[idx]
		if val.Kind() == types.KindRef {
			vm.retain(val.Ref())
		}

		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.GLOBAL_SET: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 {
			return ErrSegmentationFault
		}

		val := vm.stack[vm.sp-1]
		if idx >= len(vm.global) {
			if cap(vm.global) > idx {
				vm.global = vm.global[:idx+1]
			} else {
				global := make([]types.Boxed, idx*2)
				copy(global, vm.global)
				vm.global = global[:idx+1]
			}
		}

		if old := vm.global[idx]; old != val && old.Kind() == types.KindRef {
			vm.release(old.Ref())
		}

		vm.global[idx] = val
		vm.sp--
		frame.ip += 5
		return nil
	},
	instr.LOCAL_GET: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		addr := frame.bp + idx
		if addr < 0 || addr > vm.sp {
			return ErrSegmentationFault
		}
		val := vm.stack[addr]
		if val.Kind() == types.KindRef {
			vm.retain(val.Ref())
		}
		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.LOCAL_SET: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}
		frame := &vm.frames[vm.fp-1]
		fn := frame.fn
		code := fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 || idx >= len(fn.Params)+len(fn.Locals) {
			return ErrSegmentationFault
		}
		val := vm.stack[vm.sp-1]
		addr := frame.bp + idx
		if addr < 0 || addr > vm.sp {
			return ErrSegmentationFault
		}
		if old := vm.stack[addr]; old != val && old.Kind() == types.KindRef {
			vm.release(old.Ref())
		}
		vm.stack[addr] = val
		vm.sp--
		frame.ip += 5
		return nil
	},
	instr.FN_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}
		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 || idx >= len(vm.constants) {
			return ErrSegmentationFault
		}
		fn, ok := vm.constants[idx].(*types.Function)
		if !ok {
			return ErrTypeMismatch
		}
		addr := vm.alloc(fn)
		vm.stack[vm.sp] = types.BoxRef(addr)
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.I32_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}
		val := types.BoxI32(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.I32_ADD: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v1 + v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_SUB: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v2 - v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_MUL: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v1 * v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_DIV_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v2 / v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_DIV_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(uint32(v2) / uint32(v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_REM_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v2 % v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_REM_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(uint32(v2) % uint32(v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_SHL: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32() & 0x1F
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v2 << v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_SHR_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32() & 0x1F
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v2 >> v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_SHR_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32() & 0x1F
		v2 := uint32(vm.stack[vm.sp-2].I32())
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(v2 >> v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_XOR: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v1 ^ v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_AND: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v1 & v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_OR: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(v1 | v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_EQ: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 == v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_NE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 != v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LT_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 < v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LT_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint32(v2) < uint32(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GT_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 > v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GT_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint32(v2) > uint32(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LE_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 <= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LE_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint32(v2) <= uint32(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GE_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 >= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GE_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint32(v2) >= uint32(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_TO_I64_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].I32()
		vm.stack[vm.sp-1] = vm.boxI64(int64(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_TO_I64_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := uint32(vm.stack[vm.sp-1].I32())
		vm.stack[vm.sp-1] = vm.boxI64(int64(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_TO_F32_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].I32()
		vm.stack[vm.sp-1] = types.BoxF32(float32(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_TO_F32_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].I32()
		vm.stack[vm.sp-1] = types.BoxF32(float32(uint32(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_TO_F64_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].I32()
		vm.stack[vm.sp-1] = types.BoxF64(float64(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_TO_F64_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].I32()
		vm.stack[vm.sp-1] = types.BoxF64(float64(uint32(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}
		val := vm.boxI64(int64(binary.BigEndian.Uint64(code[frame.ip+1:])))
		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 9
		return nil
	},
	instr.I64_ADD: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(v1 + v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_SUB: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(v2 - v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_MUL: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(v1 * v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_DIV_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(v2 / v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_DIV_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(int64(uint64(v2) / uint64(v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_REM_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(v2 % v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_REM_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(int64(uint64(v2) % uint64(v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_SHL: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(int64(v2 << (v1 & 0x3F)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_SHR_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(int64(v2 >> (v1 & 0x3F)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_SHR_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = vm.boxI64(int64(uint64(v2) >> (v1 & 0x3F)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_EQ: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 == v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_NE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 != v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LT_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 < v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LT_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint64(v2) < uint64(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GT_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 > v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GT_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint64(v2) > uint64(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LE_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 <= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LE_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint64(v2) <= uint64(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GE_S: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 >= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GE_U: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.unboxI64(vm.stack[vm.sp-1])
		v2 := vm.unboxI64(vm.stack[vm.sp-2])
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(uint64(v2) >= uint64(v1))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_TO_I32: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.unboxI64(vm.stack[vm.sp-1])
		vm.stack[vm.sp-1] = types.BoxI32(int32(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_TO_F32_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.unboxI64(vm.stack[vm.sp-1])
		vm.stack[vm.sp-1] = types.BoxF32(float32(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_TO_F32_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.unboxI64(vm.stack[vm.sp-1])
		vm.stack[vm.sp-1] = types.BoxF32(float32(uint64(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_TO_F64_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.unboxI64(vm.stack[vm.sp-1])
		vm.stack[vm.sp-1] = types.BoxF64(float64(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_TO_F64_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.unboxI64(vm.stack[vm.sp-1])
		vm.stack[vm.sp-1] = types.BoxF64(float64(uint64(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}
		val := types.BoxF32(math.Float32frombits(binary.BigEndian.Uint32(code[frame.ip+1:])))
		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.F32_ADD: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF32(v1 + v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_SUB: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF32(v2 - v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_MUL: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF32(v1 * v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_DIV: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF32(v2 / v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_EQ: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 == v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_NE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 != v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_LT: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 < v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_GT: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 > v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_LE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 <= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_GE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 >= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_TO_I32_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F32()
		vm.stack[vm.sp-1] = types.BoxI32(int32(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_TO_I32_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F32()
		vm.stack[vm.sp-1] = types.BoxI32(int32(uint32(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_TO_I64_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F32()
		vm.stack[vm.sp-1] = vm.boxI64(int64(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_TO_I64_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F32()
		vm.stack[vm.sp-1] = vm.boxI64(int64(uint32(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_TO_F64: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F32()
		vm.stack[vm.sp-1] = types.BoxF64(float64(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_TO_I32_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F64()
		vm.stack[vm.sp-1] = types.BoxI32(int32(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_TO_I32_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F64()
		vm.stack[vm.sp-1] = types.BoxI32(int32(uint32(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_TO_I64_S: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F64()
		vm.stack[vm.sp-1] = vm.boxI64(int64(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_TO_I64_U: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F64()
		vm.stack[vm.sp-1] = vm.boxI64(int64(uint64(v)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_TO_F32: func(vm *VM) error {
		if vm.sp < 1 {
			return ErrStackUnderflow
		}
		v := vm.stack[vm.sp-1].F64()
		vm.stack[vm.sp-1] = types.BoxF32(float32(v))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.fn.Code
		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}
		val := types.BoxF64(math.Float64frombits(binary.BigEndian.Uint64(code[frame.ip+1:])))
		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 9
		return nil
	},
	instr.F64_ADD: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF64(v1 + v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_SUB: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF64(v2 - v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_MUL: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF64(v1 * v2)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_DIV: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		if v1 == 0 {
			return ErrDivideByZero
		}
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxF64(v2 / v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_EQ: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 == v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_NE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 != v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_LT: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 < v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_GT: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 > v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_LE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 <= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_GE: func(vm *VM) error {
		if vm.sp < 2 {
			return ErrStackUnderflow
		}
		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()
		vm.sp--
		vm.stack[vm.sp-1] = types.BoxBool(v2 >= v1)
		vm.frames[vm.fp-1].ip++
		return nil
	},
}
