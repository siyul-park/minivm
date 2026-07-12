//go:build arm64

package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
)

// lowerCFG lowers block — the single straight-line, RETURN-terminated basic
// block compileCFG has already confirmed fn's whole body reduces to — into one
// framed native callable. Unlike lower, there is no recorded trace: every step
// is decoded straight from bytecode, so only opcodes whose lowering needs
// nothing but that static decode are supported. An opcode the switch does not
// name — including CALL, DIV/REM (their fast path wants an observed divisor),
// and every heap op — rejects the whole block; Phase 2 has no deopt stub to
// fall back to mid-block, only the same clean "leave threaded dispatch
// installed" compileCFG already returns for every other rejection.
func (l arm64Lowerer) lowerCFG(ctx *lowering, block *analysis.BasicBlock) bool {
	l.enter(ctx)
	f := ctx.frame()
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(f.code[ip:])
		op := step{op: inst.Opcode(), fn: ctx.addr, ip: ip}

		ok := false
		switch op.op {
		case instr.NOP:
			ok = true
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
		case instr.DUP:
			ok = l.dup(ctx)
		case instr.SWAP:
			ok = l.swap(ctx)
		case instr.DROP:
			ok = l.drop(ctx, op)
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
		case instr.RETURN:
			// compileCFG only calls in once it has confirmed block's terminal
			// instruction is RETURN and there is exactly one frame (compileCFG
			// builds a single activation and this lowering never pushes a call
			// frame), so ret's entry-frame teardown is always the right close.
			return l.ret(ctx)
		default:
			return false
		}
		if !ok {
			return false
		}
		ip += inst.Width()
	}
	return false
}
