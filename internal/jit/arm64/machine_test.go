package arm64_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	machine := arm64.New()
	require.NotNil(t, machine)

	t.Run("lowers a straight-line plan", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			}),
		}
		input := &jit.Input{Address: 1, Function: fn}
		plans, err := jit.StaticPlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)

		assembler := asm.New(asmarm64.New())
		_, ok := machine.Lower(assembler, input, plans[0], false)
		require.True(t, ok)

		code, err := assembler.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

	t.Run("rejects an empty plan without emitting", func(t *testing.T) {
		fn := &types.Function{Typ: &types.FunctionType{}}
		assembler := asm.New(asmarm64.New())
		_, ok := machine.Lower(assembler, &jit.Input{Address: 1, Function: fn}, jit.Plan{}, false)
		require.False(t, ok, "an unlowerable plan must report failure rather than emit")
	})

	// The golden streams below state the ARM64 a shape should compile to
	// before the machine is asked to compile it. Every one opens with the
	// same prologue - mirror the journal header into the pinned registers,
	// then derive the frame base local slots are addressed from - because
	// every entry this machine takes reads its state from there.
	for _, tt := range []struct {
		name string
		addr int
		fn   *types.Function
		want []asm.Instruction
	}{
		{
			// An i32 runs on the whole register: only the low 32 bits carry
			// the value, so nothing masks between operations and boxing at
			// the frame boundary masks once.
			name: "lowers an integer arithmetic sequence",
			addr: 1,
			fn: &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).
						Emit(instr.I32_CONST, 3).Emit(instr.I32_MUL).Emit(instr.RETURN)
				}),
			},
			want: append(prologue(5, 6, 7), []asm.Instruction{
				asmarm64.LDR(vreg(0), vreg(5), 0),
				asmarm64.LDR(vreg(1), vreg(5), 8),
				asmarm64.ADD(vreg(2), vreg(0), vreg(1)),
				asmarm64.MOVZ(vreg(3), 3, 0),
				asmarm64.MUL(vreg(4), vreg(2), vreg(3)),
				asmarm64.ANDI(vreg(8), vreg(4), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(8), tag(types.KindI32), 48),
				asmarm64.STR(vreg(8), vreg(5), 0),
				asmarm64.MOV(vreg(9), vreg(8)),
				asmarm64.RET(),
			}...),
		},
		{
			// A constant materializes its unboxed form, which boxing then
			// masks and tags. The unboxed move is dead whenever every use is
			// a boxing one, as it is here: removing it needs a sweep over the
			// emitted stream this machine does not run yet.
			name: "materializes a constant",
			addr: 1,
			fn: &types.Function{
				Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN) }),
			},
			want: append(prologue(1, 2, 3), []asm.Instruction{
				asmarm64.MOVZ(vreg(0), 1, 0),
				asmarm64.ANDI(vreg(4), vreg(0), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(4), tag(types.KindI32), 48),
				asmarm64.STR(vreg(4), vreg(1), 0),
				asmarm64.MOV(vreg(5), vreg(4)),
				asmarm64.RET(),
			}...),
		},
		{
			// A function entry clears the locals its callers left, then the
			// round trip is one load and one store per slot. The word a load
			// returns is already boxed, so the mask and tag before the store
			// are redundant here; eliding them means keeping the boxed form
			// as a value's representation, which is the next stage's choice.
			name: "round-trips a local through its slot",
			addr: 1,
			fn: &types.Function{
				Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Locals: []types.Type{types.TypeI32},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_SET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
				}),
			},
			want: append(append(prologue(2, 3, 4),
				asmarm64.MOVZ(vreg(5), tag(types.KindI32), 48),
				asmarm64.STR(vreg(5), vreg(2), 8),
			), []asm.Instruction{
				asmarm64.LDR(vreg(0), vreg(2), 0),
				asmarm64.ANDI(vreg(6), vreg(0), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(6), tag(types.KindI32), 48),
				asmarm64.STR(vreg(6), vreg(2), 8),
				asmarm64.LDR(vreg(1), vreg(2), 8),
				asmarm64.ANDI(vreg(7), vreg(1), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(7), tag(types.KindI32), 48),
				asmarm64.STR(vreg(7), vreg(2), 0),
				asmarm64.MOV(vreg(8), vreg(7)),
				asmarm64.RET(),
			}...),
		},
		{
			// The layout lays a terminator's first edge out next, so a taken
			// branch costs one inverted test over the untaken block and falls
			// into the taken one.
			name: "lowers a conditional branch",
			addr: 1,
			fn: &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					other := b.Label()
					b.Emit(instr.LOCAL_GET, 0).BrIf(other)
					b.Emit(instr.I32_CONST, 7).Emit(instr.RETURN)
					b.Bind(other).Emit(instr.I32_CONST, 9).Emit(instr.RETURN)
				}),
			},
			want: append(prologue(3, 4, 5), []asm.Instruction{
				asmarm64.LDR(vreg(0), vreg(3), 0),
				asmarm64.CBZLabel(narrow(0), 2),
				asmarm64.MOVZ(vreg(1), 9, 0),
				asmarm64.ANDI(vreg(6), vreg(1), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(6), tag(types.KindI32), 48),
				asmarm64.STR(vreg(6), vreg(3), 0),
				asmarm64.MOV(vreg(7), vreg(6)),
				asmarm64.RET(),
				asmarm64.MOVZ(vreg(2), 7, 0),
				asmarm64.ANDI(vreg(8), vreg(2), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(8), tag(types.KindI32), 48),
				asmarm64.STR(vreg(8), vreg(3), 0),
				asmarm64.MOV(vreg(9), vreg(8)),
				asmarm64.RET(),
			}...),
		},
		{
			// Each result is boxed to its own frame slot for the wrapper that
			// tears the frame down, and moved into the ABI register a native
			// caller reads it back from.
			name: "returns through the frame base and the ABI registers",
			addr: 1,
			fn: &types.Function{
				Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32, types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, 2).Emit(instr.RETURN)
				}),
			},
			want: append(prologue(2, 3, 4), []asm.Instruction{
				asmarm64.MOVZ(vreg(0), 1, 0),
				asmarm64.MOVZ(vreg(1), 2, 0),
				asmarm64.ANDI(vreg(5), vreg(0), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(5), tag(types.KindI32), 48),
				asmarm64.STR(vreg(5), vreg(2), 0),
				asmarm64.MOV(vreg(6), vreg(5)),
				asmarm64.ANDI(vreg(7), vreg(1), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(7), tag(types.KindI32), 48),
				asmarm64.STR(vreg(7), vreg(2), 8),
				asmarm64.MOV(vreg(8), vreg(7)),
				asmarm64.RET(),
			}...),
		},
		{
			// A float lives in the float bank between operations, so one
			// arithmetic sequence costs one move in per operand and one out
			// at the frame boundary rather than a pair around every opcode.
			name: "keeps a float in the float bank",
			addr: 1,
			fn: &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeF64, types.TypeF64}, Returns: []types.Type{types.TypeF64}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.F64_SUB).Emit(instr.RETURN)
				}),
			},
			want: append(prologue(3, 4, 5), []asm.Instruction{
				asmarm64.LDR(vreg(6), vreg(3), 0),
				asmarm64.FMOV(freg(0), vreg(6)),
				asmarm64.LDR(vreg(7), vreg(3), 8),
				asmarm64.FMOV(freg(1), vreg(7)),
				asmarm64.FSUB(freg(2), freg(0), freg(1)),
				asmarm64.FMOV(vreg(8), freg(2)),
				asmarm64.STR(vreg(8), vreg(3), 0),
				asmarm64.MOV(vreg(9), vreg(8)),
				asmarm64.RET(),
			}...),
		},
		{
			// Module code has no return: it leaves its operand stack on the
			// VM stack, publishes the stack pointer, and reports a clean trap
			// with the offset past its last instruction.
			name: "completes module code through the journal",
			addr: 0,
			fn: &types.Function{
				Typ:    &types.FunctionType{},
				Locals: []types.Type{types.TypeI32},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.I32_CONST, 5).Emit(instr.LOCAL_SET, 0)
				}),
			},
			want: append(prologue(1, 2, 3), []asm.Instruction{
				asmarm64.MOVZ(vreg(0), 5, 0),
				asmarm64.ANDI(vreg(4), vreg(0), 0xFFFFFFFF),
				asmarm64.MOVK(vreg(4), tag(types.KindI32), 48),
				asmarm64.STR(vreg(4), vreg(1), 0),
				asmarm64.ADDI(vreg(6), vreg(9), 1),
				asmarm64.STR(vreg(6), vreg(5), int16(journal.CellSP*8)),
				asmarm64.MOVZ(vreg(7), 0, 0),
				asmarm64.STR(vreg(7), vreg(5), int16(journal.CellTrap*8)),
				asmarm64.MOVZ(vreg(8), 7, 0),
				asmarm64.STR(vreg(8), vreg(5), int16(journal.CellNextIP*8)),
				asmarm64.RET(),
			}...),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assembler := asm.New(asmarm64.New())

			entry, ok := arm64.New().Compile(assembler, input(tt.addr, tt.fn), jit.Anchor{Addr: tt.addr})
			require.True(t, ok)
			require.Equal(t, jit.Anchor{Addr: tt.addr}.Kind(), entry.Kind)
			require.Equal(t, prof.FrontendStatic, entry.Frontend)
			require.Equal(t, tt.want, assembler.Instructions())

			code, err := assembler.Build()
			require.NoError(t, err)
			require.NotEmpty(t, code, "the golden stream must encode")
		})
	}

	// Declining is the correct answer for everything the SSA machine has not
	// learned yet, and it must cost no coverage: the same root still compiles
	// through the plan pipeline this machine is being ported off.
	callee := &types.Function{
		Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN) }),
	}
	for _, tt := range []struct {
		name  string
		input *jit.Input
	}{
		{
			name: "an opcode it does not lower",
			input: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
				}),
			}),
		},
		{
			name: "a loop, whose back edge it does not close",
			input: input(1, &types.Function{
				Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Locals: []types.Type{types.TypeI32},
				Code: assemble(t, func(b *instr.Builder) {
					head, done := b.Label(), b.Label()
					b.Bind(head)
					b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
					b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
					b.Br(head)
					b.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
				}),
			}),
		},
		{
			name: "an i64 local, whose load needs a guard",
			input: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
				}),
			}),
		},
		{
			name: "a frame holding a reference",
			input: input(1, &types.Function{
				Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
				Locals: []types.Type{types.NewArrayType(types.TypeI32)},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
				}),
			}),
		},
		{
			name: "a heap access, whose guard and cold stub it does not emit",
			input: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
				}),
			}),
		},
		{
			name: "a call, whose frame it does not open",
			input: func() *jit.Input {
				in := input(1, &types.Function{
					Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
					Code: assemble(t, func(b *instr.Builder) {
						b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
					}),
				})
				in.Constants = []types.Boxed{types.BoxRef(2)}
				in.Objects[2] = jit.Object{Fn: callee}
				return in
			}(),
		},
	} {
		t.Run("declines "+tt.name, func(t *testing.T) {
			declined := asm.New(asmarm64.New())
			_, ok := arm64.New().Compile(declined, tt.input, jit.Anchor{Addr: 1})
			require.False(t, ok)

			plans, err := jit.StaticPlan(tt.input)
			require.NoError(t, err)
			require.NotEmpty(t, plans)
			planned := asm.New(asmarm64.New())
			_, ok = arm64.New().Lower(planned, tt.input, plans[0], plans[0].Kind == jit.EntryLoop)
			require.True(t, ok, "a declined root must still compile through the plan pipeline")
			code, err := planned.Build()
			require.NoError(t, err)
			require.NotEmpty(t, code)
		})
	}
}

// input is the compile-time snapshot one function is compiled from, published
// at the address its entry is anchored at.
func input(addr int, fn *types.Function) *jit.Input {
	return &jit.Input{Address: addr, Function: fn, Objects: jit.Objects{addr: {Fn: fn}}}
}

// assemble builds the code of one function.
func assemble(t *testing.T, emit func(b *instr.Builder)) []byte {
	t.Helper()
	b := instr.NewBuilder()
	emit(b)
	instructions, err := b.Assemble()
	require.NoError(t, err)
	return instr.Marshal(instructions)
}

// prologue is the entry every golden stream opens with: the journal header
// mirrored into the pinned context registers, then the frame base derived
// from the stack pointer and the frame's own base.
func prologue(base, bp, stack int32) []asm.Instruction {
	return []asm.Instruction{
		asmarm64.MOV(asmarm64.X14, asmarm64.X0),
		asmarm64.LDP(asmarm64.X10, asmarm64.X11, asmarm64.X14, int16(journal.CellStack*8)),
		asmarm64.LDR(asmarm64.X12, asmarm64.X14, int16(journal.CellBP*8)),
		asmarm64.LSLI(vreg(base), vreg(bp), 3),
		asmarm64.ADD(vreg(base), vreg(stack), vreg(base)),
	}
}

// vreg, freg, and narrow name one virtual register of the stream a compile
// emits: the integer bank, the float bank, and the 32-bit view of an integer
// register a value-lane test reads.
func vreg(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width64) }

func freg(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeFloat, asm.Width64) }

func narrow(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width32) }

// tag is the boxed word's kind tag as the 16-bit field MOVK writes at 48.
func tag(kind types.Kind) uint16 { return uint16(types.Tag(kind) >> 48) }
