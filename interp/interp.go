package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Interpreter struct {
	ctx       context.Context
	buffer    *asm.Buffer
	instrs    [][]byte
	code      [][]func(*Interpreter)
	prof      *prof.Profile
	frames    []frame
	types     []types.Type
	constants []types.Boxed
	globals   []types.Boxed
	stack     []types.Boxed
	heap      []types.Value
	free      []int
	rc        []int
	fp        int
	sp        int
	tick      int
	threshold uint64
}

type frame struct {
	code []func(*Interpreter)
	addr int
	ip   int
	bp   int
}

type option struct {
	frame     int
	globals   int
	stack     int
	heap      int
	tick      int
	threshold int
	profile   *prof.Profile
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

func WithFrame(val int) func(*option) {
	return func(o *option) { o.frame = val }
}

func WithGlobals(val int) func(*option) {
	return func(o *option) { o.globals = val }
}

func WithStack(val int) func(*option) {
	return func(o *option) { o.stack = val }
}

func WithHeap(val int) func(*option) {
	return func(o *option) { o.heap = val }
}

func WithTick(val int) func(*option) {
	return func(o *option) { o.tick = val }
}

func WithThreshold(val int) func(*option) {
	return func(o *option) { o.threshold = val }
}

func WithProfile(p *prof.Profile) func(*option) {
	return func(o *option) { o.profile = p }
}

func New(prog *program.Program, opts ...func(*option)) *Interpreter {
	opt := option{
		frame:     128,
		globals:   128,
		stack:     1024,
		heap:      128,
		tick:      128,
		threshold: 4096,
	}
	for _, o := range opts {
		o(&opt)
	}
	if opt.frame <= 0 {
		opt.frame = 1
	}

	p := opt.profile
	if p == nil {
		p = prof.New()
	}

	i := &Interpreter{
		instrs:    make([][]byte, len(prog.Constants)+1),
		code:      make([][]func(*Interpreter), len(prog.Constants)+1),
		prof:      p,
		frames:    make([]frame, opt.frame),
		types:     prog.Types,
		constants: make([]types.Boxed, len(prog.Constants)),
		globals:   make([]types.Boxed, 0, opt.globals),
		stack:     make([]types.Boxed, opt.stack),
		heap:      make([]types.Value, 0, opt.heap),
		rc:        make([]int, 0, opt.heap),
		free:      make([]int, 0, opt.heap),
		fp:        0,
		sp:        0,
		tick:      opt.tick,
		threshold: uint64(opt.threshold / opt.tick),
	}

	i.alloc(types.Null)

	for j, v := range prog.Constants {
		var val types.Boxed
		switch v := v.(type) {
		case types.Boxed:
			val = v
		case types.I32:
			val = types.BoxI32(int32(v))
		case types.I64:
			val = i.boxI64(int64(v))
		case types.F32:
			val = types.BoxF32(float32(v))
		case types.F64:
			val = types.BoxF64(float64(v))
		default:
			val = types.BoxRef(i.alloc(v))
		}
		i.constants[j] = val
	}

	c := &threadedCompiler{
		types:     i.types,
		constants: i.constants,
		heap:      i.heap,
	}

	i.instrs[0] = prog.Code
	i.code[0] = c.Compile(prog.Code)
	for j, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			i.instrs[j+1] = fn.Code
			i.code[j+1] = c.Compile(fn.Code)
		}
	}

	i.frames[0].code = i.code[0]
	i.frames[0].bp = i.sp
	i.fp = 1
	i.retain(0)

	return i
}

func (i *Interpreter) Run(ctx context.Context) (err error) {
	i.ctx = ctx
	defer func() {
		i.ctx = nil
		if r := recover(); r != nil {
			err = i.error(r)
		}
	}()

	f := i.frame()
	code := f.code
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

			i.prof.Record(f.addr, f.ip)

			if i.prof.Calls(f.addr) == i.threshold {
				if arch != nil {
					if i.buffer == nil {
						i.buffer, err = asm.NewBuffer(256)
						if err != nil {
							return err
						}
					}
					c := &jitCompiler{
						assembler: asm.NewAssembler(arch, i.buffer),
						types:     i.types,
						constants: i.constants,
						heap:      i.heap,
						profile:   i.prof,
						funcIdx:   f.addr,
					}
					for j, fn := range c.Compile(i.instrs[f.addr]) {
						if fn != nil {
							i.code[f.addr][j] = fn
						}
					}
				}
			}
		}

		code[f.ip](i)

		f = &i.frames[i.fp-1]
		code = f.code
	}
	return nil
}

func (i *Interpreter) Context() context.Context {
	return i.ctx
}

func (i *Interpreter) Profile() *prof.Profile {
	return i.prof
}

func (i *Interpreter) Const(idx int) (types.Boxed, error) {
	if idx < 0 || idx >= len(i.constants) {
		return 0, ErrSegmentationFault
	}
	return i.constants[idx], nil
}

func (i *Interpreter) Global(idx int) (types.Boxed, error) {
	if idx < 0 || idx >= len(i.globals) {
		return 0, ErrSegmentationFault
	}
	val := i.globals[idx]
	return val, nil
}

func (i *Interpreter) SetGlobal(idx int, val types.Boxed) error {
	if idx < 0 || idx >= len(i.globals) {
		return ErrSegmentationFault
	}
	old := i.globals[idx]
	if old.Kind() == types.KindRef {
		i.release(old.Ref())
	}
	i.globals[idx] = val
	return nil
}

func (i *Interpreter) Local(idx int) (types.Boxed, error) {
	f := i.frame()
	addr := f.bp + idx
	if addr < 0 || addr >= i.sp {
		return 0, ErrSegmentationFault
	}
	return i.stack[addr], nil
}

func (i *Interpreter) SetLocal(idx int, val types.Boxed) error {
	f := i.frame()
	addr := f.bp + idx
	if addr < 0 || addr >= i.sp {
		return ErrSegmentationFault
	}
	old := i.stack[addr]
	if old.Kind() == types.KindRef {
		i.release(old.Ref())
	}
	i.stack[addr] = val
	return nil
}

func (i *Interpreter) Load(addr int) (types.Value, error) {
	if addr < 0 || addr >= len(i.heap) || i.rc[addr] <= 0 {
		return nil, ErrSegmentationFault
	}
	return i.heap[addr], nil
}

func (i *Interpreter) Store(addr int, val types.Value) error {
	if addr < 0 || addr >= len(i.heap) || i.rc[addr] <= 0 {
		return ErrSegmentationFault
	}
	if v, ok := val.(types.Boxed); ok {
		if v.Kind() == types.KindRef {
			val = i.heap[v.Ref()]
		} else {
			val = types.Unbox(v)
		}
	}
	i.heap[addr] = val
	return nil
}

func (i *Interpreter) Alloc(val types.Value) (int, error) {
	if v, ok := val.(types.Boxed); ok {
		if v.Kind() == types.KindRef {
			return v.Ref(), nil
		}
		val = types.Unbox(v)
	}
	return i.alloc(val), nil
}

func (i *Interpreter) Retain(addr int) (types.Value, error) {
	if addr < 0 || addr >= len(i.heap) || i.rc[addr] <= 0 {
		return nil, ErrSegmentationFault
	}
	i.retain(addr)
	return i.heap[addr], nil
}

func (i *Interpreter) Release(addr int) error {
	if addr < 0 || addr >= len(i.heap) || i.rc[addr] <= 0 {
		return ErrSegmentationFault
	}
	i.release(addr)
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

// Peek returns the raw NaN-boxed value at position n from the top of the stack
// (n=0 is TOS) without consuming it or modifying reference counts.
func (i *Interpreter) Peek(n int) (types.Boxed, error) {
	if n < 0 || i.sp <= n {
		return 0, ErrStackUnderflow
	}
	return i.stack[i.sp-1-n], nil
}

func (i *Interpreter) Len() int {
	return i.sp
}

func (i *Interpreter) Close() error {
	i.Reset()
	if i.buffer != nil {
		if err := i.buffer.Free(); err != nil {
			return err
		}
		i.buffer = nil
	}
	return nil
}

func (i *Interpreter) Reset() {
	for i.fp > 1 {
		i.frames[i.fp] = frame{}
		i.fp--
	}
	i.frames[i.fp-1].bp = i.sp
	i.frames[i.fp-1].ip = 0

	for idx := range i.globals {
		i.globals[idx] = 0
	}
	i.globals = i.globals[:0]

	i.sp = 0

	constants := 1
	for _, v := range i.constants {
		if v.Kind() == types.KindRef {
			constants++
		}
	}

	i.heap = i.heap[:constants]
	i.rc = i.rc[:constants]
	for j := 0; j < constants; j++ {
		i.rc[j] = 1
	}
	i.free = i.free[:0]
}

func (i *Interpreter) frame() *frame {
	return &i.frames[i.fp-1]
}

func (i *Interpreter) error(r any) error {
	ip := i.frame().ip
	switch e := r.(type) {
	case error:
		return fmt.Errorf("%w: at=%d", e, ip)
	default:
		return fmt.Errorf("%v: at=%d", r, ip)
	}
}

func (i *Interpreter) unbox64(val types.Boxed) uint64 {
	switch val.Kind() {
	case types.KindI32:
		return uint64(uint32(val.I32()))
	case types.KindI64:
		return uint64(val.I64())
	case types.KindF32:
		return uint64(math.Float32bits(val.F32()))
	case types.KindF64:
		return math.Float64bits(val.F64())
	case types.KindRef:
		addr := val.Ref()
		v, _ := i.heap[addr].(types.I64)
		i.release(addr)
		return uint64(v)
	default:
		return uint64(val)
	}
}

func (i *Interpreter) box64(val uint64, kind types.Kind) types.Boxed {
	switch kind {
	case types.KindI32:
		return types.BoxI32(int32(val))
	case types.KindI64:
		return i.boxI64(int64(val))
	case types.KindF32:
		return types.BoxF32(math.Float32frombits(uint32(val)))
	case types.KindF64:
		return types.BoxF64(math.Float64frombits(val))
	case types.KindRef:
		return types.BoxRef(int(val))
	default:
		return types.Box(val, kind)
	}
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
	case types.I32:
		return types.BoxI32(int32(v))
	case types.I64:
		return i.boxI64(int64(v))
	case types.F32:
		return types.BoxF32(float32(v))
	case types.F64:
		return types.BoxF64(float64(v))
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
			v := i.heap[addr]
			if t, ok := v.(types.Traceable); ok {
				for _, r := range t.Refs() {
					stack = append(stack, int(r))
				}
			}
			if c, ok := v.(io.Closer); ok {
				_ = c.Close()
			}
			i.heap[addr] = nil
			i.free = append(i.free, addr)
		}
	}
}

func (i *Interpreter) gc() {
	for j := 1; j < len(i.heap); j++ {
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
	for _, val := range i.constants {
		if val.Kind() == types.KindRef {
			push(val.Ref())
		}
	}
	for _, val := range i.globals {
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
