package benchmarks

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func typedArraySum(size int32) *program.Program {
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

func allocationGraph(depth int32) *program.Program {
	b := program.NewBuilder()
	array := b.Type(types.NewArrayType(types.TypeRef))
	loop := b.Label()
	done := b.Label()
	b.Locals(types.TypeRef, types.TypeI32)
	b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(array)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(depth))).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(array)).Emit(instr.DUP)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_SET).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func typedArraySumReference(size int32) int32 {
	return size * (size + 1) / 2
}
