package instr_test

import (
	"encoding/binary"
	"math"
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
		mask := uint64(math.MaxUint64)
		if w := max(width, -width); w < 8 {
			mask = 1<<(8*w) - 1
		}
		if width > 0 {
			operands = append(operands, read(offset)&mask)
			offset += width
			continue
		}
		count := 0
		if len(data) > 0 {
			count = int(data[offset%len(data)] % 5)
		}
		operands = append(operands, uint64(count))
		for i := 0; i < count; i++ {
			operands = append(operands, read(offset+i*(-width))&mask)
		}
		offset += 1 + count*(-width)
	}
	return instr.MustNew(op, operands...)
}

func TestMarshal(t *testing.T) {
	insts := []instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_ADD)}
	code := instr.Marshal(insts)
	require.Equal(t, []byte{
		byte(instr.I32_CONST), 1, 0, 0, 0,
		byte(instr.I32_CONST), 2, 0, 0, 0,
		byte(instr.I32_ADD),
	}, code)
}

func TestTargets(t *testing.T) {
	t.Run("branch", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.Br(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{4}, instr.Targets(instr.Marshal(instrs), 0))
	})

	t.Run("conditional branch", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.BrIf(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{4}, instr.Targets(instr.Marshal(instrs), 0))
	})

	t.Run("branch table", func(t *testing.T) {
		b := instr.NewBuilder()
		first, second, def := b.Label(), b.Label(), b.Label()
		b.BrTable(def, first, second).
			Bind(first).Emit(instr.NOP).
			Bind(second).Emit(instr.NOP).
			Bind(def).Emit(instr.RETURN)
		instrs, err := b.Assemble()
		require.NoError(t, err)

		require.Equal(t, []int{8, 9, 10}, instr.Targets(instr.Marshal(instrs), 0))
	})

	t.Run("non branch", func(t *testing.T) {
		require.Nil(t, instr.Targets(instr.Marshal([]instr.Instruction{instr.MustNew(instr.NOP)}), 0))
	})
}

func TestFormat(t *testing.T) {
	t.Run("no branches", func(t *testing.T) {
		insts := []instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_ADD)}
		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n", assembly)
	})

	t.Run("forward branch gets a label", func(t *testing.T) {
		b := instr.NewBuilder()
		end := b.Label()
		b.Br(end).Emit(instr.NOP).Bind(end).Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\tbr L0004\n0003:\tnop\nL0004:\n0004:\treturn\n", assembly)
	})

	t.Run("backward branch gets a label", func(t *testing.T) {
		b := instr.NewBuilder()
		loop := b.Label()
		b.Bind(loop).Emit(instr.NOP).Br(loop)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "L0000:\n0000:\tnop\n0001:\tbr L0000\n", assembly)
	})

	t.Run("br_table renders count first with case labels", func(t *testing.T) {
		b := instr.NewBuilder()
		zero, one, def := b.Label(), b.Label(), b.Label()
		b.BrTable(def, zero, one).
			Bind(zero).Emit(instr.NOP).
			Bind(one).Emit(instr.NOP).
			Bind(def).Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\tbr_table 0x02 L0008 L0009 L0010\nL0008:\n0008:\tnop\nL0009:\n0009:\tnop\nL0010:\n0010:\treturn\n", assembly)
	})

	t.Run("end of code is a label site", func(t *testing.T) {
		// A loop exit branches one past the last instruction to leave the code,
		// and program/verify.go accepts that offset, so it earns a label like
		// any other boundary.
		b := instr.NewBuilder()
		done := b.Label()
		b.Emit(instr.I32_CONST, 1).BrIf(done).Emit(instr.NOP).Bind(done)
		insts, err := b.Assemble()
		require.NoError(t, err)

		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\ti32.const 0x00000001\n0005:\tbr_if L0009\n0008:\tnop\nL0009:\n", assembly)
	})

	t.Run("target past the end falls back to numeric", func(t *testing.T) {
		// next is 8 and the code is 8 bytes, so 0x0001 lands at 9 -- past the
		// end, on no boundary at all -- while 0x0000 lands exactly at the end.
		insts := []instr.Instruction{instr.MustNew(instr.BR_TABLE, 2, 0, 1, 0)}
		assembly := instr.Format(instr.Marshal(insts))
		require.Equal(t, "0000:\tbr_table 0x02 L0008 0x0001 L0008\nL0008:\n", assembly)
	})
}

func TestUnmarshal(t *testing.T) {
	insts := []instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_ADD)}
	actual := instr.Unmarshal(instr.Marshal(insts))
	require.Equal(t, insts, actual)
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
