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
	Types     []types.Type
}

func WithConstants(consts ...types.Value) func(*Program) {
	return func(p *Program) {
		p.Constants = consts
	}
}

func WithTypes(types ...types.Type) func(*Program) {
	return func(p *Program) {
		p.Types = types
	}
}

func New(instrs []instr.Instruction, options ...func(*Program)) *Program {
	p := &Program{Code: instr.Marshal(instrs)}
	for _, opt := range options {
		opt(p)
	}
	return p
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
	if len(p.Types) > 0 {
		sb.WriteString("\n")
		for i, t := range p.Types {
			lines := strings.Split(t.String(), "\n")
			if len(lines) > 0 {
				sb.WriteString(fmt.Sprintf("%d:\t%s\n", i, lines[0]))
				for _, line := range lines[1:] {
					sb.WriteString(fmt.Sprintf("\t%s\n", line))
				}
			}
		}
	}
	return sb.String()
}
