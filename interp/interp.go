package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"time"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Interpreter struct {
	ctx         context.Context
	done        <-chan struct{}
	tracer      *tracer
	hook        func(*Interpreter) error
	codec       Codec
	speculative bool

	compiler  *jit.Compiler
	cache     *cache
	profiler  *prof.Profiler
	samples   *prof.Collector
	exits     map[jit.Anchor]func(*Interpreter)
	natives   []unsafe.Pointer
	tried     map[jit.Anchor]bool
	live      map[jit.Anchor]jit.Entry
	watchdogs map[jit.Anchor]*watchdog
	journal   []uint64

	types       []types.Type
	constants   []types.Boxed
	globals     []types.Boxed
	globalTypes []types.Type
	instrs      [][]byte
	code        [][]func(*Interpreter)
	backedges   []bool
	entries     []uint64
	cold        []bool
	misses      []uint8
	coros       []bool
	handlers    [][]instr.Handler
	module      *types.Function
	dynamic     map[int]bool

	frames  []frame
	fr      *frame
	stack   []types.Boxed
	heap    []types.Value
	base    int
	target  int
	owners  map[types.Value]int
	free    []int
	rc      []int
	trial   []int
	work    []int
	refbuf  []types.Ref
	arrays  pool[*types.Array]
	structs pool[*types.Struct]

	// enc and dec are the scratch a host value converts through. A conversion
	// takes a pointer, so building one per field access would allocate on every
	// read; the interpreter executing the access owns it instead, which also
	// keeps a host value shared by pooled interpreters off a single encoder.
	enc Encoder
	dec Decoder

	// tail is the append-only byte buffer the most recent string.concat
	// published from. A join whose left operand ends exactly where tail ends
	// extends it and publishes a new ref over the longer prefix; bytes below any
	// published length are never rewritten, so every string already handed out
	// keeps its own content whatever its reference count.
	tail []byte

	fp  int
	sp  int
	gen int
	gas int64

	threshold int64
	trigger   uint64
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

// Option configures an Interpreter or Pool at construction. Only the With
// constructors produce one, so callers can name and collect options without
// reaching the unexported state they configure.
type Option func(*option)

type option struct {
	hook      func(*Interpreter) error
	codec     Codec
	cache     *cache
	tracer    *tracer
	profiler  *prof.Profiler
	threshold int

	frame   int
	stack   int
	heap    int
	maxHeap int
	tick    int
	fuel    uint64
}

const heapRunway = 64

// loopWarmup is how many times one back edge runs between reports to the
// interpreter. It is a fixed interval, not a threshold: each report is one hot
// event for the enclosing function, and the configured threshold is what counts
// those. Reporting every iteration would make a loop cross any threshold before
// its body had run enough to be worth compiling. The generated back-edge
// handlers compare against it directly.
const loopWarmup = 8

func WithHook(fn func(*Interpreter) error) Option {
	return func(o *option) { o.hook = fn }
}

// WithCodec installs the codec Marshal, Unmarshal, and every host conversion
// run through. It defaults to NewRegistry(), so per-type registration is the
// normal way to customize conversion and replacing the codec is the escape
// hatch for a wholly different one.
func WithCodec(c Codec) Option {
	return func(o *option) { o.codec = c }
}

// WithProfiler attaches a profiler that aggregates this interpreter's execution
// samples and JIT counters. It is opt-in and observational: what compiles is
// decided by the call and back-edge counters either way, so attaching one only
// adds the per-tick sampling that fills the profile. Pass the same Profiler to
// NewPool so every pooled interpreter shares it.
func WithProfiler(p *prof.Profiler) Option {
	return func(o *option) {
		o.profiler = p
	}
}

func WithFrame(val int) Option {
	return func(o *option) { o.frame = val }
}

func WithStack(val int) Option {
	return func(o *option) { o.stack = val }
}

func WithHeap(val int) Option {
	return func(o *option) { o.heap = val }
}

func WithHeapLimit(val int) Option {
	return func(o *option) { o.maxHeap = val }
}

func WithTick(val int) Option {
	return func(o *option) { o.tick = val }
}

func WithThreshold(val int) Option {
	return func(o *option) { o.threshold = val }
}

func WithFuel(val uint64) Option {
	return func(o *option) { o.fuel = val }
}

func withCache(c *cache) Option {
	return func(o *option) { o.cache = c }
}

// withTracer shares tracing state with interpreters for the same program.
// A tracer already bound to another program is isolated automatically.
func withTracer(t *tracer) Option {
	return func(o *option) { o.tracer = t }
}

// New builds an interpreter for prog. It trusts prog to be well-formed; run
// program.Verify(prog) beforehand to reject malformed or untrusted bytecode.
func New(prog *program.Program, opts ...Option) *Interpreter {
	opt := option{
		frame:     128,
		stack:     1024,
		heap:      128,
		tick:      128,
		threshold: 64,
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
		tracer = newTracer()
		tracer.bind(prog)
	}
	samples := prof.NewCollector()
	activeCodec := opt.codec
	if activeCodec == nil {
		activeCodec = NewRegistry()
	}

	var fuel int64 = -1
	if opt.fuel > 0 {
		ticks := (opt.fuel-1)/uint64(opt.tick) + 1
		fuel = int64(min(ticks, 1<<63-1))
	}

	// threshold counts hot events - one per call into a function, one per
	// warmed back edge - not instructions, so it is used as given. It is NOT
	// divided by tick: nothing about tiering up runs on the tick loop any more.
	threshold := int64(opt.threshold)
	if threshold == 0 {
		threshold = 1
	}
	var trigger uint64
	if threshold > 0 {
		trigger = uint64(threshold)
	}

	i := &Interpreter{
		tracer:      tracer,
		hook:        opt.hook,
		codec:       activeCodec,
		cache:       opt.cache,
		profiler:    opt.profiler,
		samples:     samples,
		threshold:   threshold,
		trigger:     trigger,
		types:       prog.Types,
		constants:   make([]types.Boxed, len(prog.Constants)),
		globals:     make([]types.Boxed, len(prog.Globals)),
		globalTypes: prog.Globals,
		instrs:      make([][]byte, len(prog.Constants)+1),
		code:        make([][]func(*Interpreter), len(prog.Constants)+1),
		backedges:   make([]bool, len(prog.Constants)+1),
		entries:     make([]uint64, len(prog.Constants)+1),
		cold:        make([]bool, len(prog.Constants)+1),
		misses:      make([]uint8, len(prog.Constants)+1),
		coros:       make([]bool, len(prog.Constants)+1),
		handlers:    make([][]instr.Handler, len(prog.Constants)+1),
		exits:       map[jit.Anchor]func(*Interpreter){},
		natives:     make([]unsafe.Pointer, len(prog.Constants)+1),
		tried:       map[jit.Anchor]bool{},
		live:        map[jit.Anchor]jit.Entry{},
		watchdogs:   map[jit.Anchor]*watchdog{},
		dynamic:     map[int]bool{},
		journal:     make([]uint64, journal.Len(opt.frame)),
		frames:      make([]frame, opt.frame),
		stack:       make([]types.Boxed, opt.stack),
		heap:        make([]types.Value, 0, opt.heap),
		owners:      make(map[types.Value]int),
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
	//
	// dedup gives identical string literals one shared cell. Nothing depends on
	// that sharing - every string comparison and string map key compares content
	// - so it is a load-time pool economy only, and the index dies with the loop.
	dedup := make(map[string]types.Ref)
	for j, v := range prog.Constants {
		var val types.Boxed
		switch v := v.(type) {
		case types.Boxed:
			val = v
			if val.Kind() == types.KindRef {
				if addr := val.Ref(); i.alive(addr) {
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
			if addr := int(v); i.alive(addr) {
				i.retain(addr)
			}
		case types.String:
			ref, seen := dedup[string(v)]
			if seen {
				i.retain(int(ref))
			} else {
				ref = types.Ref(i.alloc(v))
				dedup[string(v)] = ref
			}
			val = types.BoxRef(int(ref))
		default:
			val = types.BoxRef(i.alloc(v))
			for _, ref := range i.refs(v) {
				if addr := int(ref); i.alive(addr) {
					i.retain(addr)
				}
			}
		}
		i.constants[j] = val
		if fn, ok := v.(*types.Function); ok {
			addr := val.Ref()
			i.instrs[addr] = fn.Code
			i.handlers[addr] = fn.Handlers
			i.coros[addr] = i.yields(fn.Code)
		}
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

	// Execution specializes from the current global values; globalTypes remains
	// the boundary contract for SetGlobal and Reset.
	i.seed()

	i.backedges[0] = nativeBackend && i.threshold >= 0
	c := i.threader(i.backedges[0])
	i.code[0] = c.Compile(prog.Code, i.module.Slots(), i.module.Declared(), types.Kinds(i.module.Captures), i.module.Captures)

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

func (i *Interpreter) Run(ctx context.Context) (err error) {
	i.probeBoundary()
	defer i.probeBoundary()
	i.ctx = ctx
	i.done = nil
	if ctx != nil {
		i.done = ctx.Done()
	}
	// The top frame is built by New and Reset, not by a threaded call handler, so
	// this is the module's only entry hook. It runs once per Run: a caught throw
	// loops below without re-entering. The host-callback trampoline replaces the
	// frame's code table, so it is the only frame that must skip this hook.
	if i.owns(i.fr) {
		if err := i.hit(); err != nil {
			i.ctx = nil
			i.done = nil
			return err
		}
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
	return i.codec.Marshal(i, v)
}

func (i *Interpreter) Unmarshal(v types.Value, dst any) error {
	return i.codec.Unmarshal(i, v, dst)
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
	val := i.heap[addr]
	i.own(addr, val)
	return val, nil
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
	switch old.(type) {
	case *types.Function, *types.Closure, *coroutine:
		return ErrTypeMismatch
	}
	i.heap[addr] = val
	i.own(addr, val)
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
	addr = i.alloc(val)
	i.own(addr, val)
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
	val := i.heap[addr]
	i.own(addr, val)
	return val, nil
}

func (i *Interpreter) Release(addr int) error {
	if !i.alive(addr) {
		return ErrSegmentationFault
	}
	i.release(addr)
	return nil
}

// RefCount returns the live reference count for the heap value at addr, or
// ErrSegmentationFault if addr does not name a live value. An embedder that
// passes references across the host boundary with Retain and Release has no
// other way to verify that the two stayed balanced.
func (i *Interpreter) RefCount(addr int) (int, error) {
	if !i.alive(addr) {
		return 0, ErrSegmentationFault
	}
	return i.rc[addr], nil
}

// HeapLen returns one past the highest heap address the interpreter has
// allocated, so an embedder can walk live addresses with RefCount or Load.
// It bounds a scan; it is not a count of live values, because released slots
// stay in range until they are reused, and it is not the backing array's
// capacity, which runs ahead of it. Watching it grow against WithHeapLimit is
// the only way to observe memory pressure before allocation fails.
func (i *Interpreter) HeapLen() int {
	return len(i.heap)
}

func (i *Interpreter) Push(val types.Value) (err error) {
	defer i.guard(&err)
	if i.sp == len(i.stack) {
		return ErrStackOverflow
	}
	if i.owner(val) >= 0 {
		return ErrTypeMismatch
	}
	boxed := i.box(val)
	if boxed.Kind() == types.KindRef {
		i.own(boxed.Ref(), val)
	}
	i.stack[i.sp] = boxed
	i.sp++
	return nil
}

func (i *Interpreter) Pop() (types.Value, error) {
	if i.sp == 0 {
		return nil, ErrStackUnderflow
	}
	i.sp--
	boxed := i.stack[i.sp]
	val := i.unbox(boxed)
	if boxed.Kind() == types.KindRef {
		i.own(boxed.Ref(), val)
	}
	return val, nil
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

// Flush publishes the interpreter's pending execution samples and JIT
// counters into the attached profiler without closing the interpreter, so a
// long-running embedded VM can observe metrics without waiting for Close. It
// has no observable effect when no profiler is attached (see WithProfiler).
func (i *Interpreter) Flush() {
	i.flush()
}

func (i *Interpreter) Close() error {
	i.flush()
	i.Reset()
	i.arrays.clear()
	i.structs.clear()
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
	// Keep the recent peak, but let a smaller heap shrink an old high-water mark.
	dynamic := len(i.heap) - i.base
	keepStructs := i.structs.trim(dynamic)
	keepArrays := i.arrays.trim(dynamic)
	for addr := i.base; addr < len(i.heap); addr++ {
		value := i.heap[addr]
		if i.rc[addr] <= 0 {
			continue
		}
		i.finalize(addr, value)
		switch value := value.(type) {
		case *types.Struct:
			if len(value.Typ.Fields) <= 4 && len(i.structs.values) < keepStructs {
				*value = types.Struct{}
				i.structs.put(value)
			}
		case *types.Array:
			if len(i.arrays.values) < keepArrays {
				clear(value.Elems[:cap(value.Elems)])
				value.Typ = nil
				value.Elems = value.Elems[:0]
				i.arrays.put(value)
			}
		}
	}
	i.structs.reset()
	i.arrays.reset()
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
	rc := i.rc[:cap(i.rc)]
	clear(rc[i.base:])
	i.rc = rc[:i.base]
	i.recount()
	i.free = i.free[:0]
	i.tail = nil

	i.seed()
	i.pace()
}

// seed restores each global from its declaration rather than its previous value.
func (i *Interpreter) seed() {
	for idx, typ := range i.globalTypes {
		i.globals[idx] = i.zero(typ.Kind())
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
			err = i.fault(r)
		}
	}()

	f := i.fr
	code := f.code
	// Tiering up is driven by the entry and back-edge hooks compiled into the
	// handlers, not by this loop, so a program with nothing else to coordinate
	// runs with no per-instruction accounting at all whether or not the JIT is
	// enabled. The countdown below survives only for what genuinely needs an
	// instruction-grained cadence: cancellation, fuel, the user hook, the user
	// profiler, and a pool's shared-module handshake.
	if i.done == nil && i.gas < 0 && i.hook == nil && i.profiler == nil && i.cache == nil {
		for f.ip < len(code) {
			code[f.ip](i)
			f = i.fr
			code = f.code
		}
		return false, nil
	}

	tick := i.tick

	for f.ip < len(code) {
		tick--
		if tick == 0 {
			tick = i.tick
			if err := i.safepoint(); err != nil {
				return false, err
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
		// A callable the heap already holds keeps the slot it has: a second
		// one would alias the same Go value, which Alloc refuses. Only a
		// callable the host built and never published needs one.
		if addr = i.owner(target); addr >= 0 {
			i.retain(addr)
			break
		}
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

	// The trampoline runs one CALL and nothing else, so it needs no program
	// context - but it must still count the callee it dispatches, or a function
	// only ever reached from a host callback never becomes hot.
	i.fr.code = []func(*Interpreter){threaded[instr.CALL](&threader{entry: (*Interpreter).entered})}
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
	case *types.Function, *types.Closure, *HostFunction:
		return val, true
	default:
		return nil, false
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

	// Sampling here serves the user's profiler and nothing else: which
	// functions are worth compiling is decided by the entry and back-edge
	// hooks, which observe real calls and real iterations rather than whichever
	// instruction a countdown stopped on.
	if i.profiler != nil {
		i.sample(f)
	}

	// Adopting a module a peer published is a matter of elapsed time, not of
	// this member's own hotness, so sync stays on the tick. Claiming the right
	// to compile does not: it aggregates hot events across members and is
	// raised from the same hooks a solo interpreter compiles from (see entered).
	if i.cache != nil {
		i.sync()
	}
	return nil
}

func (i *Interpreter) owns(f *frame) bool {
	if f.addr < 0 || f.addr >= len(i.code) {
		return false
	}
	code := i.code[f.addr]
	return len(f.code) > 0 && len(code) > 0 && &f.code[0] == &code[0]
}

// probeBoundary discards an incomplete throughput sample at a Run boundary.
// Warmup and completed verdicts remain intact; an interrupted timed pair is
// restarted from a fresh native window so host-side work between Run calls is
// never included in the measured interpreter span.
func (i *Interpreter) probeBoundary() {
	if len(i.watchdogs) == 0 {
		return
	}
	for _, wd := range i.watchdogs {
		if wd.probe == probeShadow {
			wd.probe = probeNative
			wd.probeNative = 0
		} else if wd.probe == probeNative {
			wd.probeNative = 0
		}
		wd.probeCount = 0
		wd.probeStart = time.Time{}
		wd.probePending = false
	}
}

func (i *Interpreter) flush() {
	if i.profiler != nil {
		i.profiler.Flush(i.samples)
	}
}

// sample records one profile hit for the frame's current instruction. It feeds
// the user's profiler only; tiering up is driven by the entry and back-edge
// hooks (see entered and backedge).
func (i *Interpreter) sample(f *frame) {
	i.samples.Add(f.addr, f.ip, i.instrs[f.addr][f.ip])
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
	return types.BoxRef(i.alloc(types.WrapError(ErrorCode(err), err)))
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

// negZeroF32 and negZeroF64 are the bit patterns of -0.0. A map key folds them
// onto +0.0 so both spellings of zero index one entry.
const (
	negZeroF32 = uint32(1) << 31
	negZeroF64 = uint64(1) << 63
)

// mapKey indexes one entry of a generic map. It is the single owner of the
// rule every map opcode and the codec must agree on, because a key written
// under one spelling and looked up under another is unreachable.
//
// A scalar keys by value, i1 and i8 through their i32 representation. A string
// keys by content, so equal strings index one entry however each was
// published, as strings compare by content everywhere else. Every other
// reference keys by heap address.
//
// The second result is the key a new entry stores: zero when the MapKey alone
// reconstructs it, and otherwise a reference the entry takes ownership of. A
// caller that only looks up releases it instead.
func (i *Interpreter) mapKey(key types.Boxed) (types.MapKey, types.Boxed) {
	switch key.Kind() {
	case types.KindI1, types.KindI8, types.KindI32:
		bits := uint64(uint32(key.I32()))
		return types.MapKey{Kind: types.KindI32, Bits: bits}, types.BoxI32(int32(bits))
	case types.KindI64:
		return types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}, 0
	case types.KindF32:
		bits := math.Float32bits(key.F32())
		if bits == negZeroF32 {
			bits = 0
		}
		return types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}, types.BoxF32(math.Float32frombits(bits))
	case types.KindF64:
		bits := math.Float64bits(key.F64())
		if bits == negZeroF64 {
			bits = 0
		}
		return types.MapKey{Kind: types.KindF64, Bits: bits}, types.BoxF64(math.Float64frombits(bits))
	case types.KindRef:
		switch value := i.heap[key.Ref()].(type) {
		case types.I64:
			return types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}, 0
		case types.String:
			return types.MapKey{Kind: types.KindText, Text: string(value)}, key
		}
		return types.MapKey{Kind: types.KindRef, Bits: uint64(key.Ref())}, key
	default:
		panic(ErrTypeMismatch)
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
		return types.BoxRef(i.alloc(v))
	default:
		addr := i.alloc(v)
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

// encoder and decoder hand out the interpreter's own scratch, reset for one
// conversion. A conversion that can nest another owns one instead, which is why
// Marshal and Unmarshal build their own.
func (i *Interpreter) encoder(r *Registry) *Encoder {
	e := &i.enc
	*e = Encoder{interp: i, registry: r, owned: e.owned[:0]}
	return e
}

func (i *Interpreter) decoder(r *Registry) *Decoder {
	d := &i.dec
	*d = Decoder{interp: i, registry: r}
	return d
}

// arrayGet reads the element at index at off the array bound to heap
// address addr, covering every TypedArray[_] representation and the generic
// *types.Array alike. It is the generic counterpart to the specialized reads
// array.get fusion emits when a slot's declared element kind matches the
// runtime representation: a fused handler falls back to arrayGet exactly
// when that specialization misses, and the unfused ARRAY_GET handler calls
// it unconditionally. A *types.Array element is always an owned ref and is
// retained here; a TypedArray[_] element is a scalar copy and needs none.
// arrayGet does not release addr itself — callers that only borrowed the
// container ref (a fused read) must leave it alone, and callers that popped
// an owned ref (the unfused handler) must release it themselves.
func (i *Interpreter) arrayGet(addr, at int) types.Boxed {
	switch array := i.heap[addr].(type) {
	case types.TypedArray[bool]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		return types.BoxI1(array[at])
	case types.TypedArray[int8]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		return types.BoxI8(array[at])
	case types.TypedArray[int32]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		return types.BoxI32(array[at])
	case types.TypedArray[int64]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		return i.boxI64(array[at])
	case types.TypedArray[float32]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		return types.BoxF32(array[at])
	case types.TypedArray[float64]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		return types.BoxF64(array[at])
	case *types.Array:
		if at < 0 || at >= len(array.Elems) {
			panic(ErrIndexOutOfRange)
		}
		result := array.Elems[at]
		i.retainBox(result)
		return result
	case *HostArray:
		// A view converts on the way out instead of holding VM words, so a
		// conversion that fails traps the way the threaded contract expects.
		result, err := array.Element(i, at)
		if err != nil {
			panic(err)
		}
		return result
	default:
		panic(ErrTypeMismatch)
	}
}

// arraySet writes val to the array at addr and index.
// Reference elements transfer ownership to the array.
func (i *Interpreter) arraySet(addr, at int, val types.Boxed) {
	switch array := i.heap[addr].(type) {
	case types.TypedArray[bool]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		array[at] = val.Bool()
	case types.TypedArray[int8]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		array[at] = int8(val.I32())
	case types.TypedArray[int32]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		array[at] = val.I32()
	case types.TypedArray[int64]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		array[at] = i.unboxI64(val)
	case types.TypedArray[float32]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		array[at] = val.F32()
	case types.TypedArray[float64]:
		if at < 0 || at >= len(array) {
			panic(ErrIndexOutOfRange)
		}
		array[at] = val.F64()
	case *types.Array:
		if at < 0 || at >= len(array.Elems) {
			panic(ErrIndexOutOfRange)
		}
		elem := array.Elems[at]
		array.Elems[at] = val
		i.releaseBox(elem)
	case *HostArray:
		if err := array.SetElement(i, at, val); err != nil {
			panic(err)
		}
	default:
		panic(ErrTypeMismatch)
	}
}

// structField reads the field at index at off the struct bound to heap address
// addr, covering a native *types.Struct and a *HostStruct alike. It is the
// generic counterpart to the specialized reads struct.get
// fusion emits for a declared *types.StructType slot: the unfused STRUCT_GET
// handler calls it unconditionally, and a fused handler falls back to it when
// the runtime value does not match the slot it specialized for. A KindRef
// field is retained. structField does not release addr itself, for the same
// reason arrayGet does not.
func (i *Interpreter) structField(addr, at int) types.Boxed {
	switch value := i.heap[addr].(type) {
	case *types.Struct:
		if at < 0 || at >= len(value.Typ.Fields) {
			panic(ErrSegmentationFault)
		}
		data := value.Data[at]
		switch value.Typ.Fields[at].Kind {
		case types.KindI1:
			return types.BoxI1(data != 0)
		case types.KindI8:
			return types.BoxI8(int8(uint32(data)))
		case types.KindI32:
			return types.BoxI32(int32(uint32(data)))
		case types.KindI64:
			return i.boxI64(int64(data))
		case types.KindF32:
			return types.BoxF32(math.Float32frombits(uint32(data)))
		case types.KindF64:
			return types.BoxF64(math.Float64frombits(data))
		case types.KindRef:
			result := types.Boxed(data)
			i.retainBox(result)
			return result
		default:
			panic(ErrTypeMismatch)
		}
	case *HostStruct:
		// A host struct converts on the way out instead of holding VM words,
		// so a conversion that fails traps the way the threaded contract
		// expects rather than reporting inward.
		result, err := value.Field(i, at)
		if err != nil {
			panic(err)
		}
		return result
	default:
		panic(ErrTypeMismatch)
	}
}

// deref follows a value to the one it stands for, so a caller that accepts any
// VM value sees a standalone one however the source stored it. It is the
// borrowing counterpart of unbox: the heap value it reports stays owned by the
// slot that named it.
func (i *Interpreter) deref(val types.Value) (types.Value, error) {
	boxed, ok := val.(types.Boxed)
	if !ok {
		return val, nil
	}
	if boxed.Kind() != types.KindRef {
		return types.Unbox(boxed), nil
	}
	out, err := i.Load(boxed.Ref())
	if err != nil {
		return nil, fmt.Errorf("load ref %d: %w", boxed.Ref(), err)
	}
	return out, nil
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
	collected := i.target > 0 && len(i.heap)-len(i.free) >= i.target
	if collected {
		i.gc()
	}
	if addr, ok := i.reuse(val); ok {
		i.track(val)
		return addr
	}

	full := len(i.heap) == cap(i.heap)
	limited := i.limit > 0 && len(i.heap) >= i.limit
	if !collected && (full || limited) {
		i.gc()
		if addr, ok := i.reuse(val); ok {
			i.track(val)
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

		rc := make([]int, len(i.rc), c)
		copy(rc, i.rc)
		i.rc = rc
	}

	i.heap = append(i.heap, val)
	i.rc = append(i.rc, 1)
	i.track(val)
	return len(i.heap) - 1
}

func (i *Interpreter) track(v types.Value) {
	switch v := v.(type) {
	case *types.Struct:
		if len(v.Typ.Fields) <= 4 {
			i.structs.add()
		}
	case *types.Array:
		i.arrays.add()
	}
}

// newStruct reuses small struct objects retained by Reset. The pool is only
// for interpreter-owned heap values; larger structs keep their normal path.
func (i *Interpreter) newStruct(typ *types.StructType) *types.Struct {
	if len(typ.Fields) <= 4 {
		if s, ok := i.structs.get(); ok {
			s.Reset(typ)
			return s
		}
	}
	return types.NewStruct(typ)
}

// newArray reuses headers invalidated by Reset.
func (i *Interpreter) newArray(typ *types.ArrayType, elems []types.Boxed) *types.Array {
	if array, ok := i.arrays.get(); ok {
		*array = types.Array{Typ: typ, Elems: elems}
		return array
	}
	return &types.Array{Typ: typ, Elems: elems}
}

// newArraySized reuses the backing storage retained with a reset array header.
func (i *Interpreter) newArraySized(typ *types.ArrayType, size int) *types.Array {
	array, ok := i.arrays.get()
	if !ok {
		return &types.Array{Typ: typ, Elems: make([]types.Boxed, size)}
	}
	if cap(array.Elems) < size {
		array.Elems = make([]types.Boxed, size)
	} else {
		array.Elems = array.Elems[:size]
	}
	array.Typ = typ
	return array
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
	if addr >= len(i.entries) {
		i.entries = append(i.entries, make([]uint64, n-len(i.entries))...)
	}
	if addr >= len(i.cold) {
		i.cold = append(i.cold, make([]bool, n-len(i.cold))...)
	}
	if addr >= len(i.misses) {
		i.misses = append(i.misses, make([]uint8, n-len(i.misses))...)
	}
	if addr >= len(i.handlers) {
		i.handlers = append(i.handlers, make([][]instr.Handler, n-len(i.handlers))...)
	}
	if addr >= len(i.coros) {
		i.coros = append(i.coros, make([]bool, n-len(i.coros))...)
	}
	i.backedges[addr] = nativeBackend && i.threshold >= 0
	c := i.threader(i.backedges[addr])
	if dynamic {
		i.coros[addr] = i.yields(fn.Code)
	}
	i.instrs[addr] = fn.Code
	i.handlers[addr] = fn.Handlers
	i.code[addr] = c.Compile(fn.Code, fn.Slots(), fn.Declared(), types.Kinds(fn.Captures), fn.Captures)
	if dynamic {
		i.dynamic[addr] = true
	}
}

// globalDecls returns the declared kinds for threaded handler selection,
// unlike globalKinds which observes current values. Dynamic globals stay
// unknown so their handlers inspect each boxed value.
func (i *Interpreter) globalDecls() []types.Kind {
	kinds := make([]types.Kind, len(i.globalTypes))
	for idx, typ := range i.globalTypes {
		if typ == types.TypeAny {
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
		types:       i.types,
		constants:   i.constants,
		heap:        i.heap,
		coros:       i.coros,
		globals:     i.globalDecls(),
		globalTypes: i.globalTypes,
		exact:       i.tick == 1,
		entry:       (*Interpreter).entered,
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
	if v.Kind() == types.KindRef && v.Ref() != 0 {
		i.release(v.Ref())
	}
}

// owner returns the slot that already holds val, or -1 when val is unowned.
// Only a pointer value can be aliased into two slots; every other kind is
// copied into its slot, so unowned is the answer for them by construction.
//
// owners only hints at the answer. The heap is the authority, so a hint left
// behind by a freed or overwritten slot is discarded here instead of being
// tracked on the interpreter's allocation path.
func (i *Interpreter) owner(val types.Value) int {
	if !aliasable(val) {
		return -1
	}
	addr, ok := i.owners[val]
	if !ok || !i.holds(addr, val) {
		return -1
	}
	return addr
}

// own hints that addr holds val. Only the host boundary records hints: the VM
// never hands a value it allocated back to itself, so a pointer the host can
// present has always crossed Alloc, Store, Push, Load, Retain, or Pop first.
func (i *Interpreter) own(addr int, val types.Value) {
	if !aliasable(val) {
		return
	}
	// A hint survives only while its slot still holds it, so at most one per
	// occupied slot is live. Twice that many entries means half are stale, and
	// trimming then costs one pass per as many insertions as it discards.
	if len(i.owners) >= max(2*(len(i.heap)-len(i.free)), heapRunway) {
		i.trim()
	}
	i.owners[val] = addr
}

// trim drops every hint the heap no longer backs, keeping the index
// proportional to the values the host actually holds without charging hint
// removal to release and sweep.
func (i *Interpreter) trim() {
	for val, addr := range i.owners {
		if !i.holds(addr, val) {
			delete(i.owners, val)
		}
	}
}

// holds reports whether addr still stores val. The heap decides ownership, so
// every hint is confirmed against it before it is trusted or kept.
func (i *Interpreter) holds(addr int, val types.Value) bool {
	return i.alive(addr) && i.heap[addr] == val
}

// aliasable reports whether val is a pointer, the only shape two slots can
// share. A pointer is stored directly in the interface word and is comparable,
// so the interface itself keys the index.
func aliasable(val types.Value) bool {
	typ := reflect.TypeOf(val)
	return typ != nil && typ.Kind() == reflect.Pointer
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

// gc collects one cycle. Every pass walks the whole heap, so the recorded slot
// count is what the collection cost; the two metrics together report collector
// pressure without a build-time switch.
func (i *Interpreter) gc() {
	i.samples.AddMetric("vm_gc_cycles_total", 1)
	i.samples.AddMetric("vm_gc_slots_total", float64(len(i.heap)))
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
	var work []int
	push := func(addr int) {
		if addr <= 0 || addr >= len(i.rc) || i.rc[addr] <= 0 || i.trial[addr] < 0 {
			return
		}
		i.trial[addr] = -i.trial[addr] - 1
		work = append(work, addr)
	}

	for addr := 1; addr < len(i.rc); addr++ {
		if i.rc[addr] > 0 && i.trial[addr] > 0 {
			push(addr)
		}
	}
	for len(work) > 0 {
		addr := work[len(work)-1]
		work = work[:len(work)-1]
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

	base := len(i.work)
	i.work = append(i.work, addr)
	for len(i.work) > base {
		addr := i.work[len(i.work)-1]
		i.work = i.work[:len(i.work)-1]

		i.rc[addr]--
		if i.rc[addr] == 0 {
			v := i.heap[addr]
			for _, r := range i.refs(v) {
				i.work = append(i.work, int(r))
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
	switch v := v.(type) {
	case *types.Struct:
		if len(v.Typ.Fields) <= 4 {
			i.structs.remove()
			i.structs.put(v)
		}
	case *types.Array:
		i.arrays.remove()
	}
	i.heap[addr] = nil
	i.free = append(i.free, addr)
}

func (i *Interpreter) finalize(addr int, v types.Value) {
	if _, ok := v.(*types.Function); ok {
		i.remove(addr)
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
	i.cold[addr] = false
	i.misses[addr] = 0
	i.handlers[addr] = nil
	i.coros[addr] = false
	for a := range i.exits {
		if a.Addr == addr {
			delete(i.exits, a)
		}
	}
	for a := range i.tried {
		if a.Addr == addr {
			delete(i.tried, a)
		}
	}
	for a := range i.live {
		if a.Addr == addr {
			delete(i.live, a)
		}
	}
	for a := range i.watchdogs {
		if a.Addr == addr {
			delete(i.watchdogs, a)
		}
	}
	if addr >= 0 && addr < len(i.entries) {
		i.entries[addr] = 0
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
