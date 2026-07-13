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

func TestNewDebugger(t *testing.T) {
	require.NotNil(t, NewDebugger())
}

func TestDebugger_Hook(t *testing.T) {
	dbg := NewDebugger()
	id := dbg.Break(0, 0)
	vm := interp.New(program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7)}),
		interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	require.Equal(t, Stop{Func: 0, IP: 0, Breakpoint: id}, dbg.Stop())
	require.Zero(t, vm.Len())
}

func TestDebugger_Stop(t *testing.T) {
	dbg := NewDebugger()
	require.Zero(t, dbg.Stop())
	id := dbg.Break(0, 0)
	vm := interp.New(program.New([]instr.Instruction{instr.New(instr.NOP)}),
		interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	require.Equal(t, Stop{Func: 0, IP: 0, Breakpoint: id}, dbg.Stop())
}

func TestDebugger_Continue(t *testing.T) {
	dbg := NewDebugger()
	dbg.Continue()
	dbg.Break(0, 0)
	vm := interp.New(program.New([]instr.Instruction{instr.New(instr.I32_CONST, 7)}),
		interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	dbg.Continue()
	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(7), value)
}

func TestDebugger_Step(t *testing.T) {
	callee := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
		instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
	).MustBuild()
	prog := program.New([]instr.Instruction{
		instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.DROP),
	}, program.WithConstants(callee))
	dbg := NewDebugger()
	dbg.Break(0, 3)
	vm := interp.New(prog, interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	dbg.Step()
	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	require.Equal(t, 1, vm.Func())
	require.Equal(t, 0, vm.IP())
	require.Equal(t, 2, vm.FP())
}

func TestDebugger_Next(t *testing.T) {
	NewDebugger().Next()
	callee := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
		instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
	).MustBuild()
	prog := program.New([]instr.Instruction{
		instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.DROP),
	}, program.WithConstants(callee))
	dbg := NewDebugger()
	dbg.Break(0, 3)
	vm := interp.New(prog, interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	dbg.Next()
	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	require.Equal(t, 0, vm.Func())
	require.Equal(t, 4, vm.IP())
	require.Equal(t, 1, vm.FP())
	require.Equal(t, 1, vm.Len())
}

func TestDebugger_Finish(t *testing.T) {
	NewDebugger().Finish()
	callee := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
		instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
	).MustBuild()
	prog := program.New([]instr.Instruction{
		instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.DROP),
	}, program.WithConstants(callee))
	dbg := NewDebugger()
	dbg.Break(0, 3)
	vm := interp.New(prog, interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	dbg.Step()
	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	dbg.Finish()
	require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
	require.Equal(t, 0, vm.Func())
	require.Equal(t, 4, vm.IP())
	require.Equal(t, 1, vm.FP())
}

func TestDebugger_Break(t *testing.T) {
	dbg := NewDebugger()
	first := dbg.Break(0, 1)
	second := dbg.Break(2, 3)
	require.NotEqual(t, first, second)
	require.Equal(t, []Breakpoint{
		{ID: first, Func: 0, IP: 1, Enabled: true},
		{ID: second, Func: 2, IP: 3, Enabled: true},
	}, dbg.Breakpoints())
}

func TestDebugger_BreakIf(t *testing.T) {
	t.Run("stops when condition matches", func(t *testing.T) {
		dbg := NewDebugger()
		id := dbg.BreakIf(0, 5, func(vm *interp.Interpreter) bool { return vm.Len() == 1 })
		vm := interp.New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.DROP),
		}), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()

		require.ErrorIs(t, vm.Run(context.Background()), ErrStopped)
		require.Equal(t, id, dbg.Stop().Breakpoint)
		require.Equal(t, 1, vm.Len())
	})

	t.Run("continues when condition does not match", func(t *testing.T) {
		dbg := NewDebugger()
		dbg.BreakIf(0, 5, func(*interp.Interpreter) bool { return false })
		vm := interp.New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.DROP),
		}), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()

		require.NoError(t, vm.Run(context.Background()))
	})
}

func TestDebugger_Clear(t *testing.T) {
	dbg := NewDebugger()
	id := dbg.Break(0, 0)
	require.True(t, dbg.Clear(id))
	require.False(t, dbg.Clear(id))
	require.Empty(t, dbg.Breakpoints())
}

func TestDebugger_Enable(t *testing.T) {
	dbg := NewDebugger()
	id := dbg.Break(0, 0)
	require.True(t, dbg.Enable(id, false))
	require.False(t, dbg.Breakpoints()[0].Enabled)
	require.False(t, dbg.Enable(99, false))
	require.True(t, dbg.Enable(id, true))
	require.True(t, dbg.Breakpoints()[0].Enabled)
}

func TestDebugger_Breakpoints(t *testing.T) {
	var dbg Debugger
	first := dbg.Break(0, 0)
	second := dbg.Break(0, 1)
	breakpoints := dbg.Breakpoints()
	require.Equal(t, []Breakpoint{
		{ID: first, Func: 0, IP: 0, Enabled: true},
		{ID: second, Func: 0, IP: 1, Enabled: true},
	}, breakpoints)

	breakpoints[0].Enabled = false
	require.True(t, dbg.Breakpoints()[0].Enabled)
}
