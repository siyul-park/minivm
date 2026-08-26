package interp_test

import (
	"context"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/interp"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// refCountAt reads one address's reference count through the public API. The
// callers below assert on an address they just observed live, so a lookup
// error is a test failure rather than a case to handle.
func refCountAt(t *testing.T, i *interp.Interpreter, addr int) int {
	t.Helper()
	count, err := i.RefCount(addr)
	require.NoError(t, err)
	return count
}

// ArraySetAfterNestedCalls protects compiled stack materialization across
// a SIGSEGV in generated ARM64 code: an outer row loop whose body inlines
// branchy F64 tree calls and ends each iteration with ARRAY_SET. Register
// pressure used to spill inside the terminal mutation trace, letting a branch
// skip spill-frame work and corrupt the Go stack.
func TestARM64_ArraySetAfterNestedCalls(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	const trees = 2
	const rows = 8
	row := make([]float64, rows)
	out := make([]float64, rows)
	rowArr := types.TypedArray[float64](row)
	outArr := types.TypedArray[float64](out)

	fn := types.NewFunctionBuilder(nil).
		Params(types.TypeF64Array).
		Returns(types.TypeF64)
	left := fn.Label()
	fn.Emit(instr.New(instr.LOCAL_GET, 0)).
		Emit(instr.New(instr.I32_CONST, 0)).
		Emit(instr.New(instr.ARRAY_GET)).
		Emit(instr.New(instr.F64_CONST, math.Float64bits(0.5))).
		Emit(instr.New(instr.F64_LE)).
		BrIf(left).
		Emit(instr.New(instr.F64_CONST, math.Float64bits(-0.01))).
		Emit(instr.New(instr.RETURN)).
		Bind(left).
		Emit(instr.New(instr.F64_CONST, math.Float64bits(0.01))).
		Emit(instr.New(instr.RETURN))
	tree, err := fn.Build()
	require.NoError(t, err)

	b := program.NewBuilder()
	b.Locals(types.TypeI32, types.TypeF64)
	b.Const(rowArr)
	b.Const(outArr)
	b.Const(tree)

	loop := b.Label()
	b.Emit(instr.I32_CONST, 0).
		Emit(instr.LOCAL_SET, 0).
		Bind(loop).
		Emit(instr.F64_CONST, 0).
		Emit(instr.LOCAL_SET, 1)
	for range trees {
		b.Emit(instr.LOCAL_GET, 1).
			ConstGet(rowArr).
			ConstGet(tree).
			Emit(instr.CALL).
			Emit(instr.F64_ADD).
			Emit(instr.LOCAL_SET, 1)
	}
	b.ConstGet(outArr).
		Emit(instr.LOCAL_GET, 0).
		Emit(instr.LOCAL_GET, 1).
		Emit(instr.ARRAY_SET).
		Emit(instr.LOCAL_GET, 0).
		Emit(instr.I32_CONST, 1).
		Emit(instr.I32_ADD).
		Emit(instr.LOCAL_TEE, 0).
		Emit(instr.I32_CONST, uint64(uint32(rows))).
		Emit(instr.I32_LT_S).
		BrIf(loop)

	prog, err := b.Build()
	require.NoError(t, err)

	i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(1))
	defer i.Close()

	for n := 0; n < 256; n++ {
		for idx := range row {
			row[idx] = float64((n*13+idx*7)%19) / 19
		}
		require.NoError(t, i.Run(context.Background()))
		i.Reset()
	}

	// The JIT result must match the pure interpreter on the same program:
	// a spill-path bug would corrupt the accumulated sum.
	jitOut := make([]float64, len(out))
	copy(jitOut, out)

	ref := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer ref.Close()
	for n := 0; n < 256; n++ {
		for idx := range row {
			row[idx] = float64((n*13+idx*7)%19) / 19
		}
		require.NoError(t, ref.Run(context.Background()))
		ref.Reset()
	}
	require.Equal(t, jitOut, out)
}

func TestARM64_LoopCarriedLocals(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("folded side exits preserve accumulators", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		b.Locals(types.TypeI32, types.TypeI32)
		loop := b.Label()
		odd := b.Label()
		advance := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(advance)
		b.Bind(odd)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(advance)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for iteration := 0; iteration < 32; iteration++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, types.BoxI32(size+size/2), got)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("yield commits before WithTick one safepoint", func(t *testing.T) {
		// loopSafepointBudget mirrors the native loop back-edge budget interp
		// spends between safepoints. The loop must run past it for a native
		// yield to be observable, and the budget is not exported, so raising
		// it in interp without raising this copy silently stops covering the
		// safepoint commit.
		const loopSafepointBudget = 1 << 13
		const limit = int32(loopSafepointBudget + 3)
		b := program.NewBuilder()
		b.Locals(types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(limit))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		for iteration := 0; iteration < 12; iteration++ {
			require.NoError(t, jit.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(limit), got)
			jit.Reset()
		}
		require.NoError(t, jit.Close())

		var yields float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_yields_total" {
				yields += metric.Value
			}
		}
		require.Greater(t, yields, float64(0))
	})
}

// AbortedSideExitDoesNotComplete protects partial unsupported traces from
// miscompile where a captured side-exit fragment that recorded a few
// supported opcodes and then aborted on an unsupported one (MAP_NEW_DEFAULT
// is not recordable) could be mistaken for a normal top-level completion:
// lowering a learned continuation used to check the entry root rather than
// the current block, so
// an aborted fragment whose ops simply ran out could fall through as if it
// had returned normally. The x>0 path (taken while warming up) compiles as
// the JIT entry trace; the x<=0 path is hit often enough at runtime to cross
// exitThreshold and force the tracer to capture — and abort on — the
// MAP_NEW_DEFAULT side exit. The JIT-enabled run must match a pure
// interpreter run (WithThreshold(-1)) on every input.
func TestARM64_AbortedSideExitDoesNotComplete(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	b := program.NewBuilder()
	b.Globals(types.TypeI32, types.TypeI32) // 0: x (in), 1: result (out)
	mapIdx := b.Type(types.NewMapType(types.TypeI32, types.TypeI32))
	pathA := b.Label()
	done := b.Label()
	b.Emit(instr.GLOBAL_GET, 0).
		Emit(instr.I32_CONST, 0).
		Emit(instr.I32_GT_S).
		BrIf(pathA).
		Emit(instr.I32_CONST, 4).
		Emit(instr.MAP_NEW_DEFAULT, uint64(mapIdx)).
		Emit(instr.MAP_LEN).
		Emit(instr.I32_CONST, 77).
		Emit(instr.I32_ADD).
		Emit(instr.GLOBAL_SET, 1).
		Br(done).
		Bind(pathA).
		Emit(instr.I32_CONST, 1).
		Emit(instr.GLOBAL_SET, 1).
		Bind(done)
	prog, err := b.Build()
	require.NoError(t, err)

	// Mostly positive inputs (compile and exercise the JIT-native path A),
	// with a non-positive input every 4th call starting after warm-up (path
	// B) so the side exit's hit count reaches exitThreshold within the run.
	// The first several calls stay positive so the entry trace itself
	// records path A, not path B.
	inputs := make([]int32, 40)
	for n := range inputs {
		if n >= 4 && n%4 == 0 {
			inputs[n] = -1
		} else {
			inputs[n] = 5
		}
	}

	jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(1))
	defer jit.Close()
	threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer threaded.Close()
	for _, input := range inputs {
		require.NoError(t, jit.SetGlobal(0, types.BoxI32(input)))
		require.NoError(t, threaded.SetGlobal(0, types.BoxI32(input)))
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.Global(1)
		require.NoError(t, err)
		want, err := threaded.Global(1)
		require.NoError(t, err)
		require.Equal(t, want, got)
		jit.Reset()
		threaded.Reset()
	}
}

// TestARM64_SelfCallFromInlinedFrame protects selfCall's live-frame
// precondition. selfCall lowers recursion as a BL back to ctx.head, which
// re-enters the plan's entry prologue - correct only when the live frame is
// that plan's own. It used to check neither ctx.kind nor len(ctx.frames), and
// the trace frontend reaches it with a foreign frame live: any CALL in a
// function-entry-anchored trace whose callee ref matches the anchor is recorded
// as an ordinary self CALL, including one nested inside an already-inlined
// callee's body. So A inlining B (frames 1 -> 2) and B calling back into A
// arrives at selfCall with two frames, and the BL lays A's parameter prologue
// over B's activation.
//
// The mutual recursion below reaches that state by passing both callees as ref
// parameters, so no callee is a static constant, A's static plan is rejected
// outright, and the trace frontend is the only one left. B carries three locals
// A does not, so the two frame shapes differ; with matching shapes the
// corruption happens to be survivable, and with differing ones the process
// segfaults inside the Go runtime on main.
func TestARM64_SelfCallFromInlinedFrame(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	// A(n, selfA, selfB): if n <= 0 return 0; return 1 + selfB(n-1, selfA, selfB)
	aBuilder := types.NewFunctionBuilder(nil).
		Params(types.TypeI32, types.TypeAny, types.TypeAny).
		Returns(types.TypeI32)
	baseA := aBuilder.Label()
	aFn := aBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
		BrIf(baseA).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(baseA).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()

	// B(n, selfA, selfB): three extra locals widen B's frame past A's, then it
	// calls back into A - the self call that reaches selfCall with B's frame
	// live. Local 5 stays live across that call so the frame cannot be elided.
	bBuilder := types.NewFunctionBuilder(nil).
		Params(types.TypeI32, types.TypeAny, types.TypeAny).
		Locals(types.TypeI32, types.TypeI32, types.TypeI32).
		Returns(types.TypeI32)
	baseB := bBuilder.Label()
	bFn := bBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
		BrIf(baseB).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 5),
			instr.New(instr.LOCAL_GET, 3),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.CALL),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_SUB), instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).
		Bind(baseB).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()

	// Depth is varied so the trace tree keeps rebuilding and the self call is
	// reached from inlined frames at many recursion depths.
	for depth := 1; depth <= 16; depth++ {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, uint64(depth)),
				instr.New(instr.CONST_GET, 0), // selfA
				instr.New(instr.CONST_GET, 1), // selfB
				instr.New(instr.CONST_GET, 0), // callee
				instr.New(instr.CALL),
			},
			program.WithConstants(aFn, bFn),
		)

		jit := interp.New(prog, interp.WithThreshold(0), interp.WithTick(1))
		threaded := interp.New(prog, interp.WithThreshold(-1))
		for iter := 0; iter < 125; iter++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "depth %d iteration %d", depth, iter)
			require.Equal(t, refCounts(threaded), refCounts(jit), "depth %d iteration %d", depth, iter)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	}
}

// TestARM64_CalleeLocals covers the callee frame's non-parameter locals. A frame
// opens on stack space an earlier frame may have left populated, and threaded
// CALL clears that range before transferring control, so native code must too.
// Neither native call path did it reliably: selfCall emitted no clear at all,
// and directCall's clear was computed against the wrong frame base. A callee
// therefore started with stale boxed words - its first LOCAL_SET released a ref
// it never owned, and RETURN teardown released the rest.
//
// Both sub-cases read the local BEFORE writing it and fold the answer into the
// result, so a stale slot shows up as a wrong value rather than only as a
// refcount drift. The control has no non-parameter local, which is the shape
// every pre-existing self-call test used - len(Slots()) == params leaves the
// uninitialized range empty, which is why none of them caught this.
func TestARM64_CalleeLocals(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	// Each level contributes 1000 when its ref local reads back null, as the
	// threaded interpreter guarantees, and 0 when it reads stale stack data.
	const perLevel = 1000

	runParity := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithThreshold(-1))
		for n := 0; n < 64; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
	}

	// probe reads ref local 1 before assigning it, scales the answer, and leaves
	// it on the stack for the caller to fold in.
	probe := []instr.Instruction{
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_IS_NULL),
		instr.New(instr.I32_CONST, perLevel), instr.New(instr.I32_MUL),
		instr.New(instr.CONST_GET, 2), instr.New(instr.LOCAL_SET, 1),
	}

	t.Run("self-recursion through selfCall", func(t *testing.T) {
		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Params(types.TypeI32).
			Locals(types.TypeAny)
		base := b.Label()
		body := append(append([]instr.Instruction{}, probe...),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		)
		fn := b.
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
			BrIf(base).
			Emit(body...).
			Bind(base).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()

		runParity(t, program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 12),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			},
			program.WithConstants(fn, fn, types.String("s")),
		))
	})

	t.Run("mutual recursion through directCall", func(t *testing.T) {
		// Neither callee is the function being compiled, so each CALL lowers
		// through directCall's natives-slot BLR rather than selfCall's BL.
		build := func(other uint64) *types.Function {
			b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Params(types.TypeI32).
				Locals(types.TypeAny)
			base := b.Label()
			body := append(append([]instr.Instruction{}, probe...),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, other), instr.New(instr.CALL),
				instr.New(instr.I32_ADD), instr.New(instr.RETURN),
			)
			return b.
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
				BrIf(base).
				Emit(body...).
				Bind(base).
				Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
				MustBuild()
		}

		runParity(t, program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			},
			program.WithConstants(build(1), build(0), types.String("s")),
		))
	})
}

// SelfCallWithRefArg protects a self-recursive function that forwards its own
// callee ref as an argument. flush used to refuse a committing flush whenever
// any live operand was a KindRef, including a ref parameter merely passed
// through, so every such self-call failed to lower and rejected the whole
// compile. A rejected anchor is never retried, so the function stayed
// interpreted for the process lifetime while still returning the right value.
func TestARM64_SelfCallWithRefArg(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	b := types.NewFunctionBuilder(nil).
		Params(types.TypeI32, types.TypeAny).
		Returns(types.TypeI32)
	base := b.Label()
	fib := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fib),
	)

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile))
	threaded := interp.New(prog, interp.WithThreshold(-1))

	for range 64 {
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, types.BoxI32(6765), got)
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, threaded.Close())
	require.NoError(t, jit.Close())

	var entries float64
	for _, metric := range profile.Metrics() {
		if metric.Name != "vm_jit_native_entries_total" {
			continue
		}
		labels := map[string]string{}
		for _, label := range metric.Labels {
			labels[label.Key] = label.Value
		}
		if labels["func"] == "1" {
			entries += metric.Value
		}
	}
	require.Greater(t, entries, float64(0), "self-recursive function must retain native coverage")
}

// TestARM64_SelfCallFrameLocals protects the frame teardown that follows a
// native self-call. The callee owns every allocatable register, so the caller's
// cached local registers do not survive the BL; the teardown has to read each
// ref local from its VM stack slot instead of boxing the register it used to
// live in. Reading the stale register released whatever the recursion left
// behind, which faulted inside the Go runtime rather than diverging quietly.
//
// The shape matters: the ref local is read before it is written, which is what
// gives it a cached register live across the call, and the recursion has to run
// deep enough to reach the entry compile.
func TestARM64_SelfCallFrameLocals(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeI32).
		Locals(types.TypeAny)
	base := b.Label()
	fn := b.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_IS_NULL),
			instr.New(instr.I32_CONST, 1000), instr.New(instr.I32_MUL),
			instr.New(instr.CONST_GET, 1), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 8),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fn, types.String("s")),
	)

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile), interp.WithTick(1))
	threaded := interp.New(prog, interp.WithThreshold(-1))
	for n := range 16 {
		require.NoError(t, jit.Run(context.Background()), "iteration %d", n)
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
		require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, threaded.Close())
	require.NoError(t, jit.Close())

	var entries float64
	for _, metric := range profile.Metrics() {
		if metric.Name == "vm_jit_native_entries_total" {
			entries += metric.Value
		}
	}
	require.Greater(t, entries, float64(0), "expected native code to be installed")
}

// TestARM64_MutualEntries protects nested native entry frames.
func TestARM64_MutualEntries(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	build := func(other uint64) *types.Function {
		b := types.NewFunctionBuilder(nil).
			Params(types.TypeI32).
			Returns(types.TypeI32)
		base := b.Label()
		b.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
		).BrIf(base)
		b.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, other), instr.New(instr.CALL),
			instr.New(instr.RETURN),
		)
		b.Bind(base).
			Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
		return b.MustBuild()
	}

	const depth = int32(40)
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(uint32(depth))),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(build(1), build(0)),
	)

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile), interp.WithTick(1), interp.WithThreshold(1))
	threaded := interp.New(prog, interp.WithThreshold(-1))
	for range 16 {
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, types.BoxI32(0), got)
		require.Equal(t, refCounts(threaded), refCounts(jit))
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, threaded.Close())
	require.NoError(t, jit.Close())

	entries := map[string]float64{}
	for _, metric := range profile.Metrics() {
		if metric.Name != "vm_jit_native_entries_total" {
			continue
		}
		labels := map[string]string{}
		for _, label := range metric.Labels {
			labels[label.Key] = label.Value
		}
		entries[labels["func"]] += metric.Value
	}
	require.Greater(t, entries["1"], float64(0), "function A must install a native entry")
	require.Greater(t, entries["2"], float64(0), "function B must install a native entry")
}

// TestARM64_RefReturn protects the retain ordering at a native entry frame's
// RETURN. ret took the return value's retain after guardFrame had already read
// the backing local's refcount, and guardFrame deopts whenever rc <= pending.
// A function that allocates a node into a local and returns it sits exactly at
// that boundary - rc == pending == 1, the node being held only by that local -
// so every such RETURN deopted: 512 guard-value exits against 1024 native
// entries for the program below, which is the shape benchmarks/memory_test.go's
// structTreeWalk and binaryTrees builders use. Taking the retain first raises rc
// above pending, and the following releaseFrame brings it back down to the one
// live reference the caller now owns.
//
// A spurious deopt is invisible to a value or refcount oracle, because the
// interpreter finishes the RETURN correctly either way. The guard-value exit
// count is therefore the assertion that carries this test; the parity checks
// alongside it only rule out the retain leaking or freeing.
func TestARM64_RefReturn(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithThreshold(-1))
		for n := 0; n < 64; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		var entries, guardValueExits float64
		for _, metric := range profile.Metrics() {
			switch metric.Name {
			case "vm_jit_native_entries_total":
				entries += metric.Value
			case "vm_jit_native_exits_total":
				for _, label := range metric.Labels {
					if label.Key == "reason" && label.Value == "guard-value" {
						guardValueExits += metric.Value
					}
				}
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
		require.Zero(t, guardValueExits, "guardFrame spuriously deopted a RETURN of a singly-owned frame local")
	}

	t.Run("entry frame returns a singly-owned frame local", func(t *testing.T) {
		const iterations = int32(512)
		nodeType := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("val")))
		// build(v): n = STRUCT_NEW_DEFAULT; n.val = v; return n. Local 1 is the
		// only holder of n at RETURN, so its refcount equals the frame's pending
		// count and guardFrame's rc <= pending check is exactly straddled.
		buildFn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
			Params(types.TypeI32).
			Locals(types.TypeAny).
			Emit(
				instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.STRUCT_SET),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN),
			).
			MustBuild()

		// The module driver calls build in a loop so build's own entry warms up
		// and compiles within a single Run.
		b := program.NewBuilder()
		b.Type(nodeType)
		b.Const(buildFn)
		b.Locals(types.TypeI32, types.TypeI32) // 0=i, 1=sum
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(iterations))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(buildFn).Emit(instr.CALL)
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		runParity(t, prog, types.BoxI32(iterations*(iterations-1)/2))
	})

	t.Run("self-recursive ref return through selfCall", func(t *testing.T) {
		nodeType := types.NewStructType(
			types.NewStructField(types.TypeI32, types.FieldWithName("val")),
			types.NewStructField(types.TypeAny, types.FieldWithName("next")),
		)
		// chain(d): n = STRUCT_NEW_DEFAULT; n.val = d; if d > 0, n.next =
		// chain(d-1); return n. Every level's RETURN hands back a node whose
		// refcount is exactly 1 (held only by local 1), so the self-recursive
		// call inside the body (CONST_GET 0; CALL, where 0 is this function's
		// own constant index) must lower through selfCall - which rejected any
		// ref-returning target before checkReturns admitted KindRef.
		chainBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
			Params(types.TypeI32).
			Locals(types.TypeAny)
		done := chainBuilder.Label()
		chainFn := chainBuilder.
			Emit(
				instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.STRUCT_SET),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
			).
			BrIf(done).
			Emit(
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.STRUCT_SET),
			).
			Bind(done).
			Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN)).
			MustBuild()

		const depth = int32(16)
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, uint64(uint32(depth))),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
			},
			program.WithConstants(chainFn),
			program.WithTypes(nodeType),
		)
		runParity(t, prog, types.BoxI32(depth))
	})
}

// TestARM64_DirectSelfCall covers the fused constant-marker form of
// recursion, `const.get fn; call` where fn is the function being compiled.
// It is the shape every direct recursive call takes, and lowering it is what
// lets the static frontend plan a recursive function at all: while it was
// rejected, such a function had no whole-function plan and depended on a
// recorded trace, whose coverage varies with how much of the recursion the
// recording happened to reach. The sub-cases therefore assert both the value
// and that the emitted entry came from the static frontend.
func TestARM64_DirectSelfCall(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	build := func(n int32) *program.Program {
		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Params(types.TypeI32)
		base := b.Label()
		fn := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
			BrIf(base).
			Emit(
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
				instr.New(instr.I32_ADD), instr.New(instr.RETURN),
			).
			Bind(base).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()
		return program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, uint64(uint32(n))),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)
	}

	for _, tc := range []struct {
		name string
		n    int32
		want int32
	}{
		{name: "shallow recursion", n: 12, want: 144},
		{name: "recursion deeper than one trace can record", n: 24, want: 46368},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := build(tc.n)
			profile := prof.New()
			jit := interp.New(prog, interp.WithProfiler(profile))
			threaded := interp.New(prog, interp.WithThreshold(-1))

			for range 8 {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				want, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, want, got)
				require.Equal(t, types.BoxI32(tc.want), got)
				jit.Reset()
				threaded.Reset()
			}
			require.NoError(t, threaded.Close())
			require.NoError(t, jit.Close())

			var static, entries float64
			for _, metric := range profile.Metrics() {
				switch metric.Name {
				case "vm_jit_entry_emits_total":
					var frontend, fn string
					for _, label := range metric.Labels {
						switch label.Key {
						case "frontend":
							frontend = label.Value
						case "func":
							fn = label.Value
						}
					}
					if frontend == "static" && fn == "1" {
						static += metric.Value
					}
				case "vm_jit_native_entries_total":
					entries += metric.Value
				}
			}
			require.Greater(t, static, float64(0), "recursive function must get a static whole-function entry")
			require.Greater(t, entries, float64(0))
		})
	}
}

// DeferredRefElision protects Phase 3 of the JIT refcount-elision work:
// LOCAL_GET/GLOBAL_GET/UPVAL_GET of a ref defers its retain to the backing
// slot instead of taking one immediately, and ARRAY_GET/ARRAY_SET elide their
// matching container release when the operand is still deferred. Every
// sub-case asserts both the computed result and the exact heap refcount
// survive repeated JIT warmup, so a missed retain (use-after-free) or a
// missed release (leak) would show up as a wrong value or a wrong count.
// Coverage of a deferred value staying live across a learned exit/continuation
// boundary — the other half of emitExits' retain-on-reload path — is
// exercised separately by "jits learned br_if continuation over a live ref
// value" in interp_test.go, which already keeps a LOCAL_GET-deferred array
// live across a BR_IF and asserts both the result and stable exit counts.
func TestARM64_DeferredRefElision(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("local-backed ref stays live across a loop back-edge", func(t *testing.T) {
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.ARRAY_LEN)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(1), got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})

	t.Run("sieve-shaped kernel keeps the local-backed array refcount exact", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		// arr[i] = 1 — LOCAL_GET 0 pushes the array deferred (backingLocal);
		// ARRAY_SET must elide the container release to match.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(fill)
		b.Bind(scan)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// sum += arr[i] — the same deferred array feeds ARRAY_GET.
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		var ref int
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(size), v)

			l, err := i.Local(0)
			require.NoError(t, err)
			ref = l.Ref()
			require.Equal(t, 1, refCountAt(t, i, ref)) // the local slot's own retain, never doubled or dropped
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("global-backed variant elides the container release", func(t *testing.T) {
		const size = int32(8)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Globals(types.TypeI32Array)
		b.Locals(types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.GLOBAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		// GLOBAL_GET pushes the array deferred (backingGlobal).
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(fill)
		b.Bind(scan)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(2*size), v)

			g, err := i.Global(0)
			require.NoError(t, err)
			require.Equal(t, 1, refCountAt(t, i, g.Ref())) // the global slot's own retain, never doubled or dropped
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("upval-backed variant elides the container release", func(t *testing.T) {
		const size = int32(8)
		body := types.NewFunctionBuilder(nil).Captures(types.TypeI32Array).Returns(types.TypeI32)
		body.Locals(types.TypeI32, types.TypeI32)
		fill := body.Label()
		scan := body.Label()
		done := body.Label()
		body.Emit(instr.New(instr.I32_CONST, 0)).Emit(instr.New(instr.LOCAL_SET, 0))
		body.Bind(fill)
		body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, uint64(uint32(size)))).Emit(instr.New(instr.I32_GE_S)).BrIf(scan)
		// UPVAL_GET pushes the captured array deferred (backingUpval).
		body.Emit(instr.New(instr.UPVAL_GET, 0)).Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, 3)).Emit(instr.New(instr.ARRAY_SET))
		body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, 1)).Emit(instr.New(instr.I32_ADD)).Emit(instr.New(instr.LOCAL_SET, 0))
		body.Br(fill)
		body.Bind(scan)
		body.Emit(instr.New(instr.I32_CONST, 0)).Emit(instr.New(instr.LOCAL_SET, 0))
		body.Emit(instr.New(instr.I32_CONST, 0)).Emit(instr.New(instr.LOCAL_SET, 1))
		loop := body.Label()
		body.Bind(loop)
		body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, uint64(uint32(size)))).Emit(instr.New(instr.I32_GE_S)).BrIf(done)
		body.Emit(instr.New(instr.LOCAL_GET, 1)).Emit(instr.New(instr.UPVAL_GET, 0)).Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.ARRAY_GET)).Emit(instr.New(instr.I32_ADD)).Emit(instr.New(instr.LOCAL_SET, 1))
		body.Emit(instr.New(instr.LOCAL_GET, 0)).Emit(instr.New(instr.I32_CONST, 1)).Emit(instr.New(instr.I32_ADD)).Emit(instr.New(instr.LOCAL_SET, 0))
		body.Br(loop)
		body.Bind(done)
		body.Emit(instr.New(instr.LOCAL_GET, 1)).Emit(instr.New(instr.RETURN))
		fn, err := body.Build()
		require.NoError(t, err)

		arrayTyp := 0
		b := program.NewBuilder()
		arrayTyp = b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp))
		b.ConstGet(fn).Emit(instr.CLOSURE_NEW).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(3*size), v)
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("dup of a deferred ref consumed twice keeps both container releases elided", func(t *testing.T) {
		const size = int32(4)
		use := types.NewFunctionBuilder(nil).
			Params(types.TypeI32Array).
			Returns(types.TypeI32)
		// DUP of a deferred LOCAL_GET must stay deferred. Both ARRAY_LEN
		// consumers box their copies without retain/release churn.
		fn, err := use.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.DUP),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.SWAP),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(2*size), v)

			l, err := i.Local(0)
			require.NoError(t, err)
			wantLocal, err := threaded.Local(0)
			require.NoError(t, err)
			require.Equal(t, refCountAt(t, threaded, wantLocal.Ref()), refCountAt(t, i, l.Ref()))
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	t.Run("backing slot overwrite preserves a live deferred reader", func(t *testing.T) {
		replace := types.NewFunctionBuilder(nil).
			Params(types.TypeI32Array, types.TypeI32Array).
			Returns(types.TypeI32)
		// LOCAL_GET 0 pushes the first array deferred. LOCAL_SET 0 then replaces
		// that backing slot with parameter 1, so detach must own the stale reader
		// before its original backing slot changes.
		fn, err := replace.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 9),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.RETURN),
		).Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array, types.TypeI32Array)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(2), v)

			l, err := i.Local(1)
			require.NoError(t, err)
			wantLocal, err := threaded.Local(1)
			require.NoError(t, err)
			require.Equal(t, refCountAt(t, threaded, wantLocal.Ref()), refCountAt(t, i, l.Ref()))
			require.Equal(t, refCounts(threaded), refCounts(i), "refcount diverged from threaded on iteration %d", n)
			i.Reset()
			threaded.Reset()
		}
		require.NoError(t, i.Close())
		require.NoError(t, threaded.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	})

	// balanced runs prog under the JIT and the pure interpreter in lockstep and
	// asserts, on every iteration, that the popped result and every heap
	// refcount agree with the threaded reference. A missed retain leaves an rc
	// below threaded (and corrupts under -race via premature reuse); a missed
	// release leaves one above threaded. It is path-agnostic: whichever cold path
	// (terminal trap, direct call, module completion, or a threaded fallback)
	// the trace takes, the interpreter's own bookkeeping is the oracle. Heap
	// index 0 is the permanent Null cell whose count never gates a free, so its
	// bookkeeping is excluded.
	requireRefParity := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		ref := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 48; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, ref.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := ref.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(ref), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			ref.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, ref.Close())
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	}

	t.Run("terminal fallback preserves a live deferred ref", func(t *testing.T) {
		const size = int32(6)
		// A ref-element ARRAY_SET lowers as an unconditional terminal trap. Put it
		// in a compiled leaf function so the trap fires on every call, with an
		// extra deferred copy of the array live below the store: trap() must
		// retainDeferred that copy's retain before the threaded resume (ARRAY_LEN) reads
		// and then releases it. Without retainDeferred the copy is flushed unretained and
		// the interpreter frees the array one reference early.
		store := types.NewFunctionBuilder(nil).Params(types.NewArrayType(types.TypeAny), types.TypeI32).Returns(types.TypeI32)
		store.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_NULL), instr.New(instr.ARRAY_SET),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.RETURN),
		)
		fn, err := store.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		refArrTyp := b.Type(types.NewArrayType(types.TypeAny))
		b.Const(fn)
		b.Locals(types.NewArrayType(types.TypeAny), types.TypeI32, types.TypeI32)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(refArrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		loop := b.Label()
		done := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL) // store(arr, i) -> size
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("ref array store owns a deferred element before transfer", func(t *testing.T) {
		refArray := types.NewArrayType(types.TypeAny)
		store := types.NewFunctionBuilder(nil).
			Params(refArray, types.TypeI32Array).
			Returns(types.TypeI32)
		store.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.RETURN),
		)
		fn, err := store.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		refArrayType := b.Type(refArray)
		valueArrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(refArray, types.TypeI32Array)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(refArrayType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(valueArrayType)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("ref struct store owns a deferred field before transfer", func(t *testing.T) {
		structure := types.NewStructType(types.NewStructField(types.TypeI32Array))
		store := types.NewFunctionBuilder(nil).
			Params(structure, types.TypeI32Array).
			Returns(types.TypeI32)
		store.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.RETURN),
		)
		fn, err := store.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		structType := b.Type(structure)
		valueArrayType := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(structure, types.TypeI32Array)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(structType)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(valueArrayType)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("deferred ref passed as a call argument stays balanced", func(t *testing.T) {
		const size = int32(6)
		sink := types.NewFunctionBuilder(nil).Params(types.TypeI32Array).Returns(types.TypeI32)
		sink.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.RETURN))
		fn, err := sink.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		loop := b.Label()
		done := b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// Pass the array as a deferred (backingLocal) ref argument: the call must
		// own it before handing it to the callee, which releases it on RETURN.
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2) // acc += sink(arr)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("deferred ref forwarded through a self tail call stays balanced", func(t *testing.T) {
		const size = int32(6)
		// fill(arr, i, self): arr[i] = 7; i < 0 ? 0 : self(arr, i-1, self). Each
		// LOCAL_GET of arr defers, and the tail call commits its frame; the tail
		// dispatch must own every deferred ref before the committing flush (which
		// now rejects any deferred it still sees).
		fill := types.NewFunctionBuilder(nil).Params(types.TypeI32Array, types.TypeI32, types.TypeAny).Returns(types.TypeI32)
		base := fill.Label()
		fill.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LT_S)).
			BrIf(base).
			Emit(
				instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 7), instr.New(instr.ARRAY_SET),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_GET, 2),
				instr.New(instr.LOCAL_GET, 2),
				instr.New(instr.RETURN_CALL),
			).
			Bind(base).
			Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
		fn, err := fill.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(types.TypeI32Array)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size-1)))
		b.ConstGet(fn).ConstGet(fn).Emit(instr.CALL) // fill(arr, size-1, fill)
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})

	t.Run("module completion preserves a live deferred ref", func(t *testing.T) {
		tarr := types.TypedArray[int32]{3, 5, 7}
		b := program.NewBuilder()
		b.Const(tarr)
		// A typed-array constant used as an ARRAY_GET container is a deferred
		// (backingConst) marker. Leave one live on the operand stack at module end:
		// complete() flushes it to the top-level stack the wrapper preserves, so
		// retainDeferred must re-take its retain the way the threaded CONST_GET would.
		b.ConstGet(tarr)
		b.ConstGet(tarr).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_GET).Emit(instr.DROP)
		// [A] is left live on the stack; the module returns it at completion.
		prog, err := b.Build()
		require.NoError(t, err)
		requireRefParity(t, prog)
	})
}

// HoistedContainerLoop protects the loop-invariant container hoisting of
// issue #153: an entryLoop plan whose body is call-free and never writes the
// container local derives the heap cell, shape guard, and slice header once
// per native entry, so accesses keep only the bounds check and element op.
// Every sub-case diffs results and exact refcounts against a threaded twin.
func TestARM64_StaticLoopEntry(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	// A loop header compiled by the static frontend must emit only the blocks
	// its own root reaches, not every block of the function it belongs to (see
	// prune). Two loop headers share one whole-function block list, so an
	// unpruned header emits the whole function again and its entry ends up at
	// least as large as the whole-function entry.
	const size = int32(4096)
	b := program.NewBuilder()
	arrayTyp := b.Type(types.TypeI32Array)
	b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
	fill := b.Label()
	scan := b.Label()
	loop := b.Label()
	done := b.Label()
	b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(fill)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(fill)
	b.Bind(scan)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done)
	b.Emit(instr.LOCAL_GET, 2)
	prog, err := b.Build()
	require.NoError(t, err)

	profile := prof.New()
	jit := interp.New(prog, interp.WithProfiler(profile))
	threaded := interp.New(prog, interp.WithThreshold(-1))
	for n := 0; n < 16; n++ {
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
		require.Equal(t, types.BoxI32(size), got)
		require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
		jit.Reset()
		threaded.Reset()
	}
	require.NoError(t, jit.Close())
	require.NoError(t, threaded.Close())

	var entry, header float64
	var entered bool
	for _, metric := range profile.Metrics() {
		kind, frontend := "", ""
		for _, label := range metric.Labels {
			switch label.Key {
			case "kind":
				kind = label.Value
			case "frontend":
				frontend = label.Value
			}
		}
		if frontend != "static" {
			continue
		}
		switch {
		case metric.Name == "vm_jit_entry_bytes_total" && kind == "start":
			entry = metric.Value
		case metric.Name == "vm_jit_entry_bytes_total" && kind == "loop":
			header = metric.Value
		case metric.Name == "vm_jit_native_entries_total" && kind == "loop":
			entered = metric.Value > 0
		}
	}
	require.NotZero(t, entry, "expected a whole-module static entry")
	require.NotZero(t, header, "expected a static loop-header entry")
	require.True(t, entered, "the static loop entry was never invoked")
	require.Less(t, header, entry, "the loop header emitted blocks its own root cannot reach")
}

func TestARM64_HoistedContainerLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("sieve-shaped loops run native without per-access exits", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(fill)
		b.Bind(scan)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(size), got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		// Hoisted loops must not pay per-access deopts: the only native exits
		// are the loops' own cold branches. ARRAY_NEW_DEFAULT's bridge (see
		// bridgeable in interp/jit_plan.go) combined with ARRAY_GET's
		// declared-array-type resolution (see arrayKind) now let the static
		// planner cover this whole module - fill loop, scan loop, and all -
		// as one flat entry with no exits at all, which the compiler tries
		// before the trace frontend's loop-anchored hoist path; either
		// frontend winning satisfies "no per-access exits".
		var entries float64
		var sawBytes bool
		for _, metric := range profile.Metrics() {
			switch metric.Name {
			case "vm_jit_native_entries_total":
				entries += metric.Value
			case "vm_jit_entry_bytes_total":
				sawBytes = true
				require.Less(t, metric.Value, float64(16<<10), "loop body was duplicated instead of using a back-edge")
			case "vm_jit_native_exits_total":
				for _, label := range metric.Labels {
					if label.Key == "reason" {
						require.Equal(t, "loop-exit", label.Value)
					}
				}
			}
		}
		require.Greater(t, entries, float64(0))
		require.True(t, sawBytes, "expected a native entry byte metric")
	})

	t.Run("the prologue shape guard deopts to the header", func(t *testing.T) {
		// The container local alternates between an i32 array and null across
		// loop entries: entries with the array run native, entries with null
		// fail the prologue tag guard and fall back to threaded execution
		// with an empty operand stack and untouched refcounts.
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32)
		outer := b.Label()
		odd := b.Label()
		enter := b.Label()
		inner := b.Label()
		next := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(outer)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 8).Emit(instr.LOCAL_SET, 3)
		b.Br(enter)
		b.Bind(odd)
		b.Emit(instr.REF_NULL).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(enter)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_GE_S).BrIf(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(outer)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())

		// The null-container iterations must leave native execution through a
		// guard rather than running the access. Which guard owns that exit
		// depends on the frontend that compiled the entry: the trace loop
		// plan's hoist prologue guards the container shape once per entry,
		// while a static whole-function plan guards the value where it is
		// used. Both are correct; the contract asserted here is that a null
		// container always deopts, never executes natively.
		var guards float64
		for _, metric := range profile.Metrics() {
			if metric.Name != "vm_jit_native_exits_total" {
				continue
			}
			for _, label := range metric.Labels {
				if label.Key == "reason" && strings.HasPrefix(label.Value, "guard-") {
					guards += metric.Value
				}
			}
		}
		require.Greater(t, guards, float64(0), "null entries must deopt through a guard")
	})

	t.Run("a bounds deopt inside the loop matches threaded", func(t *testing.T) {
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 12).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			gotErr := jit.Run(context.Background())
			wantErr := threaded.Run(context.Background())
			require.Error(t, wantErr)
			require.Error(t, gotErr)
			require.Equal(t, wantErr.Error(), gotErr.Error(), "error diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})

	t.Run("a write to the container local keeps the slow path exact", func(t *testing.T) {
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32Array, types.TypeI32)
		loop := b.Label()
		odd := b.Label()
		write := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 8).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_SET, 0)
		b.Br(write)
		b.Bind(odd)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 0)
		b.Bind(write)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})

	t.Run("a second container shares the loop via the slow path", func(t *testing.T) {
		const size = int32(8)
		b := program.NewBuilder()
		arrayTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32, types.TypeI32)
		fill := b.Label()
		scan := b.Label()
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(fill)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(scan)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(fill)
		b.Bind(scan)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3)
		prog, err := b.Build()
		require.NoError(t, err)

		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(3*size), got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
	})
}

// StructSetLoop protects the continuable scalar STRUCT_SET path: a loop whose
// body stores a scalar field keeps executing natively past the store instead
// of deopting at a terminal boundary every iteration. Every sub-case diffs
// results and exact refcounts against a threaded twin.
func TestARM64_StructSetLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) *prof.Profiler {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
		return profile
	}

	storeTests := []struct {
		name  string
		typ   *types.StructType
		field uint64
		steps []instr.Instruction
		want  types.Boxed
	}{
		{
			name:  "i32 field store loop stays native",
			typ:   types.NewStructType(types.NewStructField(types.TypeI32)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD)},
			want:  types.BoxI32(24),
		},
		{
			name:  "i64 field store loop",
			typ:   types.NewStructType(types.NewStructField(types.TypeI64)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_ADD)},
			want:  types.BoxI64(24),
		},
		{
			name:  "f32 field store loop masks the stored lane",
			typ:   types.NewStructType(types.NewStructField(types.TypeF32)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.F32_CONST, uint64(math.Float32bits(1))), instr.New(instr.F32_ADD)},
			want:  types.BoxF32(24),
		},
		{
			name:  "f64 field store loop",
			typ:   types.NewStructType(types.NewStructField(types.TypeF64)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.F64_CONST, math.Float64bits(1)), instr.New(instr.F64_ADD)},
			want:  types.BoxF64(24),
		},
		{
			name:  "i1 field store loop",
			typ:   types.NewStructType(types.NewStructField(types.TypeI1)),
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_EQZ)},
			want:  types.BoxI1(false),
		},
		{
			name: "heap-backed data past the inline fields",
			typ: types.NewStructType(
				types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32),
				types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32),
			),
			field: 4,
			steps: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 4), instr.New(instr.STRUCT_GET), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD)},
			want:  types.BoxI32(24),
		},
	}
	for _, tt := range storeTests {
		t.Run(tt.name, func(t *testing.T) {
			const size = int32(24)
			b := program.NewBuilder()
			typ := b.Type(tt.typ)
			b.Locals(tt.typ, types.TypeI32)
			loop := b.Label()
			done := b.Label()
			b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, tt.field)
			for _, step := range tt.steps {
				b.Emit(step.Opcode(), step.Operands()...)
			}
			b.Emit(instr.STRUCT_SET)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, tt.field).Emit(instr.STRUCT_GET)
			prog, err := b.Build()
			require.NoError(t, err)
			// STRUCT_NEW_DEFAULT now bridges (see bridgeable in
			// interp/jit_plan.go), so the static planner covers this whole
			// module - loop included - as one flat native entry instead of
			// falling back to a trace-compiled loop anchor with its own
			// cold loop-exit branch; runParity already asserts native
			// entries were emitted.
			runParity(t, prog, tt.want)
		})
	}

	t.Run("owned container from a nested struct get", func(t *testing.T) {
		const size = int32(24)
		inner := types.NewStructType(types.NewStructField(types.TypeI32))
		outer := types.NewStructType(types.NewStructField(inner))
		b := program.NewBuilder()
		innerTyp := b.Type(inner)
		outerTyp := b.Type(outer)
		b.Locals(outer, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(outerTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_NEW_DEFAULT, uint64(innerTyp)).Emit(instr.STRUCT_SET)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// The container operand is the owned (retained) result of STRUCT_GET,
		// so the native store must take the rc guard and release it in place.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size))
	})

	t.Run("polymorphic struct types deopt and stay balanced", func(t *testing.T) {
		// The container local alternates between two struct types whose field 0
		// is i32: iterations against the type the trace recorded run native,
		// the other type deopts on the shape or kind guard and falls back to
		// the threaded handler with identical results and refcounts.
		narrow := types.NewStructType(types.NewStructField(types.TypeI32))
		wide := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI64))
		b := program.NewBuilder()
		narrowTyp := b.Type(narrow)
		wideTyp := b.Type(wide)
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32, types.TypeI32)
		outer := b.Label()
		odd := b.Label()
		enter := b.Label()
		inner := b.Label()
		next := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(outer)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(narrowTyp)).Emit(instr.LOCAL_SET, 0)
		b.Br(enter)
		b.Bind(odd)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(wideTyp)).Emit(instr.LOCAL_SET, 0)
		b.Bind(enter)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 6).Emit(instr.I32_GE_S).BrIf(next)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(next)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(outer)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(48))
	})

	t.Run("store after an inlined call stays terminal and balanced", func(t *testing.T) {
		const size = int32(8)
		id := types.NewFunctionBuilder(nil).Params(types.TypeI32).Returns(types.TypeI32)
		id.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		fn, err := id.Build()
		require.NoError(t, err)

		structTyp := types.NewStructType(types.NewStructField(types.TypeI32))
		b := program.NewBuilder()
		typ := b.Type(structTyp)
		b.Const(fn)
		b.Locals(structTyp, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 1).ConstGet(fn).Emit(instr.CALL)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size-1))
	})
}

// RefEqLoop protects the native boxed-word equality for REF_EQ/REF_NE.
// Every sub-case diffs results and exact refcounts against a threaded twin.
func TestARM64_RefEqLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	}

	// eqLoop counts, over size iterations, how often the two operands the
	// build callback pushes compare equal under op.
	eqLoop := func(setup func(b *program.Builder), operands func(b *program.Builder), op instr.Opcode) *program.Program {
		const size = int32(24)
		b := program.NewBuilder()
		setup(b)
		loop := b.Label()
		hit := b.Label()
		skip := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		operands(b)
		b.Emit(op).BrIf(hit)
		b.Br(skip)
		b.Bind(hit)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Bind(skip)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		return prog
	}

	t.Run("deferred ref equality stays native", func(t *testing.T) {
		prog := eqLoop(func(b *program.Builder) {
			arrTyp := b.Type(types.TypeI32Array)
			b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32Array)
			b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_SET, 3)
		}, func(b *program.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3)
		}, instr.REF_EQ)
		runParity(t, prog, types.BoxI32(24))
	})

	t.Run("deferred ref inequality stays native", func(t *testing.T) {
		prog := eqLoop(func(b *program.Builder) {
			arrTyp := b.Type(types.TypeI32Array)
			b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32Array)
			b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 3)
		}, func(b *program.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3)
		}, instr.REF_NE)
		runParity(t, prog, types.BoxI32(24))
	})
}

// TerminalMutationLoop protects the abort-to-terminal reclassification of bulk
// mutations: a loop containing ARRAY_FILL or MAP_SET compiles its prefix
// natively and deopts at the boundary instead of rejecting the whole trace.
// Every sub-case diffs results and exact refcounts against a threaded twin.
func TestARM64_TerminalMutationLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0))
	}

	t.Run("array fill loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_FILL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+1))
	})

	t.Run("array copy loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 7).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW, uint64(arrTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_COPY)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+7))
	})

	t.Run("array append loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+size))
	})

	t.Run("map set loop keeps its prefix native", func(t *testing.T) {
		const size = int32(24)
		b := program.NewBuilder()
		mapTyp := b.Type(types.NewMapType(types.TypeI32, types.TypeI32))
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 4).Emit(instr.MAP_NEW_DEFAULT, uint64(mapTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.MAP_GET).Emit(instr.I32_ADD)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog, types.BoxI32(size*(size-1)/2+5))
	})
}

// RefContainerStore covers a ref-kind ARRAY_SET/STRUCT_SET whose native
// continuation is nested inside a native self-recursive call. Both stores used
// to exit to the interpreter unconditionally on a ref element or field, because
// letting them continue drove refcounts negative against a threaded twin from
// recursion depth two upward. That was never a defect in either store: the
// cause was selfCall and directCall handing a callee a frame whose
// non-parameter locals were never cleared (see zeroLocals), so the callee's
// first LOCAL_SET released a stale boxed word it never owned. Lifting the store
// rule is simply what first admitted a function holding a ref local into native
// lowering, which is why the two looked connected.
//
// Each sub-case allocates a container, stores a recursive call's result into it
// so the store is never the block's last step, and returns the container.
// Depths zero and one passed even with the defect present, so the depths that
// diverged are the ones that matter here.
func TestARM64_RefContainerStore(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	depths := []int32{0, 1, 2, 3, 5, 8}

	t.Run("ref-element ARRAY_SET nested in self-recursion", func(t *testing.T) {
		// build() is self-recursive (it CONST_GETs and CALLs its own constant
		// index), so its entry compiles through the static frontend before any
		// trace could exist. arr[0]'s ARRAY_SET is immediately followed by
		// arr[1]'s ARRAY_SET in the same block, so neither is the block's last
		// step.
		build := func(t *testing.T, depth int32) *program.Program {
			t.Helper()
			arrTyp := types.NewArrayType(types.TypeAny)
			buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
				Params(types.TypeI32).
				Locals(types.TypeAny)
			buildDone := buildBuilder.Label()
			buildFn := buildBuilder.
				Emit(
					instr.New(instr.I32_CONST, 2), instr.New(instr.ARRAY_NEW_DEFAULT, 0),
					instr.New(instr.LOCAL_SET, 1),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
				).
				BrIf(buildDone).
				Emit(
					// arr[0] = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.ARRAY_SET),
					// arr[1] = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.ARRAY_SET),
				).
				Bind(buildDone).
				Emit(
					instr.New(instr.LOCAL_GET, 1),
					instr.New(instr.RETURN),
				).
				MustBuild()

			checkBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Params(types.TypeAny)
			nullCase := checkBuilder.Label()
			checkFn := checkBuilder.
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_IS_NULL)).
				BrIf(nullCase).
				Emit(
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, 0),
					instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, 0),
					instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
					instr.New(instr.RETURN),
				).
				Bind(nullCase).
				Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
				MustBuild()

			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, uint64(uint32(depth))),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
				},
				program.WithConstants(buildFn, checkFn),
				program.WithTypes(arrTyp),
			)
		}

		for _, depth := range depths {
			prog := build(t, depth)
			want := types.BoxI32(int32(1)<<uint(depth+1) - 1)

			jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
			threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
			for n := 0; n < 8; n++ {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				ref, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, ref, got, "result diverged from threaded at depth %d iteration %d", depth, n)
				require.Equal(t, want, got, "result diverged from expected node count at depth %d", depth)
				require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded at depth %d iteration %d", depth, n)
				jit.Reset()
				threaded.Reset()
			}
			require.NoError(t, jit.Close())
			require.NoError(t, threaded.Close())
		}
	})

	t.Run("ref-field STRUCT_SET nested in self-recursion", func(t *testing.T) {
		// build() is self-recursive (it CONST_GETs and CALLs its own constant
		// index), so its entry compiles through the static frontend before any
		// trace could exist. n.left's STRUCT_SET is immediately followed by
		// n.right's STRUCT_SET in the same block, so neither is the block's
		// last step.
		build := func(t *testing.T, depth int32) *program.Program {
			t.Helper()
			nodeType := types.NewStructType(
				types.NewStructField(types.TypeI32, types.FieldWithName("value")),
				types.NewStructField(types.TypeAny, types.FieldWithName("left")),
				types.NewStructField(types.TypeAny, types.FieldWithName("right")),
			)
			buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
				Params(types.TypeI32).
				Locals(types.TypeAny)
			buildDone := buildBuilder.Label()
			buildFn := buildBuilder.
				Emit(
					instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.STRUCT_SET),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
				).
				BrIf(buildDone).
				Emit(
					// n.left = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.STRUCT_SET),
					// n.right = build(d-1)
					instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 2),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.STRUCT_SET),
				).
				Bind(buildDone).
				Emit(
					instr.New(instr.LOCAL_GET, 1),
					instr.New(instr.RETURN),
				).
				MustBuild()

			checkBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Params(types.TypeAny)
			nullCase := checkBuilder.Label()
			checkFn := checkBuilder.
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_IS_NULL)).
				BrIf(nullCase).
				Emit(
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, 0),
					instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.STRUCT_GET),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
					instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
					instr.New(instr.RETURN),
				).
				Bind(nullCase).
				Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
				MustBuild()

			return program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, uint64(uint32(depth))),
					instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
					instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
				},
				program.WithConstants(buildFn, checkFn),
				program.WithTypes(nodeType),
			)
		}

		for _, depth := range depths {
			prog := build(t, depth)
			want := types.BoxI32(int32(1)<<uint(depth+1) - 1)

			jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0))
			threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
			for n := 0; n < 8; n++ {
				require.NoError(t, jit.Run(context.Background()))
				require.NoError(t, threaded.Run(context.Background()))
				got, err := jit.PopBoxed()
				require.NoError(t, err)
				ref, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, ref, got, "result diverged from threaded at depth %d iteration %d", depth, n)
				require.Equal(t, want, got, "result diverged from expected node count at depth %d", depth)
				require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded at depth %d iteration %d", depth, n)
				jit.Reset()
				threaded.Reset()
			}
			require.NoError(t, jit.Close())
			require.NoError(t, threaded.Close())
		}
	})
}

// StructGetStaticPlan protects the static frontend's STRUCT_GET support: a
// function whose struct-typed parameter is read with a constant field index
// compiles through the static planner without trace warmup, for scalar and
// ref fields alike, and matches the threaded twin including refcounts.
func TestARM64_StructGetStaticPlan(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runStatic := func(t *testing.T, prog *program.Program, want types.Boxed) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 8; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			ref, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, ref, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, want, got)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		static := false
		for _, metric := range profile.Metrics() {
			if metric.Name != "vm_jit_compiles_total" {
				continue
			}
			frontend, outcome := "", ""
			for _, label := range metric.Labels {
				switch label.Key {
				case "frontend":
					frontend = label.Value
				case "outcome":
					outcome = label.Value
				}
			}
			if frontend == "static" && outcome == "emitted" && metric.Value > 0 {
				static = true
			}
		}
		require.True(t, static, "expected a static-frontend compile")
	}

	t.Run("scalar field", func(t *testing.T) {
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32))
		get := types.NewFunctionBuilder(nil).Params(structTyp).Returns(types.TypeI32)
		get.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
			instr.New(instr.RETURN),
		)
		fn, err := get.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		typ := b.Type(structTyp)
		b.Const(fn)
		b.Locals(structTyp)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 9).Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL)
		prog, err := b.Build()
		require.NoError(t, err)
		runStatic(t, prog, types.BoxI32(9))
	})

	t.Run("ref field retains its result", func(t *testing.T) {
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32Array))
		get := types.NewFunctionBuilder(nil).Params(structTyp).Returns(types.TypeI32Array)
		get.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.STRUCT_GET),
			instr.New(instr.RETURN),
		)
		fn, err := get.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		typ := b.Type(structTyp)
		arrTyp := b.Type(types.TypeI32Array)
		b.Const(fn)
		b.Locals(structTyp)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typ)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 6).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 0).ConstGet(fn).Emit(instr.CALL).Emit(instr.ARRAY_LEN)
		prog, err := b.Build()
		require.NoError(t, err)
		runStatic(t, prog, types.BoxI32(6))
	})
}

// TestARM64_BridgedOpcodes protects the generalized bridge mechanism
// (bridgeable in interp/jit_plan.go): every opcode the ARM64 backend cannot
// lower now ends its block as a bridge instead of rejecting the whole
// function, so the static planner can compile object-shaped code that used
// to stay fully threaded. Each subtest exercises one bridgeable opcode
// family inside a hot loop and diffs the JIT run against a threaded twin
// across repeated Run+Reset cycles, on both result and exact refcount.
func TestARM64_BridgedOpcodes(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	runParity := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, gotErr := jit.PopBoxed()
			want, wantErr := threaded.PopBoxed()
			require.NoError(t, gotErr)
			require.NoError(t, wantErr)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		// Close flushes each interpreter's private sample collector into the
		// shared profiler (see Interpreter.Close), so entries must be read
		// only after both are closed.
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
	}

	runParityErr := func(t *testing.T, prog *program.Program) {
		t.Helper()
		profile := prof.New()
		jit := interp.New(prog, interp.WithTick(1), interp.WithThreshold(0), interp.WithProfiler(profile))
		threaded := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		closed := false
		t.Cleanup(func() {
			if !closed {
				require.NoError(t, jit.Close())
				require.NoError(t, threaded.Close())
			}
		})
		for n := 0; n < 32; n++ {
			gotErr := jit.Run(context.Background())
			wantErr := threaded.Run(context.Background())
			require.Error(t, wantErr)
			require.Error(t, gotErr)
			require.Equal(t, wantErr.Error(), gotErr.Error(), "error diverged from threaded on iteration %d", n)
			require.Equal(t, refCounts(threaded), refCounts(jit), "refcount diverged from threaded on iteration %d", n)
			jit.Reset()
			threaded.Reset()
		}
		require.NoError(t, jit.Close())
		require.NoError(t, threaded.Close())
		closed = true
		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		require.Greater(t, entries, float64(0), "expected native code to be installed")
	}

	t.Run("allocation family: struct.new, array.new, closure.new, and ref.new", func(t *testing.T) {
		const size = int32(16)
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32))
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeI32).
			Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()

		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		structIdx := b.Type(structTyp)
		fnIdx := b.Const(fn)
		b.Locals(types.TypeI32Array, structTyp, types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// arr = array.new([i, i+1], count=2)
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.I32_CONST, 2)
		b.Emit(instr.ARRAY_NEW, uint64(arrTyp))
		b.Emit(instr.LOCAL_SET, 0)
		// s = struct.new(field0=i, field1=i+1)
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_NEW, uint64(structIdx))
		b.Emit(instr.LOCAL_SET, 1)
		// c = closure.new(capture=i, fn) - created and released every iteration
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.CONST_GET, uint64(fnIdx))
		b.Emit(instr.CLOSURE_NEW)
		b.Emit(instr.LOCAL_SET, 2)
		// ref.new(i), then drop it - exercises create-then-release
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.REF_NEW).Emit(instr.DROP)
		// sum += struct.get(s, 0). arr is deliberately left unread: both
		// ARRAY_GET and ARRAY_LEN only lower natively for a known constant
		// container (see arrayKind in interp/jit_plan.go and arrayLen in
		// interp/jit_arm64.go, which reads the trace-only op.shape a static
		// plan never populates), so arr's local-backed value is exercised
		// only through create-and-release across LOCAL_SET each iteration.
		b.Emit(instr.LOCAL_GET, 4)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 4)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("allocation family: ref.test and ref.cast against a declared struct type", func(t *testing.T) {
		const size = int32(16)
		structTyp := types.NewStructType(types.NewStructField(types.TypeI32))

		b := program.NewBuilder()
		typIdx := b.Type(structTyp)
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.STRUCT_NEW_DEFAULT, uint64(typIdx)).Emit(instr.LOCAL_SET, 0)
		// ref.test[structTyp](s) is always true here; drop it, only the bridge
		// and its release matter.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.REF_TEST, uint64(typIdx)).Emit(instr.DROP)
		// ref.cast[structTyp](s) always succeeds against its own declared
		// type; the field-0 read afterward proves the cast value is intact.
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.REF_CAST, uint64(typIdx))
		b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("map family: map.new, map.set, map.len, map.delete, and map.clear", func(t *testing.T) {
		const size = int32(16)
		mapTyp := types.NewMapType(types.TypeI32, types.TypeI32)

		b := program.NewBuilder()
		typIdx := b.Type(mapTyp)
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// m = map.new({i: i*2}, count=1)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_MUL)
		b.Emit(instr.I32_CONST, 1)
		b.Emit(instr.MAP_NEW, uint64(typIdx))
		b.Emit(instr.LOCAL_SET, 0)
		// map.set(m, key=i+100, value=i+1)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 100).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.MAP_SET)
		// sum += map.len(m)
		b.Emit(instr.LOCAL_GET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN)
		b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		// map.delete(m, key=i+100)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 100).Emit(instr.I32_ADD)
		b.Emit(instr.MAP_DELETE)
		// map.clear(m)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_CLEAR)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("string family: string.new_utf32, string.encode_utf32, and string.iter", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		charTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeAny, types.TypeI32Array, types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 5)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// arr = new i32[1]; arr[0] = 65+i
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(charTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 65).Emit(instr.I32_ADD)
		b.Emit(instr.ARRAY_SET)
		// str = string.new_utf32(arr)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.STRING_NEW_UTF32).Emit(instr.LOCAL_SET, 1)
		// codepoints = string.encode_utf32(str)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.LOCAL_SET, 2)
		// iter = string.iter(str) - created and released every iteration
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_ITER).Emit(instr.LOCAL_SET, 3)
		// sum += string.len(str). codepoints is deliberately left unread:
		// ARRAY_LEN and ARRAY_GET only lower natively for a known constant
		// container (see arrayKind in interp/jit_plan.go and arrayLen in
		// interp/jit_arm64.go), so its local-backed value is exercised only
		// through create-and-release across LOCAL_SET each iteration.
		// string.len needs no such container-shape hint: it guards against
		// the fixed string itab directly (see stringLen in
		// interp/jit_arm64.go).
		b.Emit(instr.LOCAL_GET, 5)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_LEN)
		b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 5)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 5)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("bulk array family: array.fill, array.append, array.copy, and array.slice", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		arrTyp := b.Type(types.TypeI32Array)
		b.Locals(types.TypeI32Array, types.TypeI32Array, types.TypeI32Array, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// arr = new i32[4]; arr.fill(offset=0, value=i, count=4)
		b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 4)
		b.Emit(instr.ARRAY_FILL)
		// arr.append([i+1], count=1); ARRAY_APPEND leaves the array ref on the
		// stack for chaining (see arrayAppend in
		// internal/cmd/geninterp/lower.go), so drop it here since arr is
		// already reachable through local 0.
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.I32_CONST, 1)
		b.Emit(instr.ARRAY_APPEND)
		b.Emit(instr.DROP)
		// dst = new i32[5]; array.copy(dst, 0, arr, 0, 4)
		b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrTyp)).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0)
		b.Emit(instr.I32_CONST, 4)
		b.Emit(instr.ARRAY_COPY)
		// slice = arr[0:2]
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_SLICE).Emit(instr.LOCAL_SET, 2)
		// sum += i. dst and slice are deliberately left unread: ARRAY_GET
		// and ARRAY_LEN only lower natively for a known constant container
		// (see arrayKind in interp/jit_plan.go and arrayLen in
		// interp/jit_arm64.go), so their local-backed values are exercised
		// only through create-and-release across LOCAL_SET each iteration.
		b.Emit(instr.LOCAL_GET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 4)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("bulk array family: array.delete on a known constant container", func(t *testing.T) {
		// array.delete only resolves its removed-element kind statically for
		// a known constant container (see arrayKind in
		// interp/jit_plan.go), the same restriction ARRAY_GET has always
		// had; this exercises the bridge with that supported shape.
		//
		// array.delete shrinks and shifts its container in place, and the
		// jit and threaded interpreters below are both built from the same
		// prog, whose constant array a fresh *Interpreter does not deep-copy
		// per instance: every element is the same value so the result stays
		// independent of which interpreter's prior runs already shifted the
		// shared backing array, and the array is sized well beyond every
		// run's total deletions so it never underflows across Reset cycles.
		const size = int32(8)
		values := make(types.TypedArray[int32], size*256)
		for i := range values {
			values[i] = 7
		}
		b := program.NewBuilder()
		arr := b.Const(values)
		b.Locals(types.TypeI32, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, uint64(arr)).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_DELETE).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("error family: error.new and error.code", func(t *testing.T) {
		const size = int32(16)
		b := program.NewBuilder()
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		// err = error.new(code=42, payload=i)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.I32_CONST, 42)
		b.Emit(instr.ERROR_NEW)
		b.Emit(instr.LOCAL_SET, 0)
		// sum += error.code(err)
		b.Emit(instr.LOCAL_GET, 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.ERROR_CODE)
		b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)
		runParity(t, prog)
	})

	t.Run("error family: throw after a bridged allocation stays uncaught", func(t *testing.T) {
		const size = int32(8)
		b := program.NewBuilder()
		b.Locals(types.TypeI32)
		loop := b.Label()
		throwPoint := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(throwPoint)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(throwPoint)
		// error.new(code=99, payload=count); throw - two adjacent bridgeable
		// opcodes in a row exercise the bridge-of-a-bridge resume path.
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.I32_CONST, 99)
		b.Emit(instr.ERROR_NEW)
		b.Emit(instr.THROW)
		prog, err := b.Build()
		require.NoError(t, err)
		runParityErr(t, prog)
	})
}

// hostLoopFields is the Go struct TestARM64_HostStructLoop reads and writes
// through. It carries an unexported field so the codec picks a live view, one
// exported field per Go kind the lowerer has a row for, a string field it has
// none for, and an int64 field holding more than a box payload fits.
type hostLoopFields struct {
	Flag   bool
	I8     int8
	I16    int16
	I32    int32
	Int    int
	I64    int64
	U8     uint8
	U16    uint16
	U32    uint32
	U64    uint64
	F32    float32
	F64    float64
	Text   string
	Big    int64
	hidden int32
}

func (h *hostLoopFields) Hidden() int32 { return h.hidden }

// hostNarrowField and hostWideField hold one field of the same VM kind in two
// Go widths, which is what makes a lowered read of one wrong for the other.
type hostNarrowField struct {
	V      int16
	hidden int32
}

func (h *hostNarrowField) Hidden() int32 { return h.hidden }

type hostWideField struct {
	V      int32
	hidden int32
}

func (h *hostWideField) Hidden() int32 { return h.hidden }

// TestARM64_HostStructLoop covers STRUCT_GET and STRUCT_SET against a
// *HostStruct, whose fields hold Go memory rather than VM words. Every case
// reads or writes inside a counted loop and hands the loop's own value back, so
// a row that loads the wrong width or extension reports a wrong value rather
// than merely a different speed, and the native entry count separates an access
// that stayed in the loop from one that exited on every iteration.
func TestARM64_HostStructLoop(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	const size = int32(24)
	negI32, negI64 := int32(-99), int64(-1)<<41
	seed := hostLoopFields{
		Flag: true, I8: -8, I16: -300, I32: -70000, Int: -1 << 40, I64: -1 << 40,
		U8: 200, U16: 60000, U32: 0xFFFF_FFFF, U64: 1 << 40, F32: 1.5, F64: -2.5,
		Text: "text", Big: 1 << 60,
	}

	// run marshals its own copy of seed, so a JIT run and a threaded run each
	// own the Go memory they write and the comparison between them stays
	// honest. body runs once per iteration and tail leaves the result.
	run := func(t *testing.T, locals []types.Type, body, tail []instr.Instruction, opts ...interp.Option) (types.Value, hostLoopFields, float64) {
		t.Helper()
		setup := interp.New(program.New(nil))
		defer func() { require.NoError(t, setup.Close()) }()
		src := seed
		host, err := interp.NewRegistry().Marshal(setup, &src)
		require.NoError(t, err)

		b := program.NewBuilder()
		require.Equal(t, 0, b.Const(host))
		b.Locals(append([]types.Type{types.TypeI32}, locals...)...)
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		for _, step := range body {
			b.Emit(step.Opcode(), step.Operands()...)
		}
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		for _, step := range tail {
			b.Emit(step.Opcode(), step.Operands()...)
		}
		prog, err := b.Build()
		require.NoError(t, err)

		profile := prof.New()
		i := interp.New(prog, append(opts, interp.WithProfiler(profile))...)
		require.NoError(t, i.Run(context.Background()))
		got, err := i.Pop()
		require.NoError(t, err)
		// Metrics land when the interpreter closes, so the count is read
		// after the run has finished rather than during it.
		require.NoError(t, i.Close())

		var entries float64
		for _, metric := range profile.Metrics() {
			if metric.Name == "vm_jit_native_entries_total" {
				entries += metric.Value
			}
		}
		return got, src, entries
	}

	t.Run("a read of every lowered field kind stays native and agrees with threaded", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			at   uint64
			typ  types.Type
			want types.Value
		}{
			{name: "bool", at: 0, typ: types.TypeI1, want: types.I1(true)},
			{name: "int8", at: 1, typ: types.TypeI8, want: types.I8(-8)},
			{name: "int16", at: 2, typ: types.TypeI32, want: types.I32(-300)},
			{name: "int32", at: 3, typ: types.TypeI32, want: types.I32(-70000)},
			{name: "int", at: 4, typ: types.TypeI64, want: types.I64(-1 << 40)},
			{name: "int64", at: 5, typ: types.TypeI64, want: types.I64(-1 << 40)},
			{name: "uint8", at: 6, typ: types.TypeI32, want: types.I32(200)},
			{name: "uint16", at: 7, typ: types.TypeI32, want: types.I32(60000)},
			// A uint32 field reaches the guest as the signed i32 its
			// conversion casts to, so the load sign-extends the same four
			// bytes rather than widening them.
			{name: "uint32", at: 8, typ: types.TypeI32, want: types.I32(-1)},
			{name: "uint64", at: 9, typ: types.TypeI64, want: types.I64(1 << 40)},
			{name: "float32", at: 10, typ: types.TypeF32, want: types.F32(1.5)},
			{name: "float64", at: 11, typ: types.TypeF64, want: types.F64(-2.5)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				locals := []types.Type{tt.typ}
				body := []instr.Instruction{
					instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at),
					instr.New(instr.STRUCT_GET), instr.New(instr.LOCAL_SET, 1),
				}
				tail := []instr.Instruction{instr.New(instr.LOCAL_GET, 1)}

				want, _, _ := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(-1))
				require.Equal(t, tt.want, want)

				got, _, entries := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(0))
				require.Equal(t, want, got)
				require.Greater(t, entries, float64(0), "expected a native entry")
				require.Less(t, entries, float64(size), "the read exits the native loop")
			})
		}
	})

	t.Run("a read the interpreter still owns agrees with threaded", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			at   uint64
			typ  types.Type
			want types.Value
		}{
			// A string field publishes a heap reference rather than loading
			// a word, so it has no row at all.
			{name: "no row for the field kind", at: 12, typ: types.TypeString, want: types.String("text")},
			// An i64 past the box payload cannot stay raw, so the read
			// leaves the loop where the interpreter spills it to the heap.
			{name: "a value past the box payload", at: 13, typ: types.TypeI64, want: types.I64(1 << 60)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				locals := []types.Type{tt.typ}
				body := []instr.Instruction{
					instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at),
					instr.New(instr.STRUCT_GET), instr.New(instr.LOCAL_SET, 1),
				}
				tail := []instr.Instruction{instr.New(instr.LOCAL_GET, 1)}

				want, _, _ := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(-1))
				require.Equal(t, tt.want, want)
				got, _, _ := run(t, locals, body, tail, interp.WithTick(1), interp.WithThreshold(0))
				require.Equal(t, want, got)
			})
		}
	})

	t.Run("a write of every exactly imaged field kind reaches the Go value", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			at    uint64
			store []instr.Instruction
			want  types.Value
			check func(*testing.T, hostLoopFields)
		}{
			{
				name: "bool", at: 0,
				store: []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_EQZ)},
				want:  types.I1(false),
				check: func(t *testing.T, got hostLoopFields) { require.False(t, got.Flag) },
			},
			{
				// No opcode makes an i8 constant, so the value written is the
				// one a read of the same field produced.
				name: "int8", at: 1,
				store: []instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET)},
				want:  types.I8(-8),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, int8(-8), got.I8) },
			},
			{
				name: "int32", at: 3,
				store: []instr.Instruction{instr.New(instr.I32_CONST, uint64(uint32(negI32)))},
				want:  types.I32(negI32),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, negI32, got.I32) },
			},
			{
				name: "int64", at: 5,
				store: []instr.Instruction{instr.New(instr.I64_CONST, uint64(negI64))},
				want:  types.I64(negI64),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, negI64, got.I64) },
			},
			{
				// A uint32 field is as wide as its slot, so the store writes
				// the same four bytes the conversion would reinterpret.
				name: "uint32", at: 8,
				store: []instr.Instruction{instr.New(instr.I32_CONST, uint64(uint32(negI32)))},
				want:  types.I32(negI32),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, uint32(0xFFFF_FF9D), got.U32) },
			},
			{
				name: "uint64", at: 9,
				store: []instr.Instruction{instr.New(instr.I64_CONST, uint64(negI64))},
				want:  types.I64(negI64),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, uint64(0xFFFF_FE00_0000_0000), got.U64) },
			},
			{
				name: "float32", at: 10,
				store: []instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(-3.5)))},
				want:  types.F32(-3.5),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, float32(-3.5), got.F32) },
			},
			{
				name: "float64", at: 11,
				store: []instr.Instruction{instr.New(instr.F64_CONST, math.Float64bits(4.25))},
				want:  types.F64(4.25),
				check: func(t *testing.T, got hostLoopFields) { require.Equal(t, float64(4.25), got.F64) },
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := append([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at)}, tt.store...)
				body = append(body, instr.New(instr.STRUCT_SET))
				tail := []instr.Instruction{
					instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, tt.at), instr.New(instr.STRUCT_GET),
				}

				want, threaded, _ := run(t, nil, body, tail, interp.WithTick(1), interp.WithThreshold(-1))
				require.Equal(t, tt.want, want)
				tt.check(t, threaded)

				got, jit, entries := run(t, nil, body, tail, interp.WithTick(1), interp.WithThreshold(0))
				require.Equal(t, want, got)
				require.Equal(t, threaded, jit, "the Go value diverged from the threaded run")
				require.Greater(t, entries, float64(0), "expected a native entry")
				require.Less(t, entries, float64(size), "the write exits the native loop")
			})
		}
	})

	t.Run("a write a range check governs agrees with threaded", func(t *testing.T) {
		// An int16 field is narrower than the i32 slot the guest writes, so
		// the store can overflow and the interpreter is the one that says so.
		for _, threshold := range []int{-1, 0} {
			setup := interp.New(program.New(nil))
			src := seed
			host, err := interp.NewRegistry().Marshal(setup, &src)
			require.NoError(t, err)
			i := interp.New(program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 70000), instr.New(instr.STRUCT_SET),
			}, program.WithConstants(host)), interp.WithTick(1), interp.WithThreshold(threshold))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrValueOverflow)
			require.Equal(t, int16(-300), src.I16, "the rejected write left the Go field alone")
			require.NoError(t, i.Close())
			require.NoError(t, setup.Close())
		}
	})

	t.Run("a field of another Go width at the same read exits", func(t *testing.T) {
		// A Go int16 and a Go int32 field both reach the guest as i32, so the
		// same compiled read serves both once the container arrives from the
		// stack. Only the kind guard keeps the second run from loading two
		// bytes of a four-byte field.
		b := program.NewBuilder()
		b.Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop, done := b.Label(), b.Label()
		b.Emit(instr.LOCAL_SET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 2)
		prog, err := b.Build()
		require.NoError(t, err)

		read := func(threshold int) (types.Value, types.Value, float64) {
			profile := prof.New()
			i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(threshold), interp.WithProfiler(profile))
			pass := func(value any) types.Value {
				host, err := i.Marshal(value)
				require.NoError(t, err)
				require.NoError(t, i.Push(host))
				require.NoError(t, i.Run(context.Background()))
				got, err := i.Pop()
				require.NoError(t, err)
				i.Reset()
				return got
			}
			narrow, wide := pass(&hostNarrowField{V: -300}), pass(&hostWideField{V: -70000})
			require.NoError(t, i.Close())
			var entries float64
			for _, metric := range profile.Metrics() {
				if metric.Name == "vm_jit_native_entries_total" {
					entries += metric.Value
				}
			}
			return narrow, wide, entries
		}

		narrow, wide, _ := read(-1)
		require.Equal(t, types.I32(-300), narrow)
		require.Equal(t, types.I32(-70000), wide)

		gotNarrow, gotWide, entries := read(0)
		require.Equal(t, narrow, gotNarrow)
		require.Equal(t, wide, gotWide)
		require.Greater(t, entries, float64(0), "expected a native entry")
	})

	t.Run("a write to a field a range check narrows agrees with threaded", func(t *testing.T) {
		// A uint8 field is narrower than the i32 slot the guest writes, so a
		// store past 255 has to report an overflow rather than truncate. The
		// loop writes in range long enough to compile before it does not.
		const wide = int32(64)
		b := program.NewBuilder()
		b.Locals(types.TypeAny, types.TypeI32)
		loop, done := b.Label(), b.Label()
		b.Emit(instr.LOCAL_SET, 0).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(wide))).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 6)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 200).Emit(instr.I32_ADD)
		b.Emit(instr.STRUCT_SET)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1)
		prog, err := b.Build()
		require.NoError(t, err)

		for _, threshold := range []int{-1, 0} {
			i := interp.New(prog, interp.WithTick(1), interp.WithThreshold(threshold))
			src := seed
			host, err := i.Marshal(&src)
			require.NoError(t, err)
			require.NoError(t, i.Push(host))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrValueOverflow)
			// 200+55 is the last value a uint8 holds, so the field stops
			// there instead of wrapping to what a raw byte store would leave.
			require.Equal(t, uint8(255), src.U8)
			require.NoError(t, i.Close())
		}
	})

	t.Run("a write a range check governs agrees with threaded", func(t *testing.T) {
		// An int16 field is narrower than the i32 slot the guest writes, so
		// the store can overflow and the interpreter is the one that says so.
		for _, threshold := range []int{-1, 0} {
			setup := interp.New(program.New(nil))
			src := seed
			host, err := interp.NewRegistry().Marshal(setup, &src)
			require.NoError(t, err)
			i := interp.New(program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 70000), instr.New(instr.STRUCT_SET),
			}, program.WithConstants(host)), interp.WithTick(1), interp.WithThreshold(threshold))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrValueOverflow)
			require.Equal(t, int16(-300), src.I16, "the rejected write left the Go field alone")
			require.NoError(t, i.Close())
			require.NoError(t, setup.Close())
		}
	})

	t.Run("a field of another Go width at the same read exits", func(t *testing.T) {
		// A Go int16 and a Go int32 field both reach the guest as i32, so one
		// STRUCT_GET alternating between them keeps the same VM kind while
		// changing the load width. Only the kind guard separates them.
		build := func() *program.Program {
			setup := interp.New(program.New(nil))
			defer func() { require.NoError(t, setup.Close()) }()
			registry := interp.NewRegistry()
			narrow, err := registry.Marshal(setup, &hostNarrowField{V: -300})
			require.NoError(t, err)
			wide, err := registry.Marshal(setup, &hostWideField{V: -70000})
			require.NoError(t, err)

			b := program.NewBuilder()
			require.Equal(t, 0, b.Const(narrow))
			require.Equal(t, 1, b.Const(wide))
			b.Locals(types.TypeI32, types.TypeAny, types.TypeI32)
			loop, odd, read, done := b.Label(), b.Label(), b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(odd)
			b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1).Br(read)
			b.Bind(odd)
			b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 1)
			b.Bind(read)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 2)
			prog, err := b.Build()
			require.NoError(t, err)
			return prog
		}

		read := func(threshold int) (types.Value, float64) {
			profile := prof.New()
			i := interp.New(build(), interp.WithTick(1), interp.WithThreshold(threshold), interp.WithProfiler(profile))
			require.NoError(t, i.Run(context.Background()))
			got, err := i.Pop()
			require.NoError(t, err)
			require.NoError(t, i.Close())
			var entries float64
			for _, metric := range profile.Metrics() {
				if metric.Name == "vm_jit_native_entries_total" {
					entries += metric.Value
				}
			}
			return got, entries
		}
		want, _ := read(-1)
		require.Equal(t, types.I32(-70000), want)
		got, entries := read(0)
		require.Equal(t, want, got)
		require.Greater(t, entries, float64(0), "expected a native entry")
	})

	t.Run("an index past the layout faults the same way", func(t *testing.T) {
		for _, threshold := range []int{-1, 0} {
			setup := interp.New(program.New(nil))
			src := seed
			host, err := interp.NewRegistry().Marshal(setup, &src)
			require.NoError(t, err)
			i := interp.New(program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 99), instr.New(instr.STRUCT_GET),
			}, program.WithConstants(host)), interp.WithTick(1), interp.WithThreshold(threshold))
			require.ErrorIs(t, i.Run(context.Background()), interp.ErrSegmentationFault)
			require.NoError(t, i.Close())
			require.NoError(t, setup.Close())
		}
	})
}
