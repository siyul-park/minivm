package interp

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNewTracer(t *testing.T) {
	tracer := NewTracer()
	require.NotNil(t, tracer)
	require.NotNil(t, tracer.trees)
	require.NotNil(t, tracer.blacklist)
}

func TestTracer_Capture(t *testing.T) {
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

	t.Run("rejects array mutators like existing threaded-only array ops", func(t *testing.T) {
		tracer := NewTracer()
		for _, op := range []instr.Opcode{
			instr.ARRAY_SET,
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
