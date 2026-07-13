package benchmarks

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func branchTree(input int32, nodes int) (*program.Program, int32) {
	b := program.NewBuilder()
	b.Locals(types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(input))).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	var want int32
	for index := range nodes {
		left := b.Label()
		join := b.Label()
		threshold := int32((index*17 + 11) % 97)
		leftValue := int32(index%7 + 1)
		rightValue := int32(index%5 + 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(threshold))).Emit(instr.I32_LT_S).BrIf(left)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(rightValue))).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(join)
		b.Bind(left)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(leftValue))).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(join)
		if input < threshold {
			want += leftValue
		} else {
			want += rightValue
		}
	}
	b.Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog, want
}
