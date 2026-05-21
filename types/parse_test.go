package types

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		want    Type
		wantErr bool
	}{
		{"i32", TypeI32, false},
		{"i64", TypeI64, false},
		{"f32", TypeF32, false},
		{"f64", TypeF64, false},
		{"ref", TypeRef, false},
		{"string", TypeString, false},
		{"[]i32", NewArrayType(TypeI32), false},
		{"[]f64", NewArrayType(TypeF64), false},
		{"map[i32]string", NewMapType(TypeI32, TypeString), false},
		{"map[string][]i32", NewMapType(TypeString, NewArrayType(TypeI32)), false},
		{"map[[]i32]f64", NewMapType(NewArrayType(TypeI32), TypeF64), false},
		{"func()", &FunctionType{}, false},
		{"func(i32) i64", &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI64}}, false},
		{"func(i32, f64) i32", &FunctionType{Params: []Type{TypeI32, TypeF64}, Returns: []Type{TypeI32}}, false},
		{"func(i32) (i32, i64)", &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI32, TypeI64}}, false},
		{"struct {i32; f64}", NewStructType(NewStructField(TypeI32), NewStructField(TypeF64)), false},
		{"map[]i32", nil, true},
		{"map[i32]", nil, true},
		{"bad", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
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
			lines: strings.Split(
				NewFunctionBuilder(&FunctionType{Returns: []Type{TypeI32}}).
					Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
					Build().String(),
				"\n",
			),
		},
		{
			// with locals
			lines: strings.Split(
				NewFunctionBuilder(&FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI32}}).
					WithLocals(TypeI32, TypeI64).
					Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
					Build().String(),
				"\n",
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
			fn, err := ParseFunction(lines)
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
		fn, err := ParseFunction(lines)
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
		fn, err := ParseFunction(lines)
		require.NoError(t, err)
		require.NotNil(t, fn)
		require.Equal(t, 2, len(fn.Locals))
		require.Equal(t, 2, len(instr.Unmarshal(fn.Code)))
	})
}
