package program

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func FuzzParseProgram(f *testing.F) {
	f.Add(New([]instr.Instruction{instr.New(instr.NOP)}).String())
	f.Add(New(
		[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.DROP)},
		WithConstants(types.String("value")),
		WithLocals(types.TypeI32),
		WithGlobals(types.TypeRef),
		WithTypes(types.NewArrayType(types.TypeI32)),
	).String())
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 64<<10 {
			t.Skip()
		}
		prog, err := Parse(strings.NewReader(text))
		if err != nil {
			return
		}
		roundTrip, err := Parse(strings.NewReader(prog.String()))
		require.NoError(t, err)
		require.Equal(t, prog.String(), roundTrip.String())
	})
}

func FuzzVerify(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{byte(instr.NOP)})
	f.Add([]byte{byte(instr.I32_CONST), 1})
	f.Add([]byte{0xff})

	f.Fuzz(func(t *testing.T, code []byte) {
		if len(code) > 4096 {
			t.Skip()
		}
		_ = Verify(&Program{Code: code})
	})
}
