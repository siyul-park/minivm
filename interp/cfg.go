package interp

import (
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

	ctx := c.newLowering(i, addr, fn, c.arch)
	globals := ctx.globals
	facts, ok := blockFacts(fn, blocks, i.constants, globals, i.heap)
	if !ok {
		return mod, false, nil
	}
	kinds := make([][]types.Kind, len(facts))
	for block, state := range facts {
		kinds[block] = make([]types.Kind, len(state))
		for slot, fact := range state {
			kinds[block][slot] = fact.kind
		}
	}
	labels := make([]asm.Label, len(blocks))
	for j := range labels {
		labels[j] = ctx.assembler.Label()
	}

	if !c.lowerer.lowerCFG(ctx, blocks, kinds, labels) {
		return mod, false, nil
	}
	ok, err = c.publish(mod, anchor{addr: addr, ip: 0}, ctx, c.arch, native{cfg: true})
	return mod, ok, err
}
