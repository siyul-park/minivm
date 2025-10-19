package types

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.unbox, Unbox(tt.val))
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
			require.Equal(t, tt.val, val.I32())
		})
	}
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
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
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
			str: "0.000000",
		},
		{
			val: BoxF64(0),
			str: "0.000000",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}
