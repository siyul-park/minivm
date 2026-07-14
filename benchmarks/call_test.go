package benchmarks

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestCall_RecursiveFib(t *testing.T) {
	prog := recursiveFib(20)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(recursiveFibReference(20)), value)
}

func TestCall_IndirectRecursiveFib(t *testing.T) {
	prog := indirectRecursiveFib(20)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(recursiveFibReference(20)), value)
}

func TestCall_ClosureCounter(t *testing.T) {
	prog := closureCounter(128)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(128), value)
}

func BenchmarkCall_RecursiveFib(b *testing.B) {
	const n int32 = 20
	want := recursiveFibReference(n)
	prog := recursiveFib(n)
	require.NoError(b, program.Verify(prog))

	b.Run("threaded", func(b *testing.B) {
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(want), value)
		vm.Reset()

		benchmarkRun(b, vm, types.BoxI32(want))
	})

	compareRecursiveFib(b, n, want)
}

func BenchmarkCall_IndirectRecursiveFib(b *testing.B) {
	const n int32 = 20
	want := recursiveFibReference(n)
	prog := indirectRecursiveFib(n)
	require.NoError(b, program.Verify(prog))

	b.Run("threaded", func(b *testing.B) {
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(want), value)
		vm.Reset()

		benchmarkRun(b, vm, types.BoxI32(want))
	})

	compareIndirectRecursiveFib(b, n, want)
}

func BenchmarkCall_ClosureCounter(b *testing.B) {
	const count = 128
	want := int32(count)
	prog := closureCounter(count)
	require.NoError(b, program.Verify(prog))

	b.Run("threaded", func(b *testing.B) {
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(want), value)
		vm.Reset()

		benchmarkRun(b, vm, types.BoxI32(want))
	})

	compareClosureCounter(b, count, want)
}
