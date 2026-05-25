package interp

import (
	"math"
	"slices"
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

type jitCompiler struct {
	assembler *asm.Assembler
	profile   *prof.Stats

	addr      int
	cutoff    int
	constants []types.Boxed
	globals   []types.Boxed
	heap      []types.Value
	entries   map[int]*jitEntry
	ip        *Interpreter
	// entryReturned mirrors jitRun.entryReturned for the emit pass.
	// link consults this to gate jitEntry registration: only function
	// entries whose entry segment natively pops its frame are
	// call-target safe.
	entryReturned bool
}

// jitEntry records a JIT-compiled function entry that is callable
// natively from another JIT segment. Populated by Interpreter.jit
// after a successful function-entry segment compile (segment start
// IP == 0). chunk.Ptr() is the absolute branch target; params/locals/
// rets mirror *types.Function metadata for use by CALL/RETURN handlers.
type jitEntry struct {
	chunk  *asm.Chunk
	params []types.Kind
	locals []types.Kind
	rets   []types.Kind
}

type jitPlan struct {
	code   []byte
	blocks []*analysis.BasicBlock
	hot    []*analysis.BasicBlock
	forced map[*analysis.BasicBlock]bool
}

type jitRun struct {
	c    *jitCompiler
	a    *asm.Assembler
	code []byte

	labels map[int]int
	sigs   map[int]*asm.Signature
	scan   bool
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
	// pendingFuncRef carries the heap addr of a *types.Function set by
	// CONST_GET when its successor opcode is CALL. The value is fused
	// CONST_GET+CALL metadata: no VReg is pushed and no native code is
	// emitted for the CONST_GET itself, so a segment exit between the
	// two opcodes cannot leave a half-formed ref on the eval stack.
	// CALL consumes the field and resets it to 0.
	pendingFuncRef int
}

const (
	rStack = iota
	rHeap
	rGlobals
	rNext
	rInterp
	rScratchCount
)

type jitOp func(*jitSeg) (ok bool, stop bool)

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

		jit[i] = func(s *jitSeg) (bool, bool) {
			inst := instr.Instruction(s.code[s.ip:])
			s.ip += inst.Width()
			return false, false
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

	sigs, ok := c.scan(plan)
	if !ok {
		return nil
	}

	objs, entries := c.emit(plan, sigs)
	if len(objs) == 0 {
		return nil
	}

	return c.link(code, objs, entries)
}

func (c *jitCompiler) plan(code []byte) (jitPlan, bool) {
	blocks := c.blocks(code)
	if len(blocks) == 0 {
		return jitPlan{}, false
	}

	hot, forced := c.hot(blocks)
	return jitPlan{
		code:   code,
		blocks: blocks,
		hot:    hot,
		forced: forced,
	}, true
}

func (c *jitCompiler) scan(plan jitPlan) (map[int]*asm.Signature, bool) {
	buf, err := asm.NewBuffer(256)
	if err != nil {
		c.profile.JITError()
		return nil, false
	}
	defer buf.Free()

	// Scan pass uses a throwaway buffer to expose segment signatures
	// before the emit pass. jitSeg writes c.entryReturned directly when
	// the function-entry segment runs to RETURN — required for self-
	// recursion CALL emission since emit-time entries[c.addr] is always
	// nil for the function currently being compiled. Reset the flag so
	// the scan run rediscovers it.
	c.entryReturned = false

	r := newJITRun(c, asm.NewAssembler(arch, buf), plan.code, nil, true)
	r.bind(plan.blocks)

	objs, entries := r.compile(plan)
	sigs := make(map[int]*asm.Signature, len(objs))
	for i, obj := range objs {
		sigs[entries[i]] = obj.Sig
	}
	return sigs, true
}

func (c *jitCompiler) emit(plan jitPlan, sigs map[int]*asm.Signature) ([]*asm.RelocObject, []int) {
	c.assembler.Reset()

	r := newJITRun(c, c.assembler, plan.code, sigs, false)
	r.bind(plan.blocks)

	return r.compile(plan)
}

func (c *jitCompiler) link(code []byte, objs []*asm.RelocObject, entries []int) []func(*Interpreter) {
	callers, err := c.assembler.Link(objs)
	if err != nil {
		c.profile.JITError()
		return nil
	}

	out := make([]func(*Interpreter), len(code))
	for i, caller := range callers {
		if caller == nil {
			continue
		}

		fn := c.closure(caller, objs[i].Sig)
		if fn == nil {
			continue
		}

		c.profile.JITLink()
		out[entries[i]] = fn

		if entries[i] == 0 && c.entryReturned && c.entries != nil {
			if e := newJitEntry(c.heap, c.addr, objs[i].Chunk); e != nil {
				c.entries[c.addr] = e
			}
		}
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

func newJITRun(c *jitCompiler, a *asm.Assembler, code []byte, sigs map[int]*asm.Signature, scan bool) *jitRun {
	return &jitRun{
		c:    c,
		a:    a,
		code: code,
		sigs: sigs,
		scan: scan,
	}
}

func (r *jitRun) bind(blocks []*analysis.BasicBlock) {
	r.labels = make(map[int]int, len(blocks))
	for _, b := range blocks {
		r.labels[b.Start] = r.a.NewLabel()
	}
}

func (r *jitRun) compile(plan jitPlan) ([]*asm.RelocObject, []int) {
	var objs []*asm.RelocObject
	var entries []int

	for _, b := range plan.hot {
		blockObjs, blockEntries, _ := r.block(b, plan.forced[b])
		objs = append(objs, blockObjs...)
		entries = append(entries, blockEntries...)
	}

	return objs, entries
}

func (r *jitRun) block(b *analysis.BasicBlock, force bool) ([]*asm.RelocObject, []int, bool) {
	var objs []*asm.RelocObject
	var entries []int

	for start := b.Start; start < b.End; {
		obj, next, stop := r.segment(start, b.End, force && start == b.Start)
		if obj != nil {
			objs = append(objs, obj)
			entries = append(entries, start)
		}
		if stop {
			return objs, entries, true
		}
		if next <= start {
			next = r.next(start)
			if next <= start {
				return objs, entries, false
			}
		}
		start = next
	}

	return objs, entries, false
}

func (r *jitRun) segment(start, end int, force bool) (*asm.RelocObject, int, bool) {
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
	}

	s.init()
	return s.run()
}

func (r *jitRun) next(ip int) int {
	if ip < 0 || ip >= len(r.code) {
		return ip + 1
	}
	return ip + instr.Instruction(r.code[ip:]).Width()
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

func (s *jitSeg) init() {
	s.scratch = s.scratch[:0]
	for range rScratchCount {
		s.scratch = append(s.scratch, s.assembler.Scratch())
	}

	if jitPrologue != nil {
		jitPrologue(s)
	}
	if id, ok := s.labels[s.start]; ok {
		s.assembler.Bind(id)
	}
}

func (s *jitSeg) run() (*asm.RelocObject, int, bool) {
	count := 0

	for s.ip < s.end {
		prev := s.ip
		ok, stop := jit[s.code[s.ip]](s)
		if !ok {
			return s.partial(prev, count)
		}

		count++
		if stop {
			if ok, skip := s.can(s.ip, count); !ok {
				s.abort(skip)
				return nil, s.ip, true
			}
			return s.compileAt(s.ip, true)
		}
	}

	if ok, skip := s.can(s.ip, count); !ok {
		s.abort(skip)
		return nil, s.ip, false
	}

	if jitEpilogue != nil {
		jitEpilogue(s)
	}
	return s.compileAt(s.ip, false)
}

func (s *jitSeg) partial(fail, count int) (*asm.RelocObject, int, bool) {
	next := s.ip
	if ok, skip := s.can(fail, count); !ok {
		s.abort(skip)
		return nil, next, false
	}

	s.end = fail
	if jitEpilogue != nil {
		jitEpilogue(s)
	}
	return s.compileAt(next, false)
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
	if s.r.scan {
		return
	}
	if skip {
		s.r.c.profile.JITSkip()
	} else {
		s.r.c.profile.JITAbort()
	}
}

func (s *jitSeg) compileAt(next int, stop bool) (*asm.RelocObject, int, bool) {
	s.finalize()

	obj, err := s.assembler.Compile()
	if err != nil {
		s.assembler.Abort()
		if !s.r.scan {
			s.r.c.profile.JITError()
		}
		return nil, next, false
	}

	if !s.r.scan {
		s.r.c.profile.JITEmit(obj.Chunk.Size())
	}
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

// PinReturn pins the current eval-stack regs to ABI return slots and marks
// the current instruction index as a return Site. Called from architecture-
// specific ret() helpers.
func (s *jitSeg) PinReturn() int {
	idx := s.assembler.Index()
	for i, v := range s.stack {
		_ = s.assembler.Pin(v, asm.NewPReg(uint8(i), v.Type(), v.Width()))
	}
	s.assembler.Site(idx, slices.Clone(s.stack))
	return idx
}

func (s *jitSeg) linkable(target int, _ bool) bool {
	if s.r.scan || target < 0 || target >= len(s.code) {
		return false
	}

	sig := s.r.sigs[target]
	if sig == nil {
		return false
	}

	src := s.stack
	dst := sig.Params(sig.Entry)
	if !sameStack(src, dst) {
		return false
	}

	_ = s.PinReturn()
	return true
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

func (s *jitSeg) kind(r asm.Reg) (types.Kind, bool) {
	return s.r.c.regKind(r)
}

func sameStack(src []asm.VReg, dst []asm.PReg) bool {
	if len(src) != len(dst) {
		return false
	}
	for i, v := range src {
		if v.Type() != dst[i].Type() || v.Width() != dst[i].Width() {
			return false
		}
	}
	return true
}

// newJitEntry builds a jitEntry from heap-resident *types.Function
// metadata. chunk is the native target for cross-function callers
// (BLR) and nil for self-recursion (caller uses BL to a chunk-local
// label instead). Returns nil if addr does not point at a Function.
func newJitEntry(heap []types.Value, addr int, chunk *asm.Chunk) *jitEntry {
	if addr <= 0 || addr >= len(heap) {
		return nil
	}
	fn, ok := heap[addr].(*types.Function)
	if !ok || fn.Typ == nil {
		return nil
	}
	kinds := func(ts []types.Type) []types.Kind {
		out := make([]types.Kind, len(ts))
		for i, t := range ts {
			out[i] = t.Kind()
		}
		return out
	}
	return &jitEntry{
		chunk:  chunk,
		params: kinds(fn.Typ.Params),
		locals: kinds(fn.Locals),
		rets:   kinds(fn.Typ.Returns),
	}
}

func (c *jitCompiler) closure(fn asm.Caller, sig *asm.Signature) func(*Interpreter) {
	if len(sig.Scratch) <= rInterp {
		return nil
	}

	pregs := fn.Params(sig.Entry)
	pkinds := make([]types.Kind, len(pregs))
	for i, r := range pregs {
		kind, ok := c.regKind(r)
		if !ok {
			return nil
		}
		pkinds[i] = kind
	}

	rregs := fn.Returns(sig.Entry)
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

		savedFp := i.fp
		rets, err := fn.Call(params, &scratch)
		if err != nil {
			panic(err)
		}

		// Native RETURN inside the chunk already restored i.fr/i.fp/i.sp
		// and copied returns into i.stack. Skip the Go-side post-amble.
		if i.fp != savedFp {
			return
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
	f := i.frame()
	scratch[rStack] = uint64(uintptr(unsafe.Pointer(&i.stack[f.bp])))
	scratch[rHeap] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.heap))))
	scratch[rGlobals] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.globals))))
	scratch[rInterp] = uint64(uintptr(unsafe.Pointer(i)))
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
