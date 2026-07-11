package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Interpreter struct {
	ctx       context.Context
	done      <-chan struct{}
	tracer    *Tracer
	hook      func(*Interpreter) error
	marshaler Marshaler

	compiler *compiler
	cache    *Cache
	profiler *prof.Profiler
	samples  *prof.Collector
	exits    map[anchor]func(*Interpreter)
	stubs    []func(*Interpreter)
	tried    map[int]bool
	journal  []uint64

	types     []types.Type
	constants []types.Boxed
	globals   []types.Boxed
	instrs    [][]byte
	code      [][]func(*Interpreter)
	coros     []bool
	handlers  [][]instr.Handler
	module    *types.Function
	dynamic   map[int]bool

	frames   []frame
	fr       *frame
	stack    []types.Boxed
	roots    []types.Boxed
	heap     []types.Value
	base     int
	interned map[string]types.Ref
	free     []int
	rc       []int
	refbuf   []types.Ref

	fp  int
	sp  int
	gen int
	gas int64

	threshold int64
	tick      int
	fuel      int64
	limit     int
}

type frame struct {
	addr    int
	returns int

	code   []func(*Interpreter)
	upvals []types.Boxed

	ref     int
	release bool

	coro int

	ip int
	bp int
}

type option struct {
	hook       func(*Interpreter) error
	marshaler  Marshaler
	converters map[reflect.Type]Converter
	cache      *Cache
	tracer     *Tracer
	profiler   *prof.Profiler
	samples    *prof.Collector
	threshold  int
	cutoff     int

	frame   int
	stack   int
	heap    int
	maxHeap int
	tick    int
	fuel    uint64
}

func WithHook(fn func(*Interpreter) error) func(*option) {
	return func(o *option) { o.hook = fn }
}

func WithMarshaler(m Marshaler) func(*option) {
	return func(o *option) { o.marshaler = m }
}

// WithConverter registers VM conversion for an external Go type t that cannot
// implement ValueMarshaler / ValueUnmarshaler. It layers onto the default
// marshaler and applies wherever t appears, including nested values. It has no
// effect when WithMarshaler supplies a non-default Marshaler.
func WithConverter(t reflect.Type, c Converter) func(*option) {
	return func(o *option) {
		if o.converters == nil {
			o.converters = make(map[reflect.Type]Converter)
		}
		o.converters[t] = c
	}
}

func WithCache(c *Cache) func(*option) {
	return func(o *option) { o.cache = c }
}

func WithTracer(t *Tracer) func(*option) {
	return func(o *option) { o.tracer = t }
}

// WithProfiler attaches a profiler that aggregates this interpreter's execution
// samples and JIT counters. It is opt-in: without one, sampling still drives JIT
// hotness but no profile is collected. Pass the same Profiler to NewPool so every
// pooled interpreter shares it.
func WithProfiler(p *prof.Profiler) func(*option) {
	return func(o *option) { o.profiler = p }
}

// WithLocal injects a pre-seeded sample collector. Tests use it to drive hot-IP
// selection and to read JIT counters back after a run.
func WithLocal(p *prof.Collector) func(*option) {
	return func(o *option) { o.samples = p }
}

func WithFrame(val int) func(*option) {
	return func(o *option) { o.frame = val }
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

// New builds an interpreter for prog. It trusts prog to be well-formed; run
// program.Verify(prog) beforehand to reject malformed or untrusted bytecode.
func New(prog *program.Program, opts ...func(*option)) *Interpreter {
	opt := option{
		frame:     128,
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
	if opt.stack <= 0 {
		opt.stack = 1
	}
	if opt.heap < 0 {
		opt.heap = 0
	}
	if opt.tick <= 0 {
		opt.tick = 1
	}

	tracer := opt.tracer
	if tracer == nil {
		tracer = NewTracer()
	}
	samples := opt.samples
	if samples == nil {
		samples = prof.NewCollector()
	}
	m := opt.marshaler
	if m == nil {
		m = DefaultMarshaler
	}
	if len(opt.converters) > 0 {
		if _, ok := m.(*codec); ok {
			m = &codec{converters: opt.converters}
		}
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
		tracer:    tracer,
		hook:      opt.hook,
		marshaler: m,
		cache:     opt.cache,
		profiler:  opt.profiler,
		samples:   samples,
		threshold: threshold,
		types:     prog.Types,
		constants: make([]types.Boxed, len(prog.Constants)),
		globals:   make([]types.Boxed, len(prog.Globals)),
		instrs:    make([][]byte, len(prog.Constants)+1),
		code:      make([][]func(*Interpreter), len(prog.Constants)+1),
		coros:     make([]bool, len(prog.Constants)+1),
		handlers:  make([][]instr.Handler, len(prog.Constants)+1),
		exits:     map[anchor]func(*Interpreter){},
		stubs:     make([]func(*Interpreter), len(prog.Constants)+1),
		tried:     map[int]bool{},
		dynamic:   map[int]bool{},
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
		case types.I1:
			val = types.BoxI1(bool(v))
		case types.I8:
			val = types.BoxI8(int8(v))
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

	i.base = len(i.heap)

	i.module = &types.Function{Typ: &types.FunctionType{}, Locals: prog.Locals, Code: prog.Code, Handlers: prog.Handlers}
	i.instrs[0] = prog.Code
	i.handlers[0] = prog.Handlers
	i.coros[0] = containsYield(prog.Code)
	for j, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			addr := i.constants[j].Ref()
			i.instrs[addr] = fn.Code
			i.handlers[addr] = fn.Handlers
			i.coros[addr] = containsYield(fn.Code)
		}
	}

	// Declared globals are pre-seeded to the zero Boxed of their declared kind
	// (Program.Globals, like Locals): numeric slots get a typed zero, ref slots
	// a retained null ref. A slot's runtime kind therefore always matches its
	// declaration, so kind metadata for GLOBAL_GET/SET fusion and JIT lowering
	// can be derived from the slot values at any time. Callers overwrite each
	// run's input via Interpreter.SetGlobal before Run. This must precede bind:
	// bind reads i.globals kinds to thread each constant's GLOBAL_* accesses, so
	// an unseeded slot would misclassify a ref global as numeric and skip its
	// retain/release, corrupting the refcount.
	globals := types.Kinds(prog.Globals)
	for j, kind := range globals {
		i.globals[j] = i.zero(kind)
	}

	c := &threader{
		types:     i.types,
		constants: i.constants,
		heap:      i.heap,
		coros:     i.coros,
		globals:   globals,
		exact:     opt.tick == 1,
	}
	i.code[0] = c.Compile(prog.Code, i.module.LocalKinds(), types.Kinds(i.module.Captures))

	for j, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			i.bind(i.constants[j].Ref(), fn, false)
		}
	}

	i.frames[0].code = i.code[0]
	i.frames[0].bp = i.sp
	if locals := len(prog.Locals); locals > 0 {
		clear(i.stack[i.sp : i.sp+locals])
		i.sp += locals
	}
	i.fp = 1
	i.fr = &i.frames[0]
	i.retain(0)
	if opt.cache != nil && !i.cache.attach() {
		i.cache = nil
	}

	return i
}

func (i *Interpreter) Run(ctx context.Context) error {
	i.ctx = ctx
	i.done = nil
	if ctx != nil {
		i.done = ctx.Done()
	}
	for {
		// dispatch's recover absorbs every panic, so nothing escapes it and ctx is
		// always cleared below; a caught throw/trap loops to resume at the handler.
		caught, err := i.dispatch()
		if caught {
			continue
		}
		i.ctx = nil
		i.done = nil
		return err
	}
}

// dispatch runs the threaded loop until the frame ends, a safepoint stops it, or
// a panic unwinds it. Its recover delivers a yield, lands a catchable throw/trap
// on a guest handler (reported via caught so Run re-enters here), or wraps an
// uncatchable failure as a RuntimeError. The loop body is the interpreter's hot
// path and is intentionally kept identical regardless of exception support.
func (i *Interpreter) dispatch() (caught bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			if r == errYield {
				err = ErrYield
				return
			}
			if i.handle(r) {
				caught = true
				return
			}
			err = i.runtimeError(r)
		}
	}()

	f := i.fr
	code := f.code
	tick := i.tick
	quiet := i.done == nil && i.gas < 0 && i.hook == nil && i.profiler == nil && i.cache == nil

	for f.ip < len(code) {
		tick--
		if tick == 0 {
			tick = i.tick
			if !quiet || i.stub(f.addr) == nil {
				if err := i.safepoint(); err != nil {
					return false, err
				}
			}
		}

		code[f.ip](i)

		f = i.fr
		code = f.code
	}
	return false, nil
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

// SetGlobal writes val into global slot idx, releasing the reference the slot
// previously held. Ownership of a KindRef val transfers into the slot: the
// caller must not release it afterward, and should Retain first to keep an
// independent reference.
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

// SetLocal writes val into local slot idx, releasing the reference the slot
// previously held. Ownership of a KindRef val transfers into the slot: the
// caller must not release it afterward, and should Retain first to keep an
// independent reference.
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
	if _, ok := i.heap[addr].(*types.Function); ok {
		i.remove(addr)
	}
	i.heap[addr] = val
	if fn, ok := val.(*types.Function); ok {
		i.bind(addr, fn, true)
	}
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
	addr = i.keep(val)
	if fn, ok := val.(*types.Function); ok {
		i.bind(addr, fn, true)
	}
	return addr, nil
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

// PopBoxed consumes the top-of-stack value and returns its raw NaN-boxed word
// without constructing a types.Value, so scalar results incur no allocation
// (read them with Boxed.F64/I32/...). It is the zero-alloc counterpart to Pop.
// For a KindRef result the stack's reference is transferred to the caller
// unchanged: resolve it with Load and balance it with Release, or Retain to keep
// an extra reference. Pop instead detaches the heap value and releases the
// stack's reference, so the two stay symmetric on the consumed slot.
func (i *Interpreter) PopBoxed() (types.Boxed, error) {
	if i.sp == 0 {
		return 0, ErrStackUnderflow
	}
	i.sp--
	return i.stack[i.sp], nil
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
	i.flush()
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
	for addr := range i.dynamic {
		i.remove(addr)
	}
	for i.fp > 1 {
		i.fp--
		i.frames[i.fp] = frame{}
	}
	i.sp = 0
	f := &i.frames[i.fp-1]
	f.addr = 0
	f.ref = 0
	f.release = false
	f.bp = i.sp
	f.ip = 0
	f.returns = 0
	f.code = i.code[0]
	f.upvals = nil
	i.fr = f
	if locals := len(i.module.Locals); locals > 0 {
		clear(i.stack[i.sp : i.sp+locals])
		i.sp += locals
	}

	i.gas = i.fuel
	i.roots = i.roots[:0]
	clear(i.interned)

	i.heap = i.heap[:i.base]
	i.rc = i.rc[:i.base]
	for j := 0; j < i.base; j++ {
		i.rc[j] = 1
	}
	i.free = i.free[:0]

	// Restore the declared-kind zero seed (see New). Each slot's runtime kind
	// still matches its declaration, so the kind is read back from the slot
	// itself; the heap and refcounts were re-baselined above, so any old ref
	// value needs no release, and this runs after the rc reset so the seed's
	// null-ref retain is preserved.
	for idx := range i.globals {
		i.globals[idx] = i.zero(i.globals[idx].Kind())
	}
}

// compile lowers traces already recorded for addr and installs the resulting
// native entries. Recording belongs to observe and side-exit handling because
// only those paths hold the exact runtime state for their anchor.
func (i *Interpreter) compile(addr int) error {
	if i.cache != nil && !i.dynamic[addr] {
		return i.shared(addr)
	}
	if i.compiler == nil {
		compiler, err := newCompiler()
		if err != nil {
			i.samples.AddMetric("vm_jit_errors_total", 1)
			return err
		}
		i.compiler = compiler
	}
	if i.compiler == nil {
		return nil
	}
	mod, err := i.build(i.compiler, addr)
	if err != nil {
		return err
	}
	if mod == nil {
		return nil
	}
	i.install(mod, true)
	return nil
}

func (i *Interpreter) shared(addr int) error {
	compiler, err := newCompiler()
	if err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
		i.cache.ready(addr)
		return err
	}
	if compiler == nil {
		i.cache.ready(addr)
		return nil
	}
	mod, err := i.build(compiler, addr)
	if err != nil {
		_ = compiler.Close()
		i.cache.ready(addr)
		return err
	}
	if mod == nil {
		_ = compiler.Close()
		i.cache.fail(addr)
		return nil
	}
	i.samples.AddMetric("vm_jit_emits_total", float64(mod.emits))
	i.samples.AddMetric("vm_jit_bytes_total", float64(mod.bytes))
	var buf *asm.Buffer
	if mod.emits > 0 {
		buf = compiler.buffer
	} else {
		_ = compiler.Close()
	}
	i.cache.publish(addr, mod, buf)
	return nil
}

func (i *Interpreter) build(c *compiler, addr int) (*module, error) {
	fn, ok := i.function(addr)
	if !ok {
		return nil, nil
	}
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	mod, err := c.Compile(i, addr, fn)
	if err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
		return nil, err
	}
	return mod, nil
}

// install accounts a successful Compile and rewires the dispatch table: a
// trace entry replaces the function's first opcode handler and keeps the
// shadowed threaded handler for guard fallback.
func (i *Interpreter) install(mod *module, account bool) {
	if account {
		i.samples.AddMetric("vm_jit_emits_total", float64(mod.emits))
		i.samples.AddMetric("vm_jit_bytes_total", float64(mod.bytes))
	}
	for a, entry := range mod.entries {
		if a.addr < 0 || a.addr >= len(i.code) || a.ip < 0 || a.ip >= len(i.code[a.addr]) || entry.callable == nil {
			continue
		}
		// Save the original threaded handler once so deopt always resumes in the
		// interpreter, but reinstall the latest native callable on every publish:
		// a recompiled trace tree (with a hot side exit now inlined) must replace
		// the earlier one, not be dropped because one was already installed. An
		// entry root (ip 0) compiles the whole function and tears down the frame
		// on return; a loop root re-enters mid-function and never unwinds it.
		if i.exits[a] == nil {
			i.exits[a] = i.code[a.addr][a.ip]
			if a.ip == 0 {
				i.stubs[a.addr] = i.exits[a]
			}
		}
		if entry.loop {
			i.code[a.addr][a.ip] = i.loop(entry.callable)
		} else if a.addr == 0 {
			i.code[a.addr][a.ip] = i.start(a, entry.callable)
		} else {
			i.code[a.addr][a.ip] = i.call(a, entry.callable)
		}
	}
}

func (i *Interpreter) sync() {
	if i.cache == nil {
		return
	}
	modules := i.cache.modules.Load()
	if modules == nil {
		return
	}
	for i.gen < len(*modules) {
		i.install((*modules)[i.gen], false)
		i.gen++
	}
}

func (i *Interpreter) flush() {
	if i.profiler != nil {
		i.profiler.Flush(i.samples)
	}
}

// function returns the *types.Function at addr in the heap, or false if
// addr does not point at a function.
func (i *Interpreter) function(addr int) (*types.Function, bool) {
	if addr == 0 {
		return i.module, true
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
	if i.done != nil {
		select {
		case <-i.done:
			return i.ctx.Err()
		default:
		}
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

	// A warm function already has its entry trace installed. Its sampling and
	// entry/loop capture are dead (those gates require the entry fallback nil)
	// and its one-shot compile trigger has already fired, so the warm path pays
	// only one indexed read here. A user profiler still needs per-tick samples.
	if i.stub(f.addr) == nil {
		if err := i.observe(f); err != nil {
			return err
		}
	} else if i.profiler != nil {
		i.sample(f)
	}

	// Pooled recompilation is driven by exit thresholds, not sampling, so
	// cache.due/sync run every tick regardless of warmth: they adopt modules a
	// peer published and rearm after a hot side exit. Both are ~1 atomic when idle.
	if i.cache != nil {
		if i.cache.due(f.addr, i.threshold) {
			if err := i.compile(f.addr); err != nil {
				return err
			}
		}
		i.sync()
	}
	return nil
}

// observe samples the current tick for a not-yet-warm function: it feeds the
// profile collector, captures the entry trace on the first hot entry tick, and
// captures a hot loop header sampled mid-function. A solo interpreter also fires
// its one-shot entry compile here once the sample count crosses the threshold.
// The caller guarantees the entry fallback is still nil, so the gates that the
// threaded safepoint used to repeat on that lookup are already satisfied.
func (i *Interpreter) observe(f *frame) error {
	i.sample(f)
	if f.ip == 0 {
		if _, err := i.tracer.capture(i, anchor{addr: f.addr, ip: 0}); err != nil {
			return err
		}
	}
	// A hot loop header sampled mid-function: f.ip is genuinely at the back-edge
	// target, so the clone's operand stack matches the header and a one-iteration
	// loop trace can be recorded from the live state. A loopy function's entry
	// trace aborts on the unrolled body, so the one-shot entry threshold never
	// installs anything; compile here, once the function is hot and the loop
	// trace first appears, so the loop header gets its own native. The hotness
	// gate keeps WithThreshold(-1) a pure interpreter.
	samples := i.samples.Samples(f.addr)
	if i.threshold >= 0 && f.ip > 0 &&
		samples >= uint64(i.threshold) &&
		i.exits[anchor{addr: f.addr, ip: f.ip}] == nil {
		for _, h := range i.tracer.headers(i, f.addr) {
			if h != f.ip {
				continue
			}
			t, err := i.tracer.capture(i, anchor{addr: f.addr, ip: f.ip})
			if err != nil {
				return err
			}
			if t != nil && t.kind != aborted {
				if err := i.compile(f.addr); err != nil {
					return err
				}
			}
			break
		}
	}
	if i.cache == nil && i.threshold >= 0 && samples >= uint64(i.threshold) && !i.tried[f.addr] {
		i.tried[f.addr] = true
		if err := i.compile(f.addr); err != nil {
			return err
		}
	}
	return nil
}

// sample records one profile hit for the frame's current instruction.
func (i *Interpreter) sample(f *frame) {
	i.samples.Add(f.addr, f.ip, i.instrs[f.addr][f.ip])
}

// call wraps a native trace Entry Callable. The CALL handler has already
// pushed a frame and set i.fr before this closure runs. The native Entry reads
// params from stack scratch slots; on a normal return
// this closure performs the frame teardown that RETURN would do in the threaded
// interpreter, and on a trap it rebuilds the native call chain into real VM
// frames before resuming threaded execution at the fallback IP.
func (i *Interpreter) call(root anchor, callable asm.Callable) func(*Interpreter) {
	return func(i *Interpreter) {
		ctx := i.context()
		i.fr.code = nil
		i.fr.upvals = nil
		// Refresh the back-edge budget like loop does: an entry trace can carry a
		// self tail-call back-edge (see tailLoop) that polls the safepoint every
		// loopBudget iterations, re-entering native here after each yield.
		i.journal[journalBudget] = loopBudget
		if err := callable.Call(ctx); err != nil {
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
		i.sp = int(i.journal[journalSP])
		i.deopt()
		switch i.journal[journalTrap] {
		case trapOverflow:
			panic(ErrFrameOverflow)
		case trapYield:
			// A loop back-edge spent its budget. deopt left i.fr at the loop header;
			// run coordination, then let the threaded Run loop continue from there.
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		default:
			i.bailout(root)
		}
	}
}

// start wraps a native trace for top-level code. Unlike function entries,
// top-level completion does not tear down its frame; it preserves the operand
// stack and marks the module frame as exhausted so dispatch returns normally.
func (i *Interpreter) start(root anchor, callable asm.Callable) func(*Interpreter) {
	return func(i *Interpreter) {
		ctx := i.context()
		i.fr.code = nil
		i.fr.upvals = nil
		i.journal[journalBudget] = loopBudget
		if err := callable.Call(ctx); err != nil {
			panic(err)
		}

		i.sp = int(i.journal[journalSP])
		if i.journal[journalTrap] == trapNone {
			i.fr.ip = len(i.code[i.fr.addr])
			i.fr.code = i.code[i.fr.addr]
			return
		}

		i.deopt()
		switch i.journal[journalTrap] {
		case trapOverflow:
			panic(ErrFrameOverflow)
		case trapYield:
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		default:
			i.bailout(root)
		}
	}
}

// loop wraps a native loop Callable installed at a loop header. Unlike entry,
// the header is reached mid-function with the frame already live, so loop never
// pushes or tears down a frame and never returns normally — it always exits
// through a trap. A spent budget yields to the safepoint and the Run loop
// re-enters native at the header; a guarded side exit or the loop-exit edge
// leaves deopt with i.fr at the resume IP for threaded dispatch to continue.
func (i *Interpreter) loop(callable asm.Callable) func(*Interpreter) {
	return func(i *Interpreter) {
		ctx := i.context()
		// Decouple the loop's safepoint cadence from tick: a native iteration does
		// the work of a whole loop body, so yielding every tick (1 under exact
		// dispatch) would drown the loop in deopt/re-enter churn. Run many
		// iterations natively between safepoints instead.
		i.journal[journalBudget] = loopBudget
		if err := callable.Call(ctx); err != nil {
			panic(err)
		}
		i.sp = int(i.journal[journalSP])
		i.deopt()
		switch i.journal[journalTrap] {
		case trapOverflow:
			panic(ErrFrameOverflow)
		case trapYield:
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		}
	}
}

func (i *Interpreter) exit(root anchor) {
	hits, err := i.tracer.exit(i, root, branch{fn: i.fr.addr, ip: i.fr.ip})
	if err != nil {
		panic(err)
	}
	if i.cache != nil {
		if hits < exitThreshold || hits%exitThreshold != 0 {
			return
		}
		i.cache.rearm(root.addr)
		return
	}
	if hits != exitThreshold {
		return
	}
	if i.compiler == nil {
		return
	}
	mod, err := i.build(i.compiler, root.addr)
	if err != nil {
		panic(err)
	}
	if mod != nil {
		i.install(mod, true)
	}
}

func (i *Interpreter) bailout(root anchor) {
	i.exit(root)
	if i.fr.ip == 0 {
		if fn := i.stub(i.fr.addr); fn != nil {
			fn(i)
		}
	}
}

func (i *Interpreter) stub(addr int) func(*Interpreter) {
	if addr < 0 || addr >= len(i.stubs) {
		return nil
	}
	return i.stubs[addr]
}

// deopt rebuilds VM frames from the native journal after a trap. Native frames
// record themselves while unwinding, so record[depth-1] is the outermost native
// frame — already live at i.frames[i.fp-1]. Earlier records become deeper VM
// frames in reverse order, matching generated fused direct calls (ref
// unretained, code/upvals restored).
func (i *Interpreter) deopt() {
	depth := int(i.journal[journalDepth])
	if depth == 0 {
		return
	}
	base := i.fp - 1

	// The last record is the live outermost frame; reconcile its resume state.
	fn, bp, ip, _ := i.unpack(depth - 1)
	outer := &i.frames[base]
	outer.bp = bp
	outer.ip = ip
	i.restore(outer, fn)

	// Earlier records become fresh frames from outer to inner. Like the fused
	// generated direct call, the callee ref was never pushed or retained, so
	// release stays false.
	for n := 1; n < depth; n++ {
		fn, bp, ip, returns := i.unpack(depth - 1 - n)
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

// unpack reads frame record n from the native journal.
func (i *Interpreter) unpack(n int) (addr, bp, ip, returns int) {
	row := i.journal[journalHead+n*journalStride:]
	return int(row[recordAddr]), int(row[recordBP]), int(row[recordIP]), int(row[recordReturns])
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

// context resets and fills the journal handed to native code: stack/global base
// pointers, current frame BP/SP, pointer cells for native fast paths, and the
// per-call frame budget. It returns &journal[0], passed to native code in X0.
func (i *Interpreter) context() unsafe.Pointer {
	i.journal[journalStack] = 0
	if len(i.stack) > 0 {
		i.journal[journalStack] = uint64(uintptr(unsafe.Pointer(&i.stack[0])))
	}
	i.journal[journalGlobals] = 0
	if len(i.globals) > 0 {
		i.journal[journalGlobals] = uint64(uintptr(unsafe.Pointer(&i.globals[0])))
	}
	i.journal[journalBP] = uint64(i.fr.bp)
	i.journal[journalSP] = uint64(i.sp)

	i.journal[journalRC] = 0
	if len(i.rc) > 0 {
		i.journal[journalRC] = uint64(uintptr(unsafe.Pointer(&i.rc[0])))
	}
	i.journal[journalUpvals] = 0
	if len(i.fr.upvals) > 0 {
		i.journal[journalUpvals] = uint64(uintptr(unsafe.Pointer(&i.fr.upvals[0])))
	}
	i.journal[journalHeap] = 0
	if len(i.heap) > 0 {
		i.journal[journalHeap] = uint64(uintptr(unsafe.Pointer(&i.heap[0])))
	}

	i.journal[journalDepth] = 0
	i.journal[journalCap] = uint64(min(len(i.frames)-i.fp, nativeFrameLimit))
	i.journal[journalTrap] = trapNone
	i.journal[journalBudget] = uint64(i.tick)
	i.journal[journalActive] = 0
	return unsafe.Pointer(&i.journal[0])
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
	case escape:
		return e.err
	case error:
		return e
	default:
		return fmt.Errorf("%v", r)
	}
}

// handle attempts to deliver a recovered panic to a guest exception handler. An
// escape is a throw that already failed its handler search, so it stays
// terminal; any other Go error (a runtime trap or a host-function failure) is
// converted to an Error value and delivered if a covering handler exists.
func (i *Interpreter) handle(r any) bool {
	if _, ok := r.(escape); ok {
		return false
	}
	err, ok := r.(error)
	if !ok {
		return false
	}
	fp, h, ok := i.handler()
	if !ok {
		return false
	}
	i.land(fp, h, i.wrap(err))
	return true
}

// handler walks frames from innermost outward for the first protected region
// covering the active instruction: the throwing site in the top frame, the call
// site (ip-1, CALL/RETURN_CALL are one byte) in each suspended caller.
func (i *Interpreter) handler() (int, instr.Handler, bool) {
	for fp := i.fp; fp >= 1; fp-- {
		f := &i.frames[fp-1]
		ip := f.ip
		if fp != i.fp {
			ip--
		}
		if f.addr < 0 || f.addr >= len(i.handlers) {
			continue
		}
		for _, h := range i.handlers[f.addr] {
			if h.Start <= ip && ip < h.End {
				return fp, h, true
			}
		}
	}
	return 0, instr.Handler{}, false
}

// land unwinds to the handler frame, discarding the frames and operand values
// above the protected region's entry depth, then delivers exc as the sole
// operand and resumes at the catch IP. exc keeps the single reference it already
// owned (popped off the stack by THROW, or freshly allocated for a trap).
func (i *Interpreter) land(fp int, h instr.Handler, exc types.Boxed) {
	for i.fp > fp {
		i.discard(&i.frames[i.fp-1])
		i.fp--
	}
	f := &i.frames[fp-1]
	base := f.bp + h.Depth
	for s := i.sp - 1; s >= base; s-- {
		i.releaseBox(i.stack[s])
	}
	i.stack[base] = exc
	i.sp = base + 1
	f.ip = h.Catch
	i.fr = f
}

// discard releases an unwound frame's activation: its function reference and any
// in-flight coroutine handle. Operand slots are released by land in one sweep.
func (i *Interpreter) discard(f *frame) {
	if f.release {
		i.release(f.ref)
	}
	if f.coro != 0 {
		i.release(f.coro)
	}
	f.code = nil
	f.upvals = nil
	f.coro = 0
}

// wrap allocates a heap Error wrapping a Go failure so a recovered trap or
// host error becomes a catchable guest value while staying errors.Is/As aware.
func (i *Interpreter) wrap(err error) types.Boxed {
	return types.BoxRef(i.keep(types.WrapError(ErrorCode(err), err)))
}

// uncaught renders an escaped throw as a Go error. A thrown Error surfaces
// directly (preserving its Unwrap chain); any other value is wrapped with its
// rendered form under ErrUncaughtException.
func (i *Interpreter) uncaught(exc types.Boxed) error {
	if exc.Kind() == types.KindRef {
		v := i.heap[exc.Ref()]
		if e, ok := v.(*types.Error); ok {
			return e
		}
		return fmt.Errorf("%w: %s", ErrUncaughtException, v.String())
	}
	return fmt.Errorf("%w: %s", ErrUncaughtException, types.Unbox(exc).String())
}

// message derives an Error message from a payload: a string's contents, else the
// value's rendered form.
func (i *Interpreter) message(v types.Boxed) string {
	if v.Kind() == types.KindRef {
		if s, ok := i.heap[v.Ref()].(types.String); ok {
			return string(s)
		}
		return i.heap[v.Ref()].String()
	}
	return types.Unbox(v).String()
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

// zero returns the zero Boxed for a slot of the declared kind: a typed
// numeric zero, or for ref kinds a retained null ref (heap index 0 is
// permanently Null), so the slot's runtime kind always matches its
// declaration and releasing the seeded value stays balanced.
func (i *Interpreter) zero(kind types.Kind) types.Boxed {
	switch kind.Repr() {
	case types.KindI32:
		return types.BoxI32(0)
	case types.KindI64:
		return types.BoxI64(0)
	case types.KindF32:
		return types.BoxF32(0)
	case types.KindF64:
		return types.BoxF64(0)
	default:
		i.retain(0)
		return types.BoxedNull
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
	v, ok := i.heap[addr].(types.I64)
	if !ok {
		panic(ErrTypeMismatch)
	}
	i.release(addr)
	return int64(v)
}

// borrowI64 reads an I64 without consuming a reference: unlike unboxI64 it
// never releases, so slot-resident values (locals, globals, upvals) keep
// their ownership while the caller only borrows the scalar.
func (i *Interpreter) borrowI64(val types.Boxed) int64 {
	if val.Kind() != types.KindRef {
		return val.I64()
	}
	v, ok := i.heap[val.Ref()].(types.I64)
	if !ok {
		panic(ErrTypeMismatch)
	}
	return int64(v)
}

func (i *Interpreter) box(val types.Value) types.Boxed {
	switch v := val.(type) {
	case types.Boxed:
		return v
	case types.I1:
		return types.BoxI1(bool(v))
	case types.I8:
		return types.BoxI8(int8(v))
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

func (i *Interpreter) bind(addr int, fn *types.Function, dynamic bool) {
	n := addr + 1
	if addr >= len(i.instrs) {
		i.instrs = append(i.instrs, make([][]byte, n-len(i.instrs))...)
	}
	if addr >= len(i.code) {
		i.code = append(i.code, make([][]func(*Interpreter), n-len(i.code))...)
	}
	if addr >= len(i.stubs) {
		i.stubs = append(i.stubs, make([]func(*Interpreter), n-len(i.stubs))...)
	}
	if addr >= len(i.handlers) {
		i.handlers = append(i.handlers, make([][]instr.Handler, n-len(i.handlers))...)
	}
	if addr >= len(i.coros) {
		i.coros = append(i.coros, make([]bool, n-len(i.coros))...)
	}
	// The declared Program.Globals are out of scope here; New pre-seeds every
	// slot to the zero Boxed of its declared kind, so the runtime values carry
	// the declared kinds at all times.
	globals := make([]types.Kind, len(i.globals))
	for j, g := range i.globals {
		globals[j] = g.Kind()
	}
	c := &threader{
		types:     i.types,
		constants: i.constants,
		heap:      i.heap,
		coros:     i.coros,
		globals:   globals,
		exact:     i.tick == 1,
	}
	if dynamic {
		i.coros[addr] = containsYield(fn.Code)
	}
	i.instrs[addr] = fn.Code
	i.handlers[addr] = fn.Handlers
	i.code[addr] = c.Compile(fn.Code, fn.LocalKinds(), types.Kinds(fn.Captures))
	if dynamic {
		i.dynamic[addr] = true
	}
}

func (i *Interpreter) remove(addr int) {
	if addr < 0 || addr >= len(i.instrs) {
		delete(i.dynamic, addr)
		return
	}
	i.instrs[addr] = nil
	i.code[addr] = nil
	i.stubs[addr] = nil
	i.handlers[addr] = nil
	i.coros[addr] = false
	for a := range i.exits {
		if a.addr == addr {
			delete(i.exits, a)
		}
	}
	delete(i.tried, addr)
	if i.tracer != nil {
		i.tracer.remove(addr)
	}
	delete(i.dynamic, addr)
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
			for _, r := range i.refs(v) {
				stack = append(stack, int(r))
			}
			i.reclaim(addr, v)
		}
	}
}

func (i *Interpreter) trace(val types.Value) int {
	n := 0
	for _, ref := range i.refs(val) {
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
	i.mark()
	i.sweep()
}

// mark negates every live refcount, then walks the object graph from the roots,
// flipping each reachable slot back to its original positive count. When it
// returns, survivors hold positive counts, unreachable-but-allocated slots are
// negative, and free slots remain zero.
func (i *Interpreter) mark() {
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
	pushBox := func(val types.Boxed) {
		if val.Kind() == types.KindRef {
			push(val.Ref())
		}
	}

	for j := 0; j < i.sp; j++ {
		pushBox(i.stack[j])
	}
	for _, val := range i.constants {
		pushBox(val)
	}
	for _, val := range i.globals {
		pushBox(val)
	}
	for _, val := range i.roots {
		pushBox(val)
	}
	for j := 0; j < i.fp; j++ {
		push(i.frames[j].ref)
		if co := i.frames[j].coro; co > 0 {
			push(co)
		}
	}

	for len(stack) > 0 {
		addr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, ref := range i.refs(i.heap[addr]) {
			push(int(ref))
		}
	}
}

// sweep frees every slot mark left negative. For each dead object it first
// removes that object's contribution to its live referents' counts, so the
// surviving graph's refcounts stay exact and survivors are not pinned by edges
// from collected garbage. Dead->dead edges are skipped: the referent is zeroed
// by its own sweep.
func (i *Interpreter) sweep() {
	for j := 1; j < len(i.heap); j++ {
		if i.rc[j] >= 0 {
			continue
		}
		v := i.heap[j]
		for _, ref := range i.refs(v) {
			if r := int(ref); i.rc[r] > 0 {
				i.rc[r]--
			}
		}
		i.rc[j] = 0
		i.reclaim(j, v)
	}
}

// refs returns v's nested refs using the interpreter's reused scratch buffer,
// or nil if v is not Traceable. The result is only valid until the next call.
func (i *Interpreter) refs(v types.Value) []types.Ref {
	t, ok := v.(types.Traceable)
	if !ok {
		return nil
	}
	i.refbuf = t.Refs(i.refbuf[:0])
	return i.refbuf
}

// reclaim finalizes slot addr holding v: it drops interned-string and
// OS-resource bookkeeping, clears the slot, and returns it to the free list. The
// caller has already decided addr is dead and settled its referents' counts.
func (i *Interpreter) reclaim(addr int, v types.Value) {
	if _, ok := v.(*types.Function); ok {
		i.remove(addr)
	}
	if s, ok := v.(types.String); ok {
		delete(i.interned, string(s))
	}
	if c, ok := v.(io.Closer); ok {
		_ = c.Close()
	}
	i.heap[addr] = nil
	i.free = append(i.free, addr)
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
