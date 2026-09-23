package arm64_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	target "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// vr is the n-th machine-local virtual register the Machine allocates for
// its own scratch (m.vreg): the first call after Prologue is -2, decreasing
// by one per further call.
func vr(n int) asm.VReg { return asm.NewVReg(int32(-1-n), asm.RegTypeInt, asm.Width64) }

func TestMachine_Shape(t *testing.T) {
	i32array := func(v ssa.Value) ssa.Operation {
		return ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{1}, Results: []ssa.Value{v}}
	}

	t.Run("admits a matching array and passes the same word through", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef}
		ref, dst := r.Reg(1), r.Reg(2)

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0)
		start := len(a.Rows())
		require.True(t, m.Lower(a, i32array(2), r))

		rows := []asm.Instruction{
			target.SBFX(vr(1), ref, 0, 32), target.LSLI(vr(1), vr(1), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(1), target.X16, vr(1)),
			target.LDR(target.X16, vr(1), 0),
		}
		rows = append(rows, target.LDI(target.X17, uint64(jit.Itab(types.TypedArray[int32](nil))))...)
		rows = append(rows,
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
			target.MOV(dst, ref),
		)
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("admits a struct of the exact type and additionally checks Typ", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef}
		ref, dst := r.Reg(1), r.Reg(2)
		op := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Struct: true, Type: 0x2a}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0)
		start := len(a.Rows())
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(1), ref, 0, 32), target.LSLI(vr(1), vr(1), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(1), target.X16, vr(1)),
			target.LDR(target.X16, vr(1), 0),
		}
		rows = append(rows, target.LDI(target.X17, uint64(jit.Itab((*types.Struct)(nil))))...)
		rows = append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit))
		rows = append(rows,
			target.LDR(target.X16, vr(1), int16(jit.OffsetData)),
			target.LDR(target.X16, target.X16, int16(jit.OffsetStructTyp)),
		)
		rows = append(rows, target.LDI(target.X17, uint64(0x2a))...)
		rows = append(rows,
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
			target.MOV(dst, ref),
		)
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("admits any struct generically when Type is unset", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef}
		ref, dst := r.Reg(1), r.Reg(2)
		op := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Struct: true}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0)
		start := len(a.Rows())
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(1), ref, 0, 32), target.LSLI(vr(1), vr(1), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(1), target.X16, vr(1)),
			target.LDR(target.X16, vr(1), 0),
		}
		rows = append(rows, target.LDI(target.X17, uint64(jit.Itab((*types.Struct)(nil))))...)
		rows = append(rows,
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
			target.MOV(dst, ref),
		)
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("declines a shape naming no representation", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef}
		op := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.Kind(99)}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0)
		require.False(t, m.Lower(a, op, r))
	})
}

func TestMachine_Container(t *testing.T) {
	i32array := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	i8array := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI8}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	refarray := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindRef}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	structShape := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Struct: true, Type: 0x10}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}

	guarded := func(t *testing.T, r regs, guard ssa.Operation) (*arm64.Machine, *asm.Assembler) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0)
		require.True(t, m.Lower(a, guard, r))
		return m, a
	}

	t.Run("array.len loads the element count", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32}
		m, a := guarded(t, r, i32array)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{2}, Results: []ssa.Value{3}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), 0),
			target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
			target.MOVW(r.Reg(3), vr(5)),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("array.get loads a 4-byte scalar element", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI32}
		m, a := guarded(t, r, i32array)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), 0),
			target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.LSLI(vr(7), vr(6), 2), target.ADD(vr(7), vr(4), vr(7)),
			target.LDR(r.Reg(4), vr(7), 0),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("array.get loads a signed 1-byte element", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI8}
		m, a := guarded(t, r, i8array)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), 0),
			target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.ADD(vr(7), vr(4), vr(6)),
			target.LDRSB(r.Reg(4), vr(7), 0),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("array.get retains a loaded ref element", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeRef}
		m, a := guarded(t, r, refarray)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.ADDI(vr(3), vr(3), uint16(jit.OffsetArrayElems)),
			target.LDR(vr(4), vr(3), 0),
			target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.LSLI(vr(7), vr(6), 3), target.ADD(vr(7), vr(4), vr(7)),
			target.LDR(r.Reg(4), vr(7), 0),
		}
		rows = append(rows, retainRows(r.Reg(4))...)
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("array.set releases the old element and stores a new ref, adopting it", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeRef}
		m, a := guarded(t, r, refarray)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_SET, Args: []ssa.Value{2, 3, 4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.ADDI(vr(3), vr(3), uint16(jit.OffsetArrayElems)),
			target.LDR(vr(4), vr(3), 0),
			target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.LSLI(vr(7), vr(6), 3), target.ADD(vr(7), vr(4), vr(7)),
			target.LDR(vr(8), vr(7), 0),
			target.STR(r.Reg(4), vr(7), 0),
		}
		rows = append(rows, releaseRows(vr(8))...)
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("array.set stores a 4-byte i32 element with the narrow STRW form", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI32}
		m, a := guarded(t, r, i32array)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_SET, Args: []ssa.Value{2, 3, 4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), 0),
			target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.LSLI(vr(7), vr(6), 2), target.ADD(vr(7), vr(4), vr(7)),
			target.STRW(r.Reg(4), vr(7), 0),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("array.set stores a 1-byte scalar element", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI8}
		m, a := guarded(t, r, i8array)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_SET, Args: []ssa.Value{2, 3, 4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), 0),
			target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.ADD(vr(7), vr(4), vr(6)),
			target.STRB(r.Reg(4), vr(7), 0),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("declines a container that carries no shape guard", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI32}
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0)
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{1, 3}, Results: []ssa.Value{4}}
		require.False(t, m.Lower(a, op, r))
	})

	t.Run("struct.get loads a 4-byte field with no bounds check", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI32}
		m, a := guarded(t, r, structShape)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_GET, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), int16(jit.OffsetStructData)),
			target.SXTW(vr(5), r.Reg(3)),
			target.LSLI(vr(6), vr(5), 3), target.ADD(vr(6), vr(4), vr(6)),
			target.LDR(r.Reg(4), vr(6), 0),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("struct.get retains a ref field", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeRef}
		m, a := guarded(t, r, structShape)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_GET, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), int16(jit.OffsetStructData)),
			target.SXTW(vr(5), r.Reg(3)),
			target.LSLI(vr(6), vr(5), 3), target.ADD(vr(6), vr(4), vr(6)),
			target.LDR(r.Reg(4), vr(6), 0),
		}
		rows = append(rows, retainRows(r.Reg(4))...)
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("struct.set stores a 4-byte field, bounds-checked at Typ.Fields' own length", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI32}
		m, a := guarded(t, r, structShape)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_SET, Args: []ssa.Value{2, 3, 4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), int16(jit.OffsetStructTyp)),
			target.LDR(vr(5), vr(4), int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.LDR(vr(7), vr(3), int16(jit.OffsetStructData)),
			target.LSLI(vr(8), vr(6), 3), target.ADD(vr(8), vr(7), vr(8)),
			target.UXTW(target.X16, r.Reg(4)),
			target.STR(target.X16, vr(8), 0),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("struct.set widens and sign-extends a 1-byte field", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeI8}
		m, a := guarded(t, r, structShape)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_SET, Args: []ssa.Value{2, 3, 4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), int16(jit.OffsetStructTyp)),
			target.LDR(vr(5), vr(4), int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.LDR(vr(7), vr(3), int16(jit.OffsetStructData)),
			target.LSLI(vr(8), vr(6), 3), target.ADD(vr(8), vr(7), vr(8)),
			target.SBFX(target.W16, r.Reg(4), 0, 8),
			target.STR(target.X16, vr(8), 0),
		}
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("struct.set releases the old ref field and adopts the new one", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI32, 4: ssa.TypeRef}
		m, a := guarded(t, r, structShape)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_SET, Args: []ssa.Value{2, 3, 4}}
		require.True(t, m.Lower(a, op, r))

		rows := []asm.Instruction{
			target.SBFX(vr(2), r.Reg(2), 0, 32), target.LSLI(vr(2), vr(2), 4),
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(2), target.X16, vr(2)),
			target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
			target.LDR(vr(4), vr(3), int16(jit.OffsetStructTyp)),
			target.LDR(vr(5), vr(4), int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)),
			target.SXTW(vr(6), r.Reg(3)),
			target.CMPI(vr(6), 0), target.BCondLabel(target.OpBLT, exit),
			target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBGE, exit),
			target.LDR(vr(7), vr(3), int16(jit.OffsetStructData)),
			target.LSLI(vr(8), vr(6), 3), target.ADD(vr(8), vr(7), vr(8)),
			target.LDR(vr(9), vr(8), 0),
			target.STR(r.Reg(4), vr(8), 0),
		}
		rows = append(rows, releaseRows(vr(9))...)
		require.Equal(t, rows, a.Rows()[start:])
	})

	t.Run("ref.is_null tests the low word of a boxed ref, no guard required", func(t *testing.T) {
		r := regs{1: ssa.TypeRef, 2: ssa.TypeI1}
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0)
		start := len(a.Rows())
		op := ssa.Operation{Op: ssa.OpExec, Code: instr.REF_IS_NULL, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
		require.True(t, m.Lower(a, op, r))

		require.Equal(t, []asm.Instruction{
			target.SBFX(target.X16, r.Reg(1), 0, 32),
			target.CMPI(target.X16, 0),
			target.CSET(r.Reg(2), target.CondEQ),
		}, a.Rows()[start:])
	})
}

// retainRows is the row sequence m.retain emits for ref: the shared
// reference-count increment TestMachine_Lower's "retain counts any
// reference up" case already specifies independently. retain's own skip
// label is self-allocated (not Site-provided), so it is the next label
// after Prologue's two (end, entry) in every test here, matching
// TestMachine_Lower's hardcoded 2 for the same reason.
func retainRows(ref asm.VReg) []asm.Instruction {
	rows := []asm.Instruction{target.LSRI(target.X16, ref, 49)}
	rows = append(rows, target.LDI(target.X17, types.Tag(types.KindRef)>>49)...)
	rows = append(rows,
		target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, asm.Label(2)),
		target.SBFX(target.X17, ref, 0, 32),
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
		target.ADDI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0),
	)
	return rows
}

// releaseRows is the row sequence m.release emits for ref, resuming at
// resume — the shared reference-count decrement TestMachine_Lower's
// "release counts a non-null reference down" case already specifies
// independently.
func releaseRows(ref asm.VReg) []asm.Instruction {
	rows := []asm.Instruction{target.LSRI(target.X16, ref, 49)}
	rows = append(rows, target.LDI(target.X17, types.Tag(types.KindRef)>>49)...)
	rows = append(rows,
		target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, resume),
		target.SBFX(target.X17, ref, 0, 32),
		target.CBZLabel(target.X17, resume),
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
		target.CMPI(target.X17, 1), target.BCondLabel(target.OpBLE, exit),
		target.SUBI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0),
	)
	return rows
}
