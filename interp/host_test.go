package interp

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type hostFields struct {
	Count  int32
	Bool   bool
	I8     int8
	I16    int16
	I32    int32
	I      int
	I64    int64
	U8     uint8
	U16    uint16
	U32    uint32
	U      uint
	U64    uint64
	Uintpt uintptr
	F32    float32
	F64    float64
	Text   string
	Value  types.Value
	hidden int32
}

func (*hostFields) Context(ctx context.Context) int32 {
	if ctx.Value(contextKey(0)) == "value" {
		return 7
	}
	return 0
}

func (h *hostFields) Bump(n int32) int32 {
	h.Count += n
	return h.Count
}

func (*hostFields) Touch() {}

type hostUserID int64

type hostCodecDuration time.Duration

type hostCodecReceiver struct{}

func (hostCodecReceiver) Duration() hostCodecDuration { return hostCodecDuration(time.Second) }

type hostCodecFields struct {
	Duration hostCodecDuration
}

type hostI64 int64

type hostI64Fields struct {
	Value hostI64
}

type hostRef int32

type hostRefFields struct {
	Value hostRef
}

type hostMethod struct{}

func (*hostMethod) Value() int32 { return 7 }

func (id hostUserID) Next() hostUserID {
	return id + 1
}

func (id *hostUserID) Bump(n int64) hostUserID {
	*id += hostUserID(n)
	return *id
}

func (id *hostUserID) Value() hostUserID {
	return *id
}

func TestNewHostFunction(t *testing.T) {
	t.Run("constructor", func(t *testing.T) {
		typ := &types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}
		fn := NewHostFunction(typ, func(_ *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
		})

		require.Same(t, typ, fn.Typ)
		got, err := fn.Fn(nil, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(8)}, got)
	})

	t.Run("public fields", func(t *testing.T) {
		typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
		fn := &HostFunction{
			Typ: typ,
			Fn: func(*Interpreter, []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(7)}, nil
			},
		}

		require.Same(t, typ, fn.Typ)
		got, err := fn.Fn(nil, nil)
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(7)}, got)
	})
}

func TestNewHostObject(t *testing.T) {
	t.Run("copies, binds, tracks refs, and round-trips", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		original := hostFields{Count: 1}
		host, err := NewHostObject(i, original)
		require.NoError(t, err)

		require.Equal(t, types.BoxI32(1), host.Field(host.Typ.FieldIndex("Count")))
		bump := host.Field(host.Typ.FieldIndex("Bump"))
		context := host.Field(host.Typ.FieldIndex("Context"))
		touch := host.Field(host.Typ.FieldIndex("Touch"))
		require.Equal(t, []types.Ref{types.Ref(bump.Ref()), types.Ref(context.Ref()), types.Ref(touch.Ref())}, host.Refs(nil))
		fn, err := i.Load(bump.Ref())
		require.NoError(t, err)
		returns, err := fn.(*HostFunction).Fn(i, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(5)}, returns)
		host.SetField(host.Typ.FieldIndex("Count"), types.BoxI32(9))
		require.Equal(t, types.BoxI32(9), host.Field(host.Typ.FieldIndex("Count")))
		require.Equal(t, int32(1), original.Count)
		pointer := &hostFields{Count: 2}
		pointerHost, err := NewHostObject(i, pointer)
		require.NoError(t, err)
		pointerHost.SetField(pointerHost.Typ.FieldIndex("Count"), types.BoxI32(8))
		require.Equal(t, int32(2), pointer.Count)

		var got hostFields
		require.NoError(t, i.Unmarshal(host, &got))
		require.Equal(t, int32(9), got.Count)
	})

	t.Run("uses built-in planner and converters with a custom codec", func(t *testing.T) {
		funcType := reflect.TypeOf(hostCodecDuration(0))
		conv := Converter{VMType: types.TypeF32,
			Marshal: func(_ *Interpreter, v any) (types.Value, error) {
				return types.F32(float32(v.(hostCodecDuration))), nil
			},
			Unmarshal: func(_ *Interpreter, v types.Value, dst any) error {
				*dst.(*hostCodecDuration) = hostCodecDuration(v.(types.F32))
				return nil
			},
		}
		i := New(program.New(nil), WithCodec(upperCodec(0)), WithConverter(funcType, conv))
		defer i.Close()

		host, err := NewHostObject(i, hostCodecReceiver{})
		require.NoError(t, err)
		fn := host.Typ.Fields[host.Typ.FieldIndex("Duration")].Type.(*types.FunctionType)
		require.Equal(t, []types.Type{types.TypeF32}, fn.Returns)
	})

	t.Run("uses converters for data fields with a custom codec", func(t *testing.T) {
		conv := Converter{VMType: types.TypeF32,
			Marshal: func(_ *Interpreter, v any) (types.Value, error) {
				return types.F32(float32(v.(hostCodecDuration))), nil
			},
			Unmarshal: func(_ *Interpreter, v types.Value, dst any) error {
				*dst.(*hostCodecDuration) = hostCodecDuration(v.(types.F32))
				return nil
			},
		}
		i := New(program.New(nil), WithCodec(upperCodec(0)), WithConverter(reflect.TypeOf(hostCodecDuration(0)), conv))
		defer i.Close()
		host, err := NewHostObject(i, hostCodecFields{Duration: hostCodecDuration(3)})
		require.NoError(t, err)
		field := host.Typ.FieldIndex("Duration")

		require.Equal(t, types.TypeF32, host.Typ.Fields[field].Type)
		require.Equal(t, types.BoxF32(3), host.Field(field))
		host.SetField(field, types.BoxF32(5))
		require.Equal(t, types.BoxF32(5), host.Field(field))
		host.SetRaw(field, uint64(types.BoxF32(7)))
		require.Equal(t, uint64(types.BoxF32(7)), host.Raw(field))

		got := host.Receiver.Elem().Interface().(hostCodecFields)
		require.Equal(t, hostCodecDuration(7), got.Duration)
	})

	t.Run("round-trips converted i64 fields through threaded access", func(t *testing.T) {
		conv := Converter{
			VMType: types.TypeI64,
			Marshal: func(_ *Interpreter, value any) (types.Value, error) {
				return types.I64(value.(hostI64)), nil
			},
			Unmarshal: func(_ *Interpreter, value types.Value, dst any) error {
				*dst.(*hostI64) = hostI64(value.(types.I64))
				return nil
			},
		}
		for _, value := range []hostI64{7, 1 << 50} {
			prog := program.New([]instr.Instruction{
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.STRUCT_SET),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
			}, program.WithConstants(types.I64(value)))
			i := New(prog, WithConverter(reflect.TypeOf(hostI64(0)), conv))
			host, err := NewHostObject(i, hostI64Fields{})
			require.NoError(t, err, value)
			require.NoError(t, i.Push(host), value)

			require.NoError(t, i.Run(context.Background()), value)
			got, err := i.Pop()
			require.NoError(t, err, value)
			require.Equal(t, types.I64(value), got)
			require.Equal(t, value, host.Receiver.Elem().Interface().(hostI64Fields).Value)
			require.NoError(t, i.Close(), value)
		}
	})

	t.Run("releases converted ref reads and writes", func(t *testing.T) {
		conv := Converter{
			VMType: types.TypeI32Array,
			Marshal: func(_ *Interpreter, value any) (types.Value, error) {
				return types.TypedArray[int32]{int32(value.(hostRef))}, nil
			},
			Unmarshal: func(_ *Interpreter, value types.Value, dst any) error {
				*dst.(*hostRef) = hostRef(value.(types.TypedArray[int32])[0])
				return nil
			},
		}

		code := make([]instr.Instruction, 0, 25)
		for range 6 {
			code = append(code,
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
				instr.New(instr.DROP),
			)
		}
		code = append(code, instr.New(instr.DROP))
		i := New(program.New(code), WithHeap(3), WithHeapLimit(3), WithConverter(reflect.TypeOf(hostRef(0)), conv))
		host, err := NewHostObject(i, hostRefFields{Value: 4})
		require.NoError(t, err)
		require.NoError(t, i.Push(host))
		require.NoError(t, i.Run(context.Background()))
		require.NoError(t, i.Close())

		code = make([]instr.Instruction, 0, 31)
		for range 6 {
			code = append(code,
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.STRUCT_SET),
			)
		}
		code = append(code, instr.New(instr.DROP))
		prog := program.New(code, program.WithTypes(types.TypeI32Array))
		i = New(prog, WithHeap(3), WithHeapLimit(3), WithConverter(reflect.TypeOf(hostRef(0)), conv))
		defer i.Close()
		host, err = NewHostObject(i, hostRefFields{})
		require.NoError(t, err)
		require.NoError(t, i.Push(host))
		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("preserves direct stored refs through threaded get", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
		})
		i := New(prog)
		defer i.Close()
		addr, err := i.Alloc(types.String("stored"))
		require.NoError(t, err)
		host, err := NewHostObject(i, struct{ Value types.Value }{Value: types.Ref(addr)})
		require.NoError(t, err)
		require.NoError(t, i.Push(host))

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.String("stored"), got)
		_, err = i.Load(addr)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})

	t.Run("transfers direct interface refs through threaded set", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.GLOBAL_GET, 1),
			instr.New(instr.STRUCT_SET),
		}, program.WithGlobals(types.TypeRef, types.TypeRef))
		i := New(prog)
		defer i.Close()
		child := &trackedValue{}
		childAddr, err := i.Alloc(child)
		require.NoError(t, err)
		parent := &trackedValue{refs: []types.Ref{types.Ref(childAddr)}}
		parentAddr, err := i.Alloc(parent)
		require.NoError(t, err)
		host, err := NewHostObject(i, struct{ Value types.Value }{Value: types.Ref(0)})
		require.NoError(t, err)
		hostAddr, err := i.Alloc(host)
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(0, types.BoxRef(hostAddr)))
		require.NoError(t, i.SetGlobal(1, types.BoxRef(parentAddr)))

		require.NoError(t, i.Run(context.Background()))
		null, err := i.Alloc(types.BoxedNull)
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(1, types.BoxRef(null)))
		require.Equal(t, types.Ref(parentAddr), host.Receiver.Elem().Field(0).Interface())
		_, err = i.Load(parentAddr)
		require.NoError(t, err)
		_, err = i.Load(childAddr)
		require.NoError(t, err)
		require.Zero(t, parent.closed)
		require.Zero(t, child.closed)

		null, err = i.Alloc(types.BoxedNull)
		require.NoError(t, err)
		require.NoError(t, i.SetGlobal(0, types.BoxRef(null)))
		_, err = i.Load(parentAddr)
		require.ErrorIs(t, err, ErrSegmentationFault)
		_, err = i.Load(childAddr)
		require.ErrorIs(t, err, ErrSegmentationFault)
		require.Equal(t, 1, parent.closed)
		require.Equal(t, 1, child.closed)
	})

	t.Run("preserves direct interface refs on invalid replacement", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		resource := &trackedValue{}
		addr, err := i.Alloc(resource)
		require.NoError(t, err)
		host, err := NewHostObject(i, struct{ Value types.Value }{Value: types.Ref(addr)})
		require.NoError(t, err)

		require.PanicsWithValue(t, ErrSegmentationFault, func() {
			host.SetField(0, types.BoxRef(addr+1))
		})
		require.Equal(t, types.Ref(addr), host.Receiver.Elem().Field(0).Interface())
		_, err = i.Load(addr)
		require.NoError(t, err)
		require.Zero(t, resource.closed)

		hostAddr, err := i.Alloc(host)
		require.NoError(t, err)
		require.NoError(t, i.Release(hostAddr))
		require.Equal(t, 1, resource.closed)
	})

	t.Run("retains bound methods through threaded get", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
		})
		i := New(prog)
		defer i.Close()
		host, err := NewHostObject(i, hostMethod{})
		require.NoError(t, err)
		require.NoError(t, i.Push(host))

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		fn := got.(*HostFunction)
		returns, err := fn.Fn(i, nil)
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(7)}, returns)
	})

	t.Run("releases direct ref reads", func(t *testing.T) {
		code := make([]instr.Instruction, 0, 25)
		for range 6 {
			code = append(code,
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
				instr.New(instr.DROP),
			)
		}
		code = append(code, instr.New(instr.DROP))
		i := New(program.New(code), WithHeap(3), WithHeapLimit(3))
		defer i.Close()
		host, err := NewHostObject(i, struct{ Text string }{Text: "value"})
		require.NoError(t, err)
		require.NoError(t, i.Push(host))

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("releases direct ref writes", func(t *testing.T) {
		code := make([]instr.Instruction, 0, 31)
		for range 6 {
			code = append(code,
				instr.New(instr.DUP),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_NEW_DEFAULT, 0),
				instr.New(instr.STRUCT_SET),
			)
		}
		code = append(code, instr.New(instr.DROP))
		prog := program.New(code, program.WithTypes(types.TypeI32Array))
		i := New(prog, WithHeap(4), WithHeapLimit(4))
		defer i.Close()
		host, err := NewHostObject(i, struct{ Value types.Value }{Value: types.Ref(0)})
		require.NoError(t, err)
		require.NoError(t, i.Push(host))

		require.NoError(t, i.Run(context.Background()))
	})

	t.Run("rolls back bound methods when the heap is full", func(t *testing.T) {
		i := New(program.New(nil), WithHeap(2), WithHeapLimit(2))
		defer i.Close()

		var err error
		require.NotPanics(t, func() {
			_, err = NewHostObject(i, hostFields{})
		})
		require.ErrorIs(t, err, ErrHeapExhausted)
		addr, err := i.Alloc(types.I32(1))
		require.NoError(t, err)
		require.NoError(t, i.Release(addr))
	})

	t.Run("rejects tampered layouts in threaded field access", func(t *testing.T) {
		for _, op := range []instr.Opcode{instr.STRUCT_GET, instr.STRUCT_SET} {
			code := []instr.Instruction{instr.New(instr.I32_CONST, 0)}
			if op == instr.STRUCT_SET {
				code = append(code, instr.New(instr.CONST_GET, 0))
			}
			code = append(code, instr.New(op))
			prog := program.New(code, program.WithConstants(types.String("value")))
			i := New(prog)
			host, err := NewHostObject(i, struct{ Count int32 }{Count: 3})
			require.NoError(t, err, op)
			host.Typ = types.NewStructType(types.NewStructField(types.TypeString))
			require.NoError(t, i.Push(host), op)

			require.ErrorIs(t, i.Run(context.Background()), ErrSegmentationFault, op)
			require.Equal(t, int64(3), host.Receiver.Elem().Field(0).Int(), op)
			require.NoError(t, i.Close(), op)
		}
	})

	t.Run("rejects nil and unsupported receivers", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := NewHostObject(nil, hostFields{})
		require.ErrorIs(t, err, ErrTypeMismatch)
		_, err = NewHostObject(i, nil)
		require.ErrorIs(t, err, ErrTypeMismatch)
		_, err = NewHostObject(i, []int{1})
		require.ErrorIs(t, err, ErrUnsupportedMarshalType)
		require.False(t, errors.Is(err, ErrTypeMismatch))
	})

	t.Run("rejects replaced public layout safely", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		host, err := NewHostObject(i, hostFields{Count: 1})
		require.NoError(t, err)
		host.Typ = types.NewStructType()
		require.NotPanics(t, func() { host.Field(0) })
		host.Receiver = reflect.Value{}

		require.NotPanics(t, func() { host.Field(0) })
		require.NotPanics(t, func() { host.SetField(0, types.BoxI32(2)) })
		require.Equal(t, "host<>", host.String())
	})

	t.Run("releases bound methods after public-field tampering", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		host, err := NewHostObject(i, hostFields{})
		require.NoError(t, err)
		bump := host.Field(host.Typ.FieldIndex("Bump")).Ref()
		context := host.Field(host.Typ.FieldIndex("Context")).Ref()
		touch := host.Field(host.Typ.FieldIndex("Touch")).Ref()
		addr, err := i.Alloc(host)
		require.NoError(t, err)
		host.Typ = types.NewStructType()
		host.Receiver = reflect.Value{}

		require.NoError(t, i.Release(addr))
		_, err = i.Load(bump)
		require.ErrorIs(t, err, ErrSegmentationFault)
		_, err = i.Load(context)
		require.ErrorIs(t, err, ErrSegmentationFault)
		_, err = i.Load(touch)
		require.ErrorIs(t, err, ErrSegmentationFault)
	})
}

func TestHostFunction_Kind(t *testing.T) {
	fn := NewHostFunction(&types.FunctionType{}, nil)
	require.Equal(t, types.KindRef, fn.Kind())
}

func TestHostFunction_Type(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	fn := NewHostFunction(typ, nil)
	require.Same(t, typ, fn.Type())
}

func TestHostFunction_String(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	fn := NewHostFunction(typ, nil)
	require.Equal(t, "func() i32\n<native>", fn.String())
}

func TestHostObject_Kind(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()
	value, err := i.Marshal(hostFields{Count: 1})
	require.NoError(t, err)
	host := value.(*HostObject)

	require.Equal(t, types.KindRef, host.Kind())
}

func TestHostObject_Type(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()
	value, err := i.Marshal(hostFields{Count: 1})
	require.NoError(t, err)
	host := value.(*HostObject)

	require.Same(t, host.Typ, host.Type())
}

func TestHostObject_String(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()
	value, err := i.Marshal(hostFields{Count: 1})
	require.NoError(t, err)
	host := value.(*HostObject)

	require.Equal(t, "host<interp.hostFields>", host.String())
	require.Equal(t, "host<>", (&HostObject{}).String())
}

func TestHostObject_Refs(t *testing.T) {
	t.Run("bound methods", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		value, err := i.Marshal(hostFields{Count: 1})
		require.NoError(t, err)
		host := value.(*HostObject)
		bump := host.Field(host.Typ.FieldIndex("Bump"))
		context := host.Field(host.Typ.FieldIndex("Context"))
		touch := host.Field(host.Typ.FieldIndex("Touch"))

		require.Equal(t, []types.Ref{9, types.Ref(bump.Ref()), types.Ref(context.Ref()), types.Ref(touch.Ref())}, host.Refs([]types.Ref{9}))
	})

	t.Run("no methods", func(t *testing.T) {
		type private struct {
			Visible int32
			hidden  int32
		}
		i := New(program.New(nil))
		defer i.Close()
		value, err := i.Marshal(private{Visible: 1, hidden: 2})
		require.NoError(t, err)
		host, ok := value.(*HostObject)
		require.True(t, ok)

		refs := []types.Ref{9}
		allocs := testing.AllocsPerRun(100, func() {
			refs = host.Refs(refs[:1])
		})
		require.Equal(t, []types.Ref{9}, refs)
		require.Zero(t, allocs)
	})
}

func TestHostObject_Field(t *testing.T) {
	t.Run("data and method fields", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		value, err := i.Marshal(hostFields{Count: 1})
		require.NoError(t, err)
		host := value.(*HostObject)

		require.Equal(t, types.BoxI32(1), host.Field(host.Typ.FieldIndex("Count")))
		method := host.Field(host.Typ.FieldIndex("Bump"))
		require.Equal(t, types.KindRef, method.Kind())
		require.Zero(t, host.Field(-1))
		require.Zero(t, host.Field(len(host.Typ.Fields)))

		loaded, err := i.Load(method.Ref())
		require.NoError(t, err)
		fn := loaded.(*HostFunction)
		returns, err := fn.Fn(i, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(5)}, returns)
		require.Equal(t, types.BoxI32(5), host.Field(host.Typ.FieldIndex("Count")))
	})

	t.Run("supported Go fields", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		value, err := i.Marshal(hostFields{
			Bool: true, I8: -8, I16: -16, I32: -32, I: -64, I64: -128,
			U8: 8, U16: 16, U32: 32, U: 64, U64: 128, Uintpt: 256,
			F32: 1.25, F64: 2.5, Text: "go", Value: types.I32(7),
		})
		require.NoError(t, err)
		host := value.(*HostObject)

		require.Equal(t, types.BoxedTrue, host.Field(host.Typ.FieldIndex("Bool")))
		require.Equal(t, types.BoxI32(-8), host.Field(host.Typ.FieldIndex("I8")))
		require.Equal(t, types.BoxI32(-16), host.Field(host.Typ.FieldIndex("I16")))
		require.Equal(t, types.BoxI32(-32), host.Field(host.Typ.FieldIndex("I32")))
		require.Equal(t, types.BoxI64(-64), host.Field(host.Typ.FieldIndex("I")))
		require.Equal(t, types.BoxI64(-128), host.Field(host.Typ.FieldIndex("I64")))
		require.Equal(t, types.BoxI32(8), host.Field(host.Typ.FieldIndex("U8")))
		require.Equal(t, types.BoxI32(16), host.Field(host.Typ.FieldIndex("U16")))
		require.Equal(t, types.BoxI32(32), host.Field(host.Typ.FieldIndex("U32")))
		require.Equal(t, types.BoxI64(64), host.Field(host.Typ.FieldIndex("U")))
		require.Equal(t, types.BoxI64(128), host.Field(host.Typ.FieldIndex("U64")))
		require.Equal(t, types.BoxI64(256), host.Field(host.Typ.FieldIndex("Uintpt")))
		require.Equal(t, types.BoxF32(1.25), host.Field(host.Typ.FieldIndex("F32")))
		require.Equal(t, types.BoxF64(2.5), host.Field(host.Typ.FieldIndex("F64")))
		text, err := i.Load(host.Field(host.Typ.FieldIndex("Text")).Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("go"), text)
		require.Equal(t, types.BoxI32(7), host.Field(host.Typ.FieldIndex("Value")))
	})

	t.Run("named scalar receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		id := hostUserID(41)
		value, err := i.Marshal(&id)
		require.NoError(t, err)
		host := value.(*HostObject)

		require.Equal(t, types.BoxI64(41), host.Field(host.Typ.FieldIndex("Value")))
		require.NotEqual(t, -1, host.Typ.FieldIndex("Bump"))
		require.NotEqual(t, -1, host.Typ.FieldIndex("Next"))
	})
}

func TestHostObject_SetField(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()
	value, err := i.Marshal(hostFields{I32: 1, F64: 2, Value: types.I32(3)})
	require.NoError(t, err)
	host := value.(*HostObject)

	host.SetField(host.Typ.FieldIndex("Bool"), types.BoxedTrue)
	host.SetField(host.Typ.FieldIndex("I32"), types.BoxI32(20))
	host.SetField(host.Typ.FieldIndex("I64"), types.BoxI64(22))
	host.SetField(host.Typ.FieldIndex("U32"), types.BoxI32(23))
	host.SetField(host.Typ.FieldIndex("U64"), types.BoxI64(25))
	host.SetField(host.Typ.FieldIndex("F32"), types.BoxF32(3.5))
	host.SetField(host.Typ.FieldIndex("F64"), types.BoxF64(4.5))
	host.SetField(host.Typ.FieldIndex("Text"), i.box(types.String("vm")))
	host.SetField(host.Typ.FieldIndex("Value"), types.BoxI32(11))
	method := host.Field(host.Typ.FieldIndex("Touch"))
	host.SetField(host.Typ.FieldIndex("Touch"), types.BoxI32(99))
	host.SetField(-1, types.BoxI32(99))

	require.Equal(t, types.BoxedTrue, host.Field(host.Typ.FieldIndex("Bool")))
	require.Equal(t, types.BoxI32(20), host.Field(host.Typ.FieldIndex("I32")))
	require.Equal(t, types.BoxI64(22), host.Field(host.Typ.FieldIndex("I64")))
	require.Equal(t, types.BoxI32(23), host.Field(host.Typ.FieldIndex("U32")))
	require.Equal(t, types.BoxI64(25), host.Field(host.Typ.FieldIndex("U64")))
	require.Equal(t, types.BoxF32(3.5), host.Field(host.Typ.FieldIndex("F32")))
	require.Equal(t, types.BoxF64(4.5), host.Field(host.Typ.FieldIndex("F64")))
	require.Equal(t, types.BoxI32(11), host.Field(host.Typ.FieldIndex("Value")))
	require.Equal(t, method, host.Field(host.Typ.FieldIndex("Touch")))
}

func TestHostObject_Raw(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()
	value, err := i.Marshal(hostFields{I: -1, I64: -2, U: 3, U64: 4, Uintpt: 5})
	require.NoError(t, err)
	host := value.(*HostObject)

	require.Equal(t, uint64(^uint(0)), host.Raw(host.Typ.FieldIndex("I")))
	require.Equal(t, uint64(^uint64(1)), host.Raw(host.Typ.FieldIndex("I64")))
	require.Equal(t, uint64(3), host.Raw(host.Typ.FieldIndex("U")))
	require.Equal(t, uint64(4), host.Raw(host.Typ.FieldIndex("U64")))
	require.Equal(t, uint64(5), host.Raw(host.Typ.FieldIndex("Uintpt")))
	method := host.Field(host.Typ.FieldIndex("Touch"))
	require.Equal(t, uint64(method), host.Raw(host.Typ.FieldIndex("Touch")))
	require.Zero(t, host.Raw(-1))
}

func TestHostObject_SetRaw(t *testing.T) {
	i := New(program.New(nil))
	defer i.Close()
	value, err := i.Marshal(hostFields{})
	require.NoError(t, err)
	host := value.(*HostObject)

	host.SetRaw(host.Typ.FieldIndex("I"), 88)
	host.SetRaw(host.Typ.FieldIndex("I64"), 99)
	host.SetRaw(host.Typ.FieldIndex("U"), 100)
	host.SetRaw(host.Typ.FieldIndex("U64"), 101)
	host.SetRaw(host.Typ.FieldIndex("Uintpt"), 102)
	method := host.Field(host.Typ.FieldIndex("Touch"))
	host.SetRaw(host.Typ.FieldIndex("Touch"), uint64(types.BoxI32(7)))
	host.SetRaw(-1, 7)

	require.Equal(t, uint64(88), host.Raw(host.Typ.FieldIndex("I")))
	require.Equal(t, uint64(99), host.Raw(host.Typ.FieldIndex("I64")))
	require.Equal(t, uint64(100), host.Raw(host.Typ.FieldIndex("U")))
	require.Equal(t, uint64(101), host.Raw(host.Typ.FieldIndex("U64")))
	require.Equal(t, uint64(102), host.Raw(host.Typ.FieldIndex("Uintpt")))
	require.Equal(t, method, host.Field(host.Typ.FieldIndex("Touch")))
}
