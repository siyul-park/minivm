package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClosure_Kind(t *testing.T) {
	cl := NewClosure(nil, 1, nil)
	require.Equal(t, KindRef, cl.Kind())
}

func TestClosure_Type(t *testing.T) {
	typ := &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI32}}
	cl := NewClosure(typ, 1, nil)
	require.Equal(t, typ, cl.Type())

	t.Run("shares function type, captures excluded from equality", func(t *testing.T) {
		a := NewClosure(typ, 1, []Boxed{BoxI32(1)})
		b := NewClosure(typ, 2, []Boxed{BoxI32(2), BoxRef(3)})
		require.True(t, a.Type().Equals(b.Type()))
		require.True(t, a.Type().Equals(typ))
	})
}

func TestClosure_String(t *testing.T) {
	cl := NewClosure(&FunctionType{Returns: []Type{TypeI32}}, 1, nil)
	require.Equal(t, "func() i32", cl.String())
}

func TestClosure_Refs(t *testing.T) {
	t.Run("no upvalues reports the template", func(t *testing.T) {
		cl := NewClosure(nil, 7, nil)
		require.Equal(t, []Ref{Ref(7)}, cl.Refs())
	})

	t.Run("ref upvalues follow the template", func(t *testing.T) {
		cl := NewClosure(nil, 7, []Boxed{BoxI32(1), BoxRef(9), BoxRef(4)})
		require.Equal(t, []Ref{Ref(7), Ref(9), Ref(4)}, cl.Refs())
	})

	t.Run("primitive upvalues are skipped", func(t *testing.T) {
		cl := NewClosure(nil, 7, []Boxed{BoxI32(1), BoxF64(2)})
		require.Equal(t, []Ref{Ref(7)}, cl.Refs())
	})
}

func TestNewClosure(t *testing.T) {
	typ := &FunctionType{Returns: []Type{TypeI32}}
	ups := []Boxed{BoxRef(2)}
	cl := NewClosure(typ, 5, ups)
	require.Equal(t, typ, cl.Typ)
	require.Equal(t, Ref(5), cl.Fn)
	require.Equal(t, ups, cl.Upvals)

	t.Run("nil type defaults to empty", func(t *testing.T) {
		cl := NewClosure(nil, 1, nil)
		require.Equal(t, &FunctionType{}, cl.Typ)
	})
}
