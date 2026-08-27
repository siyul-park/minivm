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

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// Flush protects the hot-backedge invariant directly: a dirty carried local
// remains register-authoritative and emits no VM-slot materialization.
//
// Exception (docs/coding-patterns.md §1.1): this test exercises
// arm64Lowerer.flush, owned by interp/jit_arm64.go, not interp/jit.go. It is
// parked in this file because the ARM64 lowerer has no external-package test
// home until the backend moves to internal/jit/arm64; it moves there with it.
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
		frameRecordSize = 1 << journal.Shift
		saveAreaBytes   = 80 // R19-R26 callee-saved save area (4 STP pairs, 16-byte aligned)
	)
	spillBytes := asm.MaxSpillSlots * spillSlotBytes
	callBytes := nativeFrameLimit * frameRecordSize
	reserve := spillBytes + callBytes

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	abiFile := filepath.Join(filepath.Dir(thisFile), "..", "internal", "asm", "arm64", "abi_arm64.s")
	src, err := os.ReadFile(abiFile)
	require.NoError(t, err)

	reserveLiteral := regexp.MustCompile(`ADD\s+\$(\d+),\s*RSP`).FindSubmatch(src)
	require.NotNil(t, reserveLiteral, "expected an ADD $N, RSP reserve instruction in %s", abiFile)
	reserveVal, err := strconv.Atoi(string(reserveLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, reserveVal, reserve,
		"asm.MaxSpillSlots*%d + nativeFrameLimit*(1<<journal.Shift) must equal the trampoline's ADD $N, RSP reserve", spillSlotBytes)

	frameLiteral := regexp.MustCompile(`TEXT ·invoke\(SB\), \$(\d+)-`).FindSubmatch(src)
	require.NotNil(t, frameLiteral, "expected a TEXT ·invoke(SB), $N-M frame size in %s", abiFile)
	frameVal, err := strconv.Atoi(string(frameLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, frameVal, reserve+saveAreaBytes,
		"the trampoline's TEXT frame size must equal the reserve plus the callee-saved save area")
}

// TestCompiler_Compile covers the compiler-selected static and traced plans
// whose exit encoding, frame splicing, or bare static-plan shape has no public
// observable: bypassing dispatch through entry.Callable.Call to read the
// journal trap directly, driving a loop or call entry from a hand-spliced
// i.fr, or asserting staticPlan's return shape without ever running it. The
// subset of this contract reachable by warming a program up naturally and
// observing Run's result or a profiler metric lives in
// interp_test.TestARM64_StaticPlanMatchesThreaded instead.
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

			root := jit.Anchor{}
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)
			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitGuardValue, Opcode: int(instr.I32_DIV_S)}, entry.Exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("guard shape", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.ARRAY_LEN),
			}, program.WithConstants(types.TypedArray[int32]{1}, types.TypedArray[float64]{2}),
				program.WithGlobals(types.TypeAny))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			{
				value := i.constants[0]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}
			root := jit.Anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)
			{
				value := i.constants[1]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}

			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitGuardShape, Opcode: int(instr.ARRAY_LEN)}, entry.Exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("guard bounds", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{1}), program.WithGlobals(types.TypeAny, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			{
				value := i.constants[0]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			root := jit.Anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)
			require.NoError(t, i.SetGlobal(1, types.BoxI32(2)))
			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitGuardBounds, Opcode: int(instr.ARRAY_GET)}, entry.Exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		// array.get's guard-value exit for a global-backed I32 array is
		// registered by sideExit at compile time whether or not the lowered
		// code ever branches to it: for this shape (non-hoisted, non-I64,
		// non-owned container) neither guardBoxable nor guardRC's branch
		// applies, so the slot is unreachable from any runtime input. Only
		// the compiled entry's exit table can observe it.
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
			input, ok := i.compileSnapshot(0)
			require.True(t, ok)
			compiled := compiler.Compile(input, jit.Anchor{IP: headers[0]})
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			i.install(compiled.Code, false)
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

			input, ok := i.compileSnapshot(0)
			require.True(t, ok)
			compiled := compiler.Compile(input, jit.Anchor{})
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			i.install(compiled.Code, false)

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

			input, ok := i.compileSnapshot(0)
			require.True(t, ok)
			compiled := compiler.Compile(input, jit.Anchor{})
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			i.install(compiled.Code, false)

			require.NoError(t, i.Run(context.Background()))
			value, err := i.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(2), value)
			require.Equal(t, int32(2), array[0])
		})

		t.Run("array get value guard", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int32]{1}), program.WithGlobals(types.TypeAny, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			value := i.constants[0]
			i.retain(value.Ref())
			require.NoError(t, i.SetGlobal(0, value))
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			root := jit.Anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)

			for _, exit := range entry.Exits {
				if exit.Reason == prof.ExitGuardValue && exit.Opcode == int(instr.ARRAY_GET) {
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
			}, program.WithConstants(value), program.WithGlobals(types.TypeAny, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			{
				value := i.constants[0]
				i.retain(value.Ref())
				require.NoError(t, i.SetGlobal(0, value))
			}
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			root := jit.Anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)
			require.NoError(t, i.SetGlobal(1, types.BoxI32(1)))

			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitGuardKind, Opcode: int(instr.STRUCT_GET)}, entry.Exits[id])
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
			root := jit.Anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)
			require.NoError(t, i.SetGlobal(0, types.BoxI32(1)))

			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitColdBranch, Opcode: int(instr.BR_IF)}, entry.Exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("trace cut", func(t *testing.T) {
			instructions := make([]instr.Instruction, opLimit+1)
			for idx := range instructions {
				instructions[idx] = instr.New(instr.NOP)
			}
			i := New(program.New(instructions), WithThreshold(-1))
			defer i.Close()
			root := jit.Anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)

			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitTraceCut, Opcode: prof.OpcodeNone}, entry.Exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		t.Run("terminal", func(t *testing.T) {
			i := New(program.New([]instr.Instruction{
				instr.New(instr.F64_CONST, math.Float64bits(5.5)),
				instr.New(instr.F64_CONST, math.Float64bits(2)),
				instr.New(instr.F64_REM),
			}), WithThreshold(-1))
			defer i.Close()
			root := jit.Anchor{}
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)

			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitTerminalOp, Opcode: int(instr.F64_REM)}, entry.Exits[id])
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
			root := jit.Anchor{Addr: addr, IP: header}
			addrLabel := strconv.Itoa(addr)
			headerLabel := strconv.Itoa(header)
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)
			require.Equal(t, jit.EntryLoop, entry.Kind)
			metrics := i.counters(root, entry)

			i.stack[i.fr.bp] = types.BoxI32(loopBudget + 2)
			i.fr.ip = header
			i.loop(root, entry, metrics, newWatchdog(entry))(i)
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.ExitDescriptor{Reason: prof.ExitLoop, Opcode: int(instr.BR_IF)}, entry.Exits[id])
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
			root := jit.Anchor{Addr: addr}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.stubs[root.Addr] = i.code[root.Addr][0]
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			entry, ok := compiled.Code.Entries[root]
			require.True(t, ok)
			require.Equal(t, jit.EntryFunction, entry.Kind)

			i.call(root, entry, i.counters(root, entry), newWatchdog(entry))(i)
			require.Equal(t, uint64(journal.TrapYield), i.journal[journal.CellTrap])
			require.Zero(t, i.journal[journal.CellExitID])
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

		input, ok := i.compileSnapshot(0)
		require.True(t, ok)
		result := c.Compile(input, jit.Anchor{})
		require.NoError(t, result.Err)
		require.NotNil(t, result.Code)
		for _, exit := range result.Code.Entries[jit.Anchor{}].Exits {
			if exit.Reason == prof.ExitGuardValue {
				require.Equal(t, int(instr.I32_DIV_S), exit.Opcode)
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
		input, ok := i.compileSnapshot(addr)
		require.True(t, ok)
		result := c.Compile(input, jit.Anchor{Addr: addr})
		require.NoError(t, result.Err)
		mod := result.Code
		require.NotEmpty(t, mod.Entries)
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

		plans, err := jit.StaticPlan(&jit.Input{Address: 1, Function: fn})
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

		native := New(prog, WithThreshold(-1))
		defer native.Close()
		c, err := newCompiler()
		require.NoError(t, err)
		defer c.Close()
		addr := int(native.constants[idx].Ref())
		input, ok := native.compileSnapshot(addr)
		require.True(t, ok)
		result := c.Compile(input, jit.Anchor{Addr: addr})
		require.NoError(t, result.Err)
		mod := result.Code
		require.NotEmpty(t, mod.Entries)
		native.install(mod, false)
		require.NoError(t, native.Run(context.Background()))
		got, err := native.Global(0)
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

		plans, err := jit.StaticPlan(&jit.Input{Address: 1, Function: fn})
		require.NoError(t, err)
		require.NotEmpty(t, plans)
	})
}

// Backedge covers when a module loop is attempted for compilation, which
// interp records only as private install bookkeeping: whether the root was
// already tried and whether an entry is installed. No profiling metric
// separates "attempted, not installed" from "not attempted", so the claim has
// no public observable.
//
// Exception (docs/coding-patterns.md §1.1): the symbols it reads are owned by
// interp/trace.go and interp/interp.go rather than interp/jit.go. It is parked
// with the other JIT white-box tests until the compiler moves to internal/jit,
// where the same bookkeeping becomes that package's own contract.
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
		{name: "compiles module loop", limit: 64, threshold: 8, attempted: []bool{true}, installed: true},
		{name: "warms loop across runs", limit: 4, threshold: 3, attempted: []bool{false, true}},
		{name: "keeps hot threshold", limit: 4, threshold: 64, attempted: []bool{false, false}},
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
			root := jit.Anchor{IP: headers[0]}

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
