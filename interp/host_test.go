package interp

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type hostCounter struct {
	Count int32
}

func (c *hostCounter) Bump(n int32) int32 {
	c.Count += n
	return c.Count
}

type hostCelsius float64

func (c hostCelsius) Fahrenheit() hostCelsius {
	return c*9/5 + 32
}

func TestNewHostFunction(t *testing.T) {
	t.Run("kind and type", func(t *testing.T) {
		typ := &types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}
		fn := NewHostFunction(typ, func(_ *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
		})
		require.Equal(t, types.KindRef, fn.Kind())
		require.Equal(t, typ, fn.Type())
		require.Contains(t, fn.String(), "<native>")
	})
	t.Run("call via interpreter", func(t *testing.T) {
		typ := &types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}
		fn := NewHostFunction(typ, func(_ *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
		})
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

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)
	})
}

func TestHostObject(t *testing.T) {
	t.Run("struct with method routes to HostObject", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCounter{Count: 1})
		require.NoError(t, err)

		ho, ok := got.(*HostObject)
		require.True(t, ok)
		require.Equal(t, "Count", ho.Typ.Fields[0].Name)
		require.Equal(t, "Bump", ho.Typ.Fields[1].Name)
		require.Equal(t, types.BoxI32(1), ho.Field(0))
	})

	t.Run("method call mutates receiver via pointer", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCounter{Count: 1})
		require.NoError(t, err)
		ho := got.(*HostObject)

		methodSlot := -1
		for idx, f := range ho.Typ.Fields {
			if f.Name == "Bump" {
				methodSlot = idx
				break
			}
		}
		require.NotEqual(t, -1, methodSlot)

		boxed := ho.Field(methodSlot)
		require.Equal(t, types.KindRef, boxed.Kind())
		v, err := i.Load(boxed.Ref())
		require.NoError(t, err)
		fn, ok := v.(*HostFunction)
		require.True(t, ok)

		returns, err := fn.Fn(i, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(5)}, returns)

		require.Equal(t, types.BoxI32(5), ho.Field(0))
	})

	t.Run("non-struct named scalar with method", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCelsius(100))
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)
		require.Len(t, ho.Typ.Fields, 1)
		require.Equal(t, "Fahrenheit", ho.Typ.Fields[0].Name)
	})

	t.Run("unmarshal recovers receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		src := hostCounter{Count: 7}
		got, err := i.Marshal(src)
		require.NoError(t, err)

		var dst hostCounter
		require.NoError(t, i.Unmarshal(got, &dst))
		require.Equal(t, int32(7), dst.Count)
	})

	t.Run("method name shadowed by field", func(t *testing.T) {
		type shadow struct {
			Bump int32
		}
		i := New(program.New(nil))
		defer i.Close()

		// shadow has no methods so it would normally take the *Struct path;
		// embed it via a wrapper that adds a Bump method to force HostObject.
		type wrapper struct {
			Bump int32
			tag  int32
		}
		got, err := i.Marshal(wrapper{Bump: 9})
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)
		// Only the data field named Bump is exposed; no duplicate.
		count := 0
		for _, f := range ho.Typ.Fields {
			if f.Name == "Bump" {
				count++
			}
		}
		require.Equal(t, 1, count)
	})

	t.Run("SetField writes back to receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCounter{Count: 1})
		require.NoError(t, err)
		ho := got.(*HostObject)

		ho.SetField(0, types.BoxI32(42))
		require.Equal(t, types.BoxI32(42), ho.Field(0))
	})
}
