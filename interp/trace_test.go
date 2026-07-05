package interp

import (
	"sync"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewTracer(t *testing.T) {
	tracer := NewTracer()
	require.NotNil(t, tracer)
	require.NotNil(t, tracer.trees)
	require.NotNil(t, tracer.blacklist)
}

func TestTracer_Capture(t *testing.T) {
	t.Run("records top-level fallthrough as completed", func(t *testing.T) {
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		})
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		tr, err := tracer.capture(i, anchor{addr: i.fr.addr, ip: 0})
		require.NoError(t, err)
		require.NotNil(t, tr)
		require.Equal(t, completed, tr.kind)
		require.NotEmpty(t, tr.ops)
		require.Equal(t, instr.I32_CONST, tr.ops[len(tr.ops)-1].op)
	})

	t.Run("records yield as a terminal deopt boundary", func(t *testing.T) {
		// YIELD is a suspension point: capture records it as the trace's terminal
		// (kind=returned) instead of aborting, so the JIT can lower it to a deopt.
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.YIELD),
		})
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		tr, err := tracer.capture(i, anchor{addr: i.fr.addr, ip: 0})
		require.NoError(t, err)
		require.NotNil(t, tr)
		require.Equal(t, returned, tr.kind)
		require.NotEmpty(t, tr.ops)
		require.Equal(t, instr.YIELD, tr.ops[len(tr.ops)-1].op)
	})

	t.Run("records terminal set fast paths and rejects remaining array mutators", func(t *testing.T) {
		tracer := NewTracer()
		require.False(t, tracer.unrecordable(nil, instr.ARRAY_SET))
		require.False(t, tracer.unrecordable(nil, instr.STRUCT_SET))
		for _, op := range []instr.Opcode{
			instr.ARRAY_FILL,
			instr.ARRAY_COPY,
			instr.ARRAY_APPEND,
			instr.ARRAY_DELETE,
			instr.ARRAY_SLICE,
		} {
			require.True(t, tracer.unrecordable(nil, op))
		}
	})
}

func TestTracer_Headers(t *testing.T) {
	t.Run("concurrent calls return identical memoized headers", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 4).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		tracer := NewTracer()
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		const workers = 16
		results := make([][]int, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for w := range workers {
			go func() {
				defer wg.Done()
				results[w] = tracer.headers(i, 0)
			}()
		}
		wg.Wait()

		want := results[0]
		require.NotEmpty(t, want)
		for _, got := range results {
			require.Equal(t, want, got)
		}
	})
}

func TestTracer_Remove(t *testing.T) {
	tracer := NewTracer()
	first := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
	}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 2), instr.New(instr.RETURN)).MustBuild()))
	i := New(first, WithTracer(tracer), WithThreshold(-1))
	defer i.Close()

	exact := tracer.codes(i)
	require.NotNil(t, exact[1])
	tracer.remove(1)
	require.Nil(t, tracer.exact)

	second, err := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 3), instr.New(instr.RETURN)).
		Build()
	require.NoError(t, err)
	i.bind(1, second, true)
	rebuilt := tracer.codes(i)
	require.NotSame(t, &exact[1][0], &rebuilt[1][0])
}
