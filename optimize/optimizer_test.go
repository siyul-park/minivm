package optimize_test

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/testenv"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// probe reads the CFG of the program's first constant function, as a pass
// added through Optimizer.Add would.
type probe struct {
	blocks []*analysis.BasicBlock
}

func (p *probe) Run(m *pass.Manager, prog *program.Program) (bool, error) {
	blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, prog.Constants[0])
	p.blocks = blocks
	return true, err
}

func TestNew(t *testing.T) {
	optimizer := optimize.New(optimize.O2)
	require.Equal(t, optimize.O2, optimizer.Level())

	// New wires the analyses and the level's transform pipeline, so the
	// returned optimizer must be usable without any further registration.
	prog := program.New([]instr.Instruction{instr.MustNew(instr.I32_CONST, 1)})
	optimized, err := optimizer.Optimize(prog)
	require.NoError(t, err)
	require.NotNil(t, optimized)
}

func TestOptimizer_Optimize(t *testing.T) {
	t.Run("keeps a division whose quotient is dropped, since it may trap", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.I32_CONST, 7),
			instr.MustNew(instr.LOCAL_GET, 0),
			instr.MustNew(instr.I32_DIV_S),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New(
			[]instr.Instruction{instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(fn))

		optimized, err := optimize.New(optimize.O3).Optimize(prog)
		require.NoError(t, err)
		require.Contains(t, instr.Format(optimized.Constants[0].(*types.Function).Code), "i32.div_s")
	})

	t.Run("keeps a source on every instruction and still optimizes", func(t *testing.T) {
		i32 := &types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}
		corpus := []struct {
			name  string
			build func() *program.Program
		}{
			{"constant fold", func() *program.Program {
				return program.New(
					[]instr.Instruction{instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.GLOBAL_SET, 0)},
					program.WithGlobals(types.TypeI32()))
			}},
			{"inlined callees", func() *program.Program {
				redundant := types.NewFunctionBuilder(i32).Append(
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
					instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild()
				branchy := types.NewFunctionBuilder(i32)
				l := branchy.Label()
				branchy.Append(
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
					instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_GT_S))
				branchy.BrIf(l)
				branchy.Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
				branchy.Bind(l)
				branchy.Append(instr.MustNew(instr.RETURN))
				return program.New(
					[]instr.Instruction{
						instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 1), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.GLOBAL_SET, 2),
						instr.MustNew(instr.GLOBAL_GET, 0), instr.MustNew(instr.GLOBAL_GET, 1), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL), instr.MustNew(instr.GLOBAL_SET, 3)},
					program.WithGlobals(types.TypeI32(), types.TypeI32(), types.TypeI32(), types.TypeI32()),
					program.WithConstants(redundant, branchy.MustBuild()))
			}},
			{"loop", func() *program.Program {
				loop := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Locals(types.TypeI32())
				head, done := loop.Label(), loop.Label()
				loop.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
				loop.BrIf(done)
				loop.Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1))
				loop.Br(head)
				loop.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.RETURN))
				return program.New(
					[]instr.Instruction{instr.MustNew(instr.I32_CONST, 5), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL), instr.MustNew(instr.GLOBAL_SET, 0)},
					program.WithGlobals(types.TypeI32()),
					program.WithConstants(loop.MustBuild()))
			}},
		}
		// annotate gives every instruction of a body a source unique across
		// bodies: owner<<16 + ip+1.
		annotate := func(code []byte, owner int) (positions []instr.Position, sources map[uint32]bool) {
			sources = map[uint32]bool{}
			for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
				source := uint32(owner<<16 + ip + 1)
				positions = append(positions, instr.Position{IP: ip, Source: source})
				sources[source] = true
			}
			return positions, sources
		}

		check := func(t *testing.T, code []byte, positions []instr.Position, allowed map[uint32]bool, level optimize.Level) {
			require.NotEmpty(t, positions, level)
			for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
				source := instr.Find(positions, ip)
				require.NotZero(t, source, "level %d ip %d", level, ip)
				require.True(t, allowed[source], "level %d ip %d source %#x", level, ip, source)
			}
		}

		for _, tc := range corpus {
			for _, level := range []optimize.Level{optimize.O1, optimize.O2, optimize.O3} {
				t.Run(fmt.Sprintf("%s O%d", tc.name, level), func(t *testing.T) {
					prog := tc.build()
					size := len(prog.Code)
					var own map[uint32]bool
					prog.Positions, own = annotate(prog.Code, 0)
					inputs := map[*types.Function]map[uint32]bool{}
					for i, c := range prog.Constants {
						if fn, ok := c.(*types.Function); ok {
							fn.Positions, inputs[fn] = annotate(fn.Code, i+1)
						}
					}
					// The module may take the sources of the callees it calls, which
					// callees that call nothing never do.
					allowed := maps.Clone(own)
					for _, inst := range instr.Unmarshal(prog.Code) {
						if inst.Opcode() == instr.CONST_GET {
							if fn, ok := prog.Constants[inst.Operand(0)].(*types.Function); ok {
								maps.Copy(allowed, inputs[fn])
							}
						}
					}
					if level != optimize.O3 {
						allowed = own
					}

					got, err := optimize.New(level).Optimize(prog)
					require.NoError(t, err)
					require.NoError(t, program.Verify(got))
					if tc.name == "constant fold" {
						require.Less(t, len(got.Code), size)
					}
					check(t, got.Code, got.Positions, allowed, level)
					for _, c := range got.Constants {
						if fn, ok := c.(*types.Function); ok {
							check(t, fn.Code, fn.Positions, inputs[fn], level)
						}
					}
					if tc.name == "inlined callees" && level == optimize.O3 {
						require.NotContains(t, instr.Format(got.Code), "call")
						owners := map[uint32]bool{}
						for ip := 0; ip < len(got.Code); ip += instr.Instruction(got.Code[ip:]).Width() {
							owners[instr.Find(got.Positions, ip)>>16] = true
						}
						require.True(t, owners[1] && owners[2], "%v %s", owners, instr.Format(got.Code))
					}
				})
			}
		}
	})

	t.Run("resolves runtime error frames to sources", func(t *testing.T) {
		// The module calls div(7, 1) a thousand times, so the JIT can compile
		// it, then div(7, 0), which traps.
		div := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_DIV_S), instr.MustNew(instr.RETURN)).MustBuild()
		b := program.NewBuilder().Locals(types.TypeI32())
		loop, done := b.Label(), b.Label()
		b.Bind(loop).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 7).Emit(instr.I32_CONST, 1).ConstGet(div).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, 7).Emit(instr.I32_CONST, 0).ConstGet(div).Emit(instr.CALL)
		input := b.MustBuild()

		// Every instruction's source is owner<<16 + ip + 1; div is owner 1.
		annotate := func(code []byte, owner int) []instr.Position {
			var positions []instr.Position
			for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
				positions = append(positions, instr.Position{IP: ip, Source: uint32(owner<<16 + ip + 1)})
			}
			return positions
		}
		// last is the ip of the last op in code.
		last := func(code []byte, op instr.Opcode) int {
			at := -1
			for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
				if instr.Opcode(code[ip]) == op {
					at = ip
				}
			}
			return at
		}
		input.Positions, div.Positions = annotate(input.Code, 0), annotate(div.Code, 1)
		want := []uint32{uint32(1<<16 + last(div.Code, instr.I32_DIV_S) + 1), uint32(last(input.Code, instr.CALL) + 1)}

		got, err := optimize.New(optimize.O3).Optimize(input)
		require.NoError(t, err)

		// resolve maps the frames of a divide-by-zero to sources: the innermost
		// frame names the faulting instruction, and an outer frame's IP is its
		// return address, so its call is the instruction before it.
		resolve := func(vm *interp.Interpreter, err error) []uint32 {
			var runtimeErr *interp.RuntimeError
			require.ErrorAs(t, err, &runtimeErr)
			require.ErrorIs(t, err, interp.ErrDivideByZero)
			require.Len(t, runtimeErr.Frames, 2)
			callee, err := vm.Load(runtimeErr.Frames[0].Func)
			require.NoError(t, err)
			return []uint32{
				instr.Find(callee.(*types.Function).Positions, runtimeErr.Frames[0].IP),
				instr.Find(got.Positions, runtimeErr.Frames[1].IP-1),
			}
		}

		t.Run("threaded", func(t *testing.T) {
			vm := interp.MustNew(got, interp.WithThreshold(-1))
			defer vm.Close()
			require.Equal(t, want, resolve(vm, vm.Run(context.Background())))
		})

		t.Run("native", func(t *testing.T) {
			testenv.MustHaveJIT(t)
			profiler := prof.New()
			vm := interp.MustNew(got, interp.WithThreshold(1), interp.WithProfiler(profiler))
			defer vm.Close()
			entries := func() float64 {
				baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
				optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
				return baseline + optimized
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				err := vm.Run(context.Background())
				vm.Flush()
				if entries() > 0 {
					require.Equal(t, want, resolve(vm, err))
					return
				}
				require.True(t, time.Now().Before(deadline), "native code never ran")
				vm.Reset()
			}
		})
	})

	t.Run("O0 passthrough", func(t *testing.T) {
		o := optimize.New(optimize.O0)
		prog := program.New([]instr.Instruction{instr.MustNew(instr.NOP)})
		result, err := o.Optimize(prog)
		require.NoError(t, err)
		require.Equal(t, prog.String(), result.String())
	})

	t.Run("O1 folds a constant window and drops what nothing reads", func(t *testing.T) {
		// The addition is dead. Folding alone would keep the folded constant,
		// because dropping a value the code pushes is not something a peephole
		// over an operand stack can decide; over SSA it is ordinary liveness,
		// so the whole phrase goes.
		prog := program.New(
			[]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
			program.WithConstants(
				types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Append(
					instr.MustNew(instr.I32_CONST, 1),
					instr.MustNew(instr.I32_CONST, 2),
					instr.MustNew(instr.I32_ADD),
					instr.MustNew(instr.DROP),
					instr.MustNew(instr.I32_CONST, 5),
					instr.MustNew(instr.RETURN)).MustBuild()))

		optimized, err := optimize.New(optimize.O1).Optimize(prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Equal(t, instr.Format(instr.Marshal([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 5),
			instr.MustNew(instr.RETURN)})), instr.Format(optimized.Constants[0].(*types.Function).Code))

		vm := interp.MustNew(optimized)
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), value)
	})

	prog := program.New(
		[]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 20),
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.CALL)},
		program.WithConstants(
			types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI64()},
				Returns: []types.Type{types.TypeI64()}}).Append(
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_LT_S),
				instr.MustNew(instr.BR_IF, 26),
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.I32_SUB),
				instr.MustNew(instr.CONST_GET, 0),
				instr.MustNew(instr.CALL),
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_SUB),
				instr.MustNew(instr.CONST_GET, 0),
				instr.MustNew(instr.CALL),
				instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.RETURN),
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.RETURN)).MustBuild()))
	for _, tc := range []struct {
		name  string
		level optimize.Level
	}{
		{"O1", optimize.O1},
		{"O2", optimize.O2},
	} {
		t.Run(tc.name+" accepts a recursive function", func(t *testing.T) {
			_, err := optimize.New(tc.level).Optimize(prog)
			require.NoError(t, err)
		})
	}

	t.Run("O3 preserves a top-level branch to the program end", func(t *testing.T) {
		b := program.NewBuilder()
		end := b.Label()
		b.Emit(instr.I32_CONST, 1)
		b.BrIf(end)
		b.Emit(instr.UNREACHABLE)
		b.Bind(end)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))

		got, err := optimize.New(optimize.O3).Optimize(prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(got))

		vm := interp.MustNew(got)
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
	})

	t.Run("O3 eliminates a common subexpression in a callee that may trap and preserves semantics", func(t *testing.T) {
		build := func() *program.Program {
			return program.New(
				[]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 3),
					instr.MustNew(instr.I32_CONST, 4),
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.CALL)},
				program.WithConstants(
					types.NewFunctionBuilder(&types.FunctionType{
						Params:  []types.Type{types.TypeI32(), types.TypeI32()},
						Returns: []types.Type{types.TypeI32()}}).Append(
						instr.MustNew(instr.LOCAL_GET, 0),
						instr.MustNew(instr.LOCAL_GET, 1),
						instr.MustNew(instr.I32_DIV_S),
						instr.MustNew(instr.DROP),
						instr.MustNew(instr.LOCAL_GET, 0),
						instr.MustNew(instr.LOCAL_GET, 1),
						instr.MustNew(instr.I32_ADD),
						instr.MustNew(instr.LOCAL_GET, 0),
						instr.MustNew(instr.LOCAL_GET, 1),
						instr.MustNew(instr.I32_ADD),
						instr.MustNew(instr.I32_ADD),
						instr.MustNew(instr.RETURN)).MustBuild()))
		}

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)

		fn := optimized.Constants[0].(*types.Function)
		require.Len(t, fn.Locals, 1)
		require.Contains(t, instr.Format(fn.Code), "local.tee")

		beforeVM := interp.MustNew(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.MustNew(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
	})

	t.Run("O3 inlines a pure scalar leaf in a loop; O1 and O2 keep the call", func(t *testing.T) {
		build := func() *program.Program {
			leaf := types.NewFunctionBuilder(&types.FunctionType{
				Params: []types.Type{types.TypeI32(), types.TypeI32()}, Returns: []types.Type{types.TypeF64()}}).Append(
				instr.MustNew(instr.F64_CONST, math.Float64bits(1)),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.I32_MUL), instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_DIV_S),
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.I32_TO_F64_S), instr.MustNew(instr.F64_DIV), instr.MustNew(instr.RETURN)).MustBuild()
			sum := types.NewFunctionBuilder(&types.FunctionType{
				Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeF64()}}).Locals(types.TypeI32(), types.TypeF64())
			head, done := sum.Label(), sum.Label()
			sum.Append(instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.LOCAL_SET, 1))
			sum.Bind(head).Append(instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_GE_S))
			sum.BrIf(done)
			sum.Append(
				instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.CONST_GET, 1), instr.MustNew(instr.CALL),
				instr.MustNew(instr.F64_ADD), instr.MustNew(instr.LOCAL_SET, 2),
				instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.LOCAL_SET, 1))
			sum.Br(head)
			sum.Bind(done).Append(instr.MustNew(instr.LOCAL_GET, 2), instr.MustNew(instr.RETURN))
			return program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 20), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(sum.MustBuild(), leaf))
		}
		calls := func(prog *program.Program) int {
			return strings.Count(instr.Format(prog.Constants[0].(*types.Function).Code), "call")
		}
		run := func(prog *program.Program) types.Value {
			vm := interp.MustNew(prog)
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.Pop()
			require.NoError(t, err)
			return value
		}
		want := run(build())

		for level, calls0 := range map[optimize.Level]int{optimize.O1: 1, optimize.O2: 1, optimize.O3: 0} {
			optimized, err := optimize.New(level).Optimize(build())
			require.NoError(t, err)
			require.NoError(t, program.Verify(optimized))
			require.Equal(t, calls0, calls(optimized), level)
			require.Equal(t, want, run(optimized), level)
		}
	})

	t.Run("O3 inlining removes the frame a call needs, so a frame limit reached only there no longer traps", func(t *testing.T) {
		build := func() *program.Program {
			leaf := types.NewFunctionBuilder(&types.FunctionType{
				Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).Append(
				instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN)).MustBuild()
			return program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)},
				program.WithConstants(leaf))
		}
		run := func(prog *program.Program) error {
			vm := interp.MustNew(prog, interp.WithFrame(1))
			defer vm.Close()
			return vm.Run(context.Background())
		}

		require.Error(t, run(build()))
		for _, level := range []optimize.Level{optimize.O1, optimize.O2} {
			optimized, err := optimize.New(level).Optimize(build())
			require.NoError(t, err)
			require.Error(t, run(optimized), level)
		}
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, run(optimized))
	})

	t.Run("O3 repairs branch offsets after shrinking", func(t *testing.T) {
		build := func() *program.Program {
			fb := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32(), types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}})
			l := fb.Label()
			fb.Append(
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.LOCAL_GET, 1),
				instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.LOCAL_GET, 1),
				instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.I32_CONST, 0),
				instr.MustNew(instr.I32_GT_S))
			fb.BrIf(l)
			fb.Append(instr.MustNew(instr.I32_CONST, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))
			fb.Bind(l)
			fb.Append(instr.MustNew(instr.RETURN))

			return program.New(
				[]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 3),
					instr.MustNew(instr.I32_CONST, 4),
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.CALL)},
				program.WithConstants(fb.MustBuild()))
		}

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))

		beforeVM := interp.MustNew(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.MustNew(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
	})

	t.Run("O3 eliminates a redundancy across a control-flow merge", func(t *testing.T) {
		build := func() *program.Program {
			fb := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32(), types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}})
			then, merge := fb.Label(), fb.Label()
			// The division may trap, so the callee is not inlined away.
			fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_DIV_S), instr.MustNew(instr.DROP))
			fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.I32_GT_S))
			fb.BrIf(then)
			fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.DROP))
			fb.Br(merge)
			fb.Bind(then)
			fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.DROP))
			fb.Bind(merge)
			fb.Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.LOCAL_GET, 1), instr.MustNew(instr.I32_ADD), instr.MustNew(instr.RETURN))

			return program.New(
				[]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 3),
					instr.MustNew(instr.I32_CONST, 4),
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.CALL)},
				program.WithConstants(fb.MustBuild()))
		}

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))

		// Both arms drop what they compute, so the redundancy is not captured
		// and reloaded but removed outright, and only the merge's own addition
		// survives.
		fn := optimized.Constants[0].(*types.Function)
		require.Empty(t, fn.Locals)
		require.Equal(t, 1, strings.Count(instr.Format(fn.Code), "i32.add"))

		beforeVM := interp.MustNew(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.MustNew(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
	})

	t.Run("O3 preserves types and handlers", func(t *testing.T) {
		build := func() *program.Program {
			fb := types.NewFunctionBuilder(&types.FunctionType{
				Params:  []types.Type{types.TypeI32(), types.TypeI32()},
				Returns: []types.Type{types.TypeI32()}})
			start, end, catch := fb.Label(), fb.Label(), fb.Label()
			fb.Bind(start)
			fb.Append(
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.LOCAL_GET, 1),
				instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.LOCAL_GET, 1),
				instr.MustNew(instr.I32_ADD),
				instr.MustNew(instr.I32_ADD))
			fb.Bind(end)
			fb.Append(instr.MustNew(instr.RETURN))
			fb.Bind(catch)
			fb.Append(instr.MustNew(instr.DROP), instr.MustNew(instr.I32_CONST, 0), instr.MustNew(instr.RETURN))
			fb.Try(start, end, catch, 2)

			return program.New(
				[]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 3),
					instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
					instr.MustNew(instr.DROP),
					instr.MustNew(instr.I32_CONST, 3),
					instr.MustNew(instr.I32_CONST, 4),
					instr.MustNew(instr.CONST_GET, 0),
					instr.MustNew(instr.CALL)},
				program.WithConstants(fb.MustBuild()),
				program.WithTypes(types.NewArrayType(types.TypeI32())))
		}

		before := build()
		optimized, err := optimize.New(optimize.O3).Optimize(build())
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Len(t, optimized.Types, 1)

		fn := optimized.Constants[0].(*types.Function)
		require.Len(t, fn.Handlers, 1)

		beforeVM := interp.MustNew(before)
		defer beforeVM.Close()
		require.NoError(t, beforeVM.Run(context.Background()))
		beforeValue, err := beforeVM.Pop()
		require.NoError(t, err)

		optimizedVM := interp.MustNew(optimized)
		defer optimizedVM.Close()
		require.NoError(t, optimizedVM.Run(context.Background()))
		optimizedValue, err := optimizedVM.Pop()
		require.NoError(t, err)
		require.Equal(t, beforeValue, optimizedValue)
	})

	progs := []struct {
		name string
		prog *program.Program
	}{
		{"constant arithmetic", program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 20),
			instr.MustNew(instr.I32_CONST, 22),
			instr.MustNew(instr.I32_ADD)})},
		{"conditional branch", program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.BR_IF, 6),
			instr.MustNew(instr.I32_CONST, 0),
			instr.MustNew(instr.DROP),
			instr.MustNew(instr.I32_CONST, 7)})},
		{"array access", program.New([]instr.Instruction{
			instr.MustNew(instr.CONST_GET, 0),
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.ARRAY_GET)}, program.WithConstants(types.TypedArray[int32]{10, 20, 30}))},
	}
	for _, tc := range progs {
		t.Run(tc.name, func(t *testing.T) {
			original := interp.MustNew(tc.prog)
			defer original.Close()
			require.NoError(t, original.Run(context.Background()))
			var want []types.Value
			for original.Len() > 0 {
				value, err := original.Pop()
				require.NoError(t, err)
				want = append(want, value)
			}

			optimized, err := optimize.New(optimize.O3).Optimize(tc.prog)
			require.NoError(t, err)
			got := interp.MustNew(optimized)
			defer got.Close()
			require.NoError(t, got.Run(context.Background()))
			var values []types.Value
			for got.Len() > 0 {
				value, err := got.Pop()
				require.NoError(t, err)
				values = append(values, value)
			}
			require.Equal(t, want, values)
		})
	}
}

func TestOptimizer_Level(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level optimize.Level
	}{
		{"O0", optimize.O0},
		{"O1", optimize.O1},
		{"O2", optimize.O2},
		{"O3", optimize.O3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.level, optimize.New(tc.level).Level())
		})
	}
}

func TestOptimizer_Add(t *testing.T) {
	t.Run("runs the added pass", func(t *testing.T) {
		pipeline := pass.NewPipeline[*ssa.Function]()
		pipeline.Add(transform.NewFoldPass())

		o := optimize.New(optimize.O0)
		o.Add(transform.NewSSAPass(pipeline, false))

		prog := program.New([]instr.Instruction{
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.I32_CONST, 2),
			instr.MustNew(instr.I32_ADD)})
		before := prog.String()

		got, err := o.Optimize(prog)
		require.NoError(t, err)
		require.NotEqual(t, before, got.String())
	})

	t.Run("reads the analyses registered by New", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).Append(
			instr.MustNew(instr.I32_CONST, 1),
			instr.MustNew(instr.RETURN)).MustBuild()
		prog := program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 0), instr.MustNew(instr.CALL)}, program.WithConstants(fn))

		p := &probe{}
		o := optimize.New(optimize.O0)
		o.Add(p)

		_, err := o.Optimize(prog)
		require.NoError(t, err)
		require.Len(t, p.blocks, 1)
	})
}

func FuzzOptimizerParity(f *testing.F) {
	f.Add(byte(0), int64(20), int64(22))
	f.Add(byte(1), int64(-10), int64(3))
	f.Add(byte(4), int64(7), int64(0))
	f.Add(byte(9), int64(-3750763034362895579), int64(1)<<62)
	f.Add(byte(11), int64(1)<<62, int64(3))

	f.Fuzz(func(t *testing.T, operation byte, left, right int64) {
		// An even operation picks an i32 op over the operands' low halves,
		// an odd one an i64 op.
		ops := [2][]instr.Opcode{
			{instr.I32_ADD, instr.I32_SUB, instr.I32_MUL, instr.I32_XOR, instr.I32_EQ, instr.I32_LT_S},
			{instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_XOR, instr.I64_SHL, instr.I64_EQ, instr.I64_LT_S},
		}[operation&1]
		code, a, b := instr.I64_CONST, uint64(left), uint64(right)
		if operation&1 == 0 {
			code, a, b = instr.I32_CONST, uint64(uint32(left)), uint64(uint32(right))
		}
		prog := program.New([]instr.Instruction{
			instr.MustNew(code, a), instr.MustNew(code, b), instr.MustNew(ops[int(operation/2)%len(ops)])})
		require.NoError(t, program.Verify(prog))

		run := func(prog *program.Program) types.Value {
			vm := interp.MustNew(prog, interp.WithTick(1))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, err := vm.Pop()
			require.NoError(t, err)
			return value
		}

		want := run(prog)
		optimized, err := optimize.New(optimize.O3).Optimize(prog)
		require.NoError(t, err)
		require.NoError(t, program.Verify(optimized))
		require.Equal(t, want, run(optimized))
	})
}
