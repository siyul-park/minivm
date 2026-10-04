package fixtures

import (
	stdbinary "encoding/binary"

	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/leb128"
	"github.com/tetratelabs/wabin/wasm"
)

const arrayOffset int32 = 1024

func init() {
	run := func() int32 { return typedArraySum(256) }
	registry.Register(registry.Spec{
		Name:    "typed-array-sum",
		Result:  func() types.Value { return types.I32(run()) },
		Program: typedArraySumProgram,
		Native:  registry.Native{I32: run},
		Wazero:  &registry.Wazero{Module: typedArraySumModule, Export: "typed_array_sum", Args: []uint64{256}},
	})
}

func typedArraySum(size int32) int32 {
	return size * (size + 1) / 2
}

func typedArraySumProgram() *program.Program {
	size := int32(256)
	values := make(types.TypedArray[int32], size)
	for index := range values {
		values[index] = int32(index + 1)
	}

	b := program.NewBuilder()
	array := b.Const(values)
	loop := b.Label()
	done := b.Label()
	b.Locals(types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)

	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, uint64(array)).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_GET)
	b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)

	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
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
	code.i32(arrayOffset)
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

func typedArraySumModule() []byte {
	values := make([]byte, 256*4)
	for index := range 256 {
		stdbinary.LittleEndian.PutUint32(values[index*4:], uint32(index+1))
	}
	module := &wasm.Module{
		TypeSection:     []*wasm.FunctionType{{Params: []wasm.ValueType{wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}}},
		FunctionSection: []wasm.Index{0},
		CodeSection:     []*wasm.Code{{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32}, Body: typedArraySumWasm()}},
		MemorySection:   &wasm.Memory{Min: 1},
		DataSection: []*wasm.DataSegment{{
			OffsetExpression: &wasm.ConstantExpression{Opcode: wasm.OpcodeI32Const, Data: leb128.EncodeInt32(arrayOffset)},
			Init:             values,
		}},
		ExportSection: []*wasm.Export{{Name: "typed_array_sum", Type: wasm.ExternTypeFunc, Index: 0}},
	}
	return binary.EncodeModule(module)
}
