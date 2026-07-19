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

type status int

type record struct {
	step
	cut    bool
	target int
	taken  bool
}

type trace struct {
	anchor  anchor
	ops     []record
	status  status
	carried bool
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
	loop status = iota + 1
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

func (t *Tracer) bind(prog *program.Program) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prog == nil {
		t.prog = prog
	}
	return t.prog == prog
}

func (t *Tracer) branch(i *Interpreter, root anchor, target anchor) int64 {
	t.mu.Lock()
	tree := t.tree(root)
	id, ok := tree.exits[target]
	if !ok {
		id = len(tree.hits)
		tree.exits[target] = id
		tree.hits = append(tree.hits, 0)
	}
	tree.hits[id]++
	hits := tree.hits[id]
	if branch := tree.branches[id]; branch != nil {
		t.mu.Unlock()
		// The first hot exit publishes the standalone loop entry. Later exits
		// cannot be folded into the parent trace, so recompiling that parent
		// would only publish the same pair again.
		if branch.status == loop && hits != exitThreshold {
			return 0
		}
		return hits
	}
	t.mu.Unlock()

	result := t.capture(i, anchor{addr: target.addr, ip: target.ip})
	t.mu.Lock()
	if t.trees[root] == tree {
		tree.branches[id] = result.trace
	}
	t.mu.Unlock()
	return hits
}

func (t *Tracer) capture(i *Interpreter, a anchor) (result captureResult) {
	t.recordMu.Lock()
	defer t.recordMu.Unlock()
	defer func() {
		if i.profiler != nil && result.outcome != prof.CaptureOutcomeNone {
			i.samples.RecordCapture(a.addr, a.ip, result.outcome, result.reason)
		}
	}()

	t.mu.Lock()
	tree := t.trees[a]
	if tree != nil && tree.root != nil {
		tr := tree.root
		t.mu.Unlock()
		return captureResult{trace: tr}
	}
	if a.ip == 0 && (i.fr == nil || i.fr.addr != a.addr || i.fr.ip != 0) {
		t.mu.Unlock()
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonInvalidAnchor}
	}
	if tree == nil {
		tree = t.tree(a)
	}
	if tree.attempts >= attemptLimit {
		t.mu.Unlock()
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonAttemptLimit}
	}
	tree.attempts++
	t.mu.Unlock()

	if a.addr < 0 || a.addr >= len(i.instrs) || a.ip < 0 || a.ip >= len(i.instrs[a.addr]) {
		return captureResult{outcome: prof.CaptureOutcomeRejected, reason: prof.CaptureReasonInvalidAnchor}
	}

	clone := t.clone(i)
	clone.fr = &clone.frames[i.fp-1]
	clone.fr.ip = a.ip

	fn, _ := clone.function(a.addr)
	carried := fn != nil && clone.sp > clone.fr.bp+len(fn.Slots())
	tr := &trace{anchor: a, carried: carried}
	startFP := clone.fp
	hasCall := false
	var cloned map[int]bool
	for len(tr.ops) < opLimit {
		f := clone.fr
		if f.addr < 0 || f.addr >= len(clone.instrs) || f.ip < 0 || f.ip >= len(clone.instrs[f.addr]) {
			tr.status = aborted
			break
		}

		code := clone.instrs[f.addr]
		op := instr.Opcode(code[f.ip])
		if reason := t.unrecordableReason(&clone, op); reason != prof.CaptureReasonNone {
			tr.status = aborted
			result.reason = reason
			break
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
		st.terminal = terminalMutation
		if op == instr.CALL && t.callsAnchor(&clone, a) {
			t.skipCall(&clone, a.addr)
			st.callee = a.addr
			tr.ops = append(tr.ops, st)
			hasCall = true
			continue
		}
		// A tail call back to the entry anchor closes the trace as a native loop
		// back-edge: record it as the entry trace's terminal op without stepping
		// into the reused frame, so it compiles like a loop without tripping the
		// ip-0 loop ban (the trace stays kind=returned).
		if op == instr.RETURN_CALL && a.ip == 0 && t.callsAnchor(&clone, a) {
			st.callee = a.addr
			tr.ops = append(tr.ops, st)
			tr.status = returned
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePublished}
		}
		// YIELD/RESUME, exception-producing ops, and bulk mutations have side
		// effects a trace cannot represent. In the anchor frame, record the op
		// as the terminal and store kind=returned WITHOUT stepping the clone;
		// the JIT lowers this to an unconditional deopt so the threaded handler
		// performs the real work, and the compiled prefix still runs native.
		// Abort rather than miscompile when the op sits in an inlined frame
		// whose runtime-only state may not survive journal deopt.
		if op == instr.YIELD || op == instr.RESUME || op == instr.ERROR_NEW || op == instr.ERROR_CODE || op == instr.THROW ||
			op == instr.ARRAY_FILL || op == instr.ARRAY_COPY || op == instr.ARRAY_APPEND || op == instr.MAP_SET {
			if clone.fp != startFP {
				tr.status = aborted
				result.reason = prof.CaptureReasonNestedTerminal
				break
			}
			tr.ops = append(tr.ops, st)
			tr.status = returned
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePublished}
		}
		if !t.step(&clone, f.addr, f.ip) {
			tr.status = aborted
			result.reason = prof.CaptureReasonStepTrap
			break
		}

		t.finish(&clone, &st, op)
		tr.ops = append(tr.ops, st)
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
			tr.ops = append(tr.ops, record{
				step:   step{fn: clone.fr.addr, depth: clone.fp - startFP},
				target: clone.fr.ip,
				cut:    true,
			})
			tr.status = partial
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePartial}
		}
		// Boxed-array writes and ref-field struct writes remain terminal native
		// fast paths. Primitive array writes and scalar struct-field writes can
		// continue because their guarded store has no recursive release or
		// post-store deopt point.
		if terminalMutation {
			if clone.fp != startFP {
				tr.status = aborted
				result.reason = prof.CaptureReasonNestedTerminal
				break
			}
			tr.status = returned
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePublished}
		}
		switch {
		case op == instr.RETURN && st.depth == 0:
			tr.status = returned
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePublished}
		case clone.fr.addr >= 0 && clone.fr.addr < len(clone.instrs) && clone.fr.ip >= len(clone.instrs[clone.fr.addr]):
			if clone.fr.addr == 0 {
				tr.status = completed
			}
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePublished}
		case clone.fr.addr == a.addr && clone.fr.ip == a.ip:
			tr.status = loop
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePublished}
		case clone.fp < startFP:
			tr.status = returned
			t.publish(a, tree, tr)
			return captureResult{trace: tr, outcome: prof.CaptureOutcomePublished}
		}
	}
	if len(tr.ops) >= opLimit {
		// Preserve the bounded prefix. Its synthetic cut lowers through the same
		// side-exit path as a guard, so a hot remainder becomes a continuation.
		f := clone.fr
		tr.ops = append(tr.ops, record{step: step{fn: f.addr, depth: clone.fp - startFP}, target: f.ip, cut: true})
		tr.status = partial
		result.reason = prof.CaptureReasonOpLimit
	}
	if tr.status == aborted {
		if result.reason == prof.CaptureReasonNone {
			result.reason = prof.CaptureReasonUnsupportedOp
		}
		result.outcome = prof.CaptureOutcomeRejected
		return result
	}
	t.publish(a, tree, tr)
	return captureResult{trace: tr, outcome: prof.CaptureOutcomePartial, reason: result.reason}
}

func (t *Tracer) clone(i *Interpreter) Interpreter {
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
	out.code = slices.Clone(t.exactCodes(i))
	out.backedges = make([]bool, len(i.backedges))
	out.exits = map[anchor]func(*Interpreter){}
	out.stubs = make([]func(*Interpreter), len(out.code))
	out.tried = map[anchor]bool{}
	out.loopHits = map[anchor]uint8{}
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

func (t *Tracer) exactCodes(i *Interpreter) [][]func(*Interpreter) {
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
		var captures []types.Kind
		if fn, ok := i.function(addr); ok {
			locals = fn.Slots()
			captures = types.Kinds(fn.Captures)
		}
		tc := &threader{
			types:     i.types,
			constants: i.constants,
			heap:      i.heap,
			globals:   globals,
			exact:     true,
		}
		t.exact[addr] = tc.Compile(code, locals, captures)
	}
	return t.exact
}

func (t *Tracer) op(i *Interpreter, op instr.Opcode, startFP int) record {
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
			st.shape = t.shape(i, i.stack[i.sp-1])
		}
	case instr.ARRAY_GET, instr.STRUCT_GET:
		if i.sp > 0 {
			st.arg = i.stack[i.sp-1]
		}
		if i.sp > 1 {
			st.shape = t.shape(i, i.stack[i.sp-2])
		}
	case instr.ARRAY_SET, instr.STRUCT_SET:
		if i.sp > 2 {
			st.arg = i.stack[i.sp-2]
			st.shape = t.shape(i, i.stack[i.sp-3])
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

func (t *Tracer) shape(i *Interpreter, v types.Boxed) shape {
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

func (t *Tracer) finish(i *Interpreter, st *record, op instr.Opcode) {
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
func (t *Tracer) callsAnchor(i *Interpreter, a anchor) bool {
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

func (t *Tracer) skipCall(i *Interpreter, addr int) {
	fn := i.heap[addr].(*types.Function)
	i.sp -= len(fn.Typ.Params) + 1
	for _, typ := range fn.Typ.Returns {
		i.stack[i.sp] = types.Zero(typ.Kind())
		i.sp++
	}
	i.fr.ip++
}

func (t *Tracer) step(i *Interpreter, addr, ip int) (ok bool) {
	defer func() {
		ok = recover() == nil
	}()
	i.code[addr][ip](i)
	return true
}

func (t *Tracer) publish(a anchor, tree *tree, tr *trace) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trees[a] == tree {
		tree.root = tr
	}
}

func (t *Tracer) remove(addr int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for a := range t.trees {
		if a.addr == addr {
			delete(t.trees, a)
		}
	}
	delete(t.loops, addr)
	t.exact = nil
}

func (t *Tracer) tree(a anchor) *tree {
	tr := t.trees[a]
	if tr == nil {
		tr = &tree{
			branches: map[int]*trace{},
			exits:    map[anchor]int{},
		}
		t.trees[a] = tr
	}
	return tr
}

func (t *Tracer) anchors(addr int) []int {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]int, 0, len(t.trees))
	for anchor, tree := range t.trees {
		if anchor.addr == addr && tree.root != nil {
			out = append(out, anchor.ip)
		}
	}
	sort.Ints(out)
	return out
}

// rootAt returns the published tree anchored exactly at a, or nil when none is
// recorded. Published roots are always usable.
func (t *Tracer) rootAt(a anchor) *tree {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.trees[a]
	if tr == nil || tr.root == nil {
		return nil
	}
	return tr.snapshot()
}

// headers returns the loop-header IPs of the function at addr: the targets of
// backward branches, where a hot loop re-enters. The scan is static and
// memoized per address.
func (t *Tracer) headers(i *Interpreter, addr int) []int {
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

func (t *Tracer) unrecordableReason(i *Interpreter, op instr.Opcode) prof.CaptureReason {
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

// snapshot returns a compile-time-stable copy of the fields readers consume off
// a tree (root pointer, branches, hits). Published *trace values are immutable,
// so sharing the pointers is safe; copying the container lets the trace compiler
// lower a root without holding t.mu while the recorder keeps mutating the live
// tree under lock — the concurrent map read/write that races a pooled Tracer.
func (tr *tree) snapshot() *tree {
	return &tree{
		root:     tr.root,
		branches: maps.Clone(tr.branches),
		hits:     slices.Clone(tr.hits),
	}
}

func itab(v types.Value) uintptr {
	return (*iface)(unsafe.Pointer(&v)).itab
}
