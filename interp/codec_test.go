package interp_test

import (
	"context"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	interp "github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type marshalCustom int32

type marshalContextKey byte

type marshalHostFields struct {
	Count int32
}

func (v *marshalHostFields) Bump(n int32) int32 {
	v.Count += n
	return v.Count
}

func (*marshalHostFields) Context(ctx context.Context) int32 {
	if ctx.Value(marshalContextKey(0)) == "value" {
		return 7
	}
	return 0
}

func (v marshalCustom) MarshalVM(*interp.Encoder) (types.Value, error) {
	return types.I32(v), nil
}

func (v *marshalCustom) UnmarshalVM(_ *interp.Decoder, value types.Value) error {
	n, ok := value.(types.I32)
	if !ok {
		return interp.ErrTypeMismatch
	}
	*v = marshalCustom(n)
	return nil
}

type codecAlias struct{ Target types.Ref }

type codecStrings struct{ A, B, C string }

type codecNode struct {
	Value int32
	Next  *codecNode
}

type codecMillis time.Duration

type codecPair struct {
	Delay codecMillis
	Count int32
}

type codecWide int32

type codecShared struct{ A, B int32 }

type codecFirst struct{ Shared codecShared }

type codecSecond struct{ Shared codecShared }

func TestNewRegistry(t *testing.T) {
	t.Run("standard library defaults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, time.Unix(1, 2))
		require.NoError(t, err)
		require.Equal(t, types.I64(time.Unix(1, 2).UnixNano()), value)
	})

	t.Run("registration replaces a default", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(interp.WithMarshaler(
			reflect.TypeFor[time.Time](), types.TypeI64,
			interp.MarshalerFunc(func(_ *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
				return types.I64((*(*time.Time)(p)).Unix()), nil
			})))
		defer i.Close()

		value, err := r.Marshal(i, time.Unix(1, 2))
		require.NoError(t, err)
		require.Equal(t, types.I64(1), value)
	})
}

func TestWithMarshaler(t *testing.T) {
	millis := interp.WithMarshaler(
		reflect.TypeFor[codecMillis](), types.TypeI64,
		interp.MarshalerFunc(func(e *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
			ms := int64(*(*codecMillis)(p)) / int64(time.Millisecond)
			return e.Encode(reflect.TypeFor[int64](), unsafe.Pointer(&ms))
		}))

	t.Run("registered type", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(millis)
		defer i.Close()

		value, err := r.Marshal(i, codecMillis(2*time.Second))
		require.NoError(t, err)
		require.Equal(t, types.I64(2000), value)
	})

	t.Run("declared type drives the enclosing layout", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(millis)
		defer i.Close()

		value, err := r.Marshal(i, codecPair{Delay: codecMillis(3 * time.Second), Count: 4})
		require.NoError(t, err)
		st, ok := value.(*types.Struct)
		require.True(t, ok)
		require.Equal(t, types.TypeI64, st.Typ.Fields[st.Typ.FieldIndex("Delay")].Type)
		require.Equal(t, types.I64(3000), types.Unbox(st.Field(st.Typ.FieldIndex("Delay"))))
	})

	t.Run("value marshaler applies without registration", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, marshalCustom(9))
		require.NoError(t, err)
		require.Equal(t, types.I32(9), value)
	})
}

func TestWithUnmarshaler(t *testing.T) {
	millis := interp.WithUnmarshaler(
		reflect.TypeFor[codecMillis](),
		interp.UnmarshalerFunc(func(d *interp.Decoder, val types.Value, p unsafe.Pointer) error {
			var ms int64
			if err := d.Decode(val, reflect.TypeFor[int64](), unsafe.Pointer(&ms)); err != nil {
				return err
			}
			*(*codecMillis)(p) = codecMillis(ms) * codecMillis(time.Millisecond)
			return nil
		}))

	t.Run("registered type", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(millis)
		defer i.Close()

		var dst codecMillis
		require.NoError(t, r.Unmarshal(i, types.I64(2000), &dst))
		require.Equal(t, codecMillis(2*time.Second), dst)
	})

	t.Run("nested in a struct", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(millis)
		defer i.Close()
		src := types.NewStruct(
			types.NewStructType(
				types.NewStructField(types.TypeI64, types.FieldWithName("Delay")),
				types.NewStructField(types.TypeI32, types.FieldWithName("Count")),
			),
			types.BoxI64(3000), types.BoxI32(4),
		)

		var dst codecPair
		require.NoError(t, r.Unmarshal(i, src, &dst))
		require.Equal(t, codecPair{Delay: codecMillis(3 * time.Second), Count: 4}, dst)
	})

	t.Run("unregistered direction is unsupported", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(interp.WithMarshaler(
			reflect.TypeFor[codecMillis](), types.TypeI64,
			interp.MarshalerFunc(func(_ *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
				return types.I64(*(*codecMillis)(p)), nil
			})))
		defer i.Close()

		var dst codecMillis
		require.ErrorIs(t, r.Unmarshal(i, types.I64(1), &dst), interp.ErrUnsupportedMarshalType)
	})
}

func TestRegistry_Marshal(t *testing.T) {
	t.Run("scalar value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		v, err := r.Marshal(i, int32(7))
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("function receives active context", func(t *testing.T) {
		var got context.Context
		setup := interp.New(program.New(nil))
		r := interp.NewRegistry()
		fn, err := r.Marshal(setup, func(ctx context.Context) int32 {
			got = ctx
			return 7
		})
		require.NoError(t, err)
		require.NoError(t, setup.Close())

		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn))
		i := interp.New(prog)
		defer i.Close()
		ctx := context.WithValue(context.Background(), marshalContextKey(0), "value")
		require.NoError(t, i.Run(ctx))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)
		require.Equal(t, ctx, got)
	})

	t.Run("bound method receives active context", func(t *testing.T) {
		setup := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer setup.Close()
		value, err := r.Marshal(setup, marshalHostFields{})
		require.NoError(t, err)
		host := value.(*types.Struct)
		method := host.Field(host.Typ.FieldIndex("Context"))
		fn, err := setup.Load(method.Ref())
		require.NoError(t, err)

		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn))
		i := interp.New(prog)
		defer i.Close()
		ctx := context.WithValue(context.Background(), marshalContextKey(0), "value")
		require.NoError(t, i.Run(ctx))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), got)
	})

	t.Run("marshaled function is shared and race-safe across interpreters", func(t *testing.T) {
		setup := interp.New(program.New(nil))
		r := interp.NewRegistry()
		v, err := r.Marshal(setup, func(a, b int32) int32 { return a + b })
		require.NoError(t, err)
		require.NoError(t, setup.Close())

		// program.New's default constant path keeps the *HostFunction Go
		// value itself (not a copy) in each Interpreter's heap, so two
		// Interpreters built from programs referencing the same fn share one
		// *HostFunction and race on any call-scoped state it caches.
		fn := v.(*interp.HostFunction)

		prog1 := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)
		prog2 := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 10),
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)

		i1 := interp.New(prog1)
		defer i1.Close()
		i2 := interp.New(prog2)
		defer i2.Close()

		var wg sync.WaitGroup
		var err1, err2 error
		var v1, v2 types.Value
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err1 = i1.Run(context.Background()); err1 == nil {
				v1, err1 = i1.Pop()
			}
		}()
		go func() {
			defer wg.Done()
			if err2 = i2.Run(context.Background()); err2 == nil {
				v2, err2 = i2.Pop()
			}
		}()
		wg.Wait()

		require.NoError(t, err1)
		require.NoError(t, err2)
		require.Equal(t, types.I32(3), v1)
		require.Equal(t, types.I32(30), v2)
	})
	t.Run("named scalar and pointers", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		type count int32
		n := count(7)
		value, err := r.Marshal(i, &n)
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)

		var nilCount *count
		value, err = r.Marshal(i, nilCount)
		require.NoError(t, err)
		require.Equal(t, types.Ref(0), value)
	})

	t.Run("nested collections", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := struct {
			Name   string
			Values []int32
			Fixed  [2]uint16
			Lookup map[string]int64
		}{
			Name:   "vm",
			Values: []int32{1, 2, 3},
			Fixed:  [2]uint16{4, 5},
			Lookup: map[string]int64{"x": 6},
		}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		var dst struct {
			Name   string
			Values []int32
			Fixed  [2]uint16
			Lookup map[string]int64
		}
		require.NoError(t, r.Unmarshal(i, value, &dst))
		require.Equal(t, src, dst)
	})

	t.Run("custom value marshaler", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, marshalCustom(9))
		require.NoError(t, err)
		require.Equal(t, types.I32(9), value)
	})

	t.Run("standard library default", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := time.Unix(1, 2)

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		require.Equal(t, types.I64(src.UnixNano()), value)
	})

	t.Run("struct with methods", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, marshalHostFields{Count: 7})
		require.NoError(t, err)
		host, ok := value.(*types.Struct)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), host.Field(host.Typ.FieldIndex("Count")))

		bound := host.Field(host.Typ.FieldIndex("Context"))
		require.Equal(t, types.KindRef, bound.Kind())
		fn, err := i.Load(bound.Ref())
		require.NoError(t, err)
		require.IsType(t, &interp.HostFunction{}, fn)
	})

	t.Run("methods bind to the caller's pointer", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := &marshalHostFields{}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		st := value.(*types.Struct)
		bound, err := i.Load(st.Field(st.Typ.FieldIndex("Bump")).Ref())
		require.NoError(t, err)
		_, err = bound.(*interp.HostFunction).Fn(i, []types.Boxed{types.BoxI32(2)})
		require.NoError(t, err)
		require.Equal(t, int32(2), src.Count)
	})

	t.Run("a marshaled value binds methods to its own copy", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := marshalHostFields{}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		st := value.(*types.Struct)
		bound, err := i.Load(st.Field(st.Typ.FieldIndex("Bump")).Ref())
		require.NoError(t, err)
		_, err = bound.(*interp.HostFunction).Fn(i, []types.Boxed{types.BoxI32(2)})
		require.NoError(t, err)
		require.Zero(t, src.Count)
	})

	// A marshaled entry must be indexed the way MAP_GET indexes the same key,
	// or guest code cannot reach it.
	for _, tt := range []struct {
		name  string
		value any
		key   instr.Instruction
		want  types.Value
	}{
		{
			name:  "concrete primitive key",
			value: map[int32]int32{1: 7},
			key:   instr.New(instr.I32_CONST, 1),
			want:  types.I32(7),
		},
		{
			name:  "primitive key in a dynamic map",
			value: map[any]int32{int32(1): 7},
			key:   instr.New(instr.I32_CONST, 1),
			want:  types.I32(7),
		},
		{
			name:  "string key",
			value: map[string]int32{"a": 7},
			key:   instr.New(instr.CONST_GET, 0),
			want:  types.I32(7),
		},
		{
			name:  "string key in a dynamic map",
			value: map[any]int32{"a": 7},
			key:   instr.New(instr.CONST_GET, 0),
			want:  types.I32(7),
		},
		{
			name:  "bool key in a dynamic map",
			value: map[any]int32{true: 7},
			key:   instr.New(instr.I32_CONST, 1),
			want:  types.I32(7),
		},
		{
			name:  "i64 key past the boxed payload",
			value: map[int64]int32{1 << 50: 7},
			key:   instr.New(instr.I64_CONST, 1<<50),
			want:  types.I32(7),
		},
	} {
		t.Run("map key reachable from a guest lookup: "+tt.name, func(t *testing.T) {
			prog := program.New(
				[]instr.Instruction{tt.key, instr.New(instr.MAP_GET)},
				program.WithConstants(types.String("a")),
			)
			i := interp.New(prog)
			r := interp.NewRegistry()
			defer i.Close()

			value, err := r.Marshal(i, tt.value)
			require.NoError(t, err)
			require.NoError(t, i.Push(value))
			require.NoError(t, i.Run(context.Background()))

			got, err := i.Pop()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("a scalar key is stored without a heap reference", func(t *testing.T) {
		// One heap slot is the permanent null, so this interpreter can allocate
		// nothing: a key routed through a slot would exhaust the heap here.
		i := interp.New(program.New(nil), interp.WithHeapLimit(1))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, map[int64]int32{1 << 50: 7})
		require.NoError(t, err)

		got, ok := value.(*types.TypedMap[int64]).Get(1 << 50)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), got)
	})

	t.Run("every scalar key kind keeps its Go value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		bools, err := r.Marshal(i, map[bool]int32{true: 1})
		require.NoError(t, err)
		gotBool, ok := bools.(*types.TypedMap[bool]).Get(true)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(1), gotBool)

		bytes, err := r.Marshal(i, map[int8]int32{-3: 2})
		require.NoError(t, err)
		gotByte, ok := bytes.(*types.TypedMap[int8]).Get(-3)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(2), gotByte)

		words, err := r.Marshal(i, map[int32]int32{math.MinInt32: 3})
		require.NoError(t, err)
		gotWord, ok := words.(*types.TypedMap[int32]).Get(math.MinInt32)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(3), gotWord)

		longs, err := r.Marshal(i, map[int64]int32{math.MinInt64: 4})
		require.NoError(t, err)
		gotLong, ok := longs.(*types.TypedMap[int64]).Get(math.MinInt64)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(4), gotLong)

		floats, err := r.Marshal(i, map[float32]int32{-1.5: 5})
		require.NoError(t, err)
		gotFloat, ok := floats.(*types.TypedMap[float32]).Get(-1.5)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(5), gotFloat)

		doubles, err := r.Marshal(i, map[float64]int32{-1.5: 6})
		require.NoError(t, err)
		gotDouble, ok := doubles.(*types.TypedMap[float64]).Get(-1.5)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(6), gotDouble)
	})

	t.Run("a map key wider than its declared type overflows", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(interp.WithMarshaler(
			reflect.TypeFor[codecWide](), types.TypeI32,
			interp.MarshalerFunc(func(*interp.Encoder, unsafe.Pointer) (types.Value, error) {
				return types.I64(math.MaxInt32 + 1), nil
			})))
		defer i.Close()

		_, err := r.Marshal(i, map[codecWide]int32{0: 7})
		require.ErrorIs(t, err, interp.ErrValueOverflow)
	})

	t.Run("a nil pointer key has no scalar value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		_, err := r.Marshal(i, map[*int32]int32{nil: 7})
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
	})

	t.Run("a marshaled alias keeps its target alive", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		addr, err := i.Alloc(types.String("payload"))
		require.NoError(t, err)

		value, err := r.Marshal(i, codecAlias{Target: types.Ref(addr)})
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))

		got, err := i.Load(value.(*types.Struct).Field(0).Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("payload"), got)
	})

	t.Run("a failed conversion releases what it published", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			value any
		}{
			{name: "struct", value: codecStrings{A: "aaa", B: "bbb", C: "ccc"}},
			{name: "slice", value: []string{"aaa", "bbb", "ccc"}},
			{name: "map", value: map[string]string{"a": "aaa", "b": "bbb", "c": "ccc"}},
		} {
			i := interp.New(program.New(nil), interp.WithHeapLimit(3))
			r := interp.NewRegistry()

			_, err := r.Marshal(i, tt.value)
			require.ErrorIs(t, err, interp.ErrHeapExhausted, tt.name)

			addr, err := i.Alloc(types.String("x"))
			require.NoError(t, err, tt.name)
			require.NoError(t, i.Release(addr), tt.name)
			require.NoError(t, i.Close(), tt.name)
		}
	})

	t.Run("recursive type", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := &codecNode{Value: 1, Next: &codecNode{Value: 2}}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		var dst codecNode
		require.NoError(t, r.Unmarshal(i, value, &dst))
		require.Equal(t, int32(1), dst.Value)
		require.Equal(t, int32(2), dst.Next.Value)
		require.Nil(t, dst.Next.Next)
	})

	t.Run("cycle", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		node := &struct{ Next any }{}
		node.Next = node
		_, err := r.Marshal(i, node)
		require.ErrorIs(t, err, interp.ErrMarshalCycle)

		// A map or slice reaches itself without an intervening pointer, so the
		// walk has to refuse those containers too rather than recurse forever.
		entries := map[string]any{}
		entries["self"] = entries
		_, err = r.Marshal(i, entries)
		require.ErrorIs(t, err, interp.ErrMarshalCycle)

		elems := []any{nil}
		elems[0] = elems
		_, err = r.Marshal(i, elems)
		require.ErrorIs(t, err, interp.ErrMarshalCycle)
	})

	t.Run("unsupported type", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		_, err := r.Marshal(i, make(chan int))
		require.ErrorIs(t, err, interp.ErrUnsupportedMarshalType)
	})

	t.Run("shared nested type", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		first, err := r.Marshal(i, codecFirst{})
		require.NoError(t, err)
		second, err := r.Marshal(i, codecSecond{})
		require.NoError(t, err)

		// A struct type is built once per compile, so the two outer types hold
		// the same one only when the nested type was compiled once.
		lhs := first.Type().(*types.StructType)
		rhs := second.Type().(*types.StructType)
		require.Same(t, lhs.Fields[0].Type, rhs.Fields[0].Type)
	})

	t.Run("shared nested type across interpreters", func(t *testing.T) {
		r := interp.NewRegistry()
		i1 := interp.New(program.New(nil))
		defer i1.Close()
		i2 := interp.New(program.New(nil))
		defer i2.Close()

		var wg sync.WaitGroup
		var err1, err2 error
		var v1, v2 types.Value
		wg.Add(2)
		go func() {
			defer wg.Done()
			v1, err1 = r.Marshal(i1, codecFirst{})
		}()
		go func() {
			defer wg.Done()
			v2, err2 = r.Marshal(i2, codecSecond{})
		}()
		wg.Wait()

		require.NoError(t, err1)
		require.NoError(t, err2)
		require.IsType(t, &types.Struct{}, v1)
		require.IsType(t, &types.Struct{}, v2)
	})
}

func TestRegistry_Unmarshal(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		var dst int32
		require.NoError(t, r.Unmarshal(i, types.I32(7), &dst))
		require.Equal(t, int32(7), dst)
	})

	t.Run("VM function", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()

		var add func(int32, int32) (int32, error)
		require.NoError(t, r.Unmarshal(i, fn, &add))
		got, err := add(2, 3)
		require.NoError(t, err)
		require.Equal(t, int32(5), got)
	})

	t.Run("VM function with context", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()

		var add func(context.Context, int32, int32) (int32, error)
		require.NoError(t, r.Unmarshal(i, fn, &add))
		got, err := add(context.Background(), 2, 3)
		require.NoError(t, err)
		require.Equal(t, int32(5), got)
	})

	t.Run("VM function context identity", func(t *testing.T) {
		var got context.Context
		i := interp.New(program.New(nil), interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
			got = i.Context()
			return nil
		}))
		defer i.Close()
		r := interp.NewRegistry()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(addr)) }()

		var call func(context.Context) (int32, error)
		require.NoError(t, r.Unmarshal(i, types.BoxRef(addr), &call))
		ctx := context.WithValue(context.Background(), marshalContextKey(0), "value")
		value, err := call(ctx)
		require.NoError(t, err)
		require.Equal(t, int32(7), value)
		require.Equal(t, ctx, got)
	})

	t.Run("VM function canceled context", func(t *testing.T) {
		i := interp.New(program.New(nil), interp.WithTick(1))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()

		var call func(context.Context) (int32, error)
		require.NoError(t, r.Unmarshal(i, fn, &call))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := call(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("VM function nil context uses background", func(t *testing.T) {
		var got context.Context
		i := interp.New(program.New(nil), interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
			got = i.Context()
			return nil
		}))
		defer i.Close()
		r := interp.NewRegistry()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(addr)) }()

		var call func(context.Context) (int32, error)
		require.NoError(t, r.Unmarshal(i, types.BoxRef(addr), &call))
		value, err := call(nil)
		require.NoError(t, err)
		require.Equal(t, int32(7), value)
		require.Equal(t, context.Background(), got)
	})

	t.Run("VM function non-first context", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()

		var call func(int32, context.Context) (int32, error)
		require.NoError(t, r.Unmarshal(i, fn, &call))
		got, err := call(7, nil)
		require.NoError(t, err)
		require.Equal(t, int32(7), got)
	})

	t.Run("VM closure with context", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		fnAddr, err := i.Alloc(fn)
		require.NoError(t, err)
		closureAddr, err := i.Alloc(types.NewClosure(fn.Typ, types.Ref(fnAddr), nil))
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(closureAddr)) }()

		var call func(context.Context) (int32, error)
		require.NoError(t, r.Unmarshal(i, types.BoxRef(closureAddr), &call))
		got, err := call(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(7), got)
	})

	t.Run("VM function without context uses background", func(t *testing.T) {
		var got context.Context
		i := interp.New(program.New(nil), interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
			got = i.Context()
			return nil
		}))
		defer i.Close()
		r := interp.NewRegistry()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(addr)) }()

		var call func() (int32, error)
		require.NoError(t, r.Unmarshal(i, types.BoxRef(addr), &call))
		value, err := call()
		require.NoError(t, err)
		require.Equal(t, int32(7), value)
		require.Equal(t, context.Background(), got)
	})

	t.Run("function ref", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)

		var call func() int32
		require.NoError(t, r.Unmarshal(i, types.BoxRef(addr), &call))
		require.Equal(t, int32(7), call())
		require.NoError(t, i.Release(addr))
	})

	t.Run("runtime error", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_DIV_S), instr.New(instr.RETURN),
		).MustBuild()

		var call func() (int32, error)
		require.NoError(t, r.Unmarshal(i, fn, &call))
		got, err := call()
		require.Zero(t, got)
		require.ErrorIs(t, err, interp.ErrDivideByZero)

		got, err = call()
		require.Zero(t, got)
		require.ErrorIs(t, err, interp.ErrDivideByZero)
	})

	t.Run("signature mismatch", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).MustBuild()

		var call func() int64
		require.ErrorIs(t, r.Unmarshal(i, fn, &call), interp.ErrTypeMismatch)
	})
	t.Run("custom value unmarshaler", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		var dst marshalCustom

		require.NoError(t, r.Unmarshal(i, types.I32(11), &dst))
		require.Equal(t, marshalCustom(11), dst)
	})

	t.Run("standard library default", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		var dst time.Time

		require.NoError(t, r.Unmarshal(i, types.I64(123), &dst))
		require.Equal(t, time.Unix(0, 123), dst)
	})

	t.Run("struct with methods round-trips exported fields", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		value, err := r.Marshal(i, marshalHostFields{Count: 7})
		require.NoError(t, err)
		var dst marshalHostFields

		require.NoError(t, r.Unmarshal(i, value, &dst))
		require.Equal(t, marshalHostFields{Count: 7}, dst)
	})

	t.Run("invalid target", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		require.ErrorIs(t, r.Unmarshal(i, types.I32(1), nil), interp.ErrInvalidUnmarshalTarget)
		require.ErrorIs(t, r.Unmarshal(i, types.I32(1), int32(0)), interp.ErrInvalidUnmarshalTarget)
		var dst *int32
		require.ErrorIs(t, r.Unmarshal(i, types.I32(1), dst), interp.ErrInvalidUnmarshalTarget)
	})

	t.Run("overflow", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		var dst int8

		require.ErrorIs(t, r.Unmarshal(i, types.I64(256), &dst), interp.ErrValueOverflow)
	})

	t.Run("type mismatch", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		var dst int32

		require.ErrorIs(t, r.Unmarshal(i, types.String("x"), &dst), interp.ErrTypeMismatch)
	})

}
