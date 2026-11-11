package transform_test

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type run[U, R any] func(*pass.Manager, U) (R, error)

// sliceFunction returns a function that slices its array param and stores the
// slice to a global of bytes.
func sliceFunction(array types.Type) *types.Function {
	return types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{array, types.TypeI32()},
		Returns: []types.Type{types.TypeI32()}}).Append(
		instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_SLICE),
		instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.SWAP),
		instr.MustNew(instr.GLOBAL_SET, 0), instr.MustNew(instr.RETURN)).MustBuild()
}

func pipeline() *pass.Pipeline[*ssa.Function] {
	pipeline := pass.NewPipeline[*ssa.Function]()
	pipeline.Add(transform.NewFoldPass())
	pipeline.Add(transform.NewPromotePass())
	pipeline.Add(transform.NewCSEPass())
	pipeline.Add(transform.NewGuardPass())
	pipeline.Add(transform.NewLICMPass())
	pipeline.Add(transform.NewDCEPass())
	return pipeline
}

func duplicateConstants(input *program.Program) *program.Program {
	out := *input
	out.Code = append([]byte(nil), input.Code...)
	out.Locals = append([]types.Type(nil), input.Locals...)
	out.Handlers = append([]instr.Handler(nil), input.Handlers...)
	out.Constants = append([]types.Value(nil), input.Constants...)
	for i, v := range out.Constants {
		fn, ok := v.(*types.Function)
		if !ok {
			continue
		}
		copied := *fn
		copied.Code = append([]byte(nil), fn.Code...)
		copied.Locals = append([]types.Type(nil), fn.Locals...)
		copied.Handlers = append([]instr.Handler(nil), fn.Handlers...)
		out.Constants[i] = &copied
	}
	return &out
}

func TestNewSSAPass(t *testing.T) {
	require.NotNil(t, transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false))
	require.NotNil(t, transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), true))
}

func TestSSAPass_Run(t *testing.T) {
	t.Run("a round trip preserves what a program does", func(t *testing.T) {
		assemble := func(emit func(b *program.Builder)) *program.Program {
			b := program.NewBuilder()
			emit(b)
			prog, err := b.Build()
			require.NoError(t, err)
			return prog
		}
		sum := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32(), types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.RETURN)).MustBuild()

		fib := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}})
		base := fib.Label()
		fib.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_LT_S))
		fib.BrIf(base)
		fib.Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
		fib.Bind(base).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN))

		total := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.NewArrayType(types.TypeI32())},
			Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32(), types.TypeI32())
		header, done := total.Label(), total.Label()
		total.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1))
		total.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2))
		total.Bind(header)
		total.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_LE_S))
		total.BrIf(done)
		total.Append(
			instr.MustNew(instr.LOCAL_GET, 1),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.ARRAY_GET),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2))
		total.Br(header)
		total.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))
		for name, prog := range map[string]*program.Program{
			"constants": program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.I32_CONST, 22), instr.MustNew(instr.I32_ADD)}),
			"stack shuffles": program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.I32_CONST, 3),
				instr.MustNew(instr.SWAP), instr.MustNew(instr.DUP), instr.MustNew(instr.DROP), instr.MustNew(instr.I32_SUB)}),
			"select": program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.SELECT)}),
			"globals": program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.GLOBAL_SET, 0),
				instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.I32_ADD)}, program.WithGlobals(types.TypeI32())),
			"locals": program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_TEE, 0),
				instr.MustNew(instr.LOCAL_SET, 0), instr.MustNew(instr.LOCAL_GET, 0)}, program.WithLocals(types.TypeI32())),
			"constantFunction array": program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.ARRAY_GET)}, program.WithConstants(types.TypedArray[int32]{10, 20, 30})),
			"constantFunction string": program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.STRING_LEN)}, program.WithConstants(types.String("hello"))),
			"recursion": program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 12), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(fib.MustBuild())),
			"array loop": program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(total.MustBuild(), types.TypedArray[int32]{1, 2, 3, 4})),
			"call": program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 4),
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(sum)),
			"branch": assemble(func(b *program.Builder) {
				other, done := b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 1).BrIf(other)
				b.Emit(instr.I32_CONST, 2).Br(done)
				b.Bind(other).Emit(instr.I32_CONST, 3)
				b.Bind(done)
			}),
			"branch table": assemble(func(b *program.Builder) {
				one, two, done := b.Label(), b.Label(), b.Label()
				b.Locals(types.TypeI32())
				b.Emit(instr.I32_CONST, 1).BrTable(done, one, two)
				b.Bind(one).Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 0).Br(done)
				b.Bind(two).Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 0)
				b.Bind(done).Emit(instr.LOCAL_GET, 0)
			}),
			"loop": assemble(func(b *program.Builder) {
				header, done := b.Label(), b.Label()
				b.Locals(types.TypeI32())
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
				b.Bind(header)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
				b.Br(header)
				b.Bind(done).Emit(instr.LOCAL_GET, 0)
			}),
			"branch past the end": assemble(func(b *program.Builder) {
				done := b.Label()
				b.Locals(types.TypeI32())
				b.Emit(instr.I32_CONST, 1).BrIf(done)
				b.Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 0)
				b.Bind(done)
			})} {
			require.NoError(t, program.Verify(prog))
			want, wantErr := executionResult(t, prog)

			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))

			values, message := executionResult(t, got)
			require.Equal(t, wantErr, message, name)
			require.Equal(t, want, values, name)
		}
	})

	t.Run("a round trip preserves what a generatedProgram program does", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		rewritten := 0
		for range 2000 {
			prog := generatedProgram(t, rnd)
			require.NoError(t, program.Verify(prog))
			want, wantErr := executionResult(t, prog)

			got := duplicateConstants(prog)
			preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			if !preserved {
				rewritten++
			}

			values, message := executionResult(t, got)
			require.Equal(t, wantErr, message)
			require.Equal(t, want, values)
		}
		require.NotZero(t, rewritten)
	})

	t.Run("the SSA passes preserve what a generatedProgram program does", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		rewritten := 0
		for range 2000 {
			prog := generatedProgram(t, rnd)
			require.NoError(t, program.Verify(prog))
			want, wantErr := executionResult(t, prog)

			got := duplicateConstants(prog)
			preserved, err := transform.NewSSAPass(pipeline(), false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			if !preserved {
				rewritten++
			}

			values, message := executionResult(t, got)
			require.Equal(t, wantErr, message)
			require.Equal(t, want, values)
		}
		require.NotZero(t, rewritten)
	})

	t.Run("folds a window every pure opcode computes from constants alone", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewFoldPass())
		pipeline.Add(transform.NewDCEPass())

		folded := map[instr.Opcode]bool{}
		push := func(kind instr.Kind, v float64) (instr.Instruction, bool) {
			switch kind {
			case instr.KindI1, instr.KindI8, instr.KindI32, instr.KindAny:
				return instr.MustNew(instr.I32_CONST, uint64(uint32(int32(v)))), true
			case instr.KindI64:
				return instr.MustNew(instr.I64_CONST, uint64(int64(v))), true
			case instr.KindF32:
				return instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(float32(v)))), true
			case instr.KindF64:
				return instr.MustNew(instr.F64_CONST, math.Float64bits(v)), true
			case instr.KindRef:
				return instr.MustNew(instr.REF_NULL), true
			default:
				return nil, false
			}
		}
		windowOf := func(op instr.Opcode, right float64) ([]instr.Instruction, bool) {
			typ := instr.TypeOf(op)
			if len(typ.Widths) > 0 || len(typ.Pop) == 0 {
				return nil, false
			}
			code := make([]instr.Instruction, 0, len(typ.Pop)+1)
			for i := len(typ.Pop) - 1; i >= 0; i-- {
				value := 6.0
				if i == 0 {
					value = right
				}
				inst, ok := push(typ.Pop[i], value)
				if !ok {
					return nil, false
				}
				code = append(code, inst)
			}
			return append(code, instr.MustNew(op)), true
		}

		var out []struct {
			op   instr.Opcode
			code []instr.Instruction
		}
		for op := instr.Opcode(0); op < math.MaxUint8; op++ {
			if !instr.Valid(op) || !op.IsPure() {
				continue
			}
			for _, right := range []float64{3, 0} {
				if code, ok := windowOf(op, right); ok {
					out = append(out, struct {
						op   instr.Opcode
						code []instr.Instruction
					}{op: op, code: code})
				}
			}
		}
		require.NotEmpty(t, out)
		for _, window := range out {
			prog := program.New(window.code)
			require.NoError(t, program.Verify(prog))
			values, message := executionResult(t, prog)

			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			if len(got.Code) < len(prog.Code) {
				folded[window.op] = true
			}

			routed, routedMessage := executionResult(t, got)
			require.Equal(t, message, routedMessage)
			require.Equal(t, values, routed)
		}
		for _, op := range []instr.Opcode{
			instr.I32_XOR, instr.I32_AND, instr.I32_OR,
			instr.I64_XOR, instr.I64_AND, instr.I64_OR} {
			require.True(t, folded[op])
		}
	})

	t.Run("folds a wide i64.shl into one i64.const immediate", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewFoldPass())

		got := program.New([]instr.Instruction{
			instr.MustNew(instr.I64_CONST, 1), instr.MustNew(instr.I64_CONST, 50),
			instr.MustNew(instr.I64_SHL)})
		require.NoError(t, program.Verify(got))
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		require.NotContains(t, instr.Format(got.Code), "i64.shl")
		require.Contains(t, instr.Format(got.Code), fmt.Sprintf("i64.const 0x%016X", uint64(1)<<50))
	})

	t.Run("emits a const.get of a wide i64 pool cell as i64.const", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()

		wide := int64(-3750763034362895579)
		got := program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.I64_EQZ), instr.MustNew(instr.DROP)},
			program.WithConstants(types.I64(wide)))
		require.NoError(t, program.Verify(got))
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		require.NotContains(t, instr.Format(got.Code), "const.get")
		require.Contains(t, instr.Format(got.Code), fmt.Sprintf("i64.const 0x%016X", uint64(wide)))
	})

	t.Run("drops the padding an offset-preserving rewrite left behind", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewDCEPass())

		got := program.New([]instr.Instruction{instr.MustNew(instr.NOP), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.NOP)})
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		require.Equal(t, instr.Format(instr.Marshal([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1)})), instr.Format(got.Code))
	})

	t.Run("drops a computation nothing reads", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewDCEPass())

		got := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.DROP)})
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		require.Empty(t, got.Code)
	})

	t.Run("drops a block nothing reaches", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewDCEPass())
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}})
		done := fn.Label()
		fn.Append(instr.MustNew(instr.I32_CONST, 1))
		fn.Br(done)
		fn.Append(instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.DROP))
		fn.Bind(done).Append(instr.MustNew(instr.RETURN))
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(fn.MustBuild()))
		require.NoError(t, program.Verify(prog))

		want, wantErr := executionResult(t, prog)
		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		require.NotContains(t, instr.Format(got.Constants[0].(*types.Function).Code), "i32.const 0x00000002")

		values, message := executionResult(t, got)
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("eliminates a repeated computation over a repeated load", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewForwardPass())
		pipeline.Add(transform.NewCSEPass())
		pipeline.Add(transform.NewDCEPass())

		reloaded := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32(), types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild()
		shared := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_MUL),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_MUL),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 4),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL)}, program.WithConstants(reloaded, shared))

		want, wantErr := executionResult(t, prog)
		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))

		first := got.Constants[0].(*types.Function)
		require.NotEqual(t, instr.Format(reloaded.Code), instr.Format(first.Code))
		require.Len(t, first.Locals, 1)
		require.Equal(t, 2, strings.Count(instr.Format(first.Code), "i32.add"))

		second := got.Constants[1].(*types.Function)
		require.NotEqual(t, instr.Format(shared.Code), instr.Format(second.Code))
		require.Len(t, second.Locals, 1)
		require.Equal(t, 1, strings.Count(instr.Format(second.Code), "i32.mul"))

		values, message := executionResult(t, got)
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("carries a promoted loop counter through the round trip", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewPromotePass())
		pipeline.Add(transform.NewDCEPass())

		counting := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32())
		header, done := counting.Label(), counting.Label()
		counting.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1))
		counting.Bind(header)
		counting.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
		counting.BrIf(done)
		counting.Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.LOCAL_SET, 1))
		counting.Br(header)
		counting.Bind(done)
		counting.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))
		fn := counting.MustBuild()

		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 6), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(fn))

		want, wantErr := executionResult(t, prog)
		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))

		counted := got.Constants[0].(*types.Function)
		require.NotEqual(t, instr.Format(fn.Code), instr.Format(counted.Code))

		values, message := executionResult(t, got)
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("re-emits an array loop whose hoisted guard slices the array", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewPromotePass())
		pipeline.Add(transform.NewLICMPass())
		pipeline.Add(transform.NewDCEPass())

		total := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.NewArrayType(types.TypeI32())},
			Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32(), types.TypeI32())
		header, done := total.Label(), total.Label()
		total.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1))
		total.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2))
		total.Bind(header)
		total.Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.I32_GE_S))
		total.BrIf(done)
		total.Append(
			instr.MustNew(instr.LOCAL_GET, 1),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.ARRAY_GET),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2))
		total.Br(header)
		total.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))
		fn := total.MustBuild()

		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(fn, types.TypedArray[int32]{1, 2, 3, 4}))

		want, wantErr := executionResult(t, prog)
		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))

		require.NotEqual(t, instr.Format(fn.Code), instr.Format(got.Constants[0].(*types.Function).Code))

		values, message := executionResult(t, got)
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("declines a branch its own layout would put out of range", func(t *testing.T) {
		// The emitted branch spans the filler and the tail, whose dup becomes a second constant.
		four := []instr.Instruction{instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_EQZ), instr.MustNew(instr.DROP)}
		five := []instr.Instruction{instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_EQZ), instr.MustNew(instr.I32_EQZ), instr.MustNew(instr.DROP)}
		tail := []instr.Instruction{instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.DUP), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.DROP)}
		tailWidth := 0
		for _, in := range tail {
			tailWidth += in.Width()
		}
		tailWidth += instr.MustNew(instr.I32_CONST, 7).Width() - instr.MustNew(instr.DUP).Width()

		for _, tc := range []struct {
			name    string
			emitted int
			expects bool
		}{
			{name: "within reach", emitted: math.MaxInt16, expects: false},
			{name: "out of reach", emitted: math.MaxInt16 + 1, expects: true}} {
			fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32())
			done := fn.Label()
			fn.Append(instr.MustNew(instr.I32_CONST, 1))
			fn.BrIf(done)
			filler := tc.emitted - tailWidth
			fives := filler % 4
			for range (filler - 5*fives) / 4 {
				fn.Append(four...)
			}
			for range fives {
				fn.Append(five...)
			}
			fn.Append(tail...)
			fn.Bind(done).Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.RETURN))
			span := fn.MustBuild()
			input := program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(span))
			require.NoError(t, program.Verify(input))
			want, wantErr := executionResult(t, input)
			before := input.String()

			preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), input)
			require.NoError(t, err)
			require.Equal(t, tc.expects, preserved, tc.name)
			require.NoError(t, program.Verify(input))
			if preserved {
				require.Equal(t, before, input.String())
			}

			values, message := executionResult(t, input)
			require.Equal(t, wantErr, message)
			require.Equal(t, want, values)
		}
	})

	t.Run("does not invalidate program analyses while a temporary SSA pass runs", func(t *testing.T) {
		calls := 0
		manager := pass.NewManager()
		pass.Register[*program.Program, int](manager, run[*program.Program, int](func(_ *pass.Manager, prog *program.Program) (int, error) {
			calls++
			return len(prog.Code), nil
		}))
		prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1)})
		_, err := pass.GetResult[int](manager, prog)
		require.NoError(t, err)

		inner := pass.NewPipeline[*ssa.Function]()
		inner.Add(run[*ssa.Function, bool](func(*pass.Manager, *ssa.Function) (bool, error) {
			return false, nil
		}))
		outer := pass.NewPipeline[*program.Program]()
		outer.Add(transform.NewSSAPass(inner, false))
		_, err = outer.Run(manager, prog)
		require.NoError(t, err)

		_, err = pass.GetResult[int](manager, prog)
		require.NoError(t, err)
		require.Equal(t, 1, calls)
	})

	t.Run("rematerializes constants and unstored loads and falls through a branch", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		run := func(prog *program.Program, opts ...interp.Option) types.Value {
			vm := interp.MustNew(prog, opts...)
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.Pop()
			require.NoError(t, err)
			return value
		}

		fib := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}})
		base := fib.Label()
		fib.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_LT_S))
		fib.BrIf(base)
		fib.Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_SUB),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
		fib.Bind(base).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN))

		// n counts down inside the loop: its load must not be reused after the store.
		countdown := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32())
		header, done := countdown.Label(), countdown.Label()
		countdown.Bind(header).Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_LE_S))
		countdown.BrIf(done)
		countdown.Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_SUB), instr.MustNew(instr.LOCAL_SET, 0))
		countdown.Br(header)
		countdown.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))

		call := func(arg int32, f *types.Function) *program.Program {
			return program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, uint64(arg)), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(f))
		}

		t.Run("a recursive call keeps its constant callee and no value is homed", func(t *testing.T) {
			f := fib.MustBuild()
			prog := call(20, f)
			_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), prog)
			require.NoError(t, err)
			require.NoError(t, program.Verify(prog))

			got := prog.Constants[0].(*types.Function)
			require.Empty(t, got.Locals)
			var ops []instr.Opcode
			for _, inst := range instr.Unmarshal(got.Code) {
				ops = append(ops, inst.Opcode())
			}
			require.NotContains(t, ops, instr.LOCAL_TEE)
			require.NotContains(t, ops, instr.LOCAL_SET)
			calls := 0
			for i, op := range ops {
				if op == instr.CALL {
					calls++
					require.Equal(t, instr.CONST_GET, ops[i-1])
				}
				if op == instr.BR_IF {
					require.NotEqual(t, instr.BR, ops[i+1])
				}
			}
			require.Equal(t, 2, calls)
			require.Equal(t, types.I32(6765), run(prog, interp.WithThreshold(-1)))
			require.Equal(t, types.I32(6765), run(prog))
		})

		t.Run("a load a loop store can reach stays homed", func(t *testing.T) {
			for name, pipeline := range map[string]*pass.Pipeline[*ssa.Function]{
				"identity":  pass.NewPipeline[*ssa.Function](),
				"optimized": optimized,
			} {
				prog := call(10, countdown.MustBuild())
				_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), prog)
				require.NoError(t, err, name)
				require.NoError(t, program.Verify(prog), name)
				require.Equal(t, types.I32(55), run(prog, interp.WithThreshold(-1)), name)
				require.Equal(t, types.I32(55), run(prog), name)
			}
		})
	})

	t.Run("keeps a load homed when a store reaches it before a later use", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		params := &types.FunctionType{
			Params:  []types.Type{types.TypeI32(), types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}

		sameBlock := types.NewFunctionBuilder(params).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.DUP), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild()

		crossBlock := types.NewFunctionBuilder(params)
		taken := crossBlock.Label()
		crossBlock.Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_SET, 0),
			instr.MustNew(instr.LOCAL_GET, 1))
		crossBlock.BrIf(taken)
		crossBlock.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
		crossBlock.Bind(taken).Append(instr.MustNew(instr.I32_CONST, 100), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))

		for _, tc := range []struct {
			name string
			f    *types.Function
			flag uint64
			want types.Value
		}{
			{name: "the same block", f: sameBlock, flag: 0, want: types.I32(6)},
			{name: "a later block", f: crossBlock.MustBuild(), flag: 1, want: types.I32(103)},
			{name: "a fallthrough block", f: crossBlock.MustBuild(), flag: 0, want: types.I32(3)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				for name, pipeline := range map[string]*pass.Pipeline[*ssa.Function]{"identity": pass.NewPipeline[*ssa.Function](), "optimized": optimized} {
					prog := program.New([]instr.Instruction{
						instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, tc.flag),
						instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(tc.f))
					_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), prog)
					require.NoError(t, err)
					require.NoError(t, program.Verify(prog))
					for _, opts := range [][]interp.Option{{interp.WithThreshold(-1)}, nil} {
						values, message := executionResult(t, prog, opts...)
						require.Empty(t, message, tc.name+" "+name)
						require.Equal(t, []types.Value{tc.want}, values, tc.name+" "+name)
					}
				}
			})
		}
	})

	t.Run("inverts a branch only through an exact comparison inverse", func(t *testing.T) {
		kinds := map[instr.Kind]struct {
			typ   types.Type
			konst instr.Opcode
			words []uint64
		}{
			instr.KindI32: {types.TypeI32(), instr.I32_CONST, []uint64{
				1 << 31, 0xFFFFFFFE, 0xFFFFFFFF, 0, 1, 2, math.MaxInt32}},
			instr.KindI64: {types.TypeI64(), instr.I64_CONST, []uint64{
				1 << 63, math.MaxUint64 - 1, math.MaxUint64, 0, 1, 2, math.MaxInt64}},
			instr.KindF32: {types.TypeF32(), instr.F32_CONST, []uint64{
				uint64(math.Float32bits(-1)), uint64(math.Float32bits(0)), uint64(math.Float32bits(1)), uint64(math.Float32bits(float32(math.NaN())))}},
			instr.KindF64: {types.TypeF64(), instr.F64_CONST, []uint64{
				math.Float64bits(-1), math.Float64bits(0), math.Float64bits(1), math.Float64bits(math.NaN())}},
		}
		inverses := map[instr.Opcode]instr.Opcode{}
		for _, pair := range [][2]instr.Opcode{
			{instr.I32_EQ, instr.I32_NE}, {instr.I32_LT_S, instr.I32_GE_S}, {instr.I32_GT_S, instr.I32_LE_S},
			{instr.I32_LT_U, instr.I32_GE_U}, {instr.I32_GT_U, instr.I32_LE_U},
			{instr.I64_EQ, instr.I64_NE}, {instr.I64_LT_S, instr.I64_GE_S}, {instr.I64_GT_S, instr.I64_LE_S},
			{instr.I64_LT_U, instr.I64_GE_U}, {instr.I64_GT_U, instr.I64_LE_U},
			{instr.F32_EQ, instr.F32_NE}, {instr.F64_EQ, instr.F64_NE}} {
			inverses[pair[0]], inverses[pair[1]] = pair[1], pair[0]
		}
		// Ordered float comparisons are false for NaN, so their complement is no inverse.
		ops := []instr.Opcode{instr.F32_LT, instr.F32_GE, instr.F64_LT, instr.F64_LE, instr.F64_GT, instr.F64_GE}
		for op := range inverses {
			ops = append(ops, op)
		}
		for _, op := range ops {
			kind := kinds[instr.TypeOf(op).Pop[0]]
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{kind.typ, kind.typ},
				Returns: []types.Type{types.TypeI32()}})
			taken := fn.Label()
			fn.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(op))
			fn.BrIf(taken)
			fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN))
			fn.Bind(taken).Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.RETURN))
			f := fn.MustBuild()

			rewritten := duplicateConstants(program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.DROP)}, program.WithConstants(f)))
			_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), rewritten)
			require.NoError(t, err)
			var got []instr.Opcode
			for _, inst := range instr.Unmarshal(rewritten.Constants[0].(*types.Function).Code) {
				got = append(got, inst.Opcode())
			}
			if inverse, ok := inverses[op]; ok {
				require.Contains(t, got, inverse, instr.TypeOf(op).Mnemonic)
				require.NotContains(t, got, op, instr.TypeOf(op).Mnemonic)
			} else {
				require.Contains(t, got, op, instr.TypeOf(op).Mnemonic)
			}

			for _, a := range kind.words {
				for _, b := range kind.words {
					call := program.New([]instr.Instruction{
						instr.MustNew(kind.konst, a), instr.MustNew(kind.konst, b),
						instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(f))
					want, wantErr := executionResult(t, call, interp.WithThreshold(-1))
					call.Constants[0] = rewritten.Constants[0]
					values, message := executionResult(t, call, interp.WithThreshold(-1))
					require.Equal(t, wantErr, message)
					require.Equal(t, want, values, fmt.Sprintf("%s %#x %#x", instr.TypeOf(op).Mnemonic, a, b))
				}
			}
		}
	})

	t.Run("leaves the comparison of an earlier block alone", func(t *testing.T) {
		// The branch tests a value two paths join with: only one of them is a comparison.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32(), types.TypeI32(), types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}})
		join, compare, taken := fn.Label(), fn.Label(), fn.Label()
		fn.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_EQ), instr.MustNew(instr.LOCAL_GET, 2))
		fn.BrIf(compare)
		fn.Bind(join)
		fn.BrIf(taken)
		fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN))
		fn.Bind(compare).Append(instr.MustNew(instr.DROP), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_LT_S))
		fn.Br(join)
		fn.Bind(taken).Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.RETURN))
		f := fn.MustBuild()

		for _, args := range [][3]uint64{{1, 2, 0}, {2, 1, 0}, {1, 2, 1}, {2, 1, 1}, {1, 2, 0}, {1, 1, 0}} {
			input := func(f *types.Function) *program.Program {
				return program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, args[0]), instr.MustNew(instr.I32_CONST, args[1]), instr.MustNew(instr.I32_CONST, args[2]),
					instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(f))
			}
			prog := input(f)
			require.NoError(t, program.Verify(prog))
			want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))

			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			values, message := executionResult(t, got, interp.WithThreshold(-1))
			require.Equal(t, wantErr, message)
			require.Equal(t, want, values, args)
		}
	})

	t.Run("homes loop-carried values in the locals the loop already had", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		run := func(prog *program.Program) []types.Value {
			threaded, message := executionResult(t, prog, interp.WithThreshold(-1), interp.WithFuel(1<<24))
			require.Empty(t, message)
			native, message := executionResult(t, prog, interp.WithFuel(1<<24))
			require.Empty(t, message)
			require.Equal(t, threaded, native)
			return threaded
		}

		fnv := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI64()}}).Locals(types.TypeI64(), types.TypeI32())
		header, done := fnv.Label(), fnv.Label()
		fnv.Append(instr.MustNew(instr.I64_CONST, 0xCBF29CE484222325), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2))
		fnv.Bind(header).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
		fnv.BrIf(done)
		fnv.Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 255),
			instr.MustNew(instr.I32_AND), instr.MustNew(instr.I32_TO_I64_S), instr.MustNew(instr.I64_XOR),
			instr.MustNew(instr.I64_CONST, 0x00000100000001B3), instr.MustNew(instr.I64_MUL), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2))
		fnv.Br(header)
		fnv.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))

		// Three i32 values travel around the loop at once: any two sharing a local would corrupt the sequence.
		fib := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32(), types.TypeI32(), types.TypeI32())
		loop, end := fib.Label(), fib.Label()
		fib.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 3))
		fib.Bind(loop).Append(instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
		fib.BrIf(end)
		fib.Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_SET, 1), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 3))
		fib.Br(loop)
		fib.Bind(end).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))

		// The emitter declines a loop a branch closes; the case pins that it still runs the same.
		sum := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32(), types.TypeI32())
		again := sum.Label()
		sum.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2))
		sum.Bind(again).Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_LT_S))
		sum.BrIf(again)
		sum.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))

		// Values that trade places each iteration: the back edge moves them in parallel.
		trade := func(count int, order ...int) *types.Function {
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}})
			slots := make([]types.Type, count+2)
			for i := range slots {
				slots[i] = types.TypeI32()
			}
			fn.Locals(slots...)
			counter := uint64(count + 2)
			head, exit := fn.Label(), fn.Label()
			for i := range count {
				fn.Append(instr.MustNew(instr.I32_CONST, uint64(i+1)), instr.MustNew(instr.LOCAL_SET, uint64(i+1)))
			}
			fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, uint64(count+1)), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, counter))
			fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, counter), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
			fn.BrIf(exit)
			// order lists the slots to copy as scratch <- first, then each into the one before it, last <- scratch.
			scratch := uint64(count + 1)
			fn.Append(instr.MustNew(instr.LOCAL_GET, uint64(order[0])), instr.MustNew(instr.LOCAL_SET, scratch))
			for i := 1; i < len(order); i++ {
				fn.Append(instr.MustNew(instr.LOCAL_GET, uint64(order[i])), instr.MustNew(instr.LOCAL_SET, uint64(order[i-1])))
			}
			fn.Append(instr.MustNew(instr.LOCAL_GET, scratch), instr.MustNew(instr.LOCAL_SET, uint64(order[len(order)-1])),
				instr.MustNew(instr.LOCAL_GET, counter), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, counter))
			fn.Br(head)
			fn.Bind(exit).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 100), instr.MustNew(instr.I32_MUL),
				instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 10), instr.MustNew(instr.I32_MUL), instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.LOCAL_GET, uint64(count)), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
			return fn.MustBuild()
		}

		for _, tc := range []struct {
			name   string
			f      *types.Function
			arg    uint64
			extras int
			kept   bool
		}{
			{name: "a loop closed by a branch", f: sum.MustBuild(), arg: 10, kept: true},
			{name: "a swap", f: trade(2, 1, 2), arg: 5, extras: 1},
			{name: "a rotation", f: trade(3, 1, 2, 3), arg: 5, extras: 1},
			{name: "an fnv hash", f: fnv.MustBuild(), arg: 1024, extras: 1},
			{name: "a fibonacci recurrence", f: fib.MustBuild(), arg: 10},
		} {
			t.Run(tc.name, func(t *testing.T) {
				prog := program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, tc.arg), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(tc.f))
				require.NoError(t, program.Verify(prog))
				want := run(prog)

				got := duplicateConstants(prog)
				_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), got)
				require.NoError(t, err)
				require.NoError(t, program.Verify(got))
				require.Equal(t, want, run(got))

				f := got.Constants[0].(*types.Function)
				if tc.kept {
					require.Equal(t, tc.f.Code, f.Code)
				} else {
					require.NotEqual(t, tc.f.Code, f.Code)
				}
				require.LessOrEqual(t, len(f.Locals), len(tc.f.Locals)+tc.extras)
				insts := instr.Unmarshal(f.Code)
				at := make([]int, len(insts)+1)
				for i, inst := range insts {
					at[i+1] = at[i] + inst.Width()
					if i > 0 {
						require.False(t, insts[i-1].Opcode() == instr.LOCAL_GET && inst.Opcode() == instr.DROP)
					}
					if inst.Opcode() == instr.BR && int(int16(inst.Operand(0))) < 0 {
						require.Equal(t, instr.LOCAL_SET, insts[i-1].Opcode())
					}
				}
			})
		}
	})

	t.Run("round-trips a large function with many loops and locals in bounded time", func(t *testing.T) {
		// Performance guard: coalescing once rebuilt block liveness on every retry,
		// 332ms for this function. Coverage instrumentation slows it past any bound.
		const locals, inner = 100, 12
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}})
		slots := make([]types.Type, locals)
		for i := range slots {
			slots[i] = types.TypeI32()
		}
		fn.Locals(slots...)
		outer, outerDone := fn.Label(), fn.Label()
		fn.Bind(outer).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
		fn.BrIf(outerDone)
		for k := range inner {
			loop, done := fn.Label(), fn.Label()
			counter := uint64(2 + k)
			fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, counter))
			fn.Bind(loop).Append(instr.MustNew(instr.LOCAL_GET, counter), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_GE_S))
			fn.BrIf(done)
			for j := range 8 {
				acc := uint64(2 + inner + (k*8+j)%(locals-inner-2))
				fn.Append(instr.MustNew(instr.LOCAL_GET, acc), instr.MustNew(instr.LOCAL_GET, counter), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, acc))
			}
			fn.Append(instr.MustNew(instr.LOCAL_GET, counter), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, counter))
			fn.Br(loop)
			fn.Bind(done)
		}
		fn.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1))
		fn.Br(outer)
		fn.Bind(outerDone).Append(instr.MustNew(instr.LOCAL_GET, uint64(2+inner)), instr.MustNew(instr.RETURN))

		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(fn.MustBuild()))
		require.NoError(t, program.Verify(prog))
		want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))

		got := duplicateConstants(prog)
		start := time.Now()
		_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), got)
		elapsed := time.Since(start)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		if testing.CoverMode() == "" {
			require.Less(t, elapsed, 200*time.Millisecond)
		}

		values, message := executionResult(t, got, interp.WithThreshold(-1))
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("types a homed reference by what its definitions guarantee", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		arrayType := types.NewArrayType(types.TypeI32())
		run := func(prog *program.Program) []types.Value {
			threaded, message := executionResult(t, prog, interp.WithThreshold(-1), interp.WithFuel(1<<24))
			require.Empty(t, message)
			native, message := executionResult(t, prog, interp.WithFuel(1<<24))
			require.Empty(t, message)
			require.Equal(t, threaded, native)
			return threaded
		}
		optimize := func(prog *program.Program) *types.Function {
			require.NoError(t, program.Verify(prog))
			want := run(prog)
			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			require.Equal(t, want, run(got))
			return got.Constants[0].(*types.Function)
		}

		t.Run("a slice kept across blocks stays in an array local", func(t *testing.T) {
			// A loop reads and writes a slice of the argument, which it keeps in a local.
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{arrayType},
				Returns: []types.Type{types.TypeI32()}}).Locals(arrayType, types.TypeI32(), types.TypeI32())
			head, done := fn.Label(), fn.Label()
			fn.Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.ARRAY_SLICE),
				instr.MustNew(instr.LOCAL_SET, 1), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2),
				instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.LOCAL_SET, 3))
			fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.I32_GE_S))
			fn.BrIf(done)
			fn.Append(
				instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.ARRAY_GET),
				instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 3),
				instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.ARRAY_SET),
				instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2))
			fn.Br(head)
			fn.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.RETURN))

			f := fn.MustBuild()
			got := optimize(program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(f, types.TypedArray[int32]{1, 2, 3, 4}), program.WithTypes(arrayType)))
			require.NotEqual(t, f.Code, got.Code)
			for _, local := range got.Locals {
				require.False(t, local.Equals(types.TypeAny()), local.String())
			}
			// A tee would leave the slice's own, untyped value on the stack where a reload has the local's type.
			for _, inst := range instr.Unmarshal(got.Code) {
				require.NotEqual(t, instr.LOCAL_TEE, inst.Opcode())
			}
		})

		t.Run("a reference stored to slots of different types is typed any", func(t *testing.T) {
			// The slice goes to an []i32 slot and an any slot, then stays on the stack around a loop.
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{arrayType, types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}}).Locals(arrayType, types.TypeAny(), types.TypeI32())
			head, done := fn.Label(), fn.Label()
			fn.Append(
				instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.CALL),
				instr.MustNew(instr.DUP), instr.MustNew(instr.LOCAL_SET, 2), instr.MustNew(instr.DUP), instr.MustNew(instr.LOCAL_SET, 3),
				instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 4))
			fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_GE_S))
			fn.BrIf(done)
			fn.Append(
				instr.MustNew(instr.DUP), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.DROP),
				instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 4))
			fn.Br(head)
			fn.Bind(done).Append(instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.RETURN))

			f := fn.MustBuild()
			// The reference comes from a call, whose result type emit does not know.
			helper := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{arrayType}}).Append(
				instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.RETURN)).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(f, types.TypedArray[int32]{1, 2, 3}, helper), program.WithTypes(arrayType))
			require.NoError(t, program.Verify(prog))
			want := run(prog)

			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			require.Equal(t, want, run(got))

			added := got.Constants[0].(*types.Function).Locals[len(f.Locals):]
			require.True(t, slices.ContainsFunc(added, func(local types.Type) bool { return local.Equals(types.TypeAny()) }))
			for _, local := range added {
				require.False(t, local.Equals(arrayType), local.String())
			}
		})

		t.Run("a load keeps the type of what it reads whatever slot its value is stored to", func(t *testing.T) {
			first := types.NewStructType(types.NewStructField(types.TypeI32()))
			second := types.NewStructType(types.NewStructField(types.TypeF64()))
			// The capture is read twice and stored only on one branch, to a slot of another struct type.
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}}).Captures(first).Locals(second)
			narrow := fn.Label()
			fn.Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.DROP), instr.MustNew(instr.LOCAL_GET, 0))
			fn.BrIf(narrow)
			fn.Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.STRUCT_GET), instr.MustNew(instr.RETURN))
			fn.Bind(narrow).Append(instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.LOCAL_SET, 1), instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.RETURN))
			f := fn.MustBuild()

			for flag, want := range map[uint64]types.Value{0: types.I32(41), 1: types.I32(9)} {
				prog := program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, flag), instr.MustNew(instr.I32_CONST, 41), instr.MustNew(instr.STRUCT_NEW, 0),
					instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.CALL)},
					program.WithConstants(f), program.WithTypes(first, second))
				require.Equal(t, []types.Value{want}, run(prog))
				got := optimize(prog)
				for _, local := range got.Locals[len(f.Locals):] {
					require.False(t, local.Equals(second), local.String())
				}
			}
		})

		t.Run("a store on one branch does not type a value the other branch uses", func(t *testing.T) {
			// A slice lives in an any slot; only one branch stores it to an []i32 slot.
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{arrayType, types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}}).Locals(arrayType, types.TypeAny())
			narrow := fn.Label()
			fn.Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_SLICE),
				instr.MustNew(instr.LOCAL_SET, 3), instr.MustNew(instr.LOCAL_GET, 1))
			fn.BrIf(narrow)
			fn.Append(instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
			fn.Bind(narrow).Append(instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.LOCAL_SET, 2), instr.MustNew(instr.I32_CONST, 9), instr.MustNew(instr.RETURN))
			f := fn.MustBuild()

			for _, flag := range []uint64{0, 1} {
				got := optimize(program.New([]instr.Instruction{
					instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.I32_CONST, flag), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
					program.WithConstants(f, types.TypedArray[int32]{1, 2, 3}), program.WithTypes(arrayType)))
				for _, local := range got.Locals[len(f.Locals):] {
					require.False(t, local.Equals(arrayType), local.String())
				}
			}
		})

		t.Run("a global read once and stored to another global on both paths verifies", func(t *testing.T) {
			wide := types.NewArrayType(types.TypeI64())
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}})
			other, next := fn.Label(), fn.Label()
			fn.Append(instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.DROP), instr.MustNew(instr.LOCAL_GET, 0))
			fn.BrIf(other)
			fn.Append(instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_SET, 1))
			fn.Br(next)
			fn.Bind(other).Append(instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_SET, 1))
			fn.Bind(next).Append(instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN))
			f := fn.MustBuild()

			for _, flag := range []uint64{0, 1} {
				optimize(program.New([]instr.Instruction{
					instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.GLOBAL_SET, 0),
					instr.MustNew(instr.I32_CONST, flag), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
					program.WithConstants(f, types.TypedArray[int64]{5, 6}), program.WithGlobals(wide, wide), program.WithTypes(wide)))
			}
		})

		t.Run("a join of different reference types stays any", func(t *testing.T) {
			// A flag picks one of two references, and the join counts it twice, with the types in both orders.
			for _, order := range [][2]uint64{{1, 2}, {2, 1}} {
				fn := types.NewFunctionBuilder(&types.FunctionType{
					Params:  []types.Type{types.TypeI32(), arrayType, types.TypeAny()},
					Returns: []types.Type{types.TypeI32()}})
				other, join := fn.Label(), fn.Label()
				fn.Append(instr.MustNew(instr.LOCAL_GET, 0))
				fn.BrIf(other)
				fn.Append(instr.MustNew(instr.LOCAL_GET, order[0]))
				fn.Br(join)
				fn.Bind(other).Append(instr.MustNew(instr.LOCAL_GET, order[1]))
				fn.Bind(join).Append(instr.MustNew(instr.DUP), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.SWAP), instr.MustNew(instr.ARRAY_LEN),
					instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))

				f := fn.MustBuild()
				for _, flag := range []uint64{0, 1} {
					got := optimize(program.New([]instr.Instruction{
						instr.MustNew(instr.I32_CONST, flag), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CONST_GET, 1),
						instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
						program.WithConstants(f, types.TypedArray[int32]{1, 2, 3}), program.WithTypes(arrayType)))
					require.NotEmpty(t, got.Locals)
					for _, local := range got.Locals {
						require.True(t, local.Equals(types.TypeAny()), local.String())
					}
				}
			}
		})
	})

	t.Run("duplicates a value its uses consume in turn instead of homing it", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		arrayType := types.NewArrayType(types.TypeI32())
		// outcome runs prog and returns what it leaves with the live reference total.
		outcome := func(prog *program.Program, opts ...interp.Option) ([]types.Value, int) {
			vm := interp.MustNew(prog, opts...)
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			refs := 0
			for addr := 0; addr < vm.HeapLen(); addr++ {
				if n, err := vm.RefCount(addr); err == nil {
					refs += n
				}
			}
			var values []types.Value
			for vm.Len() > 0 {
				value, err := vm.Pop()
				require.NoError(t, err)
				values = append(values, value)
			}
			return values, refs
		}
		// check optimizes prog and requires what it does, references included, to match.
		check := func(t *testing.T, prog *program.Program) *types.Function {
			require.NoError(t, program.Verify(prog))
			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			for _, opts := range [][]interp.Option{{interp.WithThreshold(-1)}, nil} {
				want, wantRefs := outcome(prog, opts...)
				values, refs := outcome(got, opts...)
				require.Equal(t, want, values)
				require.Equal(t, wantRefs, refs)
			}
			return got.Constants[0].(*types.Function)
		}
		opcodes := func(f *types.Function) []instr.Opcode {
			var ops []instr.Opcode
			for _, inst := range instr.Unmarshal(f.Code) {
				ops = append(ops, inst.Opcode())
			}
			return ops
		}

		t.Run("a counter bumps a capture and returns the new count", func(t *testing.T) {
			counter := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Captures(types.TypeI32()).Locals(types.TypeI32()).Append(
				instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 0),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.UPVAL_SET, 0), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN)).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CLOSURE_NEW), instr.MustNew(instr.LOCAL_SET, 0),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(counter), program.WithLocals(types.TypeAny()))

			f := check(t, prog)
			require.NotEqual(t, counter.Code, f.Code)
			require.Len(t, f.Locals, len(counter.Locals))
			require.Contains(t, opcodes(f), instr.DUP)
			require.NotContains(t, opcodes(f), instr.LOCAL_TEE)
		})

		t.Run("a slice is stored to a global and returned", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{arrayType},
				Returns: []types.Type{arrayType}}).Locals(arrayType).Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_SLICE),
				instr.MustNew(instr.LOCAL_SET, 1), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.GLOBAL_SET, 0),
				instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN)).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(fn, types.TypedArray[int32]{1, 2, 3}), program.WithGlobals(arrayType), program.WithTypes(arrayType))

			f := check(t, prog)
			require.Len(t, f.Locals, len(fn.Locals))
			require.Contains(t, opcodes(f), instr.DUP)
		})

		t.Run("a value stored through a wider operation is homed", func(t *testing.T) {
			// The stored value is the operation's last of three operands: a dup would bury the others.
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{arrayType, types.TypeI32(), types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32()).Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_SLICE),
				instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_TEE, 3),
				instr.MustNew(instr.ARRAY_SET), instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.RETURN)).MustBuild()
			prog := program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 5),
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(fn, types.TypedArray[int32]{1, 2, 3, 4}), program.WithTypes(arrayType))

			f := check(t, prog)
			require.NotContains(t, opcodes(f), instr.DUP)
		})

		t.Run("a value stored twice in a row is duplicated once", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32(), types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32()).Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
				instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.GLOBAL_SET, 0), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.GLOBAL_SET, 1),
				instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.RETURN)).MustBuild()
			f := check(t, program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3),
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(fn), program.WithGlobals(types.TypeI32(), types.TypeI32())))
			require.Equal(t, []instr.Opcode{
				instr.LOCAL_GET, instr.LOCAL_GET, instr.I32_ADD, instr.DUP, instr.GLOBAL_SET, instr.GLOBAL_SET, instr.I32_CONST, instr.RETURN}, opcodes(f))
		})

		t.Run("a closure called three times is not duplicated into its calls", func(t *testing.T) {
			// A call can trap, and a trap exposes the stack the caller left.
			bump := types.NewFunctionBuilder(&types.FunctionType{}).Captures(types.TypeI32()).Append(
				instr.MustNew(instr.UPVAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.UPVAL_SET, 0),
				instr.MustNew(instr.RETURN)).MustBuild()
			outer := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeAny()).Append(
				instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CLOSURE_NEW),
				instr.MustNew(instr.DUP), instr.MustNew(instr.DUP), instr.MustNew(instr.CALL), instr.MustNew(instr.CALL), instr.MustNew(instr.CALL),
				instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.RETURN)).MustBuild()
			f := check(t, program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(outer, bump)))
			require.NotContains(t, opcodes(f), instr.DUP)
		})

		t.Run("a value used again after another operand is pushed", func(t *testing.T) {
			fn := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32(), types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32()).Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
				instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.GLOBAL_SET, 0),
				instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_SUB), instr.MustNew(instr.RETURN)).MustBuild()
			f := check(t, program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3),
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(fn), program.WithGlobals(types.TypeI32())))
			require.Equal(t, []instr.Opcode{
				instr.LOCAL_GET, instr.LOCAL_GET, instr.I32_ADD, instr.LOCAL_TEE, instr.GLOBAL_SET, instr.I32_CONST, instr.LOCAL_GET, instr.I32_SUB, instr.RETURN}, opcodes(f))
		})
	})

	t.Run("emits only code that verifies", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		bytes := types.NewArrayType(types.TypeI8())
		// The slice is checked by kind alone, so the original verifies whatever the array's type.
		build := func(array types.Type) *program.Program {
			fn := sliceFunction(array)
			return program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(fn, types.TypedArray[int8]{1, 2, 3, 4}), program.WithGlobals(bytes), program.WithTypes(bytes))
		}

		for name, tc := range map[string]struct {
			array   types.Type
			changed bool
		}{
			"a slice of a typed array":    {array: bytes, changed: true},
			"a slice of an untyped array": {array: types.TypeAny()},
		} {
			for pipelineName, pipeline := range map[string]*pass.Pipeline[*ssa.Function]{"identity": pass.NewPipeline[*ssa.Function](), "optimized": optimized} {
				prog := build(tc.array)
				require.NoError(t, program.Verify(prog), name)
				want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))
				before := prog.Constants[0].(*types.Function).Code

				got := duplicateConstants(prog)
				_, err := transform.NewSSAPass(pipeline, false).Run(pass.NewManager(), got)
				require.NoError(t, err, name)
				require.NoError(t, program.Verify(got), name+" "+pipelineName)
				if tc.changed {
					require.NotEqual(t, before, got.Constants[0].(*types.Function).Code, name+" "+pipelineName)
				} else {
					// Nothing the typing rule knows makes the home verify, so the function is left as it was.
					require.Equal(t, before, got.Constants[0].(*types.Function).Code, name+" "+pipelineName)
				}
				for _, opts := range [][]interp.Option{{interp.WithThreshold(-1)}, nil} {
					values, message := executionResult(t, got, opts...)
					require.Equal(t, wantErr, message)
					require.Equal(t, want, values)
				}
			}
		}
	})

	t.Run("leaves a function it cannot express unchanged", func(t *testing.T) {
		body := func(is ...instr.Instruction) *types.Function {
			return types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Append(is...).MustBuild()
		}
		guarded := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}})
		start, end, catch := guarded.Label(), guarded.Label(), guarded.Label()
		guarded.Bind(start).Append(instr.MustNew(instr.I32_CONST, 1))
		guarded.Bind(end).Append(instr.MustNew(instr.RETURN))
		guarded.Bind(catch).Append(instr.MustNew(instr.DROP), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN))
		guarded.Try(start, end, catch, 0)
		for name, input := range map[string]*program.Program{
			"a trap the IR does not represent": program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(body(
				instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.RETURN), instr.MustNew(instr.UNREACHABLE)))),
			"an opcode with an immediate operand": program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(body(
				instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
				instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.RETURN))), program.WithTypes(types.NewArrayType(types.TypeI32()))),
			"a tail call": program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(body(
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.RETURN_CALL)))),
			"an exception handler": program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(guarded.MustBuild())),
			"a module value needing a local": program.New([]instr.Instruction{
				instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.DUP), instr.MustNew(instr.I32_ADD)}, program.WithGlobals(types.TypeI32()))} {
			require.NoError(t, program.Verify(input))
			before := input.String()

			preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), input)
			require.NoError(t, err)
			require.True(t, preserved, name)
			require.Equal(t, before, input.String(), name)
		}
	})

	t.Run("restores a function whose fault is reported at a literal holding it", func(t *testing.T) {
		fn := sliceFunction(types.TypeAny())
		literal := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny()), Elems: []types.Value{fn}}
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL)},
			program.WithConstants(literal, fn, types.TypedArray[int8]{1, 2, 3, 4}), program.WithGlobals(types.NewArrayType(types.TypeI8())))
		require.NoError(t, program.Verify(prog))
		before := fn.Code

		_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))
		require.Equal(t, before, fn.Code)
	})

	t.Run("keeps the functions that verify and restores the ones that do not", func(t *testing.T) {
		bytes := types.NewArrayType(types.TypeI8())
		good, first, second := sliceFunction(bytes), sliceFunction(types.TypeAny()), sliceFunction(types.TypeAny())
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 3), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL),
			instr.MustNew(instr.CONST_GET, 3), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL),
			instr.MustNew(instr.CONST_GET, 3), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.CALL)},
			program.WithConstants(good, first, second, types.TypedArray[int8]{1, 2, 3, 4}), program.WithGlobals(bytes))
		require.NoError(t, program.Verify(prog))
		goodBefore, firstBefore, secondBefore := good.Code, first.Code, second.Code

		preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, program.Verify(prog))
		require.NotEqual(t, goodBefore, good.Code)
		require.Equal(t, firstBefore, first.Code)
		require.Equal(t, secondBefore, second.Code)
	})

	t.Run("returns an invalid program unchanged", func(t *testing.T) {
		bytes := types.NewArrayType(types.TypeI8())
		invalid := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.F64_CONST, 0), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL)},
			program.WithConstants(invalid, sliceFunction(types.TypeAny()), types.TypedArray[int8]{1, 2, 3, 4}), program.WithGlobals(bytes))
		require.Error(t, program.Verify(prog))
		before := prog.String()

		preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, prog.String())
	})

	t.Run("round-trips a function held at two constant slots once", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeAny(), types.TypeI32()},
			Returns: []types.Type{types.TypeI32(), types.TypeI32()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_SLICE),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.SWAP),
			instr.MustNew(instr.GLOBAL_SET, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_ADD),
			instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(fn, fn, types.TypedArray[int8]{1, 2, 3, 4}), program.WithGlobals(types.NewArrayType(types.TypeI8())))
		require.NoError(t, program.Verify(prog))
		before := fn.Code

		// Only the third call optimizes, so a second round trip of the same function would start from the first one's output.
		calls := 0
		counted := pass.NewPipeline[*ssa.Function]()
		counted.Add(run[*ssa.Function, bool](func(manager *pass.Manager, f *ssa.Function) (bool, error) {
			calls++
			if calls == 3 {
				_, err := optimized.Run(manager, f)
				return false, err
			}
			return true, nil
		}))
		preserved, err := transform.NewSSAPass(counted, false).Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))
		require.Equal(t, 2, calls)
		require.True(t, preserved)
		require.Equal(t, before, fn.Code)
	})

	t.Run("drops the constants a declined function added", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeAny(), types.TypeI32()},
			Returns: []types.Type{types.TypeI32(), types.TypeI1()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.ARRAY_SLICE),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.SWAP),
			instr.MustNew(instr.GLOBAL_SET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_LT_S),
			instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(fn, types.TypedArray[int8]{1, 2, 3, 4}), program.WithGlobals(types.NewArrayType(types.TypeI8())))
		require.NoError(t, program.Verify(prog))
		before := prog.String()

		preserved, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, prog.String())
	})

	t.Run("does not type a loop param after a slice whose array has no type", func(t *testing.T) {
		bytes, ints := types.NewArrayType(types.TypeI8()), types.NewArrayType(types.TypeI32())
		helper := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny()}}).Append(
			instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.RETURN)).MustBuild()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32(), bytes}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32())
		head, done := fn.Label(), fn.Label()
		fn.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 2))
		fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
		fn.BrIf(done)
		fn.Append(
			instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.SWAP), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_SLICE))
		fn.Br(head)
		fn.Bind(done).Append(instr.MustNew(instr.DUP), instr.MustNew(instr.GLOBAL_SET, 0), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.RETURN))
		loop := fn.MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 300), instr.MustNew(instr.CONST_GET, 3), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(loop, helper, types.TypedArray[int32]{300, 2000, 3}, types.TypedArray[int8]{1, 2}),
			program.WithGlobals(bytes), program.WithTypes(bytes, ints))
		require.NoError(t, program.Verify(prog))
		want, wantErr := executionResult(t, prog, interp.WithThreshold(-1), interp.WithFuel(1<<24))

		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		for _, local := range got.Constants[0].(*types.Function).Locals[len(loop.Locals):] {
			require.False(t, local.Equals(bytes), "a local declared as bytes may hold the other array")
		}
		values, message := executionResult(t, got, interp.WithThreshold(-1), interp.WithFuel(1<<24))
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("emits the same code whatever order the types of a loop param are met in", func(t *testing.T) {
		bytes, ints := types.NewArrayType(types.TypeI8()), types.NewArrayType(types.TypeI32())
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32(), bytes, ints, types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32())
		second, third, merge, head, done, mid := fn.Label(), fn.Label(), fn.Label(), fn.Label(), fn.Label(), fn.Label()
		fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 4), instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.I32_EQZ))
		fn.BrIf(second)
		fn.Append(instr.MustNew(instr.LOCAL_GET, 1))
		fn.Br(head)
		fn.Bind(second).Append(instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.I32_EQZ))
		fn.BrIf(third)
		fn.Append(instr.MustNew(instr.LOCAL_GET, 2))
		fn.Br(merge)
		fn.Bind(third).Append(instr.MustNew(instr.LOCAL_GET, 2))
		fn.Bind(merge)
		fn.Br(head)
		fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
		fn.BrIf(done)
		fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_SLICE), instr.MustNew(instr.DUP),
			instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.I32_EQZ))
		fn.BrIf(mid)
		fn.Bind(mid).Append(instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 4))
		fn.Br(head)
		fn.Bind(done).Append(instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.RETURN))
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CONST_GET, 2), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(fn.MustBuild(), types.TypedArray[int8]{1, 2, 3}, types.TypedArray[int32]{1, 2, 3}), program.WithTypes(bytes, ints))
		require.NoError(t, program.Verify(prog))

		results := map[string]bool{}
		for range 200 {
			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function](), false).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))
			results[got.String()] = true
		}
		require.Len(t, results, 1)
	})

	t.Run("does not carry a dead local through a loop", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32(), types.TypeI32())
		head, done := fn.Label(), fn.Label()
		fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1))
		fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
		fn.BrIf(done)
		fn.Append(
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_MUL), instr.MustNew(instr.LOCAL_SET, 2),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.DROP),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1))
		fn.Br(head)
		fn.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))
		loop := fn.MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(loop))
		require.NoError(t, program.Verify(prog))
		want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))

		got := duplicateConstants(prog)
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))

		require.LessOrEqual(t, len(got.Constants[0].(*types.Function).Locals), len(loop.Locals))
		for _, opts := range [][]interp.Option{{interp.WithThreshold(-1)}, nil} {
			values, message := executionResult(t, got, opts...)
			require.Equal(t, wantErr, message)
			require.Equal(t, want, values)
		}
	})

	t.Run("inlines a pure scalar leaf only when asked, and keeps a trapping one", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		pair := func(divisor instr.Instruction) *program.Program {
			leaf := types.NewFunctionBuilder(&types.FunctionType{
				Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeF64()}}).Append(
				instr.MustNew(instr.F64_CONST, math.Float64bits(1)), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.I32_MUL), divisor, instr.MustNew(instr.I32_DIV_S),
				instr.MustNew(instr.I32_TO_F64_S), instr.MustNew(instr.F64_DIV), instr.MustNew(instr.RETURN)).MustBuild()
			caller := types.NewFunctionBuilder(&types.FunctionType{
				Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeF64()}}).Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL),
				instr.MustNew(instr.F64_ADD), instr.MustNew(instr.RETURN)).MustBuild()
			return program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 3), instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(caller, leaf))
		}
		calls := func(prog *program.Program) int {
			n := 0
			for _, in := range instr.Unmarshal(prog.Constants[0].(*types.Function).Code) {
				if in.Opcode() == instr.CALL {
					n++
				}
			}
			return n
		}

		for name, tc := range map[string]struct {
			divisor instr.Instruction
			calls   int
		}{
			"a constant nonzero divisor": {divisor: instr.MustNew(instr.I32_CONST, 2), calls: 0},
			"a divisor that may be zero": {divisor: instr.MustNew(instr.LOCAL_GET, 1), calls: 2},
			"a constant zero divisor":    {divisor: instr.MustNew(instr.I32_CONST, 0), calls: 2},
		} {
			prog := pair(tc.divisor)
			require.NoError(t, program.Verify(prog), name)
			want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))

			plain := duplicateConstants(prog)
			_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), plain)
			require.NoError(t, err, name)
			require.Equal(t, 2, calls(plain), name)

			got := duplicateConstants(prog)
			_, err = transform.NewSSAPass(optimized, true).Run(pass.NewManager(), got)
			require.NoError(t, err, name)
			require.NoError(t, program.Verify(got), name)
			require.Equal(t, tc.calls, calls(got), name)
			for _, opts := range [][]interp.Option{{interp.WithThreshold(-1)}, nil} {
				values, message := executionResult(t, got, opts...)
				// A trap names its instruction offsets, which the rewrite moves.
				require.Equal(t, strings.SplitN(wantErr, ":", 2)[0], strings.SplitN(message, ":", 2)[0], name)
				if wantErr == "" {
					require.Equal(t, want, values, name)
				}
			}
		}
	})

	t.Run("inlines the callee as it was before a rejected rewrite", func(t *testing.T) {
		leaf := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild()
		caller := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL)},
			program.WithConstants(leaf, caller))
		require.NoError(t, program.Verify(prog))
		want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))
		leafBefore := leaf.Code

		// The third function rewritten is the leaf's caller; the leaf is the second, and its
		// integer addition becomes a float one, which the emitted code cannot verify.
		calls := 0
		sabotage := pass.NewPipeline[*ssa.Function]()
		sabotage.Add(run[*ssa.Function, bool](func(_ *pass.Manager, f *ssa.Function) (bool, error) {
			calls++
			if calls != 2 {
				return true, nil
			}
			for id := range f.Len() {
				for i, operation := range f.Block(id).Operations {
					if operation.Op == ssa.OpExec && operation.Code == instr.I32_ADD {
						f.Block(id).Operations[i].Code = instr.F64_ADD
					}
				}
			}
			return false, nil
		}))
		_, err := transform.NewSSAPass(sabotage, true).Run(pass.NewManager(), prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))
		require.Equal(t, leafBefore, leaf.Code)
		require.NotContains(t, instr.Format(caller.Code), "call")
		values, message := executionResult(t, prog, interp.WithThreshold(-1))
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("lays out a loop so its hot path takes no unconditional branch but the back edge", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		f64 := types.TypeF64()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{f64, f64, types.TypeI32()},
			Returns: []types.Type{types.TypeI32()}}).Locals(f64, f64, types.TypeI32(), f64, f64, f64, f64)
		head, escaped, done := fn.Label(), fn.Label(), fn.Label()
		fn.Append(instr.MustNew(instr.F64_CONST, 0), instr.MustNew(instr.LOCAL_SET, 3),
			instr.MustNew(instr.F64_CONST, 0), instr.MustNew(instr.LOCAL_SET, 4),
			instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 5))
		fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.I32_GE_S))
		fn.BrIf(done)
		fn.Append(
			instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.F64_MUL), instr.MustNew(instr.LOCAL_SET, 6),
			instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.F64_MUL), instr.MustNew(instr.LOCAL_SET, 7),
			instr.MustNew(instr.LOCAL_GET, 6), instr.MustNew(instr.LOCAL_GET, 7), instr.MustNew(instr.F64_ADD),
			instr.MustNew(instr.F64_CONST, math.Float64bits(4)), instr.MustNew(instr.F64_GT))
		fn.BrIf(escaped)
		fn.Append(
			instr.MustNew(instr.LOCAL_GET, 6), instr.MustNew(instr.LOCAL_GET, 7), instr.MustNew(instr.F64_SUB), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.F64_ADD), instr.MustNew(instr.LOCAL_SET, 8),
			instr.MustNew(instr.F64_CONST, math.Float64bits(2)), instr.MustNew(instr.LOCAL_GET, 3), instr.MustNew(instr.F64_MUL), instr.MustNew(instr.LOCAL_GET, 4), instr.MustNew(instr.F64_MUL),
			instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.F64_ADD), instr.MustNew(instr.LOCAL_SET, 9),
			instr.MustNew(instr.LOCAL_GET, 8), instr.MustNew(instr.LOCAL_SET, 3), instr.MustNew(instr.LOCAL_GET, 9), instr.MustNew(instr.LOCAL_SET, 4),
			instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 5))
		fn.Br(head)
		fn.Bind(escaped).Append(instr.MustNew(instr.LOCAL_GET, 5), instr.MustNew(instr.RETURN))
		fn.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN))
		escape := fn.MustBuild()
		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.F64_CONST, math.Float64bits(-0.75)), instr.MustNew(instr.F64_CONST, math.Float64bits(0.1)), instr.MustNew(instr.I32_CONST, 50),
			instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(escape))
		require.NoError(t, program.Verify(prog))
		want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))

		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		code := instr.Unmarshal(got.Constants[0].(*types.Function).Code)

		jumps := 0
		for _, in := range code {
			if in.Opcode() == instr.BR {
				jumps++
			}
		}
		require.Equal(t, 1, jumps, instr.Format(got.Constants[0].(*types.Function).Code))
		// The counter's test has an inverse, so it keeps its exit behind it.
		require.Regexp(t, `i32\.lt_s\n\d+:\tbr_if`, instr.Format(got.Constants[0].(*types.Function).Code))
		for _, opts := range [][]interp.Option{{interp.WithThreshold(-1)}, nil} {
			values, message := executionResult(t, got, opts...)
			require.Equal(t, wantErr, message)
			require.Equal(t, want, values)
		}
	})

	t.Run("stores a loop value as soon as it is computed unless a later reader needs its local", func(t *testing.T) {
		optimized := pass.NewPipeline[*ssa.Function]()
		for _, p := range transform.O3() {
			optimized.Add(p)
		}
		i32 := types.TypeI32()
		const counter, first, second = 1, 2, 3
		build := func(update ...instr.Instruction) *types.Function {
			fn := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{i32}, Returns: []types.Type{i32}}).Locals(i32, i32, i32)
			head, done := fn.Label(), fn.Label()
			fn.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, counter),
				instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.LOCAL_SET, first),
				instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.LOCAL_SET, second))
			fn.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, counter), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
			fn.BrIf(done)
			fn.Append(instr.MustNew(instr.LOCAL_GET, counter), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, counter))
			fn.Append(update...)
			fn.Br(head)
			fn.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, first), instr.MustNew(instr.I32_CONST, 100), instr.MustNew(instr.I32_MUL),
				instr.MustNew(instr.LOCAL_GET, second), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
			return fn.MustBuild()
		}
		adjacent := func(f *types.Function, op instr.Opcode) bool {
			code := instr.Unmarshal(f.Code)
			for i := 1; i < len(code); i++ {
				if code[i-1].Opcode() == op && code[i].Opcode() == instr.LOCAL_SET {
					return true
				}
			}
			return false
		}
		get := func(slot uint64) instr.Instruction { return instr.MustNew(instr.LOCAL_GET, slot) }
		set := func(slot uint64) instr.Instruction { return instr.MustNew(instr.LOCAL_SET, slot) }
		plus := func(n uint64) []instr.Instruction {
			return []instr.Instruction{instr.MustNew(instr.I32_CONST, n), instr.MustNew(instr.I32_ADD)}
		}

		for _, tc := range []struct {
			name   string
			update []instr.Instruction
			early  []instr.Opcode
		}{
			{
				name: "independent updates",
				update: append(append(append([]instr.Instruction{get(first)}, plus(3)...), set(first), get(second), instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.I32_SUB)),
					set(second)),
				early: []instr.Opcode{instr.I32_ADD, instr.I32_SUB},
			},
			{
				name:   "a later value reads the first one's old value",
				update: append(append([]instr.Instruction{get(first)}, plus(3)...), get(first), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_MUL), set(second), set(first)),
			},
			{
				name:   "the jump passes the first one's old value to the second",
				update: append(append([]instr.Instruction{get(second)}, plus(1)...), get(first), set(second), set(first)),
			},
			{
				name:   "the jump passes the first one's old value on after summing",
				update: []instr.Instruction{get(first), get(second), instr.MustNew(instr.I32_ADD), get(first), set(second), set(first)},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := build(tc.update...)
				prog := program.New([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 4), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
					program.WithConstants(f))
				require.NoError(t, program.Verify(prog))
				want, wantErr := executionResult(t, prog, interp.WithThreshold(-1))

				got := duplicateConstants(prog)
				_, err := transform.NewSSAPass(optimized, false).Run(pass.NewManager(), got)
				require.NoError(t, err)
				require.NoError(t, program.Verify(got))
				out := got.Constants[0].(*types.Function)
				for _, op := range tc.early {
					require.True(t, adjacent(out, op), instr.Format(out.Code))
				}
				for _, opts := range [][]interp.Option{{interp.WithThreshold(-1)}, nil} {
					values, message := executionResult(t, got, opts...)
					require.Equal(t, wantErr, message)
					require.Equal(t, want, values, instr.Format(out.Code))
				}
			})
		}
	})
}

func (r run[U, R]) Run(m *pass.Manager, unit U) (R, error) {
	return r(m, unit)
}

func generatedProgram(t *testing.T, random *rand.Rand) *program.Program {
	t.Helper()

	i32 := func() instr.Instruction {
		return []instr.Instruction{
			instr.MustNew(instr.I32_CONST, uint64(random.Intn(4))),
			instr.MustNew(instr.LOCAL_GET, 0),
			instr.MustNew(instr.GLOBAL_GET, 0),
			instr.MustNew(instr.CONST_GET, 2)}[random.Intn(4)]
	}
	ref := func() instr.Instruction {
		return []instr.Instruction{
			instr.MustNew(instr.REF_NULL),
			instr.MustNew(instr.LOCAL_GET, 1),
			instr.MustNew(instr.GLOBAL_GET, 1),
			instr.MustNew(instr.CONST_GET, 3)}[random.Intn(4)]
	}
	array := func() instr.Instruction { return instr.MustNew(instr.CONST_GET, 1) }
	phrases := []func() []instr.Instruction{
		func() []instr.Instruction { return []instr.Instruction{instr.MustNew(instr.NOP)} },
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.MustNew(instr.LOCAL_SET, 0)} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.MustNew(instr.LOCAL_TEE, 0), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.MustNew(instr.GLOBAL_SET, 0)} },
		func() []instr.Instruction { return []instr.Instruction{ref(), instr.MustNew(instr.LOCAL_SET, 1)} },
		func() []instr.Instruction { return []instr.Instruction{ref(), instr.MustNew(instr.DROP)} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 0)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.MustNew(instr.I32_MUL), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.MustNew(instr.I32_LT_S), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.MustNew(instr.DUP), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 0)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.MustNew(instr.SWAP), instr.MustNew(instr.I32_SUB), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), i32(), instr.MustNew(instr.SELECT), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), instr.MustNew(instr.ARRAY_LEN), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), instr.MustNew(instr.I32_CONST, uint64(random.Intn(3))), instr.MustNew(instr.ARRAY_GET), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), instr.MustNew(instr.I32_CONST, uint64(random.Intn(3))), i32(), instr.MustNew(instr.ARRAY_SET)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{ref(), instr.MustNew(instr.REF_IS_NULL), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0), instr.MustNew(instr.DROP)}
		},
		func() []instr.Instruction { return []instr.Instruction{instr.MustNew(instr.BR, 0)} },
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.MustNew(instr.BR_IF, 0)} }}

	type branch struct {
		at    int
		after int
	}
	var code []instr.Instruction
	var bounds []int
	var branches []branch
	width := 0
	for n := 2 + random.Intn(9); len(bounds) < n; {
		bounds = append(bounds, width)
		for _, inst := range phrases[random.Intn(len(phrases))]() {
			if op := inst.Opcode(); op == instr.BR || op == instr.BR_IF {
				branches = append(branches, branch{at: len(code), after: len(bounds)})
			}
			code = append(code, inst)
			width += inst.Width()
		}
	}
	bounds = append(bounds, width)
	code = append(code, instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN))

	offsets := make([]int, len(code))
	at := 0
	for i, inst := range code {
		offsets[i] = at
		at += inst.Width()
	}
	for _, b := range branches {
		target := bounds[b.after+random.Intn(len(bounds)-b.after)]
		code[b.at].SetOperand(0, uint64(uint16(int16(target-offsets[b.at]-code[b.at].Width()))))
	}

	body := &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32()}},
		Locals: []types.Type{types.TypeI32(), types.TypeAny()},
		Code:   instr.Marshal(code)}
	leaf := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
		Append(instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN)).MustBuild()

	return program.New(
		[]instr.Instruction{instr.MustNew(instr.CONST_GET, 4), instr.MustNew(instr.CALL)},
		program.WithConstants(leaf, types.TypedArray[int32]{1, 2, 3}, types.I32(7), types.String("ab"), body),
		program.WithGlobals(types.TypeI32(), types.TypeAny()),
		program.WithTypes(types.NewArrayType(types.TypeI32())))
}

func executionResult(t *testing.T, input *program.Program, opts ...interp.Option) ([]types.Value, string) {
	t.Helper()

	vm := interp.MustNew(input, opts...)
	defer vm.Close()

	message := ""
	if err := vm.Run(context.Background()); err != nil {
		message = err.Error()
	}
	var values []types.Value
	for vm.Len() > 0 {
		value, err := vm.Pop()
		require.NoError(t, err)
		values = append(values, value)
	}
	return values, message
}
