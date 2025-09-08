package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type Function struct {
	Code     []byte
	Params   int
	Returns  int
	Locals   int
	Captures int
}

type Closure struct {
	Function *Function
	Captures []Boxed
}

var _ Value = (*Function)(nil)
var _ Traceable = (*Closure)(nil)

func FunctionWithParams(val int) func(*Function) {
	return func(function *Function) {
		function.Params = val
	}
}

func FunctionWithReturns(val int) func(*Function) {
	return func(function *Function) {
		function.Returns = val
	}
}

func FunctionWithLocals(val int) func(*Function) {
	return func(function *Function) {
		function.Locals = val
	}
}

func FunctionWithCaptures(val int) func(*Function) {
	return func(function *Function) {
		function.Captures = val
	}
}

func NewFunction(instrs []instr.Instruction, opts ...func(*Function)) *Function {
	fn := &Function{Code: instr.Marshal(instrs)}
	for _, opt := range opts {
		opt(fn)
	}
	return fn
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
	for _, inst := range instr.Unmarshal(f.Code) {
		if inst == nil {
			sb.WriteString(fmt.Sprintf("%04d: <invalid>\n", ip))
			break
		}
		sb.WriteString(fmt.Sprintf("%04d:\t%s\n", ip, inst.String()))
		ip += len(inst)
	}
	return sb.String()
}

func (c *Closure) Interface() any {
	return c
}

func (c *Closure) String() string {
	return c.Function.String()
}

func (c *Closure) Refs() []Ref {
	refs := make([]Ref, 0, len(c.Captures))
	for _, v := range c.Captures {
		if v.Kind() == KindRef {
			refs = append(refs, Ref(v.Ref()))
		}
	}
	return refs
}
