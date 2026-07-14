package instr

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func FuzzInstructionRoundTrip(f *testing.F) {
	f.Add(byte(I32_CONST), []byte{1, 2, 3, 4})
	f.Add(byte(BR_TABLE), []byte{2, 0, 1, 0})
	f.Add(byte(STRING_ITER), []byte(nil))

	f.Fuzz(func(t *testing.T, code byte, data []byte) {
		if len(data) > 64 {
			t.Skip()
		}
		op := Opcode(code)
		if !Valid(op) {
			t.Skip()
		}

		read := func(offset int) uint64 {
			var raw [8]byte
			for i := range raw {
				if len(data) > 0 {
					raw[i] = data[(offset+i)%len(data)]
				}
			}
			return binary.LittleEndian.Uint64(raw[:])
		}

		var operands []uint64
		offset := 0
		for _, width := range TypeOf(op).Widths {
			if width > 0 {
				operands = append(operands, read(offset))
				offset += width
				continue
			}
			count := 0
			if len(data) > 0 {
				count = int(data[offset%len(data)] % 5)
			}
			operands = append(operands, uint64(count))
			for i := 0; i < count; i++ {
				operands = append(operands, read(offset+i*(-width)))
			}
			offset += 1 + count*(-width)
		}

		inst := New(op, operands...)
		require.NotNil(t, inst)
		require.Equal(t, []Instruction{inst}, Unmarshal(Marshal([]Instruction{inst})))

		parsed, err := Parse(inst.String())
		require.NoError(t, err)
		require.Equal(t, inst, parsed)
	})
}

func FuzzParse(f *testing.F) {
	f.Add("i32.const 42")
	f.Add("br_table 0x02 0x0000 0x0001 0x0000")
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, line string) {
		if len(line) > 4096 {
			t.Skip()
		}
		inst, err := Parse(line)
		if err != nil || inst == nil {
			return
		}
		roundTrip, err := Parse(inst.String())
		require.NoError(t, err)
		require.Equal(t, inst, roundTrip)
	})
}
