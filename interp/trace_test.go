package interp

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNewTracer(t *testing.T) {
	tracer := NewTracer()
	require.NotNil(t, tracer)
	require.NotNil(t, tracer.stats)
	require.NotNil(t, tracer.trees)
	require.NotNil(t, tracer.blacklist)
}

func TestTracer_Profile(t *testing.T) {
	tracer := NewTracer()
	prog := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
	})
	i := New(prog, WithTracer(tracer), WithTick(1), WithThreshold(-1))
	defer i.Close()

	require.NoError(t, i.Run(context.Background()))
	require.Equal(t, uint64(1), i.Profile().Samples)
	require.Equal(t, uint64(0), tracer.Profile().Samples)

	i.flush()
	require.Equal(t, uint64(1), tracer.Profile().Samples)
}
