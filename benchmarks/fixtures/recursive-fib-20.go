package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/wasm"
)

func init() {
	run := func() int32 { return recursiveFib20(20) }
	registry.Register(registry.Spec{
		Name:   "recursive-fib-20",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
		Wazero: &registry.Wazero{Module: recursiveFib20Module, Export: "recursive_fib", Args: []uint64{20}},
	})
}
func recursiveFib20(n int32) int32 {
	if n < 2 {
		return n
	}
	return recursiveFib20(n-1) + recursiveFib20(n-2)
}

func recursiveFib20Wasm(index uint32) []byte {
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

func recursiveFib20Module() []byte {
	module := &wasm.Module{
		TypeSection:     []*wasm.FunctionType{{Params: []wasm.ValueType{wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}}},
		FunctionSection: []wasm.Index{0},
		CodeSection:     []*wasm.Code{{Body: recursiveFib20Wasm(0)}},
		ExportSection:   []*wasm.Export{{Name: "recursive_fib", Type: wasm.ExternTypeFunc, Index: 0}},
	}
	return binary.EncodeModule(module)
}
