package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/types"
)

// numeric reports whether code pops and pushes plain numeric values only, with
// no operand, so that a function of constants and code translates alone.
func numeric(code instr.Opcode) bool {
	typ := instr.TypeOf(code)
	if len(typ.Widths) > 0 || len(typ.Push) != 1 || !typ.Push[0].IsNumeric() || len(typ.Pop) == 0 {
		return false
	}
	for _, kind := range typ.Pop {
		if !kind.IsNumeric() {
			return false
		}
	}
	return true
}

func TestPerforms(t *testing.T) {
	t.Run("is performed by exactly the opcode that was translated to the operation", func(t *testing.T) {
		for code := instr.Opcode(0); code < 0xFF; code++ {
			if !instr.Valid(code) || !numeric(code) {
				continue
			}
			name := instr.TypeOf(code).Mnemonic
			out, o := performed(t, code)

			var performers []instr.Opcode
			for other := instr.Opcode(0); other < 0xFF; other++ {
				if instr.Valid(other) && transform.Performs(out, o, other) {
					performers = append(performers, other)
				}
			}

			switch {
			case o.Op == ssa.OpExec:
				require.Equal(t, code, o.Code, name)
				require.Equal(t, []instr.Opcode{code}, performers, name)
			case code == instr.I32_EQZ || code == instr.I64_EQZ:
				require.Equal(t, ssa.OpIntCmp, o.Op, name)
				require.Equal(t, ssa.CondEQ, o.Cond, name)
				require.NotContains(t, performers, code, name)
			default:
				require.Equal(t, []instr.Opcode{code}, performers, name)
			}
		}
	})

	t.Run("gives each operation the effects its opcode declares", func(t *testing.T) {
		for code := instr.Opcode(0); code < 0xFF; code++ {
			if !instr.Valid(code) || !numeric(code) {
				continue
			}
			typ := instr.TypeOf(code)
			_, o := performed(t, code)
			require.Equal(t, ssa.Effect(typ.Reads), o.Reads(), typ.Mnemonic)
			require.Equal(t, ssa.Effect(typ.Writes), o.Writes(), typ.Mnemonic)
		}
	})

	t.Run("lifts a zero compare to int.cmp eq against a zero constant", func(t *testing.T) {
		for _, code := range []instr.Opcode{instr.I32_EQZ, instr.I64_EQZ} {
			out, o := performed(t, code)

			require.Equal(t, ssa.OpIntCmp, o.Op)
			require.Equal(t, ssa.CondEQ, o.Cond)
			require.Len(t, o.Args, 2)
			require.Contains(t, ssa.Format(out), "int.cmp eq")
			require.False(t, transform.Performs(out, o, code))
		}
	})

	t.Run("matches an exec operation by its own opcode only", func(t *testing.T) {
		o := ssa.Operation{Op: ssa.OpExec, Code: instr.F32_REM}

		require.True(t, transform.Performs(nil, o, instr.F32_REM))
		require.False(t, transform.Performs(nil, o, instr.F64_REM))
	})
}

// performed translates a function that applies code to constants and returns
// the function with the operation that produces the result.
func performed(t *testing.T, code instr.Opcode) (*ssa.Function, ssa.Operation) {
	t.Helper()
	pop := instr.TypeOf(code).Pop
	body := assemble(t, func(b *instr.Builder) {
		for i := len(pop) - 1; i >= 0; i-- {
			switch pop[i].Repr() {
			case instr.KindI64:
				b.Emit(instr.I64_CONST, 1)
			case instr.KindF32:
				b.Emit(instr.F32_CONST, 0)
			case instr.KindF64:
				b.Emit(instr.F64_CONST, 0)
			default:
				b.Emit(instr.I32_CONST, 1)
			}
		}
		b.Emit(code).Emit(instr.RETURN)
	})
	fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32()}}, Code: body}

	out, _, err := transform.Translate(transform.Module{}, nil, 1, fn, 0)

	require.NoError(t, err)
	require.NoError(t, ssa.Verify(out))
	var result ssa.Operation
	for _, o := range out.Block(0).Operations {
		if o.Op != ssa.OpState && o.Op != ssa.OpConst && len(o.Results) == 1 {
			result = o
		}
	}
	return out, result
}
