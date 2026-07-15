package transform

import (
	"math/bits"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
)

// AlgebraicPass rewrites integer peepholes whose right operand is
// a constant: identity operations are dropped and multiply/divide by a power of
// two become shifts. It only touches I32/I64 — float identities are unsound
// under IEEE-754 (NaN, signed zero) — and skips annihilators such as x*0 and
// x&0, which would need to drop the live left operand.
type AlgebraicPass struct{}

var _ pass.Pass[*program.Program] = (*AlgebraicPass)(nil)

func NewAlgebraicPass() *AlgebraicPass {
	return &AlgebraicPass{}
}

func (p *AlgebraicPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
	for _, fn := range functions(prog) {
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		if err != nil {
			return pass.PreserveNone(), err
		}

		for _, blk := range blocks {
			for ip := blk.Start; ip < blk.End; {
				konst := instr.Instruction(fn.Code[ip:])
				w0 := konst.Width()
				if ip+w0 >= blk.End {
					ip += w0
					continue
				}

				w1 := instr.Instruction(fn.Code[ip+w0:]).Width()
				if p.simplify(fn.Code, ip, konst) {
					ip += w0 + w1
					continue
				}
				ip += w0
			}
		}
	}
	return pass.PreserveNone(), nil
}

// simplify rewrites the window [konst][op] when konst is the operation's right
// operand and the pair reduces to the left operand or a shift. It reports
// whether a rewrite happened.
func (p *AlgebraicPass) simplify(code []byte, ip int, konst instr.Instruction) bool {
	op := instr.Instruction(code[ip+konst.Width():])

	switch op.Opcode() {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_OR, instr.I32_XOR,
		instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U:
		return konst.Opcode() == instr.I32_CONST && int32(konst.Operand(0)) == 0 && p.drop(code, ip, konst, op)
	case instr.I64_ADD, instr.I64_SUB, instr.I64_OR, instr.I64_XOR,
		instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U:
		return konst.Opcode() == instr.I64_CONST && int64(konst.Operand(0)) == 0 && p.drop(code, ip, konst, op)
	case instr.I32_AND:
		return konst.Opcode() == instr.I32_CONST && int32(konst.Operand(0)) == -1 && p.drop(code, ip, konst, op)
	case instr.I64_AND:
		return konst.Opcode() == instr.I64_CONST && int64(konst.Operand(0)) == -1 && p.drop(code, ip, konst, op)

	case instr.I32_MUL:
		if konst.Opcode() != instr.I32_CONST {
			return false
		}
		v := int32(konst.Operand(0))
		if v == 1 {
			return p.drop(code, ip, konst, op)
		}
		if n, ok := p.log2(uint64(uint32(v))); ok {
			return p.shift(code, ip, konst, op, n, instr.I32_SHL)
		}
	case instr.I64_MUL:
		if konst.Opcode() != instr.I64_CONST {
			return false
		}
		v := konst.Operand(0)
		if int64(v) == 1 {
			return p.drop(code, ip, konst, op)
		}
		if n, ok := p.log2(v); ok {
			return p.shift(code, ip, konst, op, n, instr.I64_SHL)
		}

	case instr.I32_DIV_S, instr.I32_DIV_U:
		if konst.Opcode() != instr.I32_CONST {
			return false
		}
		v := int32(konst.Operand(0))
		if v == 1 {
			return p.drop(code, ip, konst, op)
		}
		if n, ok := p.log2(uint64(uint32(v))); ok && op.Opcode() == instr.I32_DIV_U {
			return p.shift(code, ip, konst, op, n, instr.I32_SHR_U)
		}
	case instr.I64_DIV_S, instr.I64_DIV_U:
		if konst.Opcode() != instr.I64_CONST {
			return false
		}
		v := konst.Operand(0)
		if int64(v) == 1 {
			return p.drop(code, ip, konst, op)
		}
		if n, ok := p.log2(v); ok && op.Opcode() == instr.I64_DIV_U {
			return p.shift(code, ip, konst, op, n, instr.I64_SHR_U)
		}
	}
	return false
}

// drop replaces the [konst][op] window with NOPs, leaving the left operand.
func (p *AlgebraicPass) drop(code []byte, ip int, konst, op instr.Instruction) bool {
	end := ip + konst.Width() + op.Width()
	for i := ip; i < end; i++ {
		code[i] = byte(instr.NOP)
	}
	return true
}

// shift rewrites [konst][mul|div] into [konst n][shift], turning a power-of-two
// multiply or divide into a shift by n.
func (p *AlgebraicPass) shift(code []byte, ip int, konst, op instr.Instruction, n uint64, opcode instr.Opcode) bool {
	konst.SetOperand(0, n)
	code[ip+konst.Width()] = byte(opcode)
	return true
}

// log2 returns the exponent of v when v is a power of two greater than one.
func (p *AlgebraicPass) log2(v uint64) (uint64, bool) {
	if v < 2 || v&(v-1) != 0 {
		return 0, false
	}
	return uint64(bits.TrailingZeros64(v)), true
}
