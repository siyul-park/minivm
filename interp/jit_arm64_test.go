package interp

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// TestInterpreter_JITArraySetAfterBranchyCallsInLoop is a regression test for
// a SIGSEGV in generated ARM64 code: an outer row loop whose body inlines
// branchy F64 tree calls and ends each iteration with ARRAY_SET. Register
// pressure used to spill inside the terminal mutation trace, letting a branch
// skip spill-frame work and corrupt the Go stack.
func TestInterpreter_JITArraySetAfterBranchyCallsInLoop(t *testing.T) {
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
		WithParams(types.TypeF64Array).
		WithReturns(types.TypeF64)
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

	i := New(prog, WithTick(1), WithThreshold(1))
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

	ref := New(prog, WithTick(1), WithThreshold(-1))
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

// TestInterpreter_JITSideExitAbortNotCompletion is a regression test for a
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
func TestInterpreter_JITSideExitAbortNotCompletion(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	build := func() *program.Program {
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
			// MAP_LEN of a fresh map is always 0, which coincides with the
			// zero-seed Reset() gives global 1; add a nonzero sentinel so a
			// miscompile that silently skips this path (leaving global 1 at
			// its stale zero-seed) is observably wrong, not accidentally right.
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
		return prog
	}

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

	run := func(threshold int) []int32 {
		i := New(build(), WithTick(1), WithThreshold(threshold))
		defer i.Close()
		results := make([]int32, len(inputs))
		for n, x := range inputs {
			require.NoError(t, i.SetGlobal(0, types.BoxI32(x)))
			require.NoError(t, i.Run(context.Background()))
			v, err := i.Global(1)
			require.NoError(t, err)
			results[n] = v.I32()
			i.Reset()
		}
		return results
	}

	jit := run(1)
	ref := run(-1)
	require.Equal(t, ref, jit)
}

// TestNativeStackReserve verifies the arithmetic invariant tying three
// hand-synced constants together: asm.MaxSpillSlots (spill capacity),
// nativeFrameLimit (native call-depth cap), and the arm64 invoke
// trampoline's hard-coded stack reserve in abi_arm64.s. If any one of them
// is edited without the others, this test fails instead of the mismatch
// surfacing as a corrupted native stack at runtime. See docs/jit-internals.md
// for the full explanation.
func TestNativeStackReserve(t *testing.T) {
	const (
		spillSlotBytes  = 8 // one 64-bit value per spill slot
		frameRecordSize = journalStride * 8
		saveAreaBytes   = 80 // R19-R26 callee-saved save area (4 STP pairs, 16-byte aligned)
	)
	spillBytes := asm.MaxSpillSlots * spillSlotBytes
	callBytes := nativeFrameLimit * frameRecordSize
	reserve := spillBytes + callBytes

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	abiFile := filepath.Join(filepath.Dir(thisFile), "..", "asm", "arm64", "abi_arm64.s")
	src, err := os.ReadFile(abiFile)
	require.NoError(t, err)

	reserveLiteral := regexp.MustCompile(`ADD\s+\$(\d+),\s*RSP`).FindSubmatch(src)
	require.NotNil(t, reserveLiteral, "expected an ADD $N, RSP reserve instruction in %s", abiFile)
	reserveVal, err := strconv.Atoi(string(reserveLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, reserveVal, reserve,
		"asm.MaxSpillSlots*%d + nativeFrameLimit*journalStride*8 must equal the trampoline's ADD $N, RSP reserve", spillSlotBytes)

	frameLiteral := regexp.MustCompile(`TEXT ·invoke\(SB\), \$(\d+)-`).FindSubmatch(src)
	require.NotNil(t, frameLiteral, "expected a TEXT ·invoke(SB), $N-M frame size in %s", abiFile)
	frameVal, err := strconv.Atoi(string(frameLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, frameVal, reserve+saveAreaBytes,
		"the trampoline's TEXT frame size must equal the reserve plus the callee-saved save area")
}

// TestCompiler_Compile covers compiler-selected static plans and verifies that
// their native entries match threaded execution.
func TestCompiler_Compile(t *testing.T) {
	if runtime.GOARCH == "arm64" {
		t.Run("guard value", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.I32_DIV_S),
			}, program.WithGlobals(types.TypeI32, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			require.NoError(t, i.SetGlobal(0, types.BoxI32(8)))
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))

			entry, closeCompiler := compileNative(t, i, anchor{}, false)
			defer closeCompiler()
			assertNativeExit(t, i, entry, prof.ExitGuardValue, int(instr.I32_DIV_S))
		})

		t.Run("guard shape", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.ARRAY_LEN),
			}, program.WithConstants(types.TypedArray[int32]{1}, types.TypedArray[float64]{2}),
				program.WithGlobals(types.TypeRef))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			setGlobalConstant(t, i, 0, 0)
			entry, closeCompiler := compileNative(t, i, anchor{}, true)
			defer closeCompiler()
			setGlobalConstant(t, i, 0, 1)

			assertNativeExit(t, i, entry, prof.ExitGuardShape, int(instr.ARRAY_LEN))
		})

		t.Run("guard bounds", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{1}), program.WithGlobals(types.TypeRef, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			setGlobalConstant(t, i, 0, 0)
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			entry, closeCompiler := compileNative(t, i, anchor{}, true)
			defer closeCompiler()
			require.NoError(t, i.SetGlobal(1, types.BoxI32(2)))
			assertNativeExit(t, i, entry, prof.ExitGuardBounds, int(instr.ARRAY_GET))
		})

		t.Run("guard kind", func(t *testing.T) {
			typ := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))
			value := types.NewStruct(typ, types.BoxI32(1), types.BoxF64(2))
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.STRUCT_GET),
			}, program.WithConstants(value), program.WithGlobals(types.TypeRef, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			setGlobalConstant(t, i, 0, 0)
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			entry, closeCompiler := compileNative(t, i, anchor{}, true)
			defer closeCompiler()
			require.NoError(t, i.SetGlobal(1, types.BoxI32(1)))

			assertNativeExit(t, i, entry, prof.ExitGuardKind, int(instr.STRUCT_GET))
		})

		t.Run("cold branch", func(t *testing.T) {
			b := program.NewBuilder()
			cold := b.Label()
			done := b.Label()
			b.Globals(types.TypeI32).
				Emit(instr.GLOBAL_GET, 0).
				BrIf(cold).
				Emit(instr.I32_CONST, 1).
				Br(done).
				Bind(cold).
				Emit(instr.I32_CONST, 2).
				Bind(done)
			prog, err := b.Build()
			require.NoError(t, err)
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			require.NoError(t, i.SetGlobal(0, types.BoxI32(0)))
			entry, closeCompiler := compileNative(t, i, anchor{}, true)
			defer closeCompiler()
			require.NoError(t, i.SetGlobal(0, types.BoxI32(1)))

			assertNativeExit(t, i, entry, prof.ExitColdBranch, int(instr.BR_IF))
		})

		t.Run("trace cut", func(t *testing.T) {
			instructions := make([]instr.Instruction, opLimit+1)
			for idx := range instructions {
				instructions[idx] = instr.New(instr.NOP)
			}
			i := New(program.New(instructions), WithThreshold(-1))
			defer i.Close()
			entry, closeCompiler := compileNative(t, i, anchor{}, true)
			defer closeCompiler()

			assertNativeExit(t, i, entry, prof.ExitTraceCut, prof.OpcodeNone)
		})

		t.Run("terminal", func(t *testing.T) {
			i := New(program.New([]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(5.5)),
				instr.New(instr.F64_CONST, math.Float64bits(2)),
				instr.New(instr.F64_REM),
			}), WithThreshold(-1))
			defer i.Close()
			entry, closeCompiler := compileNative(t, i, anchor{}, false)
			defer closeCompiler()

			assertNativeExit(t, i, entry, prof.ExitTerminalOp, int(instr.F64_REM))
		})

		t.Run("loop exit", func(t *testing.T) {
			b := types.NewFunctionBuilder(nil).WithLocals(types.TypeI32)
			loop := b.Label()
			b.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 0)).
				Bind(loop).
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
					instr.New(instr.LOCAL_TEE, 0), instr.New(instr.I32_CONST, loopBudget+2), instr.New(instr.I32_LT_S)).
				BrIf(loop).
				Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
			fn, err := b.Build()
			require.NoError(t, err)
			prog := program.New([]instr.Instruction{instr.New(instr.NOP)}, program.WithConstants(fn))
			local := prof.NewCollector()
			i := New(prog, WithLocal(local), WithThreshold(-1))
			defer i.Close()
			addr := i.constants[0].Ref()
			i.fr.addr = addr
			i.fr.ref = addr
			i.fr.code = i.code[addr]
			i.fr.ip = 0
			i.fr.bp = 0
			i.sp = 1
			i.stack[0] = types.BoxI32(0)
			header := -1
			for ip := 0; ip < len(fn.Code); {
				inst := instr.Instruction(fn.Code[ip:])
				if inst.Opcode() == instr.BR_IF {
					header = instr.Targets(fn.Code, ip)[0]
					break
				}
				ip += inst.Width()
			}
			require.Greater(t, header, 0)
			for i.fr.ip < header {
				i.fr.code[i.fr.ip](i)
			}
			root := anchor{addr: addr, ip: header}
			addrLabel := strconv.Itoa(addr)
			headerLabel := strconv.Itoa(header)
			entry, closeCompiler := compileNative(t, i, root, true)
			defer closeCompiler()
			require.Equal(t, entryLoop, entry.kind)
			metrics := i.entryMetrics(root, entry)

			i.stack[i.fr.bp] = types.BoxI32(loopBudget + 2)
			i.fr.ip = header
			i.loop(entry.callable, metrics)(i)
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitLoop, opcode: int(instr.BR_IF)}, entry.exits[id])
			exits, ok := local.Metric("vm_jit_native_exits_total",
				prof.Label{Key: "func", Value: addrLabel}, prof.Label{Key: "ip", Value: headerLabel},
				prof.Label{Key: "kind", Value: "loop"}, prof.Label{Key: "frontend", Value: "trace"},
				prof.Label{Key: "reason", Value: "loop-exit"}, prof.Label{Key: "opcode", Value: "br_if"})
			require.True(t, ok)
			require.Equal(t, float64(1), exits)
		})

		t.Run("yield", func(t *testing.T) {
			fn := types.NewFunctionBuilder(nil).
				Emit(instr.New(instr.CONST_GET, 0), instr.New(instr.RETURN_CALL)).
				MustBuild()
			local := prof.NewCollector()
			i := New(program.New([]instr.Instruction{instr.New(instr.NOP)}, program.WithConstants(fn)),
				WithLocal(local), WithThreshold(-1))
			defer i.Close()
			addr := i.constants[0].Ref()
			i.fr.addr = addr
			i.fr.ref = addr
			i.fr.code = i.code[addr]
			i.fr.ip = 0
			i.fr.bp = 0
			i.sp = 0
			root := anchor{addr: addr}
			entry, closeCompiler := compileNative(t, i, root, true)
			defer closeCompiler()
			require.Equal(t, entryFunction, entry.kind)

			i.call(root, entry.callable, i.entryMetrics(root, entry))(i)
			require.Equal(t, uint64(trapYield), i.journal[journalTrap])
			require.Zero(t, i.journal[journalExitID])
			yields, ok := local.Metric("vm_jit_native_yields_total",
				prof.Label{Key: "func", Value: strconv.Itoa(addr)}, prof.Label{Key: "ip", Value: "0"},
				prof.Label{Key: "kind", Value: "call"}, prof.Label{Key: "frontend", Value: "trace"})
			require.True(t, ok)
			require.Equal(t, float64(1), yields)
			for _, metric := range local.Metrics() {
				require.NotEqual(t, "vm_jit_native_exits_total", metric.Name)
			}
		})
	}

	t.Run("attributes concrete guard exits to their opcode", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 8), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_DIV_S),
		}, program.WithLocals(types.TypeI32))
		i := New(prog, WithThreshold(-1))
		defer i.Close()
		c, err := newCompiler()
		require.NoError(t, err)
		defer c.Close()

		result := c.Compile(i, anchor{})
		require.NoError(t, result.err)
		require.NotNil(t, result.module)
		for _, exit := range result.module.entries[anchor{}].exits {
			if exit.reason == prof.ExitGuardValue {
				require.Equal(t, int(instr.I32_DIV_S), exit.opcode)
				return
			}
		}
		t.Fatal("missing guard-value exit")
	})

	t.Run("reports missing input", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		root := anchor{addr: -1, ip: 7}
		result := (&compiler{}).Compile(i, root)
		require.Equal(t, root, result.anchor)
		require.Nil(t, result.module)
		require.Equal(t, prof.FrontendNone, result.frontend)
		require.Equal(t, prof.CompileOutcomeEmpty, result.outcome)
		require.Equal(t, prof.CompileReasonNoInput, result.reason)
		require.NoError(t, result.err)
	})

	t.Run("keeps the deepest deterministic failure", func(t *testing.T) {
		deep := compileResult{frontend: prof.FrontendStatic, outcome: prof.CompileOutcomeRejected, reason: prof.CompileReasonRegisterPressure}
		shallow := []compileResult{
			{frontend: prof.FrontendTrace, outcome: prof.CompileOutcomeEmpty, reason: prof.CompileReasonNoPlan},
			{frontend: prof.FrontendTrace, outcome: prof.CompileOutcomeRejected, reason: prof.CompileReasonInvalidPlan},
		}
		for _, candidate := range shallow {
			require.Equal(t, deep, deeperCompileResult(deep, candidate))
		}
		require.Equal(t,
			compileResult{frontend: prof.FrontendTrace, outcome: prof.CompileOutcomeRejected, reason: prof.CompileReasonBranchRange},
			deeperCompileResult(deep, compileResult{frontend: prof.FrontendTrace, outcome: prof.CompileOutcomeRejected, reason: prof.CompileReasonBranchRange}),
		)
	})

	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	t.Run("straight-line arithmetic function compiles and matches threaded execution", func(t *testing.T) {
		// (a + b) * 2, exercising I32_ADD, I32_CONST, and I32_MUL — all within
		// the shared plan lowerer's scalar coverage — inside a single RETURN-terminated block.
		callee := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_MUL),
			instr.New(instr.RETURN),
		).MustBuild()

		b := program.NewBuilder()
		b.Globals(types.TypeI32)
		idx := b.Const(callee)
		// CALL pops the callee off the top of the stack, so the ref goes last:
		// args first (in declared param order), then CONST_GET of the function.
		b.Emit(instr.I32_CONST, 3).
			Emit(instr.I32_CONST, 4).
			Emit(instr.CONST_GET, uint64(idx)).
			Emit(instr.CALL).
			Emit(instr.GLOBAL_SET, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		i := New(prog, WithThreshold(-1))
		defer i.Close()

		c, err := newCompiler()
		require.NoError(t, err)
		defer c.Close()

		addr := int(i.constants[idx].Ref())
		result := c.Compile(i, anchor{addr: addr})
		require.NoError(t, result.err)
		mod := result.module
		require.NotEmpty(t, mod.entries)
		i.install(mod, false)

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, int32(14), got.I32())
	})

	t.Run("multi-block function compiles", func(t *testing.T) {
		b := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		})
		alt := b.Label()
		b.Emit(instr.New(instr.LOCAL_GET, 0)).
			BrIf(alt).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.RETURN)).
			Bind(alt).
			Emit(instr.New(instr.I32_CONST, 2)).
			Emit(instr.New(instr.RETURN))
		fn := b.MustBuild()

		i := New(program.New(nil))
		defer i.Close()

		c, err := newCompiler()
		require.NoError(t, err)
		defer c.Close()

		plans, err := staticPlan(&compileInput{address: 1, function: fn})
		require.NoError(t, err)
		require.NotEmpty(t, plans)
	})

	t.Run("branches and loops match threaded execution", func(t *testing.T) {
		calleeBuilder := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32)
		loop := calleeBuilder.Label()
		done := calleeBuilder.Label()
		calleeBuilder.Emit(instr.New(instr.I32_CONST, 0)).
			Emit(instr.New(instr.LOCAL_SET, 1)).
			Bind(loop).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_EQZ)).
			BrIf(done).
			Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_ADD)).
			Emit(instr.New(instr.LOCAL_SET, 1)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, 1)).
			Emit(instr.New(instr.I32_SUB)).
			Emit(instr.New(instr.LOCAL_SET, 0)).
			Br(loop).
			Bind(done).
			Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.RETURN))
		callee := calleeBuilder.MustBuild()

		b := program.NewBuilder()
		b.Globals(types.TypeI32)
		idx := b.Const(callee)
		b.Emit(instr.I32_CONST, 5).
			Emit(instr.CONST_GET, uint64(idx)).
			Emit(instr.CALL).
			Emit(instr.GLOBAL_SET, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		threaded := New(prog, WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Global(0)
		require.NoError(t, err)

		jit := New(prog, WithThreshold(-1))
		defer jit.Close()
		c, err := newCompiler()
		require.NoError(t, err)
		defer c.Close()
		addr := int(jit.constants[idx].Ref())
		result := c.Compile(jit, anchor{addr: addr})
		require.NoError(t, result.err)
		mod := result.module
		require.NotEmpty(t, mod.entries)
		jit.install(mod, false)
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Global(0)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("unsupported opcode compiles an exact fallback", func(t *testing.T) {
		// I32_DIV_S needs runtime trap semantics the baseline lowerer does not
		// duplicate, so the plan exits at that opcode and threaded dispatch owns it.
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32, types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		}).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_DIV_S),
			instr.New(instr.RETURN),
		).MustBuild()

		i := New(program.New(nil))
		defer i.Close()

		c, err := newCompiler()
		require.NoError(t, err)
		defer c.Close()

		plans, err := staticPlan(&compileInput{address: 1, function: fn})
		require.NoError(t, err)
		require.NotEmpty(t, plans)
	})
}

func compileNative(t *testing.T, i *Interpreter, root anchor, trace bool) (native, func()) {
	t.Helper()
	if trace {
		result, err := i.tracer.capture(i, root)
		require.NoError(t, err)
		require.NotNil(t, result.trace)
		i.stubs[root.addr] = i.code[root.addr][0]
	}
	c, err := newCompiler()
	require.NoError(t, err)
	result := c.Compile(i, root)
	require.NoError(t, result.err)
	require.NotNil(t, result.module, "%+v", result)
	entry, ok := result.module.entries[root]
	require.True(t, ok)
	return entry, func() { require.NoError(t, c.Close()) }
}

func assertNativeExit(t *testing.T, i *Interpreter, entry native, reason prof.ExitReason, opcode int) {
	t.Helper()
	require.NoError(t, entry.callable.Call(i.context()))
	require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
	encoded := i.journal[journalExitID]
	require.NotZero(t, encoded)
	id := int(encoded - 1)
	require.Less(t, id, len(entry.exits))
	require.Equal(t, exitDescriptor{reason: reason, opcode: opcode}, entry.exits[id])
	require.Equal(t, uint64(id+1), encoded)
}

func setGlobalConstant(t *testing.T, i *Interpreter, global, constant int) {
	t.Helper()
	value := i.constants[constant]
	i.retain(value.Ref())
	require.NoError(t, i.SetGlobal(global, value))
}

func TestArm64Lowerer_QueuesEachState(t *testing.T) {
	target := edge{anchor: anchor{addr: 1, ip: 2}, block: 0}
	ctx := &lowering{
		assembler: asm.New(arm64.New()),
		blocks:    []block{{anchor: target.anchor}},
		labels:    map[int]asm.Label{},
	}
	lowerer := arm64Lowerer{}

	ctx.values = []value{{kind: types.KindI32, raw: true, known: true, imm: 1}}
	first, ok := lowerer.label(ctx, target, nil, prof.OpcodeNone)
	require.True(t, ok)

	ctx.values = []value{{kind: types.KindI32, raw: true, known: true, imm: 2}}
	second, ok := lowerer.label(ctx, target, nil, prof.OpcodeNone)
	require.True(t, ok)

	require.NotEqual(t, first, second)
	require.Len(t, ctx.work, 2)
}
