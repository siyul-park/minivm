package program_test

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

const parseLineLimit = 1 << 20

func TestParse(t *testing.T) {
	t.Run("round trip preserves code", func(t *testing.T) {
		p0 := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		p1, err := program.Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Code, p1.Code)
	})

	t.Run("round trip preserves constants", func(t *testing.T) {
		p0 := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
					Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
					MustBuild(),
			),
		)
		p1, err := program.Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Constants, p1.Constants)
	})

	t.Run("round trip preserves types", func(t *testing.T) {
		p0 := program.New(
			nil,
			program.WithTypes(types.NewArrayType(types.TypeI32), types.NewStructType(
				types.NewStructField(types.TypeI32),
				types.NewStructField(types.TypeF64),
			)),
		)
		p1, err := program.Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Types, p1.Types)
	})

	t.Run("round trip preserves string constants", func(t *testing.T) {
		p0 := program.New(
			nil,
			program.WithConstants(types.String("has  spaces, \"quotes\", a tab\t, a newline\n, and 한글")),
		)
		p1, err := program.Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Constants, p1.Constants)
	})

	t.Run("named struct field colon is not an index prefix", func(t *testing.T) {
		// parseTypes strips the "NNNN:" index Format writes. A struct field
		// name also carries a colon, so a hand-written line without an index
		// must not lose everything up to it.
		src := "\n.types\nstruct {value: i64; left: any}\n.code\n\tnop\n"
		prog, err := program.Parse(strings.NewReader(src))
		require.NoError(t, err)
		require.Len(t, prog.Types, 1)
		require.Equal(t, "struct {value: i64; left: any}", prog.Types[0].String())
	})

	t.Run("round trip preserves named struct fields", func(t *testing.T) {
		p0 := program.New(
			nil,
			program.WithTypes(types.NewStructType(
				types.NewStructField(types.TypeI64, types.FieldWithName("value")),
				types.NewStructField(types.TypeAny, types.FieldWithName("left")),
				types.NewStructField(types.TypeAny),
			)),
		)
		p1, err := program.Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Types, p1.Types)

		st := p1.Types[0].(*types.StructType)
		require.Equal(t, "value", st.Fields[0].Name)
		require.Equal(t, "left", st.Fields[1].Name)
		require.Equal(t, "", st.Fields[2].Name)
	})

	t.Run("round trip preserves handlers", func(t *testing.T) {
		p0 := program.New(
			[]instr.Instruction{instr.New(instr.NOP)},
			program.WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 10, Depth: 0}),
		)
		p1, err := program.Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Handlers, p1.Handlers)
	})

	t.Run("round trip preserves all sections", func(t *testing.T) {
		p0 := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
			program.WithLocals(types.TypeI32),
			program.WithGlobals(types.TypeAny),
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI32},
					Returns: []types.Type{types.TypeI64},
				}).
					Locals(types.TypeI32).
					Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)).
					MustBuild(),
			),
			program.WithTypes(types.NewArrayType(types.TypeI32)),
			program.WithHandlers(instr.Handler{Start: 0, End: 10, Catch: 20, Depth: 1}),
		)
		p1, err := program.Parse(strings.NewReader(p0.String()))
		require.NoError(t, err)
		require.Equal(t, p0.Code, p1.Code)
		require.Equal(t, p0.Locals, p1.Locals)
		require.Equal(t, p0.Globals, p1.Globals)
		require.Equal(t, p0.Constants, p1.Constants)
		require.Equal(t, p0.Types, p1.Types)
		require.Equal(t, p0.Handlers, p1.Handlers)
	})

	t.Run("canonical section order", func(t *testing.T) {
		p0 := program.New(
			[]instr.Instruction{instr.New(instr.NOP)},
			program.WithLocals(types.TypeI32),
			program.WithGlobals(types.TypeAny),
			program.WithConstants(types.I32(42)),
			program.WithTypes(types.TypeI64),
			program.WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 10, Depth: 0}),
		)
		output := p0.String()
		require.Contains(t, output, ".code\n")
		require.Contains(t, output, ".locals\n")
		require.Contains(t, output, ".globals\n")
		require.Contains(t, output, ".constants\n")
		require.Contains(t, output, ".types\n")
		require.Contains(t, output, ".handlers\n")

		codeIdx := strings.Index(output, ".code")
		localsIdx := strings.Index(output, ".locals")
		globalsIdx := strings.Index(output, ".globals")
		constantsIdx := strings.Index(output, ".constants")
		typesIdx := strings.Index(output, ".types")
		handlersIdx := strings.Index(output, ".handlers")
		require.True(t, codeIdx < localsIdx)
		require.True(t, localsIdx < globalsIdx)
		require.True(t, globalsIdx < constantsIdx)
		require.True(t, constantsIdx < typesIdx)
		require.True(t, typesIdx < handlersIdx)
	})

	t.Run("rejects unknown section", func(t *testing.T) {
		_, err := program.Parse(strings.NewReader(".code\n.unknown\n0000:\tnop\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown section")
	})

	t.Run("rejects duplicate section", func(t *testing.T) {
		_, err := program.Parse(strings.NewReader(".code\n.code\n0000:\tnop\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate section")
	})

	t.Run("parse error includes section and line", func(t *testing.T) {
		_, err := program.Parse(strings.NewReader(".code\n0000:\tbad_opcode\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), ".code")
	})

	t.Run("accepts legacy format (code only)", func(t *testing.T) {
		p1, err := program.Parse(strings.NewReader("0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n"))
		require.NoError(t, err)
		require.Equal(t, 3, len(instr.Unmarshal(p1.Code)))
	})

	t.Run("accepts legacy format with constants and types", func(t *testing.T) {
		input := "0000:\tconst.get 0\n0005:\tcall\n\n0000:\tfunc() i32\n\t0000:\ti32.const 0x0000002A\n\t0005:\treturn\n\n0000:\t[]i32\n"
		p1, err := program.Parse(strings.NewReader(input))
		require.NoError(t, err)
		require.Equal(t, 2, len(instr.Unmarshal(p1.Code)))
		require.Equal(t, 1, len(p1.Constants))
		require.Equal(t, 1, len(p1.Types))
	})

	t.Run("long code line", func(t *testing.T) {
		parsed, err := program.Parse(strings.NewReader("i32.const" + strings.Repeat(" ", 70_000) + "1\n"))
		require.NoError(t, err)
		require.Equal(t, program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)}).String(), parsed.String())
	})

	t.Run("code line limit", func(t *testing.T) {
		prefix, suffix := "i32.const ", "1"
		line := prefix + strings.Repeat(" ", parseLineLimit-len(prefix)-len(suffix)-1) + suffix
		parsed, err := program.Parse(strings.NewReader(line))
		require.NoError(t, err)
		require.Equal(t, program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)}).String(), parsed.String())
	})

	t.Run("oversized code line", func(t *testing.T) {
		prefix, suffix := "i32.const ", "1"
		line := prefix + strings.Repeat(" ", parseLineLimit+1-len(prefix)-len(suffix)) + suffix
		_, err := program.Parse(strings.NewReader(line))
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds maximum allowed size")
	})
}
