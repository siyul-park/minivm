package interp

import (
	"math"
	"sort"
	"time"
	"unsafe"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// JIT handler contract: handlers in jit_<arch>.go must validate the stack and
// any other preconditions BEFORE mutating jitSeg state (ip, stack, params,
// facts) or emitting instructions. A handler that returns false must leave
// jitSeg unchanged so the surrounding segment can abort cleanly without a
// snapshot/restore loop.

type jitCompiler struct {
	assembler *asm.Assembler
	profile   *prof.Stats

	addr      int
	cutoff    int
	constants []types.Boxed
	globals   []types.Boxed
	heap      []types.Value
}

type jitPlan struct {
	code   []byte
	blocks []*analysis.BasicBlock
	hot    []*analysis.BasicBlock
	forced map[*analysis.BasicBlock]bool
	traces []jitTrace
}

type jitTrace struct {
	blocks []*analysis.BasicBlock
}

type jitRun struct {
	c    *jitCompiler
	a    *asm.Assembler
	code []byte

	labels  map[int]int
	entries map[int]jitEntry
	edges   []jitEdge
}

type jitEntry struct {
	obj    *asm.RelocObject
	label  int // negative = primary entry
	params []asm.PReg
}

type jitEdge struct {
	label    int
	fallback int
	target   int
	stack    []asm.VReg
}

type jitSeg struct {
	r *jitRun

	assembler *asm.Assembler
	code      []byte
	constants []types.Boxed
	labels    map[int]int

	start int
	end   int
	ip    int
	force bool

	stack   []asm.VReg
	params  []asm.VReg
	facts   map[int]types.Kind
	scratch []asm.PReg
	entries map[int]int
}

type jitOp func(*jitSeg) bool

const primaryEntry = -1

const (
	rStack = iota
	rHeap
	rGlobals
	rNext
)

var (
	arch        *asm.Arch
	jit         = [256]jitOp{}
	jitPrologue func(*jitSeg)
	jitEpilogue func(*jitSeg)
)

func init() {
	for i, fn := range jit {
		if fn != nil {
			continue
		}

		jit[i] = func(*jitSeg) bool {
			return false
		}
	}
}

func (c *jitCompiler) Compile(code []byte) []func(*Interpreter) {
	if arch == nil {
		return nil
	}

	start := time.Now()
	defer func() {
		c.profile.JITTime(time.Since(start))
	}()

	plan, ok := c.plan(code)
	if !ok {
		return nil
	}
	if len(plan.hot) == 0 {
		return make([]func(*Interpreter), len(code))
	}

	c.assembler.Reset()
	r := &jitRun{
		c:       c,
		a:       c.assembler,
		code:    plan.code,
		entries: make(map[int]jitEntry),
	}
	r.assign(plan.blocks)
	objs := r.compile(plan)
	if len(objs) == 0 {
		return nil
	}
	r.resolve()

	return c.link(code, objs, r.entries)
}

func (c *jitCompiler) plan(code []byte) (jitPlan, bool) {
	blocks := c.blocks(code)
	if len(blocks) == 0 {
		return jitPlan{}, false
	}

	hot, forced := c.hot(blocks)
	plan := jitPlan{
		code:   code,
		blocks: blocks,
		hot:    hot,
		forced: forced,
	}
	plan.traces = c.traces(plan)
	return plan, true
}

func (c *jitCompiler) link(code []byte, objs []*asm.RelocObject, entries map[int]jitEntry) []func(*Interpreter) {
	callers, err := c.assembler.Link(objs)
	if err != nil {
		c.profile.JITError()
		return nil
	}

	primaries := make(map[*asm.RelocObject]asm.Caller, len(callers))
	for i, caller := range callers {
		if caller == nil {
			continue
		}
		primaries[objs[i]] = caller
	}

	out := make([]func(*Interpreter), len(code))
	for ip, entry := range entries {
		caller := primaries[entry.obj]
		if entry.label >= 0 {
			caller, err = c.assembler.CallerAt(entry.obj, entry.label)
			if err != nil {
				c.profile.JITError()
				continue
			}
		}
		if caller == nil {
			continue
		}

		fn := c.closure(caller, entry.params, entry.obj.Sig)
		if fn == nil {
			continue
		}

		c.profile.JITLink()
		out[ip] = fn
	}
	return out
}

func (c *jitCompiler) blocks(code []byte) []*analysis.BasicBlock {
	m := pass.NewManager()
	if err := m.Register(analysis.NewBasicBlocksPass()); err != nil {
		return nil
	}
	if err := m.Run(&types.Function{Typ: &types.FunctionType{}, Code: code}); err != nil {
		return nil
	}

	var blocks []*analysis.BasicBlock
	if err := m.Load(&blocks); err != nil {
		return nil
	}
	return blocks
}

func (c *jitCompiler) hot(blocks []*analysis.BasicBlock) ([]*analysis.BasicBlock, map[*analysis.BasicBlock]bool) {
	heat := make(map[*analysis.BasicBlock]uint64, len(blocks))
	for _, b := range blocks {
		heat[b] = c.profile.Range(c.addr, b.Start, b.End)
	}

	forced := make(map[*analysis.BasicBlock]bool)
	for _, b := range blocks {
		if heat[b] == 0 {
			continue
		}

		for _, succ := range b.Succs {
			if succ < 0 || succ >= len(blocks) {
				continue
			}
			if heat[blocks[succ]] == 0 {
				forced[blocks[succ]] = true
			}
		}
	}

	out := make([]*analysis.BasicBlock, 0, len(blocks))
	for _, b := range blocks {
		if heat[b] > 0 || forced[b] {
			out = append(out, b)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		hi, hj := heat[out[i]], heat[out[j]]
		if hi != hj {
			return hi > hj
		}
		return out[i].Start < out[j].Start
	})

	return out, forced
}

func (c *jitCompiler) traces(plan jitPlan) []jitTrace {
	eligible := make(map[*analysis.BasicBlock]bool, len(plan.hot))
	for _, b := range plan.hot {
		eligible[b] = true
	}

	// adjacent reports whether b flows straight into its single successor: one
	// successor block that begins exactly where b ends.
	adjacent := func(b *analysis.BasicBlock) (*analysis.BasicBlock, bool) {
		if len(b.Succs) != 1 {
			return nil, false
		}
		next := plan.blocks[b.Succs[0]]
		return next, b.End == next.Start
	}

	used := make(map[*analysis.BasicBlock]bool, len(plan.hot))
	traces := make([]jitTrace, 0, len(plan.hot))
	appendTrace := func(first *analysis.BasicBlock) {
		trace := jitTrace{blocks: []*analysis.BasicBlock{first}}
		used[first] = true
		for current := first; ; {
			next, ok := adjacent(current)
			if !ok || used[next] || !eligible[next] {
				break
			}
			trace.blocks = append(trace.blocks, next)
			used[next] = true
			current = next
		}
		traces = append(traces, trace)
	}

	// absorbed reports whether an eligible block flows straight into b, in which
	// case b becomes a trace body rather than a head.
	absorbed := func(b *analysis.BasicBlock) bool {
		for _, p := range b.Preds {
			if p < 0 || p >= len(plan.blocks) {
				continue
			}
			prev := plan.blocks[p]
			if next, ok := adjacent(prev); ok && eligible[prev] && next == b {
				return true
			}
		}
		return false
	}

	for _, first := range plan.hot {
		if !used[first] && !absorbed(first) {
			appendTrace(first)
		}
	}
	for _, first := range plan.hot {
		if !used[first] {
			appendTrace(first)
		}
	}
	return traces
}

func (r *jitRun) assign(blocks []*analysis.BasicBlock) {
	r.labels = make(map[int]int, len(blocks))
	for _, b := range blocks {
		r.labels[b.Start] = r.a.NewLabel()
	}
}

func (r *jitRun) compile(plan jitPlan) []*asm.RelocObject {
	var objs []*asm.RelocObject

	for _, trace := range plan.traces {
		traceObjs := r.compileGroup(trace.blocks, plan.forced)
		if len(traceObjs) == 0 && len(trace.blocks) > 1 {
			for _, b := range trace.blocks {
				objs = append(objs, r.compileGroup([]*analysis.BasicBlock{b}, plan.forced)...)
			}
			continue
		}
		objs = append(objs, traceObjs...)
	}

	return objs
}

func (r *jitRun) compileGroup(blocks []*analysis.BasicBlock, forced map[*analysis.BasicBlock]bool) []*asm.RelocObject {
	start := blocks[0].Start
	end := blocks[len(blocks)-1].End

	var boundaries []int
	if len(blocks) > 1 {
		boundaries = make([]int, 0, len(blocks)-1)
		for _, block := range blocks[1:] {
			boundaries = append(boundaries, block.Start)
		}
	}

	forceAt := func(ip int) bool {
		for _, b := range blocks {
			if b.Start == ip {
				return forced[b]
			}
		}
		return false
	}

	var objs []*asm.RelocObject
	for next := start; next < end; {
		if _, exists := r.entries[next]; exists {
			next = r.next(next)
			continue
		}
		obj, after, stop := r.segment(next, end, forceAt(next), boundaries)
		if obj != nil {
			objs = append(objs, obj)
			r.record(obj, next, boundaries)
		}
		if stop {
			break
		}
		if after <= next {
			after = r.next(next)
			if after <= next {
				break
			}
		}
		next = after
	}
	return objs
}

func (r *jitRun) record(obj *asm.RelocObject, start int, boundaries []int) {
	r.entries[start] = jitEntry{obj: obj, label: primaryEntry, params: obj.Sig.Params}
	for _, ip := range boundaries {
		label := r.labels[ip]
		entry, ok := obj.Entries[label]
		if !ok {
			continue
		}
		r.entries[ip] = jitEntry{obj: obj, label: label, params: entry.Params}
	}
}

func (r *jitRun) segment(start, end int, force bool, boundaries []int) (*asm.RelocObject, int, bool) {
	entries := make(map[int]int, len(boundaries))
	for _, ip := range boundaries {
		if ip > start {
			entries[ip] = r.labels[ip]
		}
	}
	s := &jitSeg{
		r:         r,
		assembler: r.a,
		code:      r.code,
		constants: r.c.constants,
		labels:    r.labels,
		start:     start,
		end:       end,
		ip:        start,
		force:     force,
		facts:     make(map[int]types.Kind),
		entries:   entries,
	}

	s.scratch = s.scratch[:0]
	for range 4 {
		s.scratch = append(s.scratch, s.assembler.Scratch())
	}

	if jitPrologue != nil {
		jitPrologue(s)
	}
	if id, ok := s.labels[s.start]; ok {
		s.assembler.Bind(id)
	}

	return s.run()
}

func (r *jitRun) next(ip int) int {
	if ip < 0 || ip >= len(r.code) {
		return ip + 1
	}
	return ip + instr.Instruction(r.code[ip:]).Width()
}

func (r *jitRun) resolve() {
	invalid := make(map[int]bool)
	for _, edge := range r.edges {
		entry, ok := r.entries[edge.target]
		if ok && entry.label >= 0 && !asm.Compatibles(edge.stack, entry.params) {
			invalid[edge.target] = true
		}
	}
	for ip := range invalid {
		delete(r.entries, ip)
	}

	for _, edge := range r.edges {
		target := edge.fallback
		if entry, ok := r.entries[edge.target]; ok && asm.Compatibles(edge.stack, entry.params) {
			target = r.labels[edge.target]
		}
		r.a.Alias(edge.label, target)
	}
}

// accepts checks the top-N stack slots against expected (type, width) specs.
// specs[0] matches the topmost slot, specs[1] the next, etc. Slots below the
// current stack height are treated as future entry params and always accepted.
func (s *jitSeg) accepts(specs ...asm.PReg) bool {
	for i, expected := range specs {
		at := len(s.stack) - i - 1
		if at < 0 {
			continue
		}
		if !asm.Compatible(s.stack[at], expected) {
			return false
		}
	}
	return true
}

// Take pops the top of the eval stack if its type/width matches; if the
// stack is empty, a fresh VReg becomes a function-entry parameter and is
// prepended to the param list (the VM pushes args in reverse).
func (s *jitSeg) Take(typ asm.RegType, width asm.RegWidth) (asm.VReg, bool) {
	if len(s.stack) == 0 {
		r := s.assembler.NewVReg(typ, width)
		s.params = append([]asm.VReg{r}, s.params...)
		return r, true
	}

	r := s.stack[len(s.stack)-1]
	if r.Type() != typ || r.Width() != width {
		return asm.VReg{}, false
	}

	s.stack = s.stack[:len(s.stack)-1]
	return r, true
}

// Top peeks at the i-th element from the stack top (0 = topmost).
func (s *jitSeg) Top(i int) (asm.VReg, bool) {
	if i < 0 || i >= len(s.stack) {
		return asm.VReg{}, false
	}
	return s.stack[len(s.stack)-1-i], true
}

// Push appends to the eval stack.
func (s *jitSeg) Push(r asm.VReg) {
	s.stack = append(s.stack, r)
}

// Pop pops the top of the eval stack.
func (s *jitSeg) Pop() (asm.VReg, bool) {
	if len(s.stack) == 0 {
		return asm.VReg{}, false
	}
	r := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	return r, true
}

func (s *jitSeg) run() (*asm.RelocObject, int, bool) {
	count := 0

	for s.ip < s.end {
		label, internal := s.entries[s.ip]
		if internal {
			s.assembler.Entry(label, s.stack)
			s.facts = make(map[int]types.Kind)
		}
		prev := s.ip
		if !jit[s.code[s.ip]](s) {
			if internal {
				s.abort(false)
				return nil, s.r.next(prev), true
			}
			return s.partial(prev, count)
		}

		count++
		if instr.Opcode(s.code[prev]).IsBranch() {
			if ok, skip := s.can(s.ip, count); !ok {
				s.abort(skip)
				return nil, s.ip, true
			}
			return s.compile(s.ip, true)
		}
	}

	if ok, skip := s.can(s.ip, count); !ok {
		s.abort(skip)
		return nil, s.ip, false
	}

	if jitEpilogue != nil {
		jitEpilogue(s)
	}
	return s.compile(s.ip, false)
}

func (s *jitSeg) partial(fail, count int) (*asm.RelocObject, int, bool) {
	next := s.r.next(fail)
	if ok, skip := s.can(fail, count); !ok {
		s.abort(skip)
		return nil, next, false
	}

	s.end = fail
	if jitEpilogue != nil {
		jitEpilogue(s)
	}
	return s.compile(next, false)
}

func (s *jitSeg) can(end, count int) (bool, bool) {
	if count == 0 {
		return false, false
	}
	if !s.force && count < s.r.c.cutoff {
		return false, false
	}
	if !s.force && s.hot(s.start, end) == 0 {
		return false, true
	}
	return true, false
}

func (s *jitSeg) hot(start, end int) uint64 {
	return s.r.c.profile.Range(s.r.c.addr, start, end)
}

func (s *jitSeg) abort(skip bool) {
	s.assembler.Abort()
	if skip {
		s.r.c.profile.JITSkip()
	} else {
		s.r.c.profile.JITAbort()
	}
}

func (s *jitSeg) compile(next int, stop bool) (*asm.RelocObject, int, bool) {
	s.finalize()

	obj, err := s.assembler.Compile()
	if err != nil {
		s.assembler.Abort()
		s.r.c.profile.JITError()
		return nil, next, false
	}

	s.r.c.profile.JITEmit(obj.Chunk.Size())
	return obj, next, stop
}

// finalize pins discovered function-entry parameters to ABI slots and
// registers them as Site(0). The only place the assembler learns the VM's
// eval-stack-derived call convention.
func (s *jitSeg) finalize() {
	for i, v := range s.params {
		_ = s.assembler.Pin(v, asm.NewPReg(uint8(i), v.Type(), v.Width()))
	}
	if len(s.params) > 0 {
		s.assembler.Site(0, s.params)
	}
}

// pinReturn pins the current eval-stack regs to ABI return slots and marks
// the current instruction index as a return Site. Called from architecture-
// specific ret() helpers.
func (s *jitSeg) pinReturn() int {
	idx := s.assembler.Index()
	for i, v := range s.stack {
		_ = s.assembler.Pin(v, asm.NewPReg(uint8(i), v.Type(), v.Width()))
	}
	s.assembler.Site(idx, s.stack)
	return idx
}

func (s *jitSeg) edge(target int) (int, int) {
	label := s.assembler.NewLabel()
	fallback := s.assembler.NewLabel()
	s.r.edges = append(s.r.edges, jitEdge{
		label:    label,
		fallback: fallback,
		target:   target,
		stack:    append([]asm.VReg(nil), s.stack...),
	})
	return label, fallback
}

func (s *jitSeg) global(idx int) (int16, bool) {
	if idx < 0 || idx >= len(s.r.c.globals) {
		return 0, false
	}
	offset := int16(idx * 8)
	if int(offset) != idx*8 {
		return 0, false
	}
	return offset, true
}

func (s *jitSeg) local(idx int) (types.Type, bool) {
	c := s.r.c
	if c.addr <= 0 || c.addr >= len(c.heap) {
		return nil, false
	}

	fn, ok := c.heap[c.addr].(*types.Function)
	if !ok || fn.Typ == nil {
		return nil, false
	}

	if idx < len(fn.Typ.Params) {
		return fn.Typ.Params[idx], true
	}
	idx -= len(fn.Typ.Params)
	if idx >= len(fn.Locals) {
		return nil, false
	}
	return fn.Locals[idx], true
}

func (s *jitSeg) regKind(r asm.Reg) (types.Kind, bool) {
	return s.r.c.regKind(r)
}

func (c *jitCompiler) closure(fn asm.Caller, pregs []asm.PReg, sig *asm.Signature) func(*Interpreter) {
	if len(sig.Scratch) <= rNext {
		return nil
	}

	pkinds := make([]types.Kind, len(pregs))
	for i, r := range pregs {
		kind, ok := c.regKind(r)
		if !ok {
			return nil
		}
		pkinds[i] = kind
	}

	rregs := sig.Returns[0]
	rkinds := make([]types.Kind, len(rregs))
	for i, r := range rregs {
		kind, ok := c.regKind(r)
		if !ok {
			return nil
		}
		rkinds[i] = kind
	}

	params := make([]asm.Value, len(pregs))
	scratch := make([]uint64, len(sig.Scratch))

	return func(i *Interpreter) {
		base := i.sp - len(pregs)
		for j := range pregs {
			v := i.stack[base+j]
			switch pkinds[j] {
			case types.KindI32:
				params[j] = asm.I32(uint32(v.I32()))
			case types.KindI64:
				params[j] = asm.I64(uint64(i.unboxI64(v)))
			case types.KindF32:
				params[j] = asm.F32(math.Float32bits(v.F32()))
			case types.KindF64:
				params[j] = asm.F64(math.Float64bits(v.F64()))
			default:
				params[j] = asm.I64(uint64(v))
			}
		}

		for j := range scratch {
			scratch[j] = 0
		}
		c.scratch(i, scratch)

		rets, err := fn.Call(params, &scratch)
		if err != nil {
			panic(err)
		}

		for j, val := range rets {
			bits := val.Bits()
			var kind types.Kind
			if j < len(rkinds) {
				kind = rkinds[j]
			} else {
				kind = c.valueKind(val)
			}
			switch kind {
			case types.KindI32:
				i.stack[base+j] = types.BoxI32(int32(bits))
			case types.KindI64:
				i.stack[base+j] = i.boxI64(int64(bits))
			case types.KindF32:
				i.stack[base+j] = types.BoxF32(math.Float32frombits(uint32(bits)))
			case types.KindF64:
				i.stack[base+j] = types.BoxF64(math.Float64frombits(bits))
			default:
				i.stack[base+j] = i.box64(bits, kind)
			}
		}
		i.sp = base + len(rets)
		i.fr.ip = int(scratch[rNext])
	}
}

func (c *jitCompiler) scratch(i *Interpreter, scratch []uint64) {
	f := i.fr
	scratch[rStack] = uint64(uintptr(unsafe.Pointer(&i.stack[f.bp])))
	scratch[rHeap] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.heap))))
	scratch[rGlobals] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.globals))))
}

func (c *jitCompiler) regKind(r asm.Reg) (types.Kind, bool) {
	switch r.Type() {
	case asm.RegTypeFloat:
		if r.Width() == asm.Width32 {
			return types.KindF32, true
		}
		return types.KindF64, true
	case asm.RegTypeInt:
		if r.Width() == asm.Width32 {
			return types.KindI32, true
		}
		return types.KindI64, true
	default:
		return 0, false
	}
}

func (c *jitCompiler) valueKind(v asm.Value) types.Kind {
	switch {
	case v.RegType() == asm.RegTypeFloat && v.Width() == asm.Width64:
		return types.KindF64
	case v.RegType() == asm.RegTypeFloat:
		return types.KindF32
	case v.Width() == asm.Width64:
		return types.KindI64
	default:
		return types.KindI32
	}
}
