package benchmarks

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func iterativeFib(n int32) *program.Program {
	b := program.NewBuilder()
	loop := b.Label()
	done := b.Label()
	b.Locals(types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func sieve(size int32) *program.Program {
	b := program.NewBuilder()
	outer := b.Label()
	inner := b.Label()
	next := b.Label()
	count := b.Label()
	scan := b.Label()
	prime := b.Label()
	advance := b.Label()
	done := b.Label()
	array := b.Type(types.TypeI32Array)
	b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(array)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 1)
	b.Bind(outer)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_MUL)
	b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(count)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_MUL).Emit(instr.LOCAL_SET, 2)
	b.Bind(inner)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(next)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Br(inner)
	b.Bind(next)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(outer)
	b.Bind(count)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 3)
	b.Bind(scan)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.ARRAY_GET)
	b.Emit(instr.I32_CONST, 0).Emit(instr.I32_EQ).BrIf(prime)
	b.Br(advance)
	b.Bind(prime)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
	b.Bind(advance)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(scan)
	b.Bind(done).Emit(instr.LOCAL_GET, 4)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func iterativeFibReference(n int32) int32 {
	var current int32
	next := int32(1)
	for range n {
		current, next = next, current+next
	}
	return current
}

func sieveReference(size int32) int32 {
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
