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
		name  string
		lines []string
		want  *types.Function
	}{
		{
			name: "no locals",
			want: types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
				MustBuild(),
		},
		{
			name: "with locals",
			want: types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
				Locals(types.TypeI32, types.TypeI64).
				Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
				MustBuild(),
		},
		{
			name: "with captures and locals",
			want: types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.TypeI32, types.TypeAny).
				Locals(types.TypeI64).
				Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
				MustBuild(),
		},
		{
			name:  "no offset prefix",
			lines: []string{"func() i32", "i32.const 42", "return"},
			want: types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
				MustBuild(),
		},
		{
			name:  "no offset prefix with locals",
			lines: []string{"func(i32) i32", "i32", "i64", "i32.const 42", "return"},
			want: types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
				Locals(types.TypeI32, types.TypeI64).
				Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
				MustBuild(),
		},
		{
			name:  "captures before locals",
			lines: []string{"func() i32", "capture i32", "capture any", "i64", "i32.const 42", "return"},
			want: types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Captures(types.TypeI32, types.TypeAny).
				Locals(types.TypeI64).
				Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
				MustBuild(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := tt.lines
			if lines == nil {
				lines = strings.Split(strings.TrimRight(tt.want.String(), "\n"), "\n")
			}
			fn, err := types.ParseFunction(lines)
			require.NoError(t, err, lines[0])
			require.NotNil(t, fn, lines[0])
			require.Equal(t, tt.want.String(), fn.String(), lines[0])
		})
	}
}

func TestParse(t *testing.T) {
	nested := types.NewStructType(
		types.NewStructField(types.NewStructType(
			types.NewStructField(types.TypeI32, types.FieldWithName("x")),
			types.NewStructField(types.TypeI32, types.FieldWithName("y")),
		), types.FieldWithName("a")),
		types.NewStructField(types.TypeI32, types.FieldWithName("b")),
	)
	named := types.NewStructType(
		types.NewStructField(types.TypeI64, types.FieldWithName("value")),
		types.NewStructField(types.TypeAny, types.FieldWithName("left")),
		types.NewStructField(types.TypeAny),
	)

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
		// A nested struct carries its own ";" separators, so the field split must track brace depth.
		{nested.String(), nested, false},
		// StructType.Equals ignores field names; String equality below pins them.
		{named.String(), named, false},
		// "notaname" is not followed by ": ", so it must not be read as a field name.
		{"struct {notaname i32}", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := types.Parse(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, tt.want.Equals(got))
			require.Equal(t, tt.want.String(), got.String())
		})
	}
}
