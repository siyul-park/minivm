package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Interpreter struct {
	ctx       context.Context
	prof      *prof.Stats
	hook      func(*Interpreter) error
	marshaler Marshaler

	compiler  *jitCompiler
	cache     *Cache
	fallbacks map[int]func(*Interpreter)
	argv      [scratchCount]uint64
	journal   []uint64

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
	free     []int
	rc       []int

	fp  int
	sp  int
	gen int
	gas int64

	threshold int64
	cutoff    int
	tick      int
	fuel      int64
	limit     int
}

// RuntimeError wraps a guest execution failure with the VM call stack at the
// point of failure. Frames are innermost first.
type RuntimeError struct {
	Err    error
	Frames []FrameInfo
}

type FrameInfo struct {
	Func int
	IP   int
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
	cache     *Cache
	threshold int
	cutoff    int

	frame   int
	globals int
	stack   int
	heap    int
	maxHeap int
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
	ErrHeapExhausted       = errors.New("heap exhausted")
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

func WithCache(c *Cache) func(*option) {
	return func(o *option) { o.cache = c }
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

func WithMaxHeap(val int) func(*option) {
	return func(o *option) { o.maxHeap = val }
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
		cache:     opt.cache,
		threshold: threshold,
		cutoff:    opt.cutoff,
		types:     prog.Types,
		constants: make([]types.Boxed, len(prog.Constants)),
		globals:   make([]types.Boxed, 0, opt.globals),
		instrs:    make([][]byte, len(prog.Constants)+1),
		code:      make([][]func(*Interpreter), len(prog.Constants)+1),
		fallbacks: map[int]func(*Interpreter){},
		journal:   make([]uint64, journalHead+journalStride*opt.frame),
		frames:    make([]frame, opt.frame),
		stack:     make([]types.Boxed, opt.stack),
		heap:      make([]types.Value, 0, opt.heap),
		interned:  make(map[string]types.Ref),
		free:      make([]int, 0, opt.heap),
		rc:        make([]int, 0, opt.heap),
		tick:      opt.tick,
		fuel:      fuel,
		gas:       fuel,
		limit:     opt.maxHeap,
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
			i.instrs[j+1] = fn.Code
			i.code[j+1] = c.Compile(fn.Code, fn.LocalKinds())
		}
	}

	i.frames[0].code = i.code[0]
	i.frames[0].bp = i.sp
	i.fp = 1
	i.fr = &i.frames[0]
	i.retain(0)
	if i.cache != nil && !i.cache.attach() {
		i.cache = nil
	}

	return i
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := "<nil>"
	if e.Err != nil {
		msg = e.Err.Error()
	}
	if len(e.Frames) == 0 {
		return msg
	}

	var b strings.Builder
	b.WriteString(msg)
	for idx, f := range e.Frames {
		if idx == 0 {
			fmt.Fprintf(&b, ": fn=%d ip=%d", f.Func, f.IP)
			continue
		}
		fmt.Fprintf(&b, " <- fn=%d ip=%d", f.Func, f.IP)
	}
	return b.String()
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (i *Interpreter) Run(ctx context.Context) (err error) {
	i.ctx = ctx
	defer func() {
		i.ctx = nil
		if r := recover(); r != nil {
			err = i.runtimeError(r)
		}
	}()

	f := i.fr
	code := f.code
	tick := i.tick

	for f.ip < len(code) {
		tick--
		if tick == 0 {
			tick = i.tick
			if err := i.safepoint(); err != nil {
				return err
			}
		}

		code[f.ip](i)

		f = i.fr
		code = f.code
	}
	return nil
}

func (i *Interpreter) Marshal(v any) (val types.Value, err error) {
	defer i.guard(&err)
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

func (i *Interpreter) Store(addr int, val types.Value) (err error) {
	defer i.guard(&err)
	if addr < 0 || addr >= len(i.heap) || i.rc[addr] <= 0 {
		return ErrSegmentationFault
	}
	if v, ok := val.(types.Boxed); ok {
		if v.Kind() == types.KindRef {
			ref := v.Ref()
			if ref < 0 || ref >= len(i.heap) || i.rc[ref] <= 0 {
				return ErrSegmentationFault
			}
			val = i.heap[ref]
		} else {
			val = types.Unbox(v)
		}
	}
	i.heap[addr] = val
	return nil
}

func (i *Interpreter) Alloc(val types.Value) (addr int, err error) {
	defer i.guard(&err)
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

func (i *Interpreter) Push(val types.Value) (err error) {
	defer i.guard(&err)
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
	var err error
	if i.compiler != nil {
		err = errors.Join(err, i.compiler.Close())
		i.compiler = nil
	}
	if i.cache != nil {
		err = errors.Join(err, i.cache.detach())
		i.cache = nil
	}
	return err
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
	i.gas = i.fuel
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

// jit lazily builds the native compiler, compiles the function at addr, and
// installs the result into the dispatch table. Triggered once per function from
// the Run-loop safepoint when its sample count crosses the threshold.
func (i *Interpreter) jit(addr int) error {
	if i.cache != nil {
		return i.shared(addr)
	}
	if err := i.ensure(); err != nil {
		return err
	}
	if i.compiler == nil {
		return nil
	}
	i.prof.JITAdd(prof.JIT{Attempts: 1})

	fn, ok := i.function(addr)
	if !ok {
		return nil
	}
	mod, err := i.compiler.Compile(i, addr, fn)
	if err != nil {
		i.prof.JITAdd(prof.JIT{Errors: 1})
		return err
	}
	if mod == nil {
		return nil
	}
	i.install(mod, true)
	return nil
}

func (i *Interpreter) shared(addr int) error {
	fn, ok := i.function(addr)
	if !ok {
		i.cache.fail(addr)
		return nil
	}

	i.cache.mu.Lock()
	defer i.cache.mu.Unlock()

	compiler, err := newJITCompiler(i.cutoff)
	if err != nil {
		i.cache.stats.JITAdd(prof.JIT{Errors: 1})
		i.cache.done(addr)
		return err
	}
	if compiler == nil {
		i.cache.done(addr)
		return nil
	}
	i.cache.stats.JITAdd(prof.JIT{Attempts: 1})

	mod, err := compiler.Compile(i, addr, fn)
	if err != nil {
		_ = compiler.Close()
		i.cache.stats.JITAdd(prof.JIT{Errors: 1})
		i.cache.done(addr)
		return err
	}
	if mod == nil {
		mod = &jitModule{}
	}
	var buf *asm.Buffer
	if mod.emits > 0 {
		buf = compiler.buffer
	} else {
		_ = compiler.Close()
	}
	i.cache.publish(addr, mod, buf)
	return nil
}

// ensure lazily constructs the native compiler. Any failure here is
// recorded as a JIT error and propagated.
func (i *Interpreter) ensure() error {
	if i.compiler == nil {
		compiler, err := newJITCompiler(i.cutoff)
		if err != nil {
			i.prof.JITAdd(prof.JIT{Errors: 1})
			return err
		}
		i.compiler = compiler
	}
	return nil
}

// install accounts a successful Compile and rewires the dispatch table:
// the whole-function Entry replaces the first opcode handler, every
// emitted segment replaces its corresponding handler.
func (i *Interpreter) install(mod *jitModule, account bool) {
	if account {
		i.prof.JITAdd(prof.JIT{
			Emits: uint64(mod.emits),
			Links: uint64(mod.links),
			Skips: uint64(mod.skips),
			Bytes: uint64(mod.bytes),
		})
	}
	for addr, callable := range mod.entries {
		if addr <= 0 || addr >= len(i.code) || len(i.code[addr]) == 0 || callable == nil {
			continue
		}
		if i.fallbacks[addr] != nil {
			continue
		}
		i.fallbacks[addr] = i.code[addr][0]
		i.code[addr][0] = i.entry(callable)
	}
	for _, seg := range mod.segments {
		if seg.addr < 0 || seg.addr >= len(i.code) || seg.ip < 0 || seg.ip >= len(i.code[seg.addr]) || seg.callable == nil {
			continue
		}
		fallback := i.code[seg.addr][seg.ip]
		if seg.ip == 0 && seg.addr > 0 && i.fallbacks[seg.addr] == nil {
			i.fallbacks[seg.addr] = fallback
		}
		i.code[seg.addr][seg.ip] = i.segment(seg.addr, seg.callable, seg.stack, fallback)
	}
}

func (i *Interpreter) sync() {
	if i.cache == nil {
		return
	}
	mods := i.cache.mods.Load()
	if mods == nil {
		return
	}
	for i.gen < len(*mods) {
		i.install((*mods)[i.gen], false)
		i.gen++
	}
}

func (i *Interpreter) flush() {
	if i.cache != nil {
		i.cache.flush(i.prof)
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

// safepoint runs one round of per-tick coordination shared by the threaded Run
// loop and native loop yields: context cancellation, fuel metering, the user
// hook, profile sampling, and the one-shot JIT trigger. It reads the current
// frame i.fr, so a native yield must rebuild frames (deopt) and point i.fr at
// the resumable frame before calling it.
func (i *Interpreter) safepoint() error {
	select {
	case <-i.ctx.Done():
		return i.ctx.Err()
	default:
	}

	if i.gas >= 0 {
		if i.gas == 0 {
			return ErrFuelExhausted
		}
		i.gas--
	}

	f := i.fr
	if i.hook != nil {
		i.restore(f, f.addr)
		if err := i.hook(i); err != nil {
			return err
		}
	}

	i.prof.Add(f.addr, f.ip, i.instrs[f.addr][f.ip])
	if i.cache != nil {
		if i.cache.due(f.addr, i.threshold) {
			if err := i.jit(f.addr); err != nil {
				return err
			}
		}
		i.sync()
		return nil
	}
	if i.threshold >= 0 && i.prof.Samples(f.addr) == uint64(i.threshold) {
		if err := i.jit(f.addr); err != nil {
			return err
		}
	}
	return nil
}

// segment wraps a native segment Callable in a threaded-style closure. Native
// code reads and writes the VM stack directly through scratch argv.
func (i *Interpreter) segment(addr int, callable asm.Callable, argc int, fallback func(*Interpreter)) func(*Interpreter) {
	return func(i *Interpreter) {
		if i.sp < argc {
			panic(ErrStackUnderflow)
		}
		scratch := i.scratch()
		if err := callable.Call(scratch); err != nil {
			panic(err)
		}

		i.sp = int(scratch[scratchSP])
		i.fr.ip = int(i.journal[journalNextIP])
		switch i.journal[journalTrap] {
		case trapFallback:
			i.restore(i.fr, addr)
			fallback(i)
		case trapYield:
			// A loop-region back-edge spent its budget; i.fr.ip is the header.
			// Service coordination, then let the Run loop re-dispatch this same
			// segment for native resume.
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		}
	}
}

// entry wraps a whole-function Entry Callable. Unlike segment, the CALL
// handler has already pushed a frame and set i.fr before this closure runs.
// The native Entry reads params from stack scratch slots; on a normal return
// this closure performs the frame teardown that RETURN would do in the threaded
// interpreter, and on a trap it rebuilds the native call chain into real VM
// frames before resuming threaded execution at the fallback IP.
func (i *Interpreter) entry(callable asm.Callable) func(*Interpreter) {
	return func(i *Interpreter) {
		scratch := i.scratch()
		i.fr.code = nil
		i.fr.upvals = nil
		if err := callable.Call(scratch); err != nil {
			panic(err)
		}

		if i.journal[journalTrap] == trapNone {
			// Frame teardown the threaded RETURN handler does.
			f := i.fr
			i.sp = f.bp + f.returns
			if f.release {
				i.release(f.ref)
			}
			f.code = nil
			i.fp--
			i.fr = &i.frames[i.fp-1]
			return
		}

		// A trap rebuilt the native call chain into real VM frames; resume the
		// innermost in the interpreter, surface a frame overflow, or service a
		// loop safepoint.
		i.sp = int(scratch[scratchSP])
		i.deopt()
		switch i.journal[journalTrap] {
		case trapOverflow:
			panic(ErrFrameOverflow)
		case trapYield:
			// A loop back-edge spent its budget. deopt left i.fr at the loop header;
			// run coordination, then let the Run loop re-dispatch the header — a
			// native loop-region segment is installed there for in-place resume.
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		default:
			// IP 0 now dispatches to the native entry; run the handler it shadows once
			// so we make progress instead of re-entering native and re-trapping. Any
			// later IP is an untouched threaded handler the Run loop dispatches next.
			if i.fr.ip == 0 {
				i.fallbacks[i.fr.addr](i)
			}
		}
	}
}

// deopt rebuilds VM frames from the native journal after a trap. Native frames
// record themselves while unwinding, so record[depth-1] is the outermost native
// frame — already live at i.frames[i.fp-1]. Earlier records become deeper VM
// frames in reverse order, matching the fused direct call in fuse.go (ref
// unretained, code/upvals restored).
func (i *Interpreter) deopt() {
	depth := int(i.journal[journalDepth])
	if depth == 0 {
		return
	}
	base := i.fp - 1

	// The last record is the live outermost frame; reconcile its resume state.
	fn, bp, ip, _ := i.record(depth - 1)
	outer := &i.frames[base]
	outer.bp = bp
	outer.ip = ip
	i.restore(outer, fn)

	// Earlier records become fresh frames from outer to inner. Like the fused
	// direct call in fuse.go, the callee ref was never pushed or retained, so
	// release stays false.
	for n := 1; n < depth; n++ {
		fn, bp, ip, returns := i.record(depth - 1 - n)
		f := &i.frames[base+n]
		f.addr = fn
		f.ref = fn
		f.release = false
		f.bp = bp
		f.ip = ip
		f.returns = returns
		i.restore(f, fn)
	}
	i.fp += depth - 1
	i.fr = &i.frames[i.fp-1]
}

// record unpacks frame record n from the native journal.
func (i *Interpreter) record(n int) (addr, bp, ip, returns int) {
	rec := i.journal[journalHead+n*journalStride:]
	return int(rec[recordAddr]), int(rec[recordBP]), int(rec[recordIP]), int(rec[recordReturns])
}

func (i *Interpreter) restore(f *frame, addr int) {
	if f == nil {
		return
	}
	if addr <= 0 {
		addr = f.addr
	}
	if addr < 0 || addr >= len(i.code) {
		return
	}
	f.code = i.code[addr]
	f.addr = addr
	if f.ref > 0 && f.ref < len(i.heap) {
		if cl, ok := i.heap[f.ref].(*types.Closure); ok && int(cl.Fn) == addr {
			f.upvals = cl.Upvals
			return
		}
	}
	f.upvals = nil
}

// scratch resets and fills the argv/journal handed to native code: stack and
// global base pointers, the current frame's BP/SP, the journal pointer, and the
// per-call frame budget. Returns the argv slice the Callable reads and writes.
func (i *Interpreter) scratch() []uint64 {
	clear(i.argv[:])
	i.argv[scratchBP] = uint64(i.fr.bp)
	i.argv[scratchSP] = uint64(i.sp)
	i.argv[scratchCtrl] = uint64(uintptr(unsafe.Pointer(&i.journal[0])))
	if len(i.stack) > 0 {
		i.argv[scratchStack] = uint64(uintptr(unsafe.Pointer(&i.stack[0])))
	}
	if len(i.globals) > 0 {
		i.argv[scratchGlobals] = uint64(uintptr(unsafe.Pointer(&i.globals[0])))
	}
	if len(i.rc) > 0 {
		i.journal[journalRC] = uint64(uintptr(unsafe.Pointer(&i.rc[0])))
	}
	if len(i.fr.upvals) > 0 {
		i.journal[journalUpvals] = uint64(uintptr(unsafe.Pointer(&i.fr.upvals[0])))
	}
	if len(i.heap) > 0 {
		i.journal[journalHeap] = uint64(uintptr(unsafe.Pointer(&i.heap[0])))
	}

	i.journal[journalDepth] = 0
	i.journal[journalCap] = uint64(len(i.frames) - i.fp)
	i.journal[journalTrap] = trapNone
	i.journal[journalBudget] = uint64(i.tick)
	i.journal[journalActive] = 0
	return i.argv[:]
}

func (i *Interpreter) runtimeError(r any) error {
	return &RuntimeError{
		Err:    i.cause(r),
		Frames: i.framesInfo(),
	}
}

func (i *Interpreter) guard(err *error) {
	if r := recover(); r != nil {
		*err = i.cause(r)
	}
}

func (i *Interpreter) cause(r any) error {
	switch e := r.(type) {
	case error:
		return e
	default:
		return fmt.Errorf("%v", r)
	}
}

func (i *Interpreter) framesInfo() []FrameInfo {
	if i.fp <= 0 {
		return nil
	}
	frames := make([]FrameInfo, 0, i.fp)
	for idx := i.fp - 1; idx >= 0; idx-- {
		f := i.frames[idx]
		frames = append(frames, FrameInfo{Func: f.addr, IP: f.ip})
	}
	return frames
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

	if i.limit > 0 && len(i.heap) >= i.limit {
		i.gc()
		if addr, ok := i.reuse(val); ok {
			return addr
		}
		panic(ErrHeapExhausted)
	}

	if len(i.heap) == cap(i.heap) {
		i.gc()
		if addr, ok := i.reuse(val); ok {
			return addr
		}
		if i.limit > 0 && len(i.heap) >= i.limit {
			panic(ErrHeapExhausted)
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
	// Fast path: a shared object just loses one of several references and stays
	// live. This is the common case for ref-heavy code and avoids the worklist.
	if i.rc[addr] > 1 {
		i.rc[addr]--
		return
	}

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
