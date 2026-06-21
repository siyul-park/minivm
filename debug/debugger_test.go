package debug

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestDebugger(t *testing.T) {
	t.Run("breakpoint stops before instruction", func(t *testing.T) {
		dbg := NewDebugger()
		id := dbg.Break(0, 0)
		i := interp.New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		}), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrStopped)
		require.Equal(t, Stop{Func: 0, IP: 0, Breakpoint: id}, dbg.Stop())
		require.Equal(t, 0, i.Len())

		dbg.Continue()
		err = i.Run(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, i.Len())
		require.Equal(t, uint64(1), dbg.Breakpoints()[0].Hits)
	})

	t.Run("conditional breakpoint", func(t *testing.T) {
		dbg := NewDebugger()
		id := dbg.BreakIf(0, 5, func(i *interp.Interpreter) bool {
			return i.Len() == 1
		})
		i := interp.New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.DROP),
		}), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrStopped)
		require.Equal(t, id, dbg.Stop().Breakpoint)
		require.Equal(t, 1, i.Len())
	})

	t.Run("helpers inspect current frame", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.Break(0, 0)
		i := interp.New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		}), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer i.Close()

		err := i.Run(context.Background())
		require.ErrorIs(t, err, ErrStopped)

		require.Equal(t, 0, i.Func())
		require.Equal(t, 0, i.IP())
		require.Equal(t, 1, i.FP())
		op, err := i.Opcode()
		require.NoError(t, err)
		require.Equal(t, instr.I32_CONST, op)
		fn, ip, bp, err := i.Frame(0)
		require.NoError(t, err)
		require.Equal(t, 0, fn)
		require.Equal(t, 0, ip)
		require.Equal(t, 0, bp)
		_, _, _, err = i.Frame(1)
		require.ErrorIs(t, err, interp.ErrFrameUnderflow)
	})

	makeCallProg := func() *program.Program {
		callee := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.RETURN),
		).MustBuild()
		return program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.DROP),
		}, program.WithConstants(callee))
	}

	t.Run("step enters call", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.Break(0, 3)
		i := interp.New(makeCallProg(), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Step()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		require.Equal(t, 1, i.Func())
		require.Equal(t, 0, i.IP())
		require.Equal(t, 2, i.FP())
		fn, ip, _, err := i.Frame(0)
		require.NoError(t, err)
		require.Equal(t, 1, fn)
		require.Equal(t, 0, ip)
		fn, ip, _, err = i.Frame(1)
		require.NoError(t, err)
		require.Equal(t, 0, fn)
		require.Equal(t, 4, ip)
	})

	t.Run("next steps over call", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.Break(0, 3)
		i := interp.New(makeCallProg(), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Next()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		require.Equal(t, 0, i.Func())
		require.Equal(t, 4, i.IP())
		require.Equal(t, 1, i.FP())
		require.Equal(t, 1, i.Len())
	})

	t.Run("finish stops in caller", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.Break(0, 3)
		i := interp.New(makeCallProg(), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer i.Close()

		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Step()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		dbg.Finish()
		require.ErrorIs(t, i.Run(context.Background()), ErrStopped)
		require.Equal(t, 0, i.Func())
		require.Equal(t, 4, i.IP())
		require.Equal(t, 1, i.FP())
	})
}

func TestDebugger_Breakpoints(t *testing.T) {
	var dbg Debugger
	first := dbg.Break(0, 0)
	second := dbg.Break(0, 1)

	require.True(t, dbg.Enable(first, false))
	require.False(t, dbg.Enable(99, false))
	require.True(t, dbg.Clear(second))
	require.False(t, dbg.Clear(second))

	bps := dbg.Breakpoints()
	require.Len(t, bps, 1)
	require.Equal(t, first, bps[0].ID)
	require.False(t, bps[0].Enabled)
}
