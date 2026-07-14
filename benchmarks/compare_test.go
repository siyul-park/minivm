//go:build compare

package benchmarks

import (
	"context"
	stdbinary "encoding/binary"
	"fmt"
	"strings"
	"testing"

	tengo "github.com/d5/tengo/v2"
	"github.com/dop251/goja"
	"github.com/go-python/gpython/compile"
	"github.com/go-python/gpython/py"
	_ "github.com/go-python/gpython/stdlib"
	"github.com/stretchr/testify/require"
	wabin "github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/leb128"
	"github.com/tetratelabs/wabin/wasm"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	yaegi "github.com/traefik/yaegi/interp"
	lua "github.com/yuin/gopher-lua"
)

func init() {
	benchmarkCompare = runComparison
}

func runComparison(b *testing.B, comparison benchmarkComparison, want int32) {
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

const wasmArrayOffset int32 = 1024

var comparisonWasm = wabin.EncodeModule(comparisonWasmModule())

func benchmarkWazero(b *testing.B, name string, want int32, args ...uint64) {
	b.Helper()
	b.Run("wazero", func(b *testing.B) {
		ctx := context.Background()
		runtime := wazero.NewRuntime(ctx)
		defer runtime.Close(ctx)
		compiled, err := runtime.CompileModule(ctx, comparisonWasm)
		require.NoError(b, err)
		defer compiled.Close(ctx)
		module, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
		require.NoError(b, err)
		defer module.Close(ctx)
		function := module.ExportedFunction(name)
		require.NotNil(b, function)

		results, err := function.Call(ctx, args...)
		require.NoError(b, err)
		require.Len(b, results, 1)
		require.Equal(b, want, api.DecodeI32(results[0]))

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			results, err = function.Call(ctx, args...)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
		require.Len(b, results, 1)
		require.Equal(b, want, api.DecodeI32(results[0]))
	})
}

func comparisonWasmModule() *wasm.Module {
	indirect := wasm.Index(1)
	values := make([]byte, 256*4)
	for index := range 256 {
		stdbinary.LittleEndian.PutUint32(values[index*4:], uint32(index+1))
	}
	return &wasm.Module{
		TypeSection: []*wasm.FunctionType{
			{Params: []wasm.ValueType{wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}},
			{Params: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}},
		},
		FunctionSection: []wasm.Index{0, 0, 0, 0, 0, 1},
		CodeSection: []*wasm.Code{
			{Body: recursiveFibWasm(0)},
			{Body: recursiveFibIndirectWasm()},
			{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: iterativeFibWasm()},
			{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: sieveWasm()},
			{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: typedArraySumWasm()},
			{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: branchTreeWasm()},
		},
		TableSection: []*wasm.Table{{Min: 1, Type: wasm.RefTypeFuncref}},
		ElementSection: []*wasm.ElementSegment{{
			OffsetExpr: &wasm.ConstantExpression{Opcode: wasm.OpcodeI32Const, Data: leb128.EncodeInt32(0)},
			Init:       []*wasm.Index{&indirect},
			Type:       wasm.RefTypeFuncref,
			Mode:       wasm.ElementModeActive,
		}},
		MemorySection: &wasm.Memory{Min: 1},
		DataSection: []*wasm.DataSegment{{
			OffsetExpression: &wasm.ConstantExpression{Opcode: wasm.OpcodeI32Const, Data: leb128.EncodeInt32(wasmArrayOffset)},
			Init:             values,
		}},
		ExportSection: []*wasm.Export{
			{Name: "recursive_fib", Type: wasm.ExternTypeFunc, Index: 0},
			{Name: "indirect_recursive_fib", Type: wasm.ExternTypeFunc, Index: 1},
			{Name: "iterative_fib", Type: wasm.ExternTypeFunc, Index: 2},
			{Name: "sieve", Type: wasm.ExternTypeFunc, Index: 3},
			{Name: "typed_array_sum", Type: wasm.ExternTypeFunc, Index: 4},
			{Name: "branch_tree", Type: wasm.ExternTypeFunc, Index: 5},
		},
	}
}

type wasmCode []byte

func (c *wasmCode) op(op wasm.Opcode) {
	*c = append(*c, op)
}

func (c *wasmCode) u32(value uint32) {
	*c = append(*c, leb128.EncodeUint32(value)...)
}

func (c *wasmCode) i32(value int32) {
	c.op(wasm.OpcodeI32Const)
	*c = append(*c, leb128.EncodeInt32(value)...)
}

func (c *wasmCode) local(op wasm.Opcode, index uint32) {
	c.op(op)
	c.u32(index)
}

func (c *wasmCode) branch(op wasm.Opcode, depth uint32) {
	c.op(op)
	c.u32(depth)
}

func (c *wasmCode) memory(op wasm.Opcode, align, offset uint32) {
	c.op(op)
	c.u32(align)
	c.u32(offset)
}

func recursiveFibWasm(index uint32) []byte {
	var code wasmCode
	code.local(wasm.OpcodeLocalGet, 0)
	code.i32(2)
	code.op(wasm.OpcodeI32LtS)
	code.op(wasm.OpcodeIf)
	code.op(wasm.ValueTypeI32)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeElse)
	code.local(wasm.OpcodeLocalGet, 0)
	code.i32(1)
	code.op(wasm.OpcodeI32Sub)
	code.local(wasm.OpcodeCall, index)
	code.local(wasm.OpcodeLocalGet, 0)
	code.i32(2)
	code.op(wasm.OpcodeI32Sub)
	code.local(wasm.OpcodeCall, index)
	code.op(wasm.OpcodeI32Add)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)
	return code
}

func recursiveFibIndirectWasm() []byte {
	var code wasmCode
	code.local(wasm.OpcodeLocalGet, 0)
	code.i32(2)
	code.op(wasm.OpcodeI32LtS)
	code.op(wasm.OpcodeIf)
	code.op(wasm.ValueTypeI32)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeElse)
	for _, delta := range []int32{1, 2} {
		code.local(wasm.OpcodeLocalGet, 0)
		code.i32(delta)
		code.op(wasm.OpcodeI32Sub)
		code.i32(0)
		code.op(wasm.OpcodeCallIndirect)
		code.u32(0)
		code.u32(0)
	}
	code.op(wasm.OpcodeI32Add)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)
	return code
}

func iterativeFibWasm() []byte {
	var code wasmCode
	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 1)
	code.i32(1)
	code.local(wasm.OpcodeLocalSet, 2)
	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 3)
	code.op(wasm.OpcodeBlock)
	code.op(0x40)
	code.op(wasm.OpcodeLoop)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 3)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeI32GeS)
	code.branch(wasm.OpcodeBrIf, 1)
	code.local(wasm.OpcodeLocalGet, 1)
	code.local(wasm.OpcodeLocalGet, 2)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 4)
	code.local(wasm.OpcodeLocalGet, 2)
	code.local(wasm.OpcodeLocalSet, 1)
	code.local(wasm.OpcodeLocalGet, 4)
	code.local(wasm.OpcodeLocalSet, 2)
	code.local(wasm.OpcodeLocalGet, 3)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 3)
	code.branch(wasm.OpcodeBr, 0)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)
	code.local(wasm.OpcodeLocalGet, 1)
	code.op(wasm.OpcodeEnd)
	return code
}

func sieveWasm() []byte {
	var code wasmCode
	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 4)
	code.op(wasm.OpcodeBlock)
	code.op(0x40)
	code.op(wasm.OpcodeLoop)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 4)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeI32GeS)
	code.branch(wasm.OpcodeBrIf, 1)
	code.local(wasm.OpcodeLocalGet, 4)
	code.i32(2)
	code.op(wasm.OpcodeI32Shl)
	code.i32(0)
	code.memory(wasm.OpcodeI32Store, 2, 0)
	code.local(wasm.OpcodeLocalGet, 4)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 4)
	code.branch(wasm.OpcodeBr, 0)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)

	code.i32(2)
	code.local(wasm.OpcodeLocalSet, 1)
	code.op(wasm.OpcodeBlock)
	code.op(0x40)
	code.op(wasm.OpcodeLoop)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 1)
	code.local(wasm.OpcodeLocalGet, 1)
	code.op(wasm.OpcodeI32Mul)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeI32GeS)
	code.branch(wasm.OpcodeBrIf, 1)
	code.local(wasm.OpcodeLocalGet, 1)
	code.local(wasm.OpcodeLocalGet, 1)
	code.op(wasm.OpcodeI32Mul)
	code.local(wasm.OpcodeLocalSet, 2)
	code.op(wasm.OpcodeBlock)
	code.op(0x40)
	code.op(wasm.OpcodeLoop)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 2)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeI32GeS)
	code.branch(wasm.OpcodeBrIf, 1)
	code.local(wasm.OpcodeLocalGet, 2)
	code.i32(2)
	code.op(wasm.OpcodeI32Shl)
	code.i32(1)
	code.memory(wasm.OpcodeI32Store, 2, 0)
	code.local(wasm.OpcodeLocalGet, 2)
	code.local(wasm.OpcodeLocalGet, 1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 2)
	code.branch(wasm.OpcodeBr, 0)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)
	code.local(wasm.OpcodeLocalGet, 1)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 1)
	code.branch(wasm.OpcodeBr, 0)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)

	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 3)
	code.i32(2)
	code.local(wasm.OpcodeLocalSet, 1)
	code.op(wasm.OpcodeBlock)
	code.op(0x40)
	code.op(wasm.OpcodeLoop)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 1)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeI32GeS)
	code.branch(wasm.OpcodeBrIf, 1)
	code.local(wasm.OpcodeLocalGet, 1)
	code.i32(2)
	code.op(wasm.OpcodeI32Shl)
	code.memory(wasm.OpcodeI32Load, 2, 0)
	code.op(wasm.OpcodeI32Eqz)
	code.op(wasm.OpcodeIf)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 3)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 3)
	code.op(wasm.OpcodeEnd)
	code.local(wasm.OpcodeLocalGet, 1)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 1)
	code.branch(wasm.OpcodeBr, 0)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)
	code.local(wasm.OpcodeLocalGet, 3)
	code.op(wasm.OpcodeEnd)
	return code
}

func typedArraySumWasm() []byte {
	var code wasmCode
	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 1)
	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 2)
	code.op(wasm.OpcodeBlock)
	code.op(0x40)
	code.op(wasm.OpcodeLoop)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 1)
	code.local(wasm.OpcodeLocalGet, 0)
	code.op(wasm.OpcodeI32GeS)
	code.branch(wasm.OpcodeBrIf, 1)
	code.local(wasm.OpcodeLocalGet, 2)
	code.i32(wasmArrayOffset)
	code.local(wasm.OpcodeLocalGet, 1)
	code.i32(2)
	code.op(wasm.OpcodeI32Shl)
	code.op(wasm.OpcodeI32Add)
	code.memory(wasm.OpcodeI32Load, 2, 0)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 2)
	code.local(wasm.OpcodeLocalGet, 1)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 1)
	code.branch(wasm.OpcodeBr, 0)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)
	code.local(wasm.OpcodeLocalGet, 2)
	code.op(wasm.OpcodeEnd)
	return code
}

func branchTreeWasm() []byte {
	var code wasmCode
	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 2)
	code.i32(0)
	code.local(wasm.OpcodeLocalSet, 3)
	code.op(wasm.OpcodeBlock)
	code.op(0x40)
	code.op(wasm.OpcodeLoop)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 2)
	code.local(wasm.OpcodeLocalGet, 1)
	code.op(wasm.OpcodeI32GeS)
	code.branch(wasm.OpcodeBrIf, 1)
	code.local(wasm.OpcodeLocalGet, 2)
	code.i32(17)
	code.op(wasm.OpcodeI32Mul)
	code.i32(11)
	code.op(wasm.OpcodeI32Add)
	code.i32(97)
	code.op(wasm.OpcodeI32RemS)
	code.local(wasm.OpcodeLocalSet, 4)
	code.local(wasm.OpcodeLocalGet, 2)
	code.i32(7)
	code.op(wasm.OpcodeI32RemS)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 5)
	code.local(wasm.OpcodeLocalGet, 2)
	code.i32(5)
	code.op(wasm.OpcodeI32RemS)
	code.i32(2)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 6)
	code.local(wasm.OpcodeLocalGet, 0)
	code.local(wasm.OpcodeLocalGet, 4)
	code.op(wasm.OpcodeI32LtS)
	code.op(wasm.OpcodeIf)
	code.op(0x40)
	code.local(wasm.OpcodeLocalGet, 3)
	code.local(wasm.OpcodeLocalGet, 5)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 3)
	code.op(wasm.OpcodeElse)
	code.local(wasm.OpcodeLocalGet, 3)
	code.local(wasm.OpcodeLocalGet, 6)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 3)
	code.op(wasm.OpcodeEnd)
	code.local(wasm.OpcodeLocalGet, 2)
	code.i32(1)
	code.op(wasm.OpcodeI32Add)
	code.local(wasm.OpcodeLocalSet, 2)
	code.branch(wasm.OpcodeBr, 0)
	code.op(wasm.OpcodeEnd)
	code.op(wasm.OpcodeEnd)
	code.local(wasm.OpcodeLocalGet, 3)
	code.op(wasm.OpcodeEnd)
	return code
}
