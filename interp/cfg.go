package interp

import (
	"errors"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// compileCFG lowers a verified function's whole control-flow graph into one
// framed native callable. Structural uncertainty rejects the attempt; individual
// unsupported opcodes lower to exact-IP threaded fallbacks.
func (c *compiler) compileCFG(i *Interpreter, addr int, fn *types.Function) (*module, bool, error) {
	mod := &module{entries: map[anchor]native{}}
	if fn == nil || len(fn.Code) == 0 {
		return mod, false, nil
	}
	if len(c.scratchRegs) < scratchCount {
		return mod, false, nil
	}
	if addr == 0 {
		for ip := 0; ip < len(fn.Code); {
			inst := instr.Instruction(fn.Code[ip:])
			if inst.Opcode() == instr.CALL || inst.Opcode() == instr.RETURN_CALL {
				return mod, false, nil
			}
			ip += inst.Width()
		}
	}

	m := pass.NewManager()
	pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
	blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
	if err != nil {
		return mod, false, nil
	}
	if _, ok := blockHeights(fn, blocks, i.constants, i.heap); !ok {
		return mod, false, nil
	}

	asmb := asm.New(c.arch)
	entry := asmb.Label()

	// The declared Program.Globals are out of scope here; New pre-seeds every
	// slot to the zero Boxed of its declared kind, so the runtime values carry
	// the declared kinds at all times (mirrors emitRoot).
	globals := make([]types.Kind, len(i.globals))
	for j, g := range i.globals {
		globals[j] = g.Kind()
	}

	funcs := make(map[int]*types.Function)
	for fnAddr := range i.instrs {
		if target, ok := i.function(fnAddr); ok {
			funcs[fnAddr] = target
		}
	}

	ctx := &lowering{
		assembler: asmb,
		funcs:     funcs,
		queued:    map[branch]asm.Label{},
		tails:     map[*step]asm.Label{},
		constants: i.constants,
		globals:   globals,
		heap:      i.heap,
		scratch:   c.scratchRegs[:scratchCount],
		entry:     entry,
		head:      asmb.Label(),
		addr:      addr,
	}
	if fn.Typ != nil {
		ctx.returns = len(fn.Typ.Returns)
	}
	ctx.frames = append(ctx.frames, newActivation(addr, fn, 0, 0))
	kinds, ok := blockKinds(fn, blocks, i.constants, globals, i.heap)
	if !ok {
		return mod, false, nil
	}
	labels := make([]asm.Label, len(blocks))
	for j := range labels {
		labels[j] = asmb.Label()
	}

	if !c.lowerer.lowerCFG(ctx, blocks, kinds, labels) {
		return mod, false, nil
	}
	code, err := asmb.Build()
	if err != nil {
		if errors.Is(err, asm.ErrNoRegistersAvailable) || errors.Is(err, asm.ErrBranchOutOfRange) {
			return mod, false, nil
		}
		return mod, false, err
	}
	linked, err := asm.Link(c.buffer, c.arch, []*asm.Code{code}, nil)
	if err != nil {
		// See emitRoot: Link's external relocation re-encoding can also return
		// ErrBranchOutOfRange, so it gets the same clean fallback as Build.
		if errors.Is(err, asm.ErrBranchOutOfRange) {
			return mod, false, nil
		}
		return mod, false, err
	}
	a := anchor{addr: addr, ip: 0}
	mod.entries[a] = native{callable: linked[0].Callable, cfg: true}
	mod.emits++
	mod.bytes += len(code.Bytes)
	return mod, true, nil
}
