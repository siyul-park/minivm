package program

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Run("round trip preserves code", func(t *testing.T) {
		p0 := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		p1, err := Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Code, p1.Code)
	})

	t.Run("round trip preserves constants", func(t *testing.T) {
		p0 := New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
					Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
					MustBuild(),
			),
		)
		p1, err := Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Constants, p1.Constants)
	})

	t.Run("round trip preserves types", func(t *testing.T) {
		p0 := New(
			nil,
			WithTypes(types.NewArrayType(types.TypeI32), types.NewStructType(
				types.NewStructField(types.TypeI32),
				types.NewStructField(types.TypeF64),
			)),
		)
		p1, err := Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Types, p1.Types)
	})

	t.Run("round trip preserves handlers", func(t *testing.T) {
		p0 := New(
			[]instr.Instruction{instr.New(instr.NOP)},
			WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 10, Depth: 0}),
		)
		p1, err := Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Handlers, p1.Handlers)
	})

	t.Run("round trip preserves all sections", func(t *testing.T) {
		p0 := New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			WithLocals(types.TypeI32),
			WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI32},
					Returns: []types.Type{types.TypeI64},
				}).
					WithLocals(types.TypeI32).
					Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)).
					MustBuild(),
			),
			WithTypes(types.NewArrayType(types.TypeI32)),
			WithHandlers(instr.Handler{Start: 0, End: 10, Catch: 20, Depth: 1}),
		)
		p1, err := Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Code, p1.Code)
		require.Equal(t, p0.Locals, p1.Locals)
		require.Equal(t, p0.Constants, p1.Constants)
		require.Equal(t, p0.Types, p1.Types)
		require.Equal(t, p0.Handlers, p1.Handlers)
	})

	t.Run("canonical section order", func(t *testing.T) {
		p0 := New(
			[]instr.Instruction{instr.New(instr.NOP)},
			WithLocals(types.TypeI32),
			WithConstants(types.I32(42)),
			WithTypes(types.TypeI64),
			WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 10, Depth: 0}),
		)
		output := p0.String()
		require.Contains(t, output, ".code\n")
		require.Contains(t, output, ".locals\n")
		require.Contains(t, output, ".constants\n")
		require.Contains(t, output, ".types\n")
		require.Contains(t, output, ".handlers\n")

		codeIdx := strings.Index(output, ".code")
		localsIdx := strings.Index(output, ".locals")
		constantsIdx := strings.Index(output, ".constants")
		typesIdx := strings.Index(output, ".types")
		handlersIdx := strings.Index(output, ".handlers")
		require.True(t, codeIdx < localsIdx)
		require.True(t, localsIdx < constantsIdx)
		require.True(t, constantsIdx < typesIdx)
		require.True(t, typesIdx < handlersIdx)
	})

	t.Run("rejects unknown section", func(t *testing.T) {
		_, err := Parse(strings.NewReader(".code\n.unknown\n0000:\tnop\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown section")
	})

	t.Run("rejects duplicate section", func(t *testing.T) {
		_, err := Parse(strings.NewReader(".code\n.code\n0000:\tnop\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate section")
	})

	t.Run("parse error includes section and line", func(t *testing.T) {
		_, err := Parse(strings.NewReader(".code\n0000:\tbad_opcode\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), ".code")
	})

	t.Run("accepts legacy format (code only)", func(t *testing.T) {
		p1, err := Parse(strings.NewReader("0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n"))
		require.NoError(t, err)
		require.Equal(t, 3, len(instr.Unmarshal(p1.Code)))
	})

	t.Run("accepts legacy format with constants and types", func(t *testing.T) {
		input := "0000:\tconst.get 0\n0005:\tcall\n\n0000:\tfunc() i32\n\t0000:\ti32.const 0x0000002A\n\t0005:\treturn\n\n0000:\t[]i32\n"
		p1, err := Parse(strings.NewReader(input))
		require.NoError(t, err)
		require.Equal(t, 2, len(instr.Unmarshal(p1.Code)))
		require.Equal(t, 1, len(p1.Constants))
		require.Equal(t, 1, len(p1.Types))
	})

	t.Run("long code line", func(t *testing.T) {
		parsed, err := Parse(strings.NewReader("i32.const" + strings.Repeat(" ", 70_000) + "1\n"))
		require.NoError(t, err)
		require.Equal(t, New([]instr.Instruction{instr.New(instr.I32_CONST, 1)}).String(), parsed.String())
	})

	t.Run("oversized code line", func(t *testing.T) {
		_, err := Parse(strings.NewReader("i32.const " + strings.Repeat(" ", maxParseLineBytes) + "1\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds maximum allowed size")
	})
}
