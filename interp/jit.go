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
	assembler  *asm.Assembler
	profile    *prof.Stats
	addr       int
	constants  []types.Boxed
	globals    []types.Boxed
	heap       []types.Value
	code       []byte
	ip         int
	cutoff     int
	entry      int
	labels     map[int]int
	compilable map[int]*asm.Signature // presence = compilable; value = signature
	facts      map[int]types.Kind
	scratch    []asm.PReg
	end        int
}

const (
	opPrologue = len(jit) - 2 // synthetic prologue handler index
	opEpilogue = len(jit) - 1 // synthetic epilogue handler index
)

const (
	rStack = iota
	rHeap
	rGlobals
	rNext
)

var arch *asm.Arch
var jit = [256]func(c *jitCompiler) (bool, bool){}

func init() {
	for i, fn := range jit {
		if fn == nil {
			jit[i] = func(c *jitCompiler) (bool, bool) {
				inst := instr.Instruction(c.code[c.ip:])
				c.ip += inst.Width()
				return false, false
			}
		}
	}
}

func (c *jitCompiler) Compile(code []byte) []func(*Interpreter) {
	if arch == nil {
		return nil
	}
	started := time.Now()
	defer func() {
		c.profile.JITTime(time.Since(started))
	}()

	c.assembler.Reset()
	c.code = code

	blocks := c.blocks()
	if blocks == nil {
		return nil
	}

	c.compilable = make(map[int]*asm.Signature, len(blocks))
	hots, forced := c.hotBlocks(blocks)
	if len(hots) == 0 {
		return make([]func(*Interpreter), len(code))
	}
	if !c.probe(blocks, hots, forced) {
		return nil
	}

	c.assembler.Reset()
	c.buildLabels(blocks)

	var objs []*asm.RelocObject
	var entryIPs []int
	for _, b := range hots {
		segObjs, segs, _ := c.compile(b, forced[b], false)
		objs = append(objs, segObjs...)
		entryIPs = append(entryIPs, segs...)
	}

	if len(objs) == 0 {
		return nil
	}

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
		c.profile.JITLink()
		out[entryIPs[i]] = c.closure(caller, objs[i].Sig)
	}
	return out
}

// hotBlocks returns blocks selected for JIT compilation (hot blocks and their
// cold successors), sorted by heat descending. forced marks cold successor
// blocks that must be compiled to allow branch linking from hot blocks.
func (c *jitCompiler) hotBlocks(blocks []*analysis.BasicBlock) ([]*analysis.BasicBlock, map[*analysis.BasicBlock]bool) {
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
			sb := blocks[succ]
			if heat[sb] == 0 {
				forced[sb] = true
			}
		}
	}

	out := make([]*analysis.BasicBlock, 0, len(blocks))
	for _, b := range blocks {
		if heat[b] > 0 || forced[b] {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return nil, nil
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

func (c *jitCompiler) buildLabels(blocks []*analysis.BasicBlock) {
	c.labels = make(map[int]int, len(blocks))
	for _, b := range blocks {
		c.labels[b.Start] = c.assembler.NewLabel()
	}
}

// probe runs a discovery pass to populate c.compilable with the signature
// of each compilable segment entry point, using a temporary throwaway assembler.
func (c *jitCompiler) probe(blocks []*analysis.BasicBlock, hots []*analysis.BasicBlock, forced map[*analysis.BasicBlock]bool) bool {
	buf, err := asm.NewBuffer(256)
	if err != nil {
		c.profile.JITError()
		return false
	}
	defer buf.Free()

	origAssembler, origLabels := c.assembler, c.labels
	c.assembler = asm.NewAssembler(arch, buf)
	c.buildLabels(blocks)

	for _, b := range hots {
		segObjs, entryIPs, _ := c.compile(b, forced[b], true)
		for i, ip := range entryIPs {
			c.compilable[ip] = segObjs[i].Sig
		}
	}

	c.assembler, c.labels = origAssembler, origLabels
	return true
}

func (c *jitCompiler) compile(b *analysis.BasicBlock, force, discovery bool) ([]*asm.RelocObject, []int, bool) {
	var objs []*asm.RelocObject
	var entryIPs []int
	start := b.Start
	for start < b.End {
		obj, next, terminated := c.segment(start, b.End, force && start == b.Start, discovery)
		if obj != nil {
			objs = append(objs, obj)
			entryIPs = append(entryIPs, start)
		}
		if terminated {
			return objs, entryIPs, true
		}
		start = next
	}
	return objs, entryIPs, false
}

func (c *jitCompiler) segment(start, end int, force, discovery bool) (*asm.RelocObject, int, bool) {
	c.ip, c.entry, c.end = start, start, end
	c.facts = make(map[int]types.Kind)
	c.scratch = c.scratch[:0]

	for range 4 {
		c.scratch = append(c.scratch, c.assembler.Scratch())
	}

	jit[opPrologue](c)
	if id, ok := c.labels[start]; ok {
		c.assembler.Bind(id)
	}

	count, stop := 0, false
	for c.ip < end {
		prevIP := c.ip
		ok, s := jit[c.code[c.ip]](c)
		if !ok {
			failIP := prevIP // unsupported instruction starts here
			nextIP := c.ip   // handler may have advanced past unsupported

			if count == 0 || (!force && count < c.cutoff) {
				c.assembler.Abort()
				if !discovery {
					c.profile.JITAbort()
				}
				return nil, nextIP, false
			}

			if !force && c.profile.Range(c.addr, start, failIP) == 0 {
				c.assembler.Abort()
				if !discovery {
					c.profile.JITSkip()
				}
				return nil, nextIP, false
			}

			c.end = failIP
			jit[opEpilogue](c)

			obj, _ := c.emit(discovery)
			return obj, nextIP, false
		}

		count++
		stop = s
		if stop {
			break
		}
	}

	if !stop {
		if !force && count < c.cutoff {
			c.assembler.Abort()
			if !discovery {
				c.profile.JITAbort()
			}
			return nil, c.ip, false
		}
		if !force && c.profile.Range(c.addr, start, c.ip) == 0 {
			c.assembler.Abort()
			if !discovery {
				c.profile.JITSkip()
			}
			return nil, c.ip, false
		}
		jit[opEpilogue](c)
	}

	obj, next := c.emit(discovery)
	return obj, next, stop
}

func (c *jitCompiler) emit(discovery bool) (*asm.RelocObject, int) {
	obj, err := c.assembler.Compile()
	if err != nil {
		if !discovery {
			c.profile.JITError()
		}
		return nil, c.ip
	}
	if !discovery {
		c.profile.JITEmit(obj.Chunk.Size())
	}
	return obj, c.ip
}

func (c *jitCompiler) blocks() []*analysis.BasicBlock {
	m := pass.NewManager()
	if err := m.Register(analysis.NewBasicBlocksPass()); err != nil {
		return nil
	}
	if err := m.Run(&types.Function{Typ: &types.FunctionType{}, Code: c.code}); err != nil {
		return nil
	}
	var blocks []*analysis.BasicBlock
	if err := m.Load(&blocks); err != nil {
		return nil
	}
	return blocks
}

func (c *jitCompiler) linkable(targetIP int, discovery bool) bool {
	if discovery || targetIP < 0 || targetIP >= len(c.code) {
		return false
	}
	sig := c.compilable[targetIP]
	if sig == nil {
		return false
	}
	src := c.assembler.Returns(c.assembler.Index())
	params := sig.Params(sig.Entry)
	if len(src) != len(params) {
		return false
	}
	for i, v := range src {
		if v.Type() != params[i].Type() || v.Width() != params[i].Width() {
			return false
		}
	}
	return true
}

func (c *jitCompiler) closure(fn asm.Caller, sig *asm.Signature) func(*Interpreter) {
	pregs := fn.Params(sig.Entry)
	nParams := len(pregs)
	scratch := make([]uint64, len(sig.Scratch))

	kinds := make([]types.Kind, nParams)
	for i, p := range pregs {
		k, ok := c.kind(p)
		if !ok {
			return nil
		}
		kinds[i] = k
	}

	params := make([]asm.Value, nParams) // single goroutine: allocate once outside closure
	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := range nParams {
			bits := i.unbox64(i.stack[base+j])
			switch kinds[j] {
			case types.KindI32:
				params[j] = asm.I32(uint32(bits))
			case types.KindI64:
				params[j] = asm.I64(bits)
			case types.KindF32:
				params[j] = asm.F32(uint32(bits))
			default:
				params[j] = asm.F64(bits)
			}
		}
		if len(scratch) > rStack {
			f := i.frame()
			scratch[rStack] = uint64(uintptr(unsafe.Pointer(&i.stack[f.bp])))
		}
		if len(scratch) > rHeap {
			scratch[rHeap] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.heap))))
		}
		if len(scratch) > rGlobals {
			scratch[rGlobals] = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.globals))))
		}
		rets, err := fn.Call(params, &scratch)
		if err != nil {
			panic(err)
		}
		for j, val := range rets {
			var kind types.Kind
			switch {
			case val.RegType() == asm.RegTypeFloat && val.Width() == asm.Width64:
				kind = types.KindF64
			case val.RegType() == asm.RegTypeFloat:
				kind = types.KindF32
			case val.Width() == asm.Width64:
				kind = types.KindI64
			default:
				kind = types.KindI32
			}
			i.stack[base+j] = i.box64(val.Bits(), kind)
		}
		i.sp = base + len(rets)
		i.frames[i.fp-1].ip = int(scratch[rNext])
	}
}

func (c *jitCompiler) global(idx int) (int16, bool) {
	if idx < 0 || idx >= len(c.globals) {
		return 0, false
	}
	offset := int16(idx * 8)
	if int(offset) != idx*8 {
		return 0, false
	}
	return offset, true
}

func (c *jitCompiler) local(idx int) (types.Type, bool) {
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

func (c *jitCompiler) kind(r0 asm.Reg) (types.Kind, bool) {
	switch r0.Type() {
	case asm.RegTypeFloat:
		if r0.Width() == asm.Width32 {
			return types.KindF32, true
		}
		return types.KindF64, true
	case asm.RegTypeInt:
		if r0.Width() == asm.Width32 {
			return types.KindI32, true
		}
		return types.KindI64, true
	default:
		return 0, false
	}
}
