package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/leb128"
	"github.com/tetratelabs/wabin/wasm"
)

func init() {
	run := func() int32 { return indirectRecursiveFib(20) }
	registry.Register(registry.Spec{
		Name:   "indirect-recursive-fib",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
		Wazero: &registry.Wazero{Module: indirectRecursiveFibModule, Export: "indirect_recursive_fib", Args: []uint64{20}},
	})
}
func indirectRecursiveFib(n int32) int32 {
	var fib func(int32) int32
	fib = func(value int32) int32 {
		if value < 2 {
			return value
		}
		return fib(value-1) + fib(value-2)
	}
	return fib(n)
}

func indirectRecursiveFibWasm() []byte {
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

func indirectRecursiveFibModule() []byte {
	index := wasm.Index(0)
	module := &wasm.Module{
		TypeSection:     []*wasm.FunctionType{{Params: []wasm.ValueType{wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}}},
		FunctionSection: []wasm.Index{0},
		CodeSection:     []*wasm.Code{{Body: indirectRecursiveFibWasm()}},
		TableSection:    []*wasm.Table{{Min: 1, Type: wasm.RefTypeFuncref}},
		ElementSection: []*wasm.ElementSegment{{
			OffsetExpr: &wasm.ConstantExpression{Opcode: wasm.OpcodeI32Const, Data: leb128.EncodeInt32(0)},
			Init:       []*wasm.Index{&index},
			Type:       wasm.RefTypeFuncref,
			Mode:       wasm.ElementModeActive,
		}},
		ExportSection: []*wasm.Export{{Name: "indirect_recursive_fib", Type: wasm.ExternTypeFunc, Index: 0}},
	}
	return binary.EncodeModule(module)
}
