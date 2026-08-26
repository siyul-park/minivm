package types_test

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestParseFunction(t *testing.T) {
	tests := []struct {
		lines []string
	}{
		{
			// no locals
			lines: strings.Split(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
				MustBuild().String(), "\n",
			),
		},
		{
			// with locals
			lines: strings.Split(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
				Locals(types.TypeI32, types.TypeI64).
				Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
				MustBuild().String(), "\n",
			),
		},
		{
			// with captures and locals
			lines: strings.Split(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.TypeI32, types.TypeAny).
				Locals(types.TypeI64).
				Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
				MustBuild().String(), "\n",
			),
		},
	}

	for _, tt := range tests {
		// Drop trailing empty strings from split
		lines := tt.lines
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		t.Run(lines[0], func(t *testing.T) {
			fn, err := types.ParseFunction(lines)
			require.NoError(t, err)
			require.NotNil(t, fn)
			// Round-trip: String() must match input
			got := strings.Split(fn.String(), "\n")
			for len(got) > 0 && got[len(got)-1] == "" {
				got = got[:len(got)-1]
			}
			require.Equal(t, lines, got)
		})
	}

	t.Run("no offset prefix", func(t *testing.T) {
		// Instructions written without offset prefix must parse successfully.
		lines := []string{
			"func() i32",
			"i32.const 42",
			"return",
		}
		fn, err := types.ParseFunction(lines)
		require.NoError(t, err)
		require.NotNil(t, fn)
		require.Equal(t, 0, len(fn.Locals))
		require.Equal(t, 2, len(instr.Unmarshal(fn.Code)))
	})

	t.Run("no offset prefix with locals", func(t *testing.T) {
		lines := []string{
			"func(i32) i32",
			"i32",
			"i64",
			"i32.const 42",
			"return",
		}
		fn, err := types.ParseFunction(lines)
		require.NoError(t, err)
		require.NotNil(t, fn)
		require.Equal(t, 2, len(fn.Locals))
		require.Equal(t, 2, len(instr.Unmarshal(fn.Code)))
	})

	t.Run("captures before locals", func(t *testing.T) {
		lines := []string{
			"func() i32",
			"capture i32",
			"capture any",
			"i64",
			"i32.const 42",
			"return",
		}
		fn, err := types.ParseFunction(lines)
		require.NoError(t, err)
		require.NotNil(t, fn)
		require.Equal(t, []types.Type{types.TypeI32, types.TypeAny}, fn.Captures)
		require.Equal(t, []types.Type{types.TypeI64}, fn.Locals)
		require.Equal(t, 2, len(instr.Unmarshal(fn.Code)))
	})
}
func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		want    types.Type
		wantErr bool
	}{
		{"i1", types.TypeI1, false},
		{"i8", types.TypeI8, false},
		{"i32", types.TypeI32, false},
		{"i64", types.TypeI64, false},
		{"f32", types.TypeF32, false},
		{"f64", types.TypeF64, false},
		{"any", types.TypeAny, false},
		{"string", types.TypeString, false},
		{"[]i8", types.NewArrayType(types.TypeI8), false},
		{"[]i32", types.NewArrayType(types.TypeI32), false},
		{"[]f64", types.NewArrayType(types.TypeF64), false},
		{"map[i32]string", types.NewMapType(types.TypeI32, types.TypeString), false},
		{"map[string][]i32", types.NewMapType(types.TypeString, types.NewArrayType(types.TypeI32)), false},
		{"map[[]i32]f64", types.NewMapType(types.NewArrayType(types.TypeI32), types.TypeF64), false},
		{"iterator[i32]", types.NewIteratorType(types.TypeI32), false},
		{"iterator[map[string]i32]", types.NewIteratorType(types.NewMapType(types.TypeString, types.TypeI32)), false},
		{"func()", &types.FunctionType{}, false},
		{"func(i32) i64", &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}}, false},
		{"func(i32, f64) i32", &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeF64}, Returns: []types.Type{types.TypeI32}}, false},
		{"func(i32) (i32, i64)", &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32, types.TypeI64}}, false},
		{"struct {i32; f64}", types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64)), false},
		{"struct {value: i64; any}", types.NewStructType(types.NewStructField(types.TypeI64, types.FieldWithName("value")), types.NewStructField(types.TypeAny)), false},
		{"ref", nil, true},
		{"map[]i32", nil, true},
		{"map[i32]", nil, true},
		{"iterator[]", nil, true},
		{"iterator[i32", nil, true},
		{"bad", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := types.Parse(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, tt.want.Equals(got), "got %v, want %v", got, tt.want)
		})
	}
}

func TestParse_NestedStructFields(t *testing.T) {
	// A nested struct carries its own ";" separators, so the field split has to
	// track brace depth rather than cutting on every semicolon.
	inner := types.NewStructType(
		types.NewStructField(types.TypeI32, types.FieldWithName("x")),
		types.NewStructField(types.TypeI32, types.FieldWithName("y")),
	)
	outer := types.NewStructType(
		types.NewStructField(inner, types.FieldWithName("a")),
		types.NewStructField(types.TypeI32, types.FieldWithName("b")),
	)

	parsed, err := types.Parse(outer.String())
	require.NoError(t, err)
	require.Equal(t, outer.String(), parsed.String())
}

func TestParse_StructFieldNames(t *testing.T) {
	// StructType.Equals ignores field names, so a dedicated test is needed to
	// confirm parseStructType actually restores them instead of only matching
	// field types.
	t.Run("restores named and unnamed fields from String output", func(t *testing.T) {
		want := types.NewStructType(
			types.NewStructField(types.TypeI64, types.FieldWithName("value")),
			types.NewStructField(types.TypeAny, types.FieldWithName("left")),
			types.NewStructField(types.TypeAny),
		)
		got, err := types.Parse(want.String())
		require.NoError(t, err)

		st, ok := got.(*types.StructType)
		require.True(t, ok)
		require.Equal(t, want, st)
		require.Equal(t, "value", st.Fields[0].Name)
		require.Equal(t, "left", st.Fields[1].Name)
		require.Equal(t, "", st.Fields[2].Name)
	})

	t.Run("field name lookalike prefix without colon-space stays part of the type", func(t *testing.T) {
		// "notaname" here isn't followed by ": ", so parseStructType must not
		// misread it as a name and swallow the following token into the type.
		_, err := types.Parse("struct {notaname i32}")
		require.Error(t, err)
	})
}
