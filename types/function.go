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

func FunctionWithParams(kinds ...Kind) func(*Function) {
	return func(function *Function) {
		function.Params = kinds
	}
}

func FunctionWithReturns(kinds ...Kind) func(*Function) {
	return func(function *Function) {
		function.Returns = kinds
	}
}

func FunctionWithLocals(kinds ...Kind) func(*Function) {
	return func(function *Function) {
		function.Locals = kinds
	}
}

func NewFunction(instrs []instr.Instruction, opts ...func(*Function)) *Function {
	fn := &Function{Code: instr.Marshal(instrs)}
	for _, opt := range opts {
		opt(fn)
	}
	return fn
}

func (f Function) Kind() Kind {
	return KindRef
}

func (f Function) Interface() any {
	return f
}

func (f Function) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(".params %v, .returns %v, .locals: %v\n", f.Params, f.Returns, f.Locals))
	sb.WriteString(instr.Disassemble(f.Code))
	return sb.String()
}
