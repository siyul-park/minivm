package bench

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/stretchr/testify/require"
)

// fibAllocN is a smaller fib input for allocation-focused benchmarks: large
// enough to cross the default JIT sample threshold, small enough that each
// Run call is cheap to repeat once per iteration.
const fibAllocN int32 = 20

// BenchmarkFib35AllocInterpSteady isolates per-iteration allocations for the
// threaded interpreter (JIT disabled) once the interpreter is warm. A single
// untimed Run/Reset cycle runs before ResetTimer so the timed loop measures
// steady-state cost only, not the one-time entry-trace capture that a small
// -benchtime run would otherwise fold into allocs/op.
func BenchmarkFib35AllocInterpSteady(b *testing.B) {
	ctx := context.Background()
	vm := interp.New(Fib(fibN), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(b, vm.Run(ctx))
	vm.Reset()

	b.ReportAllocs()
	b.ResetTimer()
	var err error
	for n := 0; n < b.N; n++ {
		err = vm.Run(ctx)
		vm.Reset()
	}
	b.StopTimer()
	require.NoError(b, err)
}

// BenchmarkFib20AllocJITWarmup isolates the one-time cost of compiling and
// installing a native trace: each iteration builds a fresh interpreter and
// runs it exactly once, so the reported allocs/op is the warmup cost rather
// than a steady-state average diluted by many cheap iterations. It uses
// fib(20) instead of fib(35) to keep the per-iteration setup (interp.New,
// tracer capture, ARM64 compile) affordable at typical -benchtime values.
func BenchmarkFib20AllocJITWarmup(b *testing.B) {
	ctx := context.Background()
	prog := Fib(fibAllocN)

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		vm := interp.New(prog)
		require.NoError(b, vm.Run(ctx))
		vm.Close()
	}
}

// BenchmarkFib35AllocJITSteady isolates per-iteration allocations once a
// native trace is already installed: an untimed Run/Reset cycle before
// ResetTimer forces the one-shot entry compile, so the timed loop measures
// steady-state JIT cost only.
func BenchmarkFib35AllocJITSteady(b *testing.B) {
	ctx := context.Background()
	vm := interp.New(Fib(fibN))
	defer vm.Close()

	require.NoError(b, vm.Run(ctx))
	vm.Reset()

	b.ReportAllocs()
	b.ResetTimer()
	var err error
	for n := 0; n < b.N; n++ {
		err = vm.Run(ctx)
		vm.Reset()
	}
	b.StopTimer()
	require.NoError(b, err)
}
