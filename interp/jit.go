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
	blockLabels map[int]int  // blockStart IP → label ID
	compilable  map[int]bool // optimistic: true if block ends with BR/BR_IF

	// Set by jit[BR] / jit[BR_IF] when the handler emits its own terminator.
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

// Compile compiles every basic block in code into native closures via a
// three-pass strategy and returns a sparse slice indexed by instruction offset.
//
// Pass 1 – pre-analysis: assign labels; mark blocks whose terminal is BR/BR_IF.
// Pass 2 – compilation: each block is compiled into a RelocObject.
// Pass 3 – linking: cross-block branches are patched; Callers are returned.
func (c *jitCompiler) Compile(code []byte) []func(*Interpreter) {
	if arch == nil {
		return nil
	}

	c.assembler.Reset()
	c.code, c.ip = code, 0

	blocks := c.getBlocks(code)
	if blocks == nil {
		return nil
	}

	compiled := make([]func(*Interpreter), len(code))

	// Pass 1 — pre-assign labels and determine branch-compilable blocks.
	c.blockLabels = make(map[int]int, len(blocks))
	c.compilable = make(map[int]bool, len(blocks))
	for _, b := range blocks {
		c.blockLabels[b.Start] = c.assembler.NewLabel()
		c.compilable[b.Start] = c.isBranchBlock(b)
	}

	// Pass 2 — compile each block.
	type blockMeta struct {
		obj     *asm.RelocObject
		entryIP int
		exitIP  int // 0 = branch block (IP comes from native code), >0 = arith
	}
	var allObjs []*asm.RelocObject
	var allMeta []blockMeta

	for _, b := range blocks {
		obj, exitIP := c.compileBlock(b)
		if obj == nil {
			continue
		}
		allObjs = append(allObjs, obj)
		allMeta = append(allMeta, blockMeta{obj, b.Start, exitIP})
	}

	if len(allObjs) == 0 {
		return nil
	}

	// Pass 3 — link and install closures.
	callers, _ := c.assembler.Link(allObjs) // partial success: nil = failed
	for i, meta := range allMeta {
		caller := callers[i]
		if caller == nil {
			continue
		}
		sig := meta.obj.Sig
		if meta.exitIP == 0 {
			compiled[meta.entryIP] = makeBranchClosure(caller, sig)
		} else {
			compiled[meta.entryIP] = makeArithClosure(caller, sig, meta.exitIP)
		}
	}

	return compiled
}

// compileBlock compiles all instructions in b into one RelocObject.
//
// Returns (obj, 0) for branch blocks.
// Returns (obj, prevIP) for arithmetic blocks, where prevIP is the offset of
// the first non-compilable instruction (the interpreter resumes there).
// Returns (nil, 0) if the very first instruction fails.
func (c *jitCompiler) compileBlock(b *analysis.BasicBlock) (*asm.RelocObject, int) {
	c.ip = b.Start
	c.terminated = false

	jit[_PROLOGUE](c)
	c.assembler.PlaceLabel(c.blockLabels[b.Start])

	count := 0
	for c.ip < b.End {
		// Capture IP before the handler advances it.
		prevIP := c.ip
		ok := jit[c.code[c.ip]](c)

		if c.terminated {
			break
		}

		if !ok {
			if count == 0 {
				c.assembler.AbortBlock()
				return nil, 0
			}
			jit[_EPILOGUE](c)
			obj, err := c.assembler.Compile()
			if err != nil {
				return nil, 0
			}
			return obj, prevIP // interpreter resumes at the failing instruction
		}
		count++
	}

	if !c.terminated {
		jit[_EPILOGUE](c)
	}

	obj, err := c.assembler.Compile()
	if err != nil {
		return nil, 0
	}

	if c.terminated {
		return obj, 0 // branch block: next IP is in native code
	}
	return obj, b.End // arithmetic block: interpreter resumes at block end
}

// getBlocks runs BasicBlocksPass on code and returns the block list.
func (c *jitCompiler) getBlocks(code []byte) []*analysis.BasicBlock {
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

// isBranchBlock reports whether b's terminal instruction is BR or BR_IF.
func (c *jitCompiler) isBranchBlock(b *analysis.BasicBlock) bool {
	if b.Start >= b.End {
		return false
	}
	ip := b.Start
	var last byte
	for ip < b.End {
		last = c.code[ip]
		ip += instr.Instruction(c.code[ip:]).Width()
	}
	return instr.Opcode(last) == instr.BR || instr.Opcode(last) == instr.BR_IF
}

// makeBranchClosure wraps a branch-block Caller.
//
// The native ABI layout is [Reserved..., Params...] for inputs and
// [Reserved..., Returns...] for outputs.  Reserved input slots are passed as
// zero (the native code overwrites them); Reserved output slots carry metadata
// with rets[0] being the next interpreter IP.
func makeBranchClosure(fn asm.Caller, sig *asm.Signature) func(*Interpreter) {
	nRes := len(sig.Reserved)
	nParams := len(sig.Params)
	nTotal := nRes + nParams
	stackKinds := slotKinds(sig.Returns, sig.ReturnWidths)
	args := make([]uint64, nTotal) // reused; [0..nRes-1] stay zero

	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := 0; j < nParams; j++ {
			args[nRes+j] = i.unbox64(i.stack[base+j])
		}
		rets, err := fn.Call(args)
		if err != nil {
			panic(err)
		}
		nextIP := int(rets[0]) // first Reserved output = next IP
		for j, kind := range stackKinds {
			i.stack[base+j] = i.box64(rets[nRes+j], kind)
		}
		i.sp = base + len(stackKinds)
		i.frames[i.fp-1].ip = nextIP
	}
}

// makeArithClosure wraps an arithmetic-block Caller with a fixed exit IP.
// Arithmetic blocks have no Reserved slots.
func makeArithClosure(fn asm.Caller, sig *asm.Signature, exitIP int) func(*Interpreter) {
	nParams := len(sig.Params)
	kinds := slotKinds(sig.Returns, sig.ReturnWidths)
	args := make([]uint64, nParams)

	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := range args {
			args[j] = i.unbox64(i.stack[base+j])
		}
		rets, err := fn.Call(args)
		if err != nil {
			panic(err)
		}
		for j, kind := range kinds {
			i.stack[base+j] = i.box64(rets[j], kind)
		}
		i.sp = base + len(kinds)
		i.frames[i.fp-1].ip = exitIP
	}
}

// slotKinds converts parallel RegType/RegWidth slices into boxing kinds.
func slotKinds(typs []asm.RegType, widths []asm.RegWidth) []types.Kind {
	kinds := make([]types.Kind, len(typs))
	for i, t := range typs {
		w := asm.Width64
		if i < len(widths) {
			w = widths[i]
		}
		switch t {
		case asm.RegTypeFloat:
			if w == asm.Width32 {
				kinds[i] = types.KindF32
			} else {
				kinds[i] = types.KindF64
			}
		default:
			if w == asm.Width32 {
				kinds[i] = types.KindI32
			} else {
				kinds[i] = types.KindI64
			}
		}
	}
	return kinds
}
