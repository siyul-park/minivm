package types_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// newHeap returns a heap whose address 0 is the null slot.
func newHeap() *types.Heap {
	h := types.NewHeap(64, 0, types.Hooks{})
	h.Add(types.Null)
	return h
}

func text(h *types.Heap, s string) types.Boxed {
	return types.BoxRef(h.Add(types.String(s)))
}

func cell(h *types.Heap, v int64) types.Boxed {
	return types.BoxRef(h.Add(types.I64(v)))
}

// describe renders a key by what it is, so a model compares it across
// representations.
func describe(h *types.Heap, key types.Boxed) string {
	switch key.Kind() {
	case types.KindRef:
		switch v := h.At(key.Ref()).(type) {
		case types.String:
			return "s:" + string(v)
		case types.I64:
			return fmt.Sprintf("%d:%d", types.KindI64, int64(v))
		}
		return fmt.Sprintf("ref:%d", key.Ref())
	case types.KindF32:
		if f := key.F32(); f != f {
			return "nan"
		}
	case types.KindF64:
		if f := key.F64(); f != f {
			return "nan"
		}
	}
	return fmt.Sprintf("%d:%s", key.Kind(), key.String())
}

// ctrlLen returns the slots of m's table.
func ctrlLen(m *types.Map) int {
	return reflect.ValueOf(m).Elem().FieldByName("table").FieldByName("ctrl").Len()
}

// TestMap owns the whole-map contract: every key kind behaves exactly as the
// insertion-ordered model, and every reference it takes it gives back.
func TestMap(t *testing.T) {
	type item struct {
		id   string
		disp string
		nan  bool
		key  func() types.Boxed
	}
	scalar := func(h *types.Heap, key, canon types.Boxed) item {
		id := describe(h, canon)
		return item{id: id, disp: id, key: func() types.Boxed { return key }}
	}
	nan := func(key types.Boxed) item {
		return item{id: "nan", disp: "nan", nan: true, key: func() types.Boxed { return key }}
	}
	wide := func(h *types.Heap, v int64) item {
		id := fmt.Sprintf("%d:%d", types.KindI64, v)
		return item{id: id, disp: id, key: func() types.Boxed { return cell(h, v) }}
	}
	str := func(h *types.Heap, s string) item {
		return item{id: "s:" + s, disp: "s:" + s, key: func() types.Boxed { return text(h, s) }}
	}
	i64s := []int64{0, 1, -1, 5, -5, 1<<48 - 1, -(1<<48 - 1), 1 << 48, -(1 << 48), 1 << 50, -(1 << 50), math.MaxInt64, math.MinInt64}
	f64s := []float64{0, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64, math.MaxFloat64, 1, -1, 1.5, 2.5}
	strs := func(h *types.Heap) []item {
		var out []item
		for _, n := range []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 33, 100} {
			for _, c := range "ab" {
				out = append(out, str(h, strings.Repeat(string(c), n)))
			}
		}
		return append(out, str(h, "hello world, hello map"), str(h, "hello world, hello mam"))
	}
	words := func(h *types.Heap, kind types.Kind) []item {
		var out []item
		switch kind {
		case types.KindI1:
			out = append(out, scalar(h, types.BoxI1(false), types.BoxI1(false)), scalar(h, types.BoxI1(true), types.BoxI1(true)))
		case types.KindI8:
			for v := math.MinInt8; v <= math.MaxInt8; v++ {
				out = append(out, scalar(h, types.BoxI8(int8(v)), types.BoxI8(int8(v))))
			}
		case types.KindI32:
			for _, v := range []int32{0, 1, -1, math.MinInt32, math.MaxInt32, 1 << 24, -(1 << 24)} {
				out = append(out, scalar(h, types.BoxI32(v), types.BoxI32(v)))
			}
			for i := range int32(600) {
				out = append(out, scalar(h, types.BoxI32(i*7919-2000), types.BoxI32(i*7919-2000)))
			}
		default:
			for _, v := range i64s {
				if types.IsBoxable(v) {
					out = append(out, scalar(h, types.BoxI64(v), types.BoxI64(v)))
				} else {
					out = append(out, wide(h, v))
				}
			}
			out = append(out, item{id: "2:5", disp: "2:5", key: func() types.Boxed { return cell(h, 5) }})
		}
		return out
	}
	floats := func(h *types.Heap, kind types.Kind) []item {
		var out []item
		for _, v := range f64s {
			if kind == types.KindF32 {
				f := float32(v)
				canon := f
				if f == 0 {
					canon = 0
				}
				out = append(out, scalar(h, types.BoxF32(f), types.BoxF32(canon)))
				continue
			}
			canon := v
			if v == 0 {
				canon = 0
			}
			out = append(out, scalar(h, types.BoxF64(v), types.BoxF64(canon)))
		}
		if kind == types.KindF32 {
			return append(out, nan(types.BoxF32(float32(math.NaN()))))
		}
		return append(out, nan(types.BoxF64(math.NaN())))
	}
	anys := func(h *types.Heap) ([]item, []int) {
		typ := types.NewArrayType(types.TypeI32())
		var arrays []int
		out := []item{
			scalar(h, types.BoxI8(5), types.BoxI32(5)), scalar(h, types.BoxI32(5), types.BoxI32(5)),
			scalar(h, types.BoxI1(true), types.BoxI32(1)), scalar(h, types.BoxI8(-1), types.BoxI32(-1)),
			scalar(h, types.BoxI64(5), types.BoxI64(5)), scalar(h, types.BoxI64(-7), types.BoxI64(-7)),
			wide(h, 1<<50), wide(h, -(1 << 50)),
			{id: "2:5", disp: "2:5", key: func() types.Boxed { return cell(h, 5) }},
			scalar(h, types.BoxF32(1.5), types.BoxF32(1.5)), scalar(h, types.BoxF64(1.5), types.BoxF64(1.5)),
			scalar(h, types.BoxF64(math.Copysign(0, -1)), types.BoxF64(0)), scalar(h, types.BoxF32(float32(math.Copysign(0, -1))), types.BoxF32(0)),
			nan(types.BoxF64(math.NaN())), nan(types.BoxF32(float32(math.NaN()))),
			scalar(h, types.BoxedNull, types.BoxedNull),
		}
		out[len(out)-1].id, out[len(out)-1].disp = "ref:0", "ref:0"
		out = append(out, strs(h)...)
		for range 3 {
			addr := h.Add(types.NewArray(nil, typ, 0))
			arrays = append(arrays, addr)
			id := fmt.Sprintf("ref:%d", addr)
			out = append(out, item{id: id, disp: id, key: func() types.Boxed { h.Inc(addr, 1); return types.BoxRef(addr) }})
		}
		return out, arrays
	}

	tests := []struct {
		name  string
		typ   *types.MapType
		items func(h *types.Heap) ([]item, []int)
		ops   int
	}{
		{"i1", types.NewMapType(types.TypeI1(), types.TypeI32()), func(h *types.Heap) ([]item, []int) { return words(h, types.KindI1), nil }, 400},
		{"i8", types.NewMapType(types.TypeI8(), types.TypeI32()), func(h *types.Heap) ([]item, []int) { return words(h, types.KindI8), nil }, 4000},
		{"i32", types.NewMapType(types.TypeI32(), types.TypeI32()), func(h *types.Heap) ([]item, []int) { return words(h, types.KindI32), nil }, 20000},
		{"i64", types.NewMapType(types.TypeI64(), types.TypeI32()), func(h *types.Heap) ([]item, []int) { return words(h, types.KindI64), nil }, 6000},
		{"f32", types.NewMapType(types.TypeF32(), types.TypeI32()), func(h *types.Heap) ([]item, []int) { return floats(h, types.KindF32), nil }, 4000},
		{"f64", types.NewMapType(types.TypeF64(), types.TypeI32()), func(h *types.Heap) ([]item, []int) { return floats(h, types.KindF64), nil }, 4000},
		{"string", types.NewMapType(types.TypeString(), types.TypeI32()), func(h *types.Heap) ([]item, []int) { return strs(h), nil }, 8000},
		{"any", types.NewMapType(types.TypeAny(), types.TypeI32()), anys, 12000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for seed := uint64(1); seed <= 5; seed++ {
				h := newHeap()
				items, arrays := tt.items(h)
				base := h.Live()
				rng := rand.New(rand.NewPCG(seed, 1))
				m := types.NewMap(h, tt.typ, rng.IntN(4)*8)

				type entry struct {
					disp  string
					value types.Boxed
					live  bool
				}
				var log []entry
				index := map[string]int{}
				live := 0
				lines := func() []string {
					var out []string
					for _, e := range log {
						if e.live {
							out = append(out, fmt.Sprintf("%s/%d", e.disp, uint64(e.value)))
						}
					}
					return out
				}
				ranged := func(m *types.Map) []string {
					var out []string
					m.Range(func(key, value types.Boxed) bool {
						out = append(out, fmt.Sprintf("%s/%d", describe(h, key), uint64(value)))
						return true
					})
					return out
				}
				for n := range tt.ops {
					it := items[rng.IntN(len(items))]
					value := types.BoxI32(int32(rng.IntN(1 << 20)))
					i, found := index[it.id]
					if it.nan {
						found = false
					}
					switch op := rng.IntN(100); {
					case op < 45:
						key := it.key()
						require.True(t, m.Accepts(key), "accepts %s", it.id)
						if op < 35 {
							m.Store(key, value)
						} else {
							var want types.Boxed
							if found {
								want = log[i].value
							}
							got, loaded := m.Swap(key, value)
							require.Equal(t, found, loaded, "swap %s", it.id)
							require.Equal(t, want, got, "swap %s", it.id)
						}
						if found {
							log[i].value = value
						} else {
							log = append(log, entry{it.disp, value, true})
							live++
							if !it.nan {
								index[it.id] = len(log) - 1
							}
						}
					case op < 60:
						key := it.key()
						var want types.Boxed
						if found {
							want = log[i].value
						}
						got, ok := m.Load(key)
						require.Equal(t, found, ok, "load %s", it.id)
						require.Equal(t, want, got, "load %s", it.id)
						h.Release(key)
					case op < 75:
						key := it.key()
						var want types.Boxed
						if found {
							want = log[i].value
							log[i].live = false
							live--
							delete(index, it.id)
						}
						got, ok := m.LoadAndDelete(key)
						require.Equal(t, found, ok, "load and delete %s", it.id)
						require.Equal(t, want, got, "load and delete %s", it.id)
						h.Release(key)
					case op < 90:
						key := it.key()
						if found {
							log[i].live = false
							live--
							delete(index, it.id)
						}
						m.Delete(key)
						h.Release(key)
					case op < 92:
						log, index, live = nil, map[string]int{}, 0
						m.Clear()
					case op < 96:
						require.Equal(t, lines(), ranged(m), "range")
					default:
						c := m.Clone()
						require.Equal(t, lines(), ranged(c), "clone")
						key := it.key()
						c.Store(key, value)
						c.Delete(key)
						c.Clear()
						require.Equal(t, lines(), ranged(m), "clone is independent")
					}
					require.Equal(t, live, m.Len(), "len after op %d", n)
				}
				require.Equal(t, lines(), ranged(m))
				m.Clear()
				require.Equal(t, base, h.Live(), "every reference the map took is given back")
				for _, addr := range arrays {
					require.Equal(t, 1, h.Count(addr))
				}
			}
		})
	}
}

func TestMapLiteral(t *testing.T) {
	value := &types.MapLiteral{Typ: types.NewMapType(types.TypeI32(), types.TypeString()), Keys: []types.Value{types.I32(1)}, Values: []types.Value{types.String("a")}}
	require.Len(t, value.Keys, 1)
	require.Len(t, value.Values, 1)
}

func TestNewMap(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeI32())

	t.Run("bound", func(t *testing.T) {
		m := types.NewMap(newHeap(), typ, 4)
		require.Same(t, typ, m.Typ)
		require.Equal(t, types.BoxI32(0), m.Zero)
		require.Zero(t, m.Len())
	})

	t.Run("unbound", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeAny()), 0)
		require.Equal(t, types.BoxedNull, m.Zero)
		m.Store(types.BoxI32(1), types.BoxI32(2))
		v, ok := m.Load(types.BoxI32(1))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), v)
	})

	t.Run("capacity takes inserts without a rehash", func(t *testing.T) {
		m := types.NewMap(nil, typ, 100)
		slots := ctrlLen(m)
		for k := range int32(100) {
			m.Store(types.BoxI32(k), 0)
		}
		require.Equal(t, slots, ctrlLen(m))
	})
}

func TestNewMapType(t *testing.T) {
	structType := types.NewStructType(types.NewStructField(types.TypeI32()))
	tests := []struct {
		key        types.Type
		traceKeys  bool
		keyKind    types.Kind
		keyKindStr string
	}{
		{types.TypeI1(), false, types.KindI1, "i1"},
		{types.TypeI8(), false, types.KindI8, "i8"},
		{types.TypeI32(), false, types.KindI32, "i32"},
		{types.TypeI64(), true, types.KindI64, "i64"},
		{types.TypeF32(), false, types.KindF32, "f32"},
		{types.TypeF64(), false, types.KindF64, "f64"},
		{types.TypeString(), true, types.KindRef, "string"},
		{types.TypeAny(), true, types.KindAny, "any"},
		{structType, true, types.KindRef, "struct"},
	}
	for _, tt := range tests {
		t.Run(tt.keyKindStr, func(t *testing.T) {
			typ := types.NewMapType(tt.key, types.TypeI32())
			require.Equal(t, tt.key, typ.Key)
			require.Equal(t, types.TypeI32(), typ.Elem)
			require.Equal(t, tt.keyKind, typ.KeyKind)
			require.Equal(t, types.KindI32, typ.ElemKind)
			require.Equal(t, tt.traceKeys, typ.TraceKeys)
			require.False(t, typ.TraceValues)
		})
	}

	t.Run("values", func(t *testing.T) {
		require.True(t, types.NewMapType(types.TypeI32(), types.TypeString()).TraceValues)
		require.True(t, types.NewMapType(types.TypeI32(), types.TypeI64()).TraceValues)
		require.False(t, types.NewMapType(types.TypeI32(), types.TypeF64()).TraceValues)
	})

	t.Run("kinds are consecutive bytes", func(t *testing.T) {
		typ := reflect.TypeOf(types.MapType{})
		kinds, _ := typ.FieldByName("KeyKind")
		elem, _ := typ.FieldByName("ElemKind")
		require.Equal(t, kinds.Offset+1, elem.Offset)
	})
}

func TestNewMapIterator(t *testing.T) {
	m := types.NewMap(nil, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
	it := types.NewMapIterator(7, m)
	require.Equal(t, types.NewIteratorType(types.TypeI64()), it.Type())
	require.True(t, it.Done())
}

func TestMap_Accepts(t *testing.T) {
	h := newHeap()
	str, wide := text(h, "a"), cell(h, 1<<50)
	array := types.BoxRef(h.Add(types.NewArray(nil, types.NewArrayType(types.TypeI32()), 0)))
	structKey := types.NewStructType(types.NewStructField(types.TypeI32()))
	tests := []struct {
		name string
		key  types.Type
		in   types.Boxed
		want bool
	}{
		{"i32 scalar", types.TypeI32(), types.BoxI32(1), true},
		{"i32 null", types.TypeI32(), types.BoxedNull, false},
		{"i32 string", types.TypeI32(), str, false},
		{"i32 cell", types.TypeI32(), wide, false},
		{"i64 scalar", types.TypeI64(), types.BoxI64(1), true},
		{"i64 cell", types.TypeI64(), wide, true},
		{"i64 string", types.TypeI64(), str, false},
		{"f64 scalar", types.TypeF64(), types.BoxF64(1), true},
		{"f64 cell", types.TypeF64(), wide, false},
		{"f64 array", types.TypeF64(), array, false},
		{"string string", types.TypeString(), str, true},
		{"string null", types.TypeString(), types.BoxedNull, false},
		{"string scalar", types.TypeString(), types.BoxI32(1), false},
		{"string array", types.TypeString(), array, false},
		{"string cell", types.TypeString(), wide, false},
		{"any scalar", types.TypeAny(), types.BoxI32(1), true},
		{"any null", types.TypeAny(), types.BoxedNull, true},
		{"any array", types.TypeAny(), array, true},
		{"any string", types.TypeAny(), str, true},
		{"struct scalar", structKey, types.BoxI32(1), false},
		{"struct null", structKey, types.BoxedNull, true},
		{"struct array", structKey, array, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := types.NewMap(h, types.NewMapType(tt.key, types.TypeI32()), 0)
			require.Equal(t, tt.want, m.Accepts(tt.in))
		})
	}

	t.Run("unbound reference", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		require.PanicsWithValue(t, types.ErrDetached, func() { m.Accepts(types.BoxRef(1)) })
		require.False(t, m.Accepts(types.BoxI32(1)))
	})
}

func TestMap_Load(t *testing.T) {
	t.Run("borrows the key", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		m.Store(text(h, "a"), types.BoxI32(1))
		probe := text(h, "a")
		v, ok := m.Load(probe)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(1), v)
		require.Equal(t, 1, h.Count(probe.Ref()))
	})

	t.Run("miss", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		v, ok := m.Load(types.BoxI32(1))
		require.False(t, ok)
		require.Zero(t, v)
	})

	t.Run("negative zero hits positive zero and NaN misses", func(t *testing.T) {
		for _, typ := range []types.Type{types.TypeF32(), types.TypeF64(), types.TypeAny()} {
			m := types.NewMap(newHeap(), types.NewMapType(typ, types.TypeI32()), 0)
			box := func(f float64) types.Boxed {
				if typ.Kind() == types.KindRef || typ.Equals(types.TypeF64()) {
					return types.BoxF64(f)
				}
				return types.BoxF32(float32(f))
			}
			m.Store(box(0), types.BoxI32(1))
			m.Store(box(math.NaN()), types.BoxI32(2))
			v, ok := m.Load(box(math.Copysign(0, -1)))
			require.True(t, ok, typ.String())
			require.Equal(t, types.BoxI32(1), v)
			_, ok = m.Load(box(math.NaN()))
			require.False(t, ok, typ.String())
		}
	})

	t.Run("text by content", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		for _, s := range []string{"", "a", "abcd", "abcdefgh", "abcdefghi", strings.Repeat("x", 40)} {
			m.Store(text(h, s), types.BoxI32(int32(len(s))))
		}
		for _, s := range []string{"", "a", "abcd", "abcdefgh", "abcdefghi", strings.Repeat("x", 40)} {
			v, ok := m.Load(text(h, s))
			require.True(t, ok, s)
			require.Equal(t, types.BoxI32(int32(len(s))), v)
		}
		_, ok := m.Load(text(h, "abcdefgi"))
		require.False(t, ok)
	})

	t.Run("string key of distinct refs hits by content in string and any maps", func(t *testing.T) {
		for _, typ := range []types.Type{types.TypeString(), types.TypeAny()} {
			h := newHeap()
			m := types.NewMap(h, types.NewMapType(typ, types.TypeI32()), 0)
			first, second := text(h, "key"), text(h, "key")
			require.NotEqual(t, first, second)
			m.Store(first, types.BoxI32(1))
			v, ok := m.Load(second)
			require.True(t, ok, typ.String())
			require.Equal(t, types.BoxI32(1), v)
			require.Equal(t, 1, m.Len())
		}
	})

	t.Run("i1 reads any nonzero payload as true", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI1(), types.TypeI32()), 0)
		m.Store(types.BoxI1(true), types.BoxI32(1))
		v, ok := m.Load(types.Box(2, types.KindI1))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(1), v)
	})

	t.Run("any retags i1 and i8 as i32", func(t *testing.T) {
		m := types.NewMap(newHeap(), types.NewMapType(types.TypeAny(), types.TypeI32()), 0)
		m.Store(types.BoxI8(5), types.BoxI32(1))
		m.Store(types.BoxI64(5), types.BoxI32(2))
		v, _ := m.Load(types.BoxI32(5))
		require.Equal(t, types.BoxI32(1), v)
		v, _ = m.Load(types.BoxI64(5))
		require.Equal(t, types.BoxI32(2), v)
		_, ok := m.Load(types.BoxF64(5))
		require.False(t, ok)
	})

	t.Run("any folds every nonzero i1 payload into the i32 key 1", func(t *testing.T) {
		m := types.NewMap(newHeap(), types.NewMapType(types.TypeAny(), types.TypeI32()), 0)
		m.Store(types.BoxI1(true), types.BoxI32(1))
		m.Store(types.BoxI32(0), types.BoxI32(2))
		for _, key := range []types.Boxed{types.Box(2, types.KindI1), types.BoxI1(true), types.BoxI32(1)} {
			v, ok := m.Load(key)
			require.True(t, ok, key.String())
			require.Equal(t, types.BoxI32(1), v)
		}
		for _, key := range []types.Boxed{types.Box(0, types.KindI1), types.BoxI1(false), types.BoxI32(0)} {
			v, ok := m.Load(key)
			require.True(t, ok, key.String())
			require.Equal(t, types.BoxI32(2), v)
		}
		m.Store(types.Box(2, types.KindI1), types.BoxI32(3))
		require.Equal(t, 2, m.Len())
	})

	t.Run("i64 cells", func(t *testing.T) {
		for _, typ := range []types.Type{types.TypeI64(), types.TypeAny()} {
			h := newHeap()
			m := types.NewMap(h, types.NewMapType(typ, types.TypeI32()), 0)
			m.Store(types.BoxI64(7), types.BoxI32(1))
			m.Store(cell(h, 1<<50), types.BoxI32(2))
			v, ok := m.Load(cell(h, 7))
			require.True(t, ok, typ.String())
			require.Equal(t, types.BoxI32(1), v)
			v, ok = m.Load(cell(h, 1<<50))
			require.True(t, ok, typ.String())
			require.Equal(t, types.BoxI32(2), v)
			_, ok = m.Load(cell(h, 1<<51))
			require.False(t, ok)
		}
	})

	t.Run("unbound reference", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		require.PanicsWithValue(t, types.ErrDetached, func() { m.Load(types.BoxRef(1)) })
	})
}

func TestMap_Store(t *testing.T) {
	t.Run("a miss takes the key and value", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeString()), 0)
		key, value := text(h, "k"), text(h, "v")
		m.Store(key, value)
		require.Equal(t, 1, h.Count(key.Ref()))
		require.Equal(t, 1, h.Count(value.Ref()))
	})

	t.Run("a hit releases the incoming key and the old value and keeps the first key", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeString()), 0)
		first, old := text(h, "k"), text(h, "v1")
		m.Store(first, old)
		m.Store(text(h, "j"), text(h, "w"))
		again, next := text(h, "k"), text(h, "v2")
		m.Store(again, next)

		require.False(t, h.Alive(again.Ref()))
		require.False(t, h.Alive(old.Ref()))
		require.Equal(t, 1, h.Count(next.Ref()))
		var keys []types.Boxed
		m.Range(func(key, _ types.Boxed) bool { keys = append(keys, key); return true })
		require.Equal(t, first, keys[0])
		require.Len(t, keys, 2)
	})

	t.Run("a sole-owned wide cell is adopted", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		key := cell(h, 1<<50)
		m.Store(key, types.BoxI32(1))
		m.Range(func(stored, _ types.Boxed) bool {
			require.Equal(t, key, stored)
			return true
		})
		require.Equal(t, 1, h.Count(key.Ref()))
	})

	t.Run("a shared wide cell is copied", func(t *testing.T) {
		for _, typ := range []types.Type{types.TypeI64(), types.TypeAny()} {
			h := newHeap()
			m := types.NewMap(h, types.NewMapType(typ, types.TypeI32()), 0)
			key := cell(h, 1<<50)
			h.Inc(key.Ref(), 1)
			m.Store(key, types.BoxI32(1))
			require.Equal(t, 1, h.Count(key.Ref()), typ.String())
			m.Range(func(stored, _ types.Boxed) bool {
				require.NotEqual(t, key, stored)
				require.Equal(t, types.I64(1<<50), h.At(stored.Ref()))
				require.Equal(t, 1, h.Count(stored.Ref()))
				return true
			})
		}
	})

	t.Run("a cell that fits becomes its inline key", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		key := cell(h, 5)
		m.Store(key, types.BoxI32(1))
		require.False(t, h.Alive(key.Ref()))
		m.Range(func(stored, _ types.Boxed) bool {
			require.Equal(t, types.BoxI64(5), stored)
			return true
		})
	})

	t.Run("a null old value leaves the null count", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeI32(), types.TypeAny()), 0)
		m.Store(types.BoxI32(1), types.BoxedNull)
		before := h.Count(0)
		m.Store(types.BoxI32(1), types.BoxedNull)
		require.Equal(t, before, h.Count(0))
	})

	t.Run("a key deleted and stored again takes a dead slot, not a new one", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 64)
		for k := range int32(8) {
			m.Store(types.BoxI32(k), types.BoxI32(1))
		}
		ctrl := reflect.ValueOf(m).Elem().FieldByName("table").FieldByName("ctrl")
		for range 32 {
			m.Delete(types.BoxI32(-5))
			m.Store(types.BoxI32(-5), types.BoxI32(1))
			taken := 0
			for i := range ctrl.Len() {
				taken += min(int(ctrl.Index(i).Uint()), 1)
			}
			require.LessOrEqual(t, taken, 9)
		}
	})

	t.Run("tombstones alone never grow the table", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		slots := ctrlLen(m)
		for i := range int32(50000) {
			m.Store(types.BoxI32(i), types.BoxI32(i))
			if i >= 3 {
				m.Delete(types.BoxI32(i - 3))
			}
		}
		require.Equal(t, 3, m.Len())
		require.Equal(t, slots, ctrlLen(m))
	})

	t.Run("unbound map with a reference old value", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeAny()), 0)
		m.Store(types.BoxI32(1), types.BoxRef(3))
		require.PanicsWithValue(t, types.ErrDetached, func() { m.Store(types.BoxI32(1), types.BoxI32(2)) })
	})
}

func TestMap_Swap(t *testing.T) {
	t.Run("a hit returns the previous value unreleased and releases the incoming key", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeString()), 0)
		m.Store(text(h, "k"), text(h, "v1"))
		again, old := text(h, "k"), text(h, "v2")
		m.Store(text(h, "j"), old)
		prev, loaded := m.Swap(again, text(h, "v3"))
		require.True(t, loaded)
		require.Equal(t, "v1", string(h.At(prev.Ref()).(types.String)))
		require.Equal(t, 1, h.Count(prev.Ref()))
		require.False(t, h.Alive(again.Ref()))
	})

	t.Run("a miss returns nothing", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		prev, loaded := m.Swap(types.BoxI32(1), types.BoxI32(2))
		require.False(t, loaded)
		require.Zero(t, prev)
		require.Equal(t, 1, m.Len())
	})
}

func TestMap_LoadAndDelete(t *testing.T) {
	t.Run("moves the value and releases the stored key", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeString()), 0)
		key, value := text(h, "k"), text(h, "v")
		m.Store(key, value)
		probe := text(h, "k")
		got, ok := m.LoadAndDelete(probe)
		require.True(t, ok)
		require.Equal(t, value, got)
		require.Equal(t, 1, h.Count(value.Ref()))
		require.False(t, h.Alive(key.Ref()))
		require.Equal(t, 1, h.Count(probe.Ref()))
		require.Zero(t, m.Len())
	})

	t.Run("a miss changes nothing", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Store(types.BoxI32(1), types.BoxI32(2))
		got, ok := m.LoadAndDelete(types.BoxI32(9))
		require.False(t, ok)
		require.Zero(t, got)
		require.Equal(t, 1, m.Len())
	})
}

func TestMap_Delete(t *testing.T) {
	t.Run("releases the stored key and value", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeString()), 0)
		key, value := text(h, "k"), text(h, "v")
		m.Store(key, value)
		probe := text(h, "k")
		m.Delete(probe)
		require.False(t, h.Alive(key.Ref()))
		require.False(t, h.Alive(value.Ref()))
		require.Equal(t, 1, h.Count(probe.Ref()))
		require.Zero(t, m.Len())
	})

	t.Run("a miss changes nothing", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Store(types.BoxI32(1), types.BoxI32(2))
		m.Delete(types.BoxI32(9))
		require.Equal(t, 1, m.Len())
	})

	t.Run("a reinserted key goes last", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		for k := range int32(3) {
			m.Store(types.BoxI32(k), 0)
		}
		m.Delete(types.BoxI32(0))
		m.Store(types.BoxI32(0), 0)
		require.Equal(t, "map[i32]i32{1: 0, 2: 0, 0: 0}", m.String())
	})
}

func TestMap_Clear(t *testing.T) {
	t.Run("releases every key and value once", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeString()), 0)
		var all []types.Boxed
		for _, s := range []string{"a", "b", "c"} {
			k, v := text(h, s), text(h, s+s)
			m.Store(k, v)
			all = append(all, k, v)
		}
		m.Clear()
		require.Zero(t, m.Len())
		for _, v := range all {
			require.False(t, h.Alive(v.Ref()))
		}
	})

	t.Run("releases the values of NaN entries", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeF64(), types.TypeString()), 0)
		var values []types.Boxed
		for range 5 {
			v := text(h, "v")
			m.Store(types.BoxF64(math.NaN()), v)
			values = append(values, v)
		}
		require.Equal(t, 5, m.Len())
		m.Clear()
		for _, v := range values {
			require.False(t, h.Alive(v.Ref()))
		}
	})

	t.Run("a finalizer that reads the map sees it empty", func(t *testing.T) {
		var m *types.Map
		var lens []int
		h := types.NewHeap(8, 0, types.Hooks{Finalize: func(int, types.Value) { lens = append(lens, m.Len()) }})
		h.Add(types.Null)
		m = types.NewMap(h, types.NewMapType(types.TypeI32(), types.TypeAny()), 0)
		for k := range int32(3) {
			m.Store(types.BoxI32(k), types.BoxRef(h.Add(&closer{})))
		}
		m.Clear()
		require.Equal(t, []int{0, 0, 0}, lens)
	})

	t.Run("ends every walk", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Store(types.BoxI32(1), 0)
		m.Store(types.BoxI32(2), 0)
		it := types.NewMapIterator(1, m)
		require.True(t, it.Next())
		m.Clear()
		m.Store(types.BoxI32(3), 0)
		require.False(t, it.Next())
	})
}

func TestMap_Len(t *testing.T) {
	m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
	require.Zero(t, m.Len())
	m.Store(types.BoxI32(1), 0)
	m.Store(types.BoxI32(1), 0)
	require.Equal(t, 1, m.Len())
}

func TestMap_Range(t *testing.T) {
	t.Run("insertion order and early stop", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		for _, k := range []int32{3, 1, 2} {
			m.Store(types.BoxI32(k), types.BoxI32(k*10))
		}
		var keys []int32
		m.Range(func(key, value types.Boxed) bool {
			keys = append(keys, key.I32())
			require.Equal(t, types.BoxI32(key.I32()*10), value)
			return true
		})
		require.Equal(t, []int32{3, 1, 2}, keys)

		keys = nil
		m.Range(func(key, _ types.Boxed) bool {
			keys = append(keys, key.I32())
			return len(keys) < 2
		})
		require.Equal(t, []int32{3, 1}, keys)
	})

	t.Run("every kind of key comes back as it was stored", func(t *testing.T) {
		tests := []struct {
			typ types.Type
			key types.Boxed
		}{
			{types.TypeI1(), types.BoxI1(true)},
			{types.TypeI8(), types.BoxI8(-3)},
			{types.TypeI32(), types.BoxI32(-3)},
			{types.TypeI64(), types.BoxI64(-3)},
			{types.TypeI64(), types.BoxI64(1<<48 - 1)},
			{types.TypeF32(), types.BoxF32(1.5)},
			{types.TypeF64(), types.BoxF64(-1.5)},
		}
		for _, tt := range tests {
			m := types.NewMap(nil, types.NewMapType(tt.typ, types.TypeI32()), 0)
			m.Store(tt.key, 0)
			m.Range(func(key, _ types.Boxed) bool {
				require.Equal(t, tt.key, key, tt.typ.String())
				return true
			})
		}
	})

	t.Run("a wide i64 key is the stored cell and nothing allocates", func(t *testing.T) {
		h := newHeap()
		m := types.NewMap(h, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		key := cell(h, 1<<50)
		m.Store(key, 0)
		before := h.Len()
		m.Range(func(stored, _ types.Boxed) bool {
			require.Equal(t, key, stored)
			return true
		})
		require.Equal(t, before, h.Len())
	})
}

func TestMapIterator_Next(t *testing.T) {
	i32 := func() *types.Map { return types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0) }

	t.Run("exhausted iterator after clear", func(t *testing.T) {
		m := i32()
		m.Store(types.BoxI32(1), types.BoxI32(2))
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		require.False(t, it.Next())
		m.Clear()
		require.Zero(t, m.Len())
		require.False(t, it.Next())
		require.True(t, it.Done())
		require.Equal(t, types.BoxedNull, it.Current())
		require.Equal(t, []types.Ref{7}, it.Refs(nil))
		require.False(t, it.Next())
	})

	t.Run("clear then two stores ends the walk", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		for range 10 {
			m.Store(types.BoxF64(math.NaN()), 1)
		}
		it := types.NewMapIterator(7, m)
		require.True(t, it.Next())
		m.Clear()
		m.Store(types.BoxF64(100), 1)
		m.Store(types.BoxF64(101), 1)
		require.False(t, it.Next())
		require.True(t, it.Done())
		require.Equal(t, []types.Ref{7}, it.Refs(nil))
	})

	t.Run("float keys survive a rehash mid-walk", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		for range 5 {
			m.Store(types.BoxF64(math.NaN()), 1)
		}
		for _, key := range []float64{0, 1.5, -2.5, math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64} {
			m.Store(types.BoxF64(key), 1)
		}
		it := types.NewMapIterator(1, m)
		produced := map[float64]int{}
		nans := 0
		record := func() {
			key := it.Current().(types.Boxed).F64()
			if math.IsNaN(key) {
				nans++
				return
			}
			produced[key]++
		}
		require.True(t, it.Next())
		record()
		for i := range 1000 {
			m.Store(types.BoxF64(float64(i)+10), 1)
		}
		deleted := math.NaN()
		for _, key := range []float64{1.5, -2.5, math.Inf(1)} {
			if produced[key] == 0 {
				m.Delete(types.BoxF64(key))
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

	t.Run("entries inserted during the walk are produced once, across rehashes", func(t *testing.T) {
		m := i32()
		for k := range int32(4) {
			m.Store(types.BoxI32(k), 1)
		}
		it := types.NewMapIterator(1, m)
		var produced []int32
		for it.Next() {
			key := it.Current().(types.Boxed).I32()
			produced = append(produced, key)
			if key < 100 {
				m.Delete(types.BoxI32(key))
				m.Store(types.BoxI32(key+100), 1)
				m.Store(types.BoxI32(key+200), 1)
				m.Delete(types.BoxI32(key + 200))
			}
		}
		require.Equal(t, []int32{0, 1, 2, 3, 100, 101, 102, 103}, produced)
	})

	t.Run("two walks follow the same rehashes", func(t *testing.T) {
		m := i32()
		for k := range int32(6) {
			m.Store(types.BoxI32(k), 1)
		}
		first, second := types.NewMapIterator(1, m), types.NewMapIterator(2, m)
		require.True(t, first.Next())
		require.True(t, second.Next())
		require.True(t, second.Next())
		m.Delete(types.BoxI32(0))
		m.Delete(types.BoxI32(1))
		var want []int32
		for k := range int32(4) {
			want = append(want, k+2)
		}
		for k := range int32(100) {
			m.Store(types.BoxI32(k+100), 1)
			want = append(want, k+100)
		}
		for _, it := range []*types.MapIterator{first, second} {
			var produced []int32
			for it.Next() {
				produced = append(produced, it.Current().(types.Boxed).I32())
			}
			require.Equal(t, want, produced)
		}
	})

	t.Run("a text key is borrowed and a wide i64 key is a fresh value", func(t *testing.T) {
		h := newHeap()
		str := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		key := text(h, "a")
		str.Store(key, 1)
		it := types.NewMapIterator(7, str)
		require.True(t, it.Next())
		require.Equal(t, key, it.Current())
		require.Equal(t, []types.Ref{7, types.Ref(key.Ref())}, it.Refs(nil))

		wide := types.NewMap(h, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		wide.Store(cell(h, 1<<50), 1)
		it = types.NewMapIterator(7, wide)
		require.True(t, it.Next())
		require.Equal(t, types.I64(1<<50), it.Current())
		require.Equal(t, []types.Ref{7}, it.Refs(nil))
	})

	t.Run("every key kind visits each entry once", func(t *testing.T) {
		h := newHeap()
		drain := func(m *types.Map) []string {
			var out []string
			for it := types.NewMapIterator(1, m); it.Next(); {
				out = append(out, fmt.Sprint(it.Current()))
			}
			return out
		}
		i64 := types.NewMap(h, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		i64.Store(cell(h, math.MinInt64), 0)
		i64.Store(types.BoxI64(5), 0)
		f64 := types.NewMap(h, types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		f64.Store(types.BoxF64(math.NaN()), 0)
		f64.Store(types.BoxF64(math.NaN()), 0)
		f64.Store(types.BoxF64(-2.5), 0)
		str := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		str.Store(text(h, "a"), 0)
		str.Store(text(h, "b"), 0)
		anyMap := types.NewMap(h, types.NewMapType(types.TypeAny(), types.TypeI32()), 0)
		anyMap.Store(types.BoxI8(1), 0)
		anyMap.Store(types.BoxedNull, 0)
		require.Equal(t, []string{"-9223372036854775808", "5"}, drain(i64))
		require.Len(t, drain(f64), 3)
		require.Len(t, drain(str), 2)
		require.Len(t, drain(anyMap), 2)
	})

	t.Run("mutation during iteration", func(t *testing.T) {
		const present = 200
		tests := []struct {
			name   string
			mutate func(m *types.Map, rng *rand.Rand, del func(int32))
		}{
			{"set and delete", func(m *types.Map, rng *rand.Rand, del func(int32)) {
				key := int32(rng.IntN(2 * present))
				if rng.IntN(2) == 0 {
					m.Store(types.BoxI32(key), 1)
				} else {
					del(key)
				}
			}},
			{"growth every step", func(m *types.Map, rng *rand.Rand, del func(int32)) {
				for range 64 {
					m.Store(types.BoxI32(int32(present+rng.IntN(4096))), 1)
				}
				del(int32(rng.IntN(present)))
			}},
			{"delete churn", func(m *types.Map, rng *rand.Rand, del func(int32)) {
				for range 8 {
					key := int32(present + rng.IntN(64))
					m.Store(types.BoxI32(key), 1)
					del(key)
				}
				del(int32(rng.IntN(present)))
			}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				for seed := uint64(1); seed <= 20; seed++ {
					rng := rand.New(rand.NewPCG(seed, 7))
					m := i32()
					for k := range int32(present) {
						m.Store(types.BoxI32(k), 1)
					}
					deleted := map[int32]bool{}
					del := func(key int32) {
						if _, ok := m.LoadAndDelete(types.BoxI32(key)); ok {
							deleted[key] = true
						}
					}
					visited := map[int32]bool{}
					it := types.NewMapIterator(1, m)
					for it.Next() {
						key := it.Current().(types.Boxed).I32()
						_, live := m.Load(types.BoxI32(key))
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
	m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
	m.Store(types.BoxI32(3), types.BoxI32(4))
	it := types.NewMapIterator(1, m)
	require.Equal(t, types.BoxedNull, it.Current())
	require.True(t, it.Next())
	require.Equal(t, types.BoxI32(3), it.Current())
}

func TestMapIterator_Done(t *testing.T) {
	m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
	m.Store(types.BoxI32(3), types.BoxI32(4))
	it := types.NewMapIterator(1, m)
	require.True(t, it.Done())
	require.True(t, it.Next())
	require.False(t, it.Done())
	require.False(t, it.Next())
	require.True(t, it.Done())
}

func TestMap_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0).Kind())
}

func TestMap_Type(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeI32())
	require.Equal(t, typ, types.NewMap(nil, typ, 0).Type())
}

func TestMap_Refs(t *testing.T) {
	t.Run("inline i64 value", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI64()), 0)
		m.Store(types.BoxI32(1), types.BoxI64(2))

		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = m.Refs(nil)
		})
		require.Empty(t, refs)
		require.Equal(t, []types.Ref{9}, m.Refs([]types.Ref{9}))
		require.Zero(t, allocs)
	})

	t.Run("text keys, any refs, cells, and values", func(t *testing.T) {
		h := newHeap()
		str := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeString()), 0)
		k, v := text(h, "k"), text(h, "v")
		str.Store(k, v)
		require.Equal(t, []types.Ref{9, types.Ref(k.Ref()), types.Ref(v.Ref())}, str.Refs([]types.Ref{9}))

		anyMap := types.NewMap(h, types.NewMapType(types.TypeAny(), types.TypeAny()), 0)
		anyMap.Store(types.BoxedNull, types.BoxedNull)
		anyMap.Store(types.BoxI32(1), types.BoxI32(2))
		require.Equal(t, []types.Ref{0, 0}, anyMap.Refs(nil))

		i64 := types.NewMap(h, types.NewMapType(types.TypeI64(), types.TypeI64()), 0)
		wide, spilled := cell(h, 1<<50), cell(h, 1<<51)
		i64.Store(wide, spilled)
		i64.Store(types.BoxI64(3), types.BoxI64(4))
		require.Equal(t, []types.Ref{types.Ref(wide.Ref()), types.Ref(spilled.Ref())}, i64.Refs(nil))
	})

	t.Run("a float NaN key that carries the reference tag is not traced", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeF64(), types.TypeI32()), 0)
		m.Store(types.Boxed(types.Tag(types.KindRef)|7), 0)
		require.Equal(t, 1, m.Len())
		require.Empty(t, m.Refs(nil))
	})
}

func TestMap_Clone(t *testing.T) {
	t.Run("a bound copy retains every reference", func(t *testing.T) {
		h := newHeap()
		typ := types.NewMapType(types.TypeAny(), types.TypeAny())
		src := types.NewMap(h, typ, 2)
		src.Zero = types.BoxRef(7)
		k, v := text(h, "k"), text(h, "v")
		src.Store(k, v)
		src.Store(types.BoxedNull, types.BoxedNull)
		src.Store(types.BoxI32(1), types.BoxI32(2))
		nulls := h.Count(0)

		c := src.Clone()
		require.Same(t, typ, c.Typ)
		require.Equal(t, src.Zero, c.Zero)
		require.Equal(t, src.String(), c.String())
		require.Equal(t, 2, h.Count(k.Ref()))
		require.Equal(t, 2, h.Count(v.Ref()))
		require.Equal(t, nulls+2, h.Count(0))

		c.Store(text(h, "k"), types.BoxI32(9))
		c.Delete(types.BoxI32(1))
		got, ok := src.Load(types.BoxI32(1))
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), got)
		require.Equal(t, 1, h.Count(v.Ref()))
	})

	t.Run("an unbound copy copies words", func(t *testing.T) {
		src := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		src.Store(types.BoxI32(1), types.BoxI32(2))
		c := src.Clone()
		c.Store(types.BoxI32(1), types.BoxI32(9))
		v, _ := src.Load(types.BoxI32(1))
		require.Equal(t, types.BoxI32(2), v)
	})

	t.Run("a rehash of the clone leaves a walk of the original in place", func(t *testing.T) {
		src := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		for k := range int32(6) {
			src.Store(types.BoxI32(k), 1)
		}
		it := types.NewMapIterator(1, src)
		require.True(t, it.Next())
		require.True(t, it.Next())
		c := src.Clone()
		c.Delete(types.BoxI32(0))
		c.Delete(types.BoxI32(1))
		for k := range int32(100) {
			c.Store(types.BoxI32(k+100), 1)
		}
		var produced []int32
		for it.Next() {
			produced = append(produced, it.Current().(types.Boxed).I32())
		}
		require.Equal(t, []int32{2, 3, 4, 5}, produced)
	})
}

func TestMap_String(t *testing.T) {
	t.Run("insertion order", func(t *testing.T) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Store(types.BoxI32(2), types.BoxI32(4))
		m.Store(types.BoxI32(1), types.BoxI32(2))
		require.Equal(t, "map[i32]i32{2: 4, 1: 2}", m.String())
	})

	t.Run("scalar keys", func(t *testing.T) {
		tests := []struct {
			typ  types.Type
			key  types.Boxed
			want string
		}{
			{types.TypeI64(), types.BoxI64(1), "map[i64]i32{1: 2}"},
			{types.TypeF32(), types.BoxF32(1), "map[f32]i32{1: 2}"},
			{types.TypeF64(), types.BoxF64(1), "map[f64]i32{1: 2}"},
			{types.TypeI1(), types.BoxI1(true), "map[i1]i32{true: 2}"},
		}
		for _, tt := range tests {
			m := types.NewMap(nil, types.NewMapType(tt.typ, types.TypeI32()), 0)
			m.Store(tt.key, types.BoxI32(2))
			require.Equal(t, tt.want, m.String())
		}
	})

	t.Run("text and cell content", func(t *testing.T) {
		h := newHeap()
		str := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
		str.Store(text(h, "b"), types.BoxI32(1))
		str.Store(text(h, "a"), types.BoxI32(2))
		require.Equal(t, `map[string]i32{"b": 1, "a": 2}`, str.String())

		wide := types.NewMap(h, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
		wide.Store(cell(h, 1<<50), types.BoxI32(1))
		require.Equal(t, "map[i64]i32{1125899906842624: 1}", wide.String())
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
	require.Equal(t, `map[i32]string{2: "b", 1: "a"}`, value.String())
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
	require.Equal(t, types.KindRef, types.NewMapIterator(1, types.NewMap(nil, types.NewMapType(types.TypeAny(), types.TypeI32()), 0)).Kind())
}

func TestMapIterator_Type(t *testing.T) {
	m := types.NewMap(nil, types.NewMapType(types.TypeI64(), types.TypeI32()), 0)
	require.Equal(t, types.NewIteratorType(types.TypeI64()), types.NewMapIterator(1, m).Type())
}

func TestMapIterator_Refs(t *testing.T) {
	h := newHeap()
	m := types.NewMap(h, types.NewMapType(types.TypeString(), types.TypeI32()), 0)
	key := text(h, "a")
	m.Store(key, types.BoxI32(2))
	it := types.NewMapIterator(7, m)
	require.True(t, it.Next())
	require.Equal(t, []types.Ref{5, 7, types.Ref(key.Ref())}, it.Refs([]types.Ref{5}))
}

func TestMapIterator_String(t *testing.T) {
	require.Equal(t, "map.iterator", types.NewMapIterator(1, types.NewMap(nil, types.NewMapType(types.TypeAny(), types.TypeI32()), 0)).String())
}

func BenchmarkMap_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		m := types.NewMap(nil, types.NewMapType(types.TypeI32(), types.TypeI32()), 0)
		m.Store(types.BoxI32(1), types.BoxI32(2))
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
		m := types.NewMap(nil, types.NewMapType(types.TypeAny(), types.TypeAny()), 0)
		m.Store(types.BoxRef(1), types.BoxRef(2))
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
