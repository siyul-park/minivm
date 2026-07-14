//go:build compare

package benchmarks

import (
	"context"
	"strings"
	"testing"

	tengo "github.com/d5/tengo/v2"
	"github.com/dop251/goja"
	lua "github.com/yuin/gopher-lua"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// fibN is the input for the cross-runtime comparison.
// fib(35) = 9_227_465; ~29.8 M recursive calls; no memoisation.
const fibN int32 = 35

// fibWasm is a precomputed WASM binary for the recursive fib function:
//
//	(module (func $fib (export "fib") (param i32) (result i32)
//	  local.get 0  i32.const 2  i32.lt_s
//	  if (result i32)  local.get 0
//	  else
//	    local.get 0  i32.const 1  i32.sub  call $fib
//	    local.get 0  i32.const 2  i32.sub  call $fib  i32.add
//	  end))
var fibWasm = []byte{
	// magic + version
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	// type section: (i32) -> i32
	0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f,
	// function section: 1 function of type 0
	0x03, 0x02, 0x01, 0x00,
	// export section: "fib" as function 0
	0x07, 0x07, 0x01, 0x03, 0x66, 0x69, 0x62, 0x00, 0x00,
	// code section: 1 function body (28 bytes, 0 locals)
	0x0a, 0x1e, 0x01, 0x1c, 0x00,
	// body
	0x20, 0x00, // local.get 0
	0x41, 0x02, // i32.const 2
	0x48,       // i32.lt_s
	0x04, 0x7f, // if (result i32)
	0x20, 0x00, // local.get 0            — then branch
	0x05,       // else
	0x20, 0x00, // local.get 0
	0x41, 0x01, // i32.const 1
	0x6b,       // i32.sub
	0x10, 0x00, // call 0
	0x20, 0x00, // local.get 0
	0x41, 0x02, // i32.const 2
	0x6b,       // i32.sub
	0x10, 0x00, // call 0
	0x6a, // i32.add               — else branch end
	0x0b, // end (if/else)
	0x0b, // end (function)
}

// fibGo is a pure-Go recursive fib used as the native baseline.
// BenchmarkCompare_RecursiveFib compares recursive fib(35) across runtimes.
// Each sub-benchmark creates its runtime and compiles its program once
// outside the timed loop, then calls fib(35) b.N times.
func BenchmarkCompare_RecursiveFib(b *testing.B) {
	b.Run("native", func(b *testing.B) {
		var fib func(int32) int32
		fib = func(n int32) int32 {
			if n < 2 {
				return n
			}
			return fib(n-1) + fib(n-2)
		}

		var value int32
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			value = fib(fibN)
		}
		b.StopTimer()
		require.Equal(b, recursiveFibReference(fibN), value)
	})

	prog := recursiveFib(fibN)

	b.Run("minivm_threaded", func(b *testing.B) {
		ctx := context.Background()
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(ctx))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(recursiveFibReference(fibN)), value)
		vm.Reset()

		benchmarkRun(b, vm, types.BoxI32(recursiveFibReference(fibN)))
	})

	b.Run("wazero", func(b *testing.B) {
		ctx := context.Background()

		r := wazero.NewRuntime(ctx)
		defer r.Close(ctx)

		mod, err := r.Instantiate(ctx, fibWasm)
		require.NoError(b, err)
		fib := mod.ExportedFunction("fib")

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_, err = fib.Call(ctx, api.EncodeI32(fibN))
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})

	b.Run("tengo", func(b *testing.B) {
		src := `
fib := func(n) {
    if n < 2 { return n }
    return fib(n-1) + fib(n-2)
}
result := fib(35)
`
		s := tengo.NewScript([]byte(src))
		compiled, err := s.Compile()
		require.NoError(b, err)

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			cloned := compiled.Clone()
			err = cloned.Run()
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})

	b.Run("gopher_lua", func(b *testing.B) {
		L := lua.NewState()
		defer L.Close()

		// Define fib as a global so it can call itself by name.
		err := L.DoString(`
fib = function(n)
    if n < 2 then return n end
    return fib(n-1) + fib(n-2)
end
`)
		require.NoError(b, err)
		fibFn := L.GetGlobal("fib")

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			err = L.CallByParam(lua.P{
				Fn:      fibFn,
				NRet:    1,
				Protect: true,
			}, lua.LNumber(fibN))
			if err != nil {
				break
			}
			L.Pop(1)
		}
		b.StopTimer()
		require.NoError(b, err)
	})

	b.Run("goja", func(b *testing.B) {
		vm := goja.New()
		p, err := goja.Compile("fib", strings.TrimSpace(`
function fib(n) {
    if (n < 2) return n;
    return fib(n-1) + fib(n-2);
}
`), true)
		require.NoError(b, err)
		_, err = vm.RunProgram(p)
		require.NoError(b, err)
		fibFn, _ := goja.AssertFunction(vm.Get("fib"))

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_, err = fibFn(goja.Undefined(), vm.ToValue(fibN))
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
	})
}
