package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Interpreter struct {
	ctx        context.Context
	prof       *prof.Stats
	hook       func(*Interpreter) error
	marshaler  Marshaler
	buffer     *asm.Buffer
	instrs     [][]byte
	code       [][]func(*Interpreter)
	jitEntries map[int]*jitEntry
	frames     []frame
	types      []types.Type
	constants  []types.Boxed
	globals    []types.Boxed
	interned   map[string]types.Ref
	stack      []types.Boxed
	roots      []types.Boxed
	heap       []types.Value
	free       []int
	rc         []int
	fr         *frame
	fp         int
	sp         int
	tick       int
	threshold  int64
	fuel       int64
	cutoff     int
}

type frame struct {
	code    []func(*Interpreter)
	addr    int
	ip      int
	bp      int
	returns int
	release bool
}

type option struct {
	profile   *prof.Stats
	hook      func(*Interpreter) error
	marshaler Marshaler
	frame     int
	globals   int
	stack     int
	heap      int
	tick      int
	threshold int
	fuel      uint64
	cutoff    int
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
	ErrFuelExhausted       = errors.New("fuel exhausted")
)

func WithProfile(p *prof.Stats) func(*option) {
	return func(o *option) { o.profile = p }
}

func WithHook(fn func(*Interpreter) error) func(*option) {
	return func(o *option) { o.hook = fn }
}

func WithMarshaler(m Marshaler) func(*option) {
	return func(o *option) { o.marshaler = m }
}

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

func WithFuel(val uint64) func(*option) {
	return func(o *option) { o.fuel = val }
}

func WithCutoff(val int) func(*option) {
	return func(o *option) { o.cutoff = val }
}

func New(prog *program.Program, opts ...func(*option)) *Interpreter {
	opt := option{
		frame:     128,
		globals:   128,
		stack:     1024,
		heap:      128,
		tick:      128,
		threshold: 4096,
		cutoff:    8,
	}
	for _, o := range opts {
		o(&opt)
	}
	if opt.frame <= 0 {
		opt.frame = 1
	}
	if opt.tick <= 0 {
		opt.tick = 1
	}

	p := opt.profile
	if p == nil {
		p = prof.New()
	}
	m := opt.marshaler
	if m == nil {
		m = DefaultMarshaler
	}

	var fuel int64 = -1
	if opt.fuel > 0 {
		ticks := (opt.fuel-1)/uint64(opt.tick) + 1
		m := uint64(1<<63 - 1)
		if ticks > m {
			fuel = int64(m)
		}
		fuel = int64(ticks)
	}

	var threshold int64 = int64(opt.threshold)
	if threshold == 0 {
		threshold = 1
	} else if threshold > 0 {
		threshold = (threshold-1)/int64(opt.tick) + 1
	}

	i := &Interpreter{
		prof:       p,
		hook:       opt.hook,
		marshaler:  m,
		instrs:     make([][]byte, len(prog.Constants)+1),
		code:       make([][]func(*Interpreter), len(prog.Constants)+1),
		jitEntries: make(map[int]*jitEntry),
		frames:     make([]frame, opt.frame),
		types:      prog.Types,
		constants:  make([]types.Boxed, len(prog.Constants)),
		globals:    make([]types.Boxed, 0, opt.globals),
		interned:   make(map[string]types.Ref),
		stack:      make([]types.Boxed, opt.stack),
		heap:       make([]types.Value, 0, opt.heap),
		free:       make([]int, 0, opt.heap),
		rc:         make([]int, 0, opt.heap),
		fp:         0,
		sp:         0,
		tick:       opt.tick,
		threshold:  threshold,
		fuel:       fuel,
		cutoff:     opt.cutoff,
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
		case types.Ref:
			val = types.BoxRef(int(v))
		default:
			val = types.BoxRef(i.allocRoot(v))
		}
		i.constants[j] = val
	}

	c := &threadedCompiler{
		types:     i.types,
		constants: i.constants,
		heap:      i.heap,
		precise:   opt.tick == 1,
	}

	i.instrs[0] = prog.Code
	i.code[0] = c.Compile(prog.Code, nil)
	for j, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			var locals []types.Kind
			if fn.Typ != nil {
				if size := len(fn.Typ.Params) + len(fn.Locals); size != 0 {
					locals = make([]types.Kind, 0, size)
					for _, typ := range fn.Typ.Params {
						locals = append(locals, typ.Kind())
					}
					for _, typ := range fn.Locals {
						locals = append(locals, typ.Kind())
					}
				}
			}
			i.instrs[j+1] = fn.Code
			i.code[j+1] = c.Compile(fn.Code, locals)
		}
	}

	i.frames[0].code = i.code[0]
	i.frames[0].bp = i.sp
	i.fp = 1
	i.fr = &i.frames[0]
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

	f := i.fr
	code := f.code
	tick := i.tick
	fuel := i.fuel

	for f.ip < len(code) {
		tick--
		if tick == 0 {
			tick = i.tick
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if fuel >= 0 {
				if fuel == 0 {
					return ErrFuelExhausted
				}
				fuel--
			}

			if i.hook != nil {
				if err := i.hook(i); err != nil {
					return err
				}
			}

			i.prof.Add(f.addr, f.ip, i.instrs[f.addr][f.ip])
			if i.threshold >= 0 && i.prof.Samples(f.addr) == uint64(i.threshold) {
				if err := i.jit(f.addr); err != nil {
					return err
				}
			}
		}

		code[f.ip](i)

		f = i.fr
		code = f.code
	}
	return nil
}

func (i *Interpreter) Context() context.Context {
	return i.ctx
}

func (i *Interpreter) Func() int {
	return i.frame().addr
}

func (i *Interpreter) IP() int {
	return i.frame().ip
}

func (i *Interpreter) FrameDepth() int {
	return i.fp
}

func (i *Interpreter) Opcode() (instr.Opcode, error) {
	fn, ip := i.Func(), i.IP()
	if fn < 0 || fn >= len(i.instrs) || ip < 0 || ip >= len(i.instrs[fn]) {
		return 0, ErrSegmentationFault
	}
	return instr.Opcode(i.instrs[fn][ip]), nil
}

func (i *Interpreter) Frame(n int) (fn, ip, bp int, err error) {
	if n < 0 || n >= i.fp {
		return 0, 0, 0, ErrFrameUnderflow
	}
	f := i.frames[i.fp-1-n]
	return f.addr, f.ip, f.bp, nil
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
	if s, ok := val.(types.String); ok {
		return int(i.intern(string(s))), nil
	}
	return i.allocRoot(val), nil
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
	i.fr = &i.frames[i.fp-1]

	for idx := range i.globals {
		i.globals[idx] = 0
	}
	i.globals = i.globals[:0]

	i.sp = 0
	i.roots = i.roots[:0]
	clear(i.interned)

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

func (i *Interpreter) jit(addr int) error {
	if arch == nil {
		return nil
	}
	i.prof.JITAttempt()
	if i.buffer == nil {
		buffer, err := asm.NewBuffer(256)
		if err != nil {
			i.prof.JITError()
			return err
		}
		i.buffer = buffer
	}
	c := &jitCompiler{
		assembler: asm.NewAssembler(arch, i.buffer),
		profile:   i.prof,
		addr:      addr,
		constants: i.constants,
		globals:   i.globals,
		heap:      i.heap,
		cutoff:    i.cutoff,
		entries:   i.jitEntries,
		ip:        i,
	}
	for j, fn := range c.Compile(i.instrs[addr]) {
		if fn != nil {
			i.code[addr][j] = fn
		}
	}
	return nil
}

func (i *Interpreter) frame() *frame {
	return i.fr
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

// intern returns a heap ref for s. Interpreters are single-threaded; callers
// must not use one Interpreter concurrently from multiple goroutines.
func (i *Interpreter) intern(s string) types.Ref {
	if ref, ok := i.interned[s]; ok {
		i.retain(int(ref))
		return ref
	}
	ref := types.Ref(i.alloc(types.String(s)))
	i.interned[s] = ref
	return ref
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
	case types.Ref:
		return types.BoxRef(int(v))
	case types.String:
		return types.BoxRef(int(i.intern(string(v))))
	default:
		addr := i.allocRoot(v)
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
		i.rc[addr] = 1
		return addr
	}

	if len(i.heap) == cap(i.heap) {
		i.gc()
		if len(i.free) > 0 {
			addr := i.free[len(i.free)-1]
			i.free = i.free[:len(i.free)-1]
			i.heap[addr] = val
			i.rc[addr] = 1
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

func (i *Interpreter) allocRoot(val types.Value) int {
	roots := i.traceRoot(val)
	defer i.unroot(roots)
	return i.alloc(val)
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
			if s, ok := v.(types.String); ok {
				delete(i.interned, string(s))
			}
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

func (i *Interpreter) traceRoot(val types.Value) int {
	t, ok := val.(types.Traceable)
	if !ok {
		return 0
	}
	n := 0
	for _, ref := range t.Refs() {
		n += i.root(types.BoxRef(int(ref)))
	}
	return n
}

func (i *Interpreter) root(val types.Boxed) int {
	if val.Kind() != types.KindRef {
		return 0
	}
	i.roots = append(i.roots, val)
	return 1
}

func (i *Interpreter) unroot(n int) {
	if n == 0 {
		return
	}
	i.roots = i.roots[:len(i.roots)-n]
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
	for _, val := range i.roots {
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

func unboxRef[T types.Value](i *Interpreter, val types.Boxed) T {
	if val.Kind() != types.KindRef {
		panic(ErrTypeMismatch)
	}
	addr := val.Ref()
	v, ok := i.heap[addr].(T)
	if !ok {
		panic(ErrTypeMismatch)
	}
	i.release(addr)
	return v
}
