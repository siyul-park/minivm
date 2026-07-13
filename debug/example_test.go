package debug_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/debug"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
)

func ExampleNewDebugger() {
	debugger := debug.NewDebugger()
	debugger.Break(0, 0)
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
	vm := interp.New(prog, interp.WithHook(debugger.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	err := vm.Run(context.Background())
	fmt.Println(errors.Is(err, debug.ErrStopped), debugger.Stop().IP)

	// Output:
	// true 0
}
