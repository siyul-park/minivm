package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/wasm"
)

func init() {
	run := func() int32 { return iterativeFib(30) }
	registry.Register(registry.Spec{
		Name:   "iterative-fib",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
		Wazero: &registry.Wazero{Module: iterativeFibModule, Export: "iterative_fib", Args: []uint64{30}},
	})
}
func iterativeFib(n int32) int32 {
	var current int32
	next := int32(1)
	for index := int32(0); index < n; index++ {
		value := current + next
		current = next
		next = value
	}
	return current
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

func iterativeFibModule() []byte {
	module := &wasm.Module{
		TypeSection:     []*wasm.FunctionType{{Params: []wasm.ValueType{wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}}},
		FunctionSection: []wasm.Index{0},
		CodeSection:     []*wasm.Code{{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: iterativeFibWasm()}},
		ExportSection:   []*wasm.Export{{Name: "iterative_fib", Type: wasm.ExternTypeFunc, Index: 0}},
	}
	return binary.EncodeModule(module)
}
