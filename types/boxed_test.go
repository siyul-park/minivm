package types_test

import (
	"fmt"
	"math"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestTag(t *testing.T) {
	require.Equal(t, uint64(types.Box(0, types.KindI32)), types.Tag(types.KindI32))
}

func TestIsBoxable(t *testing.T) {
	tests := []struct {
		val      int64
		expected bool
	}{
		{
			val:      -1,
			expected: true,
		},
		{
			val:      0,
			expected: true,
		},
		{
			val:      math.MinInt32,
			expected: true,
		},
		{
			val:      math.MaxInt32,
			expected: true,
		},
		{
			val:      -(1 << 48),
			expected: true,
		},
		{
			val:      (1 << 48) - 1,
			expected: true,
		},
		{
			val:      -(1 << 48) - 1,
			expected: false,
		},
		{
			val:      1 << 48,
			expected: false,
		},
		{
			val:      math.MinInt64,
			expected: false,
		},
		{
			val:      math.MaxInt64,
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.expected, types.IsBoxable(tt.val))
		})
	}
}

func TestBoxI32(t *testing.T) {
	tests := []struct {
		val int32
	}{
		{
			val: -1,
		},
		{
			val: 0,
		},
		{
			val: int32(math.MinInt32),
		},
		{
			val: int32(math.MaxInt32),
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			val := types.BoxI32(tt.val)
			require.Equal(t, types.KindI32, val.Kind())
			require.Equal(t, tt.val, val.I32())
			require.Equal(t, types.I32(tt.val), types.Unbox(val))
		})
	}
}

func TestBoxI8(t *testing.T) {
	tests := []struct {
		val int8
	}{
		{val: -1},
		{val: 0},
		{val: int8(math.MinInt8)},
		{val: int8(math.MaxInt8)},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			val := types.BoxI8(tt.val)
			require.Equal(t, types.KindI8, val.Kind())
			require.Equal(t, types.TypeI8, val.Type())
			require.Equal(t, tt.val, val.I8())
			require.Equal(t, types.I8(tt.val), types.Unbox(val))
		})
	}
}

func TestBoxI1(t *testing.T) {
	require.Equal(t, types.KindI1, types.BoxI1(true).Kind())
	require.Equal(t, types.TypeI1, types.BoxI1(true).Type())
	require.True(t, types.BoxI1(true).Bool())
	require.False(t, types.BoxI1(false).Bool())
	require.Equal(t, "true", types.BoxI1(true).String())
	require.Equal(t, "false", types.BoxI1(false).String())
	require.Equal(t, types.I1(true), types.Unbox(types.BoxI1(true)))
	require.Equal(t, types.I1(false), types.Unbox(types.BoxI1(false)))
	require.Equal(t, types.BoxedTrue, types.BoxI1(true))
	require.Equal(t, types.BoxedFalse, types.BoxI1(false))
}

func TestBoxI64(t *testing.T) {
	tests := []struct {
		val int64
	}{
		{
			val: -1,
		},
		{
			val: 0,
		},
		{
			val: int64(math.MinInt32),
		},
		{
			val: int64(math.MaxInt32),
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			val := types.BoxI64(tt.val)
			require.Equal(t, tt.val, val.I64())
		})
	}
}

func TestBoxF32(t *testing.T) {
	tests := []struct {
		val float32
	}{
		{
			val: -1,
		},
		{
			val: 0,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			val := types.BoxF32(tt.val)
			require.Equal(t, tt.val, val.F32())
		})
	}
}

func TestBoxF64(t *testing.T) {
	tests := []struct {
		val float64
	}{
		{
			val: -1,
		},
		{
			val: 0,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			val := types.BoxF64(tt.val)
			require.Equal(t, tt.val, val.F64())
		})
	}
}

func TestBoxRef(t *testing.T) {
	tests := []struct {
		val int
	}{
		{
			val: -1,
		},
		{
			val: 0,
		},
		{
			val: math.MinInt32,
		},
		{
			val: math.MaxInt32,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			val := types.BoxRef(tt.val)
			require.Equal(t, tt.val, val.Ref())
		})
	}
}

func TestBox(t *testing.T) {
	tests := []struct {
		val  uint64
		kind types.Kind
	}{
		{val: 0, kind: types.KindI32},
		{val: 1, kind: types.KindI32},
		{val: 0, kind: types.KindRef},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d/%d", tt.val, tt.kind), func(t *testing.T) {
			b := types.Box(tt.val, tt.kind)
			require.Equal(t, tt.kind, b.Kind())
		})
	}
}

func TestUnbox(t *testing.T) {
	tests := []struct {
		val   types.Boxed
		unbox types.Value
	}{
		{
			val:   types.BoxI32(0),
			unbox: types.I32(0),
		},
		{
			val:   types.BoxI64(0),
			unbox: types.I64(0),
		},
		{
			val:   types.BoxF32(0),
			unbox: types.F32(0),
		},
		{
			val:   types.BoxF64(0),
			unbox: types.F64(0),
		},
		{
			val:   types.BoxRef(3),
			unbox: types.Ref(3),
		},
		{
			val:   types.Box(0, types.Kind(255)),
			unbox: nil,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.unbox, types.Unbox(tt.val))
		})
	}
}

func TestBoxed_Kind(t *testing.T) {
	tests := []struct {
		val  types.Boxed
		kind types.Kind
	}{
		{
			val:  types.BoxI32(0),
			kind: types.KindI32,
		},
		{
			val:  types.BoxI64(0),
			kind: types.KindI64,
		},
		{
			val:  types.BoxF32(0),
			kind: types.KindF32,
		},
		{
			val:  types.BoxF64(0),
			kind: types.KindF64,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.kind, tt.val.Kind())
		})
	}
}

func TestBoxed_Type(t *testing.T) {
	tests := []struct {
		val types.Boxed
		typ types.Type
	}{
		{
			val: types.BoxI32(0),
			typ: types.TypeI32,
		},
		{
			val: types.BoxI64(0),
			typ: types.TypeI64,
		},
		{
			val: types.BoxF32(0),
			typ: types.TypeF32,
		},
		{
			val: types.BoxF64(0),
			typ: types.TypeF64,
		},
		{
			val: types.BoxRef(0),
			typ: types.TypeRef,
		},
		{
			val: types.Box(0, types.Kind(255)),
			typ: nil,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
}

func TestBoxed_I32(t *testing.T) {
	require.Equal(t, int32(-42), types.BoxI32(-42).I32())
}

func TestBoxed_I8(t *testing.T) {
	require.Equal(t, int8(-8), types.BoxI8(-8).I8())
}

func TestBoxed_I64(t *testing.T) {
	require.Equal(t, int64(-64), types.BoxI64(-64).I64())
}

func TestBoxed_F32(t *testing.T) {
	require.Equal(t, float32(3.5), types.BoxF32(3.5).F32())
}

func TestBoxed_F64(t *testing.T) {
	require.Equal(t, 6.25, types.BoxF64(6.25).F64())
}

func TestBoxed_Bool(t *testing.T) {
	require.True(t, types.BoxI32(1).Bool())
	require.False(t, types.BoxI32(0).Bool())
}

func TestBoxed_Ref(t *testing.T) {
	require.Equal(t, 42, types.BoxRef(42).Ref())
}

func TestBoxed_String(t *testing.T) {
	tests := []struct {
		val types.Boxed
		str string
	}{
		{
			val: types.BoxI32(0),
			str: "0",
		},
		{
			val: types.BoxI64(0),
			str: "0",
		},
		{
			val: types.BoxF32(0),
			str: "0",
		},
		{
			val: types.BoxF64(0),
			str: "0",
		},
		{
			val: types.BoxRef(3),
			str: "3",
		},
		{
			val: types.Box(0, types.Kind(255)),
			str: "<invalid>",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}
