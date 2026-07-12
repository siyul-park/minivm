package interp

import (
	"errors"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// compileCFG lowers fn's whole control-flow graph into one framed native
// callable, without a recorded trace to drive lowering. Phase 2 accepts only
// the narrowest useful shape — a function whose body is exactly one
// straight-line, RETURN-terminated basic block — and returns ok=false for
// everything else (an indeterminate stack shape, more than one basic block, or
// an opcode the arch lowerer does not statically support) so the caller falls
// back to the existing trace-based Compile path. A multi-block function is
// Phase 3 work: lowerCFG would need each block's entry height reconciled
// across every branch edge before it could jump between native blocks, and
// only blockHeights' side of that (computing the heights) exists yet.
func (c *compiler) compileCFG(i *Interpreter, addr int, fn *types.Function) (*module, bool, error) {
	mod := &module{entries: map[anchor]native{}}
	if fn == nil || len(fn.Code) == 0 {
		return mod, false, nil
	}
	if len(c.scratchRegs) < scratchCount {
		return mod, false, nil
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
	// Phase 3: see the doc comment above — a multi-block function stays on
	// the trace-based Compile path until branch lowering exists here.
	if len(blocks) != 1 {
		return mod, false, nil
	}
	if lastInstruction(fn, blocks[0]).Opcode() != instr.RETURN {
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

	ctx := &lowering{
		assembler: asmb,
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

	if !c.lowerer.lowerCFG(ctx, blocks[0]) {
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
	mod.entries[a] = native{callable: linked[0].Callable, loop: false}
	mod.emits++
	mod.bytes += len(code.Bytes)
	return mod, true, nil
}

// lastInstruction decodes b's terminal instruction, the one compileCFG must
// confirm is RETURN before accepting b as Phase 2's supported shape.
func lastInstruction(fn *types.Function, b *analysis.BasicBlock) instr.Instruction {
	var last instr.Instruction
	for ip := b.Start; ip < b.End; {
		last = instr.Instruction(fn.Code[ip:])
		ip += last.Width()
	}
	return last
}
