package interp

import (
	"reflect"
	"slices"
	"sort"
	"sync"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// tracer is the shared JIT front-end: it records hot traces that the trace
// compiler consumes. One tracer is shared across a pool so a trace
// recorded by one member compiles once and serves all.
type tracer struct {
	prog  *program.Program
	exact [][]func(*Interpreter)
	loops map[int][]int
	trees map[jit.Anchor]*jit.Tree

	recordMu sync.Mutex
	mu       sync.Mutex
}

type captureResult struct {
	trace   *jit.Trace
	outcome prof.CaptureOutcome
	reason  prof.CaptureReason
}

const opLimit = 1024

const exitThreshold = 8

const attemptLimit = 8

func newTracer() *tracer {
	return &tracer{
		loops: map[int][]int{},
		trees: map[jit.Anchor]*jit.Tree{},
	}
}

func (t *tracer) bind(prog *program.Program) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prog == nil {
		t.prog = prog
	}
	return t.prog == prog
}

func (t *tracer) branch(i *Interpreter, root jit.Anchor, target jit.Anchor) int64 {
	t.mu.Lock()
	tree := t.tree(root)
	id, ok := tree.Exits[target]
	if !ok {
		id = len(tree.Hits)
		tree.Exits[target] = id
		tree.Hits = append(tree.Hits, 0)
	}
	tree.Hits[id]++
	hits := tree.Hits[id]
	if branch := tree.Branches[id]; branch != nil {
		t.mu.Unlock()
		// The first hot exit publishes the standalone loop entry. Later exits
		// cannot be folded into the parent trace, so recompiling that parent
		// would only publish the same pair again.
		if branch.Status == jit.StatusLoop && hits != exitThreshold {
			return 0
		}
		return hits
	}
	t.mu.Unlock()

	result := t.capture(i, jit.Anchor{Addr: target.Addr, IP: target.IP})
	t.mu.Lock()
	if t.trees[root] == tree {
		tree.Branches[id] = result.trace
	}
	t.mu.Unlock()
	return hits
}

func (t *tracer) capture(i *Interpreter, a jit.Anchor) (result captureResult) {
	t.recordMu.Lock()
	defer t.recordMu.Unlock()
	defer func() {
		if i.profiler != nil && result.outcome != prof.CaptureOutcomeNone {
			i.samples.RecordCapture(a.Addr, a.IP, result.outcome, result.reason)
		}
	}()

	t.mu.Lock()
	tree := t.trees[a]
	if tree != nil && tree.Root != nil {
		tr := tree.Root
		t.mu.Unlock()
		return captureResult{trace: tr}
	}
	if tree == nil {
		tree = t.tree(a)
	}
	if tree.Attempts >= attemptLimit {
		t.mu.Unlock()
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonAttemptLimit}
	}
	// A mis-anchored entry (a.IP==0 but the live frame isn't there) counts
	// against tree.Attempts like every other rejection, so it stops being
	// retried after attemptLimit instead of costing a fresh clone-and-walk on
	// every observation forever.
	if a.IP == 0 && (i.fr == nil || i.fr.addr != a.Addr || i.fr.ip != 0) {
		tree.Attempts++
		t.mu.Unlock()
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonInvalidAnchor}
	}
	tree.Attempts++
	t.mu.Unlock()

	if a.Addr < 0 || a.Addr >= len(i.instrs) || a.IP < 0 || a.IP >= len(i.instrs[a.Addr]) {
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonInvalidAnchor}
	}

	clone := t.clone(i)
	clone.fr = &clone.frames[i.fp-1]
	clone.fr.ip = a.IP

	fn, _ := clone.function(a.Addr)
	carried := fn != nil && clone.sp > clone.fr.bp+len(fn.Slots())
	tr := &jit.Trace{Anchor: a, Carried: carried}
	startFP := clone.fp
	hasCall := false
	var cloned map[int]bool
	for len(tr.Ops) < opLimit {
		f := clone.fr
		if f.addr < 0 || f.addr >= len(clone.instrs) || f.ip < 0 || f.ip >= len(clone.instrs[f.addr]) {
			return t.publish(a, tree, tr, jit.StatusAborted, prof.CaptureReasonUnsupportedOp)
		}

		code := clone.instrs[f.addr]
		op := instr.Opcode(code[f.ip])
		if reason := t.reason(&clone, op); reason != prof.CaptureReasonNone {
			return t.publish(a, tree, tr, jit.StatusAborted, reason)
		}

		st := t.op(&clone, op, startFP)
		terminalMutation := false
		if op == instr.ARRAY_SET || op == instr.STRUCT_SET {
			if cloned == nil {
				cloned = map[int]bool{}
			}
			continuable := cloneTarget(&clone, cloned)
			terminalMutation = hasCall || !continuable
		}
		st.Terminal = terminalMutation
		if op == instr.CALL && t.callsAnchor(&clone, a) {
			if a.IP != 0 {
				st.Target = f.ip
				st.Cut = true
				tr.Ops = append(tr.Ops, st)
				return t.publish(a, tree, tr, jit.StatusPartial, prof.CaptureReasonNone)
			}
			t.skipCall(&clone, a.Addr)
			st.Callee = a.Addr
			tr.Ops = append(tr.Ops, st)
			hasCall = true
			continue
		}
		// A tail call back to the entry anchor closes the trace as a native loop
		// back-edge: record it as the entry trace's terminal op without stepping
		// into the reused frame, so it compiles like a loop without tripping the
		// ip-0 loop ban (the trace stays status=StatusReturned).
		if op == instr.RETURN_CALL && a.IP == 0 && t.callsAnchor(&clone, a) {
			st.Callee = a.Addr
			tr.Ops = append(tr.Ops, st)
			return t.publish(a, tree, tr, jit.StatusReturned, prof.CaptureReasonNone)
		}
		// YIELD/RESUME, exception-producing ops, and bulk mutations have side
		// effects a trace cannot represent. In the anchor frame, record the op
		// as the terminal and store status=StatusReturned WITHOUT stepping the
		// clone; the JIT lowers this to an unconditional deopt so the threaded
		// handler performs the real work, and the compiled prefix still runs
		// native. Abort rather than miscompile when the op sits in an inlined
		// frame whose runtime-only state may not survive journal deopt.
		switch op {
		case instr.YIELD, instr.RESUME, instr.ERROR_NEW, instr.ERROR_CODE, instr.THROW,
			instr.ARRAY_FILL, instr.ARRAY_COPY, instr.ARRAY_APPEND, instr.MAP_SET:
			if clone.fp != startFP {
				return t.publish(a, tree, tr, jit.StatusAborted, prof.CaptureReasonNestedTerminal)
			}
			tr.Ops = append(tr.Ops, st)
			return t.publish(a, tree, tr, jit.StatusReturned, prof.CaptureReasonNone)
		}
		if !t.step(&clone, f.addr, f.ip) {
			return t.publish(a, tree, tr, jit.StatusAborted, prof.CaptureReasonStepTrap)
		}

		t.finish(&clone, &st, op)
		tr.Ops = append(tr.Ops, st)
		if instr.IsCall(op) {
			hasCall = true
		}
		// A backward edge to a different header starts a distinct loop trace.
		// Stop this linear prefix at the header instead of unrolling the loop up
		// to opLimit; threaded execution will make that header hot and compile it
		// with the native back-edge and safepoint budget intact.
		if (op == instr.BR || op == instr.BR_IF) &&
			clone.fr.addr == st.Fn && clone.fr.ip <= st.IP &&
			(clone.fr.addr != a.Addr || clone.fr.ip != a.IP) {
			tr.Ops = append(tr.Ops, jit.Record{
				Step:   jit.Step{Fn: clone.fr.addr, Depth: clone.fp - startFP},
				Target: clone.fr.ip,
				Cut:    true,
			})
			return t.publish(a, tree, tr, jit.StatusPartial, prof.CaptureReasonNone)
		}
		// Boxed-array writes and ref-field struct writes remain terminal native
		// fast paths. Primitive array writes and scalar struct-field writes can
		// continue because their guarded store has no recursive release or
		// post-store deopt point.
		if terminalMutation {
			if clone.fp != startFP {
				return t.publish(a, tree, tr, jit.StatusAborted, prof.CaptureReasonNestedTerminal)
			}
			return t.publish(a, tree, tr, jit.StatusReturned, prof.CaptureReasonNone)
		}
		switch {
		case op == instr.RETURN && st.Depth == 0:
			return t.publish(a, tree, tr, jit.StatusReturned, prof.CaptureReasonNone)
		case clone.fr.addr >= 0 && clone.fr.addr < len(clone.instrs) && clone.fr.ip >= len(clone.instrs[clone.fr.addr]):
			if clone.fr.addr == 0 {
				return t.publish(a, tree, tr, jit.StatusCompleted, prof.CaptureReasonNone)
			}
			return t.publish(a, tree, tr, jit.StatusFallback, prof.CaptureReasonNone)
		case clone.fr.addr == a.Addr && clone.fr.ip == a.IP:
			return t.publish(a, tree, tr, jit.StatusLoop, prof.CaptureReasonNone)
		case clone.fp < startFP:
			return t.publish(a, tree, tr, jit.StatusReturned, prof.CaptureReasonNone)
		}
	}
	// Preserve the bounded prefix. Its synthetic cut lowers through the same
	// side-exit path as a guard, so a hot remainder becomes a continuation.
	f := clone.fr
	tr.Ops = append(tr.Ops, jit.Record{Step: jit.Step{Fn: f.addr, Depth: clone.fp - startFP}, Target: f.ip, Cut: true})
	return t.publish(a, tree, tr, jit.StatusPartial, prof.CaptureReasonOpLimit)
}

func (t *tracer) clone(i *Interpreter) Interpreter {
	out := *i
	out.compiler = nil
	out.cache = nil
	out.tracer = nil
	out.hook = nil
	out.speculative = true
	out.threshold = -1
	// A recording walk must not tier up. The exact tables it steps are threaded
	// without the entry hook, but out starts as a shallow copy, so entries still
	// aliases the live counters; dropping both the slice and the trigger keeps a
	// recorded call from ever writing them.
	out.trigger = 0
	out.entries = nil

	out.constants = i.constants
	out.globals = slices.Clone(i.globals)
	out.instrs = slices.Clone(i.instrs)
	out.code = slices.Clone(t.exactCodes(i))
	out.backedges = make([]bool, len(i.backedges))
	out.exits = map[jit.Anchor]func(*Interpreter){}
	out.stubs = make([]func(*Interpreter), len(out.code))
	out.tried = map[jit.Anchor]bool{}
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
	// Speculative capture must not extend the committed buffer: a later
	// committed append would rewrite bytes a captured string had published.
	out.tail = nil
	// Capture never serves a host ownership query, and every allocating opcode
	// is unrecordable, so the clone only needs a writable index of its own.
	out.owners = map[types.Value]int{}
	for idx := 0; idx < out.fp; idx++ {
		addr := out.frames[idx].addr
		if addr >= 0 && addr < len(out.code) {
			out.frames[idx].code = out.code[addr]
		}
		out.frames[idx].upvals = slices.Clone(out.frames[idx].upvals)
	}
	return out
}

// cloneTarget isolates the next mutation target and reports whether the store
// may continue inside the trace: a primitive typed-array element or a scalar
// struct field. Boxed-array stores and ref-field struct stores stay terminal
// because releasing the overwritten element may recurse.
func cloneTarget(i *Interpreter, cloned map[int]bool) bool {
	if i.sp < 3 || i.stack[i.sp-3].Kind() != types.KindRef {
		return false
	}
	addr := i.stack[i.sp-3].Ref()
	if addr <= 0 || addr >= len(i.heap) {
		return false
	}
	value := i.heap[addr]
	switch value := value.(type) {
	case types.TypedArray[bool]:
		if !cloned[addr] {
			cloneAliases(i.heap, value, cloned)
		}
		return true
	case types.TypedArray[int8]:
		if !cloned[addr] {
			cloneAliases(i.heap, value, cloned)
		}
		return true
	case types.TypedArray[int32]:
		if !cloned[addr] {
			cloneAliases(i.heap, value, cloned)
		}
		return true
	case types.TypedArray[int64]:
		if !cloned[addr] {
			cloneAliases(i.heap, value, cloned)
		}
		return true
	case types.TypedArray[float32]:
		if !cloned[addr] {
			cloneAliases(i.heap, value, cloned)
		}
		return true
	case types.TypedArray[float64]:
		if !cloned[addr] {
			cloneAliases(i.heap, value, cloned)
		}
		return true
	case *types.Array:
		if !cloned[addr] {
			clone := *value
			clone.Elems = slices.Clone(value.Elems)
			i.heap[addr] = &clone
			cloned[addr] = true
		}
	case *types.Struct:
		if !cloned[addr] {
			clone := *value
			clone.Data = slices.Clone(value.Data)
			i.heap[addr] = &clone
			cloned[addr] = true
		}
		return i.stack[i.sp-1].Kind() != types.KindRef
	}
	return false
}

// cloneAliases copies the connected component of typed-array ranges that
// overlap target. Address arithmetic only identifies overlap; each original
// slice copies its own visible range into the replacement backing store.
func cloneAliases[T bool | int8 | int32 | int64 | float32 | float64](heap []types.Value, target types.TypedArray[T], cloned map[int]bool) {
	if len(target) == 0 {
		return
	}
	var zero T
	size := unsafe.Sizeof(zero)
	start := uintptr(unsafe.Pointer(unsafe.SliceData(target)))
	end := start + uintptr(len(target))*size
	aliases := make([]struct {
		addr  int
		array types.TypedArray[T]
		start uintptr
		end   uintptr
	}, 0, len(heap))
	for addr, value := range heap {
		array, ok := value.(types.TypedArray[T])
		if !ok || len(array) == 0 {
			continue
		}
		aliasStart := uintptr(unsafe.Pointer(unsafe.SliceData(array)))
		aliases = append(aliases, struct {
			addr  int
			array types.TypedArray[T]
			start uintptr
			end   uintptr
		}{
			addr:  addr,
			array: array,
			start: aliasStart,
			end:   aliasStart + uintptr(len(array))*size,
		})
	}
	for {
		previousStart, previousEnd := start, end
		for _, alias := range aliases {
			if alias.start >= end || start >= alias.end {
				continue
			}
			start = min(start, alias.start)
			end = max(end, alias.end)
		}
		if start == previousStart && end == previousEnd {
			break
		}
	}
	// Recorded array operations can observe only the current lengths. Copy the
	// transitive union of overlapping visible ranges once, then rebuild each
	// slice at its original offset so speculative writes preserve aliasing.
	backing := make([]T, int((end-start)/size))
	for _, alias := range aliases {
		if alias.start >= end || start >= alias.end {
			continue
		}
		offset := int((alias.start - start) / size)
		copy(backing[offset:offset+len(alias.array)], alias.array)
	}
	for _, alias := range aliases {
		if alias.start >= end || start >= alias.end {
			continue
		}
		offset := int((alias.start - start) / size)
		heap[alias.addr] = types.TypedArray[T](backing[offset : offset+len(alias.array)])
		cloned[alias.addr] = true
	}
}

func (t *tracer) exactCodes(i *Interpreter) [][]func(*Interpreter) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.exact) == len(i.instrs) {
		return t.exact
	}
	globals := i.globalDecls()
	t.exact = make([][]func(*Interpreter), len(i.instrs))
	for addr, code := range i.instrs {
		if len(code) == 0 {
			continue
		}
		var locals []types.Kind
		var declared []types.Type
		var captures []types.Kind
		var captureTypes []types.Type
		if fn, ok := i.function(addr); ok {
			locals = fn.Slots()
			declared = fn.Declared()
			captures = types.Kinds(fn.Captures)
			captureTypes = fn.Captures
		}
		tc := &threader{
			types:       i.types,
			constants:   i.constants,
			heap:        i.heap,
			globals:     globals,
			globalTypes: i.globalTypes,
			exact:       true,
			entry:       (*Interpreter).entered,
		}
		t.exact[addr] = tc.Compile(code, locals, declared, captures, captureTypes)
	}
	return t.exact
}

func (t *tracer) op(i *Interpreter, op instr.Opcode, startFP int) jit.Record {
	f := i.fr
	st := jit.Record{Step: jit.Step{
		Op:    op,
		Args:  jit.Args(instr.Instruction(i.instrs[f.addr][f.ip:])),
		Fn:    f.addr,
		IP:    f.ip,
		Depth: i.fp - startFP,
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
			st.Arg = i.stack[i.sp-1]
		}
	case instr.ARRAY_LEN, instr.REF_GET, instr.ERROR_GET, instr.CORO_DONE, instr.CORO_VALUE:
		if i.sp > 0 {
			st.Arg = i.stack[i.sp-1]
			st.Shape = t.shape(i, i.stack[i.sp-1])
		}
	case instr.ARRAY_GET, instr.STRUCT_GET:
		if i.sp > 0 {
			st.Arg = i.stack[i.sp-1]
		}
		if i.sp > 1 {
			st.Shape = t.shape(i, i.stack[i.sp-2])
			if op == instr.STRUCT_GET {
				st.Shape.Field = t.field(i, i.stack[i.sp-2], st.Arg)
			}
		}
	case instr.ARRAY_SET, instr.STRUCT_SET:
		if i.sp > 2 {
			st.Arg = i.stack[i.sp-2]
			st.Shape = t.shape(i, i.stack[i.sp-3])
			if op == instr.STRUCT_SET {
				st.Shape.Field = t.field(i, i.stack[i.sp-3], st.Arg)
			}
		}
	case instr.BR, instr.BR_IF:
		st.Target = f.ip + instr.ParseI16(i.instrs[f.addr], f.ip+1) + 3
		if op == instr.BR_IF && i.sp > 0 {
			st.Arg = i.stack[i.sp-1]
		}
	case instr.CALL, instr.RETURN_CALL:
		if i.sp > 0 {
			st.Seen = i.stack[i.sp-1]
		}
	}
	return st
}

func (t *tracer) shape(i *Interpreter, v types.Boxed) jit.Shape {
	if v.Kind() != types.KindRef {
		return jit.Shape{}
	}
	addr := v.Ref()
	if addr < 0 || addr >= len(i.heap) {
		return jit.Shape{}
	}
	val := i.heap[addr]
	if val == nil {
		return jit.Shape{}
	}
	out := jit.Shape{Itab: jit.Itab(val)}
	if s, ok := val.(*types.Struct); ok && s.Typ != nil {
		out.Typ = uintptr(unsafe.Pointer(s.Typ))
	}
	return out
}

// field reports the Go kind the *HostStruct at container holds its at'th field
// in. A host field is read and written through that kind rather than through a
// VM word, so it is what a backend needs to pick a load and a store; see
// jit.Shape.Field. Anything else records reflect.Invalid, which has no row.
func (t *tracer) field(i *Interpreter, container, at types.Boxed) reflect.Kind {
	if container.Kind() != types.KindRef {
		return reflect.Invalid
	}
	addr := container.Ref()
	if addr < 0 || addr >= len(i.heap) {
		return reflect.Invalid
	}
	host, ok := i.heap[addr].(*HostStruct)
	if !ok {
		return reflect.Invalid
	}
	idx := int(at.I32())
	if idx < 0 || idx >= len(host.fields) {
		return reflect.Invalid
	}
	return host.fields[idx].conversion.kind
}

func (t *tracer) finish(i *Interpreter, st *jit.Record, op instr.Opcode) {
	switch op {
	case instr.BR_IF:
		if i.fr.addr == st.Fn && i.fr.ip == st.Target {
			st.Taken = true
		}
	case instr.BR_TABLE:
		st.Target = i.fr.ip
	case instr.CALL, instr.RETURN_CALL:
		st.Callee = i.fr.addr
	case instr.REF_GET, instr.ARRAY_GET, instr.STRUCT_GET, instr.CORO_VALUE, instr.ERROR_GET:
		if i.sp > 0 {
			st.Seen = i.stack[i.sp-1]
		}
	}
}

// callsAnchor reports whether the next call targets the trace anchor through a
// plain function reference on top of the stack.
func (t *tracer) callsAnchor(i *Interpreter, a jit.Anchor) bool {
	if i.sp == 0 || i.stack[i.sp-1].Kind() != types.KindRef {
		return false
	}
	addr := i.stack[i.sp-1].Ref()
	if addr != a.Addr || addr < 0 || addr >= len(i.heap) {
		return false
	}
	_, ok := i.heap[addr].(*types.Function)
	return ok
}

func (t *tracer) skipCall(i *Interpreter, addr int) {
	fn := i.heap[addr].(*types.Function)
	i.sp -= len(fn.Typ.Params) + 1
	for _, typ := range fn.Typ.Returns {
		i.stack[i.sp] = types.Zero(typ.Kind())
		i.sp++
	}
	i.fr.ip++
}

func (t *tracer) step(i *Interpreter, addr, ip int) (ok bool) {
	defer func() {
		ok = recover() == nil
	}()
	i.code[addr][ip](i)
	return true
}

func (t *tracer) publish(a jit.Anchor, tree *jit.Tree, tr *jit.Trace, next jit.Status, reason prof.CaptureReason) captureResult {
	tr.Status = next
	result := captureResult{trace: tr, reason: reason}
	switch next {
	case jit.StatusAborted:
		result.trace = nil
		result.outcome = prof.CaptureOutcomeRejected
		return result
	case jit.StatusPartial:
		result.outcome = prof.CaptureOutcomePartial
	case jit.StatusFallback, jit.StatusLoop, jit.StatusReturned, jit.StatusCompleted:
		result.outcome = prof.CaptureOutcomePublished
	default:
		result.trace = nil
		result.outcome = prof.CaptureOutcomeRejected
		return result
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trees[a] == tree {
		tree.Root = tr
	}
	return result
}

// forget drops one anchor's recorded trace so a later arrival records it again.
// The attempt count is deliberately kept, because that count is what bounds how
// many times one anchor may be re-recorded.
func (t *tracer) forget(a jit.Anchor) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tree := t.trees[a]; tree != nil {
		tree.Root = nil
	}
}

func (t *tracer) remove(addr int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for a := range t.trees {
		if a.Addr == addr {
			delete(t.trees, a)
		}
	}
	delete(t.loops, addr)
	t.exact = nil
}

func (t *tracer) tree(a jit.Anchor) *jit.Tree {
	tr := t.trees[a]
	if tr == nil {
		tr = &jit.Tree{
			Branches: map[int]*jit.Trace{},
			Exits:    map[jit.Anchor]int{},
		}
		t.trees[a] = tr
	}
	return tr
}

// Anchors reports the IPs, within addr, that carry a published recorded
// trace. It satisfies jit.RecordedTraces.
func (t *tracer) Anchors(addr int) []int {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]int, 0, len(t.trees))
	for anchor, tree := range t.trees {
		if anchor.Addr == addr && tree.Root != nil {
			out = append(out, anchor.IP)
		}
	}
	sort.Ints(out)
	return out
}

// RootAt returns the published tree anchored exactly at a, or nil when none
// is recorded. Published roots are always usable. It satisfies
// jit.RecordedTraces.
func (t *tracer) RootAt(a jit.Anchor) *jit.Tree {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.trees[a]
	if tr == nil || tr.Root == nil {
		return nil
	}
	return tr.Snapshot()
}

// headers returns the loop-header IPs of the function at addr: the targets of
// backward branches, where a hot loop re-enters. The scan is static and
// memoized per address.
func (t *tracer) headers(i *Interpreter, addr int) []int {
	t.mu.Lock()
	hs, ok := t.loops[addr]
	t.mu.Unlock()
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
	t.mu.Lock()
	defer t.mu.Unlock()
	if cached, ok := t.loops[addr]; ok {
		return cached
	}
	t.loops[addr] = hs
	return hs
}

func (t *tracer) reason(i *Interpreter, op instr.Opcode) prof.CaptureReason {
	if instr.IsCall(op) && i.sp > 0 {
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
		instr.ARRAY_DELETE,
		instr.ARRAY_SLICE,
		instr.STRUCT_NEW,
		instr.STRUCT_NEW_DEFAULT,
		instr.MAP_NEW,
		instr.MAP_NEW_DEFAULT,
		instr.MAP_DELETE,
		instr.MAP_CLEAR,
		instr.REF_NEW,
		instr.REF_SET,
		instr.CLOSURE_NEW:
		return prof.CaptureReasonUnsupportedOp
	}
	return prof.CaptureReasonNone
}
