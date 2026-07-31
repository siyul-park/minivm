package instr_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

const parseLineLimit = 1 << 20

func TestReadU8(t *testing.T) {
	require.Equal(t, 0xAB, instr.ReadU8(0xDEADBEEF000000AB))
	require.Equal(t, 0xFF, instr.ReadU8(0xFFFFFFFFFFFFFFFF))
	require.Equal(t, 0, instr.ReadU8(0))
}

func TestReadI8(t *testing.T) {
	require.Equal(t, -1, instr.ReadI8(uint64(uint8(0xFF))))
	require.Equal(t, -128, instr.ReadI8(uint64(uint8(0x80))))
	require.Equal(t, 127, instr.ReadI8(127))
	require.Equal(t, 0, instr.ReadI8(0))
}

func TestReadU16(t *testing.T) {
	require.Equal(t, 0xCAFE, instr.ReadU16(0xDEADBEEF0000CAFE))
	require.Equal(t, 0xFFFF, instr.ReadU16(0xFFFFFFFFFFFFFFFF))
	require.Equal(t, 0, instr.ReadU16(0))
}

func TestReadI16(t *testing.T) {
	require.Equal(t, -9, instr.ReadI16(uint64(uint16(-9+1<<16))))
	require.Equal(t, -32768, instr.ReadI16(uint64(uint16(0x8000))))
	require.Equal(t, 32767, instr.ReadI16(32767))
}

func TestReadU32(t *testing.T) {
	require.Equal(t, 0xDEADBEEF, instr.ReadU32(0xCAFEBABEDEADBEEF))
	require.Equal(t, 0, instr.ReadU32(0))
}

func TestReadI32(t *testing.T) {
	require.Equal(t, -1, instr.ReadI32(uint64(uint32(0xFFFFFFFF))))
	require.Equal(t, -2147483648, instr.ReadI32(uint64(uint32(0x80000000))))
	require.Equal(t, 2147483647, instr.ReadI32(2147483647))
}

func TestParseU8(t *testing.T) {
	code := []byte{0x00, 0xAB, 0xFF}
	require.Equal(t, 0x00, instr.ParseU8(code, 0))
	require.Equal(t, 0xAB, instr.ParseU8(code, 1))
	require.Equal(t, 0xFF, instr.ParseU8(code, 2))
}

func TestParseI8(t *testing.T) {
	code := []byte{0x00, 0x7F, 0x80, 0xFF}
	require.Equal(t, 0, instr.ParseI8(code, 0))
	require.Equal(t, 127, instr.ParseI8(code, 1))
	require.Equal(t, -128, instr.ParseI8(code, 2))
	require.Equal(t, -1, instr.ParseI8(code, 3))
}

func TestParseI16(t *testing.T) {
	instruction := instr.New(instr.BR, uint64(uint16(-9+1<<16)))
	require.Equal(t, -9, instr.ParseI16(instruction, 1))

	code := []byte{0x00, 0x80, 0xFF, 0x7F}
	require.Equal(t, -32768, instr.ParseI16(code, 0))
	require.Equal(t, 32767, instr.ParseI16(code, 2))
}

func TestParseU16(t *testing.T) {
	code := []byte{0x34, 0x12, 0xFF, 0xFF}
	require.Equal(t, 0x1234, instr.ParseU16(code, 0))
	require.Equal(t, 0xFFFF, instr.ParseU16(code, 2))
}

func TestParseI32(t *testing.T) {
	code := []byte{0x00, 0x00, 0x00, 0x80, 0xFF, 0xFF, 0xFF, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF}
	require.Equal(t, -2147483648, instr.ParseI32(code, 0))
	require.Equal(t, 2147483647, instr.ParseI32(code, 4))
	require.Equal(t, -1, instr.ParseI32(code, 8))
}

func TestParseU32(t *testing.T) {
	code := []byte{0x78, 0x56, 0x34, 0x12, 0xFF, 0xFF, 0xFF, 0xFF}
	require.Equal(t, 0x12345678, instr.ParseU32(code, 0))
	require.Equal(t, int(uint32(0xFFFFFFFF)), instr.ParseU32(code, 4))
}

func TestParseAll(t *testing.T) {
	t.Run("multi-line basic", func(t *testing.T) {
		got, err := instr.ParseAll(strings.NewReader("i32.const 1\ni32.const 2\ni32.add"))
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)}, got)
	})

	t.Run("multi-line blank lines skipped", func(t *testing.T) {
		got, err := instr.ParseAll(strings.NewReader("\ni32.const 1\n\ni32.add\n"))
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD)}, got)
	})

	t.Run("multi-line long line", func(t *testing.T) {
		got, err := instr.ParseAll(strings.NewReader("i32.const" + strings.Repeat(" ", 70_000) + "1\n"))
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.New(instr.I32_CONST, 1)}, got)
	})

	t.Run("multi-line limit", func(t *testing.T) {
		prefix, suffix := "i32.const ", "1"
		line := prefix + strings.Repeat(" ", parseLineLimit-len(prefix)-len(suffix)-1) + suffix
		got, err := instr.ParseAll(strings.NewReader(line))
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.New(instr.I32_CONST, 1)}, got)
	})

	t.Run("multi-line oversized line", func(t *testing.T) {
		prefix, suffix := "i32.const ", "1"
		line := prefix + strings.Repeat(" ", parseLineLimit+1-len(prefix)-len(suffix)) + suffix
		_, err := instr.ParseAll(strings.NewReader(line))
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds maximum allowed size")
	})

	t.Run("multi-line error propagates", func(t *testing.T) {
		_, err := instr.ParseAll(strings.NewReader("i32.const 1\nbad.op\ni32.add"))
		require.Error(t, err)
	})

	t.Run("round-trip with Format", func(t *testing.T) {
		original := []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)}
		got, err := instr.ParseAll(strings.NewReader(instr.Format(instr.Marshal(original))))
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	t.Run("round-trip br_table", func(t *testing.T) {
		original := []instr.Instruction{instr.New(instr.BR_TABLE, 2, 0, 1, 0)}
		got, err := instr.ParseAll(strings.NewReader(instr.Format(instr.Marshal(original))))
		require.NoError(t, err)
		require.Equal(t, original, got)
	})
}

func TestParse(t *testing.T) {
	tests := []struct {
		line    string
		want    instr.Instruction
		wantErr bool
	}{
		{
			line: "nop",
			want: instr.New(instr.NOP),
		},
		{
			line: "i32.const 0x0000002a",
			want: instr.New(instr.I32_CONST, 42),
		},
		{
			line: "i32.const 42",
			want: instr.New(instr.I32_CONST, 42),
		},
		{
			line: "i32.const -1",
			want: instr.New(instr.I32_CONST, 0xFFFFFFFF), // int32(-1) bit pattern
		},
		{
			line: "i64.const 0x000000000000002a",
			want: instr.New(instr.I64_CONST, 42),
		},
		{
			line: "f32.const 0x3F800000",
			want: instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
		},
		{
			line: "f32.const 1.0",
			want: instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
		},
		{
			line: "f64.const 3.14",
			want: instr.New(instr.F64_CONST, math.Float64bits(3.14)),
		},
		{
			line: "i32.add",
			want: instr.New(instr.I32_ADD),
		},
		{
			line: "br 0x0005",
			want: instr.New(instr.BR, 5),
		},
		{
			line: "br 5",
			want: instr.New(instr.BR, 5),
		},
		{
			line: "local.get 0x02",
			want: instr.New(instr.LOCAL_GET, 2),
		},
		{
			line: "closure.new",
			want: instr.New(instr.CLOSURE_NEW),
		},
		{
			line: "upval.get 0x01",
			want: instr.New(instr.UPVAL_GET, 1),
		},
		{
			line: "ref.new",
			want: instr.New(instr.REF_NEW),
		},
		{
			line: "string.iter",
			want: instr.New(instr.STRING_ITER),
		},
		{
			line: "br_table 0x02 0x0000 0x0001 0x0000",
			want: instr.New(instr.BR_TABLE, 2, 0, 1, 0),
		},
		{
			line: "br_table 0x00 0x0000",
			want: instr.New(instr.BR_TABLE, 0, 0),
		},
		{
			line: "0000:\ti32.const 0x00000001",
			want: instr.New(instr.I32_CONST, 1),
		},
		{
			line: "0010:   i32.add",
			want: instr.New(instr.I32_ADD),
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
			got, err := instr.Parse(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
