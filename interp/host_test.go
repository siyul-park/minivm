package interp_test

import (
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// hostCounter carries unexported state next to an exported field, so a copy of
// it cannot reproduce the whole value and the codec exposes a live view.
type hostCounter struct {
	Count  int32
	offset int32
}

func (c *hostCounter) Bump(n int32) int32 {
	c.Count += n + c.offset
	return c.Count
}

func TestNewHostFunction(t *testing.T) {
	t.Run("constructor", func(t *testing.T) {
		typ := &types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}
		fn := interp.NewHostFunction(typ, func(_ *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
			return []types.Boxed{types.BoxI32(params[0].I32() * 2)}, nil
		})

		require.Same(t, typ, fn.Typ)
		got, err := fn.Fn(nil, []types.Boxed{types.BoxI32(4)})
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(8)}, got)
	})

	t.Run("public fields", func(t *testing.T) {
		typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
		fn := &interp.HostFunction{
			Typ: typ,
			Fn: func(*interp.Interpreter, []types.Boxed) ([]types.Boxed, error) {
				return []types.Boxed{types.BoxI32(7)}, nil
			},
		}

		require.Same(t, typ, fn.Typ)
		got, err := fn.Fn(nil, nil)
		require.NoError(t, err)
		require.Equal(t, []types.Boxed{types.BoxI32(7)}, got)
	})
}

func TestHostFunction_Kind(t *testing.T) {
	fn := interp.NewHostFunction(&types.FunctionType{}, nil)
	require.Equal(t, types.KindRef, fn.Kind())
}

func TestHostFunction_Type(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	fn := interp.NewHostFunction(typ, nil)
	require.Same(t, typ, fn.Type())
}

func TestHostFunction_String(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	fn := interp.NewHostFunction(typ, nil)
	require.Equal(t, "func() i32\n<native>", fn.String())
}

func TestHostStruct_Kind(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, &hostCounter{})
	require.NoError(t, err)
	require.Equal(t, types.KindRef, value.(*interp.HostStruct).Kind())
}

func TestHostStruct_Type(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, &hostCounter{})
	require.NoError(t, err)

	// The view reports the type a copy of the same struct would have reported,
	// so guest code that reads it needs to know nothing about the Go side.
	typ, ok := value.(*interp.HostStruct).Type().(*types.StructType)
	require.True(t, ok)
	require.Len(t, typ.Fields, 1)
	require.Equal(t, "Count", typ.Fields[0].Name)
	require.Equal(t, types.TypeI32, typ.Fields[0].Type)
}

func TestHostStruct_String(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, &hostCounter{})
	require.NoError(t, err)
	require.Equal(t, "struct {i32}\n<native>", value.(*interp.HostStruct).String())
}

func TestHostStruct_Field(t *testing.T) {
	t.Run("reads through to the Go value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := &hostCounter{Count: 7}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		host := value.(*interp.HostStruct)

		got, err := host.Field(i, 0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(7), got)

		// A copy would have frozen the 7. A view reports what the Go value
		// holds now, which is the whole point of holding one.
		src.Count = 9
		got, err = host.Field(i, 0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(9), got)
	})

	t.Run("an index outside the layout faults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, &hostCounter{})
		require.NoError(t, err)
		host := value.(*interp.HostStruct)

		_, err = host.Field(i, 1)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
		_, err = host.Field(i, -1)
		require.ErrorIs(t, err, interp.ErrSegmentationFault)
	})
}

func TestHostStruct_SetField(t *testing.T) {
	t.Run("writes through to the Go value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := &hostCounter{}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		host := value.(*interp.HostStruct)

		require.NoError(t, host.SetField(i, 0, types.BoxI32(5)))
		require.Equal(t, int32(5), src.Count)

		// The write reached the Go value, so a method reading private state
		// alongside it agrees with what the guest just wrote.
		require.Equal(t, int32(5), src.Bump(0))
	})

	t.Run("an index outside the layout faults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, &hostCounter{})
		require.NoError(t, err)

		require.ErrorIs(t, value.(*interp.HostStruct).SetField(i, 1, types.BoxI32(5)), interp.ErrSegmentationFault)
	})
}
