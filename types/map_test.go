package types_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func entryLines[K comparable](rangeFn func(func(K, types.Boxed))) []string {
	var out []string
	rangeFn(func(key K, value types.Boxed) {
		out = append(out, fmt.Sprintf("%v/%d", key, uint64(value)))
	})
	sort.Strings(out)
	return out
}

// TestTypedMap owns the whole-map contract: every key kind behaves exactly as the Go-map model.
func TestTypedMap(t *testing.T) {
	negZero32 := float32(math.Copysign(0, -1))
	negZero64 := math.Copysign(0, -1)
	floats32 := []float32{0, negZero32, float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), math.SmallestNonzeroFloat32, -math.SmallestNonzeroFloat32, math.MaxFloat32, 1, -1, 1.5}
	floats64 := []float64{0, negZero64, math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64, math.MaxFloat64, 1, -1, 1.5}
	ints := func(limit int) []int64 {
		out := []int64{0, 1, -1}
		for i := range limit {
			out = append(out, int64(i*7919-limit))
		}
		return out
	}
	pool := func(limit int) []int32 {
		var out []int32
		for _, v := range ints(limit) {
			out = append(out, int32(v))
		}
		return append(out, math.MinInt32, math.MaxInt32)
	}

	tests := []struct {
		name string
		run  func(t *testing.T, seed uint64)
	}{
		{"i1", func(t *testing.T, seed uint64) {
			replay(t, types.NewMapType(types.TypeI1(), types.TypeI32()), []bool{false, true}, 400, seed)
		}},
		{"i8", func(t *testing.T, seed uint64) {
			var keys []int8
			for i := math.MinInt8; i <= math.MaxInt8; i++ {
				keys = append(keys, int8(i))
			}
			replay(t, types.NewMapType(types.TypeI8(), types.TypeI32()), keys, 4000, seed)
		}},
		{"i32 small pool", func(t *testing.T, seed uint64) {
			replay(t, types.NewMapType(types.TypeI32(), types.TypeI32()), pool(6), 4000, seed)
		}},
		{"i32 growth", func(t *testing.T, seed uint64) {
			replay(t, types.NewMapType(types.TypeI32(), types.TypeI32()), pool(3000), 20000, seed)
		}},
		{"i64 extremes", func(t *testing.T, seed uint64) {
			keys := []int64{0, 1, -1, math.MinInt64, math.MaxInt64, 1 << 50, -(1 << 50), 1 << 32, 1 << 63 >> 1}
			replay(t, types.NewMapType(types.TypeI64(), types.TypeI32()), keys, 4000, seed)
		}},
		{"i64 growth", func(t *testing.T, seed uint64) {
			replay(t, types.NewMapType(types.TypeI64(), types.TypeI32()), ints(3000), 20000, seed)
		}},
		{"f32 edge keys", func(t *testing.T, seed uint64) {
			replay(t, types.NewMapType(types.TypeF32(), types.TypeI32()), floats32, 4000, seed)
		}},
		{"f64 edge keys", func(t *testing.T, seed uint64) {
			replay(t, types.NewMapType(types.TypeF64(), types.TypeI32()), floats64, 4000, seed)
		}},
		{"f64 growth", func(t *testing.T, seed uint64) {
			var keys []float64
			for _, v := range ints(1500) {
				keys = append(keys, float64(v)/8)
			}
			replay(t, types.NewMapType(types.TypeF64(), types.TypeI32()), append(keys, floats64...), 20000, seed)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for seed := uint64(1); seed <= 5; seed++ {
				tt.run(t, seed)
			}
		})
	}
}

// replay applies one random operation sequence to a TypedMap and to a Go map,
// the reference model, and requires every observable result to agree.
func replay[K types.Scalar](t *testing.T, typ *types.MapType, pool []K, ops int, seed uint64) {
	rng := rand.New(rand.NewPCG(seed, 1))
	m := types.NewTypedMap[K](typ, rng.IntN(4)*8)
	model := map[K]types.Boxed{}
	modelLines := func() []string {
		return entryLines[K](func(fn func(K, types.Boxed)) {
			for k, v := range model {
				fn(k, v)
			}
		})
	}
	for n := 0; n < ops; n++ {
		key := pool[rng.IntN(len(pool))]
		value := types.BoxI32(int32(rng.IntN(1 << 20)))
		switch op := rng.IntN(100); {
		case op < 40:
			wantOld, wantOK := model[key]
			model[key] = value
			gotOld, gotOK := m.Set(key, value)
			require.Equal(t, wantOK, gotOK, "set %v", key)
			require.Equal(t, wantOld, gotOld, "set %v", key)
		case op < 60:
			want, wantOK := model[key]
			got, gotOK := m.Get(key)
			require.Equal(t, wantOK, gotOK, "get %v", key)
			require.Equal(t, want, got, "get %v", key)
		case op < 90:
			wantOld, wantOK := model[key]
			delete(model, key)
			gotOld, gotOK := m.Delete(key)
			require.Equal(t, wantOK, gotOK, "delete %v", key)
			require.Equal(t, wantOld, gotOld, "delete %v", key)
		case op < 92:
			var want, got []string
			for _, v := range model {
				want = append(want, fmt.Sprint(uint64(v)))
			}
			clear(model)
			m.Clear(func(v types.Boxed) { got = append(got, fmt.Sprint(uint64(v))) })
			require.ElementsMatch(t, want, got, "clear")
		case op < 96:
			require.Equal(t, modelLines(), entryLines[K](m.Range), "range")
		default:
			c := m.Clone()
			require.Equal(t, modelLines(), entryLines[K](c.Range), "clone")
			c.Set(key, value)
			c.Delete(key)
			require.Equal(t, modelLines(), entryLines[K](m.Range), "clone shares storage")
		}
		require.Equal(t, len(model), m.Len(), "len after op %d", n)
	}
	require.Equal(t, modelLines(), entryLines[K](m.Range))
}

func TestMapLiteral(t *testing.T) {
	value := &types.MapLiteral{Typ: types.NewMapType(types.TypeI32(), types.TypeString()), Keys: []types.Value{types.I32(1)}, Values: []types.Value{types.String("a")}}
	require.Len(t, value.Keys, 1)
	require.Len(t, value.Values, 1)
}

func TestNewMapForType(t *testing.T) {
	structType := types.NewStructType(types.NewStructField(types.TypeI32()))
	tests := []struct {
		typ  *types.MapType
		want any
	}{
		{typ: types.NewMapType(types.TypeI32(), types.TypeI32()), want: (*types.TypedMap[int32])(nil)},
		{typ: types.NewMapType(types.TypeI64(), types.TypeI32()), want: (*types.TypedMap[int64])(nil)},
		{typ: types.NewMapType(types.TypeF32(), types.TypeI32()), want: (*types.TypedMap[float32])(nil)},
		{typ: types.NewMapType(types.TypeF64(), types.TypeI32()), want: (*types.TypedMap[float64])(nil)},
		{typ: types.NewMapType(types.TypeAny(), types.TypeI32()), want: (*types.Map)(nil)},
		{typ: types.NewMapType(types.TypeString(), types.TypeI32()), want: (*types.StringMap)(nil)},
		{typ: types.NewMapType(structType, types.TypeI32()), want: (*types.Map)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.typ.Key.String(), func(t *testing.T) {
			require.IsType(t, tt.want, types.NewMapForType(tt.typ, 0))
		})
	}
}

func TestNewTypedMap(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeAny())
	m := types.NewTypedMap[int32](typ, 4)
	require.Same(t, typ, m.Typ)
	require.Equal(t, types.BoxedNull, m.Zero)
	require.Zero(t, m.Len())
}

func TestNewStringMap(t *testing.T) {
	typ := types.NewMapType(types.TypeString(), types.TypeAny())
	m := types.NewStringMap(typ, 4)
	require.Same(t, typ, m.Typ)
	require.Equal(t, types.BoxedNull, m.Zero)
	require.Zero(t, m.Len())
}

func TestNewMap(t *testing.T) {
	typ := types.NewMapType(types.TypeAny(), types.TypeI32())
	m := types.NewMap(typ)
	require.Same(t, typ, m.Typ)
	require.Equal(t, types.BoxI32(0), m.Zero)
	require.Zero(t, m.Len())
}

func TestNewMapWithCapacity(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeAny())
	m := types.NewMapWithCapacity(typ, 8)
	require.Same(t, typ, m.Typ)
	require.Equal(t, types.BoxedNull, m.Zero)
	require.Zero(t, m.Len())
}

func TestNewMapType(t *testing.T) {
	t.Run("string key and i64 value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeString(), types.TypeI64())
		require.Equal(t, types.TypeString(), typ.Key)
		require.Equal(t, types.TypeI64(), typ.Elem)
		require.Equal(t, types.KindRef, typ.KeyKind)
		require.Equal(t, types.KindI64, typ.ElemKind)
		require.False(t, typ.TraceKeys)
		require.True(t, typ.TraceValues)
	})

	t.Run("primitive key and value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32(), types.TypeI32())
		require.False(t, typ.TraceKeys)
		require.False(t, typ.TraceValues)
	})
}

func TestNewMapIterator(t *testing.T) {
	m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
	it := types.NewMapIterator(7, m)
	require.Equal(t, types.NewIteratorType(types.TypeI64()), it.Type())
	require.True(t, it.Done())
}

func TestTypedMap_Get(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))
		got, ok := m.Get(1)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("i64 wide key", func(t *testing.T) {
		m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		m.Set(1<<50, types.BoxI32(2))
		got, ok := m.Get(1 << 50)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		_, ok = m.Get(2)
		require.False(t, ok)
	})

	t.Run("f64", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		m.Set(1.5, types.BoxI32(2))
		got, ok := m.Get(1.5)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
	})
}

func TestTypedMap_Set(t *testing.T) {
	t.Run("overwrite returns old", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		old, ok := m.Set(1, types.BoxI32(2))
		require.False(t, ok)
		require.Equal(t, types.Boxed(0), old)

		old, ok = m.Set(1, types.BoxI32(3))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), old)
	})

	t.Run("f32 -0.0 collapses to +0.0", func(t *testing.T) {
		m := types.NewTypedMap[float32](types.NewMapType(types.TypeF32(), types.TypeI32()), 0)
		m.Set(float32(math.Copysign(0, -1)), types.BoxI32(1))
		m.Set(0, types.BoxI32(2))

		got, ok := m.Get(float32(math.Copysign(0, -1)))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 -0.0 collapses to +0.0", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		m.Set(math.Copysign(0, -1), types.BoxI32(1))
		m.Set(0, types.BoxI32(2))

		got, ok := m.Get(math.Copysign(0, -1))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		require.Equal(t, 1, m.Len())
	})

	t.Run("f64 NaN is not retrievable", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		m.Set(math.NaN(), types.BoxI32(1))
		_, ok := m.Get(math.NaN())
		require.False(t, ok)
	})

	t.Run("f64 NaN adds an entry per set", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		for i := range 40 {
			_, ok := m.Set(math.NaN(), types.BoxI32(int32(i)))
			require.False(t, ok)
		}
		_, ok := m.Delete(math.NaN())
		require.False(t, ok)
		visited := 0
		m.Range(func(float64, types.Boxed) { visited++ })
		require.Equal(t, 40, m.Len())
		require.Equal(t, 40, visited)
	})

	t.Run("f64 reports the latest written zero", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		for _, key := range []float64{0, math.Copysign(0, -1), 0, math.Copysign(0, -1)} {
			m.Set(key, types.BoxI32(1))
			m.Range(func(got float64, _ types.Boxed) {
				require.Equal(t, math.Signbit(key), math.Signbit(got))
			})
		}
	})

	t.Run("keys crafted against the unseeded hash do not cluster", func(t *testing.T) {
		const multiplier = uint64(0x9E3779B97F4A7C15)
		inverse := multiplier
		for range 6 {
			inverse *= 2 - multiplier*inverse
		}
		require.Equal(t, uint64(1), inverse*multiplier)

		const n = 64
		m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		for k := uint64(1); k <= n; k++ {
			m.Set(int64(k*inverse), types.BoxI32(1))
		}
		var order []int64
		m.Range(func(key int64, _ types.Boxed) { order = append(order, key) })
		require.Len(t, order, n)
		runs := 0
		for i := 1; i < len(order); i++ {
			if uint64(order[i]) == uint64(order[i-1])+inverse {
				runs++
			}
		}
		require.Less(t, runs, n/2, "crafted keys share one probe cluster")
	})

	t.Run("tables built the same way hash with different seeds", func(t *testing.T) {
		orders := map[string]bool{}
		for range 16 {
			m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
			for k := range int64(24) {
				m.Set(k, types.BoxI32(1))
			}
			var order []int64
			m.Range(func(key int64, _ types.Boxed) { order = append(order, key) })
			orders[fmt.Sprint(order)] = true
		}
		require.Greater(t, len(orders), 1, "every table walked its slots in the same order")
	})

	t.Run("reuses deleted slots without rehashing", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(1))
		allocs := testing.AllocsPerRun(5, func() {
			for range 64 {
				m.Set(100, types.BoxI32(1))
				m.Delete(100)
			}
		})
		require.Zero(t, allocs)
	})
}

func TestTypedMap_Delete(t *testing.T) {
	t.Run("returns the removed value", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))

		old, ok := m.Delete(1)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), old)
		require.Equal(t, 0, m.Len())

		_, ok = m.Delete(1)
		require.False(t, ok)
	})

	t.Run("churn at a fixed size keeps lookups bounded and storage small", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		for i := int32(0); i < 50000; i++ {
			m.Set(i, types.BoxI32(i))
			if i >= 3 {
				_, ok := m.Delete(i - 3)
				require.True(t, ok)
			}
			_, ok := m.Get(-1)
			require.False(t, ok)
		}
		require.Equal(t, 3, m.Len())

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		c := m.Clone()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(c)
		require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(4096), "tombstones alone grew the table")
	})
}
func TestTypedMap_Clear(t *testing.T) {
	t.Run("integer keys", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))

		var values []types.Boxed
		m.Clear(func(value types.Boxed) {
			values = append(values, value)
		})
		require.Equal(t, []types.Boxed{types.BoxI32(2)}, values)
		require.Zero(t, m.Len())
	})

	t.Run("NaN keys release each value once", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeAny()), 0)
		m.Set(math.NaN(), types.BoxRef(2))
		m.Set(math.NaN(), types.BoxRef(3))
		m.Set(1, types.BoxRef(4))

		var values []types.Boxed
		m.Clear(func(value types.Boxed) {
			values = append(values, value)
		})
		require.ElementsMatch(t, []types.Boxed{types.BoxRef(2), types.BoxRef(3), types.BoxRef(4)}, values)
		require.Zero(t, m.Len())
		require.Empty(t, m.Refs(nil))
		m.Clear(func(types.Boxed) { t.Fatal("value released twice") })
	})
}
func TestTypedMap_Len(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
	require.Equal(t, 0, m.Len())
	m.Set(1, types.BoxI32(2))
	require.Equal(t, 1, m.Len())
}
func TestTypedMap_Range(t *testing.T) {
	m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
	m.Set(1, types.BoxI32(2))

	var keys []int64
	m.Range(func(key int64, _ types.Boxed) {
		keys = append(keys, key)
	})
	require.Equal(t, []int64{1}, keys)
}

func TestStringMap_Get(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	m.Set("a", types.BoxI32(2))
	got, ok := m.Get("a")
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), got)
	_, ok = m.Get("b")
	require.False(t, ok)
}

func TestStringMap_Set(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	old, ok := m.Set("a", types.BoxI32(2))
	require.False(t, ok)
	require.Equal(t, types.Boxed(0), old)
	old, ok = m.Set("a", types.BoxI32(3))
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), old)
}

func TestStringMap_Delete(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	m.Set("a", types.BoxI32(2))
	old, ok := m.Delete("a")
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), old)
	_, ok = m.Delete("a")
	require.False(t, ok)
}

func TestStringMap_Clear(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	m.Set("a", types.BoxI32(2))
	var values []types.Boxed
	m.Clear(func(value types.Boxed) { values = append(values, value) })
	require.Equal(t, []types.Boxed{types.BoxI32(2)}, values)
	require.Zero(t, m.Len())
}

func TestStringMap_Len(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	m.Set("a", types.BoxI32(2))
	require.Equal(t, 1, m.Len())
}

func TestStringMap_Range(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	m.Set("a", types.BoxI32(2))
	var keys []string
	m.Range(func(key string, _ types.Boxed) { keys = append(keys, key) })
	require.Equal(t, []string{"a"}, keys)
}

func TestMap_Get(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeString(), types.TypeI32()))
	m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{Key: types.BoxRef(1), Value: types.BoxI32(2)})

	entry, ok := m.Get(types.MapKey{Kind: types.KindRef, Bits: 1})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), entry.Value)

	_, ok = m.Get(types.MapKey{Kind: types.KindRef, Bits: 2})
	require.False(t, ok)
}
func TestMap_Set(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32()))

	old, ok := m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})
	require.False(t, ok)
	require.Equal(t, types.MapEntry{}, old)

	old, ok = m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(3)})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), old.Value)

	entry, ok := m.Get(types.MapKey{Kind: types.KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(3), entry.Value)
}
func TestMap_Delete(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32()))
	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})

	old, ok := m.Delete(types.MapKey{Kind: types.KindI32, Bits: 1})
	require.True(t, ok)
	require.Equal(t, types.BoxI32(2), old.Value)
	require.Equal(t, 0, m.Len())

	_, ok = m.Delete(types.MapKey{Kind: types.KindI32, Bits: 1})
	require.False(t, ok)
}
func TestMap_Clear(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32()))
	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})

	var entries []types.MapEntry
	m.Clear(func(entry types.MapEntry) {
		entries = append(entries, entry)
	})
	require.Equal(t, []types.MapEntry{{Key: types.BoxI32(1), Value: types.BoxI32(2)}}, entries)
	require.Equal(t, 0, m.Len())
}
func TestMap_Len(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32()))
	require.Equal(t, 0, m.Len())

	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})
	require.Equal(t, 1, m.Len())
}
func TestMap_Range(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32()))
	m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})

	var keys []types.MapKey
	m.Range(func(key types.MapKey, _ types.MapEntry) {
		keys = append(keys, key)
	})
	require.Equal(t, []types.MapKey{{Kind: types.KindI32, Bits: 1}}, keys)
}

func TestMapKey_Value(t *testing.T) {
	tests := []struct {
		key   types.MapKey
		entry types.MapEntry
		want  types.Value
	}{
		{types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{}, types.I32(1)},
		{types.MapKey{Kind: types.KindI64, Bits: 1}, types.MapEntry{}, types.I64(1)},
		{types.MapKey{Kind: types.KindF32, Bits: uint64(math.Float32bits(1))}, types.MapEntry{}, types.F32(1)},
		{types.MapKey{Kind: types.KindF64, Bits: math.Float64bits(1)}, types.MapEntry{}, types.F64(1)},
		{types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{}, types.Ref(1)},
		{types.MapKey{Kind: types.KindText, Text: "a"}, types.MapEntry{}, types.String("a")},
		{types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(2)}, types.BoxI32(2)},
	}
	for _, tt := range tests {
		t.Run(tt.key.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.key.Value(tt.entry))
		})
	}
}

func TestMapIterator_Next(t *testing.T) {
	t.Run("exhausted iterator after clear", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		require.False(t, it.Next())
		m.Clear(func(types.Boxed) {})
		require.Zero(t, m.Len())
		require.False(t, it.Next())
		require.True(t, it.Done())
		require.Equal(t, types.BoxedNull, it.Current())
		require.Equal(t, []types.Ref{7}, it.Refs(nil))
		require.False(t, it.Next())
	})

	t.Run("typed key", func(t *testing.T) {
		m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		m.Set(1<<50, types.BoxI32(2))
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		require.Equal(t, types.I64(1<<50), it.Current())
		require.False(t, it.Next())
	})

	t.Run("generic ref key", func(t *testing.T) {
		m := types.NewMap(types.NewMapType(types.TypeString(), types.TypeI32()))
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 9}, types.MapEntry{Key: types.BoxRef(9), Value: types.BoxI32(2)})
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		require.Equal(t, types.BoxRef(9), it.Current())
	})

	t.Run("clear ends the walk", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		for range 10 {
			m.Set(math.NaN(), 1)
		}
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		m.Clear(func(types.Boxed) {})
		m.Set(100, 1)
		require.False(t, it.Next())
		require.True(t, it.Done())
		require.Equal(t, []types.Ref{7}, it.Refs(nil))
	})

	t.Run("float keys survive a rehash mid-walk", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		for range 5 {
			m.Set(math.NaN(), 1)
		}
		for _, key := range []float64{0, 1.5, -2.5, math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64} {
			m.Set(key, 1)
		}
		it := types.NewMapIterator(1, m)
		produced := map[float64]int{}
		nans := 0
		record := func() {
			key := float64(it.Current().(types.F64))
			if math.IsNaN(key) {
				nans++
				return
			}
			produced[key]++
		}
		require.True(t, it.Next())
		record()
		for i := range 1000 {
			m.Set(float64(i)+10, 1)
		}
		deleted := math.NaN()
		for _, key := range []float64{1.5, -2.5, math.Inf(1)} {
			if produced[key] == 0 {
				m.Delete(key)
				deleted = key
				break
			}
		}
		for it.Next() {
			record()
		}
		require.Equal(t, 5, nans)
		for key, count := range produced {
			require.Equal(t, 1, count, "key %v", key)
			require.NotEqual(t, deleted, key)
		}
		for _, key := range []float64{0, 1.5, -2.5, math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64} {
			if key != deleted {
				require.Equal(t, 1, produced[key], "key %v", key)
			}
		}
	})

	t.Run("clear ends a string map walk", func(t *testing.T) {
		m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		for _, key := range []string{"a", "b", "c", "d", "e", "f"} {
			m.Set(key, 1)
		}
		it := types.NewMapIterator(1, m)
		require.True(t, it.Next())
		m.Clear(func(types.Boxed) {})
		require.False(t, it.Next())
		require.True(t, it.Done())
	})

	t.Run("clear ends a generic map walk", func(t *testing.T) {
		m := types.NewMap(types.NewMapType(types.TypeAny(), types.TypeI32()))
		for ref := 1; ref <= 6; ref++ {
			m.Set(types.MapKey{Kind: types.KindRef, Bits: uint64(ref)}, types.MapEntry{Key: types.BoxRef(ref), Value: types.BoxI32(1)})
		}
		it := types.NewMapIterator(1, m)
		require.True(t, it.Next())
		m.Clear(func(types.MapEntry) {})
		require.False(t, it.Next())
		require.True(t, it.Done())
	})

	t.Run("every key kind visits each entry once", func(t *testing.T) {
		drain := func(m types.Value) []string {
			var out []string
			for it := types.NewMapIterator(1, m); it.Next(); {
				out = append(out, it.Current().String())
			}
			sort.Strings(out)
			return out
		}
		i1 := types.NewTypedMap[bool](types.NewMapType(types.TypeI1(), types.TypeI32()), 0)
		i1.Set(false, 0)
		i1.Set(true, 0)
		i8 := types.NewTypedMap[int8](types.NewMapType(types.TypeI8(), types.TypeI32()), 0)
		i8.Set(-128, 0)
		i8.Set(127, 0)
		i32 := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		i32.Set(math.MinInt32, 0)
		i32.Set(7, 0)
		i64 := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		i64.Set(math.MinInt64, 0)
		i64.Set(1<<50, 0)
		f32 := types.NewTypedMap[float32](types.NewMapType(types.TypeF32(), types.TypeI32()), 0)
		f32.Set(float32(math.Inf(-1)), 0)
		f32.Set(1.5, 0)
		f64 := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		f64.Set(math.NaN(), 0)
		f64.Set(math.NaN(), 0)
		f64.Set(-2.5, 0)
		str := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		str.Set("a", 0)
		str.Set("b", 0)
		require.Equal(t, []string{"false", "true"}, drain(i1))
		require.Equal(t, []string{"-128", "127"}, drain(i8))
		require.Equal(t, []string{"-2147483648", "7"}, drain(i32))
		require.Equal(t, []string{"-9223372036854775808", "1125899906842624"}, drain(i64))
		require.Len(t, drain(f32), 2)
		require.Len(t, drain(f64), 3)
		require.Len(t, drain(str), 2)
	})

	t.Run("mutation during iteration", func(t *testing.T) {
		const present = 200
		tests := []struct {
			name   string
			mutate func(m *types.TypedMap[int32], rng *rand.Rand, del func(int32))
		}{
			{"set and delete", func(m *types.TypedMap[int32], rng *rand.Rand, del func(int32)) {
				key := int32(rng.IntN(2 * present))
				if rng.IntN(2) == 0 {
					m.Set(key, 1)
				} else {
					del(key)
				}
			}},
			{"growth every step", func(m *types.TypedMap[int32], rng *rand.Rand, del func(int32)) {
				for range 64 {
					m.Set(int32(present+rng.IntN(1<<20)), 1)
				}
				del(int32(rng.IntN(present)))
			}},
			{"delete churn", func(m *types.TypedMap[int32], rng *rand.Rand, del func(int32)) {
				for range 8 {
					key := int32(present + rng.IntN(64))
					m.Set(key, 1)
					del(key)
				}
				del(int32(rng.IntN(present)))
			}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				for seed := uint64(1); seed <= 20; seed++ {
					rng := rand.New(rand.NewPCG(seed, 7))
					m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
					for k := range int32(present) {
						m.Set(k, 1)
					}
					deleted := map[int32]bool{}
					del := func(key int32) {
						if _, ok := m.Delete(key); ok {
							deleted[key] = true
						}
					}
					visited := map[int32]bool{}
					it := types.NewMapIterator(1, m)
					for it.Next() {
						key := int32(it.Current().(types.I32))
						_, live := m.Get(key)
						require.True(t, live, "produced a deleted key %d", key)
						require.False(t, visited[key] && !deleted[key], "key %d visited twice without a delete", key)
						visited[key] = true
						tt.mutate(m, rng, del)
					}
					for key := range int32(present) {
						require.True(t, visited[key] || deleted[key], "present-throughout key %d skipped", key)
					}
				}
			})
		}
	})
}

func TestMapIterator_Current(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
	m.Set(3, types.BoxI32(4))
	it := types.NewMapIterator(1, m)
	require.Equal(t, types.BoxedNull, it.Current())
	require.True(t, it.Next())
	require.Equal(t, types.I32(3), it.Current())
}

func TestMapIterator_Done(t *testing.T) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
	m.Set(3, types.BoxI32(4))
	it := types.NewMapIterator(1, m)
	require.True(t, it.Done())
	require.True(t, it.Next())
	require.False(t, it.Done())
	require.False(t, it.Next())
	require.True(t, it.Done())
}

func TestTypedMap_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0).Kind())
}

func TestTypedMap_Type(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeI32())
	require.Equal(t, typ, types.NewTypedMap[int32](typ, 0).Type())
}

func TestTypedMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI64()), 0)
		m.Set(1, types.BoxI64(2))

		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs(nil)
		})
		require.Empty(t, refs)
		require.Equal(t, []types.Ref{9}, m.Refs([]types.Ref{9}))
		require.Zero(t, allocs)
	})

	t.Run("ref value", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeString()), 0)
		m.Set(1, types.BoxRef(2))
		require.Equal(t, []types.Ref{9, 2}, m.Refs([]types.Ref{9}))
	})
}

func TestTypedMap_Clone(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32(), types.TypeAny())
		src := types.NewTypedMap[int32](typ, 2)
		src.Zero = types.BoxRef(7)
		src.Set(1, types.BoxRef(1))
		c := src.Clone()

		require.Same(t, typ, c.Typ)
		require.Equal(t, src.Zero, c.Zero)
		v, ok := c.Get(1)
		require.True(t, ok)
		require.Equal(t, types.BoxRef(1), v)

		c.Set(1, types.BoxRef(9))
		c.Set(2, types.BoxRef(2))
		v, _ = src.Get(1)
		require.Equal(t, types.BoxRef(1), v)
		require.Equal(t, 1, src.Len())
	})
}

func TestTypedMap_String(t *testing.T) {
	t.Run("i32", func(t *testing.T) {
		m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("i64", func(t *testing.T) {
		m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[i64]i32{1: 2}", m.String())
	})

	t.Run("f32", func(t *testing.T) {
		m := types.NewTypedMap[float32](types.NewMapType(types.TypeF32(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[f32]i32{1: 2}", m.String())
	})

	t.Run("f64", func(t *testing.T) {
		m := types.NewTypedMap[float64](types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		m.Set(1, types.BoxI32(2))
		require.Equal(t, "map[f64]i32{1: 2}", m.String())
	})
}

func TestStringMap_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0).Kind())
}

func TestStringMap_Type(t *testing.T) {
	typ := types.NewMapType(types.TypeString(), types.TypeI32())
	require.Same(t, typ, types.NewStringMap(typ, 0).Type())
}

func TestStringMap_Refs(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeString()), 0)
	m.Set("a", types.BoxRef(2))
	require.Equal(t, []types.Ref{9, 2}, m.Refs([]types.Ref{9}))
}

func TestStringMap_Clone(t *testing.T) {
	src := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeAny()), 2)
	src.Set("a", types.BoxRef(1))
	c := src.Clone()
	c.Delete("a")
	_, ok := src.Get("a")
	require.True(t, ok)
}

func TestStringMap_String(t *testing.T) {
	m := types.NewStringMap(types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	m.Set("foo", types.BoxI32(2))
	require.Equal(t, "map[string]i32{\"foo\": 2}", m.String())
}

func TestMap_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32())).Kind())
}
func TestMap_Type(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeI32())
	require.Equal(t, typ, types.NewMap(typ).Type())
}
func TestMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI64()))
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI64(2)})

		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs(nil)
		})
		require.Empty(t, refs)
		require.Equal(t, []types.Ref{9}, m.Refs([]types.Ref{9}))
		require.Zero(t, allocs)
	})

	t.Run("ref key and value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeAny(), types.TypeAny())
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{
			Key:   types.BoxRef(1),
			Value: types.BoxRef(2),
		})
		require.Equal(t, []types.Ref{9, 1, 2}, m.Refs([]types.Ref{9}))
	})

	t.Run("spilled i64 value", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32(), types.TypeI64())
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{
			Key:   types.BoxI32(1),
			Value: types.BoxRef(2),
		})
		require.Equal(t, []types.Ref{9, 2}, m.Refs([]types.Ref{9}))
	})
}

func TestMap_Clone(t *testing.T) {
	typ := types.NewMapType(types.TypeAny(), types.TypeAny())
	src := types.NewMap(typ)
	src.Zero = types.BoxRef(7)
	key := types.MapKey{Bits: 1}
	src.Set(key, types.MapEntry{Key: types.BoxRef(1), Value: types.BoxRef(2)})
	c := src.Clone()

	require.Same(t, typ, c.Typ)
	require.Equal(t, src.Zero, c.Zero)
	e, ok := c.Get(key)
	require.True(t, ok)
	require.Equal(t, types.BoxRef(2), e.Value)

	c.Set(key, types.MapEntry{Key: types.BoxRef(1), Value: types.BoxRef(9)})
	c.Delete(types.MapKey{Bits: 5})
	e, _ = src.Get(key)
	require.Equal(t, types.BoxRef(2), e.Value)
}

func TestMap_String(t *testing.T) {
	t.Run("i32 key", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32(), types.TypeI32())
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{
			Key:   types.BoxI32(1),
			Value: types.BoxI32(2),
		})
		require.Equal(t, "map[i32]i32{1: 2}", m.String())
	})

	t.Run("empty string key", func(t *testing.T) {
		typ := types.NewMapType(types.TypeString(), types.TypeI32())
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{
			Key:   types.BoxRef(1),
			Value: types.BoxI32(2),
		})
		require.Equal(t, "map[string]i32{1: 2}", m.String())
	})

	t.Run("deterministic", func(t *testing.T) {
		typ := types.NewMapType(types.TypeI32(), types.TypeI32())
		m := types.NewMap(typ)
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 2}, types.MapEntry{Key: types.BoxI32(2), Value: types.BoxI32(20)})
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(10)})
		require.Equal(t, "map[i32]i32{1: 10, 2: 20}", m.String())
	})
}
func TestMapLiteral_Kind(t *testing.T) {
	value := &types.MapLiteral{Typ: types.NewMapType(types.TypeI32(), types.TypeString())}
	require.Equal(t, types.KindRef, value.Kind())
}
func TestMapLiteral_Type(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeString())
	require.Same(t, typ, (&types.MapLiteral{Typ: typ}).Type())
}

func TestMapLiteral_String(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeString())
	value := &types.MapLiteral{Typ: typ, Keys: []types.Value{types.I32(2), types.I32(1)}, Values: []types.Value{types.String("b"), types.String("a")}}
	require.Equal(t, `map[i32]string{1: "a", 2: "b"}`, value.String())
}

func TestMapKey_String(t *testing.T) {
	tests := []struct {
		key types.MapKey
		str string
	}{
		{types.MapKey{Kind: types.KindI32, Bits: 1}, "1"},
		{types.MapKey{Kind: types.KindI64, Bits: 1}, "1"},
		{types.MapKey{Kind: types.KindF32, Bits: uint64(math.Float32bits(1))}, "1"},
		{types.MapKey{Kind: types.KindF64, Bits: math.Float64bits(1)}, "1"},
		{types.MapKey{Kind: types.KindRef, Bits: 1}, "1"},
		{types.MapKey{Kind: types.KindText, Text: "a"}, "\"a\""},
		{types.MapKey{Kind: types.Kind(255)}, "<invalid>"},
	}
	for _, tt := range tests {
		t.Run(tt.key.Kind.String(), func(t *testing.T) {
			require.Equal(t, tt.str, tt.key.String())
		})
	}
}
func TestMapType_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewMapType(types.TypeI32(), types.TypeI32()).Kind())
}
func TestMapType_Cast(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeI32())

	require.True(t, typ.Cast(typ))
	require.True(t, typ.Cast(types.NewMapType(types.TypeI32(), types.TypeI32())))
	require.False(t, typ.Cast(types.NewMapType(types.TypeI64(), types.TypeI32())))
	require.False(t, typ.Cast(types.TypeI32()))
}
func TestMapType_Equals(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeI32())

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(types.NewMapType(types.TypeI32(), types.TypeI32())))
	require.False(t, typ.Equals(types.NewMapType(types.TypeI32(), types.TypeI64())))
	require.False(t, typ.Equals(types.TypeI32()))
}

func TestMapType_String(t *testing.T) {
	require.Equal(t, "map[i32]string", types.NewMapType(types.TypeI32(), types.TypeString()).String())
}
func TestMapIterator_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewMapIterator(1, types.NewMap(types.NewMapType(types.TypeAny(), types.TypeI32()))).Kind())
}
func TestMapIterator_Type(t *testing.T) {
	m := types.NewTypedMap[int64](types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
	require.Equal(t, types.NewIteratorType(types.TypeI64()), types.NewMapIterator(1, m).Type())
}
func TestMapIterator_Refs(t *testing.T) {
	m := types.NewMap(types.NewMapType(types.TypeString(), types.TypeI32()))
	m.Set(types.MapKey{Kind: types.KindRef, Bits: 9}, types.MapEntry{Key: types.BoxRef(9), Value: types.BoxI32(2)})
	it := types.NewMapIterator(7, m)
	require.True(t, it.Next())
	require.Equal(t, []types.Ref{5, 7, 9}, it.Refs([]types.Ref{5}))
}

func TestMapIterator_String(t *testing.T) {
	require.Equal(t, "map.iterator", types.NewMapIterator(1, types.NewMap(types.NewMapType(types.TypeAny(), types.TypeI32()))).String())
}
func BenchmarkTypedMap_Refs(b *testing.B) {
	m := types.NewTypedMap[int32](types.NewMapType(types.TypeI32(), types.TypeI64()), 0)
	m.Set(1, types.BoxI64(2))
	require.Empty(b, m.Refs(nil))

	var refs []types.Ref
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		refs = m.Refs(nil)
	}
	b.StopTimer()
	require.Empty(b, refs)
}

func BenchmarkMap_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		m := types.NewMap(types.NewMapType(types.TypeI32(), types.TypeI32()))
		m.Set(types.MapKey{Kind: types.KindI32, Bits: 1}, types.MapEntry{Key: types.BoxI32(1), Value: types.BoxI32(2)})
		require.Empty(b, m.Refs(nil))

		var refs []types.Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = m.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		m := types.NewMap(types.NewMapType(types.TypeAny(), types.TypeAny()))
		m.Set(types.MapKey{Kind: types.KindRef, Bits: 1}, types.MapEntry{Key: types.BoxRef(1), Value: types.BoxRef(2)})
		require.Equal(b, []types.Ref{1, 2}, m.Refs(nil))

		refs := make([]types.Ref, 0, 2)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = m.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []types.Ref{1, 2}, refs)
	})
}
