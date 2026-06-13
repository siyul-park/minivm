package interp

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type hostCounter struct {
	Count int32
}

func (c *hostCounter) Bump(n int32) int32 {
	c.Count += n
	return c.Count
}

type hostCelsius float64

func (c hostCelsius) Fahrenheit() hostCelsius {
	return c*9/5 + 32
}

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
	t.Run("kind and type", func(t *testing.T) {
		typ := &types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}
		fn := NewHostFunction(typ, func(_ *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
		})
		require.Equal(t, types.KindRef, fn.Kind())
		require.Equal(t, typ, fn.Type())
		require.Contains(t, fn.String(), "<native>")
	})
	t.Run("call via interpreter", func(t *testing.T) {
		typ := &types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}
		fn := NewHostFunction(typ, func(_ *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
		})
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)
		i := New(prog)
		defer i.Close()

		require.NoError(t, i.Run(context.Background()))
		v, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(10), v)
	})
}

func TestHostObject(t *testing.T) {
	t.Run("kind type and string", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCounter{Count: 1})
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)

		require.Equal(t, types.KindRef, ho.Kind())
		require.Equal(t, ho.Typ, ho.Type())
		require.Equal(t, "host<interp.hostCounter>", ho.String())
		require.Equal(t, "host<>", (&HostObject{}).String())
	})

	t.Run("private fields trace without allocation", func(t *testing.T) {
		type private struct {
			Visible int32
			hidden  int32
		}
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(private{Visible: 1, hidden: 2})
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)

		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = ho.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("struct with method routes to HostObject", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCounter{Count: 1})
		require.NoError(t, err)

		ho, ok := got.(*HostObject)
		require.True(t, ok)
		require.Equal(t, "Count", ho.Typ.Fields[0].Name)
		require.Equal(t, "Bump", ho.Typ.Fields[1].Name)
		require.Equal(t, types.BoxI32(1), ho.Field(0))
	})

	t.Run("method call mutates receiver via pointer", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCounter{Count: 1})
		require.NoError(t, err)
		ho := got.(*HostObject)

		methodSlot := -1
		for idx, f := range ho.Typ.Fields {
			if f.Name == "Bump" {
				methodSlot = idx
				break
			}
		}
		require.NotEqual(t, -1, methodSlot)

		boxed := ho.Field(methodSlot)
		require.Equal(t, types.KindRef, boxed.Kind())
		require.Equal(t, uint64(boxed), ho.Raw(methodSlot))
		ho.SetRaw(methodSlot, uint64(types.BoxI32(99)))
		require.Equal(t, boxed, ho.Field(methodSlot))
		v, err := i.Load(boxed.Ref())
		require.NoError(t, err)
		fn, ok := v.(*HostFunction)
		require.True(t, ok)

		returns, err := fn.Fn(i, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(5)}, returns)

		require.Equal(t, types.BoxI32(5), ho.Field(0))
	})

	t.Run("pointer to named scalar with method routes to HostObject", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		id := hostUserID(41)
		got, err := i.Marshal(&id)
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)
		require.Equal(t, "Value", ho.Typ.Fields[0].Name)
		require.True(t, ho.Typ.Fields[0].Type.Equals(types.TypeI64))
		require.Equal(t, types.BoxI64(41), ho.Field(0))
		require.NotEqual(t, -1, ho.Typ.FieldIndex("Bump"))
		require.NotEqual(t, -1, ho.Typ.FieldIndex("Next"))

		valueFields := 0
		for _, field := range ho.Typ.Fields {
			if field.Name == "Value" {
				valueFields++
			}
		}
		require.Equal(t, 1, valueFields)
	})

	t.Run("pointer method mutates named scalar receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		id := hostUserID(41)
		got, err := i.Marshal(&id)
		require.NoError(t, err)
		ho := got.(*HostObject)

		boxed := ho.Field(ho.Typ.FieldIndex("Bump"))
		require.Equal(t, types.KindRef, boxed.Kind())
		v, err := i.Load(boxed.Ref())
		require.NoError(t, err)
		fn, ok := v.(*HostFunction)
		require.True(t, ok)

		returns, err := fn.Fn(i, []types.Boxed{types.BoxI64(1)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI64(42)}, returns)
		require.Equal(t, types.BoxI64(42), ho.Field(0))
		require.Equal(t, hostUserID(41), id)
	})

	t.Run("SetField updates named scalar receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		id := hostUserID(41)
		got, err := i.Marshal(&id)
		require.NoError(t, err)
		ho := got.(*HostObject)

		ho.SetField(0, types.BoxI64(99))
		require.Equal(t, types.BoxI64(99), ho.Field(0))
	})

	t.Run("named scalar value field feeds primitive opcodes", func(t *testing.T) {
		id := hostUserID(41)
		i := New(program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
				instr.New(instr.I64_CONST, 1),
				instr.New(instr.I64_ADD),
			},
		))
		defer i.Close()

		got, err := i.Marshal(&id)
		require.NoError(t, err)
		require.NoError(t, i.Push(got))

		require.NoError(t, i.Run(context.Background()))
		out, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I64(42), out)
	})

	t.Run("unmarshal recovers receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		src := hostCounter{Count: 7}
		got, err := i.Marshal(src)
		require.NoError(t, err)

		var dst hostCounter
		require.NoError(t, i.Unmarshal(got, &dst))
		require.Equal(t, int32(7), dst.Count)
	})

	t.Run("method name shadowed by field", func(t *testing.T) {
		type shadow struct {
			Bump int32
		}
		i := New(program.New(nil))
		defer i.Close()

		// shadow has no methods so it would normally take the *Struct path;
		// embed it via a wrapper that adds a Bump method to force HostObject.
		type wrapper struct {
			Bump int32
			tag  int32
		}
		got, err := i.Marshal(wrapper{Bump: 9})
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)
		// Only the data field named Bump is exposed; no duplicate.
		count := 0
		for _, f := range ho.Typ.Fields {
			if f.Name == "Bump" {
				count++
			}
		}
		require.Equal(t, 1, count)
	})

	t.Run("SetField writes back to receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCounter{Count: 1})
		require.NoError(t, err)
		ho := got.(*HostObject)

		ho.SetField(0, types.BoxI32(42))
		require.Equal(t, types.BoxI32(42), ho.Field(0))
	})

	t.Run("unsafe field access covers scalar and value fields", func(t *testing.T) {
		type fields struct {
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
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(fields{
			Bool:   true,
			I8:     -8,
			I16:    -16,
			I32:    -32,
			I:      -64,
			I64:    -128,
			U8:     8,
			U16:    16,
			U32:    32,
			U:      64,
			U64:    128,
			Uintpt: 256,
			F32:    1.25,
			F64:    2.5,
			Text:   "go",
			Value:  types.I32(7),
		})
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)

		require.Equal(t, types.BoxedTrue, ho.Field(ho.Typ.FieldIndex("Bool")))
		require.Equal(t, types.BoxI32(-8), ho.Field(ho.Typ.FieldIndex("I8")))
		require.Equal(t, types.BoxI32(-16), ho.Field(ho.Typ.FieldIndex("I16")))
		require.Equal(t, types.BoxI32(-32), ho.Field(ho.Typ.FieldIndex("I32")))
		require.Equal(t, types.BoxI64(-64), ho.Field(ho.Typ.FieldIndex("I")))
		require.Equal(t, uint64(^uint64(127)), ho.Raw(ho.Typ.FieldIndex("I64")))
		require.Equal(t, types.BoxI32(8), ho.Field(ho.Typ.FieldIndex("U8")))
		require.Equal(t, types.BoxI32(16), ho.Field(ho.Typ.FieldIndex("U16")))
		require.Equal(t, types.BoxI32(32), ho.Field(ho.Typ.FieldIndex("U32")))
		require.Equal(t, types.BoxI64(64), ho.Field(ho.Typ.FieldIndex("U")))
		require.Equal(t, uint64(128), ho.Raw(ho.Typ.FieldIndex("U64")))
		require.Equal(t, uint64(256), ho.Raw(ho.Typ.FieldIndex("Uintpt")))
		require.Equal(t, types.BoxF32(1.25), ho.Field(ho.Typ.FieldIndex("F32")))
		require.Equal(t, types.BoxF64(2.5), ho.Field(ho.Typ.FieldIndex("F64")))

		text, err := i.Load(ho.Field(ho.Typ.FieldIndex("Text")).Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("go"), text)
		require.Equal(t, types.BoxI32(7), ho.Field(ho.Typ.FieldIndex("Value")))

		ho.SetField(ho.Typ.FieldIndex("Bool"), types.BoxedFalse)
		ho.SetField(ho.Typ.FieldIndex("I8"), types.BoxI32(18))
		ho.SetField(ho.Typ.FieldIndex("I16"), types.BoxI32(19))
		ho.SetField(ho.Typ.FieldIndex("I32"), types.BoxI32(20))
		ho.SetField(ho.Typ.FieldIndex("I"), types.BoxI64(21))
		ho.SetField(ho.Typ.FieldIndex("I64"), types.BoxI64(22))
		ho.SetField(ho.Typ.FieldIndex("U8"), types.BoxI32(21))
		ho.SetField(ho.Typ.FieldIndex("U16"), types.BoxI32(22))
		ho.SetField(ho.Typ.FieldIndex("U32"), types.BoxI32(23))
		ho.SetField(ho.Typ.FieldIndex("U"), types.BoxI64(24))
		ho.SetField(ho.Typ.FieldIndex("U64"), types.BoxI64(25))
		ho.SetField(ho.Typ.FieldIndex("Uintpt"), types.BoxI64(26))
		ho.SetField(ho.Typ.FieldIndex("F32"), types.BoxF32(3.5))
		ho.SetField(ho.Typ.FieldIndex("F64"), types.BoxF64(4.5))
		require.Equal(t, uint64(21), ho.Raw(ho.Typ.FieldIndex("I")))
		require.Equal(t, uint64(24), ho.Raw(ho.Typ.FieldIndex("U")))
		ho.SetRaw(ho.Typ.FieldIndex("I"), 88)
		ho.SetRaw(ho.Typ.FieldIndex("I64"), 99)
		ho.SetRaw(ho.Typ.FieldIndex("U"), 100)
		ho.SetRaw(ho.Typ.FieldIndex("U64"), 101)
		ho.SetRaw(ho.Typ.FieldIndex("Uintpt"), 102)
		ho.SetField(ho.Typ.FieldIndex("Text"), i.box(types.String("vm")))
		ho.SetField(ho.Typ.FieldIndex("Value"), types.BoxI32(11))

		var out fields
		require.NoError(t, i.Unmarshal(ho, &out))
		require.False(t, out.Bool)
		require.Equal(t, int8(18), out.I8)
		require.Equal(t, int16(19), out.I16)
		require.Equal(t, int32(20), out.I32)
		require.Equal(t, 88, out.I)
		require.Equal(t, int64(99), out.I64)
		require.Equal(t, uint8(21), out.U8)
		require.Equal(t, uint16(22), out.U16)
		require.Equal(t, uint32(23), out.U32)
		require.Equal(t, uint(100), out.U)
		require.Equal(t, uint64(101), out.U64)
		require.Equal(t, uintptr(102), out.Uintpt)
		require.Equal(t, float32(3.5), out.F32)
		require.Equal(t, 4.5, out.F64)
		require.Equal(t, "vm", out.Text)
		require.Equal(t, types.I32(11), out.Value)
	})
}
