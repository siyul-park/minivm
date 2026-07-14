//go:build compare

package benchmarks

import (
	"context"
	stdbinary "encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	wabin "github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/leb128"
	"github.com/tetratelabs/wabin/wasm"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

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
