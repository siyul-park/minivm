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
		{
			name:    "indirect_call_fib_via_local",
			program: indirectFibViaLocal(20),
			want:    types.I32(6765),
		},
		{
			name:    "closure_counter_loop",
			program: closureCounter(1024),
			want:    types.I32(1024),
		},
		{
			name:    "typed_array_sum",
			program: typedArraySum(1024),
			want:    types.I32(524800),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.Run("interp", func(b *testing.B) {
				vm, _ := interp.New(tt.program, interp.WithThreshold(-1))
				runMiniVMProgram(b, vm, tt.want)
			})
			b.Run("jit", func(b *testing.B) {
				vm, _ := interp.New(
					tt.program,
					interp.WithTick(1),
					interp.WithThreshold(1),
				)
				runMiniVMProgram(b, vm, tt.want)
			})
		})
	}
}

func runMiniVMProgram(b *testing.B, i *interp.Interpreter, want types.Value) {
	b.Helper()
	defer i.Close()

	ctx := context.Background()
	require.NoError(b, i.Run(ctx))
	got, err := i.Pop()
	require.NoError(b, err)
	require.Equal(b, want, got)
	i.Reset()

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		err = i.Run(ctx)
		if err != nil {
			break
		}
		i.Reset()
	}
	b.StopTimer()
	require.NoError(b, err)
}
