package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

type jitCompiler struct {
	assembler *asm.Assembler
	types     []types.Type
	constants []types.Boxed
	heap      []types.Value
	code      []byte
	ip        int

	// Shared across all blocks in a function; set before any block is compiled.
	labels     map[int]int            // blockStart IP → label ID
	compilable map[int]bool           // true after a block compiles successfully
	sigs       map[int]*asm.Signature // pass-1 signatures, used by canLink()

	// Set per block by compileBlock.
	blockEnd int
	scratch  asm.PReg // Scratch[0] — pre-allocated by compileBlock, used by _PROLOGUE/_EPILOGUE and branch handlers

	// Set by jit[BR/BR_IF/BR_TABLE] when the handler emits its own RET.
	terminated bool
}

var (
	_PROLOGUE = len(jit) - 2
	_EPILOGUE = len(jit) - 1
)

var arch *asm.Arch
var jit = [256]func(c *jitCompiler) bool{}

func init() {
	for i, fn := range jit {
		if fn == nil {
			jit[i] = func(c *jitCompiler) bool {
				inst := instr.Instruction(c.code[c.ip:])
				c.ip += inst.Width()
				return false
			}
		}
	}
}

// Compile compiles all basic blocks in code and returns a sparse slice of
// closures indexed by instruction offset.
//
// Two passes ensure every branch (forward and backward) gets a direct native
// link when the target compiles successfully and the register signatures are
// compatible:
//
//   - Pass 1: compile in program order; backward references emit BLabel,
//     forward references fall back to LDI+RET.  Each compiled block is marked
//     compilable and its Signature is stored.
//
//   - Pass 2: recompile branch blocks only — compilable/sigs are now complete,
//     so forward BLabels are emitted correctly.
//
//   - Link: patch BLabel relocations and install closures.
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

	var branchs []*analysis.BasicBlock
	for _, b := range blocks {
		obj, terminated := c.compileBlock(b)
		if obj == nil {
			continue
		}
		c.compilable[b.Start] = true
		c.sigs[b.Start] = obj.Sig
		if terminated {
			branchs = append(branchs, b)
		} else {
			objs = append(objs, obj)
			metas = append(metas, meta{obj, b.Start})
		}
	}

	for _, b := range branchs {
		obj, _ := c.compileBlock(b)
		if obj == nil {
			c.compilable[b.Start] = false
			continue
		}
		objs = append(objs, obj)
		metas = append(metas, meta{obj, b.Start})
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

// compileBlock compiles all instructions in b into one RelocObject.
// Returns (obj, true) for branch blocks; (obj, false) for arithmetic/partial.
// Returns (nil, false) when the very first instruction fails.
func (c *jitCompiler) compileBlock(b *analysis.BasicBlock) (*asm.RelocObject, bool) {
	c.ip = b.Start
	c.blockEnd = b.End
	c.terminated = false
	c.scratch = c.assembler.Reserve()

	jit[_PROLOGUE](c)
	c.assembler.Place(c.labels[b.Start])

	compiled := false
	for c.ip < b.End {
		prevIP := c.ip
		ok := jit[c.code[c.ip]](c)

		if c.terminated {
			break
		}

		if !ok {
			if !compiled {
				c.assembler.Abort()
				return nil, false
			}
			c.blockEnd = prevIP
			jit[_EPILOGUE](c)
			obj, err := c.assembler.Compile()
			if err != nil {
				return nil, false
			}
			return obj, false
		}
		compiled = true
	}

	if !c.terminated {
		jit[_EPILOGUE](c)
	}

	obj, err := c.assembler.Compile()
	if err != nil {
		return nil, false
	}
	return obj, c.terminated
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

// linkable reports whether the current assembler operand stack is compatible
// with the target block's expected Params (same count, type, width).
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
	rsv := make([]uint64, len(sig.Reserved))

	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := range nParams {
			params[j] = i.unbox64(i.stack[base+j])
		}
		rets, err := fn.Call(params, &rsv)
		if err != nil {
			panic(err)
		}
		for j, kind := range kinds {
			i.stack[base+j] = i.box64(rets[j], kind)
		}
		i.sp = base + len(kinds)
		i.frames[i.fp-1].ip = int(rsv[0])
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
