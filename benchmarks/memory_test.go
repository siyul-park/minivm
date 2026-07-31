package benchmarks_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkMemory_TypedArraySum(b *testing.B) {
	const size int32 = 256
	want := typedArraySumReference(size)
	prog := typedArraySum(size)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var total int32
			for value := int32(1); value <= size; value++ {
				total += value
			}
			return total
		},
		wazero: "typed_array_sum",
		args:   []uint64{uint64(uint32(size))},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { total := 0; for value := 1; value <= %d; value++ { total += value }; return total }()`, size),
			gopherLua: fmt.Sprintf(`function run() local total = 0; for value = 1, %d do total = total + value end; return total end`, size),
			goja:      fmt.Sprintf(`function run() { let total = 0; for (let value = 1; value <= %d; value++) total += value; return total; }`, size),
			gpython: fmt.Sprintf(`def run():
    total = 0
    for value in range(1, %d + 1): total += value
    return total`, size),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { var total int32; for value := int32(1); value <= %d; value++ { total += value }; return total }`, size),
		},
	}, want)
}

func BenchmarkMemory_AllocationGraph(b *testing.B) {
	const depth int32 = 128
	want := depth
	prog := allocationGraph(depth)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			type node struct{ next *node }
			root := &node{}
			for index := int32(1); index < depth; index++ {
				root = &node{next: root}
			}
			if root == nil {
				return 0
			}
			return depth
		},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { root := [undefined]; for index := 1; index < %d; index++ { root = [root] }; return %d + len(root) - 1 }()`, depth, depth),
			gopherLua: fmt.Sprintf(`function run() local root = {false}; for _ = 2, %d do root = {root} end; return %d + #root - 1 end`, depth, depth),
			goja:      fmt.Sprintf(`function run() { let root = [null]; for (let index = 1; index < %d; index++) root = [root]; return %d + root.length - 1; }`, depth, depth),
			gpython: fmt.Sprintf(`def run():
    root = [None]
    for _ in range(1, %d): root = [root]
    return %d + len(root) - 1`, depth, depth),
			yaegi: fmt.Sprintf(`package bench
type node struct { next *node }
func Run() int32 { root := &node{}; for index := int32(1); index < %d; index++ { root = &node{next: root} }; if root == nil { return 0 }; return %d }`, depth, depth),
		},
	}, want)
}

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
