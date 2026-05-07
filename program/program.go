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
	sb.WriteString(instr.Format(p.Code))
	if len(p.Constants) > 0 {
		writeIndexed(&sb, p.Constants)
	}
	if len(p.Types) > 0 {
		writeIndexed(&sb, p.Types)
	}
	return sb.String()
}

func writeIndexed[T fmt.Stringer](sb *strings.Builder, items []T) {
	sb.WriteString("\n")
	for i, item := range items {
		lines := strings.Split(item.String(), "\n")
		sb.WriteString(fmt.Sprintf("%04d:\t%s\n", i, lines[0]))
		for _, line := range lines[1:] {
			sb.WriteString(fmt.Sprintf("\t%s\n", line))
		}
	}
}
