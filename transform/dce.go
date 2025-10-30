package transform

import (
	"fmt"

	"github.com/siyul-park/minivm/types"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
)

type DeadCodeEliminationPass struct{}

var _ pass.Pass[*program.Program] = (*DeadCodeEliminationPass)(nil)

func NewDeadCodeEliminationPass() *DeadCodeEliminationPass {
	return &DeadCodeEliminationPass{}
}

func (p *DeadCodeEliminationPass) Run(m *pass.Manager) (*program.Program, error) {
	var prog *program.Program
	if err := m.Load(&prog); err != nil {
		return nil, err
	}

	var fns []*types.Function
	fns = append(fns, &types.Function{
		Signature: types.NewFunctionSignature(),
		Code:      prog.Code,
	})
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, fn)
		}
	}

	for i, fn := range fns {
		code := fn.Code

		var blocks []*analysis.BasicBlock
		if err := m.Convert(fn, &blocks); err != nil {
			return nil, err
		}
		for i := 1; i < len(blocks); i++ {
			blk := blocks[i]
			if len(blk.Preds) == 0 {
				for j := blk.Start; j < blk.End; j++ {
					code[j] = byte(instr.UNREACHABLE)
				}
			}
		}

		offsets := make([]int, len(code))
		for j := range offsets {
			offsets[j] = -1
		}

		write := 0
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

		read := 0
		write = 0
		for write < len(code) {
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
			for ; read < len(offsets) && offsets[read] != write; read++ {
			}
		}

		fn.Code = code
		if i == 0 {
			prog.Code = code
		}
	}

	return prog, nil
}
