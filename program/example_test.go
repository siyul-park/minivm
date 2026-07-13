package program_test

import (
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
)

func ExampleNewBuilder() {
	builder := program.NewBuilder()
	builder.Emit(instr.I32_CONST, 42)

	prog, err := builder.Build()
	fmt.Println(err)
	fmt.Println(program.Verify(prog))

	// Output:
	// <nil>
	// <nil>
}
