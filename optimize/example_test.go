package optimize_test

import (
	"context"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
)

func ExampleNew() {
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_CONST, 2),
		instr.New(instr.I32_ADD),
	})
	optimized, err := optimize.New(optimize.O1).Optimize(prog)
	if err != nil {
		panic(err)
	}
	vm := interp.New(optimized, interp.WithThreshold(-1))
	defer vm.Close()

	if err := vm.Run(context.Background()); err != nil {
		panic(err)
	}
	value, err := vm.Pop()
	if err != nil {
		panic(err)
	}
	fmt.Println(value)

	// Output:
	// 3
}
