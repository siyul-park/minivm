package interp

import (
	"maps"
	"slices"
	"sort"
	"sync"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// Tracer is the shared JIT front-end: it records hot traces that the trace
// compiler consumes. One Tracer is shared across a pool so a trace
// recorded by one member compiles once and serves all.
type Tracer struct {
	prog  *program.Program
	exact [][]func(*Interpreter)
	loops map[int][]int
	trees map[anchor]*tree

	recordMu sync.Mutex
	mu       sync.Mutex
}

type iface struct {
	itab uintptr
	_    uintptr
}

type outcome int

type record struct {
	step
	cut    bool
	target int
	taken  bool
}

type trace struct {
	anchor anchor
	ops    []record
	kind   outcome
}

type captureResult struct {
	trace   *trace
	outcome prof.CaptureOutcome
	reason  prof.CaptureReason
}

type tree struct {
	root     *trace
	branches map[int]*trace
	hits     []int64
	exits    map[anchor]int

	attempts int
}

const (
	loop outcome = iota + 1
	returned
	completed
	partial
	aborted
)

const opLimit = 1024

const exitThreshold = 8

const attemptLimit = 8

func NewTracer() *Tracer {
	return &Tracer{
		loops: map[int][]int{},
		trees: map[anchor]*tree{},
	}
}

func (r *Tracer) bind(prog *program.Program) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.prog == nil {
		r.prog = prog
	}
	return r.prog == prog
}

func (r *Tracer) exit(i *Interpreter, root anchor, target anchor) (int64, error) {
	r.mu.Lock()
	tree := r.tree(root)
	id := r.exitIndex(tree, target)
	tree.hits[id]++
	hits := tree.hits[id]
	if branch := tree.branches[id]; branch != nil {
		r.mu.Unlock()
		// The first hot exit publishes the standalone loop entry. Later exits
		// cannot be folded into the parent trace, so recompiling that parent
		// would only publish the same pair again.
		if branch.kind == loop && hits != exitThreshold {
			return 0, nil
		}
		return hits, nil
	}
	r.mu.Unlock()

	result, err := r.capture(i, anchor{addr: target.addr, ip: target.ip})
	if err != nil {
		return hits, err
	}
	r.mu.Lock()
	if r.trees[root] == tree {
		tree.branches[id] = result.trace
	}
	r.mu.Unlock()
	return hits, nil
}

func (r *Tracer) capture(i *Interpreter, a anchor) (result captureResult, err error) {
	r.recordMu.Lock()
	defer r.recordMu.Unlock()
	defer func() {
		if i.profiler != nil && result.outcome != prof.CaptureOutcomeNone {
			i.samples.RecordCapture(a.addr, a.ip, result.outcome, result.reason)
		}
	}()

	r.mu.Lock()
	tree := r.trees[a]
	if tree != nil && tree.root != nil {
		t := tree.root
		r.mu.Unlock()
		return captureResult{trace: t}, nil
	}
	if a.ip == 0 && (i.fr == nil || i.fr.addr != a.addr || i.fr.ip != 0) {
		r.mu.Unlock()
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonInvalidAnchor}, nil
	}
	if tree == nil {
		tree = r.tree(a)
	}
	if tree.attempts >= attemptLimit {
		r.mu.Unlock()
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonAttemptLimit}, nil
	}
	tree.attempts++
	r.mu.Unlock()

	if a.addr < 0 || a.addr >= len(i.instrs) || a.ip < 0 || a.ip >= len(i.instrs[a.addr]) {
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonInvalidAnchor}, nil
	}

	clone := r.clone(i)
	clone.fr = &clone.frames[i.fp-1]
	clone.fr.ip = a.ip

	t := &trace{anchor: a}
	startFP := clone.fp
	hasCall := false
	var cloned map[int]bool
	for len(t.ops) < opLimit {
		f := clone.fr
		if f.addr < 0 || f.addr >= len(clone.instrs) || f.ip < 0 || f.ip >= len(clone.instrs[f.addr]) {
			t.kind = aborted
			break
		}

		code := clone.instrs[f.addr]
		op := instr.Opcode(code[f.ip])
		if reason := r.unrecordableReason(&clone, op); reason != prof.CaptureReasonNone {
			t.kind = aborted
			result.reason = reason
			break
		}

		st := r.op(&clone, op, startFP)
		terminalMutation := op == instr.STRUCT_SET || op == instr.ARRAY_SET && (hasCall || !primitiveArray(&clone))
		st.terminal = terminalMutation
		if op == instr.CALL && r.callsAnchor(&clone, a) {
			r.skipCall(&clone, a.addr)
			st.callee = a.addr
			t.ops = append(t.ops, st)
			hasCall = true
			continue
		}
		// A tail call back to the entry anchor closes the trace as a native loop
		// back-edge: record it as the entry trace's terminal op without stepping
		// into the reused frame, so it compiles like a loop without tripping the
		// ip-0 loop ban (the trace stays kind=returned).
		if op == instr.RETURN_CALL && a.ip == 0 && r.callsAnchor(&clone, a) {
			st.callee = a.addr
			t.ops = append(t.ops, st)
			t.kind = returned
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePublished}, nil
		}
		// YIELD/RESUME and exception-producing ops have side effects a trace
		// cannot represent. In the anchor frame, record the op as the
		// terminal and store kind=returned WITHOUT stepping the clone; the JIT
		// lowers this to an unconditional deopt so the threaded handler performs
		// the real work. Abort rather than miscompile when the op sits in an
		// inlined frame whose runtime-only state may not survive journal deopt.
		if op == instr.YIELD || op == instr.RESUME || op == instr.ERROR_NEW || op == instr.ERROR_CODE || op == instr.THROW {
			if clone.fp != startFP {
				t.kind = aborted
				result.reason = prof.CaptureReasonNestedTerminal
				break
			}
			t.ops = append(t.ops, st)
			t.kind = returned
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePublished}, nil
		}
		if op == instr.ARRAY_SET || op == instr.STRUCT_SET {
			if cloned == nil {
				cloned = map[int]bool{}
			}
			cloneMutation(&clone, cloned)
		}
		if !r.step(&clone, f.addr, f.ip) {
			t.kind = aborted
			result.reason = prof.CaptureReasonStepTrap
			break
		}

		r.finish(&clone, &st, op)
		t.ops = append(t.ops, st)
		if op == instr.CALL || op == instr.RETURN_CALL {
			hasCall = true
		}
		// A backward edge to a different header starts a distinct loop trace.
		// Stop this linear prefix at the header instead of unrolling the loop up
		// to opLimit; threaded execution will make that header hot and compile it
		// with the native back-edge and safepoint budget intact.
		if (op == instr.BR || op == instr.BR_IF) &&
			clone.fr.addr == st.fn && clone.fr.ip <= st.ip &&
			(clone.fr.addr != a.addr || clone.fr.ip != a.ip) {
			t.ops = append(t.ops, record{
				step:   step{fn: clone.fr.addr, depth: clone.fp - startFP},
				target: clone.fr.ip,
				cut:    true,
			})
			t.kind = partial
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePartial}, nil
		}
		// Ref-bearing array writes and struct writes remain terminal native fast
		// paths. Primitive array writes can continue because their guarded store
		// has no recursive release or post-store deopt point.
		if terminalMutation {
			if clone.fp != startFP {
				t.kind = aborted
				result.reason = prof.CaptureReasonNestedTerminal
				break
			}
			t.kind = returned
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePublished}, nil
		}
		switch {
		case op == instr.RETURN && st.depth == 0:
			t.kind = returned
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePublished}, nil
		case clone.fr.addr >= 0 && clone.fr.addr < len(clone.instrs) && clone.fr.ip >= len(clone.instrs[clone.fr.addr]):
			if clone.fr.addr == 0 {
				t.kind = completed
			}
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePublished}, nil
		case clone.fr.addr == a.addr && clone.fr.ip == a.ip:
			t.kind = loop
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePublished}, nil
		case clone.fp < startFP:
			t.kind = returned
			r.publish(a, tree, t)
			return captureResult{trace: t, outcome: prof.CaptureOutcomePublished}, nil
		}
	}
	if len(t.ops) >= opLimit {
		// Preserve the bounded prefix. Its synthetic cut lowers through the same
		// side-exit path as a guard, so a hot remainder becomes a continuation.
		f := clone.fr
		t.ops = append(t.ops, record{step: step{fn: f.addr, depth: clone.fp - startFP}, target: f.ip, cut: true})
		t.kind = partial
		result.reason = prof.CaptureReasonOpLimit
	}
	if t.kind == aborted {
		if result.reason == prof.CaptureReasonNone {
			result.reason = prof.CaptureReasonUnsupportedOp
		}
		result.outcome = prof.CaptureOutcomeRejected
		return result, nil
	}
	r.publish(a, tree, t)
	return captureResult{trace: t, outcome: prof.CaptureOutcomePartial, reason: result.reason}, nil
}

func (r *Tracer) clone(i *Interpreter) Interpreter {
	out := *i
	out.compiler = nil
	out.cache = nil
	out.tracer = nil
	out.hook = nil
	out.speculative = true
	out.threshold = -1

	out.constants = i.constants
	out.globals = slices.Clone(i.globals)
	out.instrs = slices.Clone(i.instrs)
	out.code = slices.Clone(r.exactCodes(i))
	out.exits = map[anchor]func(*Interpreter){}
	out.stubs = make([]func(*Interpreter), len(out.code))
	out.tried = map[anchor]bool{}
	out.warmup = map[anchor]uint8{}
	out.journal = slices.Clone(i.journal)
	out.coros = slices.Clone(i.coros)
	out.handlers = slices.Clone(i.handlers)
	out.dynamic = map[int]bool{}
	out.frames = slices.Clone(i.frames)
	out.stack = slices.Clone(i.stack)
	out.heap = slices.Clone(i.heap)
	out.free = slices.Clone(i.free)
	out.rc = slices.Clone(i.rc)
	out.trial = nil
	out.work = nil
	out.refbuf = nil
	out.interned = map[string]types.Ref{}
	for idx := 0; idx < out.fp; idx++ {
		addr := out.frames[idx].addr
		if addr >= 0 && addr < len(out.code) {
			out.frames[idx].code = out.code[addr]
		}
		out.frames[idx].upvals = slices.Clone(out.frames[idx].upvals)
	}
	return out
}

func cloneMutation(i *Interpreter, cloned map[int]bool) {
	addr, value, ok := mutationTarget(i)
	if !ok || cloned[addr] {
		return
	}
	switch value := value.(type) {
	case types.TypedArray[bool]:
		i.heap[addr] = slices.Clone(value)
	case types.TypedArray[int8]:
		i.heap[addr] = slices.Clone(value)
	case types.TypedArray[int32]:
		i.heap[addr] = slices.Clone(value)
	case types.TypedArray[int64]:
		i.heap[addr] = slices.Clone(value)
	case types.TypedArray[float32]:
		i.heap[addr] = slices.Clone(value)
	case types.TypedArray[float64]:
		i.heap[addr] = slices.Clone(value)
	case *types.Array:
		clone := *value
		clone.Elems = slices.Clone(value.Elems)
		i.heap[addr] = &clone
	case *types.Struct:
		clone := *value
		clone.Data = slices.Clone(value.Data)
		i.heap[addr] = &clone
	default:
		return
	}
	cloned[addr] = true
}

func mutationTarget(i *Interpreter) (int, types.Value, bool) {
	if i.sp < 3 || i.stack[i.sp-3].Kind() != types.KindRef {
		return 0, nil, false
	}
	addr := i.stack[i.sp-3].Ref()
	if addr <= 0 || addr >= len(i.heap) {
		return 0, nil, false
	}
	return addr, i.heap[addr], true
}

func primitiveArray(i *Interpreter) bool {
	_, value, ok := mutationTarget(i)
	if !ok {
		return false
	}
	switch value.(type) {
	case types.TypedArray[bool],
		types.TypedArray[int8],
		types.TypedArray[int32],
		types.TypedArray[int64],
		types.TypedArray[float32],
		types.TypedArray[float64]:
		return true
	default:
		return false
	}
}

func (r *Tracer) exactCodes(i *Interpreter) [][]func(*Interpreter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.exact) == len(i.instrs) {
		return r.exact
	}
	// The declared Program.Globals are out of scope here; New pre-seeds every
	// slot to the zero Boxed of its declared kind, so the runtime values carry
	// the declared kinds at all times.
	globals := make([]types.Kind, len(i.globals))
	for j, g := range i.globals {
		globals[j] = g.Kind()
	}
	r.exact = make([][]func(*Interpreter), len(i.instrs))
	for addr, code := range i.instrs {
		if len(code) == 0 {
			continue
		}
		var locals []types.Kind
		var captures []types.Kind
		if fn, ok := i.function(addr); ok {
			locals = fn.LocalKinds()
			captures = types.Kinds(fn.Captures)
		}
		tc := &threader{
			types:     i.types,
			constants: i.constants,
			heap:      i.heap,
			globals:   globals,
			exact:     true,
		}
		r.exact[addr] = tc.Compile(code, locals, captures)
	}
	return r.exact
}

func (r *Tracer) op(i *Interpreter, op instr.Opcode, startFP int) record {
	f := i.fr
	st := record{step: step{
		op:    op,
		args:  args(instr.Instruction(i.instrs[f.addr][f.ip:])),
		fn:    f.addr,
		ip:    f.ip,
		depth: i.fp - startFP,
	}}
	switch op {
	case instr.I32_DIV_S,
		instr.I32_DIV_U,
		instr.I32_REM_S,
		instr.I32_REM_U,
		instr.I32_SHL,
		instr.I32_SHR_S,
		instr.I32_SHR_U,
		instr.I64_DIV_S,
		instr.I64_DIV_U,
		instr.I64_REM_S,
		instr.I64_REM_U,
		instr.I64_SHL,
		instr.I64_SHR_S,
		instr.I64_SHR_U,
		instr.BR_TABLE:
		if i.sp > 0 {
			st.arg = i.stack[i.sp-1]
		}
	case instr.ARRAY_LEN, instr.REF_GET, instr.ERROR_GET, instr.CORO_DONE, instr.CORO_VALUE:
		if i.sp > 0 {
			st.arg = i.stack[i.sp-1]
			st.shape = r.shape(i, i.stack[i.sp-1])
		}
	case instr.ARRAY_GET, instr.STRUCT_GET:
		if i.sp > 0 {
			st.arg = i.stack[i.sp-1]
		}
		if i.sp > 1 {
			st.shape = r.shape(i, i.stack[i.sp-2])
		}
	case instr.ARRAY_SET, instr.STRUCT_SET:
		if i.sp > 2 {
			st.arg = i.stack[i.sp-2]
			st.shape = r.shape(i, i.stack[i.sp-3])
		}
	case instr.BR, instr.BR_IF:
		st.target = f.ip + instr.ParseI16(i.instrs[f.addr], f.ip+1) + 3
		if op == instr.BR_IF && i.sp > 0 {
			st.arg = i.stack[i.sp-1]
		}
	case instr.CALL, instr.RETURN_CALL:
		if i.sp > 0 {
			st.seen = i.stack[i.sp-1]
		}
	}
	return st
}

func (r *Tracer) shape(i *Interpreter, v types.Boxed) shape {
	if v.Kind() != types.KindRef {
		return shape{}
	}
	addr := v.Ref()
	if addr < 0 || addr >= len(i.heap) {
		return shape{}
	}
	val := i.heap[addr]
	if val == nil {
		return shape{}
	}
	out := shape{itab: itab(val)}
	if s, ok := val.(*types.Struct); ok && s.Typ != nil {
		out.typ = uintptr(unsafe.Pointer(s.Typ))
	}
	return out
}

func (r *Tracer) finish(i *Interpreter, st *record, op instr.Opcode) {
	switch op {
	case instr.BR_IF:
		if i.fr.addr == st.fn && i.fr.ip == st.target {
			st.taken = true
		}
	case instr.BR_TABLE:
		st.target = i.fr.ip
	case instr.CALL, instr.RETURN_CALL:
		st.callee = i.fr.addr
	case instr.REF_GET, instr.ARRAY_GET, instr.STRUCT_GET, instr.CORO_VALUE, instr.ERROR_GET:
		if i.sp > 0 {
			st.seen = i.stack[i.sp-1]
		}
	}
}

// callsAnchor reports whether the next call targets the trace anchor through a
// plain function reference on top of the stack.
func (r *Tracer) callsAnchor(i *Interpreter, a anchor) bool {
	if i.sp == 0 || i.stack[i.sp-1].Kind() != types.KindRef {
		return false
	}
	addr := i.stack[i.sp-1].Ref()
	if addr != a.addr || addr < 0 || addr >= len(i.heap) {
		return false
	}
	_, ok := i.heap[addr].(*types.Function)
	return ok
}

func (r *Tracer) skipCall(i *Interpreter, addr int) {
	fn := i.heap[addr].(*types.Function)
	i.sp -= len(fn.Typ.Params) + 1
	for _, typ := range fn.Typ.Returns {
		var val types.Boxed
		switch typ.Kind() {
		case types.KindI32:
			val = types.BoxI32(0)
		case types.KindI64:
			val = types.BoxI64(0)
		case types.KindF32:
			val = types.BoxF32(0)
		case types.KindF64:
			val = types.BoxF64(0)
		default:
			val = types.BoxedNull
		}
		i.stack[i.sp] = val
		i.sp++
	}
	i.fr.ip++
}

func (r *Tracer) step(i *Interpreter, addr, ip int) (ok bool) {
	defer func() {
		ok = recover() == nil
	}()
	i.code[addr][ip](i)
	return true
}

func (r *Tracer) publish(a anchor, tree *tree, t *trace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.trees[a] == tree {
		tree.root = t
	}
}

func (r *Tracer) remove(addr int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for a := range r.trees {
		if a.addr == addr {
			delete(r.trees, a)
		}
	}
	delete(r.loops, addr)
	r.exact = nil
}

func (r *Tracer) tree(a anchor) *tree {
	tr := r.trees[a]
	if tr == nil {
		tr = &tree{
			branches: map[int]*trace{},
			exits:    map[anchor]int{},
		}
		r.trees[a] = tr
	}
	return tr
}

func (r *Tracer) anchors(addr int) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int, 0, len(r.trees))
	for anchor, tree := range r.trees {
		if anchor.addr == addr && tree.root != nil {
			out = append(out, anchor.ip)
		}
	}
	sort.Ints(out)
	return out
}

// rootAt returns the published tree anchored exactly at a, or nil when none is
// recorded. Published roots are always usable.
func (r *Tracer) rootAt(a anchor) *tree {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.trees[a]
	if t == nil || t.root == nil {
		return nil
	}
	return t.snapshot()
}

// headers returns the loop-header IPs of the function at addr: the targets of
// backward branches, where a hot loop re-enters. The scan is static and
// memoized per address.
func (r *Tracer) headers(i *Interpreter, addr int) []int {
	r.mu.Lock()
	hs, ok := r.loops[addr]
	r.mu.Unlock()
	if ok {
		return hs
	}

	// Scan the bytecode without the lock: i.instrs is immutable program data, so
	// the scan reads no shared tracer state and never blocks a concurrent record.
	// Only the memo write below needs the lock.
	hs = nil
	if addr >= 0 && addr < len(i.instrs) {
		code := i.instrs[addr]
		seen := map[int]bool{}
		for ip := 0; ip < len(code); {
			w := instr.Instruction(code[ip:]).Width()
			if w <= 0 {
				break
			}
			// instr.Targets covers BR_TABLE's multiple case targets as well as
			// BR/BR_IF's single target, so a loop formed only through a
			// backward BR_TABLE case is recognized as a header too.
			for _, target := range instr.Targets(code, ip) {
				if target >= 0 && target < ip && !seen[target] {
					seen[target] = true
					hs = append(hs, target)
				}
			}
			ip += w
		}
	}

	// Double-check: a peer may have memoized the same addr while we scanned. The
	// scan is deterministic, so return the stored slice for identity stability.
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.loops[addr]; ok {
		return cached
	}
	r.loops[addr] = hs
	return hs
}

func (r *Tracer) exitIndex(tree *tree, target anchor) int {
	if idx, ok := tree.exits[target]; ok {
		return idx
	}
	idx := len(tree.hits)
	tree.exits[target] = idx
	tree.hits = append(tree.hits, 0)
	return idx
}

func (r *Tracer) unrecordableReason(i *Interpreter, op instr.Opcode) prof.CaptureReason {
	if (op == instr.CALL || op == instr.RETURN_CALL) && i.sp > 0 {
		if i.stack[i.sp-1].Kind() != types.KindRef {
			return prof.CaptureReasonNone
		}
		addr := i.stack[i.sp-1].Ref()
		if addr < 0 || addr >= len(i.heap) {
			return prof.CaptureReasonNone
		}
		if _, ok := i.heap[addr].(*HostFunction); ok {
			return prof.CaptureReasonHostCall
		}
		// A tail call only lowers to a plain function target: the loop back-edge
		// and the in-place activation morph have no slot for closure upvals, so
		// closures deopt to the threaded tail() handler.
		if op == instr.RETURN_CALL {
			_, ok := i.heap[addr].(*types.Function)
			if !ok {
				return prof.CaptureReasonTailClosure
			}
			return prof.CaptureReasonNone
		}
		return prof.CaptureReasonNone
	}
	switch op {
	// YIELD and RESUME suspend or rebuild a frame, so a trace cannot
	// span them; capture records them as terminal deopt boundaries instead of
	// aborting, and the JIT lowers each to an unconditional deopt that hands the
	// real suspend/resume back to the threaded handler. CORO_DONE and
	// CORO_VALUE are pure heap reads (handle in, value out) and stay recordable
	// like ARRAY_GET and STRUCT_GET; the JIT lowers them directly.
	case instr.STRING_NEW_UTF32,
		instr.ARRAY_NEW,
		instr.ARRAY_NEW_DEFAULT,
		instr.ARRAY_FILL,
		instr.ARRAY_COPY,
		instr.ARRAY_APPEND,
		instr.ARRAY_DELETE,
		instr.ARRAY_SLICE,
		instr.STRUCT_NEW,
		instr.STRUCT_NEW_DEFAULT,
		instr.MAP_NEW,
		instr.MAP_NEW_DEFAULT,
		instr.MAP_SET,
		instr.MAP_DELETE,
		instr.MAP_CLEAR,
		instr.REF_NEW,
		instr.REF_SET,
		instr.CLOSURE_NEW:
		return prof.CaptureReasonUnsupportedOp
	}
	return prof.CaptureReasonNone
}

// snapshot returns a compile-time-stable copy of the fields readers consume off
// a tree (root pointer, branches, hits). Published *trace values are immutable,
// so sharing the pointers is safe; copying the container lets the trace compiler
// lower a root without holding r.mu while the recorder keeps mutating the live
// tree under lock — the concurrent map read/write that races a pooled Tracer.
func (t *tree) snapshot() *tree {
	return &tree{
		root:     t.root,
		branches: maps.Clone(t.branches),
		hits:     slices.Clone(t.hits),
	}
}

func itab(v types.Value) uintptr {
	return (*iface)(unsafe.Pointer(&v)).itab
}
