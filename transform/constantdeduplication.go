package transform

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type ConstantDeduplicationPass struct{}

var _ pass.Pass[*program.Program] = (*ConstantDeduplicationPass)(nil)

func NewConstantDeduplicationPass() *ConstantDeduplicationPass {
	return &ConstantDeduplicationPass{}
}

func (p *ConstantDeduplicationPass) Run(m *pass.Manager) (*program.Program, error) {
	var prog *program.Program
	if err := m.Load(&prog); err != nil {
		return nil, err
	}

	var codes [][]byte
	codes = append(codes, prog.Code)
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			codes = append(codes, fn.Code)
		}
	}

	constants := prog.Constants

	index := make([]int, len(constants))
	for i := 0; i < len(index); i++ {
		index[i] = -1
	}

	for _, code := range codes {
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				idx := inst.Operand(0)
				index[idx] = int(idx)
			default:
			}
			ip += inst.Width()
		}
	}

	for i := 0; i < len(constants); i++ {
		if index[i] == -1 {
			continue
		}
		for j := i; j < len(constants); j++ {
			if constants[j] == constants[i] {
				index[j] = index[i]
			}
		}
	}

	idx := 0
	for i := 0; i < len(index); i++ {
		if index[i] != -1 {
			if index[i] != i {
				index[i] = index[index[i]]
			} else {
				index[i] = idx
				idx++
			}
		}
	}

	for _, code := range codes {
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				idx := inst.Operand(0)
				inst.SetOperand(0, uint64(index[idx]))
			default:
			}
			ip += inst.Width()
		}
	}

	for i, v := range index {
		if v >= 0 {
			constants[v] = constants[i]
		}
	}

	constants = constants[:idx]
	if len(constants) == 0 {
		constants = nil
	}
	prog.Constants = constants

	return prog, nil
}
