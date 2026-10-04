package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/wasm"
)

func init() {
	run := func() int32 { return sieve(256) }
	registry.Register(registry.Spec{
		Name:   "sieve",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
		Wazero: &registry.Wazero{Module: sieveModule, Export: "sieve", Args: []uint64{256}},
	})
}
func sieve(size int32) int32 {
	composite := make([]bool, size)
	for value := int32(2); value*value < size; value++ {
		for multiple := value * value; multiple < size; multiple += value {
			composite[multiple] = true
		}
	}
	var count int32
	for value := int32(2); value < size; value++ {
		if !composite[value] {
			count++
		}
	}
	return count
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

func sieveModule() []byte {
	module := &wasm.Module{
		TypeSection:     []*wasm.FunctionType{{Params: []wasm.ValueType{wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}}},
		FunctionSection: []wasm.Index{0},
		CodeSection:     []*wasm.Code{{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: sieveWasm()}},
		MemorySection:   &wasm.Memory{Min: 1},
		ExportSection:   []*wasm.Export{{Name: "sieve", Type: wasm.ExternTypeFunc, Index: 0}},
	}
	return binary.EncodeModule(module)
}
