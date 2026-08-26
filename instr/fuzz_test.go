package instr_test

import (
	"encoding/binary"
	"strings"
	"testing"

	instr "github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

// fuzzInstruction builds one arbitrary-but-valid instruction for op from the
// fuzz corpus data, filling every operand (including a BR_TABLE case count)
// deterministically so the same (op, data) pair always yields the same
// instruction. Shared by the fuzz targets that need a valid instruction to
// round-trip rather than arbitrary bytes, which would mostly fail to decode.
func fuzzInstruction(op instr.Opcode, data []byte) instr.Instruction {
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
	for _, width := range instr.TypeOf(op).Widths {
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
	return instr.New(op, operands...)
}

func FuzzInstructionRoundTrip(f *testing.F) {
	f.Add(byte(instr.I32_CONST), []byte{1, 2, 3, 4})
	f.Add(byte(instr.BR_TABLE), []byte{2, 0, 1, 0})
	f.Add(byte(instr.STRING_ITER), []byte(nil))

	f.Fuzz(func(t *testing.T, code byte, data []byte) {
		if len(data) > 64 {
			t.Skip()
		}
		op := instr.Opcode(code)
		if !instr.Valid(op) {
			t.Skip()
		}

		inst := fuzzInstruction(op, data)
		require.NotNil(t, inst)
		require.Equal(t, []instr.Instruction{inst}, instr.Unmarshal(instr.Marshal([]instr.Instruction{inst})))

		parsed, err := instr.Parse(inst.String())
		require.NoError(t, err)
		require.Equal(t, inst, parsed)
	})
}

// FuzzFormatRoundTrip checks the contract instr.Format and instr.ParseAll
// exist to serve: ParseAll(Format(code)) must reproduce code exactly,
// whether or not a branch's target lands on a label Format could name.
func FuzzFormatRoundTrip(f *testing.F) {
	f.Add(byte(instr.I32_CONST), []byte{1, 2, 3, 4})
	f.Add(byte(instr.BR), []byte{1, 0})
	f.Add(byte(instr.BR_IF), []byte{0, 0})
	f.Add(byte(instr.BR_TABLE), []byte{2, 0, 1, 0})
	f.Add(byte(instr.STRING_ITER), []byte(nil))

	f.Fuzz(func(t *testing.T, code byte, data []byte) {
		if len(data) > 64 {
			t.Skip()
		}
		op := instr.Opcode(code)
		if !instr.Valid(op) {
			t.Skip()
		}

		inst := fuzzInstruction(op, data)
		require.NotNil(t, inst)

		want := instr.Marshal([]instr.Instruction{inst})
		got, err := instr.ParseAll(strings.NewReader(instr.Format(want)))
		require.NoError(t, err)
		require.Equal(t, want, instr.Marshal(got))
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
		inst, err := instr.Parse(line)
		if err != nil || inst == nil {
			return
		}
		roundTrip, err := instr.Parse(inst.String())
		require.NoError(t, err)
		require.Equal(t, inst, roundTrip)
	})
}

// FuzzParseAll drives whole listings, including malformed ones. Parse covers a
// single line and FuzzFormatRoundTrip only ever feeds ParseAll well-formed
// Format output, so neither reaches the label table or the br_table count that
// only ParseAll interprets. Text is a trust boundary: a bad listing must be an
// error, never a panic.
func FuzzParseAll(f *testing.F) {
	f.Add("i32.const 42\nnop\n")
	f.Add("loop:\n\tbr loop\n")
	f.Add("br_if done\ndone:\n\tnop\n")
	f.Add("br_table 0x02 case0 case1 fallback\ncase0:\ncase1:\nfallback:\n\tnop\n")
	f.Add("br_table 0xFFFFFFFFFFFFFFFF\n")
	f.Add("dup:\ndup:\n\tnop\n")
	f.Add("br missing\n")

	f.Fuzz(func(t *testing.T, listing string) {
		if len(listing) > 4096 {
			t.Skip()
		}
		instrs, err := instr.ParseAll(strings.NewReader(listing))
		if err != nil {
			require.Nil(t, instrs)
			return
		}
		// Anything ParseAll accepts must re-render and re-parse to the same code.
		code := instr.Marshal(instrs)
		again, err := instr.ParseAll(strings.NewReader(instr.Format(code)))
		require.NoError(t, err)
		require.Equal(t, code, instr.Marshal(again))
	})
}
