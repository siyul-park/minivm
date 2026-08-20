package interp_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type encodeCelsius int32

func TestMarshalerFunc_Marshal(t *testing.T) {
	i := interp.New(program.New(nil))
	defer i.Close()
	n := int32(7)

	m := interp.MarshalerFunc(func(_ *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
		return types.I32(*(*int32)(p) * 2), nil
	})
	r := interp.NewRegistry(interp.WithMarshaler(reflect.TypeFor[int32](), types.TypeI32, m))

	got, err := r.Marshal(i, n)
	require.NoError(t, err)
	require.Equal(t, types.I32(14), got)
}

func TestEncoder_Interp(t *testing.T) {
	i := interp.New(program.New(nil))
	defer i.Close()

	var got *interp.Interpreter
	r := interp.NewRegistry(interp.WithMarshaler(
		reflect.TypeFor[encodeCelsius](), types.TypeI32,
		interp.MarshalerFunc(func(e *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
			got = e.Interp()
			return types.I32(*(*encodeCelsius)(p)), nil
		})))

	_, err := r.Marshal(i, encodeCelsius(3))
	require.NoError(t, err)
	require.Same(t, i, got)
}

func TestEncoder_Marshal(t *testing.T) {
	// The injected encoder resolves dependencies through the registry that
	// started the conversion, so a registration on the dependency applies to
	// the delegating marshaler too.
	i := interp.New(program.New(nil))
	defer i.Close()

	r := interp.NewRegistry(
		interp.WithMarshaler(
			reflect.TypeFor[encodeCelsius](), types.TypeI32,
			interp.MarshalerFunc(func(e *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
				n := int32(*(*encodeCelsius)(p))
				return e.Marshal(reflect.TypeFor[int32](), unsafe.Pointer(&n))
			})),
		interp.WithMarshaler(
			reflect.TypeFor[int32](), types.TypeI32,
			interp.MarshalerFunc(func(_ *interp.Encoder, p unsafe.Pointer) (types.Value, error) {
				return types.I32(*(*int32)(p) + 100), nil
			})),
	)

	got, err := r.Marshal(i, encodeCelsius(3))
	require.NoError(t, err)
	require.Equal(t, types.I32(103), got)
}
