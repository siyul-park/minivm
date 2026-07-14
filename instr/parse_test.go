package instr

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		line    string
		want    Instruction
		wantErr bool
	}{
		{
			line: "nop",
			want: New(NOP),
		},
		{
			line: "i32.const 0x0000002a",
			want: New(I32_CONST, 42),
		},
		{
			line: "i32.const 42",
			want: New(I32_CONST, 42),
		},
		{
			line: "i32.const -1",
			want: New(I32_CONST, 0xFFFFFFFF), // int32(-1) bit pattern
		},
		{
			line: "i64.const 0x000000000000002a",
			want: New(I64_CONST, 42),
		},
		{
			line: "f32.const 0x3F800000",
			want: New(F32_CONST, uint64(math.Float32bits(1.0))),
		},
		{
			line: "f32.const 1.0",
			want: New(F32_CONST, uint64(math.Float32bits(1.0))),
		},
		{
			line: "f64.const 3.14",
			want: New(F64_CONST, math.Float64bits(3.14)),
		},
		{
			line: "i32.add",
			want: New(I32_ADD),
		},
		{
			line: "br 0x0005",
			want: New(BR, 5),
		},
		{
			line: "br 5",
			want: New(BR, 5),
		},
		{
			line: "local.get 0x02",
			want: New(LOCAL_GET, 2),
		},
		{
			line: "closure.new",
			want: New(CLOSURE_NEW),
		},
		{
			line: "upval.get 0x01",
			want: New(UPVAL_GET, 1),
		},
		{
			line: "ref.new",
			want: New(REF_NEW),
		},
		{
			line: "string.iter",
			want: New(STRING_ITER),
		},
		{
			line: "br_table 0x02 0x0000 0x0001 0x0000",
			want: New(BR_TABLE, 2, 0, 1, 0),
		},
		{
			line: "br_table 0x00 0x0000",
			want: New(BR_TABLE, 0, 0),
		},
		{
			line: "0000:\ti32.const 0x00000001",
			want: New(I32_CONST, 1),
		},
		{
			line: "0010:   i32.add",
			want: New(I32_ADD),
		},
		{
			line: "",
			want: nil,
		},
		{
			line: "   ",
			want: nil,
		},
		{
			line:    "i32.unknown",
			wantErr: true,
		},
		{
			line:    "i32.const",
			wantErr: true,
		},
		{
			line:    "nop 0x01",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(strconv.Quote(tt.line), func(t *testing.T) {
			got, err := Parse(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

}

func TestParseAll(t *testing.T) {
	t.Run("multi-line basic", func(t *testing.T) {
		got, err := ParseAll(strings.NewReader("i32.const 1\ni32.const 2\ni32.add"))
		require.NoError(t, err)
		require.Equal(t, []Instruction{New(I32_CONST, 1), New(I32_CONST, 2), New(I32_ADD)}, got)
	})

	t.Run("multi-line blank lines skipped", func(t *testing.T) {
		got, err := ParseAll(strings.NewReader("\ni32.const 1\n\ni32.add\n"))
		require.NoError(t, err)
		require.Equal(t, []Instruction{New(I32_CONST, 1), New(I32_ADD)}, got)
	})

	t.Run("multi-line long line", func(t *testing.T) {
		got, err := ParseAll(strings.NewReader("i32.const" + strings.Repeat(" ", 70_000) + "1\n"))
		require.NoError(t, err)
		require.Equal(t, []Instruction{New(I32_CONST, 1)}, got)
	})

	t.Run("multi-line oversized line", func(t *testing.T) {
		_, err := ParseAll(strings.NewReader("i32.const " + strings.Repeat(" ", maxParseLineBytes) + "1\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds maximum allowed size")
	})

	t.Run("multi-line error propagates", func(t *testing.T) {
		_, err := ParseAll(strings.NewReader("i32.const 1\nbad.op\ni32.add"))
		require.Error(t, err)
	})

	t.Run("round-trip with Format", func(t *testing.T) {
		original := []Instruction{New(I32_CONST, 1), New(I32_CONST, 2), New(I32_ADD)}
		got, err := ParseAll(strings.NewReader(Format(Marshal(original))))
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	t.Run("round-trip br_table", func(t *testing.T) {
		original := []Instruction{New(BR_TABLE, 2, 0, 1, 0)}
		got, err := ParseAll(strings.NewReader(Format(Marshal(original))))
		require.NoError(t, err)
		require.Equal(t, original, got)
	})
}

func TestReadU8(t *testing.T) {
	require.Equal(t, 0xAB, ReadU8(0xDEADBEEF000000AB))
	require.Equal(t, 0xFF, ReadU8(0xFFFFFFFFFFFFFFFF))
	require.Equal(t, 0, ReadU8(0))
}

func TestReadI8(t *testing.T) {
	require.Equal(t, -1, ReadI8(uint64(uint8(0xFF))))
	require.Equal(t, -128, ReadI8(uint64(uint8(0x80))))
	require.Equal(t, 127, ReadI8(127))
	require.Equal(t, 0, ReadI8(0))
}

func TestReadU16(t *testing.T) {
	require.Equal(t, 0xCAFE, ReadU16(0xDEADBEEF0000CAFE))
	require.Equal(t, 0xFFFF, ReadU16(0xFFFFFFFFFFFFFFFF))
	require.Equal(t, 0, ReadU16(0))
}

func TestReadI16(t *testing.T) {
	require.Equal(t, -9, ReadI16(uint64(uint16(-9+1<<16))))
	require.Equal(t, -32768, ReadI16(uint64(uint16(0x8000))))
	require.Equal(t, 32767, ReadI16(32767))
}

func TestReadU32(t *testing.T) {
	require.Equal(t, 0xDEADBEEF, ReadU32(0xCAFEBABEDEADBEEF))
	require.Equal(t, 0, ReadU32(0))
}

func TestReadI32(t *testing.T) {
	require.Equal(t, -1, ReadI32(uint64(uint32(0xFFFFFFFF))))
	require.Equal(t, -2147483648, ReadI32(uint64(uint32(0x80000000))))
	require.Equal(t, 2147483647, ReadI32(2147483647))
}

func TestParseU8(t *testing.T) {
	code := []byte{0x00, 0xAB, 0xFF}
	require.Equal(t, 0x00, ParseU8(code, 0))
	require.Equal(t, 0xAB, ParseU8(code, 1))
	require.Equal(t, 0xFF, ParseU8(code, 2))
}

func TestParseI8(t *testing.T) {
	code := []byte{0x00, 0x7F, 0x80, 0xFF}
	require.Equal(t, 0, ParseI8(code, 0))
	require.Equal(t, 127, ParseI8(code, 1))
	require.Equal(t, -128, ParseI8(code, 2))
	require.Equal(t, -1, ParseI8(code, 3))
}

func TestParseU16(t *testing.T) {
	code := []byte{0x34, 0x12, 0xFF, 0xFF}
	require.Equal(t, 0x1234, ParseU16(code, 0))
	require.Equal(t, 0xFFFF, ParseU16(code, 2))
}

func TestParseI16(t *testing.T) {
	instr := New(BR, uint64(uint16(-9+1<<16)))
	require.Equal(t, -9, ParseI16(instr, 1))

	code := []byte{0x00, 0x80, 0xFF, 0x7F}
	require.Equal(t, -32768, ParseI16(code, 0))
	require.Equal(t, 32767, ParseI16(code, 2))
}

func TestParseU32(t *testing.T) {
	code := []byte{0x78, 0x56, 0x34, 0x12, 0xFF, 0xFF, 0xFF, 0xFF}
	require.Equal(t, 0x12345678, ParseU32(code, 0))
	require.Equal(t, int(uint32(0xFFFFFFFF)), ParseU32(code, 4))
}

func TestParseI32(t *testing.T) {
	code := []byte{0x00, 0x00, 0x00, 0x80, 0xFF, 0xFF, 0xFF, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF}
	require.Equal(t, -2147483648, ParseI32(code, 0))
	require.Equal(t, 2147483647, ParseI32(code, 4))
	require.Equal(t, -1, ParseI32(code, 8))
}
