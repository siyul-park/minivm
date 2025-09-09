package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type Function struct {
	Code    []byte
	Params  []Kind
	Returns []Kind
	Locals  []Kind
}

var _ Value = (*Function)(nil)

func FunctionWithParams(vals ...Kind) func(*Function) {
	return func(function *Function) {
		function.Params = vals
	}
}

func FunctionWithReturns(vals ...Kind) func(*Function) {
	return func(function *Function) {
		function.Returns = vals
	}
}

func FunctionWithLocals(vals ...Kind) func(*Function) {
	return func(function *Function) {
		function.Locals = vals
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
