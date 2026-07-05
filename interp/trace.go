package interp

import (
	"fmt"
	"sync"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Tracer is the shared JIT front-end: it records hot linear traces that the
// trace compiler consumes. One Tracer is shared across a pool so a trace
// recorded by one member compiles once and serves all.
type Tracer struct {
	mu        sync.Mutex
	precise   [][]func(*Interpreter)
	loops     map[int][]int
	trees     map[anchor]*tree
	blacklist map[anchor]bool
}

type anchor struct {
	addr int
	ip   int
}

type branch struct {
	fn int
	ip int
}

type leg struct {
	trace *trace
	hits  int64
}

type outcome int

type step struct {
	op   instr.Opcode
	seen types.Boxed
	arg  types.Boxed

	fn     int
	ip     int
	depth  int
	target int
	taken  bool
	callee int
}

type trace struct {
	anchor anchor
	ops    []step
	kind   outcome
}

type tree struct {
	root     *trace
	branches map[int]*trace
	hits     []int64
	exits    map[branch]int

	attempts int
}

const (
	linear outcome = iota
	loop
	returned
	completed
	aborted
)

const opLimit = 1024

const exitThreshold = 8

const attemptLimit = 8

func NewTracer() *Tracer {
	return &Tracer{
		loops:     map[int][]int{},
		trees:     map[anchor]*tree{},
		blacklist: map[anchor]bool{},
	}
}

func (r *Tracer) exit(i *Interpreter, root anchor, target branch) (int64, error) {
	r.mu.Lock()
	tree := r.tree(root)
	id := r.exitIndex(tree, target)
	tree.hits[id]++
	hits := tree.hits[id]
	if tree.branches[id] != nil {
		r.mu.Unlock()
		return hits, nil
	}
	r.mu.Unlock()

	t, err := r.capture(i, anchor{addr: target.fn, ip: target.ip})
	if err != nil {
		return hits, err
	}
	r.mu.Lock()
	tree.branches[id] = t
	r.mu.Unlock()
	return hits, nil
}

func (r *Tracer) capture(i *Interpreter, a anchor) (*trace, error) {
	r.mu.Lock()
	if r.blacklist[a] {
		r.mu.Unlock()
		return nil, nil
	}
	tree := r.tree(a)
	if tree.attempts >= attemptLimit {
		r.blacklist[a] = true
		r.mu.Unlock()
		return nil, nil
	}
	tree.attempts++
	r.mu.Unlock()

	if a.addr < 0 || a.addr >= len(i.instrs) || a.ip < 0 || a.ip >= len(i.instrs[a.addr]) {
		r.mu.Lock()
		r.blacklist[a] = true
		r.mu.Unlock()
		return nil, nil
	}

	clone := r.clone(i)
	clone.fr = &clone.frames[i.fp-1]
	clone.fr.ip = a.ip
	// An entry anchor replays the function from a clean frame: drop any operands
	// the sampling safepoint left mid-expression so the trace begins exactly as
	// a fresh call would, with only params and locals live.
	if a.ip == 0 && clone.fr.addr == a.addr {
		if fn, ok := clone.function(a.addr); ok {
			clone.sp = clone.fr.bp + len(fn.LocalKinds())
		}
	}

	t := &trace{anchor: a, kind: linear}
	startFP := clone.fp
	for len(t.ops) < opLimit {
		f := clone.fr
		if f.addr < 0 || f.addr >= len(clone.instrs) || f.ip < 0 || f.ip >= len(clone.instrs[f.addr]) {
			t.kind = aborted
			break
		}

		code := clone.instrs[f.addr]
		op := instr.Opcode(code[f.ip])
		if r.unrecordable(&clone, op) {
			t.kind = aborted
			break
		}

		st := r.op(&clone, op, startFP)
		if op == instr.CALL && r.recursive(&clone, a) {
			r.skipCall(&clone, a.addr)
			st.callee = a.addr
			t.ops = append(t.ops, st)
			continue
		}
		// A tail call back to the entry anchor closes the trace as a native loop
		// back-edge: record it as the entry trace's terminal op without stepping
		// into the reused frame, so it compiles like a loop without tripping the
		// ip-0 loop ban (the trace stays kind=returned).
		if op == instr.RETURN_CALL && a.ip == 0 && r.tailToAnchor(&clone, a) {
			st.callee = a.addr
			t.ops = append(t.ops, st)
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
		// YIELD/RESUME and exception-producing ops have side effects a linear
		// trace cannot represent. In the anchor frame, record the op as the
		// terminal and store kind=returned WITHOUT stepping the clone; the JIT
		// lowers this to an unconditional deopt so the threaded handler performs
		// the real work. Abort rather than miscompile when the op sits in an
		// inlined frame whose runtime-only state may not survive journal deopt.
		if op == instr.YIELD || op == instr.RESUME || op == instr.ERROR_NEW || op == instr.ERROR_CODE || op == instr.THROW {
			if clone.fp != startFP {
				t.kind = aborted
				break
			}
			t.ops = append(t.ops, st)
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
		if err := r.step(&clone, f.addr, f.ip); err != nil {
			t.kind = aborted
			break
		}

		r.finish(&clone, &st, op)
		t.ops = append(t.ops, st)
		// Heap mutations still lower as terminal native fast paths: the hot
		// path performs the store and resumes at the next threaded instruction;
		// guard failures resume at the opcode so the interpreter owns the full
		// handler semantics.
		if op == instr.ARRAY_SET || op == instr.STRUCT_SET {
			if clone.fp != startFP {
				t.kind = aborted
				break
			}
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
		if (op == instr.ARRAY_GET || op == instr.STRUCT_GET) && st.seen.Kind() == types.KindI64 {
			if clone.fp != startFP {
				t.kind = aborted
				break
			}
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
		switch {
		case op == instr.RETURN && st.depth == 0:
			t.kind = returned
			r.store(a, t)
			return t, nil
		case clone.fr.addr >= 0 && clone.fr.addr < len(clone.instrs) && clone.fr.ip >= len(clone.instrs[clone.fr.addr]):
			if clone.fr.addr == 0 {
				t.kind = completed
			}
			r.store(a, t)
			return t, nil
		case clone.fr.addr == a.addr && clone.fr.ip == a.ip:
			t.kind = loop
			r.store(a, t)
			return t, nil
		case clone.fp < startFP:
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
	}
	if len(t.ops) >= opLimit {
		t.kind = aborted
	}
	r.store(a, t)
	return t, nil
}

func (r *Tracer) clone(i *Interpreter) Interpreter {
	out := *i
	out.compiler = nil
	out.hook = nil
	out.threshold = -1

	out.constants = append([]types.Boxed(nil), i.constants...)
	out.globals = append([]types.Boxed(nil), i.globals...)
	out.instrs = append([][]byte(nil), i.instrs...)
	out.code = r.codes(i)
	out.fallbacks = map[anchor]func(*Interpreter){}
	out.journal = append([]uint64(nil), i.journal...)
	out.frames = append([]frame(nil), i.frames...)
	out.stack = append([]types.Boxed(nil), i.stack...)
	out.roots = append([]types.Boxed(nil), i.roots...)
	out.heap = r.heap(i.heap)
	out.free = append([]int(nil), i.free...)
	out.rc = append([]int(nil), i.rc...)
	out.interned = make(map[string]types.Ref, len(i.interned))
	for key, val := range i.interned {
		out.interned[key] = val
	}
	for idx := 0; idx < out.fp; idx++ {
		addr := out.frames[idx].addr
		if addr >= 0 && addr < len(out.code) {
			out.frames[idx].code = out.code[addr]
		}
		out.frames[idx].upvals = append([]types.Boxed(nil), out.frames[idx].upvals...)
	}
	return out
}

func (r *Tracer) heap(heap []types.Value) []types.Value {
	out := make([]types.Value, len(heap))
	for idx, val := range heap {
		switch v := val.(type) {
		case *types.Closure:
			clone := *v
			clone.Upvals = append([]types.Boxed(nil), v.Upvals...)
			out[idx] = &clone
		default:
			out[idx] = val
		}
	}
	return out
}

func (r *Tracer) codes(i *Interpreter) [][]func(*Interpreter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.precise) == len(i.instrs) {
		return r.precise
	}
	r.precise = make([][]func(*Interpreter), len(i.instrs))
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
		tc := &threadedCompiler{
			types:     i.types,
			constants: i.constants,
			heap:      i.heap,
			precise:   true,
		}
		r.precise[addr] = tc.Compile(code, locals, captures)
	}
	return r.precise
}

func (r *Tracer) op(i *Interpreter, op instr.Opcode, startFP int) step {
	f := i.fr
	st := step{
		op:    op,
		fn:    f.addr,
		ip:    f.ip,
		depth: i.fp - startFP,
	}
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
		instr.BR_TABLE,
		instr.ARRAY_GET,
		instr.STRUCT_GET:
		if i.sp > 0 {
			st.arg = i.stack[i.sp-1]
		}
	case instr.ARRAY_SET, instr.STRUCT_SET:
		if i.sp > 1 {
			st.arg = i.stack[i.sp-2]
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

func (r *Tracer) finish(i *Interpreter, st *step, op instr.Opcode) {
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

func (r *Tracer) recursive(i *Interpreter, a anchor) bool {
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

// tailToAnchor reports whether the next RETURN_CALL tail-calls the trace's own
// anchor function with a plain function ref on top of the stack. Such a tail
// call closes the trace as a native loop back-edge rather than stepping into a
// fresh callee body, so the recorder treats it as the entry trace's terminal.
func (r *Tracer) tailToAnchor(i *Interpreter, a anchor) bool {
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

func (r *Tracer) step(i *Interpreter, addr, ip int) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("trace aborted: %v", v)
		}
	}()
	i.code[addr][ip](i)
	return nil
}

func (r *Tracer) store(a anchor, t *trace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tr := r.tree(a)
	tr.root = t
}

func (r *Tracer) remove(addr int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for a := range r.trees {
		if a.addr == addr {
			delete(r.trees, a)
		}
	}
	for a := range r.blacklist {
		if a.addr == addr {
			delete(r.blacklist, a)
		}
	}
	delete(r.loops, addr)
}

func (r *Tracer) tree(a anchor) *tree {
	tr := r.trees[a]
	if tr == nil {
		tr = &tree{
			branches: map[int]*trace{},
			exits:    map[branch]int{},
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
		if anchor.addr == addr && tree.root != nil && tree.root.kind != aborted {
			out = append(out, anchor.ip)
		}
	}
	return out
}

// rootAt returns the recorded tree anchored exactly at a, or nil when none is
// recorded or its root aborted. The compiler emits one native per usable root.
func (r *Tracer) rootAt(a anchor) *tree {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.trees[a]
	if t == nil || t.root == nil || t.root.kind == aborted {
		return nil
	}
	return t.snapshot()
}

func (r *Tracer) hasEntry(addr int) bool {
	return r.hasLoop(addr, 0)
}

// hasLoop reports whether a non-aborted trace is already recorded at
// (addr, ip), so the trigger does not re-capture an existing root.
func (r *Tracer) hasLoop(addr, ip int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	tree := r.trees[anchor{addr: addr, ip: ip}]
	return tree != nil && tree.root != nil && tree.root.kind != aborted
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
			switch instr.Opcode(code[ip]) {
			case instr.BR, instr.BR_IF:
				target := ip + instr.ParseI16(code, ip+1) + 3
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

func (r *Tracer) exitIndex(tree *tree, target branch) int {
	if idx, ok := tree.exits[target]; ok {
		return idx
	}
	idx := len(tree.hits)
	tree.exits[target] = idx
	tree.hits = append(tree.hits, 0)
	return idx
}

func (r *Tracer) unrecordable(i *Interpreter, op instr.Opcode) bool {
	if (op == instr.CALL || op == instr.RETURN_CALL) && i.sp > 0 {
		if i.stack[i.sp-1].Kind() != types.KindRef {
			return false
		}
		addr := i.stack[i.sp-1].Ref()
		if addr < 0 || addr >= len(i.heap) {
			return false
		}
		if _, ok := i.heap[addr].(*HostFunction); ok {
			return true
		}
		// A tail call only lowers to a plain function target: the loop back-edge
		// and the in-place activation morph have no slot for closure upvals, so
		// closures deopt to the threaded tail() handler.
		if op == instr.RETURN_CALL {
			_, ok := i.heap[addr].(*types.Function)
			return !ok
		}
		return false
	}
	switch op {
	// YIELD and RESUME suspend or rebuild a frame, so a linear trace cannot
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
		return true
	}
	return false
}

// snapshot returns a compile-time-stable copy of the fields readers consume off
// a tree (root pointer, branches, hits). Published *trace values are immutable,
// so sharing the pointers is safe; copying the container lets the trace compiler
// lower a root without holding r.mu while the recorder keeps mutating the live
// tree under lock — the concurrent map read/write that races a pooled Tracer.
func (t *tree) snapshot() *tree {
	branches := make(map[int]*trace, len(t.branches))
	for id, tr := range t.branches {
		branches[id] = tr
	}
	return &tree{
		root:     t.root,
		branches: branches,
		hits:     append([]int64(nil), t.hits...),
	}
}

func (t *tree) branchIPs() map[branch]leg {
	out := map[branch]leg{}
	for id, tr := range t.branches {
		if tr != nil {
			out[branch{fn: tr.anchor.addr, ip: tr.anchor.ip}] = leg{trace: tr, hits: t.hits[id]}
		}
	}
	return out
}
