package types

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTag(t *testing.T) {
	require.Equal(t, uint64(Box(0, KindI32)), Tag(KindI32))
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
			require.Equal(t, tt.expected, IsBoxable(tt.val))
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
			val := BoxI32(tt.val)
			require.Equal(t, KindI32, val.Kind())
			require.Equal(t, tt.val, val.I32())
			require.Equal(t, I32(tt.val), Unbox(val))
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
			val := BoxI8(tt.val)
			require.Equal(t, KindI8, val.Kind())
			require.Equal(t, TypeI8, val.Type())
			require.Equal(t, tt.val, val.I8())
			require.Equal(t, I8(tt.val), Unbox(val))
		})
	}
}

func TestBoxI1(t *testing.T) {
	require.Equal(t, KindI1, BoxI1(true).Kind())
	require.Equal(t, TypeI1, BoxI1(true).Type())
	require.True(t, BoxI1(true).Bool())
	require.False(t, BoxI1(false).Bool())
	require.Equal(t, "true", BoxI1(true).String())
	require.Equal(t, "false", BoxI1(false).String())
	require.Equal(t, I1(true), Unbox(BoxI1(true)))
	require.Equal(t, I1(false), Unbox(BoxI1(false)))
	require.Equal(t, BoxedTrue, BoxI1(true))
	require.Equal(t, BoxedFalse, BoxI1(false))
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
			val := BoxI64(tt.val)
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
			val := BoxF32(tt.val)
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
			val := BoxF64(tt.val)
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
			val := BoxRef(tt.val)
			require.Equal(t, tt.val, val.Ref())
		})
	}
}

func TestBox(t *testing.T) {
	tests := []struct {
		val  uint64
		kind Kind
	}{
		{val: 0, kind: KindI32},
		{val: 1, kind: KindI32},
		{val: 0, kind: KindRef},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d/%d", tt.val, tt.kind), func(t *testing.T) {
			b := Box(tt.val, tt.kind)
			require.Equal(t, tt.kind, b.Kind())
		})
	}
}

func TestUnbox(t *testing.T) {
	tests := []struct {
		val   Boxed
		unbox Value
	}{
		{
			val:   BoxI32(0),
			unbox: I32(0),
		},
		{
			val:   BoxI64(0),
			unbox: I64(0),
		},
		{
			val:   BoxF32(0),
			unbox: F32(0),
		},
		{
			val:   BoxF64(0),
			unbox: F64(0),
		},
		{
			val:   BoxRef(3),
			unbox: Ref(3),
		},
		{
			val:   Box(0, Kind(255)),
			unbox: nil,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.unbox, Unbox(tt.val))
		})
	}
}

func TestBoxed_Kind(t *testing.T) {
	tests := []struct {
		val  Boxed
		kind Kind
	}{
		{
			val:  BoxI32(0),
			kind: KindI32,
		},
		{
			val:  BoxI64(0),
			kind: KindI64,
		},
		{
			val:  BoxF32(0),
			kind: KindF32,
		},
		{
			val:  BoxF64(0),
			kind: KindF64,
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
		val Boxed
		typ Type
	}{
		{
			val: BoxI32(0),
			typ: TypeI32,
		},
		{
			val: BoxI64(0),
			typ: TypeI64,
		},
		{
			val: BoxF32(0),
			typ: TypeF32,
		},
		{
			val: BoxF64(0),
			typ: TypeF64,
		},
		{
			val: BoxRef(0),
			typ: TypeRef,
		},
		{
			val: Box(0, Kind(255)),
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
	require.Equal(t, int32(-42), BoxI32(-42).I32())
}

func TestBoxed_I8(t *testing.T) {
	require.Equal(t, int8(-8), BoxI8(-8).I8())
}

func TestBoxed_I64(t *testing.T) {
	require.Equal(t, int64(-64), BoxI64(-64).I64())
}

func TestBoxed_F32(t *testing.T) {
	require.Equal(t, float32(3.5), BoxF32(3.5).F32())
}

func TestBoxed_F64(t *testing.T) {
	require.Equal(t, 6.25, BoxF64(6.25).F64())
}

func TestBoxed_Bool(t *testing.T) {
	require.True(t, BoxI32(1).Bool())
	require.False(t, BoxI32(0).Bool())
}

func TestBoxed_Ref(t *testing.T) {
	require.Equal(t, 42, BoxRef(42).Ref())
}

func TestBoxed_String(t *testing.T) {
	tests := []struct {
		val Boxed
		str string
	}{
		{
			val: BoxI32(0),
			str: "0",
		},
		{
			val: BoxI64(0),
			str: "0",
		},
		{
			val: BoxF32(0),
			str: "0",
		},
		{
			val: BoxF64(0),
			str: "0",
		},
		{
			val: BoxRef(3),
			str: "3",
		},
		{
			val: Box(0, Kind(255)),
			str: "<invalid>",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}
