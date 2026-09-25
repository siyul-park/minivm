package transform_test

import (
	"context"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewSSAPass(t *testing.T) {
	require.NotNil(t, transform.NewSSAPass(pass.NewPipeline[*ssa.Function]()))
}

func TestSSAPass_Run(t *testing.T) {
	t.Run("a round trip preserves what a program does", func(t *testing.T) {
		for _, prog := range programCases(t) {
			require.NoError(t, program.Verify(prog))
			want, wantErr := executionResult(t, prog)

			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function]()).Run(pass.NewManager(), got)
			require.NoError(t, err)
			require.NoError(t, program.Verify(got))

			values, message := executionResult(t, got)
			require.Equal(t, wantErr, message)
			require.Equal(t, want, values)
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
			preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function]()).Run(pass.NewManager(), got)
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
			preserved, err := transform.NewSSAPass(pipeline()).Run(pass.NewManager(), got)
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
		for _, window := range constantFunction(t) {
			prog := program.New(window.code)
			require.NoError(t, program.Verify(prog))
			values, message := executionResult(t, prog)

			got := duplicateConstants(prog)
			_, err := transform.NewSSAPass(pipeline).Run(pass.NewManager(), got)
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

	t.Run("drops the padding an offset-preserving rewrite left behind", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewDCEPass())

		got := program.New([]instr.Instruction{instr.New(instr.NOP), instr.New(instr.I32_CONST, 1), instr.New(instr.NOP)})
		_, err := transform.NewSSAPass(pipeline).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		require.Equal(t, instr.Format(instr.Marshal([]instr.Instruction{instr.New(instr.I32_CONST, 1)})), instr.Format(got.Code))
	})

	t.Run("drops a computation nothing reads", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewDCEPass())

		got := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD), instr.New(instr.DROP)})
		_, err := transform.NewSSAPass(pipeline).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))
		require.Empty(t, got.Code)
	})

	t.Run("drops a block nothing reaches", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewDCEPass())
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}})
		done := fn.Label()
		fn.Emit(instr.New(instr.I32_CONST, 1))
		fn.Br(done)
		fn.Emit(instr.New(instr.I32_CONST, 2), instr.New(instr.DROP))
		fn.Bind(done).Emit(instr.New(instr.RETURN))
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn.MustBuild()))
		require.NoError(t, program.Verify(prog))

		want, wantErr := executionResult(t, prog)
		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pipeline).Run(pass.NewManager(), got)
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
			Params:  []types.Type{types.TypeI32, types.TypeI32},
			Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()
		shared := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_MUL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 3), instr.New(instr.I32_MUL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL)}, program.WithConstants(reloaded, shared))

		want, wantErr := executionResult(t, prog)
		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pipeline).Run(pass.NewManager(), got)
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
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32)
		header, done := counting.Label(), counting.Label()
		counting.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1))
		counting.Bind(header)
		counting.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S))
		counting.BrIf(done)
		counting.Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1))
		counting.Br(header)
		counting.Bind(done)
		counting.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN))
		fn := counting.MustBuild()

		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 6), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn))

		want, wantErr := executionResult(t, prog)
		got := duplicateConstants(prog)
		_, err := transform.NewSSAPass(pipeline).Run(pass.NewManager(), got)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))

		counted := got.Constants[0].(*types.Function)
		require.NotEqual(t, instr.Format(fn.Code), instr.Format(counted.Code))

		values, message := executionResult(t, got)
		require.Equal(t, wantErr, message)
		require.Equal(t, want, values)
	})

	t.Run("declines a branch its own layout would put out of range", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			pad     int
			expects bool
		}{
			{name: "within reach", pad: 32748, expects: false},
			{name: "out of reach", pad: 32751, expects: true}} {
			input := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(spanningFunction(t, tc.pad)))
			require.NoError(t, program.Verify(input))
			want, wantErr := executionResult(t, input)
			before := input.String()

			preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function]()).Run(pass.NewManager(), input)
			require.NoError(t, err)
			require.Equal(t, tc.expects, preserved)
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
		prog := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)})
		_, err := pass.GetResult[int](manager, prog)
		require.NoError(t, err)

		inner := pass.NewPipeline[*ssa.Function]()
		inner.Add(run[*ssa.Function, bool](func(*pass.Manager, *ssa.Function) (bool, error) {
			return false, nil
		}))
		outer := pass.NewPipeline[*program.Program]()
		outer.Add(transform.NewSSAPass(inner))
		_, err = outer.Run(manager, prog)
		require.NoError(t, err)

		_, err = pass.GetResult[int](manager, prog)
		require.NoError(t, err)
		require.Equal(t, 1, calls)
	})

	t.Run("leaves a function it cannot express unchanged", func(t *testing.T) {
		for _, input := range declinedCases(t) {
			require.NoError(t, program.Verify(input))
			before := input.String()

			preserved, err := transform.NewSSAPass(pass.NewPipeline[*ssa.Function]()).Run(pass.NewManager(), input)
			require.NoError(t, err)
			require.True(t, preserved)
			require.Equal(t, before, input.String())
		}
	})
}

type run[U, R any] func(*pass.Manager, U) (R, error)

func (r run[U, R]) Run(m *pass.Manager, unit U) (R, error) {
	return r(m, unit)
}

func pipeline() *pass.Pipeline[*ssa.Function] {
	pipeline := pass.NewPipeline[*ssa.Function]()
	pipeline.Add(transform.NewFoldPass())
	pipeline.Add(transform.NewPromotePass())
	pipeline.Add(transform.NewCSEPass())
	pipeline.Add(transform.NewGuardPass())
	pipeline.Add(transform.NewHoistPass())
	pipeline.Add(transform.NewDCEPass())
	return pipeline
}

func programCases(t *testing.T) map[string]*program.Program {
	t.Helper()

	assemble := func(emit func(b *program.Builder)) *program.Program {
		b := program.NewBuilder()
		emit(b)
		prog, err := b.Build()
		require.NoError(t, err)
		return prog
	}
	sum := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeI32, types.TypeI32},
		Returns: []types.Type{types.TypeI32}}).Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD),
		instr.New(instr.RETURN)).MustBuild()

	fib := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI32}})
	base := fib.Label()
	fib.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S))
	fib.BrIf(base)
	fib.Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
		instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
		instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.I32_ADD), instr.New(instr.RETURN))
	fib.Bind(base).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))

	total := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.NewArrayType(types.TypeI32)},
		Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32)
	header, done := total.Label(), total.Label()
	total.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1))
	total.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2))
	total.Bind(header)
	total.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_LE_S))
	total.BrIf(done)
	total.Emit(
		instr.New(instr.LOCAL_GET, 1),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 2), instr.New(instr.ARRAY_GET),
		instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
		instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
	total.Br(header)
	total.Bind(done).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN))

	return map[string]*program.Program{
		"constants": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 20), instr.New(instr.I32_CONST, 22), instr.New(instr.I32_ADD)}),
		"stack shuffles": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.I32_CONST, 3),
			instr.New(instr.SWAP), instr.New(instr.DUP), instr.New(instr.DROP), instr.New(instr.I32_SUB)}),
		"select": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_CONST, 0), instr.New(instr.SELECT)}),
		"globals": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.GLOBAL_SET, 0),
			instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.I32_ADD)}, program.WithGlobals(types.TypeI32)),
		"locals": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5), instr.New(instr.LOCAL_TEE, 0),
			instr.New(instr.LOCAL_SET, 0), instr.New(instr.LOCAL_GET, 0)}, program.WithLocals(types.TypeI32)),
		"constantFunction array": program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET)}, program.WithConstants(types.TypedArray[int32]{10, 20, 30})),
		"constantFunction string": program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.STRING_LEN)}, program.WithConstants(types.String("hello"))),
		"recursion": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 12), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fib.MustBuild())),
		"array loop": program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 1), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(total.MustBuild(), types.TypedArray[int32]{1, 2, 3, 4})),
		"call": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 3), instr.New(instr.I32_CONST, 4),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(sum)),
		"branch": assemble(func(b *program.Builder) {
			other, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 1).BrIf(other)
			b.Emit(instr.I32_CONST, 2).Br(done)
			b.Bind(other).Emit(instr.I32_CONST, 3)
			b.Bind(done)
		}),
		"branch table": assemble(func(b *program.Builder) {
			one, two, done := b.Label(), b.Label(), b.Label()
			b.Locals(types.TypeI32)
			b.Emit(instr.I32_CONST, 1).BrTable(done, one, two)
			b.Bind(one).Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 0).Br(done)
			b.Bind(two).Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 0)
			b.Bind(done).Emit(instr.LOCAL_GET, 0)
		}),
		"loop": assemble(func(b *program.Builder) {
			header, done := b.Label(), b.Label()
			b.Locals(types.TypeI32)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Bind(header)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(header)
			b.Bind(done).Emit(instr.LOCAL_GET, 0)
		}),
		"branch past the end": assemble(func(b *program.Builder) {
			done := b.Label()
			b.Locals(types.TypeI32)
			b.Emit(instr.I32_CONST, 1).BrIf(done)
			b.Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 0)
			b.Bind(done)
		})}
}

func declinedCases(t *testing.T) map[string]*program.Program {
	t.Helper()

	body := func(is ...instr.Instruction) *types.Function {
		return types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Emit(is...).MustBuild()
	}
	guarded := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}})
	start, end, catch := guarded.Label(), guarded.Label(), guarded.Label()
	guarded.Bind(start).Emit(instr.New(instr.I32_CONST, 1))
	guarded.Bind(end).Emit(instr.New(instr.RETURN))
	guarded.Bind(catch).Emit(instr.New(instr.DROP), instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
	guarded.Try(start, end, catch, 0)

	return map[string]*program.Program{
		"a trap the IR does not represent": program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(body(
			instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN), instr.New(instr.UNREACHABLE)))),
		"an opcode with an immediate operand": program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(body(
			instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.ARRAY_LEN), instr.New(instr.RETURN))), program.WithTypes(types.NewArrayType(types.TypeI32))),
		"a tail call": program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(body(
			instr.New(instr.CONST_GET, 0), instr.New(instr.RETURN_CALL)))),
		"an exception handler": program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(guarded.MustBuild())),
		"a module value needing a local": program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7), instr.New(instr.DUP), instr.New(instr.I32_ADD)})}
}

func constantFunction(t *testing.T) []struct {
	op   instr.Opcode
	code []instr.Instruction
} {
	t.Helper()

	push := func(kind instr.Kind, v float64) (instr.Instruction, bool) {
		switch kind {
		case instr.KindI1, instr.KindI8, instr.KindI32, instr.KindAny:
			return instr.New(instr.I32_CONST, uint64(uint32(int32(v)))), true
		case instr.KindI64:
			return instr.New(instr.I64_CONST, uint64(int64(v))), true
		case instr.KindF32:
			return instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(v)))), true
		case instr.KindF64:
			return instr.New(instr.F64_CONST, math.Float64bits(v)), true
		case instr.KindRef:
			return instr.New(instr.REF_NULL), true
		default:
			return nil, false
		}
	}
	window := func(op instr.Opcode, right float64) ([]instr.Instruction, bool) {
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
		return append(code, instr.New(op)), true
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
			if code, ok := window(op, right); ok {
				out = append(out, struct {
					op   instr.Opcode
					code []instr.Instruction
				}{op: op, code: code})
			}
		}
	}
	require.NotEmpty(t, out)
	return out
}

func spanningFunction(t *testing.T, pad int) *types.Function {
	t.Helper()
	require.Zero(t, pad%3)

	fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32)
	done := fn.Label()
	fn.Emit(instr.New(instr.I32_CONST, 1))
	fn.BrIf(done)
	for range pad / 3 {
		fn.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.DROP))
	}
	fn.Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.DUP), instr.New(instr.I32_ADD), instr.New(instr.DROP))
	fn.Bind(done).Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN))
	return fn.MustBuild()
}

func generatedProgram(t *testing.T, random *rand.Rand) *program.Program {
	t.Helper()

	i32 := func() instr.Instruction {
		return []instr.Instruction{
			instr.New(instr.I32_CONST, uint64(random.Intn(4))),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.CONST_GET, 2)}[random.Intn(4)]
	}
	ref := func() instr.Instruction {
		return []instr.Instruction{
			instr.New(instr.REF_NULL),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.GLOBAL_GET, 1),
			instr.New(instr.CONST_GET, 3)}[random.Intn(4)]
	}
	array := func() instr.Instruction { return instr.New(instr.CONST_GET, 1) }
	phrases := []func() []instr.Instruction{
		func() []instr.Instruction { return []instr.Instruction{instr.New(instr.NOP)} },
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.New(instr.LOCAL_SET, 0)} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.LOCAL_TEE, 0), instr.New(instr.DROP)}
		},
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.New(instr.GLOBAL_SET, 0)} },
		func() []instr.Instruction { return []instr.Instruction{ref(), instr.New(instr.LOCAL_SET, 1)} },
		func() []instr.Instruction { return []instr.Instruction{ref(), instr.New(instr.DROP)} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 0)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.New(instr.I32_MUL), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.New(instr.I32_LT_S), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.DUP), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 0)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.New(instr.SWAP), instr.New(instr.I32_SUB), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), i32(), instr.New(instr.SELECT), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), instr.New(instr.ARRAY_LEN), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), instr.New(instr.I32_CONST, uint64(random.Intn(3))), instr.New(instr.ARRAY_GET), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), instr.New(instr.I32_CONST, uint64(random.Intn(3))), i32(), instr.New(instr.ARRAY_SET)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{ref(), instr.New(instr.REF_IS_NULL), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0), instr.New(instr.DROP)}
		},
		func() []instr.Instruction { return []instr.Instruction{instr.New(instr.BR, 0)} },
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.New(instr.BR_IF, 0)} }}

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
	code = append(code, instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))

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
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32, types.TypeAny},
		Code:   instr.Marshal(code)}
	leaf := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)).MustBuild()

	return program.New(
		[]instr.Instruction{instr.New(instr.CONST_GET, 4), instr.New(instr.CALL)},
		program.WithConstants(leaf, types.TypedArray[int32]{1, 2, 3}, types.I32(7), types.String("ab"), body),
		program.WithGlobals(types.TypeI32, types.TypeAny),
		program.WithTypes(types.NewArrayType(types.TypeI32)))
}

func executionResult(t *testing.T, input *program.Program) ([]types.Value, string) {
	t.Helper()

	vm := interp.New(input)
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
