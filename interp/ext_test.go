package interp

import (
	"context"
	"errors"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

var errExtFail = errors.New("ext fail")

// extArith is a test extension with three ops selected by the EXT operand's low
// byte: 0 doubles the top i32, 1 adds an inline operand, 2 always fails. Lower
// returns false so every op runs threaded.
type extArith struct{}

func (extArith) Types() []instr.Type {
	return []instr.Type{
		{Mnemonic: "x.double", Widths: nil},
		{Mnemonic: "x.addk", Widths: []int{8}},
		{Mnemonic: "x.fail", Widths: nil},
	}
}

func (extArith) Compile(inst instr.Instruction) func(*Interpreter) error {
	switch byte(inst.Operand(0)) {
	case 0:
		return func(i *Interpreter) error {
			v, err := i.Pop()
			if err != nil {
				return err
			}
			n, ok := v.(types.I32)
			if !ok {
				return ErrTypeMismatch
			}
			return i.Push(types.I32(n * 2))
		}
	case 1:
		k := int32(inst.Operand(2))
		return func(i *Interpreter) error {
			v, err := i.Pop()
			if err != nil {
				return err
			}
			n, ok := v.(types.I32)
			if !ok {
				return ErrTypeMismatch
			}
			return i.Push(types.I32(int32(n) + k))
		}
	default:
		return func(*Interpreter) error { return errExtFail }
	}
}

func (extArith) Lower(instr.Instruction, *Emitter) bool { return false }

// extInc adds one to the top i32. Used to prove routing between extensions.
type extInc struct{}

func (extInc) Types() []instr.Type {
	return []instr.Type{{Mnemonic: "y.inc", Widths: nil}}
}

func (extInc) Compile(instr.Instruction) func(*Interpreter) error {
	return func(i *Interpreter) error {
		v, err := i.Pop()
		if err != nil {
			return err
		}
		n, ok := v.(types.I32)
		if !ok {
			return ErrTypeMismatch
		}
		return i.Push(types.I32(n + 1))
	}
}

func (extInc) Lower(instr.Instruction, *Emitter) bool { return false }

func TestRegistry_Register(t *testing.T) {
	t.Run("sequential ids", func(t *testing.T) {
		r := NewRegistry()
		require.Equal(t, uint8(0), r.Register(extArith{}))
		require.Equal(t, uint8(1), r.Register(extInc{}))
	})
	t.Run("overflow panics", func(t *testing.T) {
		r := NewRegistry()
		for n := 0; n < 256; n++ {
			r.Register(extArith{})
		}
		require.Panics(t, func() { r.Register(extArith{}) })
	})
}

func TestWithRegistry(t *testing.T) {
	t.Run("stack-only op", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extArith{})

		b := program.NewBuilder()
		b.Emit(instr.I32_CONST, 5)
		b.Ext(id, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		i, _ := New(prog, WithRegistry(r))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)
	})
	t.Run("op with operand", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extArith{})

		b := program.NewBuilder()
		b.Emit(instr.I32_CONST, 5)
		b.Ext(id, 1, 7)
		prog, err := b.Build()
		require.NoError(t, err)

		i, _ := New(prog, WithRegistry(r))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(12), v)
	})
	t.Run("routes between extensions", func(t *testing.T) {
		r := NewRegistry()
		arith := r.Register(extArith{})
		inc := r.Register(extInc{})

		b := program.NewBuilder()
		b.Emit(instr.I32_CONST, 5)
		b.Ext(arith, 0) // 10
		b.Ext(inc, 0)   // 11
		prog, err := b.Build()
		require.NoError(t, err)

		i, _ := New(prog, WithRegistry(r))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(11), v)
	})
	t.Run("captures registry at construction", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extArith{})

		b := program.NewBuilder()
		b.Emit(instr.I32_CONST, 5)
		b.Ext(id, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		i, _ := New(prog, WithRegistry(r))
		defer i.Close()
		r.exts[id] = extInc{}

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)
	})
	t.Run("fuses before ext", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extArith{})
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32).Emit(
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 3),
			instr.New(instr.I32_ADD), // fuses LOCAL_GET+CONST+ADD -> 8
			instr.New(instr.EXT, uint64(id)<<8, 0),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		i, _ := New(prog, WithRegistry(r))
		defer i.Close()
		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(16), v)
	})
	t.Run("unregistered ext traps", func(t *testing.T) {
		r := NewRegistry()
		r.Register(extArith{})

		b := program.NewBuilder()
		b.Emit(instr.I32_CONST, 5)
		b.Ext(9, 0) // no extension at slot 9
		prog, err := b.Build()
		require.NoError(t, err)

		i, _ := New(prog, WithRegistry(r))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), ErrUnknownOpcode)
	})
	t.Run("handler error surfaces", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extArith{})

		b := program.NewBuilder()
		b.Emit(instr.I32_CONST, 5)
		b.Ext(id, 2) // x.fail
		prog, err := b.Build()
		require.NoError(t, err)

		i, _ := New(prog, WithRegistry(r))
		defer i.Close()
		require.ErrorIs(t, i.Run(context.Background()), errExtFail)
	})
}
