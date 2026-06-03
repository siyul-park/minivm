package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Interpreter struct {
	ctx       context.Context
	prof      *prof.Stats
	hook      func(*Interpreter) error
	marshaler Marshaler

	compiler *jit.Compiler

	types     []types.Type
	constants []types.Boxed
	globals   []types.Boxed
	instrs    [][]byte
	code      [][]func(*Interpreter)

	frames   []frame
	fr       *frame
	stack    []types.Boxed
	roots    []types.Boxed
	heap     []types.Value
	interned map[string]types.Ref
	jitted   map[int]bool
	free     []int
	rc       []int

	fp int
	sp int

	threshold int64
	cutoff    int
	tick      int
	fuel      int64
}

type frame struct {
	addr    int
	returns int

	code   []func(*Interpreter)
	upvals []types.Boxed

	ref     int
	release bool

	ip int
	bp int
}

type option struct {
	profile   *prof.Stats
	hook      func(*Interpreter) error
	marshaler Marshaler
	threshold int
	cutoff    int

	frame   int
	globals int
	stack   int
	heap    int
	tick    int
	fuel    uint64
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
		fuel = int64(min(ticks, 1<<63-1))
	}

	var threshold int64 = int64(opt.threshold)
	if threshold == 0 {
		threshold = 1
	} else if threshold > 0 {
		threshold = (threshold-1)/int64(opt.tick) + 1
	}

	i := &Interpreter{
		prof:      p,
		hook:      opt.hook,
		marshaler: m,
		threshold: threshold,
		cutoff:    opt.cutoff,
		types:     prog.Types,
		constants: make([]types.Boxed, len(prog.Constants)),
		globals:   make([]types.Boxed, 0, opt.globals),
		instrs:    make([][]byte, len(prog.Constants)+1),
		code:      make([][]func(*Interpreter), len(prog.Constants)+1),
		frames:    make([]frame, opt.frame),
		stack:     make([]types.Boxed, opt.stack),
		heap:      make([]types.Value, 0, opt.heap),
		interned:  make(map[string]types.Ref),
		free:      make([]int, 0, opt.heap),
		rc:        make([]int, 0, opt.heap),
		tick:      opt.tick,
		fuel:      fuel,
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
			val = types.BoxRef(i.keep(v))
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

func (i *Interpreter) Marshal(v any) (types.Value, error) {
	return i.marshaler.Marshal(i, v)
}

func (i *Interpreter) Unmarshal(v types.Value, dst any) error {
	return i.marshaler.Unmarshal(i, v, dst)
}

func (i *Interpreter) Context() context.Context {
	return i.ctx
}

func (i *Interpreter) Func() int {
	return i.fr.addr
}

func (i *Interpreter) IP() int {
	return i.fr.ip
}

func (i *Interpreter) FP() int {
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
	f := i.fr
	addr := f.bp + idx
	if addr < 0 || addr >= i.sp {
		return 0, ErrSegmentationFault
	}
	return i.stack[addr], nil
}

func (i *Interpreter) SetLocal(idx int, val types.Boxed) error {
	f := i.fr
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
	return i.keep(val), nil
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
	if i.compiler != nil {
		if err := i.compiler.Close(); err != nil {
			return err
		}
		i.compiler = nil
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
	if jit.Active() == nil {
		return nil
	}
	if i.compiler == nil {
		compiler, err := jit.New(jit.WithCutoff(i.cutoff))
		if err != nil {
			i.prof.JITError()
			return err
		}
		i.compiler = compiler
	}
	if _, err := i.compiler.Slots(); err != nil {
		i.prof.JITError()
		return err
	}
	i.prof.JITAttempt()

	fn, ok := i.function(addr)
	if !ok {
		return nil
	}

	mod, err := i.compiler.Compile(fn, addr, i.snapshot(addr, fn))
	if err != nil {
		i.prof.JITError()
		return err
	}
	if mod == nil {
		return nil
	}
	for _, n := range mod.Bytes {
		i.prof.JITEmit(n)
	}
	for range mod.Links {
		i.prof.JITLink()
	}
	for range mod.Skips {
		i.prof.JITSkip()
	}
	if mod.Entry != nil && addr > 0 && addr < len(i.code) && len(i.code[addr]) > 0 {
		i.code[addr][0] = i.entry(mod.Entry)
		if i.jitted == nil {
			i.jitted = make(map[int]bool)
		}
		i.jitted[addr] = true
	}
	for ip, callable := range mod.Segments {
		if ip < 0 || ip >= len(i.code[addr]) || callable == nil {
			continue
		}
		i.code[addr][ip] = i.segment(callable, mod.Stacks[ip])
	}
	return nil
}

// snapshot bundles the consumer-side tables the JIT inspects at compile
// time. Locals concatenates the function's params and declared locals so
// LOCAL_* lowering can check kinds by raw index.
func (i *Interpreter) snapshot(addr int, fn *types.Function) jit.Snapshot {
	var locals []types.Kind
	if fn != nil && fn.Typ != nil {
		size := len(fn.Typ.Params) + len(fn.Locals)
		if size > 0 {
			locals = make([]types.Kind, 0, size)
			for _, t := range fn.Typ.Params {
				locals = append(locals, t.Kind())
			}
			for _, t := range fn.Locals {
				locals = append(locals, t.Kind())
			}
		}
	}
	// Collect any function refs present in constants so the CALL lowerer can
	// look up param/return types at compile time.
	var functions map[int]*types.Function
	for _, v := range i.constants {
		if v.Kind() != types.KindRef {
			continue
		}
		a := v.Ref()
		if !i.jitted[a] {
			continue
		}
		fn, ok := i.function(a)
		if !ok {
			continue
		}
		if functions == nil {
			functions = make(map[int]*types.Function)
		}
		functions[a] = fn
	}
	return jit.Snapshot{
		Constants: i.constants,
		Globals:   i.globals,
		Locals:    locals,
		Functions: functions,
		Hot:       i.hot(addr),
	}
}

func (i *Interpreter) hot(addr int) []int {
	fn := i.prof.Func(addr)
	if len(fn.IPs) == 0 {
		return nil
	}
	ips := make([]int, 0, len(fn.IPs))
	for _, ip := range fn.IPs {
		ips = append(ips, ip.Offset)
	}
	return ips
}

// function returns the *types.Function at addr in the heap, or false if
// addr does not point at a function.
func (i *Interpreter) function(addr int) (*types.Function, bool) {
	if addr == 0 {
		return &types.Function{Code: i.instrs[0]}, true
	}
	if addr <= 0 || addr >= len(i.heap) {
		return nil, false
	}
	fn, ok := i.heap[addr].(*types.Function)
	return fn, ok
}

// segment wraps a native segment Callable in a threaded-style closure. The
// closure passes consumed VM stack values as Callable args, marshals VM
// context through scratch slots, and appends Callable returns back to the
// interpreter stack.
//
// Scratch slot conventions use jit.Scratch*:
//
//	ScratchStack   → &i.stack[0]
//	ScratchGlobals → &i.globals[0]
//	ScratchBP      → i.fr.bp
//	ScratchNext    → next IP (out)
func (i *Interpreter) segment(callable asm.Callable, argc int) func(*Interpreter) {
	in := make([]asm.Value, argc)
	scratch := make([]uint64, jit.ScratchCount)
	return func(i *Interpreter) {
		if i.sp < argc {
			panic(ErrStackUnderflow)
		}
		base := i.sp - argc
		for n := range in {
			in[n] = jit.Arg(i.stack[base+n])
		}

		scratch[jit.ScratchStack] = stackBase(i.stack)
		scratch[jit.ScratchGlobals] = stackBase(i.globals)
		scratch[jit.ScratchBP] = uint64(i.fr.bp)
		scratch[jit.ScratchNext] = 0
		returns, err := callable.Call(in, scratch)
		if err != nil {
			panic(err)
		}
		i.sp = base
		if i.sp+len(returns) > len(i.stack) {
			panic(ErrStackOverflow)
		}
		for _, ret := range returns {
			i.stack[i.sp] = jit.Ret(ret)
			i.sp++
		}
		i.fr.ip = int(scratch[jit.ScratchNext])
	}
}

// entry wraps a whole-function Entry Callable. Unlike segment, the CALL
// handler has already pushed a frame and set i.fr before this closure runs.
// The native Entry reads params from stack scratch slots, then this closure
// performs the frame teardown that RETURN would normally do in the threaded
// interpreter.
func (i *Interpreter) entry(callable asm.Callable) func(*Interpreter) {
	scratch := make([]uint64, jit.ScratchCount)
	return func(i *Interpreter) {
		scratch[jit.ScratchStack] = stackBase(i.stack)
		scratch[jit.ScratchGlobals] = stackBase(i.globals)
		scratch[jit.ScratchBP] = uint64(i.fr.bp)
		scratch[jit.ScratchNext] = 0

		returns, err := callable.Call(nil, scratch)
		if err != nil {
			panic(err)
		}

		// Perform the frame teardown that the threaded RETURN handler does.
		f := i.fr
		for k, ret := range returns {
			i.stack[f.bp+k] = jit.Ret(ret)
		}
		i.sp = f.bp + f.returns
		if f.release {
			i.release(f.ref)
		}
		f.code = nil
		i.fp--
		i.fr = &i.frames[i.fp-1]
	}
}

func (i *Interpreter) error(r any) error {
	ip := i.fr.ip
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
		addr := i.keep(v)
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
	if addr, ok := i.reuse(val); ok {
		return addr
	}

	if len(i.heap) == cap(i.heap) {
		i.gc()
		if addr, ok := i.reuse(val); ok {
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

func (i *Interpreter) reuse(val types.Value) (int, bool) {
	if len(i.free) == 0 {
		return 0, false
	}
	addr := i.free[len(i.free)-1]
	i.free = i.free[:len(i.free)-1]
	i.heap[addr] = val
	i.rc[addr] = 1
	return addr, true
}

func (i *Interpreter) keep(val types.Value) int {
	roots := i.trace(val)
	defer i.unroot(roots)
	return i.alloc(val)
}

func (i *Interpreter) retainBox(v types.Boxed) {
	if v.Kind() == types.KindRef {
		i.retain(v.Ref())
	}
}

func (i *Interpreter) releaseBox(v types.Boxed) {
	if v.Kind() == types.KindRef {
		i.release(v.Ref())
	}
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

func (i *Interpreter) trace(val types.Value) int {
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
	if j := i.fp - 1; j >= 0 {
		push(i.frames[j].ref)
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

// stackBase returns the address of the first slot in s as a uintptr packed
// into a uint64. Returns 0 for an empty slice so native code receives a
// well-defined sentinel rather than a wild pointer.
func stackBase(s []types.Boxed) uint64 {
	if len(s) == 0 {
		return 0
	}
	return uint64(uintptr(unsafe.Pointer(&s[0])))
}
