package jit_test

import (
	"sync"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

// noFrameArch reports no spill frame, so register exhaustion fails the build
// instead of spilling. Arch is an exported extension point, and this is the
// shape internal/asm's own tests use to drive that path.
type noFrameArch struct{ asm.Arch }

func (noFrameArch) Frame() asm.Frame { return nil }

// attempt records one call into the Machine: how many carried locals the plan
// still pinned, and whether the back-edge was still to stay in native code.
type attempt struct {
	carried    int
	nativeLoop bool
}

// pressureMachine emits more simultaneously live values than any register bank
// holds until it has been asked relent times, then emits a trivial body. It
// stands in for a backend whose lowering only fits once the compiler has
// relaxed what the plan pins.
type pressureMachine struct {
	relent   int
	attempts *[]attempt
}

func (m pressureMachine) Lower(a *asm.Assembler, _ *jit.Input, p jit.Plan, nativeLoop bool) ([]jit.Exit, bool) {
	*m.attempts = append(*m.attempts, attempt{carried: len(p.Carried), nativeLoop: nativeLoop})

	live := 1
	if len(*m.attempts) <= m.relent {
		live = 64
	}
	regs := make([]asm.VReg, live)
	for n := range regs {
		regs[n] = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(asmarm64.LDI(regs[n], uint64(n+1))...)
	}
	sum := regs[0]
	for _, r := range regs[1:] {
		a.Emit(asmarm64.ADD(sum, sum, r))
	}
	a.Emit(asmarm64.RET())
	return nil, true
}

func newTestCompiler(t *testing.T, machine jit.Machine) *jit.Compiler {
	t.Helper()
	c, err := jit.New(noFrameArch{asmarm64.New()}, machine)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

// loopInput builds a counting loop, whose header plan pins one carried local -
// the rung the compiler drops first under register pressure.
func loopInput(t *testing.T) (*jit.Input, jit.Plan, jit.Plan) {
	t.Helper()
	b := instr.NewBuilder()
	loop := b.Label()
	done := b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0).
		Bind(loop).
		Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 64).Emit(instr.I32_GE_S).BrIf(done).
		Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).
		Br(loop).
		Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	instructions, err := b.Assemble()
	require.NoError(t, err)

	input := &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(instructions),
		},
	}
	plans, err := jit.StaticPlan(input)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	entry := plans[0]
	header := plans[1]
	require.Equal(t, jit.EntryFunction, entry.Kind)
	require.Equal(t, jit.EntryLoop, header.Kind)
	require.NotEmpty(t, header.Carried, "the fixture must pin a carried local for the ladder to have a rung to drop")
	return input, entry, header
}

// newNativeCompiler builds a compiler over the real ARM64 arch and backend,
// so a Compile through it exercises every lowering that reads an Input.
func newNativeCompiler(t *testing.T) *jit.Compiler {
	t.Helper()
	c, err := jit.New(asmarm64.New(), arm64.New())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

func TestNew(t *testing.T) {
	var attempts []attempt
	c := newTestCompiler(t, pressureMachine{attempts: &attempts})
	require.NotNil(t, c)
	require.NotNil(t, c.Buffer(), "a compiler owns the executable buffer its code is published into")
}

func TestCompiler_Compile(t *testing.T) {
	input, entry, header := loopInput(t)

	t.Run("keeps a static entry's loop native", func(t *testing.T) {
		var attempts []attempt
		c := newTestCompiler(t, pressureMachine{attempts: &attempts})

		c.Compile(input, entry.Anchor)
		require.Equal(t, []attempt{{carried: len(entry.Carried), nativeLoop: true}}, attempts)
	})

	for _, tt := range []struct {
		name   string
		relent int
		want   []attempt
	}{
		{
			name:   "lowers once when the full plan fits",
			relent: 0,
			want:   []attempt{{carried: len(header.Carried), nativeLoop: true}},
		},
		{
			name:   "drops carried locals when the full plan does not fit",
			relent: 1,
			want: []attempt{
				{carried: len(header.Carried), nativeLoop: true},
				{carried: 0, nativeLoop: true},
			},
		},
		{
			name:   "gives up the native back-edge when dropping carried locals is not enough",
			relent: 2,
			want: []attempt{
				{carried: len(header.Carried), nativeLoop: true},
				{carried: 0, nativeLoop: true},
				{carried: 0, nativeLoop: false},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var attempts []attempt
			c := newTestCompiler(t, pressureMachine{relent: tt.relent, attempts: &attempts})

			c.Compile(input, header.Anchor)
			require.Equal(t, tt.want, attempts)
		})
	}
}

// TestCompiler_CompileConcurrentHeap pins the property that makes moving
// compilation off the interpreter's goroutine possible: an Input carries
// facts resolved out of the heap, not the heap itself, so a Compile neither
// observes nor races the mutation execution keeps performing on it. The
// mutator reproduces both hazards the live view had - a released slot handed
// to the next allocation, and a slice that grows out from under a reader -
// against the very cells this snapshot was resolved from.
func TestCompiler_CompileConcurrentHeap(t *testing.T) {
	callee := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}, Code: instr.Marshal([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.RETURN),
	})}
	caller := &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.ARRAY_GET),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		}),
	}

	array := types.TypedArray[int32]{7}
	heap := []types.Value{types.Null, caller, callee, array}
	constants := []types.Boxed{types.BoxRef(2), types.BoxRef(3)}

	// Resolving the cells a compile may name is the interpreter's step, taken
	// on its own goroutine before any compile starts; this loop is that step
	// written out.
	objects := jit.Objects{}
	for _, val := range constants {
		switch cell := heap[val.Ref()].(type) {
		case *types.Function:
			objects[val.Ref()] = jit.Object{Fn: cell}
		case types.TypedArray[int32]:
			objects[val.Ref()] = jit.Object{Array: jit.Itab(cell)}
		}
	}
	input := &jit.Input{Address: 1, Function: caller, Constants: constants, Objects: objects}

	want := newNativeCompiler(t).Compile(input, jit.Anchor{Addr: 1})
	require.Equal(t, prof.CompileOutcomeEmitted, want.Outcome, "the fixture must reach the backend for the concurrent runs to prove anything")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for n := 0; n < 4096; n++ {
			array[0] = int32(n)
			heap[2], heap[3] = types.Null, types.Null
			heap = append(heap, types.Null)
			heap[2], heap[3] = callee, array
		}
	}()

	results := make([]jit.Result, 4)
	var wg sync.WaitGroup
	for idx := range results {
		compiler := newNativeCompiler(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[idx] = compiler.Compile(input, jit.Anchor{Addr: 1})
		}()
	}
	wg.Wait()
	<-done

	for _, got := range results {
		require.Equal(t, want.Outcome, got.Outcome)
		require.Equal(t, want.Frontend, got.Frontend)
		require.Len(t, got.Code.Entries, len(want.Code.Entries))
	}
}
