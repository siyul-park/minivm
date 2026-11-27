package types_test

import (
	"math"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type closer struct{ types.I32 }

func (c *closer) Close() error { return nil }

func TestHeap(t *testing.T) {
	t.Run("collects an unreachable cycle", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny())
		h := types.NewHeap(8, 8, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		a, b := types.NewArray(nil, typ, 0), types.NewArray(nil, typ, 0)
		ra, rb := h.Add(a), h.Add(b)
		a.Append(types.BoxRef(rb))
		b.Append(types.BoxRef(ra))
		for range 5 {
			h.Add(types.I32(1))
		}

		h.Add(types.I32(2))
		require.Equal(t, 7, h.Live())
		require.NotEqual(t, types.Value(a), h.At(ra))
		require.NotEqual(t, types.Value(b), h.At(rb))
	})

	t.Run("keeps a cycle a root holds until the root lets go", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny())
		h := types.NewHeap(8, 0, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		a, b := types.NewArray(nil, typ, 0), types.NewArray(nil, typ, 0)
		ra, rb := h.Add(a), h.Add(b)
		a.Append(types.BoxRef(rb))
		b.Append(types.BoxRef(ra))
		h.Inc(ra, 1)

		for h.Room() > 0 {
			h.Add(types.I32(1))
		}
		h.Add(types.I32(2))
		require.True(t, h.Alive(ra))
		require.True(t, h.Alive(rb))

		h.Dec(ra)
		for h.Room() > 0 {
			h.Add(types.I32(1))
		}
		h.Add(types.I32(2))
		require.NotEqual(t, types.Value(a), h.At(ra))
		require.NotEqual(t, types.Value(b), h.At(rb))
	})

	t.Run("counts null like any slot", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		h.Add(types.Null)
		h.Inc(0, 1)
		require.Equal(t, 2, h.Count(0))
	})
}

func TestHooks(t *testing.T) {
	t.Run("exhaust frees space and the heap retries", func(t *testing.T) {
		var h *types.Heap
		calls := 0
		h = types.NewHeap(2, 2, types.Hooks{Exhaust: func() {
			calls++
			h.Dec(1)
		}})
		h.Add(types.Null)
		h.Add(types.I32(1))

		addr := h.Add(types.I32(2))
		require.Equal(t, 1, calls)
		require.Equal(t, 1, addr)
		require.Equal(t, types.I32(2), h.At(addr))
	})

	t.Run("nil exhaust panics at the limit", func(t *testing.T) {
		h := types.NewHeap(2, 2, types.Hooks{})
		h.Add(types.Null)
		h.Add(types.I32(1))
		require.Panics(t, func() { h.Add(types.I32(2)) })
	})

	t.Run("fault panics from a collection that finds corruption", func(t *testing.T) {
		h := types.NewHeap(2, 2, types.Hooks{Fault: func() { panic("fault") }})
		h.Add(types.Null)
		h.Add(newArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(9)))
		require.PanicsWithValue(t, "fault", func() { h.Add(types.I32(1)) })
	})

	t.Run("nil fault panics from a collection that finds corruption", func(t *testing.T) {
		h := types.NewHeap(2, 2, types.Hooks{})
		h.Add(types.Null)
		h.Add(newArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(9)))
		require.Panics(t, func() { h.Add(types.I32(1)) })
	})

	t.Run("finalize sees only functions and closers leaving their slot", func(t *testing.T) {
		var got []int
		h := types.NewHeap(4, 0, types.Hooks{Finalize: func(addr int, _ types.Value) { got = append(got, addr) }})
		fn := h.Add(&types.Function{Typ: &types.FunctionType{}})
		cl := h.Add(&closer{})
		plain := h.Add(types.I32(1))

		h.Dec(plain)
		require.Empty(t, got)
		h.Dec(fn)
		h.Dec(cl)
		require.Equal(t, []int{fn, cl}, got)
	})

	t.Run("nil finalize does nothing", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		addr := h.Add(&closer{})
		require.NotPanics(t, func() { h.Dec(addr) })
	})

	t.Run("collect reports the slots walked and freed", func(t *testing.T) {
		var slots, freed, calls int
		h := types.NewHeap(8, 8, types.Hooks{Collect: func(s, f int) { slots, freed, calls = s, f, calls+1 }})
		typ := types.NewArrayType(types.TypeAny())
		h.Add(types.Null)
		h.Recount(nil)
		for range 3 {
			a, b := types.NewArray(nil, typ, 0), types.NewArray(nil, typ, 0)
			ra, rb := h.Add(a), h.Add(b)
			a.Append(types.BoxRef(rb))
			b.Append(types.BoxRef(ra))
		}
		h.Add(types.I32(1))
		require.Zero(t, calls)

		h.Add(types.I32(2))
		require.Equal(t, 1, calls)
		require.Equal(t, 8, slots)
		require.Equal(t, 6, freed)
	})

	t.Run("nil collect does nothing", func(t *testing.T) {
		h := types.NewHeap(2, 2, types.Hooks{})
		h.Add(types.Null)
		h.Add(types.I32(1))
		h.Dec(1)
		require.NotPanics(t, func() { h.Add(types.I32(2)) })
	})
}

func TestNewHeap(t *testing.T) {
	t.Run("starts empty", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		require.Equal(t, 0, h.Len())
		require.Equal(t, 0, h.Live())
		require.Equal(t, 0, h.Limit())
	})

	t.Run("keeps the limit", func(t *testing.T) {
		h := types.NewHeap(4, 7, types.Hooks{})
		require.Equal(t, 7, h.Limit())
	})
}

func TestHeap_At(t *testing.T) {
	h := types.NewHeap(2, 0, types.Hooks{})
	addr := h.Add(types.I32(7))
	require.Equal(t, types.I32(7), h.At(addr))
}

func TestHeap_Set(t *testing.T) {
	h := types.NewHeap(2, 0, types.Hooks{})
	addr := h.Add(types.I32(7))
	h.Set(addr, types.I32(8))
	require.Equal(t, types.I32(8), h.At(addr))
	require.Equal(t, 1, h.Count(addr))
}

func TestHeap_Add(t *testing.T) {
	t.Run("returns a new address with one reference", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		first, second := h.Add(types.I32(1)), h.Add(types.I32(2))
		require.Equal(t, 0, first)
		require.Equal(t, 1, second)
		require.Equal(t, 1, h.Count(second))
	})

	t.Run("reuses the last freed slot", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		a, b := h.Add(types.I32(1)), h.Add(types.I32(2))
		h.Dec(a)
		h.Dec(b)
		require.Equal(t, b, h.Add(types.I32(3)))
		require.Equal(t, a, h.Add(types.I32(4)))
	})

	t.Run("paces the next collection from the live set's growth", func(t *testing.T) {
		h := types.NewHeap(256, 0, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		h.Reset()
		h.Add(types.Null)
		h.Recount(nil)
		for n := range 150 {
			h.Add(types.I32(n))
		}
		// Collections at 65 and 129 live slots set the target to 129 + 128.
		require.Equal(t, 257-h.Live(), h.Room())
	})

	t.Run("never rebinds a container of another heap", func(t *testing.T) {
		h, other := types.NewHeap(8, 0, types.Hooks{}), types.NewHeap(8, 0, types.Hooks{})
		other.Add(types.Null)
		old := other.Add(types.I32(1))
		array := newArray(other, types.NewArrayType(types.TypeAny()), types.BoxRef(old))

		h.Add(array)
		array.SetIndex(0, types.BoxedNull)
		require.False(t, other.Alive(old))
	})

	t.Run("grows past its capacity", func(t *testing.T) {
		h := types.NewHeap(0, 0, types.Hooks{})
		for n := range 9 {
			require.Equal(t, n, h.Add(types.I32(n)))
		}
		require.Equal(t, 9, h.Len())
		require.Equal(t, types.I32(8), h.At(8))
	})
}

func TestHeap_Adopt(t *testing.T) {
	arrayTyp := types.NewArrayType(types.TypeAny())
	structTyp := types.NewStructType(types.NewStructField(types.TypeAny()))
	mapTyp := types.NewMapType(types.TypeI32(), types.TypeAny())

	containers := map[string]func(h *types.Heap) types.Value{
		"array":  func(h *types.Heap) types.Value { return newArray(h, arrayTyp, types.BoxRef(1)) },
		"struct": func(h *types.Heap) types.Value { return types.NewStruct(h, structTyp, types.BoxRef(1)) },
		"map": func(h *types.Heap) types.Value {
			m := types.NewMap(h, mapTyp, 0)
			m.Store(types.BoxI32(1), types.BoxRef(1))
			return m
		},
	}
	// release panics with ErrDetached exactly when the container is unbound.
	releases := map[string]func(v types.Value){
		"array":  func(v types.Value) { v.(*types.Array).SetIndex(0, types.BoxedNull) },
		"struct": func(v types.Value) { v.(*types.Struct).SetField(0, types.BoxedNull) },
		"map": func(v types.Value) {
			v.(*types.Map).Store(types.BoxI32(1), types.BoxedNull)
		},
	}

	for name, build := range containers {
		t.Run(name+" binds an unbound container", func(t *testing.T) {
			h := types.NewHeap(4, 0, types.Hooks{})
			h.Add(types.Null)
			h.Add(types.I32(1))
			v := build(nil)
			require.PanicsWithValue(t, types.ErrDetached, func() { releases[name](v) })

			require.True(t, h.Adopt(v))
			require.NotPanics(t, func() { releases[name](v) })
		})

		t.Run(name+" keeps its own heap", func(t *testing.T) {
			h := types.NewHeap(4, 0, types.Hooks{})
			require.True(t, h.Adopt(build(h)))
		})

		t.Run(name+" rejects a foreign container and leaves it bound", func(t *testing.T) {
			h, other := types.NewHeap(4, 0, types.Hooks{}), types.NewHeap(4, 0, types.Hooks{})
			v := build(other)
			require.False(t, h.Adopt(v))
			require.True(t, other.Adopt(v))
		})
	}

	t.Run("accepts a value that is not a container", func(t *testing.T) {
		require.True(t, types.NewHeap(4, 0, types.Hooks{}).Adopt(types.I32(1)))
	})

	t.Run("changes no count", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		addr := h.Add(types.I32(1))
		h.Adopt(newArray(nil, arrayTyp, types.BoxRef(addr)))
		require.Equal(t, 1, h.Count(addr))
		require.Equal(t, 1, h.Count(0))
	})
}

func TestHeap_Inc(t *testing.T) {
	h := types.NewHeap(2, 0, types.Hooks{})
	addr := h.Add(types.I32(1))
	h.Inc(addr, 3)
	require.Equal(t, 4, h.Count(addr))
}

func TestHeap_Dec(t *testing.T) {
	t.Run("keeps a value another reference holds", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		addr := h.Add(types.I32(1))
		h.Inc(addr, 1)
		h.Dec(addr)
		require.True(t, h.Alive(addr))
		require.Equal(t, 1, h.Count(addr))
	})

	t.Run("releases what the last reference held", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		child := h.Add(types.I32(1))
		grandchild := h.Add(types.NewArray(nil, types.NewArrayType(types.TypeAny()), 0))
		parent := h.Add(newArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(child), types.BoxRef(grandchild)))

		h.Dec(parent)
		require.False(t, h.Alive(parent))
		require.False(t, h.Alive(child))
		require.False(t, h.Alive(grandchild))
	})

	t.Run("never releases null through a container", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		h.Dec(h.Add(types.NewArray(h, types.NewArrayType(types.TypeAny()), 2)))
		require.Equal(t, 1, h.Count(0))
		require.True(t, h.Alive(0))
	})

	t.Run("does not reuse an array someone may still hold", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny())
		h := types.NewHeap(4, 0, types.Hooks{})
		array := types.NewArray(nil, typ, 0)
		h.Dec(h.Add(array))
		require.NotSame(t, array, types.NewArray(h, typ, 0))
	})
}

func TestHeap_Retain(t *testing.T) {
	t.Run("counts a reference", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		addr := h.Add(types.I32(1))
		h.Retain(types.BoxRef(addr))
		require.Equal(t, 2, h.Count(addr))
	})

	t.Run("counts null", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		null := h.Add(types.I32(0))
		h.Retain(types.BoxRef(null))
		require.Equal(t, 2, h.Count(null))
	})

	t.Run("ignores a value that is not a reference", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		addr := h.Add(types.I32(1))
		h.Retain(types.BoxI32(7))
		require.Equal(t, 1, h.Count(addr))
	})
}

func TestHeap_Release(t *testing.T) {
	t.Run("removes a reference", func(t *testing.T) {
		h := types.NewHeap(3, 0, types.Hooks{})
		h.Add(types.I32(0))
		addr := h.Add(types.I32(1))
		h.Inc(addr, 1)
		h.Release(types.BoxRef(addr))
		require.Equal(t, 1, h.Count(addr))
	})

	t.Run("reclaims on the last reference", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.I32(0))
		child := h.Add(types.I32(1))
		parent := h.Add(newArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(child)))
		h.Release(types.BoxRef(parent))
		require.False(t, h.Alive(parent))
		require.False(t, h.Alive(child))
	})

	t.Run("skips null", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		null := h.Add(types.I32(0))
		h.Release(types.BoxRef(null))
		require.Equal(t, 1, h.Count(null))
	})

	t.Run("ignores a value that is not a reference", func(t *testing.T) {
		h := types.NewHeap(3, 0, types.Hooks{})
		h.Add(types.I32(0))
		addr := h.Add(types.I32(1))
		h.Release(types.BoxI32(7))
		require.Equal(t, 1, h.Count(addr))
	})
}

func TestHeap_Count(t *testing.T) {
	h := types.NewHeap(2, 0, types.Hooks{})
	addr := h.Add(types.I32(1))
	require.Equal(t, 1, h.Count(addr))
	h.Inc(addr, 2)
	require.Equal(t, 3, h.Count(addr))
}

func TestHeap_Alive(t *testing.T) {
	h := types.NewHeap(2, 0, types.Hooks{})
	addr := h.Add(types.I32(1))
	freed := h.Add(types.I32(2))
	h.Dec(freed)

	for _, tt := range []struct {
		name string
		addr int
		want bool
	}{
		{name: "allocated", addr: addr, want: true},
		{name: "freed", addr: freed, want: false},
		{name: "negative", addr: -1, want: false},
		{name: "past the heap", addr: 2, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, h.Alive(tt.addr))
		})
	}
}

func TestHeap_Len(t *testing.T) {
	h := types.NewHeap(2, 0, types.Hooks{})
	h.Add(types.I32(1))
	h.Add(types.I32(2))
	h.Dec(0)
	require.Equal(t, 2, h.Len())
}

func TestHeap_Live(t *testing.T) {
	h := types.NewHeap(2, 0, types.Hooks{})
	h.Add(types.I32(1))
	h.Add(types.I32(2))
	h.Dec(0)
	require.Equal(t, 1, h.Live())
}

func TestHeap_Dispose(t *testing.T) {
	var finalized []int
	h := types.NewHeap(4, 0, types.Hooks{Finalize: func(addr int, _ types.Value) { finalized = append(finalized, addr) }})
	h.Add(types.Null)
	child := h.Add(types.I32(1))
	holder := h.Add(newArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(child)))
	old := h.At(holder)
	h.Set(holder, &closer{})

	h.Dispose(holder, old)
	require.False(t, h.Alive(child))
	require.True(t, h.Alive(holder))
	require.Equal(t, 1, h.Count(holder))
	require.Empty(t, finalized)

	h.Dispose(holder, h.At(holder))
	require.Equal(t, []int{holder}, finalized)

	nulls := types.NewArray(h, types.NewArrayType(types.TypeAny()), 2)
	holder = h.Add(nulls)
	h.Dispose(holder, nulls)
	require.Equal(t, 1, h.Count(0))
}

func TestHeap_Recount(t *testing.T) {
	t.Run("counts roots and edges", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		leaf := h.Add(types.I32(1))
		root := h.Add(newArray(nil, types.NewArrayType(types.TypeAny()), types.BoxRef(leaf)))
		h.Inc(leaf, 5)

		h.Recount([]types.Boxed{types.BoxRef(root), types.BoxI32(1)})
		require.Equal(t, 1, h.Count(0))
		require.Equal(t, 1, h.Count(leaf))
		require.Equal(t, 1, h.Count(root))
	})

	t.Run("paces from the baseline", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		require.Equal(t, 64, h.Room())
	})

	t.Run("waits for a fresh heap's capacity, then paces a reloaded one from the live set", func(t *testing.T) {
		h := types.NewHeap(256, 0, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		require.Equal(t, 255, h.Room())

		h.Reset()
		h.Add(types.Null)
		h.Recount(nil)
		require.Equal(t, 64, h.Room())
	})

	t.Run("paces within the limit", func(t *testing.T) {
		h := types.NewHeap(4, 10, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		require.Equal(t, 9, h.Room())
	})
}

func TestHeap_Reset(t *testing.T) {
	t.Run("finalizes live dynamic values and empties the heap", func(t *testing.T) {
		var finalized []int
		h := types.NewHeap(8, 0, types.Hooks{Finalize: func(addr int, _ types.Value) { finalized = append(finalized, addr) }})
		h.Add(&closer{})
		h.Recount(nil)
		live := h.Add(&closer{})
		freed := h.Add(&closer{})
		h.Dec(freed)
		finalized = nil

		h.Reset()
		require.Equal(t, []int{live}, finalized)
		require.Equal(t, 0, h.Len())
		require.Equal(t, 0, h.Live())
		require.Equal(t, math.MaxInt, h.Room())
	})

	t.Run("keeps freed structs for reuse", func(t *testing.T) {
		typ := types.NewStructType(types.NewStructField(types.TypeI32()))
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		s := types.NewStruct(nil, typ)
		h.Add(s)

		h.Reset()
		require.Same(t, s, types.NewStruct(h, typ))
	})

	t.Run("keeps freed arrays with their cleared storage", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny())
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		array := newArray(nil, typ, types.BoxI32(1), types.BoxI32(2))
		h.Add(array)

		h.Reset()
		require.Same(t, array, types.NewArray(h, typ, 2))
		require.Equal(t, []types.Boxed{types.BoxedNull, types.BoxedNull}, words(array))
	})
}

func TestHeap_Drain(t *testing.T) {
	typ := types.NewStructType(types.NewStructField(types.TypeI32()))
	h := types.NewHeap(4, 0, types.Hooks{})
	s := types.NewStruct(nil, typ)
	h.Dec(h.Add(s))

	h.Drain()
	require.NotSame(t, s, types.NewStruct(h, typ))
}

func TestHeap_Undo(t *testing.T) {
	t.Run("frees an unpublished value without finalizing it", func(t *testing.T) {
		finalized := 0
		h := types.NewHeap(4, 0, types.Hooks{Finalize: func(int, types.Value) { finalized++ }})
		addr := h.Add(&closer{})

		h.Undo(addr)
		require.False(t, h.Alive(addr))
		require.Equal(t, 0, h.Live())
		require.Zero(t, finalized)
	})

	t.Run("keeps the storage of a generic array without releasing its nulls", func(t *testing.T) {
		typ := types.NewArrayType(types.TypeAny())
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		array := newArray(nil, typ, types.BoxedNull, types.BoxedNull)
		addr := h.Add(array)

		h.Undo(addr)
		require.Equal(t, 1, h.Count(0))
		reused := types.NewArray(h, typ, 0)
		require.Same(t, array, reused)
		require.Zero(t, reused.Len())
		require.Zero(t, testing.AllocsPerRun(1, func() {
			reused.Append(types.BoxedNull, types.BoxedNull)
			reused.Delete(1)
			reused.Delete(0)
		}))
	})
}

func TestHeap_Mark(t *testing.T) {
	h := types.NewHeap(4, 0, types.Hooks{})
	require.Equal(t, 0, h.Mark())
	h.Dec(h.Add(types.I32(1)))
	require.Equal(t, 1, h.Mark())
}

func TestHeap_Shrink(t *testing.T) {
	t.Run("drops the freed slots that end the heap", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.I32(0))
		mark := h.Mark()
		a, b := h.Add(types.I32(1)), h.Add(types.I32(2))
		h.Dec(a)
		h.Dec(b)

		h.Shrink(mark)
		require.Equal(t, 1, h.Len())
		require.Equal(t, 1, h.Live())
	})

	t.Run("keeps the freed slots inside the heap", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.I32(0))
		mark := h.Mark()
		a := h.Add(types.I32(1))
		h.Add(types.I32(2))
		h.Dec(a)

		h.Shrink(mark)
		require.Equal(t, 3, h.Len())
		require.Equal(t, 2, h.Live())
		require.Equal(t, a, h.Add(types.I32(3)))
	})
}

func TestHeap_Room(t *testing.T) {
	t.Run("is unbounded before a baseline", func(t *testing.T) {
		h := types.NewHeap(2, 0, types.Hooks{})
		require.Equal(t, math.MaxInt, h.Room())
	})

	t.Run("shrinks as the heap fills", func(t *testing.T) {
		h := types.NewHeap(4, 0, types.Hooks{})
		h.Add(types.Null)
		h.Recount(nil)
		before := h.Room()
		h.Add(types.I32(1))
		require.Equal(t, before-1, h.Room())
	})
}

func TestHeap_Limit(t *testing.T) {
	h := types.NewHeap(2, 5, types.Hooks{})
	require.Equal(t, 5, h.Limit())
}

func TestHeap_SetLimit(t *testing.T) {
	h := types.NewHeap(2, 2, types.Hooks{})
	h.Add(types.I32(0))
	h.Add(types.I32(1))
	require.Panics(t, func() { h.Add(types.I32(2)) })

	h.SetLimit(0)
	require.Equal(t, 0, h.Limit())
	require.NotPanics(t, func() { h.Add(types.I32(2)) })
}

func TestHeap_Bases(t *testing.T) {
	h := types.NewHeap(1, 0, types.Hooks{})
	h.Add(types.I32(0))
	objects, rc := h.Bases()
	require.NotZero(t, objects)
	require.NotZero(t, rc)

	h.Add(types.I32(1))
	grown, _ := h.Bases()
	require.NotEqual(t, objects, grown)
}
