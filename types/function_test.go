package types

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestFunction_Kind(t *testing.T) {
	fn := NewFunction(nil, nil, nil)
	require.Equal(t, KindRef, fn.Kind())
}

func TestFunction_Type(t *testing.T) {
	fn := NewFunction(nil, nil, nil)
	require.Equal(t, &FunctionType{}, fn.Type())
}

func TestFunction_String(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		fn := NewFunction(nil, nil, nil)
		require.Equal(t, "func()\n", fn.String())
	})

	t.Run("with captures", func(t *testing.T) {
		fn := NewFunctionBuilder(&FunctionType{Returns: []Type{TypeI32}}).
			WithCaptures(TypeI32, TypeRef).
			WithLocals(TypeI64).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
			Build()
		require.Contains(t, fn.String(), "capture i32\ncapture ref\n")
	})
}

func TestFunctionBuilder_WithCaptures(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithCaptures(TypeI32, TypeF64).Build()
	require.Equal(t, []Type{TypeI32, TypeF64}, fn.Captures)
}
