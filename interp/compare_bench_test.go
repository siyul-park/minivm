package interp

import (
	"context"
	"testing"

	"github.com/d5/tengo/v2"
	"github.com/dop251/goja"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func fiboGo(n int32) int32 {
	if n < 2 {
		return n
	}
	return fiboGo(n-1) + fiboGo(n-2)
}

var fiboProgram = program.New(
	[]instr.Instruction{
		instr.New(instr.I32_CONST, 20),
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.CALL),
	},
	program.WithConstants(
		types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_LT_S),
			instr.New(instr.BR_IF, 26),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.RETURN),
		).Build(),
	),
)

const fiboJS = `
function fibo(n) {
	if (n < 2) return n;
	return fibo(n-1) + fibo(n-2);
}
fibo(20);
`

const fiboTengo = `
fibo := func(n) {
	if n < 2 { return n }
	return fibo(n-1) + fibo(n-2)
}
result := fibo(20)
`

func BenchmarkFibo20(b *testing.B) {
	b.Run("native_go", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			_ = fiboGo(20)
		}
	})

	b.Run("minivm_threaded", func(b *testing.B) {
		ctx := context.Background()
		i := New(fiboProgram)
		defer i.Close()

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_ = i.Run(ctx)
			i.Reset()
		}
	})

	b.Run("goja", func(b *testing.B) {
		vm := goja.New()
		_, err := vm.RunString(fiboJS)
		if err != nil {
			b.Fatal(err)
		}
		fn, ok := goja.AssertFunction(vm.Get("fibo"))
		if !ok {
			b.Fatal("fibo not a function")
		}

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_, _ = fn(goja.Undefined(), vm.ToValue(20))
		}
	})

	b.Run("tengo", func(b *testing.B) {
		script := tengo.NewScript([]byte(fiboTengo))
		compiled, err := script.Compile()
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			c := compiled.Clone()
			_ = c.Run()
		}
	})
}
