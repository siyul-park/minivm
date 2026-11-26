package types_test

import (
	"fmt"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// arrayFixture returns a heap whose slot 0 is null and a function that
// allocates a counted reference.
func arrayFixture(hooks types.Hooks) (*types.Heap, func() types.Boxed) {
	h := types.NewHeap(16, 0, hooks)
	h.Add(types.Null)
	n := int32(0)
	return h, func() types.Boxed {
		n++
		return types.BoxRef(h.Add(types.I32(n)))
	}
}

func TestArrayLiteral(t *testing.T) {
	value := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeI32()), Elems: []types.Value{types.I32(1)}}
	require.Len(t, value.Elems, 1)
}

func TestMakeArray(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())
	nulls := func(n int) []types.Boxed {
		elems := make([]types.Boxed, n)
		for i := range elems {
			elems[i] = types.BoxedNull
		}
		return elems
	}

	t.Run("holds size null elements for a reference element", func(t *testing.T) {
		array := types.MakeArray(nil, typ, 3)
		require.Same(t, typ, array.Typ)
		require.Equal(t, nulls(3), array.Elems)
	})

	t.Run("keeps zero words for a scalar element", func(t *testing.T) {
		array := types.MakeArray(nil, types.NewArrayType(types.TypeI32()), 3)
		require.Equal(t, make([]types.Boxed, 3), array.Elems)
	})

	t.Run("leaves the null count unchanged", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		h.Add(types.Null)
		before := h.Count(0)

		types.MakeArray(h, typ, 3)
		require.Equal(t, before, h.Count(0))
	})

	t.Run("reuses a header the heap kept with its cleared storage", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		kept := types.NewArray(nil, typ, types.BoxRef(1), types.BoxRef(2))
		h.Undo(h.Add(kept))

		array := types.MakeArray(h, typ, 2)
		require.Same(t, kept, array)
		require.Equal(t, nulls(2), array.Elems)
	})

	t.Run("grows kept storage that is too small", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		kept := types.NewArray(nil, typ, types.BoxRef(1))
		h.Undo(h.Add(kept))

		array := types.MakeArray(h, typ, 4)
		require.Same(t, kept, array)
		require.Equal(t, nulls(4), array.Elems)
	})
}

func TestNewArray(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())

	t.Run("holds elems", func(t *testing.T) {
		elems := []types.Boxed{types.BoxRef(1), types.BoxRef(2)}
		array := types.NewArray(nil, typ, elems...)

		require.Same(t, typ, array.Typ)
		require.Equal(t, elems, array.Elems)
	})

	t.Run("reuses a header the heap kept with its storage", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		kept := types.NewArray(nil, typ, types.BoxRef(1), types.BoxRef(2))
		h.Undo(h.Add(kept))

		array := types.NewArray(h, typ)
		require.Same(t, kept, array)
		require.Empty(t, array.Elems)
		require.GreaterOrEqual(t, cap(array.Elems), 2)
	})

	t.Run("builds a new header without a heap", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		kept := types.NewArray(nil, typ)
		h.Undo(h.Add(kept))

		require.NotSame(t, kept, types.NewArray(nil, typ))
	})
}

func TestNewArrayType(t *testing.T) {
	typ := types.NewArrayType(types.TypeI32())
	require.Equal(t, types.TypeI32(), typ.Elem)
}

func TestArray_Len(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())
	require.Equal(t, 3, types.MakeArray(nil, typ, 3).Len())
	require.Zero(t, types.NewArray(nil, typ).Len())
}

func TestArray_Index(t *testing.T) {
	h, ref := arrayFixture(types.Hooks{})
	a0 := ref()
	array := types.NewArray(h, types.NewArrayType(types.TypeAny()), a0, types.BoxedNull)

	require.Equal(t, a0, array.Index(0))
	require.Equal(t, types.BoxedNull, array.Index(1))
	require.Equal(t, 1, h.Count(a0.Ref()))
}

func TestArray_SetIndex(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())

	t.Run("takes the new word and releases the replaced one", func(t *testing.T) {
		h, ref := arrayFixture(types.Hooks{})
		old, v := ref(), ref()
		array := types.NewArray(h, typ, old)

		array.SetIndex(0, v)
		require.Equal(t, v, array.Index(0))
		require.False(t, h.Alive(old.Ref()))
		require.Equal(t, 1, h.Count(v.Ref()))
	})

	t.Run("null is counted by the caller and never released", func(t *testing.T) {
		h, ref := arrayFixture(types.Hooks{})
		old := ref()
		array := types.NewArray(h, typ, old, types.BoxedNull)
		h.Retain(types.BoxedNull)
		require.Equal(t, 2, h.Count(0))

		array.SetIndex(0, types.BoxedNull)
		require.Equal(t, 2, h.Count(0))
		require.False(t, h.Alive(old.Ref()))

		array.SetIndex(1, ref())
		require.Equal(t, 2, h.Count(0))
	})

	t.Run("a finalizer sees the new word and may store another index", func(t *testing.T) {
		var array *types.Array
		var seen []types.Boxed
		var other types.Boxed
		h, ref := arrayFixture(types.Hooks{Finalize: func(int, types.Value) {
			seen = append(seen, array.Index(0))
			array.SetIndex(1, types.BoxedNull)
		}})
		other = ref()
		closer := types.BoxRef(h.Add(&closer{}))
		v := ref()
		array = types.NewArray(h, typ, closer, other)

		array.SetIndex(0, v)
		require.Equal(t, []types.Boxed{v}, seen)
		require.False(t, h.Alive(other.Ref()))
		require.Equal(t, 1, h.Count(v.Ref()))
	})

	t.Run("an unbound array panics only over a reference", func(t *testing.T) {
		array := types.NewArray(nil, typ, types.BoxRef(5), types.BoxedNull, types.BoxI32(1))

		require.NotPanics(t, func() { array.SetIndex(1, types.BoxRef(6)) })
		require.NotPanics(t, func() { array.SetIndex(2, types.BoxRef(7)) })
		require.PanicsWithValue(t, types.ErrDetached, func() { array.SetIndex(0, types.BoxedNull) })
		require.Equal(t, types.BoxRef(5), array.Index(0))
	})
}

func TestArray_SwapIndex(t *testing.T) {
	h, ref := arrayFixture(types.Hooks{})
	old, v := ref(), ref()
	array := types.NewArray(h, types.NewArrayType(types.TypeAny()), old)

	require.Equal(t, old, array.SwapIndex(0, v))
	require.Equal(t, v, array.Index(0))
	require.Equal(t, 1, h.Count(old.Ref()))
}

func TestArray_Fill(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())

	for _, n := range []int{1, 3} {
		t.Run(fmt.Sprintf("n=%d takes one reference and adds the rest", n), func(t *testing.T) {
			h, ref := arrayFixture(types.Hooks{})
			v := ref()
			array := types.MakeArray(h, typ, 3)
			for i := range 3 {
				array.SetIndex(i, types.BoxedNull)
			}
			nulls := h.Count(0)

			array.Fill(v, 0, n)
			for i := range n {
				require.Equal(t, v, array.Index(i))
			}
			require.Equal(t, n, h.Count(v.Ref()))
			require.Equal(t, nulls, h.Count(0))
		})
	}

	for _, n := range []int{0, -1} {
		t.Run(fmt.Sprintf("n=%d releases the reference", n), func(t *testing.T) {
			h, ref := arrayFixture(types.Hooks{})
			v := ref()
			array := types.MakeArray(h, typ, 2)

			array.Fill(v, 0, n)
			require.False(t, h.Alive(v.Ref()))
			require.Equal(t, types.BoxedNull, array.Index(0))
		})
	}

	t.Run("fills a null window with counted nulls", func(t *testing.T) {
		h, _ := arrayFixture(types.Hooks{})
		array := types.MakeArray(h, typ, 3)
		before := h.Count(0)

		array.Fill(types.BoxedNull, 1, 2)
		require.Equal(t, before+1, h.Count(0))
	})

	t.Run("releases the replaced words after every store", func(t *testing.T) {
		var array *types.Array
		var lens []int
		var words [][]types.Boxed
		h, ref := arrayFixture(types.Hooks{Finalize: func(int, types.Value) {
			lens = append(lens, array.Len())
			words = append(words, []types.Boxed{array.Index(0), array.Index(1), array.Index(2)})
		}})
		v := ref()
		array = types.NewArray(h, typ,
			types.BoxRef(h.Add(&closer{})), types.BoxRef(h.Add(&closer{})), types.BoxRef(h.Add(&closer{})))

		array.Fill(v, 0, 3)
		require.Equal(t, []int{3, 3}, lens[:2])
		for _, w := range words {
			require.Equal(t, []types.Boxed{v, v, v}, w)
		}
		require.Equal(t, 3, h.Count(v.Ref()))
	})

	t.Run("a finalizer that deletes an element cannot shrink the fill", func(t *testing.T) {
		var array *types.Array
		h, ref := arrayFixture(types.Hooks{Finalize: func(int, types.Value) { array.Delete(0) }})
		v := ref()
		array = types.NewArray(h, typ,
			types.BoxRef(h.Add(&closer{})), types.BoxRef(h.Add(&closer{})), types.BoxedNull, types.BoxedNull)

		require.NotPanics(t, func() { array.Fill(v, 0, 4) })
	})
}

func TestArray_Copy(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())

	t.Run("retains the source words and releases the replaced ones", func(t *testing.T) {
		h, ref := arrayFixture(types.Hooks{})
		a, b, c, d := ref(), ref(), ref(), ref()
		src := types.NewArray(h, typ, a, b)
		dst := types.NewArray(h, typ, c, d)

		dst.Copy(0, src, 0, 2)
		require.Equal(t, []types.Boxed{a, b}, []types.Boxed{dst.Index(0), dst.Index(1)})
		require.Equal(t, 2, h.Count(a.Ref()))
		require.Equal(t, 2, h.Count(b.Ref()))
		require.False(t, h.Alive(c.Ref()))
		require.False(t, h.Alive(d.Ref()))
	})

	t.Run("counts null words", func(t *testing.T) {
		h, _ := arrayFixture(types.Hooks{})
		src := types.NewArray(h, typ, types.BoxedNull, types.BoxedNull)
		dst := types.MakeArray(h, typ, 2)
		before := h.Count(0)

		dst.Copy(0, src, 0, 2)
		require.Equal(t, before+2, h.Count(0))
	})

	t.Run("overlaps forward", func(t *testing.T) {
		h, ref := arrayFixture(types.Hooks{})
		a, b, c := ref(), ref(), ref()
		array := types.NewArray(h, typ, a, b, c)

		array.Copy(1, array, 0, 2)
		require.Equal(t, []types.Boxed{a, a, b}, []types.Boxed{array.Index(0), array.Index(1), array.Index(2)})
		require.Equal(t, 2, h.Count(a.Ref()))
		require.Equal(t, 1, h.Count(b.Ref()))
		require.False(t, h.Alive(c.Ref()))
	})

	t.Run("overlaps backward", func(t *testing.T) {
		h, ref := arrayFixture(types.Hooks{})
		a, b, c := ref(), ref(), ref()
		array := types.NewArray(h, typ, a, b, c)

		array.Copy(0, array, 1, 2)
		require.Equal(t, []types.Boxed{b, c, c}, []types.Boxed{array.Index(0), array.Index(1), array.Index(2)})
		require.Equal(t, 1, h.Count(b.Ref()))
		require.Equal(t, 2, h.Count(c.Ref()))
		require.False(t, h.Alive(a.Ref()))
	})

	t.Run("a finalizer that stores a source word leaves every count exact", func(t *testing.T) {
		var src, dst *types.Array
		var seen []types.Boxed
		h, ref := arrayFixture(types.Hooks{Finalize: func(int, types.Value) {
			seen = append(seen, dst.Index(0), dst.Index(1))
			src.SetIndex(1, types.BoxedNull)
		}})
		o0, o1 := ref(), ref()
		src = types.NewArray(h, typ, o0, o1)
		dst = types.NewArray(h, typ, types.BoxRef(h.Add(&closer{})), types.BoxedNull)

		dst.Copy(0, src, 0, 2)
		require.Equal(t, []types.Boxed{o0, o1}, seen)
		require.Equal(t, []types.Boxed{o0, o1}, []types.Boxed{dst.Index(0), dst.Index(1)})
		require.Equal(t, 2, h.Count(o0.Ref()))
		require.Equal(t, 1, h.Count(o1.Ref()))
	})

	t.Run("an unbound array copies scalars and panics over references", func(t *testing.T) {
		src := types.NewArray(nil, typ, types.BoxI32(1))
		dst := types.NewArray(nil, typ, types.BoxI32(2))
		require.NotPanics(t, func() { dst.Copy(0, src, 0, 1) })
		require.Equal(t, types.BoxI32(1), dst.Index(0))

		refs := types.NewArray(nil, typ, types.BoxRef(5))
		require.PanicsWithValue(t, types.ErrDetached, func() { dst.Copy(0, refs, 0, 1) })
	})
}

func TestArray_Append(t *testing.T) {
	array := types.NewArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(1))
	array.Append(types.BoxRef(2), types.BoxRef(3))

	require.Equal(t, 3, array.Len())
	require.Equal(t, types.BoxRef(3), array.Index(2))
}

func TestArray_Delete(t *testing.T) {
	h, ref := arrayFixture(types.Hooks{})
	a, b, c := ref(), ref(), ref()
	array := types.NewArray(h, types.NewArrayType(types.TypeAny()), a, b, c)

	require.Equal(t, b, array.Delete(1))
	require.Equal(t, 2, array.Len())
	require.Equal(t, []types.Boxed{a, c}, []types.Boxed{array.Index(0), array.Index(1)})
	require.Equal(t, 1, h.Count(b.Ref()))
}

func TestTypedArray_Clone(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		src := types.TypedArray[int32]{1, 2}
		c := src.Clone()
		require.Equal(t, src, c)
		c[0] = 9
		require.Equal(t, int32(1), src[0])
	})

	t.Run("float64", func(t *testing.T) {
		src := types.TypedArray[float64]{1.5, 2.5}
		c := src.Clone()
		require.Equal(t, src, c)
		c[1] = 9
		require.Equal(t, 2.5, src[1])
	})
}

func TestArray_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewArray(nil, types.NewArrayType(types.TypeAny())).Kind())
}

func TestArray_Type(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())
	require.Equal(t, typ, types.NewArray(nil, typ).Type())
}

func TestArray_Refs(t *testing.T) {
	t.Run("primitive elements", func(t *testing.T) {
		a := types.NewArray(nil, types.NewArrayType(types.TypeI32()), types.BoxI32(1), types.BoxI32(2))

		require.Equal(t, []types.Ref{9}, a.Refs([]types.Ref{9}))
		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = a.Refs(nil)
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference elements", func(t *testing.T) {
		a := types.NewArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(1), types.BoxI32(2), types.BoxRef(3))

		require.Equal(t, []types.Ref{9, 1, 3}, a.Refs([]types.Ref{9}))
	})
}

func TestArray_Clone(t *testing.T) {
	typ := types.NewArrayType(types.TypeAny())
	src := types.NewArray(nil, typ, types.BoxRef(1), types.BoxRef(2))
	c := src.Clone()

	require.Same(t, typ, c.Typ)
	require.Equal(t, src.Elems, c.Elems)

	c.Elems[0] = types.BoxRef(9)
	require.Equal(t, types.BoxRef(1), src.Elems[0])

	t.Run("a bound copy retains every reference it holds", func(t *testing.T) {
		h, ref := arrayFixture(types.Hooks{})
		a := ref()
		bound := types.NewArray(h, typ, a, types.BoxedNull, types.BoxI32(1))
		nulls := h.Count(0)

		c := bound.Clone()
		require.Equal(t, 2, h.Count(a.Ref()))
		require.Equal(t, nulls+1, h.Count(0))
		c.SetIndex(0, types.BoxedNull)
		require.Equal(t, 1, h.Count(a.Ref()))
	})

	t.Run("an unbound copy shares its words", func(t *testing.T) {
		h, ref := arrayFixture(types.Hooks{})
		a := ref()
		unbound := types.NewArray(nil, typ, a)

		unbound.Clone()
		require.Equal(t, 1, h.Count(a.Ref()))
	})
}

func TestArray_String(t *testing.T) {
	a := types.NewArray(nil, types.NewArrayType(types.TypeAny()), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))
	require.Equal(t, "[]any{1, 2, 3}", a.String())
}

func TestTypedArray_Kind(t *testing.T) {
	tests := []types.Value{types.TypedArray[int8]{}, types.TypedArray[int32]{}, types.TypedArray[int64]{}, types.TypedArray[float32]{}, types.TypedArray[float64]{}}
	for _, val := range tests {
		t.Run(fmt.Sprint(val), func(t *testing.T) {
			require.Equal(t, types.KindRef, val.Kind())
		})
	}
}

func TestTypedArray_String(t *testing.T) {
	tests := []struct {
		val types.Value
		str string
	}{
		{val: types.TypedArray[int8]{1, 2, 3}, str: "[]i8{1, 2, 3}"},
		{val: types.TypedArray[int32]{1, 2, 3}, str: "[]i32{1, 2, 3}"},
		{val: types.TypedArray[int64]{1, 2, 3}, str: "[]i64{1, 2, 3}"},
		{val: types.TypedArray[float32]{1, 2, 3}, str: "[]f32{1, 2, 3}"},
		{val: types.TypedArray[float64]{1, 2, 3}, str: "[]f64{1, 2, 3}"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestTypedArray_Type(t *testing.T) {
	tests := []struct {
		val types.Value
		typ types.Type
	}{
		{val: types.TypedArray[int8]{}, typ: types.TypeI8Array()},
		{val: types.TypedArray[int32]{}, typ: types.TypeI32Array()},
		{val: types.TypedArray[int64]{}, typ: types.TypeI64Array()},
		{val: types.TypedArray[float32]{}, typ: types.TypeF32Array()},
		{val: types.TypedArray[float64]{}, typ: types.TypeF64Array()},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
}

func TestArrayLiteral_Kind(t *testing.T) {
	value := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeI32())}
	require.Equal(t, types.KindRef, value.Kind())
}

func TestArrayLiteral_Type(t *testing.T) {
	typ := types.NewArrayType(types.TypeI32())
	require.Same(t, typ, (&types.ArrayLiteral{Typ: typ}).Type())
}

func TestArrayLiteral_String(t *testing.T) {
	value := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeI32()), Elems: []types.Value{types.I32(1), types.I32(2)}}
	require.Equal(t, "[]i32{1, 2}", value.String())
}

func TestArrayType_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewArrayType(types.TypeI32()).Kind())
}

func TestArrayType_Cast(t *testing.T) {
	typ := types.NewArrayType(types.TypeI32())

	require.True(t, typ.Cast(types.NewArrayType(types.TypeI32())))
	require.False(t, typ.Cast(types.NewArrayType(types.TypeI64())))
	require.False(t, typ.Cast(types.TypeI32()))
}

func TestArrayType_Equals(t *testing.T) {
	typ := types.NewArrayType(types.TypeI32())

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(types.NewArrayType(types.TypeI32())))
	require.False(t, typ.Equals(types.NewArrayType(types.TypeI64())))
	require.False(t, typ.Equals(types.TypeI32()))
}

func TestArrayType_String(t *testing.T) {
	require.Equal(t, "[]i32", types.NewArrayType(types.TypeI32()).String())
}

func BenchmarkArray_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		a := types.NewArray(nil, types.NewArrayType(types.TypeI32()), types.BoxI32(1), types.BoxI32(2))
		require.Empty(b, a.Refs(nil))

		var refs []types.Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = a.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		a := types.NewArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(1), types.BoxRef(2))
		require.Equal(b, []types.Ref{1, 2}, a.Refs(nil))

		refs := make([]types.Ref, 0, 2)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = a.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []types.Ref{1, 2}, refs)
	})
}
