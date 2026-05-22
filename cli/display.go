package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/types"
)

// printStack writes the operand stack to out in top-down order. An empty
// stack produces no output, matching the REPL's behavior.
func printStack(out io.Writer, vm *interp.Interpreter) {
	n := vm.Len()
	if n == 0 {
		return
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		v, _ := vm.Peek(i)
		parts[n-1-i] = formatValue(v, vm)
	}
	fmt.Fprintln(out, strings.Join(parts, " "))
}

// formatValue renders a boxed value. KindRef values are resolved through
// the interpreter heap; multi-line String() output is truncated to its
// first line; integer/float types carry a type suffix to disambiguate
// kinds that share a textual form.
func formatValue(v types.Boxed, vm *interp.Interpreter) string {
	switch v.Kind() {
	case types.KindI32:
		return fmt.Sprintf("%d", v.I32())
	case types.KindI64:
		return fmt.Sprintf("%d (i64)", v.I64())
	case types.KindF32:
		return fmt.Sprintf("%g (f32)", v.F32())
	case types.KindF64:
		return fmt.Sprintf("%g (f64)", v.F64())
	case types.KindRef:
		val, err := vm.Load(v.Ref())
		if err != nil || val == nil {
			return "null"
		}
		s := val.String()
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[:i]
		}
		return s
	default:
		return "<invalid>"
	}
}
