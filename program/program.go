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

	var fns []*types.Function
	var idx []int
	for i, c := range p.Constants {
		if fn, ok := c.(*types.Function); ok {
			fns = append(fns, fn)
			idx = append(idx, i)
		}
	}

	sb.WriteString(".text\n")
	sb.WriteString("main:\n")
	ip := 0
	for _, inst := range instr.Unmarshal(p.Code) {
		if inst == nil {
			sb.WriteString(fmt.Sprintf("%04d: <invalid>\n", ip))
			break
		}
		sb.WriteString(fmt.Sprintf("%04d:\t%s\n", ip, inst.String()))
		ip += len(inst)
	}
	for i, fn := range fns {
		sb.WriteString(fmt.Sprintf("%04d:\n", idx[i]))
		sb.WriteString(fn.String())
	}
	sb.WriteString("\n")

	sb.WriteString(".data\n")
	for i, c := range p.Constants {
		if _, ok := c.(*types.Function); !ok {
			sb.WriteString(fmt.Sprintf("%04d: %s\n", i, c.String()))
		}
	}

	return sb.String()
}
