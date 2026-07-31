package types_test

import (
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestClosure_Kind(t *testing.T) {
	cl := types.NewClosure(nil, 1, nil)
	require.Equal(t, types.KindRef, cl.Kind())
}

func TestClosure_Type(t *testing.T) {
	typ := &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}
	cl := types.NewClosure(typ, 1, nil)
	require.Equal(t, typ, cl.Type())

	t.Run("shares function type, captures excluded from equality", func(t *testing.T) {
		a := types.NewClosure(typ, 1, []types.Boxed{types.BoxI32(1)})
		b := types.NewClosure(typ, 2, []types.Boxed{types.BoxI32(2), types.BoxRef(3)})
		require.True(t, a.Type().Equals(b.Type()))
		require.True(t, a.Type().Equals(typ))
	})
}

func TestClosure_String(t *testing.T) {
	cl := types.NewClosure(&types.FunctionType{Returns: []types.Type{types.TypeI32}}, 1, nil)
	require.Equal(t, "func() i32", cl.String())
}

func TestClosure_Refs(t *testing.T) {
	t.Run("no upvalues reports the template", func(t *testing.T) {
		cl := types.NewClosure(nil, 7, nil)
		require.Equal(t, []types.Ref{types.Ref(5), types.Ref(7)}, cl.Refs([]types.Ref{5}))
	})

	t.Run("ref upvalues follow the template", func(t *testing.T) {
		cl := types.NewClosure(nil, 7, []types.Boxed{types.BoxI32(1), types.BoxRef(9), types.BoxRef(4)})
		require.Equal(t, []types.Ref{types.Ref(5), types.Ref(7), types.Ref(9), types.Ref(4)}, cl.Refs([]types.Ref{5}))
	})

	t.Run("primitive upvalues are skipped", func(t *testing.T) {
		cl := types.NewClosure(nil, 7, []types.Boxed{types.BoxI32(1), types.BoxF64(2)})
		require.Equal(t, []types.Ref{types.Ref(7)}, cl.Refs(nil))
	})
}

func TestNewClosure(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	ups := []types.Boxed{types.BoxRef(2)}
	cl := types.NewClosure(typ, 5, ups)
	require.Equal(t, typ, cl.Typ)
	require.Equal(t, types.Ref(5), cl.Fn)
	require.Equal(t, ups, cl.Upvals)

	t.Run("nil type defaults to empty", func(t *testing.T) {
		cl := types.NewClosure(nil, 1, nil)
		require.Equal(t, &types.FunctionType{}, cl.Typ)
	})
}
