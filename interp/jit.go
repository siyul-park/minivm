package interp

import (
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
}

type jitPlan struct {
	code   []byte
	blocks []*analysis.BasicBlock
	hot    []*analysis.BasicBlock
	forced map[*analysis.BasicBlock]bool
}

type jitPass struct {
	c    *jitCompiler
	a    *asm.Assembler
	code []byte

	labels map[int]int
	sigs   map[int]*asm.Signature
	scan   bool
}

type jitSeg struct {
	pass *jitPass

	assembler *asm.Assembler
	code      []byte
	constants []types.Boxed
	labels    map[int]int

	start int
	end   int
	ip    int
	force bool

	facts   map[int]types.Kind
	scratch []asm.PReg
}

const (
	opPrologue = len(jit) - 2
	opEpilogue = len(jit) - 1
)

const (
	rStack = iota
	rHeap
	rGlobals
	rNext
)

type jitOp func(*jitSeg) (ok bool, stop bool)

var arch *asm.Arch
var jit = [256]jitOp{}

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

	pass := newJITPass(c, asm.NewAssembler(arch, buf), plan.code, nil, true)
	pass.bind(plan.blocks)

	objs, entries := pass.compile(plan)
	sigs := make(map[int]*asm.Signature, len(objs))
	for i, obj := range objs {
		sigs[entries[i]] = obj.Sig
	}
	return sigs, true
}

func (c *jitCompiler) emit(plan jitPlan, sigs map[int]*asm.Signature) ([]*asm.RelocObject, []int) {
	c.assembler.Reset()

	pass := newJITPass(c, c.assembler, plan.code, sigs, false)
	pass.bind(plan.blocks)

	return pass.compile(plan)
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

func newJITPass(c *jitCompiler, a *asm.Assembler, code []byte, sigs map[int]*asm.Signature, scan bool) *jitPass {
	return &jitPass{
		c:    c,
		a:    a,
		code: code,
		sigs: sigs,
		scan: scan,
	}
}

func (p *jitPass) bind(blocks []*analysis.BasicBlock) {
	p.labels = make(map[int]int, len(blocks))
	for _, b := range blocks {
		p.labels[b.Start] = p.a.NewLabel()
	}
}

func (p *jitPass) compile(plan jitPlan) ([]*asm.RelocObject, []int) {
	var objs []*asm.RelocObject
	var entries []int

	for _, b := range plan.hot {
		blockObjs, blockEntries, _ := p.block(b, plan.forced[b])
		objs = append(objs, blockObjs...)
		entries = append(entries, blockEntries...)
	}

	return objs, entries
}

func (p *jitPass) block(b *analysis.BasicBlock, force bool) ([]*asm.RelocObject, []int, bool) {
	var objs []*asm.RelocObject
	var entries []int

	for start := b.Start; start < b.End; {
		obj, next, stop := p.segment(start, b.End, force && start == b.Start)
		if obj != nil {
			objs = append(objs, obj)
			entries = append(entries, start)
		}
		if stop {
			return objs, entries, true
		}
		if next <= start {
			next = p.next(start)
			if next <= start {
				return objs, entries, false
			}
		}
		start = next
	}

	return objs, entries, false
}

func (p *jitPass) segment(start, end int, force bool) (*asm.RelocObject, int, bool) {
	s := &jitSeg{
		pass:      p,
		assembler: p.a,
		code:      p.code,
		constants: p.c.constants,
		labels:    p.labels,
		start:     start,
		end:       end,
		ip:        start,
		force:     force,
		facts:     make(map[int]types.Kind),
	}

	s.init()
	return s.run()
}

func (p *jitPass) next(ip int) int {
	if ip < 0 || ip >= len(p.code) {
		return ip + 1
	}
	return ip + instr.Instruction(p.code[ip:]).Width()
}

func (s *jitSeg) init() {
	s.scratch = s.scratch[:0]
	for range 4 {
		s.scratch = append(s.scratch, s.assembler.Scratch())
	}

	jit[opPrologue](s)
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
			return s.compileAt(s.ip, true)
		}
	}

	if ok, skip := s.can(s.ip, count); !ok {
		s.abort(skip)
		return nil, s.ip, false
	}

	jit[opEpilogue](s)
	return s.compileAt(s.ip, false)
}

func (s *jitSeg) partial(fail, count int) (*asm.RelocObject, int, bool) {
	next := s.ip
	if ok, skip := s.can(fail, count); !ok {
		s.abort(skip)
		return nil, next, false
	}

	s.end = fail
	jit[opEpilogue](s)
	return s.compileAt(next, false)
}

func (s *jitSeg) can(end, count int) (bool, bool) {
	if count == 0 {
		return false, false
	}
	if !s.force && count < s.pass.c.cutoff {
		return false, false
	}
	if !s.force && s.hot(s.start, end) == 0 {
		return false, true
	}
	return true, false
}

func (s *jitSeg) hot(start, end int) uint64 {
	return s.pass.c.profile.Range(s.pass.c.addr, start, end)
}

func (s *jitSeg) abort(skip bool) {
	s.assembler.Abort()
	if s.pass.scan {
		return
	}
	if skip {
		s.pass.c.profile.JITSkip()
	} else {
		s.pass.c.profile.JITAbort()
	}
}

func (s *jitSeg) compileAt(next int, stop bool) (*asm.RelocObject, int, bool) {
	obj, err := s.assembler.Compile()
	if err != nil {
		s.assembler.Abort()
		if !s.pass.scan {
			s.pass.c.profile.JITError()
		}
		return nil, next, false
	}

	if !s.pass.scan {
		s.pass.c.profile.JITEmit(obj.Chunk.Size())
	}
	return obj, next, stop
}

func (s *jitSeg) linkable(target int, _ bool) bool {
	if s.pass.scan || target < 0 || target >= len(s.code) {
		return false
	}

	sig := s.pass.sigs[target]
	if sig == nil {
		return false
	}

	src := s.assembler.Returns(s.assembler.Index())
	dst := sig.Params(sig.Entry)
	if !s.same(src, dst) {
		return false
	}

	s.assembler.Mark()
	return true
}

func (s *jitSeg) same(src []asm.VReg, dst []asm.PReg) bool {
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

func (s *jitSeg) global(idx int) (int16, bool) {
	if idx < 0 || idx >= len(s.pass.c.globals) {
		return 0, false
	}
	offset := int16(idx * 8)
	if int(offset) != idx*8 {
		return 0, false
	}
	return offset, true
}

func (s *jitSeg) local(idx int) (types.Type, bool) {
	c := s.pass.c
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

func (c *jitCompiler) closure(fn asm.Caller, sig *asm.Signature) func(*Interpreter) {
	regs := fn.Params(sig.Entry)
	if len(sig.Scratch) <= rNext {
		return nil
	}

	kinds := make([]types.Kind, len(regs))
	for i, r := range regs {
		kind, ok := c.kind(r)
		if !ok {
			return nil
		}
		kinds[i] = kind
	}

	return func(i *Interpreter) {
		base := i.sp - len(regs)
		params := make([]asm.Value, len(regs))
		for j := range regs {
			bits := i.unbox64(i.stack[base+j])
			params[j] = c.value(bits, kinds[j])
		}

		scratch := make([]uint64, len(sig.Scratch))
		c.scratch(i, scratch)

		rets, err := fn.Call(params, &scratch)
		if err != nil {
			panic(err)
		}

		for j, val := range rets {
			i.stack[base+j] = i.box64(val.Bits(), c.valueKind(val))
		}
		i.sp = base + len(rets)
		i.frames[i.fp-1].ip = int(scratch[rNext])
	}
}

func (c *jitCompiler) scratch(i *Interpreter, scratch []uint64) {
	f := i.frame()
	scratch[rStack] = uint64(uintptr(unsafe.Pointer(&i.stack[f.bp])))
	scratch[rHeap] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.heap))))
	scratch[rGlobals] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.globals))))
}

func (c *jitCompiler) value(bits uint64, kind types.Kind) asm.Value {
	switch kind {
	case types.KindI32:
		return asm.I32(uint32(bits))
	case types.KindI64:
		return asm.I64(bits)
	case types.KindF32:
		return asm.F32(uint32(bits))
	default:
		return asm.F64(bits)
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

func (c *jitCompiler) kind(r asm.Reg) (types.Kind, bool) {
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
