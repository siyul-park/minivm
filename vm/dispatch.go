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
			vm.hits[val.Ref()]++
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
		code := frame.cl.Function.Code

		offset := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		frame.ip += offset + 5
		return nil
	},
	instr.BR_IF: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

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
		if vm.fp == len(vm.frames) {
			return ErrFrameOverflow
		}

		vm.sp--
		addr := vm.stack[vm.sp].Ref()

		cl, ok := vm.heap[addr].(*types.Closure)
		if !ok {
			return ErrSegmentationFault
		}

		frame := &vm.frames[vm.fp]
		frame.cl = cl
		frame.ref = types.Ref(addr)
		frame.ip = 0
		frame.bp = vm.sp - cl.Function.Params
		vm.fp++

		for i := 0; i < cl.Function.Locals-cl.Function.Params; i++ {
			vm.stack[frame.bp+i] = 0
		}

		sp := frame.bp + cl.Function.Locals
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
		fn := frame.cl.Function

		if fn.Returns > 0 {
			copy(vm.stack[frame.bp:frame.bp+fn.Returns], vm.stack[vm.sp-fn.Returns:vm.sp])
		}
		vm.sp = frame.bp + fn.Returns

		if frame.ref >= 0 {
			if err := vm.free(int(frame.ref)); err != nil {
				return err
			}
		}

		frame.cl = nil
		vm.fp--
		return nil
	},
	instr.GLOBAL_GET: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		val, err := vm.gload(idx)
		if err != nil {
			return err
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
		code := frame.cl.Function.Code

		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		val := vm.stack[vm.sp-1]
		if err := vm.gstore(idx, val); err != nil {
			return err
		}

		vm.sp--
		frame.ip += 5
		return nil
	},
	instr.GLOBAL_TEE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		val := vm.stack[vm.sp-1]
		if err := vm.gstore(idx, val); err != nil {
			return err
		}

		frame.ip += 5
		return nil
	},
	instr.LOCAL_GET: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		val, err := vm.lload(idx)
		if err != nil {
			return err
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
		code := frame.cl.Function.Code

		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		val := vm.stack[vm.sp-1]
		if err := vm.lstore(idx, val); err != nil {
			return err
		}

		vm.sp--
		frame.ip += 5
		return nil
	},
	instr.LOCAL_TEE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		val := vm.stack[vm.sp-1]
		if err := vm.lstore(idx, val); err != nil {
			return err
		}

		frame.ip += 5
		return nil
	},
	instr.FN_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}

		idx := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
		if idx < 0 || idx >= len(vm.constants) {
			return ErrSegmentationFault
		}
		fn, ok := vm.constants[idx].(*types.Function)
		if !ok {
			return ErrSegmentationFault
		}

		cl := &types.Closure{Function: fn}
		if fn.Captures > 0 {
			cl.Captures = make([]types.Boxed, fn.Captures)
			copy(cl.Captures, vm.stack[vm.sp-fn.Captures:vm.sp])
			vm.sp -= fn.Captures
		}

		addr, err := vm.alloc(cl)
		if err != nil {
			return err
		}

		vm.stack[vm.sp] = types.BoxRef(addr)
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.I32_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}

		val := types.BoxI32(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))

		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.I32_LOAD: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		addr := vm.stack[vm.sp-1].Ref()
		if addr < 0 || addr >= len(vm.heap) {
			return ErrSegmentationFault
		}
		val, ok := vm.heap[addr].(types.I32)
		if !ok {
			return ErrSegmentationFault
		}
		if err := vm.free(addr); err != nil {
			return err
		}

		vm.stack[vm.sp-1] = types.BoxI32(int32(val))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_STORE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		val := vm.stack[vm.sp-1].I32()
		addr, err := vm.alloc(types.I32(val))
		if err != nil {
			return err
		}

		vm.stack[vm.sp-1] = types.BoxRef(addr)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_ADD: func(vm *VM) error {
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
	instr.I32_XOR: func(vm *VM) error {
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 == v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_NE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 != v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LT_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 < v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LT_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint32(v2) < uint32(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GT_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 > v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GT_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint32(v2) > uint32(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LE_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 <= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_LE_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint32(v2) <= uint32(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GE_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 >= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I32_GE_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].I32()
		v2 := vm.stack[vm.sp-2].I32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint32(v2) >= uint32(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}

		val, err := vm.boxI64(types.I64(binary.BigEndian.Uint64(code[frame.ip+1:])))
		if err != nil {
			return err
		}

		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 9
		return nil
	},
	instr.I64_LOAD: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		addr := vm.stack[vm.sp-1].Ref()
		if addr < 0 || addr >= len(vm.heap) {
			return ErrSegmentationFault
		}
		val, ok := vm.heap[addr].(types.I64)
		if !ok {
			return ErrSegmentationFault
		}
		if types.IsBoxable(int64(val)) {
			if err := vm.free(addr); err != nil {
				return err
			}
			vm.stack[vm.sp-1] = types.BoxI64(int64(val))
		}

		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_STORE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		val := vm.stack[vm.sp-1]
		if val.Kind() != types.KindRef {
			addr, err := vm.alloc(types.I64(val.I64()))
			if err != nil {
				return err
			}
			vm.stack[vm.sp-1] = types.BoxRef(addr)
		}

		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_ADD: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}
		v3, err := vm.boxI64(v1 + v2)
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = v3
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_SUB: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}
		v3, err := vm.boxI64(v2 - v1)
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = v3
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_MUL: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}
		v3, err := vm.boxI64(v1 * v2)
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = v3
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_DIV_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}
		if v1 == 0 {
			return ErrDivideByZero
		}
		v3, err := vm.boxI64(v2 / v1)
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = v3
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_DIV_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}
		if v1 == 0 {
			return ErrDivideByZero
		}
		v3, err := vm.boxI64(types.I64(uint64(v2) / uint64(v1)))
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = v3
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_REM_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}
		if v1 == 0 {
			return ErrDivideByZero
		}
		v3, err := vm.boxI64(v2 % v1)
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = v3
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_REM_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}
		if v1 == 0 {
			return ErrDivideByZero
		}
		v3, err := vm.boxI64(types.I64(uint64(v2) % uint64(v1)))
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = v3
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_EQ: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 == v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_NE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 != v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LT_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 < v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LT_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint64(v2) < uint64(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GT_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 > v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GT_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint64(v2) > uint64(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LE_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 <= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_LE_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint64(v2) <= uint64(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GE_S: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 >= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.I64_GE_U: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1, err := vm.unboxI64(vm.stack[vm.sp-1])
		if err != nil {
			return err
		}
		v2, err := vm.unboxI64(vm.stack[vm.sp-2])
		if err != nil {
			return err
		}

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(uint64(v2) >= uint64(v1))))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}

		val := types.BoxF32(math.Float32frombits(binary.BigEndian.Uint32(code[frame.ip+1:])))

		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 5
		return nil
	},
	instr.F32_LOAD: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		addr := vm.stack[vm.sp-1].Ref()
		if addr < 0 || addr >= len(vm.heap) {
			return ErrSegmentationFault
		}
		val, ok := vm.heap[addr].(types.F32)
		if !ok {
			return ErrSegmentationFault
		}
		if err := vm.free(addr); err != nil {
			return err
		}

		vm.stack[vm.sp-1] = types.BoxF32(float32(val))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_STORE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		val := vm.stack[vm.sp-1].F32()
		addr, err := vm.alloc(types.F32(val))
		if err != nil {
			return err
		}

		vm.stack[vm.sp-1] = types.BoxRef(addr)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_ADD: func(vm *VM) error {
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 == v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_NE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 != v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_LT: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 < v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_GT: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 > v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_LE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 <= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F32_GE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F32()
		v2 := vm.stack[vm.sp-2].F32()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 >= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_CONST: func(vm *VM) error {
		frame := &vm.frames[vm.fp-1]
		code := frame.cl.Function.Code

		if vm.sp == len(vm.stack) {
			return ErrStackOverflow
		}

		val := types.BoxF64(math.Float64frombits(binary.BigEndian.Uint64(code[frame.ip+1:])))

		vm.stack[vm.sp] = val
		vm.sp++
		frame.ip += 9
		return nil
	},
	instr.F64_LOAD: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		addr := vm.stack[vm.sp-1].Ref()
		if addr < 0 || addr >= len(vm.heap) {
			return ErrSegmentationFault
		}
		val, ok := vm.heap[addr].(types.F64)
		if !ok {
			return ErrSegmentationFault
		}
		if err := vm.free(addr); err != nil {
			return err
		}

		vm.stack[vm.sp-1] = types.BoxF64(float64(val))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_STORE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		val := vm.stack[vm.sp-1].F64()
		addr, err := vm.alloc(types.F64(val))
		if err != nil {
			return err
		}

		vm.stack[vm.sp-1] = types.BoxRef(addr)
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_ADD: func(vm *VM) error {
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
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
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 == v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_NE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 != v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_LT: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 < v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_GT: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 > v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_LE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 <= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
	instr.F64_GE: func(vm *VM) error {
		if vm.sp == 0 {
			return ErrStackUnderflow
		}

		v1 := vm.stack[vm.sp-1].F64()
		v2 := vm.stack[vm.sp-2].F64()

		vm.sp--
		vm.stack[vm.sp-1] = types.BoxI32(int32(types.Bool(v2 >= v1)))
		vm.frames[vm.fp-1].ip++
		return nil
	},
}
