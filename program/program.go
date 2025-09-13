package program

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/types"

	"github.com/siyul-park/minivm/instr"
)

type Program struct {
	Code      []byte
	Constants []types.Value
}

func New(instrs []instr.Instruction, consts []types.Value) *Program {
	return &Program{
		Code:      instr.Marshal(instrs),
		Constants: consts,
	}
}

func (p *Program) String() string {
	var sb strings.Builder

	sb.WriteString(instr.Disassemble(p.Code))

	if len(p.Constants) > 0 {
		sb.WriteString("\n")

		for i, c := range p.Constants {
			lines := strings.Split(c.String(), "\n")
			if len(lines) > 0 {
				sb.WriteString(fmt.Sprintf("%04d:\t%s\n", i, lines[0]))
				for _, line := range lines[1:] {
					sb.WriteString(fmt.Sprintf("\t%s\n", line))
				}
			}
		}
	}
	return sb.String()
}
