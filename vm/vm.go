package vm

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Option struct {
	Stack  int
	Heap   int
	Global int
	Frame  int
}

type VM struct {
	constants []types.Value
	stack     []types.Boxed
	heap      []types.Value
	frees     []int
	hits      []int
	global    []types.Boxed
	frames    []Frame
	sp        int
	fp        int
}

var (
	ErrUnknownOpcode       = errors.New("unknown opcode")
	ErrUnreachableExecuted = errors.New("unreachable executed")
	ErrReferenceInvalid    = errors.New("reference invalid")
	ErrStackOverflow       = errors.New("stack overflow")
	ErrStackUnderflow      = errors.New("stack underflow")
	ErrFrameOverflow       = errors.New("frame overflow")
	ErrDivideByZero        = errors.New("divide by zero")
)

func New(prog *program.Program, opts ...Option) *VM {
	stack := 1024
	heap := 64
	global := 128
	frame := 64
	for _, opt := range opts {
		if opt.Stack > 0 {
			stack = opt.Stack
		}
		if opt.Heap > 0 {
			heap = opt.Heap
		}
		if opt.Global > 0 {
			global = opt.Global
		}
		if opt.Frame > 0 {
			frame = opt.Frame
		}
	}
	if frame <= 0 {
		frame = 1
	}

	vm := &VM{
		constants: prog.Constants,
		stack:     make([]types.Boxed, stack),
		heap:      make([]types.Value, 0, heap),
		hits:      make([]int, 0, heap),
		frees:     make([]int, 0, heap),
		global:    make([]types.Boxed, 0, global),
		frames:    make([]Frame, frame),
		sp:        -1,
		fp:        0,
	}
	vm.frames[0].cl = &types.Closure{Function: &types.Function{Code: prog.Code}}
	vm.frames[0].ref = -1
	vm.frames[0].bp = vm.sp
	return vm
}

func (vm *VM) Run() error {
	frame := &vm.frames[vm.fp]
	code := frame.cl.Function.Code

	for frame.ip < len(code) {
		opcode := instr.Opcode(code[frame.ip])
		switch opcode {
		case instr.NOP:
			frame.ip++

		case instr.UNREACHABLE:
			frame.ip++
			return ErrUnreachableExecuted

		case instr.DROP:
			if _, err := vm.pop(); err != nil {
				return err
			}
			frame.ip++

		case instr.DUP:
			v, err := vm.peek()
			if err != nil {
				return err
			}
			if v.Kind() == types.KindRef {
				if err := vm.retain(v.Ref()); err != nil {
					return err
				}
			}
			if err := vm.push(v); err != nil {
				return err
			}
			frame.ip++

		case instr.SWAP:
			if err := vm.swap(); err != nil {
				return err
			}
			frame.ip++

		case instr.BR:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			frame.ip += p + 5

		case instr.BR_IF:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v, err := vm.popI32()
			if err != nil {
				return err
			}
			if v != 0 {
				frame.ip += p + 5
			} else {
				frame.ip += 5
			}

		case instr.CALL:
			ref, err := vm.popRef()
			if err != nil {
				return err
			}
			if err := vm.call(ref); err != nil {
				return err
			}
			frame.ip++
			frame = &vm.frames[vm.fp]
			code = frame.cl.Function.Code

		case instr.RETURN:
			if err := vm.ret(); err != nil {
				return err
			}

			frame.ip++
			frame = &vm.frames[vm.fp]
			code = frame.cl.Function.Code

		case instr.GLOBAL_GET:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v, err := vm.gload(p)
			if err != nil {
				return err
			}
			if err := vm.push(v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.GLOBAL_SET:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v, err := vm.pop()
			if err != nil {
				return err
			}
			if err := vm.gstore(p, v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.GLOBAL_TEE:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v, err := vm.peek()
			if err != nil {
				return err
			}
			if v.Kind() == types.KindRef {
				if err := vm.retain(v.Ref()); err != nil {
					return err
				}
			}
			if err := vm.gstore(p, v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.LOCAL_GET:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v, err := vm.lload(p)
			if err != nil {
				return err
			}
			if err := vm.push(v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.LOCAL_SET:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v, err := vm.pop()
			if err != nil {
				return err
			}
			if err := vm.lstore(p, v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.LOCAL_TEE:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v, err := vm.peek()
			if err != nil {
				return err
			}
			if v.Kind() == types.KindRef {
				if err := vm.retain(v.Ref()); err != nil {
					return err
				}
			}
			if err := vm.lstore(p, v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.FN_CONST:
			p := int(int32(binary.BigEndian.Uint32(code[frame.ip+1:])))
			v2, err := vm.constant(p)
			if err != nil {
				return err
			}
			fn, ok := v2.(*types.Function)
			if !ok {
				return ErrReferenceInvalid
			}
			cl := &types.Closure{Function: fn}
			if fn.Captures > 0 {
				cl.Captures = make([]types.Boxed, fn.Captures)
				copy(cl.Captures, vm.stack[vm.sp-fn.Captures:vm.sp])
				vm.sp -= fn.Captures
			}
			if err := vm.pushFn(cl); err != nil {
				return err
			}
			frame.ip += 5

		case instr.I32_CONST:
			p := types.I32(binary.BigEndian.Uint32(code[frame.ip+1:]))
			if err := vm.pushI32(p); err != nil {
				return err
			}
			frame.ip += 5

		case instr.I32_LOAD:
			addr, err := vm.popRef()
			if err != nil {
				return err
			}
			v, err := vm.load(int(addr))
			if err != nil {
				return err
			}
			i, ok := v.(types.I32)
			if !ok {
				return ErrReferenceInvalid
			}
			if err := vm.pushI32(i); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_STORE:
			v, err := vm.popI32()
			if err != nil {
				return err
			}
			addr, err := vm.store(v)
			if err != nil {
				return err
			}
			if err := vm.push(types.BoxRef(addr)); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_ADD:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(v1 + v2); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_SUB:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(v2 - v1); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_MUL:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(v1 * v2); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_DIV_S:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI32(v2 / v1); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_DIV_U:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI32(types.I32(uint32(v2) / uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_REM_S:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI32(v2 % v1); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_REM_U:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI32(types.I32(uint32(v2) % uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_EQ:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 == v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_NE:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 != v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_LT_S:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 < v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_LT_U:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) < uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_GT_S:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 > v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_GT_U:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) > uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_LE_S:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 <= v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_LE_U:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) <= uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_GE_S:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 >= v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I32_GE_U:
			v1, err := vm.popI32()
			if err != nil {
				return err
			}
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) >= uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_CONST:
			p := types.I64(binary.BigEndian.Uint64(code[frame.ip+1:]))
			if err := vm.pushI64(p); err != nil {
				return err
			}
			frame.ip += 9

		case instr.I64_LOAD:
			addr, err := vm.popRef()
			if err != nil {
				return err
			}
			v, err := vm.load(int(addr))
			if err != nil {
				return err
			}
			i, ok := v.(types.I64)
			if !ok {
				return ErrReferenceInvalid
			}
			if err := vm.pushI64(i); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_STORE:
			v, err := vm.peek()
			if err != nil {
				return err
			}
			if v.Kind() != types.KindRef {
				addr, err := vm.store(types.I64(v.I64()))
				if err != nil {
					return err
				}
				if err := vm.poke(types.BoxRef(addr)); err != nil {
					return err
				}
			}
			frame.ip++

		case instr.I64_ADD:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI64(v1 + v2); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_SUB:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI64(v2 - v1); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_MUL:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI64(v1 * v2); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_DIV_S:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI64(v2 / v1); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_DIV_U:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI64(types.I64(uint64(v2) / uint64(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_REM_S:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI64(v2 % v1); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_REM_U:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if v1 == 0 {
				return ErrDivideByZero
			}
			if err := vm.pushI64(types.I64(uint64(v2) % uint64(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_EQ:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 == v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_NE:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 != v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_LT_S:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 < v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_LT_U:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) < uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_GT_S:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 > v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_GT_U:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) > uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_LE_S:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 <= v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_LE_U:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) <= uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_GE_S:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 >= v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.I64_GE_U:
			v1, err := vm.popI64()
			if err != nil {
				return err
			}
			v2, err := vm.popI64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(uint32(v2) >= uint32(v1))); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_CONST:
			p := types.F32(math.Float32frombits(binary.BigEndian.Uint32(code[frame.ip+1:])))
			if err := vm.pushF32(p); err != nil {
				return err
			}
			frame.ip += 5

		case instr.F32_LOAD:
			addr, err := vm.popRef()
			if err != nil {
				return err
			}
			v, err := vm.load(int(addr))
			if err != nil {
				return err
			}
			f, ok := v.(types.F32)
			if !ok {
				return ErrReferenceInvalid
			}
			if err := vm.pushF32(f); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_STORE:
			v, err := vm.popF32()
			if err != nil {
				return err
			}
			addr, err := vm.store(v)
			if err != nil {
				return err
			}
			if err := vm.push(types.BoxRef(addr)); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_ADD:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushF32(v1 + v2); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_SUB:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushF32(v2 - v1); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_MUL:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushF32(v1 * v2); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_DIV:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushF32(v2 / v1); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_EQ:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 == v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_NE:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 != v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_LT:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 < v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_GT:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 > v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_LE:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 <= v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F32_GE:
			v1, err := vm.popF32()
			if err != nil {
				return err
			}
			v2, err := vm.popF32()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 >= v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_CONST:
			p := types.F64(math.Float64frombits(binary.BigEndian.Uint64(code[frame.ip+1:])))
			if err := vm.pushF64(p); err != nil {
				return err
			}
			frame.ip += 9

		case instr.F64_LOAD:
			addr, err := vm.popRef()
			if err != nil {
				return err
			}
			v, err := vm.load(int(addr))
			if err != nil {
				return err
			}
			f, ok := v.(types.F64)
			if !ok {
				return ErrReferenceInvalid
			}
			if err := vm.pushF64(f); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_STORE:
			v, err := vm.popF64()
			if err != nil {
				return err
			}
			addr, err := vm.store(v)
			if err != nil {
				return err
			}
			if err := vm.push(types.BoxRef(addr)); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_ADD:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushF64(v1 + v2); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_SUB:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushF64(v2 - v1); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_MUL:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushF64(v1 * v2); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_DIV:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushF64(v2 / v1); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_EQ:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 == v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_NE:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 != v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_LT:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 < v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_GT:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 > v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_LE:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 <= v1)); err != nil {
				return err
			}
			frame.ip++

		case instr.F64_GE:
			v1, err := vm.popF64()
			if err != nil {
				return err
			}
			v2, err := vm.popF64()
			if err != nil {
				return err
			}
			if err := vm.pushI32(types.Bool(v2 >= v1)); err != nil {
				return err
			}
			frame.ip++

		default:
			return ErrUnknownOpcode
		}
	}
	return nil
}

func (vm *VM) Push(val types.Value) error {
	switch val := val.(type) {
	case types.Boxed:
		return vm.push(val)
	default:
		addr, err := vm.alloc(val)
		if err != nil {
			return err
		}
		return vm.push(types.BoxRef(addr))
	}
}

func (vm *VM) Pop() (types.Value, error) {
	box, err := vm.pop()
	if err != nil {
		return nil, err
	}
	if box.Kind() == types.KindRef {
		addr := box.Ref()
		val := vm.heap[addr]
		if err := vm.free(addr); err != nil {
			return nil, err
		}
		return val, nil
	}
	return box, nil
}

func (vm *VM) Len() int {
	return vm.sp + 1
}

func (vm *VM) Clear() {
	for vm.sp >= 0 {
		vm.stack[vm.sp] = 0
		vm.sp--
	}

	for vm.fp > 0 {
		vm.frames[vm.fp] = Frame{}
		vm.fp--
	}
	vm.frames[vm.fp].bp = vm.sp
	vm.frames[vm.fp].ip = 0

	vm.heap = vm.heap[:0]
	vm.hits = vm.hits[:0]
	vm.frees = vm.frees[:0]
	vm.global = vm.global[:0]
}

func (vm *VM) pushI32(val types.I32) error {
	return vm.push(types.BoxI32(int32(val)))
}

func (vm *VM) pushI64(val types.I64) error {
	var box types.Boxed
	if types.IsBoxable(int64(val)) {
		box = types.BoxI64(int64(val))
	} else {
		addr, err := vm.alloc(val)
		if err != nil {
			return err
		}
		box = types.BoxRef(addr)
	}
	return vm.push(box)
}

func (vm *VM) pushF32(val types.F32) error {
	return vm.push(types.BoxF32(float32(val)))
}

func (vm *VM) pushF64(val types.F64) error {
	return vm.push(types.BoxF64(float64(val)))
}

func (vm *VM) pushRef(val types.Ref) error {
	return vm.push(types.BoxRef(int(val)))
}

func (vm *VM) pushFn(val *types.Closure) error {
	addr, err := vm.alloc(val)
	if err != nil {
		return err
	}
	return vm.push(types.BoxRef(addr))
}

func (vm *VM) popI32() (types.I32, error) {
	box, err := vm.pop()
	if err != nil {
		return 0, err
	}
	return types.I32(box.I32()), nil
}

func (vm *VM) popI64() (types.I64, error) {
	box, err := vm.pop()
	if err != nil {
		return 0, err
	}
	if box.Kind() == types.KindRef {
		val, err := vm.load(box.Ref())
		if err != nil {
			return 0, err
		}
		v, ok := val.(types.I64)
		if !ok {
			return 0, ErrReferenceInvalid
		}
		return v, nil
	}
	return types.I64(box.I64()), nil
}

func (vm *VM) popF32() (types.F32, error) {
	box, err := vm.pop()
	if err != nil {
		return 0, err
	}
	return types.F32(box.F32()), nil
}

func (vm *VM) popF64() (types.F64, error) {
	box, err := vm.pop()
	if err != nil {
		return 0, err
	}
	return types.F64(box.F64()), nil
}

func (vm *VM) popRef() (types.Ref, error) {
	box, err := vm.pop()
	if err != nil {
		return 0, err
	}
	return types.Ref(box.Ref()), nil
}

func (vm *VM) call(ref types.Ref) error {
	if vm.fp+1 >= len(vm.frames) {
		return ErrFrameOverflow
	}

	v, err := vm.lookup(int(ref))
	if err != nil {
		return err
	}
	cl, ok := v.(*types.Closure)
	if !ok {
		return ErrReferenceInvalid
	}

	vm.fp++
	vm.frames[vm.fp].cl = cl
	vm.frames[vm.fp].ref = ref
	vm.frames[vm.fp].ip = 0
	vm.frames[vm.fp].bp = vm.sp - cl.Function.Params

	for i := cl.Function.Params; i < cl.Function.Locals; i++ {
		vm.stack[vm.frames[vm.fp].bp+i+1] = 0
	}

	sp := vm.frames[vm.fp].bp + cl.Function.Locals
	if sp >= len(vm.stack) {
		return ErrStackOverflow
	}
	vm.sp = sp
	return nil
}

func (vm *VM) ret() error {
	frame := &vm.frames[vm.fp]
	fn := frame.cl.Function

	if fn.Returns > 0 {
		start := frame.bp + 1
		end := start + fn.Returns
		copy(vm.stack[start:end], vm.stack[vm.sp-fn.Returns+1:vm.sp+1])
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
}

func (vm *VM) constant(idx int) (types.Value, error) {
	if idx < 0 || idx >= len(vm.constants) {
		return nil, ErrReferenceInvalid
	}
	return vm.constants[idx], nil
}

func (vm *VM) gload(idx int) (types.Boxed, error) {
	if idx < 0 || idx >= len(vm.global) {
		return 0, ErrReferenceInvalid
	}
	val := vm.global[idx]
	if val.Kind() == types.KindRef {
		if err := vm.retain(val.Ref()); err != nil {
			return 0, err
		}
	}
	return val, nil
}

func (vm *VM) gstore(idx int, val types.Boxed) error {
	if idx < 0 {
		return ErrReferenceInvalid
	}
	if idx >= cap(vm.global) {
		global := make([]types.Boxed, len(vm.global), (idx+1)*2)
		copy(global, vm.global)
		vm.global = global
	}
	if idx >= len(vm.global) {
		vm.global = vm.global[:idx+1]
	}
	old := vm.global[idx]
	if old.Kind() == types.KindRef {
		if err := vm.free(old.Ref()); err != nil {
			return err
		}
	}
	vm.global[idx] = val
	return nil
}

func (vm *VM) lload(idx int) (types.Boxed, error) {
	frame := &vm.frames[vm.fp]
	if idx < 0 || idx >= frame.cl.Function.Locals {
		return 0, ErrReferenceInvalid
	}
	val := vm.stack[frame.bp+idx+1]
	if val.Kind() == types.KindRef {
		if err := vm.retain(val.Ref()); err != nil {
			return 0, err
		}
	}
	return val, nil
}

func (vm *VM) lstore(idx int, val types.Boxed) error {
	frame := &vm.frames[vm.fp]
	if idx < 0 || idx >= frame.cl.Function.Locals {
		return ErrReferenceInvalid
	}
	old := vm.stack[frame.bp+idx+1]
	if old.Kind() == types.KindRef {
		if err := vm.free(old.Ref()); err != nil {
			return err
		}
	}
	vm.stack[frame.bp+idx+1] = val
	return nil
}

func (vm *VM) lookup(addr int) (types.Value, error) {
	if addr < 0 || addr >= len(vm.heap) {
		return nil, ErrReferenceInvalid
	}
	return vm.heap[addr], nil
}

func (vm *VM) load(addr int) (types.Value, error) {
	if addr < 0 || addr >= len(vm.heap) {
		return nil, ErrReferenceInvalid
	}
	val := vm.heap[addr]
	if err := vm.free(addr); err != nil {
		return nil, err
	}
	return val, nil
}

func (vm *VM) store(val types.Value) (int, error) {
	addr, err := vm.alloc(val)
	if err != nil {
		return 0, err
	}
	return addr, nil
}

func (vm *VM) push(val types.Boxed) error {
	if vm.sp+1 >= len(vm.stack) {
		return ErrStackOverflow
	}
	vm.sp++
	vm.stack[vm.sp] = val
	return nil
}

func (vm *VM) poke(val types.Boxed) error {
	if vm.sp < 0 {
		return ErrStackUnderflow
	}
	vm.stack[vm.sp] = val
	return nil
}

func (vm *VM) pop() (types.Boxed, error) {
	if vm.sp < 0 {
		return 0, ErrStackUnderflow
	}
	val := vm.stack[vm.sp]
	vm.sp--
	return val, nil
}

func (vm *VM) peek() (types.Boxed, error) {
	if vm.sp < 0 {
		return 0, ErrStackUnderflow
	}
	return vm.stack[vm.sp], nil
}

func (vm *VM) swap() error {
	if vm.sp < 1 {
		return ErrStackUnderflow
	}
	vm.stack[vm.sp], vm.stack[vm.sp-1] = vm.stack[vm.sp-1], vm.stack[vm.sp]
	return nil
}

func (vm *VM) alloc(val types.Value) (int, error) {
	if len(vm.frees) > 0 {
		addr := vm.frees[len(vm.frees)-1]
		vm.frees = vm.frees[:len(vm.frees)-1]
		vm.heap[addr] = val
		return addr, nil
	}
	if len(vm.heap) == cap(vm.heap) {
		c := 2 * cap(vm.heap)
		if c == 0 {
			c = 1
		}
		heap := make([]types.Value, len(vm.heap), c)
		copy(heap, vm.heap)
		vm.heap = heap
		hits := make([]int, len(vm.hits), c)
		copy(hits, vm.hits)
		vm.hits = hits
	}
	vm.heap = append(vm.heap, val)
	vm.hits = append(vm.hits, 1)

	return len(vm.heap) - 1, nil
}

func (vm *VM) free(addr int) error {
	if addr < 0 || addr >= len(vm.heap) {
		return ErrReferenceInvalid
	}

	stack := []int{addr}
	for len(stack) > 0 {
		a := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		vm.hits[a]--
		if vm.hits[a] > 0 {
			continue
		}

		obj := vm.heap[a]
		vm.heap[a] = nil
		vm.hits[a] = 0
		vm.frees = append(vm.frees, a)

		if t, ok := obj.(types.Traceable); ok {
			for _, ref := range t.Refs() {
				stack = append(stack, int(ref))
			}
		}
	}
	return nil
}

func (vm *VM) retain(addr int) error {
	if addr < 0 || addr >= len(vm.hits) {
		return ErrReferenceInvalid
	}
	vm.hits[addr]++
	return nil
}
