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
		v, err := i.Load(boxed.Ref())
		require.NoError(t, err)
		fn, ok := v.(*HostFunction)
		require.True(t, ok)

		returns, err := fn.Fn(i, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(5)}, returns)

		require.Equal(t, types.BoxI32(5), ho.Field(0))
	})

	t.Run("non-struct named scalar with method", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		got, err := i.Marshal(hostCelsius(100))
		require.NoError(t, err)
		ho, ok := got.(*HostObject)
		require.True(t, ok)
		require.Len(t, ho.Typ.Fields, 1)
		require.Equal(t, "Fahrenheit", ho.Typ.Fields[0].Name)
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

		require.Equal(t, types.BoxedTrue, ho.Field(hostFieldIndex(t, ho, "Bool")))
		require.Equal(t, types.BoxI32(-8), ho.Field(hostFieldIndex(t, ho, "I8")))
		require.Equal(t, types.BoxI32(-16), ho.Field(hostFieldIndex(t, ho, "I16")))
		require.Equal(t, types.BoxI32(-32), ho.Field(hostFieldIndex(t, ho, "I32")))
		require.Equal(t, types.BoxI64(-64), ho.Field(hostFieldIndex(t, ho, "I")))
		require.Equal(t, uint64(^uint64(127)), ho.Raw(hostFieldIndex(t, ho, "I64")))
		require.Equal(t, types.BoxI32(8), ho.Field(hostFieldIndex(t, ho, "U8")))
		require.Equal(t, types.BoxI32(16), ho.Field(hostFieldIndex(t, ho, "U16")))
		require.Equal(t, types.BoxI32(32), ho.Field(hostFieldIndex(t, ho, "U32")))
		require.Equal(t, types.BoxI64(64), ho.Field(hostFieldIndex(t, ho, "U")))
		require.Equal(t, uint64(128), ho.Raw(hostFieldIndex(t, ho, "U64")))
		require.Equal(t, uint64(256), ho.Raw(hostFieldIndex(t, ho, "Uintpt")))
		require.Equal(t, types.BoxF32(1.25), ho.Field(hostFieldIndex(t, ho, "F32")))
		require.Equal(t, types.BoxF64(2.5), ho.Field(hostFieldIndex(t, ho, "F64")))

		text, err := i.Load(ho.Field(hostFieldIndex(t, ho, "Text")).Ref())
		require.NoError(t, err)
		require.Equal(t, types.String("go"), text)
		require.Equal(t, types.BoxI32(7), ho.Field(hostFieldIndex(t, ho, "Value")))

		ho.SetField(hostFieldIndex(t, ho, "Bool"), types.BoxedFalse)
		ho.SetRaw(hostFieldIndex(t, ho, "I64"), 99)
		ho.SetField(hostFieldIndex(t, ho, "Text"), i.box(types.String("vm")))
		ho.SetField(hostFieldIndex(t, ho, "Value"), types.BoxI32(11))

		var out fields
		require.NoError(t, i.Unmarshal(ho, &out))
		require.False(t, out.Bool)
		require.Equal(t, int64(99), out.I64)
		require.Equal(t, "vm", out.Text)
		require.Equal(t, types.I32(11), out.Value)
	})
}

func hostFieldIndex(t *testing.T, ho *HostObject, name string) int {
	t.Helper()
	for idx, field := range ho.Typ.Fields {
		if field.Name == name {
			return idx
		}
	}
	t.Fatalf("missing host field %s", name)
	return -1
}
