package program

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type Program struct {
	Code []byte
}

func New(instrs ...instr.Instruction) *Program {
	return &Program{Code: instr.Marshal(instrs)}
}

func (p *Program) String() string {
	var sb strings.Builder
	ip := 0
	for _, inst := range instr.Unmarshal(p.Code) {
		if inst == nil {
			sb.WriteString(fmt.Sprintf("%04d: <invalid>\n", ip))
			break
		}
		sb.WriteString(fmt.Sprintf("%04d: %s\n", ip, inst.String()))
		ip += len(inst)
	}
	return sb.String()
}

func (p *Program) Size() int {
	return len(p.Code)
}
