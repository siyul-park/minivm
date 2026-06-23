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
	Handlers  []instr.Handler
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

// WithHandlers attaches the exception table for the top-level code (slot 0).
func WithHandlers(handlers ...instr.Handler) func(*Program) {
	return func(p *Program) {
		p.Handlers = handlers
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
		sb.WriteString("\n")
		writeIndexed(&sb, p.Constants)
	}
	if len(p.Types) > 0 {
		sb.WriteString("\n")
		writeIndexed(&sb, p.Types)
	}
	return sb.String()
}

func writeIndexed[T fmt.Stringer](sb *strings.Builder, items []T) {
	for i, item := range items {
		head, tail, _ := strings.Cut(item.String(), "\n")
		sb.WriteString(fmt.Sprintf("%04d:\t%s\n", i, head))
		for tail != "" {
			var line string
			line, tail, _ = strings.Cut(tail, "\n")
			sb.WriteString(fmt.Sprintf("\t%s\n", line))
		}
	}
}
