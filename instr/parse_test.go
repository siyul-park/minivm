package instr

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    Instruction
		wantErr bool
	}{
		{
			name: "nop",
			line: "nop",
			want: New(NOP),
		},
		{
			name: "i32.const hex",
			line: "i32.const 0x0000002a",
			want: New(I32_CONST, 42),
		},
		{
			name: "i32.const decimal",
			line: "i32.const 42",
			want: New(I32_CONST, 42),
		},
		{
			name: "i32.const negative",
			line: "i32.const -1",
			want: New(I32_CONST, 0xFFFFFFFF), // int32(-1) bit pattern
		},
		{
			name: "i64.const",
			line: "i64.const 0x000000000000002a",
			want: New(I64_CONST, 42),
		},
		{
			name: "f32.const hex",
			line: "f32.const 0x3F800000",
			want: New(F32_CONST, uint64(math.Float32bits(1.0))),
		},
		{
			name: "f32.const float",
			line: "f32.const 1.0",
			want: New(F32_CONST, uint64(math.Float32bits(1.0))),
		},
		{
			name: "f64.const float",
			line: "f64.const 3.14",
			want: New(F64_CONST, math.Float64bits(3.14)),
		},
		{
			name: "i32.add no operands",
			line: "i32.add",
			want: New(I32_ADD),
		},
		{
			name: "br",
			line: "br 0x0005",
			want: New(BR, 5),
		},
		{
			name: "br decimal",
			line: "br 5",
			want: New(BR, 5),
		},
		{
			name: "local.get",
			line: "local.get 0x02",
			want: New(LOCAL_GET, 2),
		},
		{
			name: "br_table",
			line: "br_table 0x02 0x0000 0x0001 0x0000",
			want: New(BR_TABLE, 2, 0, 1, 0),
		},
		{
			name: "br_table zero count",
			line: "br_table 0x00 0x0000",
			want: New(BR_TABLE, 0, 0),
		},
		{
			name: "offset prefix tab",
			line: "0000:\ti32.const 0x00000001",
			want: New(I32_CONST, 1),
		},
		{
			name: "offset prefix spaces",
			line: "0010:   i32.add",
			want: New(I32_ADD),
		},
		{
			name: "blank line",
			line: "",
			want: nil,
		},
		{
			name: "whitespace only",
			line: "   ",
			want: nil,
		},
		{
			name:    "unknown mnemonic",
			line:    "i32.unknown",
			wantErr: true,
		},
		{
			name:    "missing operand",
			line:    "i32.const",
			wantErr: true,
		},
		{
			name:    "extra operand",
			line:    "nop 0x01",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
	t.Run("basic", func(t *testing.T) {
		instrs, err := ParseAll("i32.const 1\ni32.const 2\ni32.add")
		require.NoError(t, err)
		require.Equal(t, []Instruction{
			New(I32_CONST, 1),
			New(I32_CONST, 2),
			New(I32_ADD),
		}, instrs)
	})

	t.Run("blank lines skipped", func(t *testing.T) {
		instrs, err := ParseAll("\ni32.const 1\n\ni32.add\n")
		require.NoError(t, err)
		require.Equal(t, []Instruction{
			New(I32_CONST, 1),
			New(I32_ADD),
		}, instrs)
	})

	t.Run("error propagates", func(t *testing.T) {
		_, err := ParseAll("i32.const 1\nbad.op\ni32.add")
		require.Error(t, err)
	})

	t.Run("round-trip with Disassemble", func(t *testing.T) {
		original := []Instruction{
			New(I32_CONST, 1),
			New(I32_CONST, 2),
			New(I32_ADD),
		}
		disasm := Disassemble(Marshal(original))
		parsed, err := ParseAll(disasm)
		require.NoError(t, err)
		require.Equal(t, original, parsed)
	})

	t.Run("round-trip br_table", func(t *testing.T) {
		original := []Instruction{
			New(BR_TABLE, 2, 0, 1, 0),
		}
		disasm := Disassemble(Marshal(original))
		parsed, err := ParseAll(disasm)
		require.NoError(t, err)
		require.Equal(t, original, parsed)
	})
}
