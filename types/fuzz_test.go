package types_test

import (
	"strings"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func FuzzParseType(f *testing.F) {
	for _, value := range []string{
		"i32",
		"[]ref",
		"map[string][]i32",
		"iterator[map[i64]f32]",
		"func(i32, ref) (i64, f32)",
		"struct {i32; []ref}",
		"invalid",
	} {
		f.Add(value)
	}

	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 4096 {
			t.Skip()
		}
		typ, err := types.Parse(value)
		if err != nil {
			return
		}
		roundTrip, err := types.Parse(typ.String())
		require.NoError(t, err)
		require.True(t, typ.Equals(roundTrip), "type %q formatted as %q", value, typ.String())
	})
}

func FuzzParseFunction(f *testing.F) {
	f.Add("func()\n")
	f.Add("func() i32\n0000:\ti32.const 0x0000002A\n0005:\treturn\n")
	f.Add("func(i32) i32\ni64\ni32.const 42\nreturn")
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 4096 {
			t.Skip()
		}
		lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
		fn, err := types.ParseFunction(lines)
		if err != nil {
			return
		}
		roundTrip, err := types.ParseFunction(strings.Split(strings.TrimSuffix(fn.String(), "\n"), "\n"))
		require.NoError(t, err)
		require.Equal(t, fn.String(), roundTrip.String())
	})
}
