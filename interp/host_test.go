package interp_test

import (
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewHostFunction(t *testing.T) {
	t.Run("constructor", func(t *testing.T) {
		typ := &types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}
		fn := interp.NewHostFunction(typ, func(_ *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
		})

		require.Same(t, typ, fn.Typ)
		got, err := fn.Fn(nil, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(8)}, got)
	})

	t.Run("public fields", func(t *testing.T) {
		typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
		fn := &interp.HostFunction{
			Typ: typ,
			Fn: func(*interp.Interpreter, []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(7)}, nil
			},
		}

		require.Same(t, typ, fn.Typ)
		got, err := fn.Fn(nil, nil)
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(7)}, got)
	})
}

func TestHostFunction_Kind(t *testing.T) {
	fn := interp.NewHostFunction(&types.FunctionType{}, nil)
	require.Equal(t, types.KindRef, fn.Kind())
}

func TestHostFunction_Type(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	fn := interp.NewHostFunction(typ, nil)
	require.Same(t, typ, fn.Type())
}

func TestHostFunction_String(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	fn := interp.NewHostFunction(typ, nil)
	require.Equal(t, "func() i32\n<native>", fn.String())
}
