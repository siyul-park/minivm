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
// Pass 2 – compilation: each block is compiled with Compile() into a RelocObject.
//
//	If any instruction fails, the whole block is aborted.
//	Branch blocks (terminated=true) record their entry/exit metadata.
//	Non-branch blocks record a fixed exitIP (block's End).
//
// Pass 3 – linking: Link() patches cross-block branches and returns Callers.
//
//	Closures are installed only for blocks with a non-nil Caller.
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
// Returns (obj, 0) for branch blocks (jit[BR/BR_IF] set c.terminated).
// Returns (obj, exitIP) for arithmetic blocks where exitIP is the byte offset
// of the first non-compilable instruction (i.e. where the interpreter resumes).
// Returns (nil, 0) if any non-terminal instruction fails to compile.
func (c *jitCompiler) compileBlock(b *analysis.BasicBlock) (*asm.RelocObject, int) {
	c.ip = b.Start
	c.terminated = false

	jit[_PROLOGUE](c)
	c.assembler.PlaceLabel(c.blockLabels[b.Start])

	count := 0
	for c.ip < b.End {
		// Save the IP *before* the handler advances it so we can report the
		// correct resume point when a non-compilable instruction is encountered.
		prevIP := c.ip
		op := c.code[c.ip]
		ok := jit[op](c)

		if c.terminated {
			break
		}

		if !ok {
			if count == 0 {
				// Nothing useful was compiled — discard and skip the block.
				c.assembler.AbortBlock()
				return nil, 0
			}
			// Seal the arithmetic portion compiled so far.
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
		// All instructions compiled, no explicit branch — arithmetic block.
		jit[_EPILOGUE](c)
	}

	obj, err := c.assembler.Compile()
	if err != nil {
		return nil, 0
	}

	if c.terminated {
		return obj, 0 // branch block: IP from native code
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
// rets[0..ReservedReturns-1] are metadata (first = next IP);
// rets[ReservedReturns:] are stack values returned to the interpreter.
func makeBranchClosure(fn asm.Caller, sig *asm.Signature) func(*Interpreter) {
	nParams := len(sig.Params)
	nRes := sig.ReservedReturns
	nStack := len(sig.Returns) - nRes

	kinds := retKinds(sig.Returns[nRes:])
	params := make([]uint64, nParams)

	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := range params {
			params[j] = i.unbox64(i.stack[base+j])
		}
		rets, err := fn.Call(params)
		if err != nil {
			panic(err)
		}
		nextIP := int(rets[0])
		for j := 0; j < nStack; j++ {
			i.stack[base+j] = i.box64(rets[nRes+j], kinds[j])
		}
		i.sp = base + nStack
		i.frames[i.fp-1].ip = nextIP
	}
}

// makeArithClosure wraps an arithmetic-block Caller with a fixed exit IP.
func makeArithClosure(fn asm.Caller, sig *asm.Signature, exitIP int) func(*Interpreter) {
	nParams := len(sig.Params)
	nRets := len(sig.Returns)

	kinds := retKinds(sig.Returns)
	params := make([]uint64, nParams)

	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := range params {
			params[j] = i.unbox64(i.stack[base+j])
		}
		rets, err := fn.Call(params)
		if err != nil {
			panic(err)
		}
		for j := 0; j < nRets; j++ {
			i.stack[base+j] = i.box64(rets[j], kinds[j])
		}
		i.sp = base + nRets
		i.frames[i.fp-1].ip = exitIP
	}
}

// retKinds converts a slice of RegType to the corresponding types.Kind values.
func retKinds(returns []asm.RegType) []types.Kind {
	kinds := make([]types.Kind, len(returns))
	for i, rt := range returns {
		switch rt {
		case asm.RegTypeFloat:
			kinds[i] = types.KindF64
		default:
			kinds[i] = types.KindI64
		}
	}
	return kinds
}
