package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type Function struct {
	Code    []byte
	Params  int
	Returns int
	Locals  int
}

type Closure struct {
	Function *Function
	Free     []Value
}

var _ Value = (*Function)(nil)
var _ Value = (*Closure)(nil)

func NewFunction(instrs []instr.Instruction, params, returns, locals int) *Function {
	return &Function{
		Code:    instr.Marshal(instrs),
		Params:  params,
		Returns: returns,
		Locals:  locals,
	}
}

func (f Function) Interface() any {
	return f
}

func (f Function) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\t.params %d\n", f.Params))
	sb.WriteString(fmt.Sprintf("\t.returns %d\n", f.Returns))
	sb.WriteString(fmt.Sprintf("\t.locals: %d\n", f.Locals))

	ip := 0
	for _, b := range f.Code {
		sb.WriteString(fmt.Sprintf("%04d:\t0x%02X\n", ip, b))
		ip++
	}

	return sb.String()
}

func (c *Closure) Interface() any {
	return c
}

func (c *Closure) String() string {
	return c.Function.String()
}
