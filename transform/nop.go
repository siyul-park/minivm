package transform

import (
	"fmt"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type NOPEliminationPass struct{}

var _ pass.Pass[*program.Program] = (*NOPEliminationPass)(nil)

func NewNOPEliminationPass() *NOPEliminationPass {
	return &NOPEliminationPass{}
}

func (p *NOPEliminationPass) Run(m *pass.Manager) (*program.Program, error) {
	var prog *program.Program
	if err := m.Load(&prog); err != nil {
		return nil, err
	}

	var codes []*[]byte
	codes = append(codes, &prog.Code)
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			codes = append(codes, &fn.Code)
		}
	}

	for _, ptr := range codes {
		code := *ptr
		write := 0

		offsets := make([]int, len(code))
		for i := range offsets {
			offsets[i] = -1
		}

		for read := 0; read < len(code); {
			inst := instr.Instruction(code[read:])
			width := inst.Width()
			if inst.Opcode() != instr.NOP && inst.Opcode() != instr.UNREACHABLE {
				offsets[read] = write
				if write != read {
					copy(code[write:write+width], code[read:read+width])
				}
				write += width
			}
			read += width
		}

		code = code[:write]
		if len(code) == 0 {
			code = nil
		}
		*ptr = code

		read := 0
		write = 0
		for read < len(code) {
			inst := instr.Instruction(code[write:])

			switch inst.Opcode() {
			case instr.BR, instr.BR_IF:
				offset := read + int(inst.Operand(0)) + inst.Width()
				for offset < len(offsets) && offsets[offset] == -1 {
					offset++
				}
				if offset >= len(offsets) {
					return nil, fmt.Errorf("%w: at=%d", analysis.ErrInvalidJump, read)
				}
				offset = offsets[offset] - write - inst.Width()
				inst.SetOperand(0, uint64(offset))
			case instr.BR_TABLE:
				width := inst.Width()
				operands := inst.Operands()
				count := int(operands[0])
				for j := 0; j <= count; j++ {
					offset := read + int(operands[j+1]) + width
					for offset < len(offsets) && offsets[offset] == -1 {
						offset++
					}
					if offset >= len(offsets) {
						return nil, fmt.Errorf("%w: at=%d", analysis.ErrInvalidJump, read)
					}
					offset = offsets[offset] - write - width
					inst.SetOperand(j+1, uint64(offset))
				}
				offset := read + int(operands[len(operands)-1]) + width
				offset = offsets[offset] - write - width
				inst.SetOperand(len(operands)-1, uint64(offset))
			default:
			}

			write += inst.Width()
			for ; read < len(offsets) && offsets[read] < write; read++ {
			}
		}
	}

	return prog, nil
}
