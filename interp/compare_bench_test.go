package interp

import (
	"context"
	"testing"

	"github.com/d5/tengo/v2"
	"github.com/dop251/goja"
	lua "github.com/yuin/gopher-lua"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// fib(20) = 6765

func fiboGo(n int32) int32 {
	if n < 2 {
		return n
	}
	return fiboGo(n-1) + fiboGo(n-2)
}

// Minimal hand-encoded wasm module: (func (export "fib") (param i32) (result i32) ...)
var fiboWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	// type section: (i32) -> (i32)
	0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f,
	// function section: func[0] uses type[0]
	0x03, 0x02, 0x01, 0x00,
	// export section: "fib" -> func[0]
	0x07, 0x07, 0x01, 0x03, 0x66, 0x69, 0x62, 0x00, 0x00,
	// code section
	0x0a, 0x1e, 0x01, 0x1c,
	0x00,             // 0 locals
	0x20, 0x00,       // local.get 0
	0x41, 0x02,       // i32.const 2
	0x48,             // i32.lt_s
	0x04, 0x7f,       // if (result i32)
	0x20, 0x00,       //   local.get 0
	0x05,             // else
	0x20, 0x00,       //   local.get 0
	0x41, 0x01,       //   i32.const 1
	0x6b,             //   i32.sub
	0x10, 0x00,       //   call 0
	0x20, 0x00,       //   local.get 0
	0x41, 0x02,       //   i32.const 2
	0x6b,             //   i32.sub
	0x10, 0x00,       //   call 0
	0x6a,             //   i32.add
	0x0b,             // end (if)
	0x0b,             // end (func)
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
function fib(n) {
	if (n < 2) return n;
	return fib(n-1) + fib(n-2);
}
`

const fiboLua = `
function fib(n)
	if n < 2 then return n end
	return fib(n-1) + fib(n-2)
end
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

	b.Run("wazero", func(b *testing.B) {
		ctx := context.Background()
		r := wazero.NewRuntime(ctx)
		defer r.Close(ctx)

		mod, err := r.Instantiate(ctx, fiboWasm)
		if err != nil {
			b.Fatal(err)
		}
		fib := mod.ExportedFunction("fib")

		// validate
		results, err := fib.Call(ctx, 20)
		if err != nil {
			b.Fatal(err)
		}
		if api.DecodeI32(results[0]) != 6765 {
			b.Fatalf("unexpected result: %d", results[0])
		}

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_, _ = fib.Call(ctx, 20)
		}
	})

	b.Run("gopher_lua", func(b *testing.B) {
		L := lua.NewState()
		defer L.Close()

		if err := L.DoString(fiboLua); err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			if err := L.CallByParam(lua.P{
				Fn:      L.GetGlobal("fib"),
				NRet:    1,
				Protect: false,
			}, lua.LNumber(20)); err != nil {
				b.Fatal(err)
			}
			L.Pop(1)
		}
	})

	b.Run("goja", func(b *testing.B) {
		vm := goja.New()
		if _, err := vm.RunString(fiboJS); err != nil {
			b.Fatal(err)
		}
		fn, ok := goja.AssertFunction(vm.Get("fib"))
		if !ok {
			b.Fatal("fib not a function")
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
