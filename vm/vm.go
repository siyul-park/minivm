package vm

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
	Stack int
	Heap  int
	Frame int
}

type VM struct {
	code   []byte
	stack  []types.Boxed
	heap   []types.Value
	frees  []int
	hits   []int
	frames []Frame
	sp     int
	fp     int
}

var (
	ErrStackOverflow  = errors.New("stack overflow")
	ErrStackUnderflow = errors.New("stack underflow")
	ErrFrameOverflow  = errors.New("frame overflow")
	ErrUnknownOpcode  = errors.New("unknown opcode")
)

func New(prog *program.Program, opts ...Option) *VM {
	stack := 1024
	heap := 64
	frame := 64
	for _, opt := range opts {
		if opt.Stack > 0 {
			stack = opt.Stack
		}
		if opt.Heap > 0 {
			heap = opt.Heap
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
		frames: make([]Frame, frame),
		sp:     -1,
		fp:     -1,
	}
}

func (vm *VM) Run() error {
	vm.fp++
	if vm.fp >= len(vm.frames) {
		return fmt.Errorf("%w: fp=%d", ErrFrameOverflow, vm.fp)
	}

	frame := &vm.frames[vm.fp]
	frame.ip = 0

	for frame.ip < len(vm.code) {
		opcode := instr.Opcode(vm.code[frame.ip])
		switch opcode {
		case instr.NOP:
			frame.ip++

		case instr.I32_CONST:
			v1 := types.BoxI32(int32(binary.BigEndian.Uint32(vm.code[frame.ip+1:])))
			if err := vm.push(v1); err != nil {
				return err
			}
			frame.ip += 5

		case instr.I64_CONST:
			u1 := binary.BigEndian.Uint64(vm.code[frame.ip+1:])
			var v1 types.Boxed
			if types.IsBoxable(u1) {
				v1 = types.BoxI64(int64(u1))
			} else {
				addr := vm.alloc(types.I64(u1))
				vm.retain(addr)
				v1 = types.BoxRef(addr)
			}
			if err := vm.push(v1); err != nil {
				return err
			}
			frame.ip += 9

		case instr.F32_CONST:
			bits := binary.BigEndian.Uint32(vm.code[frame.ip+1:])
			v := types.BoxF32(math.Float32frombits(bits))
			if err := vm.push(v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.F64_CONST:
			bits := binary.BigEndian.Uint64(vm.code[frame.ip+1:])
			v := types.BoxF64(math.Float64frombits(bits))
			if err := vm.push(v); err != nil {
				return err
			}
			frame.ip += 9

		default:
			return fmt.Errorf("%w at ip=%d, opcode=0x%x", ErrUnknownOpcode, frame.ip, opcode)
		}
	}
	return nil
}

func (vm *VM) Push(val types.Value) error {
	switch val := val.(type) {
	case types.Boxed:
		return vm.push(val)
	default:
		addr := vm.alloc(val)
		vm.retain(addr)
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
		if vm.release(addr) {
			vm.free(addr)
		}
		return val, nil
	}
	return box, nil
}

func (vm *VM) Len() int {
	return vm.sp + 1
}

func (vm *VM) Clear() {
	vm.sp = -1
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

func (vm *VM) alloc(val types.Value) int {
	if len(vm.frees) > 0 {
		addr := vm.frees[len(vm.frees)-1]
		vm.frees = vm.frees[:len(vm.frees)-1]
		vm.heap[addr] = val
		return addr
	}
	vm.heap = append(vm.heap, val)
	vm.hits = append(vm.hits, 0)
	return len(vm.heap) - 1
}

func (vm *VM) free(addr int) {
	if addr < 0 || addr >= len(vm.heap) {
		return
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
}

func (vm *VM) retain(addr int) {
	if addr < 0 || addr >= len(vm.hits) {
		return
	}
	vm.hits[addr]++
}

func (vm *VM) release(addr int) bool {
	if addr < 0 || addr >= len(vm.hits) {
		return false
	}
	vm.hits[addr]--
	return vm.hits[addr] <= 0
}
