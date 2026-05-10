package interp

import (
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
	types      []types.Type
	constants  []types.Boxed
	heap       []types.Value
	code       []byte
	ip         int
	labels     map[int]int
	compilable map[int]bool
	sigs       map[int]*asm.Signature
	scratch    []asm.PReg
	end        int
}

var (
	_PROLOGUE = len(jit) - 2
	_EPILOGUE = len(jit) - 1
)

const (
	rStack = iota
	rHeap
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

	var branches []*analysis.BasicBlock
	for _, b := range blocks {
		if c.profile.HitsInRange(c.addr, b.Start, b.End) == 0 {
			c.compilable[b.Start] = false
			continue
		}
		segObjs, entryIPs, terminated := c.compile(b)
		for i, entryIP := range entryIPs {
			c.compilable[entryIP] = true
			c.sigs[entryIP] = segObjs[i].Sig
		}
		if terminated {
			branches = append(branches, b)
		} else {
			for i, obj := range segObjs {
				objs = append(objs, obj)
				metas = append(metas, meta{obj, entryIPs[i]})
			}
		}
	}

	for _, b := range branches {
		segObjs, entryIPs, _ := c.compile(b)
		if len(segObjs) == 0 {
			c.compilable[b.Start] = false
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

	callers, _ := c.assembler.Link(objs)
	for i, m := range metas {
		if callers[i] == nil {
			continue
		}
		out[m.entryIP] = c.closure(callers[i], m.obj.Sig)
	}

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
	c.end = end
	c.scratch = append(c.scratch[:0], c.assembler.Scratch())
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
				return nil, c.ip, false
			}
			c.end = prevIP
			jit[_EPILOGUE](c)
			obj, err := c.assembler.Compile()
			if err != nil {
				return nil, c.ip, false
			}
			return obj, c.ip, false
		}

		count++
		stop = s
		if stop {
			break
		}
	}

	if count <= 4 {
		c.assembler.Abort()
		return nil, c.ip, stop
	}
	if !stop {
		jit[_EPILOGUE](c)
	}
	obj, err := c.assembler.Compile()
	if err != nil {
		return nil, c.ip, stop
	}
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
	src := c.assembler.Returns()
	if len(src) != len(sig.Params) {
		return false
	}
	for i, v := range src {
		if v.Type() != sig.Params[i].Type() || v.Width() != sig.Params[i].Width() {
			return false
		}
	}
	return true
}

func (c *jitCompiler) closure(fn asm.Caller, sig *asm.Signature) func(*Interpreter) {
	nParams := len(sig.Params)
	kinds := c.kinds(sig.Returns)
	params := make([]uint64, nParams)
	scratch := make([]uint64, len(sig.Scratch))

	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := range nParams {
			params[j] = i.unbox64(i.stack[base+j])
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
		rets, err := fn.Call(params, &scratch)
		if err != nil {
			panic(err)
		}
		for j, kind := range kinds {
			i.stack[base+j] = i.box64(rets[j], kind)
		}
		i.sp = base + len(kinds)
		i.frames[i.fp-1].ip = int(scratch[rNext])
	}
}

func (c *jitCompiler) kinds(regs []asm.PReg) []types.Kind {
	kinds := make([]types.Kind, len(regs))
	for i, p := range regs {
		switch p.Type() {
		case asm.RegTypeFloat:
			if p.Width() == asm.Width32 {
				kinds[i] = types.KindF32
			} else {
				kinds[i] = types.KindF64
			}
		default:
			if p.Width() == asm.Width32 {
				kinds[i] = types.KindI32
			} else {
				kinds[i] = types.KindI64
			}
		}
	}
	return kinds
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
