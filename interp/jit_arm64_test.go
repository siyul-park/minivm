package interp

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

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

func TestARM64_Backedge(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	tests := []struct {
		name      string
		limit     int32
		threshold int
		attempted []bool
		installed bool
	}{
		{name: "compiles module loop", limit: 64, threshold: 0, attempted: []bool{true}, installed: true},
		{name: "warms eager loop", limit: 4, threshold: 0, attempted: []bool{false, true}},
		{name: "keeps sample threshold", limit: 4, threshold: 1, attempted: []bool{false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := program.NewBuilder()
			loop := b.Label()
			done := b.Label()
			b.Locals(types.TypeI32)
			b.Emit(instr.I32_CONST, 0).
				Emit(instr.LOCAL_SET, 0).
				Bind(loop).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, uint64(uint32(tt.limit))).
				Emit(instr.I32_GE_S).
				BrIf(done).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.I32_ADD).
				Emit(instr.LOCAL_SET, 0).
				Br(loop).
				Bind(done).
				Emit(instr.LOCAL_GET, 0)
			prog, err := b.Build()
			require.NoError(t, err)

			i := New(prog, WithTick(1<<20), WithThreshold(tt.threshold))
			defer i.Close()
			headers := i.tracer.headers(i, 0)
			require.NotEmpty(t, headers)
			root := anchor{ip: headers[0]}

			for run, attempted := range tt.attempted {
				require.NoError(t, i.Run(context.Background()))
				value, err := i.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, types.BoxI32(tt.limit), value)
				require.Equal(t, attempted, i.tried[root])
				if run+1 < len(tt.attempted) {
					i.Reset()
				}
			}
			if tt.installed {
				require.NotEmpty(t, i.exits)
			}
		})
	}
}

// LoopCarriedLocals protects write-back scalar locals in native loops. Hot
// backedges keep their slots stale; every side exit and safepoint yield must
// commit current registers before threaded execution observes the frame.
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
		const limit = int32(loopBudget + 3)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
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

// Flush protects the hot-backedge invariant directly: a dirty carried local
// remains register-authoritative and emits no VM-slot materialization.
func TestARM64_Flush(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	assembler := asm.New(arm64.New())
	reg := assembler.Reg(asm.RegTypeInt, asm.Width64)
	local := value{reg: reg, kind: types.KindI32, raw: true}
	ctx := &lowering{
		assembler: assembler,
		frames: []activation{{
			kinds:  []types.Kind{types.KindI32},
			locals: []value{local},
			state:  []localState{localLoaded | localDirty},
		}},
		carried: []carriedLocal{{value: local}},
	}

	require.True(t, (arm64Lowerer{}).flush(ctx, flushCommit))
	code, err := assembler.Build()
	require.NoError(t, err)
	require.Empty(t, code)
	require.Equal(t, localLoaded|localDirty, ctx.frames[0].state[0])
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

	jit := New(prog, WithTick(1), WithThreshold(1))
	defer jit.Close()
	threaded := New(prog, WithTick(1), WithThreshold(-1))
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

// TestNativeStackReserve verifies the arithmetic invariant tying three
// hand-synced constants together: asm.MaxSpillSlots (spill capacity),
// nativeFrameLimit (native call-depth cap), and the arm64 invoke
// trampoline's hard-coded stack reserve in abi_arm64.s. If any one of them
// is edited without the others, this test fails instead of the mismatch
// surfacing as a corrupted native stack at runtime. See docs/jit-internals.md
// for the full explanation.
func TestARM64_StackReserve(t *testing.T) {
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

			root := anchor{}
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)
			require.NoError(t, entry.callable.Call(i.journalPtr()))
			require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitGuardValue, opcode: int(instr.I32_DIV_S)}, entry.exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("guard shape", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.ARRAY_LEN),
			}, program.WithConstants(types.TypedArray[int32]{1}, types.TypedArray[float64]{2}),
				program.WithGlobals(types.TypeRef))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			{
				value := i.constants[0]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}
			root := anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)
			{
				value := i.constants[1]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}

			require.NoError(t, entry.callable.Call(i.journalPtr()))
			require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitGuardShape, opcode: int(instr.ARRAY_LEN)}, entry.exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("guard bounds", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{1}), program.WithGlobals(types.TypeRef, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			{
				value := i.constants[0]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			root := anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)
			require.NoError(t, i.SetGlobal(1, types.BoxI32(2)))
			require.NoError(t, entry.callable.Call(i.journalPtr()))
			require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitGuardBounds, opcode: int(instr.ARRAY_GET)}, entry.exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("primitive array set loop", func(t *testing.T) {
			array := make(types.TypedArray[int32], 64)
			b := program.NewBuilder()
			loop := b.Label()
			done := b.Label()
			b.Locals(types.TypeI32)
			b.Const(array)
			b.Emit(instr.I32_CONST, 0).
				Emit(instr.LOCAL_SET, 0).
				Bind(loop).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 64).
				Emit(instr.I32_GE_S).
				BrIf(done).
				ConstGet(array).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.ARRAY_SET).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.I32_ADD).
				Emit(instr.LOCAL_SET, 0).
				Br(loop).
				Bind(done).
				ConstGet(array).
				Emit(instr.I32_CONST, 0).
				Emit(instr.ARRAY_GET)
			prog, err := b.Build()
			require.NoError(t, err)
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })

			// Module code owning a loop compiles at the loop root, not at its
			// entry: the entry runs once per execution while the loop carries
			// the work, so the planner leaves that anchor to the loop.
			headers := i.tracer.headers(i, 0)
			require.NotEmpty(t, headers)
			compiled := compiler.Compile(i, anchor{ip: headers[0]})
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			i.install(compiled.module, false)
			require.NoError(t, i.Run(context.Background()))
			value, err := i.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(1), value)
		})

		t.Run("primitive array set branch", func(t *testing.T) {
			array := make(types.TypedArray[int32], 16)
			b := program.NewBuilder()
			loop := b.Label()
			skip := b.Label()
			done := b.Label()
			b.Locals(types.TypeI32)
			b.Const(array)
			b.Emit(instr.I32_CONST, 0).
				Emit(instr.LOCAL_SET, 0).
				Bind(loop).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 16).
				Emit(instr.I32_GE_S).
				BrIf(done).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.I32_AND).
				BrIf(skip).
				ConstGet(array).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.I32_ADD).
				Emit(instr.ARRAY_SET).
				Bind(skip).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.I32_ADD).
				Emit(instr.LOCAL_SET, 0).
				Br(loop).
				Bind(done).
				ConstGet(array).
				Emit(instr.I32_CONST, 0).
				Emit(instr.ARRAY_GET).
				ConstGet(array).
				Emit(instr.I32_CONST, 2).
				Emit(instr.ARRAY_GET).
				Emit(instr.I32_ADD)
			prog, err := b.Build()
			require.NoError(t, err)
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })

			compiled := compiler.Compile(i, anchor{})
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			i.install(compiled.module, false)

			require.NoError(t, i.Run(context.Background()))
			value, err := i.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(4), value)
		})

		t.Run("primitive array set continues", func(t *testing.T) {
			array := types.TypedArray[int32]{1}
			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.ARRAY_SET),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.ARRAY_GET),
			}, program.WithConstants(array))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })

			compiled := compiler.Compile(i, anchor{})
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			i.install(compiled.module, false)

			require.NoError(t, i.Run(context.Background()))
			value, err := i.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(2), value)
			require.Equal(t, int32(2), array[0])
		})

		t.Run("array get value guard", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{1}), program.WithGlobals(types.TypeRef, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			value := i.constants[0]
			i.retain(value.Ref())
			require.NoError(t, i.SetGlobal(0, value))
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			root := anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)

			for _, exit := range entry.exits {
				if exit.reason == prof.ExitGuardValue && exit.opcode == int(instr.ARRAY_GET) {
					return
				}
			}
			require.Fail(t, "missing array.get guard-value exit")
		})

		t.Run("guard kind", func(t *testing.T) {
			typ := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))
			value := types.NewStruct(typ, types.BoxI32(1), types.BoxF64(2))
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.STRUCT_GET),
			}, program.WithConstants(value), program.WithGlobals(types.TypeRef, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			{
				value := i.constants[0]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			root := anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)
			require.NoError(t, i.SetGlobal(1, types.BoxI32(1)))

			require.NoError(t, entry.callable.Call(i.journalPtr()))
			require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitGuardKind, opcode: int(instr.STRUCT_GET)}, entry.exits[id])
			require.Equal(t, uint64(id+1), encoded)
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
			root := anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)
			require.NoError(t, i.SetGlobal(0, types.BoxI32(1)))

			require.NoError(t, entry.callable.Call(i.journalPtr()))
			require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitColdBranch, opcode: int(instr.BR_IF)}, entry.exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("trace cut", func(t *testing.T) {
			instructions := make([]instr.Instruction, opLimit+1)
			for idx := range instructions {
				instructions[idx] = instr.New(instr.NOP)
			}
			i := New(program.New(instructions), WithThreshold(-1))
			defer i.Close()
			root := anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)

			require.NoError(t, entry.callable.Call(i.journalPtr()))
			require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitTraceCut, opcode: prof.OpcodeNone}, entry.exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("terminal", func(t *testing.T) {
			i := New(program.New([]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(5.5)),
				instr.New(instr.F64_CONST, math.Float64bits(2)),
				instr.New(instr.F64_REM),
			}), WithThreshold(-1))
			defer i.Close()
			root := anchor{}
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)

			require.NoError(t, entry.callable.Call(i.journalPtr()))
			require.Equal(t, uint64(trapFallback), i.journal[journalTrap])
			encoded := i.journal[journalExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.exits))
			require.Equal(t, exitDescriptor{reason: prof.ExitTerminalOp, opcode: int(instr.F64_REM)}, entry.exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("loop exit", func(t *testing.T) {
			b := types.NewFunctionBuilder(nil).Locals(types.TypeI32)
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
			i := New(prog, WithThreshold(-1))
			i.samples = local
			i.profiler = prof.New()
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
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)
			require.Equal(t, entryLoop, entry.kind)
			metrics := i.counters(root, entry)

			i.stack[i.fr.bp] = types.BoxI32(loopBudget + 2)
			i.fr.ip = header
			i.loop(root, entry, metrics, newWatchdog(entry))(i)
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
				WithThreshold(-1))
			i.samples = local
			i.profiler = prof.New()
			defer i.Close()
			addr := i.constants[0].Ref()
			i.fr.addr = addr
			i.fr.ref = addr
			i.fr.code = i.code[addr]
			i.fr.ip = 0
			i.fr.bp = 0
			i.sp = 0
			root := anchor{addr: addr}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.addr] = i.code[root.addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			compiled := compiler.Compile(i, root)
			require.NoError(t, compiled.err)
			require.NotNil(t, compiled.module, "%+v", compiled)
			entry, ok := compiled.module.entries[root]
			require.True(t, ok)
			require.Equal(t, entryFunction, entry.kind)

			i.call(root, entry, i.counters(root, entry), newWatchdog(entry))(i)
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
		require.Fail(t, "missing guard-value exit")
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
		}).Locals(types.TypeI32)
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
		Params(types.TypeI32, types.TypeRef).
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
	jit := New(prog, WithProfiler(profile))
	threaded := New(prog, WithThreshold(-1))

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
		if metric.Name == "vm_jit_native_entries_total" {
			entries += metric.Value
		}
	}
	require.Greater(t, entries, float64(0))
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

		jit := New(prog, WithTick(1), WithThreshold(0))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(1), got)
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, 1, i.rc[ref]) // the local slot's own retain, never doubled or dropped
			require.Equal(t, threaded.rc[1:], i.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, 1, i.rc[g.Ref()]) // the global slot's own retain, never doubled or dropped
			require.Equal(t, threaded.rc[1:], i.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, i.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			v, err := i.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, v, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(3*size), v)
			require.Equal(t, threaded.rc[1:], i.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[wantLocal.Ref()], i.rc[l.Ref()])
			require.Equal(t, threaded.rc[1:], i.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		i := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[wantLocal.Ref()], i.rc[l.Ref()])
			require.Equal(t, threaded.rc[1:], i.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		ref := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 48; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, ref.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := ref.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, ref.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		store := types.NewFunctionBuilder(nil).Params(types.NewArrayType(types.TypeRef), types.TypeI32).Returns(types.TypeI32)
		store.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_NULL), instr.New(instr.ARRAY_SET),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.RETURN),
		)
		fn, err := store.Build()
		require.NoError(t, err)

		b := program.NewBuilder()
		refArrTyp := b.Type(types.NewArrayType(types.TypeRef))
		b.Const(fn)
		b.Locals(types.NewArrayType(types.TypeRef), types.TypeI32, types.TypeI32)
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
		refArray := types.NewArrayType(types.TypeRef)
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
		fill := types.NewFunctionBuilder(nil).Params(types.TypeI32Array, types.TypeI32, types.TypeRef).Returns(types.TypeI32)
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
	jit := New(prog, WithProfiler(profile))
	threaded := New(prog, WithThreshold(-1))
	for n := 0; n < 16; n++ {
		require.NoError(t, jit.Run(context.Background()))
		require.NoError(t, threaded.Run(context.Background()))
		got, err := jit.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.PopBoxed()
		require.NoError(t, err)
		require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
		require.Equal(t, types.BoxI32(size), got)
		require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(size), got)
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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

		jit := New(prog, WithTick(1), WithThreshold(0))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 32; n++ {
			gotErr := jit.Run(context.Background())
			wantErr := threaded.Run(context.Background())
			require.Error(t, wantErr)
			require.Error(t, gotErr)
			require.Equal(t, wantErr.Error(), gotErr.Error(), "error diverged from threaded on iteration %d", n)
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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

		jit := New(prog, WithTick(1), WithThreshold(0))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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

		jit := New(prog, WithTick(1), WithThreshold(0))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
		for n := 0; n < 32; n++ {
			require.NoError(t, jit.Run(context.Background()))
			require.NoError(t, threaded.Run(context.Background()))
			got, err := jit.PopBoxed()
			require.NoError(t, err)
			want, err := threaded.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, want, got, "result diverged from threaded on iteration %d", n)
			require.Equal(t, types.BoxI32(3*size), got)
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		b.Locals(types.TypeRef, types.TypeI32, types.TypeI32, types.TypeI32)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		b.Locals(types.TypeRef, types.TypeI32, types.TypeI32)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		jit := New(prog, WithTick(1), WithThreshold(0), WithProfiler(profile))
		threaded := New(prog, WithTick(1), WithThreshold(-1))
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
			require.Equal(t, threaded.rc[1:], jit.rc[1:], "refcount diverged from threaded on iteration %d", n)
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
		b.Locals(types.TypeI32Array, structTyp, types.TypeRef, types.TypeI32, types.TypeI32)
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
		b.Locals(types.TypeRef, types.TypeI32, types.TypeI32)
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
		b.Locals(types.TypeRef, types.TypeI32, types.TypeI32)
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
		b.Locals(types.TypeI32Array, types.TypeRef, types.TypeI32Array, types.TypeRef, types.TypeI32, types.TypeI32)
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
		b.Locals(types.TypeRef, types.TypeI32, types.TypeI32)
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
