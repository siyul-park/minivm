package interp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Option struct {
	Frame  int
	Global int
	Stack  int
	Heap   int
}

type Interpreter struct {
	frames    []frame
	constants []types.Value
	global    []types.Boxed
	stack     []types.Boxed
	heap      []types.Value
	free      []int
	rc        []int
	fp        int
	sp        int
}

type frame struct {
	fn   *types.Function
	addr int
	ip   int
	bp   int
}

var (
	ErrUnknownOpcode       = errors.New("unknown opcode")
	ErrUnreachableExecuted = errors.New("unreachable executed")
	ErrSegmentationFault   = errors.New("segmentation fault")
	ErrStackOverflow       = errors.New("stack overflow")
	ErrStackUnderflow      = errors.New("stack underflow")
	ErrFrameOverflow       = errors.New("frame overflow")
	ErrFrameUnderflow      = errors.New("frame underflow")
	ErrTypeMismatch        = errors.New("type mismatch")
	ErrDivideByZero        = errors.New("divide by zero")
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
		if i.fp == len(i.frames) {
			return ErrFrameOverflow
		}

		i.sp--
		addr := i.stack[i.sp].Ref()
		fn, ok := i.heap[addr].(*types.Function)
		if !ok {
			return ErrTypeMismatch
		}

		params := len(fn.Params)
		locals := len(fn.Locals)
		if i.sp < params {
			return ErrStackUnderflow
		}
		if i.sp+locals >= len(i.stack) {
			return ErrStackOverflow
		}

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
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}

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
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}

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
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
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
		if i.sp == len(i.stack) {
			return ErrStackOverflow
		}
		frame := &i.frames[i.fp-1]
		code := frame.fn.Code
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

func New(prog *program.Program, opts ...Option) *Interpreter {
	f := 128
	g := 128
	s := 1024
	h := 128
	for _, opt := range opts {
		if opt.Frame > 0 {
			f = opt.Frame
		}
		if opt.Global > 0 {
			g = opt.Global
		}
		if opt.Stack > 0 {
			s = opt.Stack
		}
		if opt.Heap > 0 {
			h = opt.Heap
		}
	}
	if f <= 0 {
		f = 1
	}

	i := &Interpreter{
		frames:    make([]frame, f),
		constants: prog.Constants,
		global:    make([]types.Boxed, 0, g),
		stack:     make([]types.Boxed, s),
		heap:      make([]types.Value, 0, h),
		rc:        make([]int, 0, h),
		free:      make([]int, 0, h),
		fp:        1,
		sp:        0,
	}

	i.heap = append(i.heap, nil)
	i.rc = append(i.rc, 0)

	i.frames[0].fn = &types.Function{Code: prog.Code}
	i.frames[0].bp = i.sp
	return i
}

func (i *Interpreter) Run() error {
	frame := &i.frames[i.fp-1]
	code := frame.fn.Code

	for frame.ip < len(code) {
		opcode := instr.Opcode(code[frame.ip])
		fn := dispatch[opcode]
		if fn == nil {
			return fmt.Errorf("%w: at=%d", ErrUnknownOpcode, frame.ip)
		}
		if err := fn(i); err != nil {
			return fmt.Errorf("%w: at=%d", err, frame.ip)
		}
		frame = &i.frames[i.fp-1]
		code = frame.fn.Code
	}
	return nil
}

func (i *Interpreter) Push(val types.Value) error {
	if i.sp == len(i.stack) {
		return ErrStackOverflow
	}

	switch val := val.(type) {
	case types.Boxed:
		i.stack[i.sp] = val
	default:
		addr := i.alloc(val)
		i.stack[i.sp] = types.BoxRef(addr)
	}
	i.sp++
	return nil
}

func (i *Interpreter) Pop() (types.Value, error) {
	if i.sp == 0 {
		return nil, ErrStackUnderflow
	}

	i.sp--
	val := i.stack[i.sp]

	if val.Kind() == types.KindRef {
		addr := val.Ref()
		v := i.heap[addr]
		i.release(addr)
		return v, nil
	}
	return val, nil
}

func (i *Interpreter) Len() int {
	return i.sp - 1
}

func (i *Interpreter) Clear() {
	for i.fp > 1 {
		i.frames[i.fp] = frame{}
		i.fp--
	}
	i.frames[i.fp-1].bp = i.sp
	i.frames[i.fp-1].ip = 0

	for idx := range i.global {
		i.global[idx] = 0
	}
	i.global = i.global[:0]

	i.sp = 0

	for idx := range i.heap {
		i.heap[idx] = nil
	}
	for idx := range i.rc {
		i.rc[idx] = 0
	}
	for idx := range i.free {
		i.free[idx] = 0
	}
	i.heap = i.heap[:1]
	i.rc = i.rc[:1]
	i.free = i.free[:0]
}

func (i *Interpreter) boxI64(val int64) types.Boxed {
	if types.IsBoxable(val) {
		return types.BoxI64(val)
	}
	addr := i.alloc(types.I64(val))
	return types.BoxRef(addr)
}

func (i *Interpreter) unboxI64(val types.Boxed) int64 {
	if val.Kind() != types.KindRef {
		return val.I64()
	}
	addr := val.Ref()
	v, _ := i.heap[addr].(types.I64)
	i.release(addr)
	return int64(v)
}

func (i *Interpreter) alloc(val types.Value) int {
	if len(i.free) > 0 {
		addr := i.free[len(i.free)-1]
		i.free = i.free[:len(i.free)-1]
		i.heap[addr] = val
		return addr
	}

	if len(i.heap) == cap(i.heap) {
		c := 2 * cap(i.heap)
		if c == 0 {
			c = 1
		}
		heap := make([]types.Value, len(i.heap), c)
		copy(heap, i.heap)
		i.heap = heap

		hits := make([]int, len(i.rc), c)
		copy(hits, i.rc)
		i.rc = hits
	}

	i.heap = append(i.heap, val)
	i.rc = append(i.rc, 1)
	return len(i.heap) - 1
}

func (i *Interpreter) retain(addr int) {
	i.rc[addr]++
}

func (i *Interpreter) release(addr int) {
	stack := []int{addr}
	for len(stack) > 0 {
		a := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		i.rc[a]--
		if i.rc[a] > 0 {
			continue
		}

		obj := i.heap[a]
		i.heap[a] = nil
		i.rc[a] = 0
		i.free = append(i.free, a)

		if t, ok := obj.(types.Traceable); ok {
			for _, ref := range t.Refs() {
				stack = append(stack, int(ref))
			}
		}
	}
}
