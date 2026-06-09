package bench

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
func fibGo(n int32) int32 {
	if n < 2 {
		return n
	}
	return fibGo(n-1) + fibGo(n-2)
}

func TestFib35MinivmModes(t *testing.T) {
	ctx := context.Background()
	want := types.I32(fibGo(fibN))
	modes := []struct {
		name string
		new  func() *interp.Interpreter
	}{
		{
			name: "interp",
			new: func() *interp.Interpreter {
				return interp.New(Fib(fibN), interp.WithThreshold(-1))
			},
		},
		{
			name: "jit",
			new: func() *interp.Interpreter {
				return interp.New(Fib(fibN))
			},
		},
	}

	for _, mode := range modes {
		mode := mode
		t.Run(mode.name, func(t *testing.T) {
			i := mode.new()
			defer i.Close()

			for run := 0; run < 2; run++ {
				require.NoError(t, i.Run(ctx))

				got, err := i.Pop()
				require.NoError(t, err)
				require.Equal(t, want, got)

				i.Reset()
			}
		})
	}
}

// BenchmarkFib35 compares recursive fib(35) across six runtimes.
// Each sub-benchmark creates its runtime and compiles its program once
// outside the timed loop, then calls fib(35) b.N times.
func BenchmarkFib35(b *testing.B) {
	b.Run("native", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			fibGo(fibN)
		}
	})

	// minivm is measured twice: interpreter-only (JIT disabled via
	// WithThreshold(-1)) and with the JIT enabled (default New, which
	// promotes hot segments on ARM64). On architectures without a JIT
	// backend the two rows coincide.
	runMiniVM := func(b *testing.B, i *interp.Interpreter) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		defer i.Close()

		b.ReportAllocs()
		b.ResetTimer()
		var err error
		for n := 0; n < b.N; n++ {
			err = i.Run(ctx)
			i.Reset()
		}
		b.StopTimer()
		require.NoError(b, err)
	}

	b.Run("minivm_interp", func(b *testing.B) {
		runMiniVM(b, interp.New(Fib(fibN), interp.WithThreshold(-1)))
	})

	b.Run("minivm_jit", func(b *testing.B) {
		runMiniVM(b, interp.New(Fib(fibN)))
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
