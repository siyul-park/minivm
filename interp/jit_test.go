package interp

import (
	"context"
	"math"
	"runtime"
	"strconv"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/frontend"
	"github.com/siyul-park/minivm/internal/jit/tier"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

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
			require.Equal(t, jit.Exit{Reason: prof.ExitGuardValue, Opcode: int(instr.I32_DIV_S)}, entry.Exits[id])
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
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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
			require.Equal(t, jit.Exit{Reason: prof.ExitGuardShape, Opcode: int(instr.ARRAY_LEN)}, entry.Exits[id])
			require.Equal(t, uint64(id+1), encoded)
		})

		// The SSA machine's guards are the first exits it emits, so this is
		// where their cold stub is judged: not that a stub was emitted, but
		// that the interpreter picks execution up exactly where the guard
		// refused it, with the operand stack and the reference counts the
		// threaded handler about to run expects. Both of the read's exits
		// are taken here - the shape guard in front of it and the bounds
		// test inside it, which leaves through the same state.
		//
		// A borrowed container is the case that can only be caught here: it
		// derives its count from the global slot native code is about to
		// leave behind, so the stub owes it the retain the interpreter will
		// release, and no run that does not deoptimize can tell the
		// difference.
		t.Run("a cold stub resumes the interpreter", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			},
				program.WithConstants(
					types.TypedArray[int32]{70, 80, 90},
					types.NewArray(types.TypeI32Array, types.BoxI32(7), types.BoxI32(8), types.BoxI32(9)),
				),
				program.WithGlobals(types.TypeI32Array, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()

			// The recording is what resolves the element shape: a global
			// carries only its kind, so bytecode alone names no container.
			speculated, other := i.constants[0], i.constants[1]
			i.retain(speculated.Ref())
			require.NoError(t, i.SetGlobal(0, speculated))
			require.NoError(t, i.SetGlobal(1, types.BoxI32(1)))
			root := jit.Anchor{}
			require.NotNil(t, i.tracer.capture(i, root).trace)
			i.exits[root] = i.code[root.Addr][0]

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
			require.Equal(t, []jit.Exit{
				{Reason: prof.ExitGuardShape, Opcode: int(instr.ARRAY_GET)},
				{Reason: prof.ExitGuardBounds, Opcode: int(instr.ARRAY_GET)},
			}, entry.Exits, "the SSA machine emits the shape guard and the read's bounds test, in that order")

			at := instr.New(instr.GLOBAL_GET, 0).Width() * 2

			// The read's own bounds test leaves through the state its guard
			// carries, so an index past the end exits at the same opcode the
			// shape guard would have.
			require.NoError(t, i.SetGlobal(1, types.BoxI32(9)))
			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			require.Equal(t, uint64(at), i.journal[journal.CellNextIP])
			require.Equal(t, jit.Exit{Reason: prof.ExitGuardBounds, Opcode: int(instr.ARRAY_GET)}, entry.Exits[i.journal[journal.CellExitID]-1])
			require.NoError(t, i.SetGlobal(1, types.BoxI32(1)))

			// A ref array of the same declared type is a container of another
			// concrete type, so the shape the read was compiled against no
			// longer holds and the guard must refuse it before the load.
			i.retain(other.Ref())
			require.NoError(t, i.SetGlobal(0, other))
			counted := i.rc[other.Ref()]

			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			require.Equal(t, uint64(at), i.journal[journal.CellNextIP], "a guard resumes at the opcode it refused, never past it")
			require.Equal(t, jit.Exit{Reason: prof.ExitGuardShape, Opcode: int(instr.ARRAY_GET)}, entry.Exits[i.journal[journal.CellExitID]-1])

			i.sp = int(i.journal[journal.CellSP])
			i.deopt()
			require.Equal(t, 2, i.sp)
			require.Equal(t, at, i.fr.ip)
			require.Equal(t, []types.Boxed{other, types.BoxI32(1)}, i.stack[:i.sp], "the interpreter resumes with the operands the opcode started with")
			require.Equal(t, counted+1, i.rc[other.Ref()], "a borrowed container is retained for the interpreter that adopts it")

			// Running the opcode the guard refused balances that retain: the
			// threaded handler releases the container it pops, which is the
			// count the stub took.
			i.code[root.Addr][i.fr.ip](i)
			require.Equal(t, types.BoxI32(8), i.stack[i.sp-1])
			require.Equal(t, counted, i.rc[other.Ref()])
		})

		// The hazard the store rule exists for, end to end: a global declared
		// any currently holds a reference, and a scalar written over it has
		// to release that reference. Neither pipeline lowers such a store -
		// the SSA machine refuses it on the slot's kind, the plan pipeline's
		// globalSet on the same fact - so nothing is installed and the root
		// stays threaded. What a weakened rule would produce instead is
		// native code that writes the slot and drops the count, which the
		// refcount assertion below is what catches.
		t.Run("a scalar store over a reference global keeps its count", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5), instr.New(instr.GLOBAL_SET, 0),
			}, program.WithConstants(types.TypedArray[int32]{1}), program.WithGlobals(types.TypeAny))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			value := i.constants[0]
			i.retain(value.Ref())
			require.NoError(t, i.SetGlobal(0, value))
			counted := i.rc[value.Ref()]

			root := jit.Anchor{}
			compiler, err := newCompiler()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, compiler.Close()) })
			input, ok := i.compileSnapshot(root.Addr)
			require.True(t, ok)
			require.Equal(t, types.KindRef, input.Globals[0], "the snapshot reports what the slot holds, which is what the rule reads")
			compiled := compiler.Compile(input, root)
			require.NoError(t, compiled.Err)
			require.Nil(t, compiled.Code, "a store that would drop a reference count is compiled by neither pipeline")

			require.NoError(t, i.Run(context.Background()))
			require.Equal(t, types.BoxI32(5), i.globals[0])
			require.Equal(t, counted-1, i.rc[value.Ref()], "the overwritten reference is released exactly once")
		})

		// An i64 element lowers through the SSA machine the same way an i32's
		// does (see arm64/read.go): the shape guard, its own bounds test, and
		// the boxability guard the raw i64 element needs once loaded, since a
		// typed array stores it unboxed rather than tag-boxed. This subtest
		// pins that the bounds test still resumes correctly for an i64
		// element specifically; the shape guard's own stub is covered where
		// every read shares it, in "a cold stub resumes the interpreter"
		// above, and the boxability guard's stub in
		// TestNew's own i64 goldens.
		t.Run("guard bounds, i64 element", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int64]{1}), program.WithGlobals(types.TypeAny, types.TypeI32))
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
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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
			require.Equal(t, []jit.Exit{
				{Reason: prof.ExitGuardShape, Opcode: int(instr.ARRAY_GET)},
				{Reason: prof.ExitGuardBounds, Opcode: int(instr.ARRAY_GET)},
				{Reason: prof.ExitGuardValue, Opcode: int(instr.ARRAY_GET)},
			}, entry.Exits, "the SSA machine emits the shape guard, the read's bounds test, and the i64 boxability guard, in that order")

			require.NoError(t, i.SetGlobal(1, types.BoxI32(2)))
			require.NoError(t, entry.Callable.Call(i.journalPtr()))
			require.Equal(t, uint64(journal.TrapFallback), i.journal[journal.CellTrap])
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.Exit{Reason: prof.ExitGuardBounds, Opcode: int(instr.ARRAY_GET)}, entry.Exits[id])
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
			headers := i.tracer.headers(i.instrs, 0)
			require.NotEmpty(t, headers)
			input, ok := i.compileSnapshot(0)
			require.True(t, ok)
			compiled := compiler.Compile(input, jit.Anchor{IP: headers[0].header})
			require.NoError(t, compiled.Err)
			require.NotNil(t, compiled.Code, "%+v", compiled)
			i.install(compiled.Code)
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
			i.install(compiled.Code)

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
			i.install(compiled.Code)

			require.NoError(t, i.Run(context.Background()))
			value, err := i.PopBoxed()
			require.NoError(t, err)
			require.Equal(t, types.BoxI32(2), value)
			require.Equal(t, int32(2), array[0])
		})

		// An i64 element is the shape that reaches the plan pipeline's own
		// value guard: it may be heap-promoted, so the read has to prove the
		// loaded word still fits the boxed lane before it continues. It is
		// also a shape the SSA machine declines, so the compiler still
		// selects a plan for this root.
		t.Run("array get value guard", func(t *testing.T) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.GLOBAL_GET, 0), instr.New(instr.GLOBAL_GET, 1), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int64]{1}), program.WithGlobals(types.TypeAny, types.TypeI32))
			i := New(prog, WithThreshold(-1))
			defer i.Close()
			value := i.constants[0]
			i.retain(value.Ref())
			require.NoError(t, i.SetGlobal(0, value))
			require.NoError(t, i.SetGlobal(1, types.BoxI32(0)))
			root := jit.Anchor{}
			capture := i.tracer.capture(i, root)
			require.NotNil(t, capture.trace)
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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
			require.Equal(t, jit.Exit{Reason: prof.ExitGuardKind, Opcode: int(instr.STRUCT_GET)}, entry.Exits[id])
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
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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
			require.Equal(t, jit.Exit{Reason: prof.ExitColdBranch, Opcode: int(instr.BR_IF)}, entry.Exits[id])
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
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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
			require.Equal(t, jit.Exit{Reason: prof.ExitTraceCut, Opcode: prof.OpcodeNone}, entry.Exits[id])
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
			require.Equal(t, jit.Exit{Reason: prof.ExitTerminalOp, Opcode: int(instr.F64_REM)}, entry.Exits[id])
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
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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
			i.cycle(root, entry, metrics, tier.New(entry))(i)
			encoded := i.journal[journal.CellExitID]
			require.NotZero(t, encoded)
			id := int(encoded - 1)
			require.Less(t, id, len(entry.Exits))
			require.Equal(t, jit.Exit{Reason: prof.ExitLoop, Opcode: int(instr.BR_IF)}, entry.Exits[id])
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
			i.exits[jit.Anchor{Addr: root.Addr}] = i.code[root.Addr][0]
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

			i.cycle(root, entry, i.counters(root, entry), tier.New(entry))(i)
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
		i.install(mod)

		require.NoError(t, i.Run(context.Background()))
		got, err := i.Global(0)
		require.NoError(t, err)
		require.Equal(t, int32(14), got.I32())
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
		native.install(mod)
		require.NoError(t, native.Run(context.Background()))
		got, err := native.Global(0)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	// A typed array's i64 element now lowers through the SSA machine, which
	// TestNew's own goldens pin structurally. What they cannot prove is that
	// running the compiled code end to end produces the value a threaded run
	// would: this does, for an element the boxed payload holds inline and one
	// Interpreter.boxI64 heap-promotes instead, resolving either result
	// through Load so a promoted ref's own heap address - necessarily
	// different between the two independent interpreters below - never
	// enters the comparison, and balancing the reference PopBoxed hands over
	// with Release once RefCount has confirmed the resumed read left it
	// owned exactly once.
	t.Run("i64 array element matches threaded execution", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native JIT is only available on arm64")
		}
		resolve := func(t *testing.T, i *Interpreter) types.Value {
			t.Helper()
			boxed, err := i.PopBoxed()
			require.NoError(t, err)
			if boxed.Kind() != types.KindRef {
				return types.Unbox(boxed)
			}
			count, err := i.RefCount(boxed.Ref())
			require.NoError(t, err)
			require.Equal(t, 1, count)
			val, err := i.Load(boxed.Ref())
			require.NoError(t, err)
			require.NoError(t, i.Release(boxed.Ref()))
			return val
		}
		check := func(t *testing.T, elem int64) {
			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
			}, program.WithConstants(types.TypedArray[int64]{elem}))

			threaded := New(prog, WithThreshold(-1))
			defer threaded.Close()
			require.NoError(t, threaded.Run(context.Background()))
			want := resolve(t, threaded)

			native := New(prog, WithThreshold(-1))
			defer native.Close()
			c, err := newCompiler()
			require.NoError(t, err)
			defer c.Close()
			input, ok := native.compileSnapshot(0)
			require.True(t, ok)
			result := c.Compile(input, jit.Anchor{})
			require.NoError(t, result.Err)
			require.NotEmpty(t, result.Code.Entries)
			native.install(result.Code)
			require.NoError(t, native.Run(context.Background()))
			got := resolve(t, native)

			require.Equal(t, want, got)
		}
		t.Run("in range", func(t *testing.T) { check(t, 42) })
		t.Run("heap-promoted", func(t *testing.T) { check(t, math.MaxInt64) })
	})
}

// TestARM64_Encloses covers the containment the installer arbitrates on: a
// nested loop header lies inside the enclosing loop's body, while a sibling
// header only follows it. install refuses a static plan that would swallow a
// loop root already dispatching, and that refusal turns entirely on telling
// those two shapes apart (see Interpreter.covered).
//
// Exception (docs/coding-patterns.md §1.1): loop spans and install ownership
// are interp-private bookkeeping with no profiler metric of their own, the
// same reason TestARM64_Backedge above reads `tried` and `exits`.
func TestARM64_Encloses(t *testing.T) {
	t.Run("an outer loop encloses a nested header", func(t *testing.T) {
		b := program.NewBuilder()
		outer := b.Label()
		outerDone := b.Label()
		inner := b.Label()
		innerDone := b.Label()
		b.Locals(types.TypeI32, types.TypeI32)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0).
			Bind(outer).
			Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(outerDone).
			Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
			Bind(inner).
			Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone).
			Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
			Br(inner).
			Bind(innerDone).
			Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).
			Br(outer).
			Bind(outerDone).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		i := New(prog, WithTick(1<<20), WithThreshold(-1))
		defer i.Close()
		spans := i.tracer.headers(i.instrs, 0)
		require.Len(t, spans, 2)

		outerHeader, innerHeader := spans[0].header, spans[1].header
		if outerHeader > innerHeader {
			outerHeader, innerHeader = innerHeader, outerHeader
		}
		require.True(t, i.tracer.encloses(i.instrs, 0, outerHeader, innerHeader),
			"the outer header must enclose the nested one")
		require.False(t, i.tracer.encloses(i.instrs, 0, innerHeader, outerHeader),
			"containment must not run the other way")
	})

	t.Run("sequential loops enclose neither the other", func(t *testing.T) {
		b := program.NewBuilder()
		first := b.Label()
		firstDone := b.Label()
		second := b.Label()
		secondDone := b.Label()
		b.Locals(types.TypeI32, types.TypeI32)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0).
			Bind(first).
			Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(firstDone).
			Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).
			Br(first).
			Bind(firstDone).
			Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1).
			Bind(second).
			Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(secondDone).
			Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).
			Br(second).
			Bind(secondDone).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)

		i := New(prog, WithTick(1<<20), WithThreshold(-1))
		defer i.Close()
		spans := i.tracer.headers(i.instrs, 0)
		require.Len(t, spans, 2)

		require.False(t, i.tracer.encloses(i.instrs, 0, spans[0].header, spans[1].header),
			"a loop that merely precedes another must not enclose it")
		require.False(t, i.tracer.encloses(i.instrs, 0, spans[1].header, spans[0].header),
			"nor the other way round")
	})
}

// TestFrontend_Trace drives the SSA trace frontend against recordings the real
// recorder produced, which no test outside interp can obtain: capture clones a
// running interpreter and single-steps its threaded closures, and the snapshot
// a plan is built from is assembled from interpreter-private state. It asserts
// the same three claims frontend.TestTrace makes over hand-built trees - the
// frontend plans no root jit.TracePlan refuses, every function it emits
// verifies, and the two block graphs agree - over trees nobody wrote down.
func TestFrontend_Trace(t *testing.T) {
	build := func(emit func(b *instr.Builder)) []instr.Instruction {
		b := instr.NewBuilder()
		emit(b)
		instrs, err := b.Assemble()
		require.NoError(t, err)
		return instrs
	}
	counter := func(body func(b *instr.Builder)) []instr.Instruction {
		return build(func(b *instr.Builder) {
			head, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.GLOBAL_SET, 0)
			b.Bind(head)
			b.Emit(instr.GLOBAL_GET, 0).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
			body(b)
			b.Emit(instr.GLOBAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.GLOBAL_SET, 0)
			b.Br(head)
			b.Bind(done)
		})
	}
	callee := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI32},
	}).Emit(
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_ADD),
		instr.New(instr.RETURN),
	).MustBuild()

	for _, tc := range []struct {
		name string
		prog *program.Program
	}{
		{
			name: "counting loop",
			prog: program.New(counter(func(b *instr.Builder) {}), program.WithGlobals(types.TypeI32)),
		},
		{
			name: "loop calling a function",
			prog: program.New(counter(func(b *instr.Builder) {
				b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
			}), program.WithGlobals(types.TypeI32), program.WithConstants(callee)),
		},
		{
			name: "loop reading an array",
			prog: program.New(counter(func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_GET).Emit(instr.DROP)
			}), program.WithGlobals(types.TypeI32), program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
		},
		{
			name: "straight-line array read",
			prog: program.New(build(func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.DROP)
			}), program.WithConstants(types.TypedArray[int32]{1, 2, 3})),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracer := newTracer()
			i := New(tc.prog, withTracer(tracer), WithThreshold(-1))
			defer i.Close()

			tracer.capture(i, jit.Anchor{})
			for _, span := range tracer.headers(i.instrs, 0) {
				tracer.capture(i, jit.Anchor{IP: span.header})
			}
			input, ok := i.compileSnapshot(0)
			require.True(t, ok)

			plans, err := jit.TracePlan(input)
			require.NoError(t, err)
			roots := map[jit.Anchor]jit.Plan{}
			for _, plan := range plans {
				roots[plan.Anchor] = plan
			}
			anchors := tracer.Anchors(0)
			require.NotEmpty(t, anchors)

			emitted := 0
			for _, ip := range anchors {
				anchor := jit.Anchor{IP: ip}
				fn := frontend.Trace(input, anchor)
				if fn == nil {
					continue
				}
				emitted++
				plan, planned := roots[anchor]
				require.True(t, planned, "anchor %+v is not a root the plan takes", anchor)
				require.NoError(t, ssa.Verify(fn), "anchor %+v\n%s", anchor, ssa.Format(fn))
				require.Equal(t, blocks(plan), succs(fn), "anchor %+v\n%s", anchor, ssa.Format(fn))
			}
			require.NotZero(t, emitted, "no root planned from %d recordings", len(anchors))
			require.Equal(t, len(roots), emitted, "every root the plan takes must still be planned")
		})
	}
}

// blocks is a trace plan's block graph in breadth-first order from its root,
// with a node of its own for every edge that leaves the plan - which is the
// block the SSA lays out for exactly that exit.
func blocks(plan jit.Plan) [][]int {
	ids := map[int]int{plan.Root: 0}
	queue := []int{plan.Root}
	var out [][]int
	for n := 0; n < len(queue); n++ {
		var edges []int
		if queue[n] != jit.NoBlock {
			for _, edge := range plan.Blocks[queue[n]].Term.Edges {
				node, ok := ids[edge.Index]
				if edge.Index == jit.NoBlock || !ok {
					node = len(queue)
					queue = append(queue, edge.Index)
					if edge.Index != jit.NoBlock {
						ids[edge.Index] = node
					}
				}
				edges = append(edges, node)
			}
		}
		out = append(out, edges)
	}
	return out
}

// succs is an SSA function's block graph in the same breadth-first numbering.
func succs(fn *ssa.Function) [][]int {
	ids := map[int]int{0: 0}
	queue := []int{0}
	var out [][]int
	for n := 0; n < len(queue); n++ {
		var edges []int
		for _, succ := range fn.Succ(queue[n]) {
			node, ok := ids[succ]
			if !ok {
				node = len(queue)
				ids[succ] = node
				queue = append(queue, succ)
			}
			edges = append(edges, node)
		}
		out = append(out, edges)
	}
	return out
}
