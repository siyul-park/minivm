package arm64_test

import (
	"reflect"
	"slices"
	"testing"
	"unsafe"

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
	elems := types.TypedArray[int32]{10, 20, 30}
	fieldsTyp := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeF64))
	for _, tt := range []struct {
		name string
		addr int
		in   *jit.Input
		want []asm.Instruction
	}{
		{
			// An i32 runs on the whole register: only the low 32 bits carry
			// the value, so nothing masks between operations and boxing at
			// the frame boundary masks once.
			name: "lowers an integer arithmetic sequence",
			addr: 1,
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).
						Emit(instr.I32_CONST, 3).Emit(instr.I32_MUL).Emit(instr.RETURN)
				}),
			}),
			want: append(prologue(5, 6, 7), []asm.Instruction{
				asmarm64.LDR(narrow(0), vreg(5), 0),
				asmarm64.LDR(narrow(1), vreg(5), 8),
				asmarm64.ADD(narrow(2), narrow(0), narrow(1)),
				asmarm64.MOVZ(narrow(3), 3, 0),
				asmarm64.MUL(narrow(4), narrow(2), narrow(3)),
				asmarm64.MOV(vreg(8), vreg(4)),
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
			in: input(1, &types.Function{
				Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN) }),
			}),
			want: append(prologue(1, 2, 3), []asm.Instruction{
				asmarm64.MOVZ(narrow(0), 1, 0),
				asmarm64.MOV(vreg(4), vreg(0)),
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
			in: input(1, &types.Function{
				Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Locals: []types.Type{types.TypeI32},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_SET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
				}),
			}),
			want: append(append(prologue(2, 3, 4),
				asmarm64.MOVZ(vreg(5), tag(types.KindI32), 48),
				asmarm64.STR(vreg(5), vreg(2), 8),
			), []asm.Instruction{
				asmarm64.LDR(narrow(0), vreg(2), 0),
				asmarm64.MOV(vreg(6), vreg(0)),
				asmarm64.MOVK(vreg(6), tag(types.KindI32), 48),
				asmarm64.STR(vreg(6), vreg(2), 8),
				asmarm64.LDR(narrow(1), vreg(2), 8),
				asmarm64.MOV(vreg(7), vreg(1)),
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
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					other := b.Label()
					b.Emit(instr.LOCAL_GET, 0).BrIf(other)
					b.Emit(instr.I32_CONST, 7).Emit(instr.RETURN)
					b.Bind(other).Emit(instr.I32_CONST, 9).Emit(instr.RETURN)
				}),
			}),
			want: append(prologue(3, 4, 5), []asm.Instruction{
				asmarm64.LDR(narrow(0), vreg(3), 0),
				asmarm64.CBZLabel(narrow(0), 2),
				asmarm64.MOVZ(narrow(1), 9, 0),
				asmarm64.MOV(vreg(6), vreg(1)),
				asmarm64.MOVK(vreg(6), tag(types.KindI32), 48),
				asmarm64.STR(vreg(6), vreg(3), 0),
				asmarm64.MOV(vreg(7), vreg(6)),
				asmarm64.RET(),
				asmarm64.MOVZ(narrow(2), 7, 0),
				asmarm64.MOV(vreg(8), vreg(2)),
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
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32, types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, 2).Emit(instr.RETURN)
				}),
			}),
			want: append(prologue(2, 3, 4), []asm.Instruction{
				asmarm64.MOVZ(narrow(0), 1, 0),
				asmarm64.MOVZ(narrow(1), 2, 0),
				asmarm64.MOV(vreg(5), vreg(0)),
				asmarm64.MOVK(vreg(5), tag(types.KindI32), 48),
				asmarm64.STR(vreg(5), vreg(2), 0),
				asmarm64.MOV(vreg(6), vreg(5)),
				asmarm64.MOV(vreg(7), vreg(1)),
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
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeF64, types.TypeF64}, Returns: []types.Type{types.TypeF64}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.F64_SUB).Emit(instr.RETURN)
				}),
			}),
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
			// An f32 rejoins the boxed word through the same two-instruction
			// shape as an i32: box's FMOV already zero-extends the low 32
			// bits into the fresh 64-bit register, so MOVK is the only
			// instruction left to tag it - no mask, and no third
			// instruction moving the bits into a temporary first.
			name: "boxes an f32 result at the return boundary",
			addr: 1,
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeF32, types.TypeF32}, Returns: []types.Type{types.TypeF32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.F32_ADD).Emit(instr.RETURN)
				}),
			}),
			want: append(prologue(3, 4, 5), []asm.Instruction{
				asmarm64.LDR(vreg(6), vreg(3), 0),
				asmarm64.FMOV(freg32(0), narrow(6)),
				asmarm64.LDR(vreg(7), vreg(3), 8),
				asmarm64.FMOV(freg32(1), narrow(7)),
				asmarm64.FADD(freg32(2), freg32(0), freg32(1)),
				asmarm64.FMOV(vreg(8), freg32(2)),
				asmarm64.MOVK(vreg(8), tag(types.KindF32), 48),
				asmarm64.STR(vreg(8), vreg(3), 0),
				asmarm64.MOV(vreg(9), vreg(8)),
				asmarm64.RET(),
			}...),
		},
		{
			// A shift's value and amount already sit raw in the W lane, so
			// neither needs a prep instruction: the amount is masked to five
			// bits and the shift runs directly on the two operands. I32_SHL
			// and I32_SHR_U share this shape; I32_SHR_S below shares it too,
			// because ASR reads bit 31 as the sign bit within a clean
			// 32-bit register the same way LSL and LSR read it as data -
			// unlike the 64-bit-sign-extend prep this machine used to need.
			name: "shifts an i32 left with no operand prep",
			addr: 1,
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_SHL).Emit(instr.RETURN)
				}),
			}),
			want: append(prologue(3, 4, 5), []asm.Instruction{
				asmarm64.LDR(narrow(0), vreg(3), 0),
				asmarm64.LDR(narrow(1), vreg(3), 8),
				asmarm64.ANDI(narrow(6), narrow(1), 0x1F),
				asmarm64.LSL(narrow(2), narrow(0), narrow(6)),
				asmarm64.MOV(vreg(7), vreg(2)),
				asmarm64.MOVK(vreg(7), tag(types.KindI32), 48),
				asmarm64.STR(vreg(7), vreg(3), 0),
				asmarm64.MOV(vreg(8), vreg(7)),
				asmarm64.RET(),
			}...),
		},
		{
			// ASR on a clean W-lane value reads bit 31 as the sign bit
			// directly - no SXTW widening to a 64-bit register first, which
			// is what this machine emitted before the operands were raw.
			name: "shifts an i32 right arithmetically with no sign-extend prep",
			addr: 1,
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_SHR_S).Emit(instr.RETURN)
				}),
			}),
			want: append(prologue(3, 4, 5), []asm.Instruction{
				asmarm64.LDR(narrow(0), vreg(3), 0),
				asmarm64.LDR(narrow(1), vreg(3), 8),
				asmarm64.ANDI(narrow(6), narrow(1), 0x1F),
				asmarm64.ASR(narrow(2), narrow(0), narrow(6)),
				asmarm64.MOV(vreg(7), vreg(2)),
				asmarm64.MOVK(vreg(7), tag(types.KindI32), 48),
				asmarm64.STR(vreg(7), vreg(3), 0),
				asmarm64.MOV(vreg(8), vreg(7)),
				asmarm64.RET(),
			}...),
		},
		{
			name: "shifts an i32 right logically with no operand prep",
			addr: 1,
			in: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_SHR_U).Emit(instr.RETURN)
				}),
			}),
			want: append(prologue(3, 4, 5), []asm.Instruction{
				asmarm64.LDR(narrow(0), vreg(3), 0),
				asmarm64.LDR(narrow(1), vreg(3), 8),
				asmarm64.ANDI(narrow(6), narrow(1), 0x1F),
				asmarm64.LSR(narrow(2), narrow(0), narrow(6)),
				asmarm64.MOV(vreg(7), vreg(2)),
				asmarm64.MOVK(vreg(7), tag(types.KindI32), 48),
				asmarm64.STR(vreg(7), vreg(3), 0),
				asmarm64.MOV(vreg(8), vreg(7)),
				asmarm64.RET(),
			}...),
		},
		{
			// Module code has no return: it leaves its operand stack on the
			// VM stack, publishes the stack pointer, and reports a clean trap
			// with the offset past its last instruction.
			name: "completes module code through the journal",
			addr: 0,
			in: input(0, &types.Function{
				Typ:    &types.FunctionType{},
				Locals: []types.Type{types.TypeI32},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.I32_CONST, 5).Emit(instr.LOCAL_SET, 0)
				}),
			}),
			want: append(prologue(1, 2, 3), []asm.Instruction{
				asmarm64.MOVZ(narrow(0), 5, 0),
				asmarm64.MOV(vreg(4), vreg(0)),
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
		{
			// A read speculates: the guard admits only the concrete array
			// type the access was compiled against, and it runs first, so a
			// container of any other type leaves before the load. The read
			// behind it is one unsigned index test - a sign-extended negative
			// index is above any length - and one load of the element's own
			// width, out of the heap cell the guard already walked to.
			name: "reads an array element through the shape that admitted it",
			addr: 1,
			in: func() *jit.Input {
				in := input(1, &types.Function{
					Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
					Code: assemble(t, func(b *instr.Builder) {
						b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
					}),
				})
				in.Constants = []types.Boxed{types.BoxRef(2)}
				in.Objects[2] = jit.Object{Array: jit.Itab(elems)}
				return in
			}(),
			want: slices.Concat(
				prologue(4, 5, 6),
				asmarm64.LDI(vreg(0), uint64(types.BoxRef(2))),
				[]asm.Instruction{
					asmarm64.LDR(narrow(1), vreg(4), 0),
					asmarm64.LSRI(vreg(7), vreg(0), uint8(types.VBits)),
				},
				asmarm64.LDI(vreg(8), uint64(types.Tag(types.KindRef))>>types.VBits),
				[]asm.Instruction{
					asmarm64.CMP(vreg(7), vreg(8)),
					asmarm64.BCondLabel(asmarm64.OpBNE, shapeExit),
					asmarm64.ANDI(vreg(9), vreg(0), 0xFFFFFFFF),
					asmarm64.LDR(vreg(10), vreg(15), int16(journal.CellHeap*8)),
					asmarm64.LSLI(vreg(11), vreg(9), 4),
					asmarm64.ADD(vreg(12), vreg(10), vreg(11)),
					asmarm64.LDR(vreg(13), vreg(12), 0),
					asmarm64.LDR(vreg(14), vreg(12), 8),
				},
				asmarm64.LDI(vreg(16), uint64(jit.Itab(elems))),
				[]asm.Instruction{
					asmarm64.CMP(vreg(13), vreg(16)),
					asmarm64.BCondLabel(asmarm64.OpBNE, shapeExit),
					asmarm64.MOV(vreg(2), vreg(0)),

					asmarm64.LDR(vreg(17), vreg(14), 0),
					asmarm64.LDR(vreg(18), vreg(14), 8),
					asmarm64.SXTW(vreg(19), narrow(1)),
					asmarm64.CMP(vreg(19), vreg(18)),
					asmarm64.BCondLabel(asmarm64.OpBCS, boundsExit),
					asmarm64.LSLI(vreg(21), vreg(19), 2),
					asmarm64.ADD(vreg(20), vreg(17), vreg(21)),
					asmarm64.LDR(narrow(3), vreg(20), 0),

					asmarm64.MOV(vreg(22), vreg(3)),
					asmarm64.MOVK(vreg(22), tag(types.KindI32), 48),
					asmarm64.STR(vreg(22), vreg(4), 0),
					asmarm64.MOV(vreg(23), vreg(22)),
					asmarm64.RET(),
				},
				stub(24, 1, 3, arrayGetIP, 8, 3),
				stub(38, 2, 4, arrayGetIP, 8, 3),
			),
		},
		{
			name: "reads a struct field through the struct type and the field kind that admitted it",
			addr: 1,
			in: func() *jit.Input {
				in := input(1, &types.Function{
					Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
					Code: assemble(t, func(b *instr.Builder) {
						b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.RETURN)
					}),
				})
				in.Constants = []types.Boxed{types.BoxRef(2)}
				in.Objects[2] = jit.Object{Typ: fieldsTyp}
				return in
			}(),
			want: func() []asm.Instruction {
				typ := fieldsTyp
				return slices.Concat(
					prologue(4, 5, 6),
					asmarm64.LDI(vreg(0), uint64(types.BoxRef(2))),
					[]asm.Instruction{asmarm64.MOVZ(narrow(1), 0, 0)},
					[]asm.Instruction{
						asmarm64.LSRI(vreg(7), vreg(0), uint8(types.VBits)),
					},
					asmarm64.LDI(vreg(8), uint64(types.Tag(types.KindRef))>>types.VBits),
					[]asm.Instruction{
						asmarm64.CMP(vreg(7), vreg(8)),
						asmarm64.BCondLabel(asmarm64.OpBNE, shapeExit),
						asmarm64.ANDI(vreg(9), vreg(0), 0xFFFFFFFF),
						asmarm64.LDR(vreg(10), vreg(15), int16(journal.CellHeap*8)),
						asmarm64.LSLI(vreg(11), vreg(9), 4),
						asmarm64.ADD(vreg(12), vreg(10), vreg(11)),
						asmarm64.LDR(vreg(13), vreg(12), 0),
						asmarm64.LDR(vreg(14), vreg(12), 8),
					},
					asmarm64.LDI(vreg(16), uint64(jit.HeapStruct)),
					[]asm.Instruction{
						asmarm64.CMP(vreg(13), vreg(16)),
						asmarm64.BCondLabel(asmarm64.OpBNE, shapeExit),
						asmarm64.MOV(vreg(2), vreg(0)),
						asmarm64.LDR(vreg(17), vreg(14), 0),
					},
					asmarm64.LDI(vreg(18), uint64(uintptr(unsafe.Pointer(typ)))),
					[]asm.Instruction{
						asmarm64.CMP(vreg(17), vreg(18)),
						asmarm64.BCondLabel(asmarm64.OpBNE, shapeExit),
						asmarm64.LDR(vreg(19), vreg(17), 0),
						asmarm64.LDR(vreg(20), vreg(17), 8),
						asmarm64.SXTW(vreg(21), narrow(1)),
						asmarm64.CMP(vreg(21), vreg(20)),
						asmarm64.BCondLabel(asmarm64.OpBCS, boundsExit),
						asmarm64.MOVZ(vreg(22), 40, 0),
						asmarm64.MUL(vreg(22), vreg(21), vreg(22)),
						asmarm64.ADD(vreg(23), vreg(19), vreg(22)),
						asmarm64.LDRB(vreg(24), vreg(23), 32),
						asmarm64.CMPI(vreg(24), uint16(types.KindI32)),
						asmarm64.BCondLabel(asmarm64.OpBNE, kindExit),
						asmarm64.LDR(vreg(25), vreg(14), 8),
						asmarm64.LDRR(narrow(3), vreg(25), vreg(21)),
						asmarm64.MOV(vreg(26), vreg(3)),
						asmarm64.MOVK(vreg(26), tag(types.KindI32), 48),
						asmarm64.STR(vreg(26), vreg(4), 0),
						asmarm64.MOV(vreg(27), vreg(26)),
						asmarm64.RET(),
					},
					stub(28, 1, 4, structGetIP, 0, 2),
					stub(42, 2, 5, structGetIP, 0, 2),
					stub(56, 3, 6, structGetIP, 0, 2),
				)
			}(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assembler := asm.New(asmarm64.New())

			entry, ok := arm64.New().Compile(assembler, tt.in, jit.Anchor{Addr: tt.addr})
			require.True(t, ok)
			require.Equal(t, jit.Anchor{Addr: tt.addr}.Kind(), entry.Kind)
			require.Equal(t, prof.FrontendStatic, entry.Frontend)
			require.Equal(t, tt.want, assembler.Instructions())

			code, err := assembler.Build()
			require.NoError(t, err)
			require.NotEmpty(t, code, "the golden stream must encode")
		})
	}

	// hostRead's Shape.Host names a Go kind a trace observes on a live
	// *HostStruct (interp/trace.go's tracer.field), never a fact the static
	// frontend resolves from bytecode alone (see frontend/walk.go's field),
	// so this golden row anchors on a hand-built jit.Trace instead of the
	// bare jit.Input every row above it uses.
	t.Run("reads a *HostStruct field through the shape and Go kind that admitted it", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.RETURN)
			}),
		}
		in := input(1, fn)
		in.Constants = []types.Boxed{types.BoxRef(2)}
		layout := jit.Layout{
			HostFields:      0,
			HostPtr:         8,
			HostFieldOffset: 0,
			HostFieldConv:   8,
			HostFieldSize:   16,
			HostConvKind:    0,
			HostStructItab:  0xABCDEF,
		}
		in.Layout = layout

		rec := &tape{}
		rec.at(fn, 1, 0, 0)
		rec.at(fn, 1, 3, 0)
		get := rec.at(fn, 1, structGetIP, 0)
		get.Arg = types.BoxI32(0)
		get.Shape = jit.Shape{Itab: layout.HostStructItab, Field: reflect.Int32}
		get.Seen = types.BoxI32(11)
		rec.at(fn, 1, structGetIP+1, 0)
		in.Traces = fakeTraces{{Addr: 1}: {Root: &jit.Trace{Anchor: jit.Anchor{Addr: 1}, Ops: rec.ops, Status: jit.StatusReturned}}}

		assembler := asm.New(asmarm64.New())
		entry, ok := arm64.New().Compile(assembler, in, jit.Anchor{Addr: 1})
		require.True(t, ok)
		require.Equal(t, prof.FrontendTrace, entry.Frontend)

		want := slices.Concat(
			prologue(4, 5, 6),
			asmarm64.LDI(vreg(0), uint64(types.BoxRef(2))),
			[]asm.Instruction{asmarm64.MOVZ(narrow(1), 0, 0)},
			[]asm.Instruction{asmarm64.LSRI(vreg(7), vreg(0), uint8(types.VBits))},
			asmarm64.LDI(vreg(8), uint64(types.Tag(types.KindRef))>>types.VBits),
			[]asm.Instruction{
				asmarm64.CMP(vreg(7), vreg(8)),
				asmarm64.BCondLabel(asmarm64.OpBNE, shapeExit),
				asmarm64.ANDI(vreg(9), vreg(0), 0xFFFFFFFF),
				asmarm64.LDR(vreg(10), vreg(15), int16(journal.CellHeap*8)),
				asmarm64.LSLI(vreg(11), vreg(9), 4),
				asmarm64.ADD(vreg(12), vreg(10), vreg(11)),
				asmarm64.LDR(vreg(13), vreg(12), 0),
				asmarm64.LDR(vreg(14), vreg(12), 8),
			},
			asmarm64.LDI(vreg(16), uint64(layout.HostStructItab)),
			[]asm.Instruction{
				asmarm64.CMP(vreg(13), vreg(16)),
				asmarm64.BCondLabel(asmarm64.OpBNE, shapeExit),
				asmarm64.MOV(vreg(2), vreg(0)),
				asmarm64.LDR(vreg(17), vreg(14), int16(layout.HostFields)),
				asmarm64.LDR(vreg(18), vreg(14), int16(layout.HostFields+8)),
				asmarm64.SXTW(vreg(19), narrow(1)),
				asmarm64.CMP(vreg(19), vreg(18)),
				asmarm64.BCondLabel(asmarm64.OpBCS, boundsExit),
				asmarm64.MOVZ(vreg(20), uint16(layout.HostFieldSize), 0),
				asmarm64.MUL(vreg(20), vreg(19), vreg(20)),
				asmarm64.ADD(vreg(21), vreg(17), vreg(20)),
				asmarm64.LDR(vreg(22), vreg(21), int16(layout.HostFieldConv)),
				asmarm64.LDRB(vreg(23), vreg(22), int16(layout.HostConvKind)),
				asmarm64.CMPI(vreg(23), uint16(reflect.Int32)),
				asmarm64.BCondLabel(asmarm64.OpBNE, kindExit),
				asmarm64.LDR(vreg(24), vreg(21), int16(layout.HostFieldOffset)),
				asmarm64.LDR(vreg(25), vreg(14), int16(layout.HostPtr)),
				asmarm64.ADD(vreg(26), vreg(25), vreg(24)),
				asmarm64.LDR(narrow(3), vreg(26), 0),
				asmarm64.MOV(vreg(27), vreg(3)),
				asmarm64.MOVK(vreg(27), tag(types.KindI32), 48),
				asmarm64.STR(vreg(27), vreg(4), 0),
				asmarm64.MOV(vreg(28), vreg(27)),
				asmarm64.RET(),
			},
			stub(29, 1, 4, structGetIP, 0, 2),
			stub(43, 2, 5, structGetIP, 0, 2),
			stub(57, 3, 6, structGetIP, 0, 2),
		)
		require.Equal(t, want, assembler.Instructions())

		code, err := assembler.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

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
			// The read itself lowers (see the golden stream above); what
			// declines is the frame, because a reference in a local carries a
			// count the teardown on a native return does not drop.
			name: "an array read off a reference parameter",
			input: input(1, &types.Function{
				Typ: &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
				}),
			}),
		},
		{
			// An i64 element may be heap-promoted, so reading one needs the
			// boxability guard and the register lane this machine has neither
			// of.
			name: "an i64 array element",
			input: func() *jit.Input {
				in := input(1, &types.Function{
					Typ: &types.FunctionType{Returns: []types.Type{types.TypeI64}},
					Code: assemble(t, func(b *instr.Builder) {
						b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
					}),
				})
				in.Constants = []types.Boxed{types.BoxRef(2)}
				in.Objects[2] = jit.Object{Array: jit.Itab(types.TypedArray[int64]{1})}
				return in
			}(),
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

	// A slot store is refused by the slot it writes, never by the value it
	// writes: overwriting a slot that currently holds a reference releases
	// that reference, and an i32 written over one drops its count exactly as
	// a reference would. The two compiles below differ in nothing but the
	// kind the global can hold, which is what pins the refusal to that rule
	// rather than to anything upstream of it.
	//
	// Neither pipeline lowers the refused half - the plan's own globalSet
	// declines a scalar written into a reference-kinded global too - so this
	// case stands outside the table above, whose contract is that the plan
	// still compiles what this machine declines.
	t.Run("refuses a store by the slot it writes", func(t *testing.T) {
		store := func(holds types.Kind) *jit.Input {
			in := input(1, &types.Function{
				Typ: &types.FunctionType{},
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.I32_CONST, 5).Emit(instr.GLOBAL_SET, 0).Emit(instr.RETURN)
				}),
			})
			in.Globals = []types.Kind{holds}
			return in
		}
		_, ok := arm64.New().Compile(asm.New(asmarm64.New()), store(types.KindI32), jit.Anchor{Addr: 1})
		require.True(t, ok, "a global that can only ever hold a scalar takes the store")

		_, ok = arm64.New().Compile(asm.New(asmarm64.New()), store(types.KindRef), jit.Anchor{Addr: 1})
		require.False(t, ok, "a global that can hold a reference does not, because the store would drop its count")
	})
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

// tape records one jit.Trace by decoding each instruction's Step from fn's
// own code, mirroring interp/trace.go's tracer.op; the caller sets whatever
// fields the tracer would have observed at runtime (Arg, Shape, Seen) on the
// *jit.Record it gets back.
type tape struct {
	ops []jit.Record
}

func (t *tape) at(fn *types.Function, addr, ip, depth int) *jit.Record {
	inst := instr.Instruction(fn.Code[ip:])
	t.ops = append(t.ops, jit.Record{Step: jit.Step{
		Op: inst.Opcode(), Args: jit.Args(inst), Fn: addr, IP: ip, Depth: depth,
	}})
	return &t.ops[len(t.ops)-1]
}

// fakeTraces is a jit.RecordedTraces client built from a fixed set of trees,
// standing in for interp's recorder (mirrors frontend/trace_test.go's own).
type fakeTraces map[jit.Anchor]*jit.Tree

func (f fakeTraces) Anchors(addr int) []int {
	var out []int
	for a, tree := range f {
		if a.Addr == addr && tree.Root != nil {
			out = append(out, a.IP)
		}
	}
	return out
}

func (f fakeTraces) RootAt(a jit.Anchor) *jit.Tree {
	return f[a]
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

// shapeExit, boundsExit, and kindExit are the labels the guarded reads' cold
// stubs take, in the order a guard and its read reserve them; each stub then
// takes one more, for the retain its null-reference test skips. STRUCT_GET is
// the only read that ever reaches kindExit - see structRead.
const (
	shapeExit asm.Label = iota + 1
	boundsExit
	kindExit
)

// stub is one cold stub of a guarded-read stream: the container and index
// operands flushed to their VM stack slots (at slot and slot+8), the retain
// the borrowed container owes the interpreter that adopts it, the published
// stack pointer, the one frame record, and the trap. first names the stub's
// own first virtual register and id the exit descriptor it reports; live is
// the label its null-reference test skips the retain to; ip is both the
// opcode's own IP (a guard exits before the operation it admits, never past
// it) and the frame record's IP; sp is the operand-stack depth (in words)
// the function had live at that opcode, which STRUCT_GET's one fewer
// declared param than the ARRAY_GET golden stream's own does not share.
func stub(first int32, id uint16, live asm.Label, ip int, slot int16, sp uint16) []asm.Instruction {
	ctrl, bp := vreg(first), vreg(first+5)
	return []asm.Instruction{
		// The container is held boxed already, so it flushes as it stands,
		// and it borrows its count from the constant pool, so the stub takes
		// the retain the resumed interpreter releases when it pops it.
		asmarm64.STR(vreg(0), vreg(4), slot),
		asmarm64.ANDI(vreg(first+1), vreg(0), 0xFFFFFFFF),
		asmarm64.CMPI(vreg(first+1), 0),
		asmarm64.BCondLabel(asmarm64.OpBEQ, live),
		asmarm64.LDR(vreg(first+2), ctrl, int16(journal.CellRC*8)),
		asmarm64.LDRR(vreg(first+3), vreg(first+2), vreg(first+1)),
		asmarm64.ADDI(vreg(first+3), vreg(first+3), 1),
		asmarm64.STRR(vreg(first+3), vreg(first+2), vreg(first+1)),
		asmarm64.MOV(vreg(first+4), vreg(1)),
		asmarm64.MOVK(vreg(first+4), tag(types.KindI32), 48),
		asmarm64.STR(vreg(first+4), vreg(4), slot+8),
		asmarm64.ADDI(vreg(first+6), bp, sp),
		asmarm64.STR(vreg(first+6), ctrl, int16(journal.CellSP*8)),
		asmarm64.MOVZ(vreg(first+7), 1, 0),
		asmarm64.STP(vreg(first+7), bp, ctrl, int16(journal.At(0, journal.RecordAddr)*8)),
		asmarm64.MOVZ(vreg(first+8), uint16(ip), 0),
		asmarm64.MOVZ(vreg(first+9), 1, 0),
		asmarm64.STP(vreg(first+8), vreg(first+9), ctrl, int16(journal.At(0, journal.RecordIP)*8)),
		asmarm64.MOVZ(vreg(first+10), 1, 0),
		asmarm64.STR(vreg(first+10), ctrl, int16(journal.CellDepth*8)),
		asmarm64.MOVZ(vreg(first+11), id, 0),
		asmarm64.STR(vreg(first+11), ctrl, int16(journal.CellExitID*8)),
		asmarm64.MOVZ(vreg(first+12), uint16(journal.TrapFallback), 0),
		asmarm64.STR(vreg(first+12), ctrl, int16(journal.CellTrap*8)),
		asmarm64.MOVZ(vreg(first+13), uint16(ip), 0),
		asmarm64.STR(vreg(first+13), ctrl, int16(journal.CellNextIP*8)),
		asmarm64.RET(),
	}
}

// arrayGetIP and structGetIP are where each guarded read's own opcode sits in
// its golden stream's bytecode, which is both the IP its frame record
// carries and the IP its stubs resume at.
const (
	arrayGetIP  = 5
	structGetIP = 8
)

// vreg, freg, freg32, and narrow name one virtual register of the stream a
// compile emits: the integer bank, the float bank at f64's width, the float
// bank at f32's own narrower width, and the 32-bit view of an integer
// register a value-lane test reads.
func vreg(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width64) }

func freg(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeFloat, asm.Width64) }

func freg32(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeFloat, asm.Width32) }

func narrow(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width32) }

// tag is the boxed word's kind tag as the 16-bit field MOVK writes at 48.
func tag(kind types.Kind) uint16 { return uint16(types.Tag(kind) >> 48) }
