package interp_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// The README Quick Start mirrors this example.
func ExampleNew() {
	prog := program.NewBuilder().
		Emit(instr.I32_CONST, 6).
		Emit(instr.I32_CONST, 7).
		Emit(instr.I32_MUL).
		MustBuild()

	vm, err := interp.New(prog) // verifies prog before it can run
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Close()

	if err := vm.Run(context.Background()); err != nil {
		fmt.Println(err)
		return
	}
	result, err := vm.Pop()
	fmt.Println(result, err)

	// Output:
	// 42 <nil>
}

// The README "Call Go from bytecode" section mirrors this example.
func ExampleInterpreter_Marshal() {
	add := func(ctx context.Context, a, b int64) (int64, error) { return a + b, nil }
	typ := &types.FunctionType{
		Params:  []types.Type{types.TypeI64(), types.TypeI64()},
		Returns: []types.Type{types.TypeI64()},
	}
	prog := program.NewBuilder().
		Globals(typ).
		Emit(instr.I64_CONST, 2).
		Emit(instr.I64_CONST, 3).
		Emit(instr.GLOBAL_GET, 0).
		Emit(instr.CALL).
		MustBuild()

	vm, err := interp.New(prog)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Close()

	fn, err := vm.Marshal(add)
	if err != nil {
		fmt.Println(err)
		return
	}
	addr, err := vm.Alloc(fn) // the caller owns the returned reference
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := vm.SetGlobal(0, types.BoxRef(addr)); err != nil { // the global takes it
		fmt.Println(err)
		return
	}
	if err := vm.Run(context.Background()); err != nil {
		fmt.Println(err)
		return
	}
	result, err := vm.Pop()
	fmt.Println(result, err)

	// Output:
	// 5 <nil>
}

func ExampleInterpreter_Unmarshal() {
	vm, err := interp.New(program.New(nil))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Close()

	type Point struct{ X, Y int64 }
	in := struct {
		Point Point
		Tags  []string
		Index map[string]int64
	}{Point{1, 2}, []string{"a", "b"}, map[string]int64{"n": 7}}

	val, err := vm.Marshal(in)
	if err != nil {
		fmt.Println(err)
		return
	}
	addr, err := vm.Alloc(val) // owns the refs val holds until released
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Release(addr)

	var out struct {
		Point Point
		Tags  []string
		Index map[string]int64
	}
	if err := vm.Unmarshal(types.BoxRef(addr), &out); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(out.Point, out.Tags, out.Index)

	// Output:
	// {1 2} [a b] map[n:7]
}

// The README "Call bytecode from Go" section mirrors this example.
func ExampleRegistry_Unmarshal() {
	vm, err := interp.New(program.New(nil))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Close()

	typ := &types.FunctionType{Params: []types.Type{types.TypeI64()}, Returns: []types.Type{types.TypeI64()}}
	inc := types.NewFunctionBuilder(typ).
		Emit(instr.LOCAL_GET, 0).
		Emit(instr.I64_CONST, 1).
		Emit(instr.I64_ADD).
		Emit(instr.RETURN).
		MustBuild()
	addr, err := vm.Alloc(inc)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Release(addr)

	var call func(context.Context, int64) (int64, error)
	if err := interp.NewRegistry().Unmarshal(vm, types.BoxRef(addr), &call); err != nil {
		fmt.Println(err)
		return
	}
	result, err := call(context.Background(), 41)
	fmt.Println(result, err)

	// Output:
	// 42 <nil>
}

func ExampleInterpreter_Run() {
	errBoom := errors.New("boom")
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI64()}}
	prog := program.NewBuilder().
		Globals(typ).
		Emit(instr.GLOBAL_GET, 0).
		Emit(instr.CALL).
		MustBuild()

	vm, err := interp.New(prog)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Close()

	fn, err := vm.Marshal(func() (int64, error) { return 0, errBoom })
	if err != nil {
		fmt.Println(err)
		return
	}
	addr, err := vm.Alloc(fn)
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := vm.SetGlobal(0, types.BoxRef(addr)); err != nil {
		fmt.Println(err)
		return
	}

	err = vm.Run(context.Background())
	fmt.Println(errors.Is(err, errBoom))

	// Output:
	// true
}

func ExampleNewHostFunction() {
	typ := &types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}
	// params are borrowed for the call; an i32 carries no heap reference, so
	// the result needs no ownership handling.
	double := interp.NewHostFunction(typ, func(_ *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
		return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
	})
	prog := program.NewBuilder().
		Emit(instr.I32_CONST, 21).
		ConstGet(double).
		Emit(instr.CALL).
		MustBuild()

	vm, err := interp.New(prog)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer vm.Close()

	if err := vm.Run(context.Background()); err != nil {
		fmt.Println(err)
		return
	}
	result, err := vm.Pop()
	fmt.Println(result, err)

	// Output:
	// 42 <nil>
}
