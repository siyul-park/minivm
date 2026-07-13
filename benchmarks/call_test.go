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
	b.Run("threaded", func(b *testing.B) {
		prog := recursiveFib(20)
		require.NoError(b, program.Verify(prog))
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(recursiveFibReference(20)), value)
		vm.Reset()

		var runErr, popErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			runErr = vm.Run(context.Background())
			b.StopTimer()
			value, popErr = vm.Pop()
			vm.Reset()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, runErr)
		require.NoError(b, popErr)
		require.Equal(b, types.I32(recursiveFibReference(20)), value)
	})
}

func BenchmarkCall_IndirectRecursiveFib(b *testing.B) {
	b.Run("threaded", func(b *testing.B) {
		prog := indirectRecursiveFib(20)
		require.NoError(b, program.Verify(prog))
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(recursiveFibReference(20)), value)
		vm.Reset()

		var runErr, popErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			runErr = vm.Run(context.Background())
			b.StopTimer()
			value, popErr = vm.Pop()
			vm.Reset()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, runErr)
		require.NoError(b, popErr)
		require.Equal(b, types.I32(recursiveFibReference(20)), value)
	})
}

func BenchmarkCall_ClosureCounter(b *testing.B) {
	b.Run("threaded", func(b *testing.B) {
		prog := closureCounter(128)
		require.NoError(b, program.Verify(prog))
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(128), value)
		vm.Reset()

		var runErr, popErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			runErr = vm.Run(context.Background())
			b.StopTimer()
			value, popErr = vm.Pop()
			vm.Reset()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, runErr)
		require.NoError(b, popErr)
		require.Equal(b, types.I32(128), value)
	})
}
