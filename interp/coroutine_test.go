package interp_test

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	interp "github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type coroutineCycle struct {
	refs []types.Ref
}

func (*coroutineCycle) Kind() types.Kind { return types.KindRef }
func (*coroutineCycle) Type() types.Type { return types.TypeRef }
func (*coroutineCycle) String() string   { return "cycle" }

func (c *coroutineCycle) Refs(dst []types.Ref) []types.Ref {
	return append(dst, c.refs...)
}

func TestCoroutineReferences(t *testing.T) {
	t.Run("keeps closure captures live without duplicate collector edges", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeRef},
			Returns: []types.Type{types.TypeI32},
		}).Captures(types.TypeRef).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.UPVAL_GET, 0),
			instr.New(instr.I32_CONST, 30),
			instr.New(instr.REF_NEW),
			instr.New(instr.YIELD),
			instr.New(instr.DROP),
			instr.New(instr.REF_GET),
			instr.New(instr.SWAP),
			instr.New(instr.REF_GET),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.REF_NEW),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.DUP),
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.REF_NEW),
			instr.New(instr.SWAP),
			instr.New(instr.CALL),
			instr.New(instr.DUP),
			instr.New(instr.CORO_VALUE),
			instr.New(instr.REF_GET),
			instr.New(instr.RESUME),
			instr.New(instr.CORO_VALUE),
		}, program.WithConstants(fn))
		collected := false
		var callableStoreErr error
		var coroutineStoreErr error
		var functionStoreErr error
		vm := interp.New(prog,
			interp.WithHeap(8),
			interp.WithHeapLimit(8),
			interp.WithTick(1),
			interp.WithThreshold(-1),
			interp.WithHook(func(vm *interp.Interpreter) error {
				if collected || vm.IP() != 19 {
					return nil
				}
				collected = true
				callable, err := vm.Peek(1)
				if err != nil {
					return err
				}
				callableStoreErr = vm.Store(callable.Ref(), types.I32(99))
				coroutine, err := vm.Peek(0)
				if err != nil {
					return err
				}
				coroutineStoreErr = vm.Store(coroutine.Ref(), types.I32(99))
				value, err := vm.Load(callable.Ref())
				if err != nil {
					return err
				}
				closure, ok := value.(*types.Closure)
				if !ok {
					return interp.ErrTypeMismatch
				}
				functionStoreErr = vm.Store(int(closure.Fn), types.I32(99))
				cycle := &coroutineCycle{}
				addr, err := vm.Alloc(cycle)
				if err != nil {
					return err
				}
				cycle.refs = []types.Ref{types.Ref(addr)}
				if _, err := vm.Retain(addr); err != nil {
					return err
				}
				if err := vm.Release(addr); err != nil {
					return err
				}
				pressure, err := vm.Alloc(types.I32(1))
				if err != nil {
					return err
				}
				return vm.Release(pressure)
			}),
		)
		defer vm.Close()

		require.NoError(t, vm.Run(context.Background()))
		require.True(t, collected)
		require.ErrorIs(t, callableStoreErr, interp.ErrTypeMismatch)
		require.ErrorIs(t, coroutineStoreErr, interp.ErrTypeMismatch)
		require.ErrorIs(t, functionStoreErr, interp.ErrTypeMismatch)
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(30), value)
	})

	t.Run("preserves coroutine identity through function tail calls", func(t *testing.T) {
		tail := types.NewFunctionBuilder(nil).
			Returns(types.TypeI32).
			Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
			MustBuild()
		fn := types.NewFunctionBuilder(nil).
			Returns(types.TypeI32).
			Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.YIELD),
				instr.New(instr.DROP),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.RETURN_CALL),
			).
			MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.REF_NULL),
			instr.New(instr.RESUME),
			instr.New(instr.CORO_VALUE),
		}, program.WithConstants(fn, tail))
		vm := interp.New(prog, interp.WithThreshold(-1))
		defer vm.Close()

		require.NoError(t, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)
	})

	t.Run("preserves coroutine identity through host tail calls", func(t *testing.T) {
		tail := interp.NewHostFunction(
			&types.FunctionType{Returns: []types.Type{types.TypeI32}},
			func(*interp.Interpreter, []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(42)}, nil
			},
		)
		fn := types.NewFunctionBuilder(nil).
			Returns(types.TypeI32).
			Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.YIELD),
				instr.New(instr.DROP),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.RETURN_CALL),
			).
			MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.REF_NULL),
			instr.New(instr.RESUME),
			instr.New(instr.CORO_VALUE),
		}, program.WithConstants(fn, tail))
		vm := interp.New(prog, interp.WithThreshold(-1))
		defer vm.Close()

		require.NoError(t, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(42), value)
	})

	t.Run("releases discarded completion values", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).
			Returns(types.TypeRef, types.TypeRef).
			Emit(
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.YIELD),
				instr.New(instr.DROP),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.REF_NEW),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.REF_NEW),
				instr.New(instr.RETURN),
			).
			MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.REF_NULL),
			instr.New(instr.RESUME),
			instr.New(instr.DUP),
			instr.New(instr.CORO_VALUE),
			instr.New(instr.REF_GET),
			instr.New(instr.SWAP),
			instr.New(instr.DROP),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 3),
			instr.New(instr.REF_NEW),
			instr.New(instr.I32_CONST, 4),
			instr.New(instr.REF_NEW),
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.REF_NEW),
		}, program.WithConstants(fn))
		vm := interp.New(prog,
			interp.WithHeap(5),
			interp.WithHeapLimit(5),
			interp.WithThreshold(-1),
		)
		defer vm.Close()

		require.NoError(t, vm.Run(context.Background()))
		require.Equal(t, 3, vm.Len())
	})
}
