package bench

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// BenchmarkJITIssue60 tracks the workloads added with the #60 ARM64 JIT
// expansion. Each case runs both the threaded interpreter and the JIT-enabled
// interpreter from the same program.
func BenchmarkJITIssue60(b *testing.B) {
	tests := []struct {
		name    string
		program *program.Program
		want    types.Value
	}{
		{name: "indirect_call_fib_via_local", program: indirectFibViaLocal(20), want: types.I32(6765)},
		{name: "closure_counter_loop", program: closureCounter(1024), want: types.I32(1024)},
		{name: "typed_array_sum", program: typedArraySum(1024), want: types.I32(524800)},
	}

	ctx := context.Background()
	for _, tt := range tests {
		b.Run(tt.name+"/interp", func(b *testing.B) {
			vm := interp.New(tt.program, interp.WithThreshold(-1))
			defer vm.Close()

			require.NoError(b, vm.Run(ctx))
			got, err := vm.Pop()
			require.NoError(b, err)
			require.Equal(b, tt.want, got)
			vm.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				err = vm.Run(ctx)
				if err != nil {
					break
				}
				vm.Reset()
			}
			b.StopTimer()
			require.NoError(b, err)
		})

		b.Run(tt.name+"/jit", func(b *testing.B) {
			vm := interp.New(tt.program, interp.WithTick(1), interp.WithThreshold(1))
			defer vm.Close()

			require.NoError(b, vm.Run(ctx))
			got, err := vm.Pop()
			require.NoError(b, err)
			require.Equal(b, tt.want, got)
			vm.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				err = vm.Run(ctx)
				if err != nil {
					break
				}
				vm.Reset()
			}
			b.StopTimer()
			require.NoError(b, err)
		})
	}
}
