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
	typs := prog.Types

	constIndex := make([]int, len(constants))
	typeIndex := make([]int, len(typs))
	for i := 0; i < len(constIndex); i++ {
		constIndex[i] = -1
	}
	for i := 0; i < len(typeIndex); i++ {
		typeIndex[i] = -1
	}

	for _, code := range codes {
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				idx := inst.Operand(0)
				constIndex[idx] = int(idx)
			case instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT, instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT:
				idx := inst.Operand(0)
				typeIndex[idx] = int(idx)
			default:
			}
			ip += inst.Width()
		}
	}

	for i := 0; i < len(constants); i++ {
		if constIndex[i] == -1 {
			continue
		}
		for j := i + 1; j < len(constants); j++ {
			if constants[j] == constants[i] {
				constIndex[j] = constIndex[i]
			}
		}
	}
	for i := 0; i < len(typs); i++ {
		if typeIndex[i] == -1 {
			continue
		}
		for j := i + 1; j < len(typs); j++ {
			if typs[j].Equals(typs[i]) {
				typeIndex[j] = typeIndex[i]
			}
		}
	}

	constSize := 0
	typesSize := 0
	for i := 0; i < len(constIndex); i++ {
		if constIndex[i] != -1 {
			if constIndex[i] != i {
				constIndex[i] = constIndex[constIndex[i]]
			} else {
				constIndex[i] = constSize
				constSize++
			}
		}
	}
	for i := 0; i < len(typeIndex); i++ {
		if typeIndex[i] != -1 {
			if typeIndex[i] != i {
				typeIndex[i] = typeIndex[typeIndex[i]]
			} else {
				typeIndex[i] = typesSize
				typesSize++
			}
		}
	}

	for i, v := range constIndex {
		if v >= 0 {
			constants[v] = constants[i]
		}
	}
	for i, v := range typeIndex {
		if v >= 0 {
			typs[v] = typs[i]
		}
	}

	constants = constants[:constSize]
	typs = typs[:typesSize]
	if len(constants) == 0 {
		constants = nil
	}
	if len(typs) == 0 {
		typs = nil
	}

	for _, code := range codes {
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				idx := inst.Operand(0)
				inst.SetOperand(0, uint64(constIndex[idx]))
			case instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT, instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT:
				idx := inst.Operand(0)
				inst.SetOperand(0, uint64(typeIndex[idx]))
			default:
			}
			ip += inst.Width()
		}
	}

	prog.Constants = constants
	prog.Types = typs

	return prog, nil
}
