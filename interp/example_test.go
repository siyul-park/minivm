package interp_test

import (
	"context"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func ExampleNew() {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
	vm := interp.New(prog, interp.WithThreshold(-1))
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
	// 42
}

func ExampleInterpreter_Marshal() {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	value, err := vm.Marshal([]int32{1, 2, 3})
	if err != nil {
		panic(err)
	}
	var result []int32
	if err := vm.Unmarshal(value, &result); err != nil {
		panic(err)
	}
	fmt.Println(result)

	// Output:
	// [1 2 3]
}

func ExampleNewHostFunction() {
	host := interp.NewHostFunction(
		&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		func(_ *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() + 1)}, nil
		},
	)
	prog := program.New(
		[]instr.Instruction{instr.New(instr.I32_CONST, 41), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
		program.WithConstants(host),
	)
	vm := interp.New(prog, interp.WithThreshold(-1))
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
	// 42
}

func ExampleNewPool() {
	prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)})
	pool := interp.NewPool(prog, 1, interp.WithThreshold(-1))
	defer pool.Close()

	vm, err := pool.Get(context.Background())
	if err != nil {
		panic(err)
	}
	if err := vm.Run(context.Background()); err != nil {
		panic(err)
	}
	value, err := vm.Pop()
	if err != nil {
		panic(err)
	}
	pool.Put(vm)
	fmt.Println(value)

	// Output:
	// 42
}
