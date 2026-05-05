package interp

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func nopProg() *program.Program {
	return program.New([]instr.Instruction{instr.New(instr.NOP)})
}

func TestWithOptions(t *testing.T) {
	i := New(nopProg(),
		WithFrame(64),
		WithGlobals(64),
		WithStack(512),
		WithHeap(64),
		WithTick(64),
		WithThreshold(2048),
	)
	require.NotNil(t, i)
	require.NoError(t, i.Close())
}

func TestInterpreter_Context(t *testing.T) {
	i := New(nopProg())
	ctx := context.Background()
	require.NoError(t, i.Run(ctx))
	// After Run completes ctx is nil again; just verify it doesn't panic during run.
}

func TestInterpreter_Const(t *testing.T) {
	prog := program.New(
		[]instr.Instruction{instr.New(instr.NOP)},
		program.WithConstants(types.I32(7)),
	)
	i := New(prog)
	defer i.Close()

	v, err := i.Const(0)
	require.NoError(t, err)
	require.Equal(t, types.KindI32, v.Kind())

	_, err = i.Const(-1)
	require.ErrorIs(t, err, ErrSegmentationFault)

	_, err = i.Const(99)
	require.ErrorIs(t, err, ErrSegmentationFault)
}

func TestInterpreter_Push_Pop_Len(t *testing.T) {
	prog := program.New([]instr.Instruction{instr.New(instr.NOP)})
	i := New(prog, WithStack(16))
	defer i.Close()

	require.NoError(t, i.Push(types.I32(10)))
	require.NoError(t, i.Push(types.I32(20)))
	require.Equal(t, 1, i.Len())

	v, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(20), v)
}

func TestInterpreter_Push_Overflow(t *testing.T) {
	i := New(nopProg(), WithStack(1))
	defer i.Close()

	// stack size 1: first push occupies index 0, second push hits overflow
	require.NoError(t, i.Push(types.I32(1)))
	err := i.Push(types.I32(2))
	require.ErrorIs(t, err, ErrStackOverflow)
}

func TestInterpreter_Pop_Underflow(t *testing.T) {
	i := New(nopProg())
	defer i.Close()

	_, err := i.Pop()
	require.ErrorIs(t, err, ErrStackUnderflow)
}

func TestInterpreter_Alloc_Load_Store_Retain_Release(t *testing.T) {
	i := New(nopProg())
	defer i.Close()

	addr, err := i.Alloc(types.I32(99))
	require.NoError(t, err)
	require.Greater(t, addr, 0)

	v, err := i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I32(99), v)

	require.NoError(t, i.Store(addr, types.I64(42)))
	v, err = i.Load(addr)
	require.NoError(t, err)
	require.Equal(t, types.I64(42), v)

	_, err = i.Retain(addr)
	require.NoError(t, err)
	require.NoError(t, i.Release(addr))

	// Invalid address
	_, err = i.Load(-1)
	require.ErrorIs(t, err, ErrSegmentationFault)
	_, err = i.Retain(-1)
	require.ErrorIs(t, err, ErrSegmentationFault)
	require.ErrorIs(t, i.Release(-1), ErrSegmentationFault)
	require.ErrorIs(t, i.Store(-1, types.I32(0)), ErrSegmentationFault)
}

func TestInterpreter_Alloc_BoxedRef(t *testing.T) {
	i := New(nopProg())
	defer i.Close()

	// Alloc a value first
	addr, err := i.Alloc(types.I32(5))
	require.NoError(t, err)

	// Alloc with a boxed ref returns the same address
	addr2, err := i.Alloc(types.BoxRef(addr))
	require.NoError(t, err)
	require.Equal(t, addr, addr2)
}

func TestInterpreter_Close(t *testing.T) {
	i := New(nopProg())
	require.NoError(t, i.Close())
	// Double close should not panic
	require.NoError(t, i.Close())
}
