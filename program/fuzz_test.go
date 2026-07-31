package program_test

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	program "github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func FuzzParseProgram(f *testing.F) {
	f.Add(program.New([]instr.Instruction{instr.New(instr.NOP)}).String())
	f.Add(program.New(
		[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.DROP)}, program.WithConstants(types.String("value")), program.WithLocals(types.TypeI32), program.WithGlobals(types.TypeRef), program.WithTypes(types.NewArrayType(types.TypeI32)),
	).String())
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 64<<10 {
			t.Skip()
		}
		prog, err := program.Parse(strings.NewReader(text))
		if err != nil {
			return
		}
		roundTrip, err := program.Parse(strings.NewReader(prog.String()))
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
		_ = program.Verify(&program.Program{Code: code})
	})
}
