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
	code   []byte
	stack  []types.Boxed
	heap   []types.Value
	frees  []int
	hits   []int
	global []types.Boxed
	frames []Frame
	sp     int
	fp     int
}

var (
	ErrUnknownOpcode       = errors.New("unknown opcode")
	ErrUnreachableExecuted = errors.New("unreachable executed")
	ErrReferenceNotFound   = errors.New("reference not found")
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
	return &VM{
		code:   prog.Code,
		stack:  make([]types.Boxed, stack),
		heap:   make([]types.Value, 0, heap),
		hits:   make([]int, 0, heap),
		frees:  make([]int, 0, heap),
		global: make([]types.Boxed, 0, global),
		frames: make([]Frame, frame),
		sp:     -1,
		fp:     -1,
	}
}

func (vm *VM) Run() error {
	if vm.fp+1 >= len(vm.frames) {
		return ErrFrameOverflow
	}
	vm.fp++
	frame := &vm.frames[vm.fp]

	for frame.ip < len(vm.code) {
		opcode := instr.Opcode(vm.code[frame.ip])
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
				vm.retain(v.Ref())
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
			v := int(binary.BigEndian.Uint32(vm.code[frame.ip+1:]))
			frame.ip += v + 5

		case instr.BR_IF:
			v1 := int(binary.BigEndian.Uint32(vm.code[frame.ip+1:]))
			v2, err := vm.popI32()
			if err != nil {
				return err
			}
			if v2 != 0 {
				frame.ip += v1 + 5
			} else {
				frame.ip += 5
			}

		case instr.GLOBAL_GET:
			v1 := int(binary.BigEndian.Uint32(vm.code[frame.ip+1:]))
			v2, err := vm.globalGet(v1)
			if err != nil {
				return err
			}
			if err := vm.push(v2); err != nil {
				return err
			}
			frame.ip += 5

		case instr.GLOBAL_SET:
			v1 := int(binary.BigEndian.Uint32(vm.code[frame.ip+1:]))
			v2, err := vm.pop()
			if err != nil {
				return err
			}
			if err := vm.globalSet(v1, v2); err != nil {
				return err
			}
			frame.ip += 5

		case instr.GLOBAL_TEE:
			v1 := int(binary.BigEndian.Uint32(vm.code[frame.ip+1:]))
			v2, err := vm.peek()
			if err != nil {
				return err
			}
			if err := vm.globalSet(v1, v2); err != nil {
				return err
			}
			frame.ip += 5

		case instr.I32_CONST:
			v := types.I32(binary.BigEndian.Uint32(vm.code[frame.ip+1:]))
			if err := vm.pushI32(v); err != nil {
				return err
			}
			frame.ip += 5

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
			v := types.I64(binary.BigEndian.Uint64(vm.code[frame.ip+1:]))
			if err := vm.pushI64(v); err != nil {
				return err
			}
			frame.ip += 9

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
			v := types.F32(math.Float32frombits(binary.BigEndian.Uint32(vm.code[frame.ip+1:])))
			if err := vm.pushF32(v); err != nil {
				return err
			}
			frame.ip += 5

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
			v := types.F64(math.Float64frombits(binary.BigEndian.Uint64(vm.code[frame.ip+1:])))
			if err := vm.pushF64(v); err != nil {
				return err
			}
			frame.ip += 9

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
		if err := vm.retain(addr); err != nil {
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
		if ok, err := vm.release(addr); err != nil {
			return nil, err
		} else if ok {
			if err := vm.free(addr); err != nil {
				return nil, err
			}
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

	for vm.fp >= 0 {
		vm.frames[vm.fp] = Frame{}
		vm.fp--
	}

	vm.heap = vm.heap[:0]
	vm.hits = vm.hits[:0]
	vm.frees = vm.frees[:0]
}

func (vm *VM) globalGet(idx int) (types.Boxed, error) {
	if idx < 0 || idx >= len(vm.global) {
		return 0, ErrReferenceNotFound
	}
	val := vm.global[idx]
	if val.Kind() == types.KindRef {
		if err := vm.retain(val.Ref()); err != nil {
			return 0, err
		}
	}
	return val, nil
}

func (vm *VM) globalSet(idx int, val types.Boxed) error {
	if idx < 0 {
		return ErrReferenceNotFound
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
		if ok, err := vm.release(old.Ref()); err != nil {
			return err
		} else if ok {
			if err := vm.free(old.Ref()); err != nil {
				return err
			}
		}
	}
	if val.Kind() == types.KindRef {
		if err := vm.retain(val.Ref()); err != nil {
			return err
		}
	}
	vm.global[idx] = val
	return nil
}

func (vm *VM) pushI32(val types.I32) error {
	return vm.push(types.BoxI32(int32(val)))
}

func (vm *VM) pushI64(val types.I64) error {
	var box types.Boxed
	if types.IsBoxable(uint64(val)) {
		box = types.BoxI64(int64(val))
	} else {
		addr, err := vm.alloc(val)
		if err != nil {
			return err
		}
		if err := vm.retain(addr); err != nil {
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
		addr := box.Ref()
		val := vm.heap[addr]
		if ok, err := vm.release(addr); err != nil {
			return 0, err
		} else if ok {
			if err := vm.free(addr); err != nil {
				return 0, err
			}
		}
		v, _ := val.(types.I64)
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

func (vm *VM) push(val types.Boxed) error {
	if vm.sp+1 >= len(vm.stack) {
		return ErrStackOverflow
	}
	vm.stack[vm.sp+1] = val
	vm.sp++
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
	vm.heap = append(vm.heap, val)
	vm.hits = append(vm.hits, 0)
	return len(vm.heap) - 1, nil
}

func (vm *VM) free(addr int) error {
	if addr < 0 || addr >= len(vm.heap) {
		return ErrReferenceNotFound
	}
	vm.heap[addr] = nil
	vm.hits[addr] = 0

	vm.frees = append(vm.frees, addr)

	for addr == len(vm.heap)-1 && len(vm.heap) > 0 {
		vm.heap = vm.heap[:len(vm.heap)-1]
		vm.hits = vm.hits[:len(vm.hits)-1]
		if len(vm.frees) > 0 && vm.frees[len(vm.frees)-1] == addr {
			vm.frees = vm.frees[:len(vm.frees)-1]
		}
		addr = len(vm.heap) - 1
	}

	return nil
}

func (vm *VM) retain(addr int) error {
	if addr < 0 || addr >= len(vm.hits) {
		return ErrReferenceNotFound
	}
	vm.hits[addr]++
	return nil
}

func (vm *VM) release(addr int) (bool, error) {
	if addr < 0 || addr >= len(vm.hits) {
		return false, ErrReferenceNotFound
	}
	vm.hits[addr]--
	return vm.hits[addr] <= 0, nil
}
