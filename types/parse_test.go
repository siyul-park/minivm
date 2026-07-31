package types_test

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

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
		{"ref", types.TypeRef, false},
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
				Captures(types.TypeI32, types.TypeRef).
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
			"capture ref",
			"i64",
			"i32.const 42",
			"return",
		}
		fn, err := types.ParseFunction(lines)
		require.NoError(t, err)
		require.NotNil(t, fn)
		require.Equal(t, []types.Type{types.TypeI32, types.TypeRef}, fn.Captures)
		require.Equal(t, []types.Type{types.TypeI64}, fn.Locals)
		require.Equal(t, 2, len(instr.Unmarshal(fn.Code)))
	})
}
