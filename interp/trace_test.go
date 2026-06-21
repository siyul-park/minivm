package interp

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNewTracer(t *testing.T) {
	tracer := NewTracer()
	require.NotNil(t, tracer)
	require.NotNil(t, tracer.trees)
	require.NotNil(t, tracer.blacklist)
}

func TestInterpreter_flush(t *testing.T) {
	pr := prof.New()
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
	})
	i, _ := New(prog, WithProfiler(pr), WithTick(1), WithThreshold(-1))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	// The member's samples stay local until flush; the profiler reads zero.
	require.Equal(t, uint64(1), i.local.Total())
	v, _ := pr.Metric("vm_samples_total")
	require.Equal(t, float64(0), v)

	i.flush()
	require.Equal(t, float64(1), mustMetric(t, pr, "vm_samples_total"))
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
		i, _ := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		tr, err := tracer.capture(i, anchor{addr: i.fr.addr, ip: 0})
		require.NoError(t, err)
		require.NotNil(t, tr)
		require.Equal(t, returned, tr.kind)
		require.NotEmpty(t, tr.ops)
		require.Equal(t, instr.YIELD, tr.ops[len(tr.ops)-1].op)
	})
}
