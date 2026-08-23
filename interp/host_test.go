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
	require.Equal(t, "struct {Count: i32}\n<native>", value.(*interp.HostStruct).String())
}

func TestHostStruct_SetField(t *testing.T) {
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

	t.Run("reading an index outside the layout faults", func(t *testing.T) {
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

	t.Run("writing an index outside the layout faults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, &hostCounter{})
		require.NoError(t, err)

		require.ErrorIs(t, value.(*interp.HostStruct).SetField(i, 1, types.BoxI32(5)), interp.ErrSegmentationFault)
	})
}

func TestHostArray_Kind(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, []int32{1})
	require.NoError(t, err)
	require.Equal(t, types.KindRef, value.(*interp.HostArray).Kind())
}

func TestHostArray_Type(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, []int32{1})
	require.NoError(t, err)
	require.True(t, value.(*interp.HostArray).Type().Equals(types.NewArrayType(types.TypeI32)))
}

func TestHostArray_String(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, []int32{1})
	require.NoError(t, err)
	require.Equal(t, "[]i32\n<native>", value.(*interp.HostArray).String())
}

func TestHostArray_Len(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()
	src := []int32{1, 2}

	value, err := r.Marshal(i, &src)
	require.NoError(t, err)
	host := value.(*interp.HostArray)
	require.Equal(t, 2, host.Len())

	// A copy would have frozen the length the slice had at marshal time.
	src = append(src, 3)
	require.Equal(t, 3, host.Len())
}

func TestHostArray_SetElement(t *testing.T) {
	t.Run("reads through to the Go value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := []int32{7}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		host := value.(*interp.HostArray)

		got, err := host.Element(i, 0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(7), got)

		src[0] = 9
		got, err = host.Element(i, 0)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(9), got)
	})

	t.Run("reading an index outside the length faults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, []int32{7})
		require.NoError(t, err)
		host := value.(*interp.HostArray)

		_, err = host.Element(i, 1)
		require.ErrorIs(t, err, interp.ErrIndexOutOfRange)
		_, err = host.Element(i, -1)
		require.ErrorIs(t, err, interp.ErrIndexOutOfRange)
	})

	t.Run("writes through to the Go value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := []int32{0}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)

		require.NoError(t, value.(*interp.HostArray).SetElement(i, 0, types.BoxI32(5)))
		require.Equal(t, []int32{5}, src)
	})

	t.Run("writing an index outside the length faults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, []int32{0})
		require.NoError(t, err)

		require.ErrorIs(t, value.(*interp.HostArray).SetElement(i, 1, types.BoxI32(5)), interp.ErrIndexOutOfRange)
	})
}

func TestHostArray_Fill(t *testing.T) {
	t.Run("writes every slot of the range", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := []int32{0, 0, 0}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)

		require.NoError(t, value.(*interp.HostArray).Fill(i, 1, 2, types.BoxI32(5)))
		require.Equal(t, []int32{0, 5, 5}, src)
	})

	t.Run("a range outside the length faults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, []int32{0})
		require.NoError(t, err)

		require.ErrorIs(t, value.(*interp.HostArray).Fill(i, 0, 2, types.BoxI32(5)), interp.ErrIndexOutOfRange)
	})
}

func TestHostArray_Append(t *testing.T) {
	t.Run("growth reaches the Go slice", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := []int32{1}

		value, err := r.Marshal(i, &src)
		require.NoError(t, err)

		// The view addresses the slice variable, so the Go side sees the growth
		// even though append moved the elements somewhere else.
		require.NoError(t, value.(*interp.HostArray).Append(i, []types.Boxed{types.BoxI32(2), types.BoxI32(3)}))
		require.Equal(t, []int32{1, 2, 3}, src)
	})

	t.Run("a Go array cannot grow", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := [1]int32{1}

		value, err := r.Marshal(i, &src)
		require.NoError(t, err)

		err = value.(*interp.HostArray).Append(i, []types.Boxed{types.BoxI32(2)})
		require.ErrorIs(t, err, interp.ErrUnsupportedMarshalType)
	})
}

func TestHostArray_Delete(t *testing.T) {
	t.Run("removes the element and shortens the Go slice", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := []int32{1, 2, 3}

		value, err := r.Marshal(i, &src)
		require.NoError(t, err)

		removed, err := value.(*interp.HostArray).Delete(i, 1)
		require.NoError(t, err)
		require.Equal(t, types.BoxI32(2), removed)
		require.Equal(t, []int32{1, 3}, src)
	})

	t.Run("an index outside the length faults", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := []int32{1}

		value, err := r.Marshal(i, &src)
		require.NoError(t, err)

		_, err = value.(*interp.HostArray).Delete(i, 1)
		require.ErrorIs(t, err, interp.ErrIndexOutOfRange)
	})

	t.Run("a Go array cannot shrink", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := [1]int32{1}

		value, err := r.Marshal(i, &src)
		require.NoError(t, err)

		_, err = value.(*interp.HostArray).Delete(i, 0)
		require.ErrorIs(t, err, interp.ErrUnsupportedMarshalType)
	})
}

func TestHostArray_Array(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()
	src := []int32{1, 2}

	value, err := r.Marshal(i, &src)
	require.NoError(t, err)

	// The copy is VM-owned, so a later write to the Go slice leaves it alone.
	out, err := value.(*interp.HostArray).Array(i)
	require.NoError(t, err)
	require.Equal(t, types.TypedArray[int32]{1, 2}, out)

	src[0] = 9
	require.Equal(t, types.TypedArray[int32]{1, 2}, out)
}

func TestHostMap_Kind(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, map[int32]int32{})
	require.NoError(t, err)
	require.Equal(t, types.KindRef, value.(*interp.HostMap).Kind())
}

func TestHostMap_Type(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, map[int32]int32{})
	require.NoError(t, err)
	require.True(t, value.(*interp.HostMap).Type().Equals(types.NewMapType(types.TypeI32, types.TypeI32)))
}

func TestHostMap_String(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()

	value, err := r.Marshal(i, map[int32]int32{})
	require.NoError(t, err)
	require.Equal(t, "map[i32]i32\n<native>", value.(*interp.HostMap).String())
}

func TestHostMap_Len(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()
	src := map[int32]int32{1: 7}

	value, err := r.Marshal(i, src)
	require.NoError(t, err)
	host := value.(*interp.HostMap)
	require.Equal(t, 1, host.Len())

	src[2] = 8
	require.Equal(t, 2, host.Len())
}

func TestHostMap_Set(t *testing.T) {
	t.Run("reads through to the Go value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := map[int32]int32{1: 7}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		host := value.(*interp.HostMap)

		got, ok, err := host.Get(i, types.BoxI32(1))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), got)

		src[1] = 9
		got, ok, err = host.Get(i, types.BoxI32(1))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(9), got)
	})

	t.Run("a missing key reads as the element zero", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		value, err := r.Marshal(i, map[int32]int32{})
		require.NoError(t, err)

		got, ok, err := value.(*interp.HostMap).Get(i, types.BoxI32(1))
		require.NoError(t, err)
		require.False(t, ok)
		require.Equal(t, types.BoxI32(0), got)
	})

	t.Run("a dynamic key finds what the Go side stored", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()

		// A dynamic key has no Go type of its own, so it decodes to the one the
		// VM's own key normalization names for its kind.
		value, err := r.Marshal(i, map[any]int32{int32(1): 7})
		require.NoError(t, err)

		got, ok, err := value.(*interp.HostMap).Get(i, types.BoxI32(1))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), got)
	})

	t.Run("writes through to the Go value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := map[int32]int32{}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)

		require.NoError(t, value.(*interp.HostMap).Set(i, types.BoxI32(1), types.BoxI32(7)))
		require.Equal(t, map[int32]int32{1: 7}, src)
	})

	t.Run("a dynamic key is stored where a lookup finds it", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		src := map[any]int32{}

		value, err := r.Marshal(i, src)
		require.NoError(t, err)
		host := value.(*interp.HostMap)

		require.NoError(t, host.Set(i, types.BoxI32(1), types.BoxI32(7)))
		require.Equal(t, map[any]int32{int32(1): 7}, src)

		_, ok, err := host.Get(i, types.BoxI32(1))
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("a nil Go map has nowhere to write", func(t *testing.T) {
		i := interp.New(program.New(nil))
		r := interp.NewRegistry()
		defer i.Close()
		var src map[int32]int32

		value, err := r.Marshal(i, &src)
		require.NoError(t, err)

		err = value.(*interp.HostMap).Set(i, types.BoxI32(1), types.BoxI32(7))
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
	})
}

func TestHostMap_Delete(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()
	src := map[int32]int32{1: 7}

	value, err := r.Marshal(i, src)
	require.NoError(t, err)

	require.NoError(t, value.(*interp.HostMap).Delete(i, types.BoxI32(1)))
	require.Empty(t, src)
}

func TestHostMap_Clear(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()
	src := map[int32]int32{1: 7, 2: 8}

	value, err := r.Marshal(i, src)
	require.NoError(t, err)

	value.(*interp.HostMap).Clear()
	require.Empty(t, src)
}

func TestHostMap_Map(t *testing.T) {
	i := interp.New(program.New(nil))
	r := interp.NewRegistry()
	defer i.Close()
	src := map[int32]int32{1: 7}

	value, err := r.Marshal(i, src)
	require.NoError(t, err)

	// The copy is VM-owned, so a later write to the Go map leaves it alone.
	out, err := value.(*interp.HostMap).Map(i)
	require.NoError(t, err)
	require.Equal(t, 1, out.(*types.TypedMap[int32]).Len())

	src[2] = 8
	require.Equal(t, 1, out.(*types.TypedMap[int32]).Len())
}
