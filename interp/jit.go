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

	// blockLabels and compilable are shared across all blocks in a function.
	blockLabels map[int]int  // blockStart IP → label ID
	compilable  map[int]bool // true after a branch block compiles successfully

	// terminated is set by jit[BR/BR_IF/BR_TABLE] when the handler emits its own RET.
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
// Two compilation passes are used so that every branch (forward and backward)
// gets a direct native link when the target compiles successfully:
//
//   - Pass 1: compile all blocks in order.  Backward references to already-
//     compiled branch blocks emit BLabel; forward references fall back to
//     LoadImm+RET.  Successfully compiled branch blocks are marked compilable.
//
//   - Pass 2: recompile branch blocks only.  compilable is now complete, so
//     every target that compiled in pass 1 gets a BLabel in pass 2 as well.
//
//   - Link: patch all BLabel relocations and install closures.
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

	// Pre-assign label IDs so handlers can reference any target by label
	// before that block is compiled.
	c.blockLabels = make(map[int]int, len(blocks))
	c.compilable = make(map[int]bool, len(blocks))
	for _, b := range blocks {
		c.blockLabels[b.Start] = c.assembler.NewLabel()
	}

	type blockMeta struct {
		obj     *asm.RelocObject
		entryIP int
		exitIP  int // 0 = branch block, >0 = arithmetic block
	}
	var allObjs []*asm.RelocObject
	var allMeta []blockMeta

	// Pass 1
	var branchBlocks []*analysis.BasicBlock
	for _, b := range blocks {
		obj, exitIP := c.compileBlock(b)
		if obj == nil {
			continue
		}
		if exitIP == 0 {
			c.compilable[b.Start] = true
			branchBlocks = append(branchBlocks, b)
		} else {
			allObjs = append(allObjs, obj)
			allMeta = append(allMeta, blockMeta{obj, b.Start, exitIP})
		}
	}

	// Pass 2: recompile branch blocks with complete compilable information.
	for _, b := range branchBlocks {
		obj, exitIP := c.compileBlock(b)
		if obj == nil {
			c.compilable[b.Start] = false
			continue
		}
		allObjs = append(allObjs, obj)
		allMeta = append(allMeta, blockMeta{obj, b.Start, exitIP})
	}

	if len(allObjs) == 0 {
		return nil
	}

	// Link
	callers, _ := c.assembler.Link(allObjs)
	for i, meta := range allMeta {
		caller := callers[i]
		if caller == nil {
			continue
		}
		sig := meta.obj.Sig
		if meta.exitIP == 0 {
			compiled[meta.entryIP] = c.branchClosure(caller, sig)
		} else {
			compiled[meta.entryIP] = c.arithClosure(caller, sig, meta.exitIP)
		}
	}

	return compiled
}

// compileBlock compiles all instructions in b into one RelocObject.
//
// Returns (obj, 0) for branch blocks (c.terminated set by handler).
// Returns (obj, prevIP) for arithmetic blocks; interpreter resumes at prevIP.
// Returns (nil, 0) when the first instruction already fails.
func (c *jitCompiler) compileBlock(b *analysis.BasicBlock) (*asm.RelocObject, int) {
	c.ip = b.Start
	c.terminated = false

	jit[_PROLOGUE](c)
	c.assembler.PlaceLabel(c.blockLabels[b.Start])

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
				return nil, 0
			}
			jit[_EPILOGUE](c)
			obj, err := c.assembler.Compile()
			if err != nil {
				return nil, 0
			}
			return obj, prevIP
		}
		compiled = true
	}

	if !c.terminated {
		jit[_EPILOGUE](c)
	}

	obj, err := c.assembler.Compile()
	if err != nil {
		return nil, 0
	}

	if c.terminated {
		return obj, 0
	}
	return obj, b.End
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

// branchClosure wraps a branch-block Caller.  rets[0] is the next IP.
func (c *jitCompiler) branchClosure(fn asm.Caller, sig *asm.Signature) func(*Interpreter) {
	nRes := len(sig.Reserved)
	nParams := len(sig.Params)
	stackKinds := c.slotKinds(sig.Returns, sig.ReturnWidths)
	args := make([]uint64, nRes+nParams) // reserved inputs stay zero

	return func(i *Interpreter) {
		base := i.sp - nParams
		for j := range nParams {
			args[nRes+j] = i.unbox64(i.stack[base+j])
		}
		rets, err := fn.Call(args)
		if err != nil {
			panic(err)
		}
		for j, kind := range stackKinds {
			i.stack[base+j] = i.box64(rets[nRes+j], kind)
		}
		i.sp = base + len(stackKinds)
		i.frames[i.fp-1].ip = int(rets[0])
	}
}

// arithClosure wraps an arithmetic-block Caller with a fixed exit IP.
func (c *jitCompiler) arithClosure(fn asm.Caller, sig *asm.Signature, exitIP int) func(*Interpreter) {
	nParams := len(sig.Params)
	kinds := c.slotKinds(sig.Returns, sig.ReturnWidths)
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

func (c *jitCompiler) slotKinds(typs []asm.RegType, widths []asm.RegWidth) []types.Kind {
	kinds := make([]types.Kind, len(typs))
	for i, t := range typs {
		switch t {
		case asm.RegTypeFloat:
			if widths[i] == asm.Width32 {
				kinds[i] = types.KindF32
			} else {
				kinds[i] = types.KindF64
			}
		default:
			if widths[i] == asm.Width32 {
				kinds[i] = types.KindI32
			} else {
				kinds[i] = types.KindI64
			}
		}
	}
	return kinds
}
