//go:build compare

package benchmarks

import (
	"fmt"
	"strings"
	"testing"

	tengo "github.com/d5/tengo/v2"
	"github.com/dop251/goja"
	"github.com/go-python/gpython/compile"
	"github.com/go-python/gpython/py"
	_ "github.com/go-python/gpython/stdlib"
	"github.com/stretchr/testify/require"
	yaegi "github.com/traefik/yaegi/interp"
	lua "github.com/yuin/gopher-lua"
)

func benchmarkCompare(b *testing.B, comparison benchmarkComparison, want int32) {
	b.Helper()
	b.Run("native", func(b *testing.B) {
		benchmarkNative(b, comparison.native, want)
	})
	if comparison.wazero != "" {
		benchmarkWazero(b, comparison.wazero, want, comparison.args...)
	}
	benchmarkScriptRuntimes(b, comparison.scripts, comparison.values, want)
}

func benchmarkNative(b *testing.B, run func() int32, want int32) {
	b.Helper()
	require.Equal(b, want, run())

	var value int32
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		value = run()
	}
	b.StopTimer()
	require.Equal(b, want, value)
}

func benchmarkScriptRuntimes(b *testing.B, scripts benchmarkScripts, values []int32, want int32) {
	b.Helper()
	b.Run("tengo", func(b *testing.B) {
		benchmarkTengo(b, scripts.tengo, values, want)
	})
	b.Run("gopher_lua", func(b *testing.B) {
		benchmarkGopherLua(b, scripts.gopherLua, values, want)
	})
	b.Run("goja", func(b *testing.B) {
		benchmarkGoja(b, scripts.goja, values, want)
	})
	if scripts.gpython != "" {
		b.Run("gpython", func(b *testing.B) {
			benchmarkGpython(b, scripts.gpython, want)
		})
	}
	if scripts.yaegi != "" {
		b.Run("yaegi", func(b *testing.B) {
			benchmarkYaegi(b, scripts.yaegi, want)
		})
	}
}

func benchmarkTengo(b *testing.B, source string, values []int32, want int32) {
	b.Helper()
	script := tengo.NewScript([]byte(source))
	if values != nil {
		items := make([]interface{}, len(values))
		for index, value := range values {
			items[index] = int64(value)
		}
		require.NoError(b, script.Add("values", items))
	}
	compiled, err := script.Compile()
	require.NoError(b, err)
	require.NoError(b, compiled.Run())
	require.Equal(b, int64(want), compiled.Get("result").Int64())

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = compiled.Run()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, int64(want), compiled.Get("result").Int64())
}

func benchmarkGopherLua(b *testing.B, source string, values []int32, want int32) {
	b.Helper()
	state := lua.NewState()
	defer state.Close()
	if values != nil {
		table := state.NewTable()
		for _, value := range values {
			table.Append(lua.LNumber(value))
		}
		state.SetGlobal("values", table)
	}
	require.NoError(b, state.DoString(source))
	function := state.GetGlobal("run")

	var value int32
	call := func() error {
		if err := state.CallByParam(lua.P{Fn: function, NRet: 1, Protect: true}); err != nil {
			return err
		}
		result := state.Get(-1)
		state.Pop(1)
		number, ok := result.(lua.LNumber)
		if !ok {
			return fmt.Errorf("run returned %s", result.Type())
		}
		value = int32(number)
		return nil
	}
	require.NoError(b, call())
	require.Equal(b, want, value)

	var err error
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = call()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, want, value)
}

func benchmarkGoja(b *testing.B, source string, values []int32, want int32) {
	b.Helper()
	vm := goja.New()
	if values != nil {
		require.NoError(b, vm.Set("values", values))
	}
	program, err := goja.Compile("compare", strings.TrimSpace(source), true)
	require.NoError(b, err)
	_, err = vm.RunProgram(program)
	require.NoError(b, err)
	function, ok := goja.AssertFunction(vm.Get("run"))
	require.True(b, ok)

	var value int32
	call := func() error {
		result, err := function(goja.Undefined())
		if err != nil {
			return err
		}
		value = int32(result.ToInteger())
		return nil
	}
	require.NoError(b, call())
	require.Equal(b, want, value)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = call()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, want, value)
}

func benchmarkGpython(b *testing.B, source string, want int32) {
	b.Helper()
	code, err := compile.Compile(strings.TrimSpace(source), "compare.py", py.ExecMode, 0, true)
	require.NoError(b, err)
	ctx := py.NewContext(py.DefaultContextOpts())
	defer ctx.Close()
	module, err := py.RunCode(ctx, code, "compare.py", nil)
	require.NoError(b, err)

	var result py.Object
	call := func() error {
		result, err = module.Call("run", nil, nil)
		return err
	}
	require.NoError(b, call())
	require.Equal(b, want, int32(result.(py.Int)))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err = call(); err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, want, int32(result.(py.Int)))
}

func benchmarkYaegi(b *testing.B, source string, want int32) {
	b.Helper()
	vm := yaegi.New(yaegi.Options{})
	_, err := vm.Eval(strings.TrimSpace(source))
	require.NoError(b, err)
	value, err := vm.Eval("bench.Run")
	require.NoError(b, err)
	run, ok := value.Interface().(func() int32)
	require.True(b, ok)
	require.Equal(b, want, run())

	var result int32
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result = run()
	}
	b.StopTimer()
	require.Equal(b, want, result)
}
