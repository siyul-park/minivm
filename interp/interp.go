package interp

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

type Interpreter struct {
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

func New(prog *program.Program, opts ...Option) *Interpreter {
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

	i := &Interpreter{
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
		i.frames[i.fp] = Frame{}
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
	for idx := range i.frees {
		i.frees[idx] = 0
	}
	i.heap = i.heap[:1]
	i.rc = i.rc[:1]
	i.frees = i.frees[:0]
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
	if len(i.frees) > 0 {
		addr := i.frees[len(i.frees)-1]
		i.frees = i.frees[:len(i.frees)-1]
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
		i.frees = append(i.frees, a)

		if t, ok := obj.(types.Traceable); ok {
			for _, ref := range t.Refs() {
				stack = append(stack, int(ref))
			}
		}
	}
}
