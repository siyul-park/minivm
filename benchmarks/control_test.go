package benchmarks

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestControl_IterativeFib(t *testing.T) {
	prog := iterativeFib(30)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(iterativeFibReference(30)), value)
}

func TestControl_Sieve(t *testing.T) {
	prog := sieve(256)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(sieveReference(256)), value)
}

func BenchmarkControl_IterativeFib(b *testing.B) {
	b.Run("threaded", func(b *testing.B) {
		prog := iterativeFib(30)
		require.NoError(b, program.Verify(prog))
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(iterativeFibReference(30)), value)
		vm.Reset()

		benchmarkRun(b, vm, types.BoxI32(iterativeFibReference(30)))
	})
}

func BenchmarkControl_Sieve(b *testing.B) {
	b.Run("threaded", func(b *testing.B) {
		prog := sieve(256)
		require.NoError(b, program.Verify(prog))
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(sieveReference(256)), value)
		vm.Reset()

		benchmarkRun(b, vm, types.BoxI32(sieveReference(256)))
	})
}
