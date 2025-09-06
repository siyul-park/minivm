package vm

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Option struct {
	Stack int
	Frame int
}

type VM struct {
	code   []byte
	stack  []types.Boxed
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
	frame := 64
	for _, opt := range opts {
		if opt.Stack > 0 {
			stack = opt.Stack
		}
		if opt.Frame > 0 {
			frame = opt.Frame
		}
	}
	return &VM{
		code:   prog.Code,
		stack:  make([]types.Boxed, stack),
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
		default:
			return fmt.Errorf("%w at ip=%d, opcode=0x%x", ErrUnknownOpcode, frame.ip, opcode)
		}
	}
	return nil
}

func (vm *VM) Push(val types.Boxed) error {
	if vm.sp+1 >= len(vm.stack) {
		return fmt.Errorf("%w: sp=%d", ErrStackOverflow, vm.sp)
	}
	vm.sp++
	vm.stack[vm.sp] = val
	return nil
}

func (vm *VM) Pop() (types.Boxed, error) {
	if vm.sp < 0 {
		return 0, fmt.Errorf("%w: sp=%d", ErrStackUnderflow, vm.sp)
	}
	val := vm.stack[vm.sp]
	vm.sp--
	return val, nil
}

func (vm *VM) Peek() (types.Boxed, error) {
	if vm.sp < 0 {
		return 0, fmt.Errorf("%w: sp=%d", ErrStackUnderflow, vm.sp)
	}
	return vm.stack[vm.sp], nil
}

func (vm *VM) Len() int {
	return vm.sp + 1
}

func (vm *VM) Clear() {
	vm.sp = -1
}
