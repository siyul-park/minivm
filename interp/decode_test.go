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

type decodeCelsius int32

func TestUnmarshalerFunc_Unmarshal(t *testing.T) {
	i := interp.New(program.New(nil))
	defer i.Close()

	u := interp.UnmarshalerFunc(func(_ *interp.Decoder, val types.Value, p unsafe.Pointer) error {
		n, ok := val.(types.I32)
		if !ok {
			return interp.ErrTypeMismatch
		}
		*(*int32)(p) = int32(n) * 2
		return nil
	})
	r := interp.NewRegistry(interp.WithUnmarshaler(reflect.TypeFor[int32](), u))

	var dst int32
	require.NoError(t, r.Unmarshal(i, types.I32(7), &dst))
	require.Equal(t, int32(14), dst)
}

func TestDecoder_Interp(t *testing.T) {
	i := interp.New(program.New(nil))
	defer i.Close()

	var got *interp.Interpreter
	r := interp.NewRegistry(interp.WithUnmarshaler(
		reflect.TypeFor[decodeCelsius](),
		interp.UnmarshalerFunc(func(d *interp.Decoder, _ types.Value, p unsafe.Pointer) error {
			got = d.Interp()
			*(*decodeCelsius)(p) = 1
			return nil
		})))

	var dst decodeCelsius
	require.NoError(t, r.Unmarshal(i, types.I32(3), &dst))
	require.Same(t, i, got)
}

func TestDecoder_Unmarshal(t *testing.T) {
	// The injected decoder resolves dependencies through the registry that
	// started the conversion, so a registration on the dependency applies to
	// the delegating unmarshaler too.
	i := interp.New(program.New(nil))
	defer i.Close()

	r := interp.NewRegistry(
		interp.WithUnmarshaler(
			reflect.TypeFor[decodeCelsius](),
			interp.UnmarshalerFunc(func(d *interp.Decoder, val types.Value, p unsafe.Pointer) error {
				var n int32
				if err := d.Decode(val, reflect.TypeFor[int32](), unsafe.Pointer(&n)); err != nil {
					return err
				}
				*(*decodeCelsius)(p) = decodeCelsius(n)
				return nil
			})),
		interp.WithUnmarshaler(
			reflect.TypeFor[int32](),
			interp.UnmarshalerFunc(func(_ *interp.Decoder, val types.Value, p unsafe.Pointer) error {
				n, ok := val.(types.I32)
				if !ok {
					return interp.ErrTypeMismatch
				}
				*(*int32)(p) = int32(n) + 100
				return nil
			})),
	)

	var dst decodeCelsius
	require.NoError(t, r.Unmarshal(i, types.I32(3), &dst))
	require.Equal(t, decodeCelsius(103), dst)
}
