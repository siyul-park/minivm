package interp

import (
	"context"
	"errors"
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
	Tick   int
}

type Interpreter struct {
	frames    []frame
	constants []types.Value
	types     []types.Type
	global    []types.Boxed
	stack     []types.Boxed
	heap      []types.Value
	free      []int
	rc        []int
	fp        int
	sp        int
	tick      int
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
	ErrIndexOutOfRange     = errors.New("index out of range")
)

func New(prog *program.Program, opts ...Option) *Interpreter {
	f := 128
	g := 128
	s := 1024
	h := 128
	t := 1024
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
		if opt.Tick > 0 {
			t = opt.Tick
		}
	}
	if f <= 0 {
		f = 1
	}

	i := &Interpreter{
		frames:    make([]frame, f),
		constants: prog.Constants,
		types:     prog.Types,
		global:    make([]types.Boxed, 0, g),
		stack:     make([]types.Boxed, s),
		heap:      make([]types.Value, 0, h),
		rc:        make([]int, 0, h),
		free:      make([]int, 0, h),
		fp:        1,
		sp:        0,
		tick:      t,
	}

	i.heap = append(i.heap, nil)
	i.rc = append(i.rc, -1)

	i.frames[0].fn = &types.Function{Code: prog.Code}
	i.frames[0].bp = i.sp
	return i
}

func (i *Interpreter) Run(ctx context.Context) error {
	f := &i.frames[i.fp-1]
	code := f.fn.Code
	tick := i.tick

	for f.ip < len(code) {
		tick--
		if tick == 0 {
			tick = i.tick
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		opcode := instr.Opcode(code[f.ip])
		fn := dispatch[opcode]
		if fn == nil {
			return fmt.Errorf("%w: at=%d", ErrUnknownOpcode, f.ip)
		}
		if err := fn(i); err != nil {
			return fmt.Errorf("%w: at=%d", err, f.ip)
		}

		f = &i.frames[i.fp-1]
		code = f.fn.Code
	}
	return nil
}

func (i *Interpreter) Push(val types.Value) error {
	if i.sp == len(i.stack) {
		return ErrStackOverflow
	}
	i.stack[i.sp] = i.box(val)
	i.sp++
	return nil
}

func (i *Interpreter) Pop() (types.Value, error) {
	if i.sp == 0 {
		return nil, ErrStackUnderflow
	}
	i.sp--
	return i.unbox(i.stack[i.sp]), nil
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

func (i *Interpreter) box(val types.Value) types.Boxed {
	switch v := val.(type) {
	case types.Boxed:
		return v
	default:
		addr := i.alloc(v)
		return types.BoxRef(addr)
	}
}

func (i *Interpreter) unbox(val types.Boxed) types.Value {
	if val.Kind() != types.KindRef {
		return types.Unbox(val)
	}
	addr := val.Ref()
	v := i.heap[addr]
	i.release(addr)
	return v
}

func (i *Interpreter) alloc(val types.Value) int {
	if len(i.free) > 0 {
		addr := i.free[len(i.free)-1]
		i.free = i.free[:len(i.free)-1]
		i.heap[addr] = val
		return addr
	}

	if len(i.heap) == cap(i.heap) {
		i.gc()
		if len(i.free) > 0 {
			addr := i.free[len(i.free)-1]
			i.free = i.free[:len(i.free)-1]
			i.heap[addr] = val
			return addr
		}

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

func (i *Interpreter) retains(addr int, n int) {
	i.rc[addr] += n
}

func (i *Interpreter) release(addr int) {
	stack := []int{addr}
	for len(stack) > 0 {
		addr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		i.rc[addr]--
		if i.rc[addr] == 0 {
			t, ok := i.heap[addr].(types.Traceable)
			if ok {
				for _, r := range t.Refs() {
					stack = append(stack, int(r))
				}
			}
			i.heap[addr] = nil
			i.free = append(i.free, addr)
		}
	}
}

func (i *Interpreter) gc() {
	for j := 0; j < len(i.heap); j++ {
		if i.rc[j] < 0 {
			i.rc[j] = 0
		}
		i.rc[j] *= -1
	}

	var stack []int
	push := func(addr int) {
		if i.rc[addr] < 0 {
			i.rc[addr] *= -1
			stack = append(stack, addr)
		}
	}

	for j := 0; j < i.sp; j++ {
		val := i.stack[j]
		if val.Kind() == types.KindRef {
			push(val.Ref())
		}
	}
	for _, val := range i.global {
		if val.Kind() == types.KindRef {
			push(val.Ref())
		}
	}

	for len(stack) > 0 {
		addr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if t, ok := i.heap[addr].(types.Traceable); ok {
			for _, ref := range t.Refs() {
				push(int(ref))
			}
		}
	}

	for j := 0; j < len(i.heap); j++ {
		if i.rc[j] < 0 {
			i.heap[j] = nil
			i.rc[j] = 0
			i.free = append(i.free, j)
		}
	}
}
