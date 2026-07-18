package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync/atomic"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Interpreter struct {
	ctx         context.Context
	done        <-chan struct{}
	tracer      *Tracer
	hook        func(*Interpreter) error
	marshaler   Marshaler
	speculative bool

	compiler *compiler
	cache    *Cache
	profiler *prof.Profiler
	samples  *prof.Collector
	exits    map[anchor]func(*Interpreter)
	stubs    []func(*Interpreter)
	natives  []unsafe.Pointer
	tried    map[anchor]bool
	loopHits map[anchor]uint8
	journal  []uint64

	types       []types.Type
	constants   []types.Boxed
	globals     []types.Boxed
	globalTypes []types.Type
	instrs      [][]byte
	code        [][]func(*Interpreter)
	backedges   []bool
	coros       []bool
	handlers    [][]instr.Handler
	module      *types.Function
	dynamic     map[int]bool

	frames   []frame
	fr       *frame
	stack    []types.Boxed
	heap     []types.Value
	base     int
	target   int
	interned map[string]types.Ref
	free     []int
	rc       []int
	trial    []int
	work     []int
	refbuf   []types.Ref

	fp  int
	sp  int
	gen int
	gas int64

	threshold int64
	eager     bool
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
	threshold  int

	frame   int
	stack   int
	heap    int
	maxHeap int
	tick    int
	fuel    uint64
}

const heapRunway = 64
const loopWarmup = 8

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

// WithTracer shares tracing state with interpreters for the same program.
// A tracer already bound to another program is isolated automatically.
func WithTracer(t *Tracer) func(*option) {
	return func(o *option) { o.tracer = t }
}

// WithProfiler attaches a profiler that aggregates this interpreter's execution
// samples and JIT counters. It is opt-in: without one, sampling still drives JIT
// hotness but no profile is collected. Pass the same Profiler to NewPool so every
// pooled interpreter shares it.
func WithProfiler(p *prof.Profiler) func(*option) {
	return func(o *option) {
		o.profiler = p
	}
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

func WithHeapLimit(val int) func(*option) {
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
	if tracer == nil || !tracer.bind(prog) {
		tracer = NewTracer()
		tracer.bind(prog)
	}
	samples := prof.NewCollector()
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
		tracer:      tracer,
		hook:        opt.hook,
		marshaler:   m,
		cache:       opt.cache,
		profiler:    opt.profiler,
		samples:     samples,
		threshold:   threshold,
		eager:       opt.threshold == 0,
		types:       prog.Types,
		constants:   make([]types.Boxed, len(prog.Constants)),
		globals:     make([]types.Boxed, len(prog.Globals)),
		globalTypes: prog.Globals,
		instrs:      make([][]byte, len(prog.Constants)+1),
		code:        make([][]func(*Interpreter), len(prog.Constants)+1),
		backedges:   make([]bool, len(prog.Constants)+1),
		coros:       make([]bool, len(prog.Constants)+1),
		handlers:    make([][]instr.Handler, len(prog.Constants)+1),
		exits:       map[anchor]func(*Interpreter){},
		stubs:       make([]func(*Interpreter), len(prog.Constants)+1),
		natives:     make([]unsafe.Pointer, len(prog.Constants)+1),
		tried:       map[anchor]bool{},
		loopHits:    map[anchor]uint8{},
		dynamic:     map[int]bool{},
		journal:     make([]uint64, journalHead+journalStride*opt.frame),
		frames:      make([]frame, opt.frame),
		stack:       make([]types.Boxed, opt.stack),
		heap:        make([]types.Value, 0, opt.heap),
		interned:    make(map[string]types.Ref),
		free:        make([]int, 0, opt.heap),
		rc:          make([]int, 0, opt.heap),
		tick:        opt.tick,
		fuel:        fuel,
		gas:         fuel,
		limit:       opt.maxHeap,
	}
	i.alloc(types.Null)

	// Retain each constant root and nested edge as it becomes visible because a
	// later constant may allocate and trigger GC. recount normalizes the final
	// baseline after the whole constant pool has been boxed.
	for j, v := range prog.Constants {
		var val types.Boxed
		switch v := v.(type) {
		case types.Boxed:
			val = v
			if val.Kind() == types.KindRef {
				addr := val.Ref()
				if addr >= 0 && addr < len(i.rc) && i.rc[addr] > 0 {
					i.retain(addr)
				}
			}
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
			if addr := int(v); addr >= 0 && addr < len(i.rc) && i.rc[addr] > 0 {
				i.retain(addr)
			}
		default:
			if s, ok := v.(types.String); ok {
				val = types.BoxRef(int(i.intern(string(s))))
			} else {
				val = types.BoxRef(i.keep(v))
			}
			for _, ref := range i.refs(v) {
				if addr := int(ref); addr >= 0 && addr < len(i.rc) && i.rc[addr] > 0 {
					i.retain(addr)
				}
			}
		}
		i.constants[j] = val
	}

	i.base = len(i.heap)
	i.recount()
	i.target = max(cap(i.heap), i.base+heapRunway)
	if i.limit > 0 {
		i.target = max(min(i.target, i.limit), i.base)
	}

	i.module = &types.Function{Typ: &types.FunctionType{}, Locals: prog.Locals, Code: prog.Code, Handlers: prog.Handlers}
	i.instrs[0] = prog.Code
	i.handlers[0] = prog.Handlers
	i.coros[0] = i.yields(prog.Code)
	for j, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			addr := i.constants[j].Ref()
			i.instrs[addr] = fn.Code
			i.handlers[addr] = fn.Handlers
			i.coros[addr] = i.yields(fn.Code)
		}
	}

	// Seed globals from their declarations. Execution specializes from the
	// current values; globalTypes remains the boundary contract for SetGlobal and
	// Reset.
	for j, typ := range i.globalTypes {
		i.globals[j] = i.zero(typ.Kind())
	}

	i.backedges[0] = nativeBackend && i.eager
	c := i.threader(i.backedges[0])
	i.code[0] = c.Compile(prog.Code, i.module.Slots(), types.Kinds(i.module.Captures))

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

func (i *Interpreter) Func() int {
	return i.fr.addr
}

func (i *Interpreter) IP() int {
	return i.fr.ip
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
// previously held. Ownership of a different KindRef val transfers into the
// slot: the caller must not release it afterward, and should Retain first to
// keep an independent reference. Reassigning the current value is a no-op and
// leaves the caller's ownership unchanged.
func (i *Interpreter) SetGlobal(idx int, val types.Boxed) error {
	if idx < 0 || idx >= len(i.globals) {
		return ErrSegmentationFault
	}
	actual := val.Type()
	if val.Kind() == types.KindRef {
		if !i.alive(val.Ref()) {
			return ErrSegmentationFault
		}
		actual = i.heap[val.Ref()].Type()
	}
	if !i.globalTypes[idx].Cast(actual) {
		return ErrTypeMismatch
	}
	old := i.globals[idx]
	if old == val {
		return nil
	}
	i.globals[idx] = val
	if old.Kind() == types.KindRef {
		i.release(old.Ref())
	}
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
// previously held. Ownership of a different KindRef val transfers into the
// slot: the caller must not release it afterward, and should Retain first to
// keep an independent reference. Reassigning the current value is a no-op and
// leaves the caller's ownership unchanged.
func (i *Interpreter) SetLocal(idx int, val types.Boxed) error {
	f := i.fr
	addr := f.bp + idx
	if addr < 0 || addr >= i.sp {
		return ErrSegmentationFault
	}
	if val.Kind() == types.KindRef && !i.alive(val.Ref()) {
		return ErrSegmentationFault
	}
	old := i.stack[addr]
	if old == val {
		return nil
	}
	i.stack[addr] = val
	if old.Kind() == types.KindRef {
		i.release(old.Ref())
	}
	return nil
}

func (i *Interpreter) Load(addr int) (types.Value, error) {
	if !i.alive(addr) {
		return nil, ErrSegmentationFault
	}
	return i.heap[addr], nil
}

// Store replaces the value at addr. Concrete values transfer unique slot
// ownership; an existing heap ref is accepted only when it already names addr.
func (i *Interpreter) Store(addr int, val types.Value) (err error) {
	defer i.guard(&err)
	if !i.alive(addr) {
		return ErrSegmentationFault
	}
	ref, alias := 0, false
	switch v := val.(type) {
	case types.Boxed:
		if v.Kind() == types.KindRef {
			ref, alias = v.Ref(), true
		} else {
			val = types.Unbox(v)
		}
	case types.Ref:
		ref, alias = int(v), true
	}
	if alias {
		if !i.alive(ref) {
			return ErrSegmentationFault
		}
		if ref == addr {
			return nil
		}
		return ErrTypeMismatch
	}
	if owner := i.owner(val); owner >= 0 {
		if owner == addr {
			return nil
		}
		return ErrTypeMismatch
	}
	old := i.heap[addr]
	i.heap[addr] = val
	i.dispose(addr, old)
	if fn, ok := val.(*types.Function); ok {
		i.bind(addr, fn, true)
	}
	return nil
}

func (i *Interpreter) Alloc(val types.Value) (addr int, err error) {
	defer i.guard(&err)
	switch v := val.(type) {
	case types.Boxed:
		if v.Kind() == types.KindRef {
			addr = v.Ref()
			if !i.alive(addr) {
				return 0, ErrSegmentationFault
			}
			i.retain(addr)
			return addr, nil
		}
		val = types.Unbox(v)
	case types.Ref:
		addr = int(v)
		if !i.alive(addr) {
			return 0, ErrSegmentationFault
		}
		i.retain(addr)
		return addr, nil
	}
	if i.owner(val) >= 0 {
		return 0, ErrTypeMismatch
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
	if !i.alive(addr) {
		return nil, ErrSegmentationFault
	}
	i.retain(addr)
	return i.heap[addr], nil
}

func (i *Interpreter) Release(addr int) error {
	if !i.alive(addr) {
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
	if i.owner(val) >= 0 {
		return ErrTypeMismatch
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
	for addr := i.base; addr < len(i.heap); addr++ {
		if i.rc[addr] > 0 {
			i.finalize(addr, i.heap[addr])
		}
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

	heap := i.heap[:cap(i.heap)]
	clear(heap[i.base:])
	i.heap = heap[:i.base]
	hits := i.rc[:cap(i.rc)]
	clear(hits[i.base:])
	i.rc = hits[:i.base]
	i.recount()
	i.free = i.free[:0]

	// Restore each global from its declaration rather than its previous value.
	for idx, typ := range i.globalTypes {
		i.globals[idx] = i.zero(typ.Kind())
	}
	i.pace()
}

// compile lowers traces already recorded for addr and installs the resulting
// native entries. Recording belongs to observe and side-exit handling because
// only those paths hold the exact runtime state for their anchor.
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
			err = i.fault(r)
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

func (i *Interpreter) invoke(ctx context.Context, val types.Value, params []types.Boxed) (returns []types.Boxed, err error) {
	if i.ctx != nil || i.fp != 1 {
		return nil, ErrInterpreterBusy
	}
	target, ok := i.callable(val)
	if !ok {
		return nil, ErrTypeMismatch
	}
	base := i.sp
	if base+len(params)+1 > len(i.stack) {
		return nil, ErrStackOverflow
	}
	copy(i.stack[base:], params)
	i.sp += len(params)

	var addr int
	switch v := val.(type) {
	case types.Boxed:
		addr = v.Ref()
		i.retain(addr)
	default:
		addr, err = i.Alloc(target)
		if err != nil {
			i.sp = base
			return nil, err
		}
	}
	i.stack[i.sp] = types.BoxRef(addr)
	i.sp++

	saved := *i.fr
	defer func() {
		if err != nil {
			for i.fp > 1 {
				f := &i.frames[i.fp-1]
				if f.release {
					i.release(f.ref)
				}
				i.fp--
			}
			for _, value := range i.stack[base:i.sp] {
				i.releaseBox(value)
			}
		}
		i.sp = base
		i.fr = &i.frames[0]
		*i.fr = saved
	}()

	i.fr.code = []func(*Interpreter){threaded[instr.CALL](&threader{})}
	i.fr.ip = 0
	if err = i.Run(ctx); err != nil {
		return nil, err
	}
	returns = append([]types.Boxed(nil), i.stack[base:i.sp]...)
	return returns, nil
}

func (i *Interpreter) callable(val types.Value) (types.Value, bool) {
	if boxed, ok := val.(types.Boxed); ok {
		if boxed.Kind() != types.KindRef {
			return nil, false
		}
		loaded, err := i.Load(boxed.Ref())
		if err != nil {
			return nil, false
		}
		val = loaded
	}
	switch val.(type) {
	case *types.Function, *types.Closure:
		return val, true
	default:
		return nil, false
	}
}

func (i *Interpreter) compile(root anchor) error {
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	if i.compiler == nil {
		compiler, err := newCompiler()
		if err != nil {
			i.samples.AddMetric("vm_jit_errors_total", 1)
			i.recordCompile(prof.TriggerHot, compileResult{anchor: root, outcome: prof.CompileOutcomeError, reason: prof.CompileReasonError, err: err})
			return err
		}
		i.compiler = compiler
	}
	if i.compiler == nil {
		i.recordCompile(prof.TriggerHot, compileResult{anchor: root, outcome: prof.CompileOutcomeRejected, reason: prof.CompileReasonBackendUnavailable})
		return nil
	}
	result := i.compiler.Compile(i, root)
	i.recordCompile(prof.TriggerHot, result)
	if result.err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
		return result.err
	}
	if result.module == nil {
		return nil
	}
	i.install(result.module, true)
	return nil
}

// install accounts a successful Compile and rewires the dispatch table: a
// trace entry replaces the function's first opcode handler and keeps the
// shadowed threaded handler for guard fallback.
func (i *Interpreter) install(mod *module, account bool) {
	if account {
		i.account(mod)
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
		if entry.kind == entryFunction {
			atomic.StorePointer(&i.natives[a.addr], entry.callable.Addr())
		}
		stats := i.counters(a, entry)
		if entry.kind == entryLoop {
			i.code[a.addr][a.ip] = i.loop(a, entry.callable, stats)
		} else if entry.kind == entryModule {
			i.code[a.addr][a.ip] = i.start(a, entry.callable, stats)
		} else {
			i.code[a.addr][a.ip] = i.call(a, entry.callable, stats)
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
	// cache.claim/sync run every tick regardless of warmth: they adopt modules a
	// peer published and rearm after a hot side exit. Both are ~1 atomic when idle.
	if i.cache != nil {
		if request, ok := i.cache.claim(f.addr, i.threshold); ok {
			if err := i.shared(request.root, request.trigger); err != nil {
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
	samples := i.sample(f)
	if f.ip == 0 {
		i.tracer.capture(i, anchor{addr: f.addr})
	}
	if i.threshold >= 0 && f.ip > 0 && (i.eager || samples >= uint64(i.threshold)) {
		for _, header := range i.tracer.headers(i, f.addr) {
			if header != f.ip {
				continue
			}
			if err := i.trace(f); err != nil {
				return err
			}
			break
		}
	}
	root := anchor{addr: f.addr}
	if i.cache == nil && i.threshold >= 0 && samples >= uint64(i.threshold) && !i.tried[root] {
		i.tried[root] = true
		if err := i.compile(root); err != nil {
			return err
		}
	}
	return nil
}

// backedge receives the exact target of an unconditional backward
// branch. Eager functions install it immediately; sampled functions install it
// once when periodic profiling reaches the threshold, so cold BR handlers stay
// unchanged and no header scan is needed here.
func (i *Interpreter) backedge(f *frame) error {
	if f.ip <= 0 {
		return nil
	}
	return i.trace(f)
}

func (i *Interpreter) trace(f *frame) error {
	root := anchor{addr: f.addr, ip: f.ip}
	if i.exits[root] != nil || i.tried[root] {
		return nil
	}
	if i.eager {
		if i.samples.Samples(f.addr) == 0 {
			i.sample(f)
		}
		hits := i.loopHits[root] + 1
		i.loopHits[root] = hits
		if hits < loopWarmup {
			return nil
		}
	}
	i.tried[root] = true
	result := i.tracer.capture(i, root)
	if result.trace == nil {
		return nil
	}
	if i.cache != nil {
		i.cache.request(cacheRequest{root: root, trigger: prof.TriggerHot})
		return nil
	}
	return i.compile(root)
}

// call wraps a native trace Entry Callable. The CALL handler has already
// pushed a frame and set i.fr before this closure runs. The native Entry reads
// params from stack scratch slots; on a normal return
// this closure performs the frame teardown that RETURN would do in the threaded
// interpreter, and on a trap it rebuilds the native call chain into real VM
// frames before resuming threaded execution at the fallback IP.
func (i *Interpreter) call(root anchor, callable asm.Callable, stats counters) func(*Interpreter) {
	return func(i *Interpreter) {
		stats.enter()
		ctx := i.journalPtr()
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
			i.returnFrame()
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
			stats.yield()
			// A loop back-edge spent its budget. deopt left i.fr at the loop header;
			// run coordination, then let the threaded Run loop continue from there.
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		default:
			stats.exit(i.journal[journalExitID])
			i.bailout(root)
		}
	}
}

// start wraps a native trace for top-level code. Unlike function entries,
// top-level completion does not tear down its frame; it preserves the operand
// stack and marks the module frame as exhausted so dispatch returns normally.
func (i *Interpreter) start(root anchor, callable asm.Callable, stats counters) func(*Interpreter) {
	return func(i *Interpreter) {
		stats.enter()
		ctx := i.journalPtr()
		i.fr.code = nil
		i.fr.upvals = nil
		i.journal[journalBudget] = loopBudget
		if err := callable.Call(ctx); err != nil {
			panic(err)
		}

		i.sp = int(i.journal[journalSP])
		if i.journal[journalTrap] == trapNone {
			i.completeModule()
			return
		}

		i.deopt()
		switch i.journal[journalTrap] {
		case trapOverflow:
			panic(ErrFrameOverflow)
		case trapYield:
			stats.yield()
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		default:
			stats.exit(i.journal[journalExitID])
			i.bailout(root)
		}
	}
}

// loop wraps a native loop Callable installed at a loop header. Unlike entry,
// the header is reached mid-function with the frame already live, so loop never
// pushes a frame. It returns normally when a folded return or completion leg
// finishes native execution; every other exit is a trap.
// A spent budget yields to the safepoint and the Run loop re-enters native at
// the header; a guarded side exit or the loop-exit edge leaves deopt with
// i.fr at the resume IP for threaded dispatch to continue.
func (i *Interpreter) loop(root anchor, callable asm.Callable, stats counters) func(*Interpreter) {
	return func(i *Interpreter) {
		stats.enter()
		ctx := i.journalPtr()
		// Decouple the loop's safepoint cadence from tick: a native iteration does
		// the work of a whole loop body, so yielding every tick (1 under exact
		// dispatch) would drown the loop in deopt/re-enter churn. Run many
		// iterations natively between safepoints instead.
		i.journal[journalBudget] = loopBudget
		if err := callable.Call(ctx); err != nil {
			panic(err)
		}
		i.sp = int(i.journal[journalSP])
		if i.journal[journalTrap] == trapNone {
			if root.addr == 0 {
				i.completeModule()
			} else {
				i.returnFrame()
			}
			return
		}
		i.deopt()
		switch i.journal[journalTrap] {
		case trapOverflow:
			panic(ErrFrameOverflow)
		case trapYield:
			stats.yield()
			if err := i.safepoint(); err != nil {
				panic(err)
			}
		case trapFallback:
			stats.exit(i.journal[journalExitID])
			// Record the exit as a branch so the tracer captures the leg and a
			// hot in-loop branch recompiles the tree with the leg folded in.
			i.exit(root)
			// An exit that resumes at the header itself made no progress — the
			// header slot holds this native stub, so dispatching it again would
			// livelock (the hoist prologue's shape guard exits here). Run the
			// shadowed threaded handler once so the interpreter advances.
			if i.fr.addr == root.addr && i.fr.ip == root.ip {
				if fn := i.exits[root]; fn != nil {
					fn(i)
				}
			}
		}
	}
}

func (i *Interpreter) returnFrame() {
	f := i.fr
	i.sp = f.bp + f.returns
	if f.release {
		i.release(f.ref)
	}
	f.code = nil
	i.fp--
	i.fr = &i.frames[i.fp-1]
}

func (i *Interpreter) completeModule() {
	i.fr.ip = len(i.code[i.fr.addr])
	i.fr.code = i.code[i.fr.addr]
}

func (i *Interpreter) exit(root anchor) {
	hits := i.tracer.branch(i, root, anchor{addr: i.fr.addr, ip: i.fr.ip})
	if i.cache != nil {
		if hits < exitThreshold || hits%exitThreshold != 0 {
			return
		}
		i.cache.rearm(root)
		return
	}
	if hits != exitThreshold {
		return
	}
	if i.compiler == nil {
		return
	}
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	result := i.compiler.Compile(i, root)
	i.recordCompile(prof.TriggerSideExit, result)
	if result.err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
		panic(result.err)
	}
	if result.module != nil {
		i.install(result.module, true)
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

func (i *Interpreter) shared(root anchor, trigger prof.Trigger) error {
	addr := root.addr
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	compiler, err := newCompiler()
	if err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
		i.recordCompile(trigger, compileResult{anchor: root, outcome: prof.CompileOutcomeError, reason: prof.CompileReasonError, err: err})
		i.cache.fail(addr)
		return err
	}
	if compiler == nil {
		i.recordCompile(trigger, compileResult{anchor: root, outcome: prof.CompileOutcomeRejected, reason: prof.CompileReasonBackendUnavailable})
		i.cache.fail(addr)
		return nil
	}
	result := compiler.Compile(i, root)
	i.recordCompile(trigger, result)
	if result.err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
		_ = compiler.Close()
		i.cache.fail(addr)
		return result.err
	}
	if result.module == nil {
		_ = compiler.Close()
		i.cache.fail(addr)
		return nil
	}
	mod := result.module
	i.account(mod)
	var buf *asm.Buffer
	if len(mod.entries) > 0 {
		buf = compiler.buffer
	} else {
		_ = compiler.Close()
	}
	i.cache.publish(addr, mod, buf)
	return nil
}

func (i *Interpreter) counters(a anchor, entry native) counters {
	if i.profiler == nil {
		return counters{}
	}
	kind := entry.kind.profile()
	stats := counters{
		entry:  i.samples.RegisterEntry(a.addr, a.ip, kind, entry.frontend),
		yields: i.samples.RegisterYield(a.addr, a.ip, kind, entry.frontend),
		exits:  make([]*prof.Counter, len(entry.exits)),
	}
	for id, exit := range entry.exits {
		stats.exits[id] = i.samples.RegisterExit(a.addr, a.ip, kind, entry.frontend, exit.reason, exit.opcode)
	}
	return stats
}

func (i *Interpreter) account(mod *module) {
	i.samples.AddMetric("vm_jit_emits_total", float64(len(mod.entries)))
	i.samples.AddMetric("vm_jit_bytes_total", float64(mod.bytes))
	if i.profiler == nil {
		return
	}
	for a, entry := range mod.entries {
		i.samples.RecordEmit(a.addr, a.ip, entry.kind.profile(), entry.frontend, entry.bytes)
	}
}

func (i *Interpreter) recordCompile(trigger prof.Trigger, result compileResult) {
	if i.profiler != nil {
		i.samples.RecordCompile(result.anchor.addr, result.anchor.ip, trigger, result.frontend, result.outcome, result.reason)
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

// sample records one profile hit for the frame's current instruction and
// returns the updated function count.
func (i *Interpreter) sample(f *frame) uint64 {
	i.samples.Add(f.addr, f.ip, i.instrs[f.addr][f.ip])
	samples := i.samples.Samples(f.addr)
	if nativeBackend && !i.eager && i.threshold >= 0 && samples >= uint64(i.threshold) {
		i.enableBackedges(f.addr)
	}
	return samples
}

// enableBackedges rethreads one hot function with exact unconditional
// back-edge observation. Cold functions keep the original zero-overhead BR
// handler until periodic sampling reaches the configured threshold.
func (i *Interpreter) enableBackedges(addr int) {
	if addr < 0 || addr >= len(i.backedges) || i.backedges[addr] {
		return
	}
	fn, ok := i.function(addr)
	if !ok || fn == nil {
		return
	}
	c := i.threader(true)
	previous := i.code[addr]
	compiled := c.Compile(fn.Code, fn.Slots(), types.Kinds(fn.Captures))
	// Rethreading replaces only interpreted handlers. Installed native entries
	// stay live while their saved fallbacks advance to the exact-backedge table.
	for root := range i.exits {
		if root.addr != addr || root.ip < 0 || root.ip >= len(compiled) || root.ip >= len(previous) {
			continue
		}
		i.exits[root] = compiled[root.ip]
		if root.ip == 0 {
			i.stubs[addr] = compiled[root.ip]
		}
		compiled[root.ip] = previous[root.ip]
	}
	i.code[addr] = compiled
	i.backedges[addr] = true
	for idx := 0; idx < i.fp; idx++ {
		if i.frames[idx].addr == addr {
			i.frames[idx].code = i.code[addr]
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

// journalPtr resets and fills the journal handed to native code: stack/global base
// pointers, current frame BP/SP, pointer cells for native fast paths, and the
// per-call frame budget. It returns &journal[0], passed to native code in X0.
func (i *Interpreter) journalPtr() unsafe.Pointer {
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
	i.journal[journalNatives] = 0
	if len(i.natives) > 0 {
		i.journal[journalNatives] = uint64(uintptr(unsafe.Pointer(&i.natives[0])))
	}

	i.journal[journalDepth] = 0
	i.journal[journalCap] = uint64(min(len(i.frames)-i.fp, nativeFrameLimit))
	i.journal[journalTrap] = trapNone
	i.journal[journalExitID] = 0
	i.journal[journalBudget] = uint64(i.tick)
	i.journal[journalActive] = 0
	return unsafe.Pointer(&i.journal[0])
}

func (i *Interpreter) fault(r any) error {
	return &RuntimeError{
		Err:    i.cause(r),
		Frames: i.stacktrace(),
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

func (i *Interpreter) stacktrace() []FrameInfo {
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

func (i *Interpreter) unbox(val types.Boxed) types.Value {
	if val.Kind() != types.KindRef {
		return types.Unbox(val)
	}
	addr := val.Ref()
	v := i.heap[addr]
	i.release(addr)
	return v
}

func (i *Interpreter) keep(val types.Value) int {
	return i.alloc(val)
}

func (i *Interpreter) alloc(val types.Value) int {
	collected := i.target > 0 && len(i.heap)-len(i.free) >= i.target
	if collected {
		i.gc()
	}
	if addr, ok := i.reuse(val); ok {
		return addr
	}

	full := len(i.heap) == cap(i.heap)
	limited := i.limit > 0 && len(i.heap) >= i.limit
	if !collected && (full || limited) {
		i.gc()
		if addr, ok := i.reuse(val); ok {
			return addr
		}
	}
	if limited {
		panic(ErrHeapExhausted)
	}

	if full {
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

func (i *Interpreter) bind(addr int, fn *types.Function, dynamic bool) {
	n := addr + 1
	if addr >= len(i.instrs) {
		i.instrs = append(i.instrs, make([][]byte, n-len(i.instrs))...)
	}
	if addr >= len(i.code) {
		i.code = append(i.code, make([][]func(*Interpreter), n-len(i.code))...)
	}
	if addr >= len(i.backedges) {
		i.backedges = append(i.backedges, make([]bool, n-len(i.backedges))...)
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
	i.backedges[addr] = nativeBackend && i.eager
	c := i.threader(i.backedges[addr])
	if dynamic {
		i.coros[addr] = i.yields(fn.Code)
	}
	i.instrs[addr] = fn.Code
	i.handlers[addr] = fn.Handlers
	i.code[addr] = c.Compile(fn.Code, fn.Slots(), types.Kinds(fn.Captures))
	if dynamic {
		i.dynamic[addr] = true
	}
}

// globalKinds returns the logical kinds of current global values for JIT
// specialization. Heap-backed i64 values recover KindI64 from the heap object.
// Dynamic scalar globals remain unknown so native lowering does not assume a
// stable representation.
func (i *Interpreter) globalKinds() []types.Kind {
	kinds := make([]types.Kind, len(i.globals))
	for idx, val := range i.globals {
		kind := val.Kind()
		if kind == types.KindRef && i.alive(val.Ref()) {
			kind = i.heap[val.Ref()].Kind()
		}
		if i.globalTypes[idx] == types.TypeRef && kind != types.KindRef {
			kind = instr.KindAny
		}
		kinds[idx] = kind
	}
	return kinds
}

// globalReprs returns stable representations for threaded handler selection.
// Dynamic globals stay unknown so their handlers inspect each boxed value.
func (i *Interpreter) globalReprs() []types.Kind {
	kinds := make([]types.Kind, len(i.globalTypes))
	for idx, typ := range i.globalTypes {
		if typ == types.TypeRef {
			kinds[idx] = instr.KindAny
		} else {
			kinds[idx] = typ.Kind()
		}
	}
	return kinds
}

// threader builds generated dispatch state. The backedge callback is injected at
// runtime instead of referenced by the generated global handler table, avoiding
// an initialization cycle through trace compilation.
func (i *Interpreter) threader(backedge bool) *threader {
	c := &threader{
		types:     i.types,
		constants: i.constants,
		heap:      i.heap,
		coros:     i.coros,
		globals:   i.globalReprs(),
		exact:     i.tick == 1,
	}
	if backedge {
		c.backedge = (*Interpreter).backedge
	}
	return c
}

// recount rebuilds baseline counts from constant roots and heap edges after
// construction or reset has removed all dynamic slots.
func (i *Interpreter) recount() {
	clear(i.rc)
	i.rc[0] = 1
	for _, val := range i.constants {
		if val.Kind() != types.KindRef {
			continue
		}
		addr := val.Ref()
		if addr >= 0 && addr < len(i.rc) {
			i.rc[addr]++
		}
	}
	for addr := 1; addr < len(i.heap); addr++ {
		for _, ref := range i.refs(i.heap[addr]) {
			child := int(ref)
			if child >= 0 && child < len(i.rc) {
				i.rc[child]++
			}
		}
	}
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

func (i *Interpreter) owner(val types.Value) int {
	pointer := reflect.ValueOf(val)
	if !pointer.IsValid() || pointer.Kind() != reflect.Pointer {
		return -1
	}
	for addr, current := range i.heap {
		if !i.alive(addr) {
			continue
		}
		owner := reflect.ValueOf(current)
		if owner.IsValid() && owner.Type() == pointer.Type() && owner.Pointer() == pointer.Pointer() {
			return addr
		}
	}
	return -1
}

func (i *Interpreter) alive(addr int) bool {
	return addr >= 0 && addr < len(i.heap) && i.rc[addr] > 0
}

func (i *Interpreter) retain(addr int) {
	i.rc[addr]++
}

func (i *Interpreter) retains(addr int, n int) {
	i.rc[addr] += n
}

func (i *Interpreter) gc() {
	i.scan()
	i.mark()
	i.sweep()
	i.pace()
}

func (i *Interpreter) pace() {
	live := len(i.heap) - len(i.free)
	target := live + max(live-i.base, heapRunway)
	if i.limit > 0 {
		target = min(target, i.limit)
	}
	i.target = max(target, live)
}

// scan derives each object's external incoming count. Exact rc includes both
// heap edges and owners outside the heap; subtracting every heap-to-heap edge
// leaves a positive value only for objects owned by the stack, constants,
// globals, frames, temporary construction state, or the host.
func (i *Interpreter) scan() {
	n := len(i.rc)
	if cap(i.trial) < n {
		i.trial = make([]int, n)
	} else {
		i.trial = i.trial[:n]
	}
	copy(i.trial, i.rc)

	for addr := 1; addr < n; addr++ {
		if i.rc[addr] <= 0 {
			continue
		}
		for _, ref := range i.refs(i.heap[addr]) {
			child := int(ref)
			if child == 0 {
				continue
			}
			if child < 0 || child >= n || i.rc[child] <= 0 {
				panic(ErrSegmentationFault)
			}
			i.trial[child]--
		}
	}
	for addr := 1; addr < n; addr++ {
		if i.rc[addr] > 0 && i.trial[addr] < 0 {
			panic(ErrSegmentationFault)
		}
	}
}

// mark traces from every positive external count. A negative trial value marks
// a survivor; zero remains an unreachable cycle candidate.
func (i *Interpreter) mark() {
	i.work = i.work[:0]
	push := func(addr int) {
		if addr <= 0 || addr >= len(i.rc) || i.rc[addr] <= 0 || i.trial[addr] < 0 {
			return
		}
		i.trial[addr] = -i.trial[addr] - 1
		i.work = append(i.work, addr)
	}

	for addr := 1; addr < len(i.rc); addr++ {
		if i.rc[addr] > 0 && i.trial[addr] > 0 {
			push(addr)
		}
	}
	for len(i.work) > 0 {
		addr := i.work[len(i.work)-1]
		i.work = i.work[:len(i.work)-1]
		for _, ref := range i.refs(i.heap[addr]) {
			push(int(ref))
		}
	}
}

// sweep reclaims every allocated unmarked slot. Edges from dead objects to
// survivors are removed from exact rc; dead-to-dead edges need no adjustment
// because both slots are discarded by this pass.
func (i *Interpreter) sweep() {
	for addr := 1; addr < len(i.heap); addr++ {
		if i.rc[addr] <= 0 || i.trial[addr] < 0 {
			continue
		}
		v := i.heap[addr]
		for _, ref := range i.refs(v) {
			child := int(ref)
			if child > 0 && child < len(i.rc) && i.rc[child] > 0 && i.trial[child] < 0 {
				i.rc[child]--
			}
		}
		i.rc[addr] = 0
		i.reclaim(addr, v)
	}
}

// dispose releases the refs owned by v and finalizes its non-heap resources.
// The containing slot stays allocated, so Store can replace a value without
// changing the address or its external refcount.
func (i *Interpreter) dispose(addr int, v types.Value) {
	var local [8]int
	children := local[:0]
	for _, ref := range i.refs(v) {
		children = append(children, int(ref))
	}
	for _, child := range children {
		i.release(child)
	}
	i.finalize(addr, v)
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

// reclaim finalizes slot addr holding v, clears it, and returns the stable
// address to the free list. The caller has already settled its referents.
func (i *Interpreter) reclaim(addr int, v types.Value) {
	i.finalize(addr, v)
	i.heap[addr] = nil
	i.free = append(i.free, addr)
}

func (i *Interpreter) finalize(addr int, v types.Value) {
	if _, ok := v.(*types.Function); ok {
		i.remove(addr)
	}
	if s, ok := v.(types.String); ok && i.interned[string(s)] == types.Ref(addr) {
		delete(i.interned, string(s))
	}
	// External finalizers belong to committed execution, never speculative
	// trace capture.
	if !i.speculative {
		if c, ok := v.(io.Closer); ok {
			_ = c.Close()
		}
	}
}

func (i *Interpreter) remove(addr int) {
	if addr < 0 || addr >= len(i.instrs) {
		delete(i.dynamic, addr)
		return
	}
	i.instrs[addr] = nil
	i.code[addr] = nil
	i.backedges[addr] = false
	i.stubs[addr] = nil
	i.handlers[addr] = nil
	i.coros[addr] = false
	for a := range i.exits {
		if a.addr == addr {
			delete(i.exits, a)
		}
	}
	for a := range i.tried {
		if a.addr == addr {
			delete(i.tried, a)
		}
	}
	for a := range i.loopHits {
		if a.addr == addr {
			delete(i.loopHits, a)
		}
	}
	if i.tracer != nil {
		i.tracer.remove(addr)
	}
	delete(i.dynamic, addr)
}

func (i *Interpreter) yields(code []byte) bool {
	for ip := 0; ip < len(code); {
		if instr.Opcode(code[ip]) == instr.YIELD {
			return true
		}
		w := instr.Instruction(code[ip:]).Width()
		if w <= 0 {
			break
		}
		ip += w
	}
	return false
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
