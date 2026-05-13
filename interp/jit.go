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
	assembler   *asm.Assembler
	profile     *prof.Stats
	addr        int
	types       []types.Type
	constants   []types.Boxed
	globals     []types.Boxed
	heap        []types.Value
	code        []byte
	ip          int
	cutoff      int
	entry       int
	labels      map[int]int
	compilable  map[int]bool
	sigs        map[int]*asm.Signature
	globalKinds map[int]types.Kind
	scratch     []asm.PReg
	end         int
}

type profiledBlock struct {
	block *analysis.BasicBlock
	heat  uint64
}

var (
	_PROLOGUE = len(jit) - 2
	_EPILOGUE = len(jit) - 1
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
	c.code, c.ip = code, 0

	blocks := c.blocks(code)
	if blocks == nil {
		return nil
	}

	out := make([]func(*Interpreter), len(code))

	c.labels = make(map[int]int, len(blocks))
	c.compilable = make(map[int]bool, len(blocks))
	c.sigs = make(map[int]*asm.Signature, len(blocks))
	for _, b := range blocks {
		c.labels[b.Start] = c.assembler.NewLabel()
	}

	type meta struct {
		obj     *asm.RelocObject
		entryIP int
	}
	var objs []*asm.RelocObject
	var metas []meta

	var branches []profiledBlock
	for _, pb := range c.hotBlocks(blocks) {
		segObjs, entryIPs, terminated := c.compile(pb.block)
		for i, entryIP := range entryIPs {
			c.compilable[entryIP] = true
			c.sigs[entryIP] = segObjs[i].Sig
		}
		if terminated {
			branches = append(branches, pb)
		} else {
			for i, obj := range segObjs {
				objs = append(objs, obj)
				metas = append(metas, meta{obj, entryIPs[i]})
			}
		}
	}

	for _, pb := range branches {
		segObjs, entryIPs, _ := c.compile(pb.block)
		if len(segObjs) == 0 {
			c.compilable[pb.block.Start] = false
			continue
		}
		for i, obj := range segObjs {
			objs = append(objs, obj)
			metas = append(metas, meta{obj, entryIPs[i]})
		}
	}

	if len(objs) == 0 {
		return nil
	}

	callers, err := c.assembler.Link(objs)
	if err != nil {
		c.profile.JITError()
		return nil
	}
	for i, m := range metas {
		if callers[i] == nil {
			continue
		}
		c.profile.JITLink()
		out[m.entryIP] = c.closure(callers[i], m.obj.Sig)
	}

	return out
}

func (c *jitCompiler) hotBlocks(blocks []*analysis.BasicBlock) []profiledBlock {
	out := make([]profiledBlock, 0, len(blocks))
	for _, b := range blocks {
		heat := c.profile.Range(c.addr, b.Start, b.End)
		if heat == 0 {
			c.compilable[b.Start] = false
			continue
		}
		out = append(out, profiledBlock{block: b, heat: heat})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].heat != out[j].heat {
			return out[i].heat > out[j].heat
		}
		return out[i].block.Start < out[j].block.Start
	})
	return out
}

func (c *jitCompiler) compile(b *analysis.BasicBlock) ([]*asm.RelocObject, []int, bool) {
	var objs []*asm.RelocObject
	var entryIPs []int

	start := b.Start
	for start < b.End {
		obj, next, terminated := c.segment(c.code, start, b.End)
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

func (c *jitCompiler) segment(code []byte, start, end int) (*asm.RelocObject, int, bool) {
	c.ip = start
	c.entry = start
	c.end = end
	c.globalKinds = make(map[int]types.Kind)
	c.scratch = append(c.scratch[:0], c.assembler.Scratch())
	c.scratch = append(c.scratch, c.assembler.Scratch())
	c.scratch = append(c.scratch, c.assembler.Scratch())
	c.scratch = append(c.scratch, c.assembler.Scratch())

	jit[_PROLOGUE](c)
	if id, ok := c.labels[start]; ok {
		c.assembler.Bind(id)
	}

	count := 0
	stop := false
	for c.ip < end {
		prevIP := c.ip
		ok, s := jit[code[c.ip]](c)

		if !ok {
			if count <= 4 {
				c.assembler.Abort()
				c.profile.JITAbort()
				return nil, c.ip, false
			}
			if c.profile.Range(c.addr, start, prevIP) == 0 {
				c.assembler.Abort()
				c.profile.JITSkip()
				return nil, c.ip, false
			}
			c.end = prevIP
			jit[_EPILOGUE](c)
			obj, err := c.assembler.Compile()
			if err != nil {
				c.profile.JITError()
				return nil, c.ip, false
			}
			c.profile.JITEmit(obj.Chunk.Size())
			return obj, c.ip, false
		}

		count++
		stop = s
		if stop {
			break
		}
	}

	if count < c.cutoff {
		c.assembler.Abort()
		c.profile.JITAbort()
		return nil, c.ip, stop
	}
	if c.profile.Range(c.addr, start, c.ip) == 0 {
		c.assembler.Abort()
		c.profile.JITSkip()
		return nil, c.ip, stop
	}
	if !stop {
		jit[_EPILOGUE](c)
	}
	obj, err := c.assembler.Compile()
	if err != nil {
		c.profile.JITError()
		return nil, c.ip, stop
	}
	c.profile.JITEmit(obj.Chunk.Size())
	return obj, c.ip, stop
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

func (c *jitCompiler) linkable(targetIP int) bool {
	sig := c.sigs[targetIP]
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
		p, ok := c.kind(p)
		if !ok {
			return nil
		}
		kinds[i] = p
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
			if len(i.heap) > 0 {
				scratch[rHeap] = uint64(uintptr(unsafe.Pointer(&i.heap[0])))
			} else {
				scratch[rHeap] = 0
			}
		}
		if len(scratch) > rGlobals {
			if len(i.globals) > 0 {
				scratch[rGlobals] = uint64(uintptr(unsafe.Pointer(&i.globals[0])))
			} else {
				scratch[rGlobals] = 0
			}
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
	offset := idx * 8
	if offset > int(^uint16(0)>>1) {
		return 0, false
	}
	return int16(offset), true
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
	if idx < 0 || idx >= len(fn.Locals) {
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
