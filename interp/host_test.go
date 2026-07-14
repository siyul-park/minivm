package interp

import (
	"context"
	"testing"

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
