package vm

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Option struct {
	Stack int
	Heap  int
	Frame int
	Yield int
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
	yield  int
}

var (
	ErrStackOverflow       = errors.New("stack overflow")
	ErrStackUnderflow      = errors.New("stack underflow")
	ErrFrameOverflow       = errors.New("frame overflow")
	ErrUnreachableExecuted = errors.New("unreachable executed")
	ErrUnknownOpcode       = errors.New("unknown opcode")
)

func New(prog *program.Program, opts ...Option) *VM {
	stack := 1024
	heap := 64
	frame := 64
	yield := 128
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
		if opt.Yield > 0 {
			yield = opt.Yield
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
		fp:     0,
		yield:  yield,
	}
}

func (vm *VM) RunWithContext(ctx context.Context) error {
	instrs := program.New(vm.code).Instructions()
	injects := make([]instr.Instruction, 0, len(instrs)+len(instrs)/vm.yield+1)
	for i, op := range instrs {
		if i > 0 && i%vm.yield == 0 {
			injects = append(injects, instr.New(instr.UNREACHABLE))
		}
		injects = append(injects, op)
	}
	vm.code = program.New(injects...).Code

	for {
		err := vm.Run()
		if err != nil && errors.Is(err, ErrUnreachableExecuted) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(0):
				continue
			}
		}
		return err
	}
}

func (vm *VM) Run() error {
	frame := &vm.frames[vm.fp]

	for frame.ip < len(vm.code) {
		opcode := instr.Opcode(vm.code[frame.ip])
		switch opcode {
		case instr.NOP:
			frame.ip++

		case instr.UNREACHABLE:
			frame.ip++
			return fmt.Errorf("%w: at ip=%d", ErrUnreachableExecuted, frame.ip)

		case instr.I32_CONST:
			v := types.I32(binary.BigEndian.Uint32(vm.code[frame.ip+1:]))
			if err := vm.pushI32(v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.I64_CONST:
			v := types.I64(binary.BigEndian.Uint64(vm.code[frame.ip+1:]))
			if err := vm.pushI64(v); err != nil {
				return err
			}
			frame.ip += 9

		case instr.F32_CONST:
			v := types.F32(math.Float32frombits(binary.BigEndian.Uint32(vm.code[frame.ip+1:])))
			if err := vm.pushF32(v); err != nil {
				return err
			}
			frame.ip += 5

		case instr.F64_CONST:
			v := types.F64(math.Float64frombits(binary.BigEndian.Uint64(vm.code[frame.ip+1:])))
			if err := vm.pushF64(v); err != nil {
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

func (vm *VM) pushI32(val types.I32) error {
	return vm.push(types.BoxI32(int32(val)))
}

func (vm *VM) pushI64(val types.I64) error {
	var box types.Boxed
	if types.IsBoxable(uint64(val)) {
		box = types.BoxI64(int64(val))
	} else {
		addr := vm.alloc(val)
		vm.retain(addr)
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
		if vm.release(addr) {
			vm.free(addr)
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
