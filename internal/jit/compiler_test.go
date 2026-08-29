package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
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
func loopInput(t *testing.T) (*jit.Input, jit.Plan) {
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
	header := plans[1]
	require.Equal(t, jit.EntryLoop, header.Kind)
	require.NotEmpty(t, header.Carried, "the fixture must pin a carried local for the ladder to have a rung to drop")
	return input, header
}

func TestNew(t *testing.T) {
	var attempts []attempt
	c := newTestCompiler(t, pressureMachine{attempts: &attempts})
	require.NotNil(t, c)
	require.NotNil(t, c.Buffer(), "a compiler owns the executable buffer its code is published into")
}

func TestCompiler_Compile(t *testing.T) {
	input, header := loopInput(t)

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
