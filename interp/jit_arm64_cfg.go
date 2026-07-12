//go:build arm64

package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

const cfgTableLimit = 32

// lowerCFG lowers every basic block with canonical VM stack state at each edge.
// Unsupported instructions end their block with an exact-IP fallback instead
// of rejecting the whole function.
func (l arm64Lowerer) lowerCFG(ctx *lowering, blocks []*analysis.BasicBlock, kinds [][]types.Kind, labels []asm.Label) bool {
	if len(blocks) == 0 || len(blocks) != len(kinds) || len(blocks) != len(labels) {
		return false
	}
	l.enter(ctx)
	index := make(map[int]int, len(blocks))
	for idx, block := range blocks {
		index[block.Start] = idx
	}

	for idx, block := range blocks {
		ctx.assembler.Bind(labels[idx])
		l.cfgEntry(ctx, kinds[idx])
		if !l.cfgBlock(ctx, block, labels, index) {
			return false
		}
	}
	for _, exit := range ctx.exits {
		ctx.values = exit.values
		ctx.frames = exit.frames
		ctx.assembler.Bind(exit.label)
		l.trapFlushed(ctx, trapFallback, exit.resume)
	}
	return true
}

func (l arm64Lowerer) cfgEntry(ctx *lowering, kinds []types.Kind) {
	ctx.values = ctx.values[:0]
	for _, kind := range kinds {
		ctx.values = append(ctx.values, value{kind: kind})
	}
	f := ctx.frame()
	clear(f.loaded)
	clear(f.dirty)
	l.reload(ctx)
}

func (l arm64Lowerer) cfgBlock(ctx *lowering, block *analysis.BasicBlock, labels []asm.Label, index map[int]int) bool {
	f := ctx.frame()
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(f.code[ip:])
		op := step{op: inst.Opcode(), fn: ctx.addr, ip: ip}
		next := ip + inst.Width()

		switch op.op {
		case instr.BR:
			if !l.flush(ctx, false) {
				return false
			}
			return l.cfgEdge(ctx, instr.Targets(f.code, ip)[0], block.Start, labels, index)
		case instr.BR_IF:
			return l.cfgIf(ctx, op, block.Start, next, labels, index)
		case instr.BR_TABLE:
			return l.cfgTable(ctx, op, block.Start, labels, index)
		case instr.RETURN:
			return l.ret(ctx)
		}

		ok, terminal := l.cfgOp(ctx, op)
		if !ok {
			return false
		}
		if terminal {
			return true
		}
		ip = next
	}

	if !l.flush(ctx, false) {
		return false
	}
	return l.cfgEdge(ctx, block.End, block.Start, labels, index)
}

func (l arm64Lowerer) cfgOp(ctx *lowering, op step) (bool, bool) {
	ok := false
	switch op.op {
	case instr.NOP:
		ok = true
	case instr.UNREACHABLE:
		return l.exit(ctx, op.ip), true
	case instr.I32_CONST:
		ok = l.i32Const(ctx, op)
	case instr.I64_CONST:
		ok = l.i64Const(ctx, op)
	case instr.F32_CONST:
		ok = l.f32Const(ctx, op)
	case instr.F64_CONST:
		ok = l.f64Const(ctx, op)
	case instr.CONST_GET:
		ok = l.constGet(ctx, op)
	case instr.LOCAL_GET:
		ok = l.localGet(ctx, op)
	case instr.LOCAL_SET:
		ok = l.localSet(ctx, op, true)
	case instr.LOCAL_TEE:
		ok = l.localSet(ctx, op, false)
	case instr.GLOBAL_GET:
		ok = l.globalGet(ctx, op)
	case instr.GLOBAL_SET:
		ok = l.globalSet(ctx, op, true)
	case instr.GLOBAL_TEE:
		ok = l.globalSet(ctx, op, false)
	case instr.DROP:
		ok = l.drop(ctx, op)
	case instr.DUP:
		ok = l.dup(ctx)
	case instr.SWAP:
		ok = l.swap(ctx)
	case instr.SELECT:
		ok = l.selectOp(ctx)
	case instr.I32_ADD:
		ok = l.i32Binary(ctx, arm64.ADD)
	case instr.I32_SUB:
		ok = l.i32Binary(ctx, arm64.SUB)
	case instr.I32_MUL:
		ok = l.i32Binary(ctx, arm64.MUL)
	case instr.I32_AND:
		ok = l.i32Bitwise(ctx, arm64.AND)
	case instr.I32_OR:
		ok = l.i32Bitwise(ctx, arm64.ORR)
	case instr.I32_XOR:
		ok = l.i32Bitwise(ctx, arm64.EOR)
	case instr.I32_EQZ:
		ok = l.i32Eqz(ctx)
	case instr.I32_EQ:
		ok = l.i32Cmp(ctx, arm64.CondEQ)
	case instr.I32_NE:
		ok = l.i32Cmp(ctx, arm64.CondNE)
	case instr.I32_LT_S:
		ok = l.i32Cmp(ctx, arm64.CondLT)
	case instr.I32_LE_S:
		ok = l.i32Cmp(ctx, arm64.CondLE)
	case instr.I32_GT_S:
		ok = l.i32Cmp(ctx, arm64.CondGT)
	case instr.I32_GE_S:
		ok = l.i32Cmp(ctx, arm64.CondGE)
	case instr.I32_LT_U:
		ok = l.i32Cmp(ctx, arm64.CondCC)
	case instr.I32_LE_U:
		ok = l.i32Cmp(ctx, arm64.CondLS)
	case instr.I32_GT_U:
		ok = l.i32Cmp(ctx, arm64.CondHI)
	case instr.I32_GE_U:
		ok = l.i32Cmp(ctx, arm64.CondCS)
	case instr.I64_ADD:
		ok = l.i64Binary(ctx, op, arm64.ADD, true)
	case instr.I64_SUB:
		ok = l.i64Binary(ctx, op, arm64.SUB, true)
	case instr.I64_MUL:
		ok = l.i64Binary(ctx, op, arm64.MUL, true)
	case instr.I64_AND:
		ok = l.i64Binary(ctx, op, arm64.AND, false)
	case instr.I64_OR:
		ok = l.i64Binary(ctx, op, arm64.ORR, false)
	case instr.I64_XOR:
		ok = l.i64Binary(ctx, op, arm64.EOR, false)
	case instr.I64_EQZ:
		ok = l.i64Eqz(ctx)
	case instr.I64_EQ:
		ok = l.i64Cmp(ctx, arm64.CondEQ)
	case instr.I64_NE:
		ok = l.i64Cmp(ctx, arm64.CondNE)
	case instr.I64_LT_S:
		ok = l.i64Cmp(ctx, arm64.CondLT)
	case instr.I64_LE_S:
		ok = l.i64Cmp(ctx, arm64.CondLE)
	case instr.I64_GT_S:
		ok = l.i64Cmp(ctx, arm64.CondGT)
	case instr.I64_GE_S:
		ok = l.i64Cmp(ctx, arm64.CondGE)
	case instr.I64_LT_U:
		ok = l.i64Cmp(ctx, arm64.CondCC)
	case instr.I64_LE_U:
		ok = l.i64Cmp(ctx, arm64.CondLS)
	case instr.I64_GT_U:
		ok = l.i64Cmp(ctx, arm64.CondHI)
	case instr.I64_GE_U:
		ok = l.i64Cmp(ctx, arm64.CondCS)
	case instr.F32_ADD:
		ok = l.f32Binary(ctx, arm64.FADD)
	case instr.F32_SUB:
		ok = l.f32Binary(ctx, arm64.FSUB)
	case instr.F32_MUL:
		ok = l.f32Binary(ctx, arm64.FMUL)
	case instr.F32_DIV:
		ok = l.f32Binary(ctx, arm64.FDIV)
	case instr.F32_EQ:
		ok = l.f32Cmp(ctx, arm64.CondEQ)
	case instr.F32_NE:
		ok = l.f32Cmp(ctx, arm64.CondNE)
	case instr.F32_LT:
		ok = l.f32Cmp(ctx, arm64.CondMI)
	case instr.F32_GT:
		ok = l.f32Cmp(ctx, arm64.CondGT)
	case instr.F32_LE:
		ok = l.f32Cmp(ctx, arm64.CondLS)
	case instr.F32_GE:
		ok = l.f32Cmp(ctx, arm64.CondGE)
	case instr.F64_ADD:
		ok = l.f64Binary(ctx, arm64.FADD)
	case instr.F64_SUB:
		ok = l.f64Binary(ctx, arm64.FSUB)
	case instr.F64_MUL:
		ok = l.f64Binary(ctx, arm64.FMUL)
	case instr.F64_DIV:
		ok = l.f64Binary(ctx, arm64.FDIV)
	case instr.F64_EQ:
		ok = l.f64Cmp(ctx, arm64.CondEQ)
	case instr.F64_NE:
		ok = l.f64Cmp(ctx, arm64.CondNE)
	case instr.F64_LT:
		ok = l.f64Cmp(ctx, arm64.CondMI)
	case instr.F64_GT:
		ok = l.f64Cmp(ctx, arm64.CondGT)
	case instr.F64_LE:
		ok = l.f64Cmp(ctx, arm64.CondLS)
	case instr.F64_GE:
		ok = l.f64Cmp(ctx, arm64.CondGE)
	default:
		return l.exit(ctx, op.ip), true
	}
	return ok, false
}

func (l arm64Lowerer) cfgIf(ctx *lowering, op step, from, nextIP int, labels []asm.Label, index map[int]int) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	cond := ctx.pop()
	if !l.flush(ctx, false) {
		return false
	}
	targets := instr.Targets(ctx.frame().code, op.ip)
	if len(targets) != 1 {
		return false
	}
	taken := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CBNZLabel(l.narrow32(cond.reg), taken))
	if !l.cfgEdge(ctx, nextIP, from, labels, index) {
		return false
	}
	ctx.assembler.Bind(taken)
	return l.cfgEdge(ctx, targets[0], from, labels, index)
}

func (l arm64Lowerer) cfgTable(ctx *lowering, op step, from int, labels []asm.Label, index map[int]int) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	code := ctx.frame().code
	count := int(code[op.ip+1])
	if count > cfgTableLimit {
		return false
	}
	targets := instr.Targets(code, op.ip)
	if len(targets) != count+1 {
		return false
	}
	cond := ctx.pop()
	cond32 := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(cond32, cond.reg, maskI32))
	if !l.flush(ctx, false) {
		return false
	}
	paths := make([]asm.Label, len(targets))
	for idx := range paths {
		paths[idx] = ctx.assembler.Label()
	}
	for idx := 0; idx < count; idx++ {
		ctx.assembler.Emit(arm64.CMPI(cond32, uint16(idx)))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, paths[idx]))
	}
	ctx.assembler.Emit(arm64.BLabel(paths[count]))
	for idx, label := range paths {
		ctx.assembler.Bind(label)
		if !l.cfgEdge(ctx, targets[idx], from, labels, index) {
			return false
		}
	}
	return true
}

func (l arm64Lowerer) cfgEdge(ctx *lowering, target, from int, labels []asm.Label, index map[int]int) bool {
	if target == len(ctx.frame().code) {
		return l.ret(ctx)
	}
	idx, ok := index[target]
	if !ok {
		return false
	}
	if target > from {
		ctx.assembler.Emit(arm64.BLabel(labels[idx]))
		return true
	}

	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.SUBI(budget, budget, 1))
	a.Emit(arm64.STR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.CBNZLabel(budget, labels[idx]))
	l.trapFlushed(ctx, trapYield, target)
	return true
}
