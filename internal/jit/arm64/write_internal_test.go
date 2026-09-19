package arm64

import (
	"reflect"
	"slices"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func testInput(fn *types.Function) *jit.Input {
	return &jit.Input{Address: 1, Function: fn, Objects: jit.Objects{1: {Fn: fn}}}
}

func vreg(id int32) asm.VReg     { return asm.NewVReg(id, asm.RegTypeInt, asm.Width64) }
func narrow(id int32) asm.VReg   { return asm.NewVReg(id, asm.RegTypeInt, asm.Width32) }
func tag(kind types.Kind) uint16 { return uint16(types.Tag(kind) >> 48) }

// prologue is the entry every golden stream in this file opens with: the
// journal header mirrored into the pinned context registers.
func prologue() []asm.Instruction {
	return []asm.Instruction{
		asmarm64.MOV(asmarm64.X14, asmarm64.X0),
		asmarm64.LDP(asmarm64.X10, asmarm64.X11, asmarm64.X14, int16(journal.CellStack*8)),
		asmarm64.LDR(asmarm64.X12, asmarm64.X14, int16(journal.CellBP*8)),
	}
}

// frameBase derives the frame base every slot is addressed from: the VM
// stack plus the frame pointer scaled to bytes.
func frameBase(base, bp, stack int32) []asm.Instruction {
	return []asm.Instruction{
		asmarm64.LSLI(vreg(base), vreg(bp), 3),
		asmarm64.ADD(vreg(base), vreg(stack), vreg(base)),
	}
}

// heapSetFn builds the ssa.Function one ARRAY_SET or STRUCT_SET golden
// compiles: a container, an index, and a value, each a compile-time
// constant, guarded through shape and stored at the guarded cell. container
// comes first so its own register (vreg(0)) matches the container operand
// every stub below flushes at slot 0.
func heapSetFn(code instr.Opcode, shape ssa.Shape, container, val types.Boxed, valTyp ssa.Type) *ssa.Function {
	b := ssa.New("f")
	block := b.Block()
	containerV := b.Value(ssa.TypeRef)
	idxV := b.Value(ssa.TypeI32)
	valV := b.Value(valTyp)
	guardedV := b.Value(ssa.TypeRef)
	state := b.Value(ssa.TypeState)

	b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: container, Results: []ssa.Value{containerV}})
	b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{idxV}})
	b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: val, Results: []ssa.Value{valV}})
	b.Add(block, ssa.Operation{
		Op:      ssa.OpState,
		Frames:  []ssa.Frame{{Addr: 1, Stack: []ssa.Operand{{Value: containerV}, {Value: idxV}, {Value: valV}}}},
		Results: []ssa.Value{state},
	})
	b.Add(block, ssa.Operation{
		Op: ssa.OpGuardShape, Shape: shape,
		Args: []ssa.Value{containerV}, State: state, Results: []ssa.Value{guardedV},
	})
	b.Add(block, ssa.Operation{
		Op: ssa.OpExec, Code: code,
		Args: []ssa.Value{guardedV, idxV, valV}, State: state,
	})
	b.Term(block, ssa.Terminator{Op: ssa.OpReturn})
	return b.Build()
}

// TestEmitter_Write proves the array-element store this machine fuses with
// its own container guard: the shape check runs before the bounds check,
// which runs before the store, and both exits flush the same three operands
// - container, index, value - the interpreter had live at ARRAY_SET's own IP.
func TestEmitter_Write(t *testing.T) {
	t.Run("stores a scalar i32 array element through the shape that admitted it", func(t *testing.T) {
		fn := heapSetFn(instr.ARRAY_SET, ssa.Shape{Itab: jit.HeapArrayI32}, types.BoxRef(2), types.BoxI32(9), ssa.TypeI32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})

		a := asm.New(asmarm64.New())
		_, ok := backend.Compile(New(), a, in, jit.Anchor{Addr: 1}, fn)
		require.True(t, ok)

		want := slices.Concat(
			prologue(),
			frameBase(4, 5, 6),
			asmarm64.LDI(vreg(0), uint64(types.BoxRef(2))),
			[]asm.Instruction{asmarm64.MOVZ(narrow(1), 0, 0), asmarm64.MOVZ(narrow(2), 9, 0)},
			[]asm.Instruction{asmarm64.LSRI(vreg(7), vreg(0), uint8(types.VBits))},
			asmarm64.LDI(vreg(8), uint64(types.Tag(types.KindRef))>>types.VBits),
			[]asm.Instruction{
				asmarm64.CMP(vreg(7), vreg(8)),
				asmarm64.BCondLabel(asmarm64.OpBNE, shapeExitLabel),
				asmarm64.ANDI(vreg(9), vreg(0), 0xFFFFFFFF),
				asmarm64.LDR(vreg(10), vreg(15), int16(journal.CellHeap*8)),
				asmarm64.LSLI(vreg(11), vreg(9), 4),
				asmarm64.ADD(vreg(12), vreg(10), vreg(11)),
				asmarm64.LDR(vreg(13), vreg(12), 0),
				asmarm64.LDR(vreg(14), vreg(12), 8),
			},
			asmarm64.LDI(vreg(16), uint64(jit.HeapArrayI32)),
			[]asm.Instruction{
				asmarm64.CMP(vreg(13), vreg(16)),
				asmarm64.BCondLabel(asmarm64.OpBNE, shapeExitLabel),
				asmarm64.MOV(vreg(3), vreg(0)),

				asmarm64.LDR(vreg(17), vreg(14), 0),
				asmarm64.LDR(vreg(18), vreg(14), 8),
				asmarm64.SXTW(vreg(19), narrow(1)),
				asmarm64.CMP(vreg(19), vreg(18)),
				asmarm64.BCondLabel(asmarm64.OpBCS, boundsExitLabel),
				asmarm64.LSLI(vreg(21), vreg(19), 2),
				asmarm64.ADD(vreg(20), vreg(17), vreg(21)),
				asmarm64.STRW(vreg(2), vreg(20), 0),
				asmarm64.RET(),
			},
			writeStub(22, 1, retainLive1),
			writeStub(39, 2, retainLive2),
		)
		require.Equal(t, want, a.Instructions())

		code, err := a.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

	t.Run("declines a reference-typed element", func(t *testing.T) {
		elem, ok := jit.ElemShapeByKind(types.KindRef)
		require.True(t, ok)
		fn := heapSetFn(instr.ARRAY_SET, ssa.Shape{Itab: elem.Itab}, types.BoxRef(2), types.BoxRef(3), ssa.TypeRef)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		_, ok = backend.Compile(New(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})

	t.Run("declines a guard shape no element row recognizes", func(t *testing.T) {
		fn := heapSetFn(instr.ARRAY_SET, ssa.Shape{Itab: jit.HeapStruct}, types.BoxRef(2), types.BoxI32(9), ssa.TypeI32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		_, ok := backend.Compile(New(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})

	// The remaining rows exercise raw's own i64, f32, and f64 conversions,
	// which the i32 golden above never reaches, against every element width
	// write addresses: byte, word-scaled, and doubleword-scaled.
	for _, tt := range []struct {
		name   string
		kind   types.Kind
		val    types.Boxed
		valTyp ssa.Type
	}{
		{name: "i1", kind: types.KindI1, val: types.Box(1, types.KindI1), valTyp: ssa.TypeI1},
		{name: "i8", kind: types.KindI8, val: types.Box(uint64(uint8(7)), types.KindI8), valTyp: ssa.TypeI8},
		{name: "i64", kind: types.KindI64, val: types.BoxI64(1 << 40), valTyp: ssa.TypeI64},
		{name: "f32", kind: types.KindF32, val: types.Box(0, types.KindF32), valTyp: ssa.TypeF32},
		{name: "f64", kind: types.KindF64, val: types.Box(0, types.KindF64), valTyp: ssa.TypeF64},
	} {
		t.Run("stores a scalar "+tt.name+" array element", func(t *testing.T) {
			elem, ok := jit.ElemShapeByKind(tt.kind)
			require.True(t, ok)
			fn := heapSetFn(instr.ARRAY_SET, ssa.Shape{Itab: elem.Itab}, types.BoxRef(2), tt.val, tt.valTyp)
			in := testInput(&types.Function{Typ: &types.FunctionType{}})
			a := asm.New(asmarm64.New())
			_, ok = backend.Compile(New(), a, in, jit.Anchor{Addr: 1}, fn)
			require.True(t, ok)
			code, err := a.Build()
			require.NoError(t, err)
			require.NotEmpty(t, code)
		})
	}
}

// shapeExitLabel, boundsExitLabel, retainLive1, and retainLive2 are the
// labels TestEmitter_Write's own guard, bounds check, and two stubs' retain
// skips reserve, in reservation order: block 0's own label (0) is bound by
// newCompiler before Enter ever runs, so the guard's own exit is the first
// this compile reserves.
const (
	shapeExitLabel asm.Label = iota + 1
	boundsExitLabel
	retainLive1
	retainLive2
)

// TestEmitter_StructWrite proves the field store this machine fuses with its
// own container guard: unlike structRead, the guard admits any struct itab
// without narrowing to a specific *types.StructType, since the fields table
// is read back out of the runtime type pointer the guard's own cell already
// carries rather than a compile-time one - so the golden below carries no
// Shape.Typ and the store still runs against whichever struct type shows up
// at the guarded cell.
func TestEmitter_StructWrite(t *testing.T) {
	t.Run("stores a scalar i32 struct field through the shape that admitted it", func(t *testing.T) {
		fn := heapSetFn(instr.STRUCT_SET, ssa.Shape{Itab: jit.HeapStruct}, types.BoxRef(2), types.BoxI32(9), ssa.TypeI32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})

		a := asm.New(asmarm64.New())
		_, ok := backend.Compile(New(), a, in, jit.Anchor{Addr: 1}, fn)
		require.True(t, ok)

		want := slices.Concat(
			prologue(),
			frameBase(4, 5, 6),
			asmarm64.LDI(vreg(0), uint64(types.BoxRef(2))),
			[]asm.Instruction{asmarm64.MOVZ(narrow(1), 0, 0), asmarm64.MOVZ(narrow(2), 9, 0)},
			[]asm.Instruction{asmarm64.LSRI(vreg(7), vreg(0), uint8(types.VBits))},
			asmarm64.LDI(vreg(8), uint64(types.Tag(types.KindRef))>>types.VBits),
			[]asm.Instruction{
				asmarm64.CMP(vreg(7), vreg(8)),
				asmarm64.BCondLabel(asmarm64.OpBNE, structShapeExitLabel),
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
				asmarm64.BCondLabel(asmarm64.OpBNE, structShapeExitLabel),
				asmarm64.MOV(vreg(3), vreg(0)),

				asmarm64.LDR(vreg(17), vreg(14), int16(structTyp)),
				asmarm64.LDR(vreg(18), vreg(17), int16(fieldsSlice+sliceData)),
				asmarm64.LDR(vreg(19), vreg(17), int16(fieldsSlice+sliceLen)),
				asmarm64.SXTW(vreg(20), narrow(1)),
				asmarm64.CMP(vreg(20), vreg(19)),
				asmarm64.BCondLabel(asmarm64.OpBCS, structBoundsExitLabel),
			},
			asmarm64.LDI(vreg(21), uint64(fieldSize)),
			[]asm.Instruction{
				asmarm64.MUL(vreg(21), vreg(20), vreg(21)),
				asmarm64.ADD(vreg(22), vreg(18), vreg(21)),
				asmarm64.LDRB(vreg(23), vreg(22), int16(fieldKind)),
				asmarm64.CMPI(vreg(23), uint16(types.KindI32)),
				asmarm64.BCondLabel(asmarm64.OpBNE, structKindExitLabel),
				asmarm64.LDR(vreg(24), vreg(14), int16(structData+sliceData)),
				asmarm64.STRR(vreg(2), vreg(24), vreg(20)),
				asmarm64.RET(),
			},
			writeStub(25, 1, structRetainLive1),
			writeStub(42, 2, structRetainLive2),
			writeStub(59, 3, structRetainLive3),
		)
		require.Equal(t, want, a.Instructions())

		code, err := a.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

	t.Run("declines a reference-typed field", func(t *testing.T) {
		fn := heapSetFn(instr.STRUCT_SET, ssa.Shape{Itab: jit.HeapStruct}, types.BoxRef(2), types.BoxRef(3), ssa.TypeRef)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		_, ok := backend.Compile(New(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})

	t.Run("declines a guard shape that is not a struct", func(t *testing.T) {
		elem, ok := jit.ElemShapeByKind(types.KindI32)
		require.True(t, ok)
		fn := heapSetFn(instr.STRUCT_SET, ssa.Shape{Itab: elem.Itab}, types.BoxRef(2), types.BoxI32(9), ssa.TypeI32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		_, ok = backend.Compile(New(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})
}

// structShapeExitLabel, structBoundsExitLabel, structKindExitLabel, and the
// three struct retain-skip labels are TestEmitter_StructWrite's own labels,
// in reservation order following the same rule as TestEmitter_Write's.
const (
	structShapeExitLabel asm.Label = iota + 1
	structBoundsExitLabel
	structKindExitLabel
	structRetainLive1
	structRetainLive2
	structRetainLive3
)

// TestEmitter_HostWrite proves hostWrite's own shape checks: an exact-width
// field takes the store, and each of a narrower field, a value whose kind
// disagrees with the field's Go kind, and a specific struct type pinned onto
// what is otherwise a host guard is refused rather than risking a wrong
// write into Go memory.
func TestEmitter_HostWrite(t *testing.T) {
	layout := jit.Layout{
		HostStructItab:  777,
		HostFields:      16,
		HostFieldOffset: 0,
		HostFieldConv:   8,
		HostFieldSize:   24,
		HostConvKind:    4,
	}

	t.Run("stores an exact-width host field", func(t *testing.T) {
		fn := heapSetFn(instr.STRUCT_SET, ssa.Shape{Itab: layout.HostStructItab, Host: reflect.Int32}, types.BoxRef(2), types.BoxI32(9), ssa.TypeI32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		in.Layout = layout

		a := asm.New(asmarm64.New())
		_, ok := backend.Compile(New(), a, in, jit.Anchor{Addr: 1}, fn)
		require.True(t, ok)

		code, err := a.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

	t.Run("declines a field narrower than its slot", func(t *testing.T) {
		fn := heapSetFn(instr.STRUCT_SET, ssa.Shape{Itab: layout.HostStructItab, Host: reflect.Int16}, types.BoxRef(2), types.BoxI32(9), ssa.TypeI32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		in.Layout = layout
		_, ok := backend.Compile(New(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})

	t.Run("declines a value whose kind disagrees with the field's Go kind", func(t *testing.T) {
		fn := heapSetFn(instr.STRUCT_SET, ssa.Shape{Itab: layout.HostStructItab, Host: reflect.Int32}, types.BoxRef(2), types.Box(0, types.KindF32), ssa.TypeF32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		in.Layout = layout
		_, ok := backend.Compile(New(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})

	t.Run("declines a host guard carrying a struct type", func(t *testing.T) {
		fn := heapSetFn(instr.STRUCT_SET, ssa.Shape{Itab: layout.HostStructItab, Typ: 1, Host: reflect.Int32}, types.BoxRef(2), types.BoxI32(9), ssa.TypeI32)
		in := testInput(&types.Function{Typ: &types.FunctionType{}})
		in.Layout = layout
		_, ok := backend.Compile(New(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})
}

// writeStub is the cold stub write's own guarded store lowers into: the
// container flushes as it stands and takes the retain the interpreter's
// adopt on resume owes it, the index and the value each box through a fresh
// tagged word to their own slot, the stack pointer publishes, one frame
// record appends, and the trap reports fallback. first names the stub's own
// first virtual register and id the exit descriptor it reports; live is the
// label its null-reference test skips the retain to.
func writeStub(first int32, id uint16, live asm.Label) []asm.Instruction {
	ctrl := vreg(first)
	addr, base, rc := vreg(first+1), vreg(first+2), vreg(first+3)
	idxBoxed, valBoxed := vreg(first+4), vreg(first+5)
	bp, sp := vreg(first+6), vreg(first+7)
	depth, off, recBase := vreg(first+8), vreg(first+9), vreg(first+10)
	recAddr, ip, returns := vreg(first+11), vreg(first+12), vreg(first+13)
	exitReg, trapReg, nextReg := vreg(first+14), vreg(first+15), vreg(first+16)
	return []asm.Instruction{
		asmarm64.STR(vreg(0), vreg(4), 0),
		asmarm64.ANDI(addr, vreg(0), 0xFFFFFFFF),
		asmarm64.CMPI(addr, 0),
		asmarm64.BCondLabel(asmarm64.OpBEQ, live),
		asmarm64.LDR(base, ctrl, int16(journal.CellRC*8)),
		asmarm64.LDRR(rc, base, addr),
		asmarm64.ADDI(rc, rc, 1),
		asmarm64.STRR(rc, base, addr),

		asmarm64.MOV(idxBoxed, vreg(1)),
		asmarm64.MOVK(idxBoxed, tag(types.KindI32), 48),
		asmarm64.STR(idxBoxed, vreg(4), 8),
		asmarm64.MOV(valBoxed, vreg(2)),
		asmarm64.MOVK(valBoxed, tag(types.KindI32), 48),
		asmarm64.STR(valBoxed, vreg(4), 16),

		asmarm64.ADDI(sp, bp, 3),
		asmarm64.STR(sp, ctrl, int16(journal.CellSP*8)),

		asmarm64.LDR(depth, ctrl, int16(journal.CellDepth*8)),
		asmarm64.LSLI(off, depth, journal.Shift),
		asmarm64.ADD(recBase, ctrl, off),
		asmarm64.MOVZ(recAddr, 1, 0),
		asmarm64.STP(recAddr, bp, recBase, int16(journal.At(0, journal.RecordAddr)*8)),
		asmarm64.MOVZ(ip, 0, 0),
		asmarm64.MOVZ(returns, 0, 0),
		asmarm64.STP(ip, returns, recBase, int16(journal.At(0, journal.RecordIP)*8)),
		asmarm64.ADDI(depth, depth, 1),
		asmarm64.STR(depth, ctrl, int16(journal.CellDepth*8)),

		asmarm64.MOVZ(exitReg, id, 0),
		asmarm64.STR(exitReg, ctrl, int16(journal.CellExitID*8)),
		asmarm64.MOVZ(trapReg, uint16(journal.TrapFallback), 0),
		asmarm64.STR(trapReg, ctrl, int16(journal.CellTrap*8)),
		asmarm64.MOVZ(nextReg, 0, 0),
		asmarm64.STR(nextReg, ctrl, int16(journal.CellNextIP*8)),
		asmarm64.RET(),
	}
}
