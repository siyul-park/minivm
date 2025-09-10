package vm

import (
	"fmt"

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

type VM struct {
	frames    []Frame
	constants []types.Value
	global    []types.Boxed
	stack     []types.Boxed
	heap      []types.Value
	frees     []int
	rc        []int
	fp        int
	sp        int
}

func New(prog *program.Program, opts ...Option) *VM {
	stack := 1024
	heap := 128
	global := 128
	frame := 128
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
		frames:    make([]Frame, frame),
		constants: prog.Constants,
		global:    make([]types.Boxed, 0, global),
		stack:     make([]types.Boxed, stack),
		heap:      make([]types.Value, 0, heap),
		rc:        make([]int, 0, heap),
		frees:     make([]int, 0, heap),
		fp:        1,
		sp:        0,
	}

	vm.heap = append(vm.heap, nil)
	vm.rc = append(vm.rc, 0)

	vm.frames[0].fn = &types.Function{Code: prog.Code}
	vm.frames[0].bp = vm.sp
	return vm
}

func (vm *VM) Run() error {
	frame := &vm.frames[vm.fp-1]
	code := frame.fn.Code

	for frame.ip < len(code) {
		opcode := instr.Opcode(code[frame.ip])
		fn := dispatch[opcode]
		if fn == nil {
			return fmt.Errorf("%w: at=%d", ErrUnknownOpcode, frame.ip)
		}
		if err := fn(vm); err != nil {
			return fmt.Errorf("%w: at=%d", err, frame.ip)
		}
		frame = &vm.frames[vm.fp-1]
		code = frame.fn.Code
	}
	return nil
}

func (vm *VM) Push(val types.Value) error {
	if vm.sp == len(vm.stack) {
		return ErrStackOverflow
	}

	switch val := val.(type) {
	case types.Boxed:
		vm.stack[vm.sp] = val
	default:
		addr := vm.alloc(val)
		vm.stack[vm.sp] = types.BoxRef(addr)
	}
	vm.sp++
	return nil
}

func (vm *VM) Pop() (types.Value, error) {
	if vm.sp == 0 {
		return nil, ErrStackUnderflow
	}

	vm.sp--
	val := vm.stack[vm.sp]

	if val.Kind() == types.KindRef {
		addr := val.Ref()
		v := vm.heap[addr]
		vm.release(addr)
		return v, nil
	}
	return val, nil
}

func (vm *VM) Len() int {
	return vm.sp - 1
}

func (vm *VM) Clear() {
	for vm.fp > 1 {
		vm.frames[vm.fp] = Frame{}
		vm.fp--
	}
	vm.frames[vm.fp-1].bp = vm.sp
	vm.frames[vm.fp-1].ip = 0

	for i := range vm.global {
		vm.global[i] = 0
	}
	vm.global = vm.global[:0]

	vm.sp = 0

	for i := range vm.heap {
		vm.heap[i] = nil
	}
	for i := range vm.rc {
		vm.rc[i] = 0
	}
	for i := range vm.frees {
		vm.frees[i] = 0
	}
	vm.heap = vm.heap[:1]
	vm.rc = vm.rc[:1]
	vm.frees = vm.frees[:0]
}

func (vm *VM) boxI64(val int64) types.Boxed {
	if types.IsBoxable(val) {
		return types.BoxI64(val)
	}
	addr := vm.alloc(types.I64(val))
	return types.BoxRef(addr)
}

func (vm *VM) unboxI64(val types.Boxed) int64 {
	if val.Kind() != types.KindRef {
		return val.I64()
	}
	addr := val.Ref()
	v, _ := vm.heap[addr].(types.I64)
	vm.release(addr)
	return int64(v)
}

func (vm *VM) alloc(val types.Value) int {
	if len(vm.frees) > 0 {
		addr := vm.frees[len(vm.frees)-1]
		vm.frees = vm.frees[:len(vm.frees)-1]
		vm.heap[addr] = val
		return addr
	}

	if len(vm.heap) == cap(vm.heap) {
		c := 2 * cap(vm.heap)
		if c == 0 {
			c = 1
		}
		heap := make([]types.Value, len(vm.heap), c)
		copy(heap, vm.heap)
		vm.heap = heap

		hits := make([]int, len(vm.rc), c)
		copy(hits, vm.rc)
		vm.rc = hits
	}

	vm.heap = append(vm.heap, val)
	vm.rc = append(vm.rc, 1)
	return len(vm.heap) - 1
}

func (vm *VM) retain(addr int) {
	vm.rc[addr]++
}

func (vm *VM) release(addr int) {
	stack := []int{addr}
	for len(stack) > 0 {
		a := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		vm.rc[a]--
		if vm.rc[a] > 0 {
			continue
		}

		obj := vm.heap[a]
		vm.heap[a] = nil
		vm.rc[a] = 0
		vm.frees = append(vm.frees, a)

		if t, ok := obj.(types.Traceable); ok {
			for _, ref := range t.Refs() {
				stack = append(stack, int(ref))
			}
		}
	}
}
