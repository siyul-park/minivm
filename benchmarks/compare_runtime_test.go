//go:build compare

package benchmarks

import (
	"fmt"
	"strings"
	"testing"

	tengo "github.com/d5/tengo/v2"
	"github.com/dop251/goja"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

type compareScripts struct {
	tengo     string
	gopherLua string
	goja      string
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

func benchmarkScripts(b *testing.B, scripts compareScripts, values []int32, want int32) {
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
