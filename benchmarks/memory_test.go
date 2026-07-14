package benchmarks

import (
	"context"
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestMemory_TypedArraySum(t *testing.T) {
	prog := typedArraySum(256)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(typedArraySumReference(256)), value)
}

func TestMemory_AllocationGraph(t *testing.T) {
	const depth = 128
	prog := allocationGraph(depth)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	root, err := vm.Local(0)
	require.NoError(t, err)
	ref := root.Ref()
	for index := 0; index < depth; index++ {
		value, err := vm.Load(ref)
		require.NoError(t, err)
		array, ok := value.(*types.Array)
		require.True(t, ok)
		require.Len(t, array.Elems, 1)
		if index+1 == depth {
			require.True(t, types.IsNull(array.Elems[0]))
			break
		}
		ref = array.Elems[0].Ref()
	}
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(depth), value)
}

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
