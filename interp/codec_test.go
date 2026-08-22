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
	Count  int32
	hidden int32
}

func (v *marshalHostFields) mark(n int32) { v.hidden = n }

func (v marshalHostFields) marked() int32 { return v.hidden }

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

// codecHeld holds a VM value next to an exported field, so a host layout has to
// leave the VM-valued field out rather than own the reference it names.
type codecHeld struct {
	Count int32
	Held  types.Value
	tag   int32
}

func (h *codecHeld) Tag() int32 { return h.tag }

// stringKey publishes text as the heap reference a string key arrives as.
func stringKey(t *testing.T, i *interp.Interpreter, text string) types.Boxed {
	addr, err := i.Alloc(types.String(text))
	require.NoError(t, err)
	return types.BoxRef(addr)
}

// codecCounted is fully exported and still carries a pointer method, the case
// that separates "has methods" from "a copy would lose something".
type codecCounted struct{ Count int32 }

func (c *codecCounted) Bump() int32 { c.Count++; return c.Count }

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

	t.Run("a method receives the active context", func(t *testing.T) {
		setup := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer setup.Close()
		obj, err := r.Marshal(setup, marshalHostFields{})
		require.NoError(t, err)
		fn, err := r.Marshal(setup, (*marshalHostFields).Context)
		require.NoError(t, err)

		// The receiver is the first parameter, so guest code pushes the host
		// value ahead of the function it calls.
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn, obj))
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

	// The form a struct takes follows one rule: use the VM type system whenever
	// a copy reproduces the whole value, and a live view only when it cannot.
	for _, tt := range []struct {
		name  string
		value any
		want  types.Value
	}{
		{name: "a value copies", value: codecShared{}, want: &types.Struct{}},
		{name: "a value with methods still copies", value: codecCounted{}, want: &types.Struct{}},
		{name: "a pointer is a view", value: &codecShared{}, want: &interp.HostStruct{}},
		{name: "a pointer with methods is a view", value: &codecCounted{}, want: &interp.HostStruct{}},
		{name: "an unexported field forces a view", value: marshalHostFields{}, want: &interp.HostStruct{}},
	} {
		t.Run("struct form: "+tt.name, func(t *testing.T) {
			i := interp.New(program.New(nil))
			r := interp.NewRegistry()
			defer i.Close()

			value, err := r.Marshal(i, tt.value)
			require.NoError(t, err)
			require.IsType(t, tt.want, value)
		})
	}

	t.Run("a struct with unexported state becomes a live view", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, marshalHostFields{Count: 7})
		require.NoError(t, err)
		host, ok := value.(*interp.HostStruct)
		require.True(t, ok)

		// The VM type is the one a copy would have had, methods included in
		// neither: a method is a function of its own, not a field.
		typ, ok := host.Type().(*types.StructType)
		require.True(t, ok)
		require.Len(t, typ.Fields, 1)
		require.Equal(t, "Count", typ.Fields[0].Name)
		require.Equal(t, types.KindRef, host.Kind())
	})

	t.Run("a method reached through a host value mutates the caller", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := &marshalHostFields{}

		obj, err := r.Marshal(i, src)
		require.NoError(t, err)
		fn, err := r.Marshal(i, (*marshalHostFields).Bump)
		require.NoError(t, err)
		recv, err := i.Alloc(obj)
		require.NoError(t, err)

		_, err = fn.(*interp.HostFunction).Fn(i, []types.Boxed{types.BoxRef(recv), types.BoxI32(2)})
		require.NoError(t, err)
		require.Equal(t, int32(2), src.Count)
	})

	t.Run("a marshaled value views the conversion's copy", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := marshalHostFields{}

		obj, err := r.Marshal(i, src)
		require.NoError(t, err)
		fn, err := r.Marshal(i, (*marshalHostFields).Bump)
		require.NoError(t, err)
		recv, err := i.Alloc(obj)
		require.NoError(t, err)

		_, err = fn.(*interp.HostFunction).Fn(i, []types.Boxed{types.BoxRef(recv), types.BoxI32(2)})
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
			// The VM keys i1, i8, and i32 alike, so a dynamic map holds one Go
			// type for the three of them and a key stored under another reads
			// as the element zero, the way Go's own dynamic keys miss.
			name:  "dynamic key outside the canonical Go type",
			value: map[any]int32{true: 7},
			key:   instr.New(instr.I32_CONST, 1),
			want:  types.I32(0),
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

		// A view keys straight into the Go map, so no key is ever routed
		// through a slot; the guest lookup itself is covered by the table above.
		value, err := r.Marshal(i, map[int64]int32{1 << 50: 7})
		require.NoError(t, err)
		require.IsType(t, &interp.HostMap{}, value)
	})

	t.Run("every scalar key kind reaches its entry", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		for _, tt := range []struct {
			name  string
			value any
			key   types.Boxed
		}{
			{name: "bool", value: map[bool]int32{true: 7}, key: types.BoxI1(true)},
			{name: "int8", value: map[int8]int32{-3: 7}, key: types.BoxI8(-3)},
			{name: "int32", value: map[int32]int32{math.MinInt32: 7}, key: types.BoxI32(math.MinInt32)},
			{name: "int64", value: map[int64]int32{-9: 7}, key: types.BoxI64(-9)},
			{name: "float32", value: map[float32]int32{-1.5: 7}, key: types.BoxF32(-1.5)},
			{name: "float64", value: map[float64]int32{-1.5: 7}, key: types.BoxF64(-1.5)},
			{name: "string", value: map[string]int32{"a": 7}, key: stringKey(t, i, "a")},
		} {
			value, err := r.Marshal(i, tt.value)
			require.NoError(t, err, tt.name)

			got, ok, err := value.(*interp.HostMap).Get(i, tt.key)
			require.NoError(t, err, tt.name)
			require.True(t, ok, tt.name)
			require.Equal(t, types.BoxI32(7), got, tt.name)
		}
	})

	t.Run("a map key that converts one way only cannot be looked up", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(interp.WithMarshaler(
			reflect.TypeFor[codecWide](), types.TypeI32,
			interp.MarshalerFunc(func(*interp.Encoder, unsafe.Pointer) (types.Value, error) {
				return types.I64(math.MaxInt32 + 1), nil
			})))
		defer i.Close()

		value, err := r.Marshal(i, map[codecWide]int32{0: 7})
		require.NoError(t, err)

		// A view converts a key on the way in rather than at marshal time, so a
		// key type that converts in one direction only cannot be looked up.
		_, _, err = value.(*interp.HostMap).Get(i, types.BoxI32(0))
		require.ErrorIs(t, err, interp.ErrUnsupportedMarshalType)
	})

	t.Run("a nil pointer key is a key like any other", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, map[*int32]int32{nil: 7})
		require.NoError(t, err)

		// A view keys by the Go pointer itself, so a nil key is a key like any
		// other rather than one that collides with a zero-valued pointee.
		got, ok, err := value.(*interp.HostMap).Get(i, types.BoxedNull)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), got)
	})

	t.Run("a host view reads a field holding a VM value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, &codecHeld{Count: 7, Held: types.I32(1)})
		require.NoError(t, err)
		host := value.(*interp.HostStruct)

		// One layout serves the value and the pointer, so a VM-valued field
		// keeps its slot. The view hands the reference out to the caller and
		// keeps none of its own.
		typ, ok := host.Type().(*types.StructType)
		require.True(t, ok)
		require.Len(t, typ.Fields, 2)
		require.Equal(t, "Held", typ.Fields[1].Name)

		got, err := host.Field(i, 1)
		require.NoError(t, err)
		held, err := i.Load(got.Ref())
		require.NoError(t, err)
		require.Equal(t, types.I32(1), held)
	})

	t.Run("a registered marshaler wins over the host rule", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry(interp.WithMarshaler(
			reflect.TypeFor[codecHeld](), types.TypeI32,
			interp.MarshalerFunc(func(_ *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
				return types.I32((*codecHeld)(p).Count), nil
			})))
		defer i.Close()

		value, err := r.Marshal(i, codecHeld{Count: 7})
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)
	})

	t.Run("a method runs against a struct guest code built", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn, err := r.Marshal(i, (*marshalHostFields).Bump)
		require.NoError(t, err)

		// Not a host value: the receiver decodes into a fresh Go struct, so the
		// call still runs and the mutation stays on that copy.
		typ := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("Count")))
		recv, err := i.Alloc(types.NewStruct(typ, types.BoxI32(5)))
		require.NoError(t, err)

		got, err := fn.(*interp.HostFunction).Fn(i, []types.Boxed{types.BoxRef(recv), types.BoxI32(2)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(7)}, got)
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
			// A slice and a map are views now: they publish nothing while
			// converting, so only a copying conversion can strand a reference.
			{name: "struct", value: codecStrings{A: "aaa", B: "bbb", C: "ccc"}},
			{name: "array", value: [3]string{"aaa", "bbb", "ccc"}},
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

		// A struct behind a pointer is a view, and a view is shallow, so a
		// self-referential one converts instead of recursing: the guest walks
		// it a field at a time, and each step produces the next view.
		node := &struct{ Next any }{}
		node.Next = node
		value, err := r.Marshal(i, node)
		require.NoError(t, err)
		require.IsType(t, &interp.HostStruct{}, value)

		// Every container is a view, so the copying walk that can still reach
		// itself is a pointer to something no view stands for. That one has to
		// refuse.
		var self any
		self = &self
		_, err = r.Marshal(i, &self)
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

	t.Run("a boxed ref decodes as the value it names", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		text, err := i.Alloc(types.String("hello"))
		require.NoError(t, err)
		var s string
		require.NoError(t, r.Unmarshal(i, types.BoxRef(text), &s))
		require.Equal(t, "hello", s)
		require.NoError(t, i.Release(text))

		// An i64 past the boxed payload lives on the heap, so a slot naming it
		// is the only form a guest can hand over.
		big := int64(1) << 60
		spilled, err := i.Alloc(types.I64(big))
		require.NoError(t, err)
		var n int64
		require.NoError(t, r.Unmarshal(i, types.BoxRef(spilled), &n))
		require.Equal(t, big, n)
		require.NoError(t, i.Release(spilled))
	})

	t.Run("a function the heap already holds keeps its slot", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)

		var call func() int32
		require.NoError(t, r.Unmarshal(i, fn, &call))
		require.Equal(t, int32(7), call())
		require.NoError(t, i.Release(addr))
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

	t.Run("VM function context in any position is host-only", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		// The VM signature carries the i32 alone: a context.Context stays on
		// the host side wherever it sits, so a method expression whose receiver
		// comes first is no different from a plain leading context.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32},
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

	t.Run("a host value round-trips unexported state", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := marshalHostFields{Count: 7}
		src.mark(3)
		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		var dst marshalHostFields

		require.NoError(t, r.Unmarshal(i, value, &dst))
		require.Equal(t, src, dst)
		require.Equal(t, int32(3), dst.marked())
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
