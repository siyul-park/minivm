package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/wasm"
)

func init() {
	run := func() int32 { return branchTree(37, 96) }
	registry.Register(registry.Spec{
		Name:   "branch-tree",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
		Wazero: &registry.Wazero{Module: branchTreeModule, Export: "branch_tree", Args: []uint64{37, 96}},
	})
}
func branchTree(input, nodes int32) int32 {
	var total int32
	for index := int32(0); index < nodes; index++ {
		threshold := (index*17 + 11) % 97
		if input < threshold {
			total += index%7 + 1
		} else {
			total += index%5 + 2
		}
	}
	return total
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

func branchTreeModule() []byte {
	module := &wasm.Module{
		TypeSection:     []*wasm.FunctionType{{Params: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}}},
		FunctionSection: []wasm.Index{0},
		CodeSection:     []*wasm.Code{{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: branchTreeWasm()}},
		ExportSection:   []*wasm.Export{{Name: "branch_tree", Type: wasm.ExternTypeFunc, Index: 0}},
	}
	return binary.EncodeModule(module)
}
