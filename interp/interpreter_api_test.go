package interp

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func newInterp(instrs []instr.Instruction, opts ...func(*option)) *Interpreter {
	return New(program.New(instrs), opts...)
}

func TestInterpreter_Context(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	ctx := context.WithValue(context.Background(), "key", "val")
	i.ctx = ctx
	require.Equal(t, ctx, i.Context())
}

func TestInterpreter_Push_Pop(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	err := i.Push(types.I32(42))
	require.NoError(t, err)

	v, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(42), v)
}

func TestInterpreter_Push_Overflow(t *testing.T) {
	i := newInterp(nil, WithStack(1))
	defer i.Close()

	err := i.Push(types.I32(1))
	require.NoError(t, err)
	err = i.Push(types.I32(2))
	require.ErrorIs(t, err, ErrStackOverflow)
}

func TestInterpreter_Pop_Underflow(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	_, err := i.Pop()
	require.ErrorIs(t, err, ErrStackUnderflow)
}

func TestInterpreter_Len(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	require.Equal(t, -1, i.Len())

	_ = i.Push(types.I32(1))
	require.Equal(t, 0, i.Len())

	_ = i.Push(types.I32(2))
	require.Equal(t, 1, i.Len())
}

func TestInterpreter_Alloc_Load_Store(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	addr, err := i.Alloc(types.I32(7))
	require.NoError(t, err)
	require.Greater(t, addr, 0)

	v, err := i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I32(7), v)

	err = i.Store(addr, types.I64(99))
	require.NoError(t, err)

	v, err = i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I64(99), v)
}

func TestInterpreter_Load_SegFault(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	_, err := i.Load(-1)
	require.ErrorIs(t, err, ErrSegmentationFault)

	_, err = i.Load(9999)
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Store_SegFault(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	err := i.Store(-1, types.I32(1))
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Retain_Release(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	addr, err := i.Alloc(types.I32(5))
	require.NoError(t, err)

	v, err := i.Retain(addr)
	require.NoError(t, err)
	require.Equal(t, types.I32(5), v)

	err = i.Release(addr)
	require.NoError(t, err)

	_, err = i.Load(addr)
	require.NoError(t, err)
}

func TestInterpreter_Retain_SegFault(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	_, err := i.Retain(9999)
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Release_SegFault(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	err := i.Release(9999)
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Alloc_BoxedRef(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	addr, err := i.Alloc(types.BoxI32(3))
	require.NoError(t, err)
	require.Greater(t, addr, 0)
}

func TestInterpreter_Global_SetGlobal(t *testing.T) {
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.GLOBAL_SET, 0),
		},
		program.WithConstants(),
	)
	i := New(prog, WithGlobals(4))
	defer i.Close()

	err := i.Run(context.Background())
	require.NoError(t, err)

	err = i.SetGlobal(0, types.BoxI32(99))
	require.NoError(t, err)

	v, err := i.Global(0)
	require.NoError(t, err)
	require.Equal(t, int32(99), v.I32())
}

func TestInterpreter_Global_SegFault(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	_, err := i.Global(-1)
	require.ErrorIs(t, err, ErrSegmentationFault)

	_, err = i.Global(9999)
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_SetGlobal_SegFault(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	err := i.SetGlobal(-1, types.BoxI32(0))
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Const(t *testing.T) {
	prog := program.New(nil, program.WithConstants(types.I32(42)))
	i := New(prog)
	defer i.Close()

	v, err := i.Const(0)
	require.NoError(t, err)
	require.Equal(t, int32(42), v.I32())
}

func TestInterpreter_Const_SegFault(t *testing.T) {
	i := newInterp(nil)
	defer i.Close()

	_, err := i.Const(-1)
	require.ErrorIs(t, err, ErrSegmentationFault)

	_, err = i.Const(9999)
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Close(t *testing.T) {
	i := newInterp(nil)
	err := i.Close()
	require.NoError(t, err)
}

func TestInterpreter_Reset(t *testing.T) {
	i := newInterp([]instr.Instruction{
		instr.New(instr.I32_CONST, 7),
	})
	defer i.Close()

	err := i.Run(context.Background())
	require.NoError(t, err)
	require.Greater(t, i.Len(), -1)

	i.Reset()
	require.Equal(t, -1, i.Len())
}

func TestInterpreter_HostFunction(t *testing.T) {
	typ := &types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI32},
	}
	fn := NewHostFunction(typ, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
		v := params[0].I32()
		return []types.Boxed{types.BoxI32(v * 2)}, nil
	})

	require.Equal(t, types.KindRef, fn.Kind())
	require.Equal(t, typ, fn.Type())
	require.Contains(t, fn.String(), "<native>")

	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fn),
	)
	i := New(prog)
	defer i.Close()

	err := i.Run(context.Background())
	require.NoError(t, err)

	v, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(10), v)
}
