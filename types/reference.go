package types

import (
	"fmt"
	"strings"
)

type Function struct {
	Code    []byte
	Params  int
	Locals  int
	Returns int
}

type Closure struct {
	Function *Function
	Free     []Value
}

var _ Value = (*Function)(nil)
var _ Value = (*Closure)(nil)

func (f Function) Interface() any {
	return f
}

func (f Function) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\t.params %d\n", f.Params))
	sb.WriteString(fmt.Sprintf("\t.locals: %d\n", f.Locals))
	sb.WriteString(fmt.Sprintf("\t.returns %d\n", f.Returns))

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
