package arm64_test

import (
	"math"
	"slices"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	target "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// regs names value v with the register its type is represented in; every
// exit it is asked for is exit, and every release resumes at resume.
type regs map[ssa.Value]ssa.Type

const (
	exit   = asm.Label(99)
	resume = asm.Label(98)
)

func (r regs) Type(v ssa.Value) ssa.Type { return r[v] }

func (r regs) Slot(s ssa.Slot) ssa.Type {
	if s.Space == ssa.SpaceGlobal {
		return ssa.TypeRef
	}
	return 0
}

func (r regs) Deopt() asm.Label { return exit }

func (r regs) Release(asm.VReg) (asm.Label, asm.Label) { return exit, resume }

func (r regs) Box(asm.VReg) (asm.Label, asm.Label) { return exit, resume }

func (r regs) Reg(v ssa.Value) asm.VReg {
	switch r[v] {
	case ssa.TypeI64, ssa.TypeRef:
		return asm.NewVReg(int32(v), asm.RegTypeInt, asm.Width64)
	case ssa.TypeF32:
		return asm.NewVReg(int32(v), asm.RegTypeFloat, asm.Width32)
	case ssa.TypeF64:
		return asm.NewVReg(int32(v), asm.RegTypeFloat, asm.Width64)
	default:
		return asm.NewVReg(int32(v), asm.RegTypeInt, asm.Width32)
	}
}

func TestMachine_Reserve(t *testing.T) {
	require.Equal(t, []asm.PReg{target.X16, target.X17, target.X24, target.X25, target.X27}, arm64.New().Reserve())
}

func TestMachine_Prologue(t *testing.T) {
	a := asm.New(target.New())
	arm64.New().Prologue(a, []types.Kind{types.KindI32, types.KindI64, types.KindRef}, 1, true, 3, nil, nil)
	require.Equal(t, []asm.Instruction{
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		{Op: uint16(target.OpSUBI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LSLI(target.X17, target.X27, 5),
		target.ADD(target.X17, target.Ctx, target.X17),
		target.STR(target.X25, target.X17, int16(jit.OffsetRecords+jit.RecordFB)),
		target.STR(target.LR, target.X17, int16(jit.OffsetRecords+jit.RecordPC)),
		target.ADDI(target.X27, target.X27, 1),
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetEntries)),
		target.LDR(target.X17, target.X16, 24),
		target.ADDI(target.X17, target.X17, 1),
		target.STR(target.X17, target.X16, 24),
		target.STR(target.XZR, target.X25, 8),
		target.STR(target.XZR, target.X25, 16),
	}, a.Rows())

	t.Run("skips entry count", func(t *testing.T) {
		a := asm.New(target.New())
		arm64.New().Prologue(a, []types.Kind{types.KindI32, types.KindI64, types.KindRef}, 1, false, 3, nil, nil)
		require.Len(t, a.Rows(), 10)
		require.NotContains(t, a.Rows(), target.LDR(target.X16, target.Ctx, int16(jit.OffsetEntries)))
	})

	t.Run("captures register-passed parameters once the frame is built", func(t *testing.T) {
		a := asm.New(target.New())
		got := arm64.New().Prologue(a, []types.Kind{types.KindI32, types.KindRef}, 2, false, 0, []types.Kind{types.KindI32, types.KindRef}, nil)
		want := []asm.VReg{asm.NewVReg(-2, asm.RegTypeInt, asm.Width32), asm.NewVReg(-3, asm.RegTypeInt, asm.Width64)}
		require.Equal(t, want, got)
		require.Equal(t, []asm.Instruction{
			target.DEF(target.X0),
			target.DEF(target.X1),
			target.MOVW(want[0], target.X0),
			target.MOV(want[1], target.X1),
		}, a.Rows()[8:])
	})

	t.Run("captures a float register-passed parameter by FMOV", func(t *testing.T) {
		a := asm.New(target.New())
		got := arm64.New().Prologue(a, []types.Kind{types.KindF32, types.KindF64}, 2, false, 0, []types.Kind{types.KindF32, types.KindF64}, nil)
		want := []asm.VReg{asm.NewVReg(-2, asm.RegTypeFloat, asm.Width32), asm.NewVReg(-3, asm.RegTypeFloat, asm.Width64)}
		require.Equal(t, want, got)
		require.Equal(t, []asm.Instruction{
			target.DEF(target.X0),
			target.DEF(target.X1),
			target.FMOV(want[0], target.X0),
			target.FMOV(want[1], target.X1),
		}, a.Rows()[8:])
	})

	t.Run("captures an i64 register-passed parameter by a raw 64-bit MOV", func(t *testing.T) {
		a := asm.New(target.New())
		got := arm64.New().Prologue(a, []types.Kind{types.KindI64, types.KindRef}, 2, false, 0, []types.Kind{types.KindI64, types.KindRef}, nil)
		want := []asm.VReg{asm.NewVReg(-2, asm.RegTypeInt, asm.Width64), asm.NewVReg(-3, asm.RegTypeInt, asm.Width64)}
		require.Equal(t, want, got)
		require.Equal(t, []asm.Instruction{
			target.DEF(target.X0),
			target.DEF(target.X1),
			target.MOV(want[0], target.X0),
			target.MOV(want[1], target.X1),
		}, a.Rows()[8:])
	})
}

func TestMachine_Epilogue(t *testing.T) {
	pop := []asm.Instruction{
		target.SUBI(target.X27, target.X27, 1),
		{Op: uint16(target.OpADDI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LDR(target.LR, target.SP, 8),
		target.ADDI(target.SP, target.SP, 16),
		target.RET(),
	}

	m, a := arm64.New(), asm.New(target.New())
	m.Prologue(a, nil, 0, true, 0, nil, nil)
	start := len(a.Rows())
	m.Epilogue(a)
	require.Equal(t, pop, a.Rows()[start:])
}

func TestMachine_Enter(t *testing.T) {
	t.Run("loads the pinned registers, calls the body, and returns with no results", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		label := m.Enter(a, nil, nil)

		// entry is Prologue's own label, the second label a fresh Assembler
		// allocates (end is the first).
		entry := asm.Label(1)
		require.Equal(t, []asm.Instruction{
			target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
			target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(entry),
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		}, a.Rows()[start:])

		code, err := a.Build()
		require.NoError(t, err)
		off, ok := a.Offset(label)
		require.True(t, ok)
		require.Equal(t, start*4, off)
		require.Len(t, code, len(a.Rows())*4)
	})

	t.Run("boxes a narrow result by tag in slot 0 and a ref raw in slot 1", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		m.Enter(a, nil, []types.Kind{types.KindI32, types.KindRef})

		want := []asm.Instruction{
			target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
			target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(1),
			target.UXTW(target.X16, target.X0),
		}
		want = append(want, target.LDI(target.X17, types.Tag(types.KindI32))...)
		want = append(want,
			target.ORR(target.X16, target.X16, target.X17),
			target.STR(target.X16, target.X25, 0),
			target.STR(target.X1, target.X25, 8),
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		)
		require.Equal(t, want, a.Rows()[start:])
	})

	t.Run("boxes an f64 result raw", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		m.Enter(a, nil, []types.Kind{types.KindF64})

		require.Equal(t, target.STR(target.X0, target.X25, 0), a.Rows()[start+6])
	})

	t.Run("stores an i64 result raw", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		m.Enter(a, nil, []types.Kind{types.KindI64})

		require.Equal(t, target.STR(target.X0, target.X25, 0), a.Rows()[start+6])
	})

	t.Run("loads and unboxes register-passed parameters from their slots before the body", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		m.Enter(a, []types.Kind{types.KindI32, types.KindRef}, nil)

		require.Equal(t, []asm.Instruction{
			target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
			target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
			target.LDR(asm.NewPReg(target.X0.ID(), asm.RegTypeInt, asm.Width32), target.X25, 0),
			target.LDR(target.X1, target.X25, 8),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(1),
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		}, a.Rows()[start:])
	})

	t.Run("loads an f32 register-passed parameter's low word and an f64's whole word", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		m.Enter(a, []types.Kind{types.KindF32, types.KindF64}, nil)

		require.Equal(t, []asm.Instruction{
			target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
			target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
			target.LDR(asm.NewPReg(target.X0.ID(), asm.RegTypeInt, asm.Width32), target.X25, 0),
			target.LDR(target.X1, target.X25, 8),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(1),
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		}, a.Rows()[start:])
	})

	t.Run("loads an i64 register-passed parameter's slot and unboxes its 49-bit payload inline", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		m.Enter(a, []types.Kind{types.KindI64}, nil)

		require.Equal(t, []asm.Instruction{
			target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
			target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
			target.LDR(target.X0, target.X25, 0),
			target.SBFX(target.X0, target.X0, 0, 49),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(1),
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		}, a.Rows()[start:])
	})
}

func TestMachine_Lower(t *testing.T) {
	type emit2 = func(dst, src asm.Reg) asm.Instruction
	type emit3 = func(dst, src1, src2 asm.Reg) asm.Instruction
	type test struct {
		name  string
		regs  regs
		op    ssa.Operation
		rows  []asm.Instruction
		lower bool
	}
	i32, i64, f32, f64 := ssa.TypeI32, ssa.TypeI64, ssa.TypeF32, ssa.TypeF64
	reg := func(typ ssa.Type, v ssa.Value) asm.VReg { return regs{v: typ}.Reg(v) }
	exec := func(code instr.Opcode, args ...ssa.Value) ssa.Operation {
		return ssa.Operation{Op: ssa.OpExec, Code: code, Args: args, Results: []ssa.Value{ssa.Value(len(args) + 1)}}
	}
	boxed := func(k types.Kind, rows ...asm.Instruction) []asm.Instruction {
		rows = append(rows, target.LDI(target.X17, types.Tag(k))...)
		return append(rows, target.ORR(target.X16, target.X16, target.X17))
	}
	local := func(i int) ssa.Slot { return ssa.Slot{Space: ssa.SpaceLocal, Index: i} }
	globals := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
	old := asm.NewVReg(-3, asm.RegTypeInt, asm.Width64)
	heap := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
	counter := []asm.Instruction{
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
	}

	var tests []test
	for _, c := range []struct {
		code instr.Opcode
		typ  ssa.Type
		emit emit3
	}{
		{instr.I32_ADD, i32, target.ADD}, {instr.I32_SUB, i32, target.SUB}, {instr.I32_MUL, i32, target.MUL},
		{instr.I32_AND, i32, target.AND}, {instr.I32_OR, i32, target.ORR}, {instr.I32_XOR, i32, target.EOR},
		{instr.I32_SHL, i32, target.LSL}, {instr.I32_SHR_S, i32, target.ASR}, {instr.I32_SHR_U, i32, target.LSR},
		{instr.I64_ADD, i64, target.ADD}, {instr.I64_SUB, i64, target.SUB}, {instr.I64_MUL, i64, target.MUL},
		{instr.I64_AND, i64, target.AND}, {instr.I64_OR, i64, target.ORR}, {instr.I64_XOR, i64, target.EOR},
		{instr.I64_SHL, i64, target.LSL}, {instr.I64_SHR_S, i64, target.ASR}, {instr.I64_SHR_U, i64, target.LSR},
		{instr.F32_ADD, f32, target.FADD}, {instr.F32_SUB, f32, target.FSUB}, {instr.F32_MUL, f32, target.FMUL},
		{instr.F32_DIV, f32, target.FDIV}, {instr.F32_MIN, f32, target.FMIN}, {instr.F32_MAX, f32, target.FMAX},
		{instr.F64_ADD, f64, target.FADD}, {instr.F64_SUB, f64, target.FSUB}, {instr.F64_MUL, f64, target.FMUL},
		{instr.F64_DIV, f64, target.FDIV}, {instr.F64_MIN, f64, target.FMIN}, {instr.F64_MAX, f64, target.FMAX},
	} {
		tests = append(tests, test{
			name: instr.TypeOf(c.code).Mnemonic,
			regs: regs{1: c.typ, 2: c.typ, 3: c.typ},
			op:   exec(c.code, 1, 2),
			rows: []asm.Instruction{c.emit(reg(c.typ, 3), reg(c.typ, 1), reg(c.typ, 2))}, lower: true,
		})
	}
	for _, c := range []struct {
		code instr.Opcode
		typ  ssa.Type
		cond uint8
	}{
		{instr.I32_EQ, i32, target.CondEQ}, {instr.I32_NE, i32, target.CondNE},
		{instr.I32_LT_S, i32, target.CondLT}, {instr.I32_LT_U, i32, target.CondCC},
		{instr.I32_GT_S, i32, target.CondGT}, {instr.I32_GT_U, i32, target.CondHI},
		{instr.I32_LE_S, i32, target.CondLE}, {instr.I32_LE_U, i32, target.CondLS},
		{instr.I32_GE_S, i32, target.CondGE}, {instr.I32_GE_U, i32, target.CondCS},
		{instr.I64_EQ, i64, target.CondEQ}, {instr.I64_NE, i64, target.CondNE},
		{instr.I64_LT_S, i64, target.CondLT}, {instr.I64_LT_U, i64, target.CondCC},
		{instr.I64_GT_S, i64, target.CondGT}, {instr.I64_GT_U, i64, target.CondHI},
		{instr.I64_LE_S, i64, target.CondLE}, {instr.I64_LE_U, i64, target.CondLS},
		{instr.I64_GE_S, i64, target.CondGE}, {instr.I64_GE_U, i64, target.CondCS},
	} {
		tests = append(tests, test{
			name: instr.TypeOf(c.code).Mnemonic,
			regs: regs{1: c.typ, 2: c.typ, 3: ssa.TypeI1},
			op:   exec(c.code, 1, 2),
			rows: []asm.Instruction{target.CMP(reg(c.typ, 1), reg(c.typ, 2)), target.CSET(reg(ssa.TypeI1, 3), c.cond)}, lower: true,
		})
	}
	flag := reg(ssa.TypeI1, 3)
	for _, c := range []struct {
		code instr.Opcode
		typ  ssa.Type
		rows []asm.Instruction
	}{
		{instr.F32_EQ, f32, []asm.Instruction{
			target.FCMP(reg(f32, 1), reg(f32, 2)), target.CSET(flag, target.CondEQ), target.CSETM(target.W16, target.CondVS), target.BIC(flag, flag, target.W16),
		}},
		{instr.F32_NE, f32, []asm.Instruction{
			target.FCMP(reg(f32, 1), reg(f32, 2)), target.CSET(flag, target.CondNE), target.CSET(target.W16, target.CondVS), target.ORR(flag, flag, target.W16),
		}},
		{instr.F32_LT, f32, []asm.Instruction{target.FCMP(reg(f32, 1), reg(f32, 2)), target.CSET(flag, target.CondMI)}},
		{instr.F32_GT, f32, []asm.Instruction{target.FCMP(reg(f32, 1), reg(f32, 2)), target.CSET(flag, target.CondGT)}},
		{instr.F32_LE, f32, []asm.Instruction{
			target.FCMP(reg(f32, 1), reg(f32, 2)), target.CSET(flag, target.CondLS), target.CSETM(target.W16, target.CondVS), target.BIC(flag, flag, target.W16),
		}},
		{instr.F32_GE, f32, []asm.Instruction{target.FCMP(reg(f32, 1), reg(f32, 2)), target.CSET(flag, target.CondGE)}},
		{instr.F64_EQ, f64, []asm.Instruction{
			target.FCMP(reg(f64, 1), reg(f64, 2)), target.CSET(flag, target.CondEQ), target.CSETM(target.W16, target.CondVS), target.BIC(flag, flag, target.W16),
		}},
		{instr.F64_NE, f64, []asm.Instruction{
			target.FCMP(reg(f64, 1), reg(f64, 2)), target.CSET(flag, target.CondNE), target.CSET(target.W16, target.CondVS), target.ORR(flag, flag, target.W16),
		}},
		{instr.F64_LT, f64, []asm.Instruction{target.FCMP(reg(f64, 1), reg(f64, 2)), target.CSET(flag, target.CondMI)}},
		{instr.F64_GT, f64, []asm.Instruction{target.FCMP(reg(f64, 1), reg(f64, 2)), target.CSET(flag, target.CondGT)}},
		{instr.F64_LE, f64, []asm.Instruction{
			target.FCMP(reg(f64, 1), reg(f64, 2)), target.CSET(flag, target.CondLS), target.CSETM(target.W16, target.CondVS), target.BIC(flag, flag, target.W16),
		}},
		{instr.F64_GE, f64, []asm.Instruction{target.FCMP(reg(f64, 1), reg(f64, 2)), target.CSET(flag, target.CondGE)}},
	} {
		tests = append(tests, test{name: instr.TypeOf(c.code).Mnemonic, regs: regs{1: c.typ, 2: c.typ, 3: ssa.TypeI1}, op: exec(c.code, 1, 2), rows: c.rows, lower: true})
	}
	for _, c := range []struct {
		code     instr.Opcode
		from, to ssa.Type
		emit     emit2
	}{
		{instr.I32_EXTEND8_S, i32, i32, target.SXTB}, {instr.I32_EXTEND16_S, i32, i32, target.SXTH},
		{instr.I64_EXTEND8_S, i64, i64, target.SXTB}, {instr.I64_EXTEND16_S, i64, i64, target.SXTH},
		{instr.I64_EXTEND32_S, i64, i64, target.SXTW},
		{instr.F32_ABS, f32, f32, target.FABS}, {instr.F32_NEG, f32, f32, target.FNEG}, {instr.F32_SQRT, f32, f32, target.FSQRT},
		{instr.F32_CEIL, f32, f32, target.FRINTP}, {instr.F32_FLOOR, f32, f32, target.FRINTM},
		{instr.F32_TRUNC, f32, f32, target.FRINTZ}, {instr.F32_NEAREST, f32, f32, target.FRINTN},
		{instr.F64_ABS, f64, f64, target.FABS}, {instr.F64_NEG, f64, f64, target.FNEG}, {instr.F64_SQRT, f64, f64, target.FSQRT},
		{instr.F64_CEIL, f64, f64, target.FRINTP}, {instr.F64_FLOOR, f64, f64, target.FRINTM},
		{instr.F64_TRUNC, f64, f64, target.FRINTZ}, {instr.F64_NEAREST, f64, f64, target.FRINTN},
		{instr.I32_TO_I64_S, i32, i64, target.SXTW}, {instr.I32_TO_I64_U, i32, i64, target.UXTW},
		{instr.I64_TO_I32, i64, i32, target.MOVW},
		{instr.I32_TO_F32_S, i32, f32, target.SCVTF}, {instr.I32_TO_F32_U, i32, f32, target.UCVTF},
		{instr.I32_TO_F64_S, i32, f64, target.SCVTF}, {instr.I32_TO_F64_U, i32, f64, target.UCVTF},
		{instr.I64_TO_F32_S, i64, f32, target.SCVTF}, {instr.I64_TO_F32_U, i64, f32, target.UCVTF},
		{instr.I64_TO_F64_S, i64, f64, target.SCVTF}, {instr.I64_TO_F64_U, i64, f64, target.UCVTF},
		{instr.F32_TO_I32_S, f32, i32, target.FCVTZS}, {instr.F32_TO_I32_U, f32, i32, target.FCVTZU},
		{instr.F32_TO_I64_S, f32, i64, target.FCVTZS}, {instr.F32_TO_I64_U, f32, i64, target.FCVTZU},
		{instr.F64_TO_I32_S, f64, i32, target.FCVTZS}, {instr.F64_TO_I32_U, f64, i32, target.FCVTZU},
		{instr.F64_TO_I64_S, f64, i64, target.FCVTZS}, {instr.F64_TO_I64_U, f64, i64, target.FCVTZU},
		{instr.F32_TO_F64, f32, f64, target.FCVT}, {instr.F64_TO_F32, f64, f32, target.FCVT},
		{instr.I32_REINTERPRET_F32, f32, i32, target.FMOV}, {instr.F32_REINTERPRET_I32, i32, f32, target.FMOV},
		{instr.I64_REINTERPRET_F64, f64, i64, target.FMOV}, {instr.F64_REINTERPRET_I64, i64, f64, target.FMOV},
	} {
		tests = append(tests, test{
			name: instr.TypeOf(c.code).Mnemonic,
			regs: regs{1: c.from, 2: c.to},
			op:   exec(c.code, 1),
			rows: []asm.Instruction{c.emit(reg(c.to, 2), reg(c.from, 1))}, lower: true,
		})
	}
	for _, c := range []struct {
		code instr.Opcode
		typ  ssa.Type
	}{{instr.I32_EQZ, i32}, {instr.I64_EQZ, i64}} {
		tests = append(tests, test{
			name: instr.TypeOf(c.code).Mnemonic,
			regs: regs{1: c.typ, 2: ssa.TypeI1},
			op:   exec(c.code, 1),
			rows: []asm.Instruction{target.CMPI(reg(c.typ, 1), 0), target.CSET(reg(ssa.TypeI1, 2), target.CondEQ)}, lower: true,
		})
	}

	x := func(v ssa.Value) asm.VReg { return reg(i64, v) }
	w := func(v ssa.Value) asm.VReg { return reg(i32, v) }
	tests = append(tests, []test{
		{
			name: "i64.div_s fails on a zero divisor",
			regs: regs{1: i64, 2: i64, 3: i64},
			op:   exec(instr.I64_DIV_S, 1, 2),
			rows: []asm.Instruction{target.CBZLabel(x(2), exit), target.SDIV(x(3), x(1), x(2))}, lower: true,
		},
		{
			name: "i32.div_u fails on a zero divisor",
			regs: regs{1: i32, 2: i32, 3: i32},
			op:   exec(instr.I32_DIV_U, 1, 2),
			rows: []asm.Instruction{target.CBZLabel(w(2), exit), target.UDIV(w(3), w(1), w(2))}, lower: true,
		},
		{
			name: "i32.rem_u subtracts the quotient",
			regs: regs{1: i32, 2: i32, 3: i32},
			op:   exec(instr.I32_REM_U, 1, 2),
			rows: []asm.Instruction{
				target.CBZLabel(w(2), exit), target.UDIV(target.W16, w(1), w(2)), target.MSUB(w(3), target.W16, w(2), w(1)),
			}, lower: true,
		},
		{
			name: "i64.rem_s subtracts the quotient",
			regs: regs{1: i64, 2: i64, 3: i64},
			op:   exec(instr.I64_REM_S, 1, 2),
			rows: []asm.Instruction{
				target.CBZLabel(x(2), exit), target.SDIV(target.X16, x(1), x(2)), target.MSUB(x(3), target.X16, x(2), x(1)),
			}, lower: true,
		},
		{
			name: "select takes the first operand on a nonzero condition",
			regs: regs{1: i64, 2: i64, 3: i32, 4: i64},
			op:   exec(instr.SELECT, 1, 2, 3),
			rows: []asm.Instruction{target.CMPI(w(3), 0), target.CSEL(x(4), x(1), x(2), target.CondNE)}, lower: true,
		},
		{
			name: "select chooses between float registers",
			regs: regs{1: f64, 2: f64, 3: i32, 4: f64},
			op:   exec(instr.SELECT, 1, 2, 3),
			rows: []asm.Instruction{target.CMPI(w(3), 0), target.FCSEL(reg(f64, 4), reg(f64, 1), reg(f64, 2), target.CondNE)}, lower: true,
		},
		{
			name: "const i1 is its truth value",
			regs: regs{1: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpConst, Const: types.BoxI1(true), Results: []ssa.Value{1}},
			rows: target.LDI(w(1), 1), lower: true,
		},
		{
			name: "const i8 is its sign-extended lane",
			regs: regs{1: ssa.TypeI8},
			op:   ssa.Operation{Op: ssa.OpConst, Const: types.BoxI8(-1), Results: []ssa.Value{1}},
			rows: target.LDI(w(1), 0xffffffff), lower: true,
		},
		{
			name: "const i64 is its value",
			regs: regs{1: i64},
			op:   ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(-3), Results: []ssa.Value{1}},
			rows: target.LDI(x(1), uint64(0xfffffffffffffffd)), lower: true,
		},
		{
			name: "const f32 moves its bits through scratch",
			regs: regs{1: f32},
			op:   ssa.Operation{Op: ssa.OpConst, Const: types.BoxF32(1.5), Results: []ssa.Value{1}},
			rows: append(target.LDI(target.X16, uint64(math.Float32bits(1.5))), target.FMOV(reg(f32, 1), target.W16)), lower: true,
		},
		{
			name: "const f64 moves its bits through scratch",
			regs: regs{1: f64},
			op:   ssa.Operation{Op: ssa.OpConst, Const: types.BoxF64(1.5), Results: []ssa.Value{1}},
			rows: append(target.LDI(target.X16, math.Float64bits(1.5)), target.FMOV(reg(f64, 1), target.X16)), lower: true,
		},
		{
			name: "const ref is its boxed word",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpConst, Const: types.BoxRef(4), Results: []ssa.Value{1}},
			rows: target.LDI(x(1), uint64(types.BoxRef(4))), lower: true,
		},
		{
			name: "load i64 keeps the slot word for its guard",
			regs: regs{1: i64},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: local(2), Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(x(1), target.X25, 16)}, lower: true,
		},
		{
			name: "guard.kind unboxes an inline i64 or a heap-promoted one, deopts on a ref to another object",
			regs: regs{1: i64, 2: i64},
			op:   ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: slices.Concat(
				[]asm.Instruction{
					target.LSRI(target.X16, x(1), 49),
					target.TSTI(target.X16, 1),
					target.BCondLabel(target.OpBEQ, 2),
				},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, 2),
					target.SBFX(heap, x(1), 0, 32),
					target.LSLI(heap, heap, 4),
					target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)),
					target.ADD(heap, target.X16, heap),
					target.LDR(target.X16, heap, 0),
				},
				target.LDI(target.X17, uint64(jit.Itab(types.I64(0)))),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, exit),
					target.LDR(x(2), heap, int16(jit.OffsetData)),
					target.LDR(x(2), x(2), 0),
					target.BLabel(3),
					target.SBFX(x(2), x(1), 0, 49),
				},
			), lower: true,
		},
		{
			name: "retain counts any reference up",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{target.LSRI(target.X16, x(1), 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, 2),
					target.SBFX(target.X17, x(1), 0, 32),
				},
				counter,
				[]asm.Instruction{target.ADDI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0)},
			), lower: true,
		},
		{
			name: "release counts a non-null reference down and exits on the last",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{target.LSRI(target.X16, x(1), 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, x(1), 0, 32),
					target.CBZLabel(target.X17, resume),
				},
				counter,
				[]asm.Instruction{
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
				},
			), lower: true,
		},
		{
			name: "load f32 reads the low word",
			regs: regs{1: f32},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: local(2), Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(reg(f32, 1), target.X25, 16)}, lower: true,
		},
		{
			name: "load ref keeps its boxed word",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: local(2), Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(x(1), target.X25, 16)}, lower: true,
		},
		{
			name: "load global addresses the globals base",
			regs: regs{1: i32},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 2}, Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(globals, target.Ctx, int16(jit.OffsetGlobals)), target.LDR(w(1), globals, 16)}, lower: true,
		},
		{
			name: "store i8 tags its lane",
			regs: regs{1: ssa.TypeI8},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(2), Args: []ssa.Value{1}},
			rows: append(boxed(types.KindI8, target.UXTW(target.X16, w(1))), target.STR(target.X16, target.X25, 16)), lower: true,
		},
		{
			// The inline (in-range) path is row-identical to a narrow
			// store: the CBNZ target is the only difference from before
			// resumable boxing (a Box exit instead of a deopt).
			name: "store i64 boxes outside the inline range",
			regs: regs{1: i64},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(1), Args: []ssa.Value{1}},
			rows: append(boxed(types.KindI64, append(target.LDI(target.X16, 1<<48),
				target.ADD(target.X17, x(1), target.X16),
				target.LSRI(target.X17, target.X17, 49),
				target.CBNZLabel(target.X17, exit),
				target.ANDI(target.X16, x(1), types.VMask),
			)...), target.STR(target.X16, target.X25, 8)), lower: true,
		},
		{
			name: "store f32 tags its bits",
			regs: regs{1: f32},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(1), Args: []ssa.Value{1}},
			rows: append(boxed(types.KindF32, target.FMOV(target.W16, reg(f32, 1))), target.STR(target.X16, target.X25, 8)), lower: true,
		},
		{
			name: "store f64 writes its bits",
			regs: regs{1: f64},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(1), Args: []ssa.Value{1}},
			rows: []asm.Instruction{target.STR(reg(f64, 1), target.X25, 8)}, lower: true,
		},
		{
			name: "store global addresses the globals base",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 1}, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{
					target.LDR(globals, target.Ctx, int16(jit.OffsetGlobals)),
					target.LDR(old, globals, 8),
					target.LSRI(target.X16, old, 49),
				},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, old, 0, 32),
					target.CBZLabel(target.X17, resume),
				},
				counter,
				[]asm.Instruction{
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
					target.STR(x(1), globals, 8),
				},
			), lower: true,
		},
		{
			name: "declines an upvalue slot",
			regs: regs{1: i32},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceUpval}, Results: []ssa.Value{1}},
		},
		{
			name: "declines an opcode without a lowering",
			regs: regs{1: ssa.TypeRef, 2: i32, 3: ssa.TypeRef},
			op:   exec(instr.MAP_GET, 1, 2),
		},
	}...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, a := arm64.New(), asm.New(target.New())
			m.Prologue(a, nil, 0, true, 0, nil, nil)
			start := len(a.Rows())
			require.Equal(t, tt.lower, m.Lower(a, tt.op, tt.regs))
			if tt.lower {
				require.Equal(t, tt.rows, a.Rows()[start:])
			}
		})
	}
}

func TestMachine_Branch(t *testing.T) {
	r := regs{1: ssa.TypeI32}
	index := r.Reg(1)

	t.Run("takes the first label on nonzero", func(t *testing.T) {
		a := asm.New(target.New())
		yes, no := a.Label(), a.Label()
		arm64.New().Branch(a, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{1}}, r, []asm.Label{yes, no})
		require.Equal(t, []asm.Instruction{target.CBNZLabel(index, yes), target.BLabel(no)}, a.Rows())
	})

	t.Run("takes the last label for an index out of range", func(t *testing.T) {
		a := asm.New(target.New())
		first, second, rest := a.Label(), a.Label(), a.Label()
		arm64.New().Branch(a, ssa.Terminator{Op: ssa.OpTable, Args: []ssa.Value{1}}, r, []asm.Label{first, second, rest})
		require.Equal(t, []asm.Instruction{
			target.CMPI(index, 0), target.BCondLabel(target.OpBEQ, first),
			target.CMPI(index, 1), target.BCondLabel(target.OpBEQ, second),
			target.BLabel(rest),
		}, a.Rows())
	})
}

func TestMachine_Return(t *testing.T) {
	r := regs{1: ssa.TypeI32}
	rows := func(slot int16) []asm.Instruction {
		rows := []asm.Instruction{target.UXTW(target.X16, r.Reg(1))}
		rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI32))...)
		return append(rows, target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, target.X25, slot), target.BLabel(0))
	}
	scalars := []types.Kind{types.KindI32, types.KindF64}

	t.Run("stores results from slot zero", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, scalars, 0, true, 0, nil, nil)
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{1}}, r)
		require.Equal(t, rows(0), a.Rows()[start:])
	})

	t.Run("stores completed operands past the locals", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, scalars, 0, true, 0, nil, nil)
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Op: ssa.OpComplete, Args: []ssa.Value{1}}, r)
		require.Equal(t, rows(16), a.Rows()[start:])
	})

	t.Run("releases and clears every slot that can hold a reference first", func(t *testing.T) {
		sweep := func(slot int16, word asm.VReg) []asm.Instruction {
			return slices.Concat(
				[]asm.Instruction{target.LDR(word, target.X25, slot), target.LSRI(target.X16, word, 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, word, 0, 32),
					target.CBZLabel(target.X17, resume),
					target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
					target.LSLI(target.X17, target.X17, 3),
					target.ADD(target.X16, target.X16, target.X17),
					target.LDR(target.X17, target.X16, 0),
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
					target.STR(target.XZR, target.X25, slot),
				},
			)
		}

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, []types.Kind{types.KindI32, types.KindRef, types.KindI8, types.KindI64}, 1, true, 0, nil, nil)
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{1}}, r)
		require.Equal(t, slices.Concat(
			sweep(8, asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)),
			sweep(24, asm.NewVReg(-3, asm.RegTypeInt, asm.Width64)),
			rows(0),
		), a.Rows()[start:])
	})

	t.Run("moves one register-convention result to X0 instead of boxing it", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, scalars, 0, true, 0, nil, []types.Kind{types.KindI32})
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{1}}, r)
		require.Equal(t, []asm.Instruction{
			target.MOV(target.W0, r.Reg(1)),
			target.USE(target.X0),
			target.BLabel(0),
		}, a.Rows()[start:])
	})

	t.Run("moves two register-convention results to X0 and X1, a float by FMOV", func(t *testing.T) {
		r2 := regs{1: ssa.TypeF64, 2: ssa.TypeRef}
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, scalars, 0, true, 0, nil, []types.Kind{types.KindF64, types.KindRef})
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{1, 2}}, r2)
		require.Equal(t, []asm.Instruction{
			target.FMOV(target.X0, r2.Reg(1)),
			target.MOV(target.X1, r2.Reg(2)),
			target.USE(target.X0),
			target.USE(target.X1),
			target.BLabel(0),
		}, a.Rows()[start:])
	})

	t.Run("keeps OpComplete boxing to slots even under the register convention", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, scalars, 0, true, 0, nil, []types.Kind{types.KindI32})
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Op: ssa.OpComplete, Args: []ssa.Value{1}}, r)
		require.Equal(t, rows(16), a.Rows()[start:])
	})
}

func TestMachine_Call(t *testing.T) {
	r := regs{1: ssa.TypeI32, 2: ssa.TypeRef, 3: ssa.TypeF64}
	live := asm.NewVReg(9, asm.RegTypeInt, asm.Width32)
	record := func(field uintptr) int16 { return int16(jit.OffsetRecords - unsafe.Sizeof(jit.Record{}) + field) }

	t.Run("calls through the natives table and bridges when it cannot", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		bridge, join := a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Address: 5, Callee: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Bridge: bridge, Resume: join, Owned: true,
		}, r))

		code := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
		callee := r.Reg(2)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.LDR(code, target.Ctx, int16(jit.OffsetNatives)),
			},
			target.LDI(target.X16, 5),
			[]asm.Instruction{
				target.LDRR(code, code, target.X16),
				target.CBZLabel(code, bridge),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.LSLI(target.X16, target.X27, 5),
				target.ADD(target.X16, target.Ctx, target.X16),
				target.ADDI(target.X17, target.SP, 0),
				target.STR(target.X17, target.X16, record(jit.RecordSP)),
			},
			target.LDI(target.X17, 7),
			[]asm.Instruction{
				target.STR(target.X17, target.X16, record(jit.RecordExit)),
				target.ADDI(target.X25, target.X25, 32),
				target.BLR(code),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.LSRI(target.X16, callee, 49),
			},
			target.LDI(target.X17, types.Tag(types.KindRef)>>49),
			[]asm.Instruction{
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBNE, resume),
				target.SBFX(target.X17, callee, 0, 32),
				target.CBZLabel(target.X17, resume),
				target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
				target.LSLI(target.X17, target.X17, 3),
				target.ADD(target.X16, target.X16, target.X17),
				target.LDR(target.X17, target.X16, 0),
				target.CMPI(target.X17, 1),
				target.BCondLabel(target.OpBLE, exit),
				target.SUBI(target.X17, target.X17, 1),
				target.STR(target.X17, target.X16, 0),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("borrows a callee it does not own", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		bridge, join := a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Address: 5, Callee: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Bridge: bridge, Resume: join,
		}, r))

		code := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.LDR(code, target.Ctx, int16(jit.OffsetNatives)),
			},
			target.LDI(target.X16, 5),
			[]asm.Instruction{
				target.LDRR(code, code, target.X16),
				target.CBZLabel(code, bridge),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.LSLI(target.X16, target.X27, 5),
				target.ADD(target.X16, target.Ctx, target.X16),
				target.ADDI(target.X17, target.SP, 0),
				target.STR(target.X17, target.X16, record(jit.RecordSP)),
			},
			target.LDI(target.X17, 7),
			[]asm.Instruction{
				target.STR(target.X17, target.X16, record(jit.RecordExit)),
				target.ADDI(target.X25, target.X25, 32),
				target.BLR(code),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("reads a register-convention result from X0 instead of the slot", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		bridge, join := a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Address: 5, Callee: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Bridge: bridge, Resume: join,
			Registers: []types.Kind{types.KindF64},
		}, r))

		code := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.LDR(code, target.Ctx, int16(jit.OffsetNatives)),
			},
			target.LDI(target.X16, 5),
			[]asm.Instruction{
				target.LDRR(code, code, target.X16),
				target.CBZLabel(code, bridge),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.LSLI(target.X16, target.X27, 5),
				target.ADD(target.X16, target.Ctx, target.X16),
				target.ADDI(target.X17, target.SP, 0),
				target.STR(target.X17, target.X16, record(jit.RecordSP)),
			},
			target.LDI(target.X17, 7),
			[]asm.Instruction{
				target.STR(target.X17, target.X16, record(jit.RecordExit)),
				target.ADDI(target.X25, target.X25, 32),
				target.BLR(code),
				target.DEF(target.X0),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.FMOV(r.Reg(3), target.X0),
			},
		), a.Rows()[start:])
	})

	t.Run("calls its own entry directly when it is a self call", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		bridge, join := a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Address: 5, Callee: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Bridge: bridge, Resume: join, Owned: true, Self: true,
		}, r))

		// entry is Prologue's own label, bound before any other row: the
		// second label a fresh Assembler allocates (end is the first).
		entry := asm.Label(1)
		callee := r.Reg(2)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.LSLI(target.X16, target.X27, 5),
				target.ADD(target.X16, target.Ctx, target.X16),
				target.ADDI(target.X17, target.SP, 0),
				target.STR(target.X17, target.X16, record(jit.RecordSP)),
			},
			target.LDI(target.X17, 7),
			[]asm.Instruction{
				target.STR(target.X17, target.X16, record(jit.RecordExit)),
				target.ADDI(target.X25, target.X25, 32),
				target.BLLabel(entry),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.LSRI(target.X16, callee, 49),
			},
			target.LDI(target.X17, types.Tag(types.KindRef)>>49),
			[]asm.Instruction{
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBNE, resume),
				target.SBFX(target.X17, callee, 0, 32),
				target.CBZLabel(target.X17, resume),
				target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
				target.LSLI(target.X17, target.X17, 3),
				target.ADD(target.X16, target.X16, target.X17),
				target.LDR(target.X17, target.X16, 0),
				target.CMPI(target.X17, 1),
				target.BCondLabel(target.OpBLE, exit),
				target.SUBI(target.X17, target.X17, 1),
				target.STR(target.X17, target.X16, 0),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("moves register-passed arguments into X0/X1 on top of their boxed slot store", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		bridge, join := a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Address: 5, Callee: 2, Args: []ssa.Value{1, 3}, Base: 4, Size: 3, Exit: 7,
			Bridge: bridge, Resume: join, Self: true,
			Arguments: []types.Kind{types.KindI32, types.KindF64},
		}, r))

		// entry is Prologue's own label, bound before any other row: the
		// second label a fresh Assembler allocates (end is the first).
		entry := asm.Label(1)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.STR(r.Reg(3), target.X25, 40),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.LSLI(target.X16, target.X27, 5),
				target.ADD(target.X16, target.Ctx, target.X16),
				target.ADDI(target.X17, target.SP, 0),
				target.STR(target.X17, target.X16, record(jit.RecordSP)),
			},
			target.LDI(target.X17, 7),
			[]asm.Instruction{
				target.STR(target.X17, target.X16, record(jit.RecordExit)),
				target.ADDI(target.X25, target.X25, 32),
				target.MOV(asm.NewPReg(target.X0.ID(), asm.RegTypeInt, asm.Width32), r.Reg(1)),
				target.FMOV(target.X1, r.Reg(3)),
				target.USE(target.X0),
				target.USE(target.X1),
				target.BLLabel(entry),
				target.SUBI(target.X25, target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("moves a register-passed i64 argument by a raw 64-bit MOV, on top of its boxed slot store", func(t *testing.T) {
		r64 := regs{1: ssa.TypeI64}
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		bridge, join := a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Address: 5, Callee: 2, Args: []ssa.Value{1}, Base: 4, Size: 3, Exit: 7,
			Bridge: bridge, Resume: join, Self: true,
			Arguments: []types.Kind{types.KindI64},
		}, r64))

		// entry is Prologue's own label, bound before any other row: the
		// second label a fresh Assembler allocates (end is the first).
		entry := asm.Label(1)
		require.Equal(t, slices.Concat(
			target.LDI(target.X16, 1<<48),
			[]asm.Instruction{
				target.ADD(target.X17, r64.Reg(1), target.X16),
				target.LSRI(target.X17, target.X17, 49),
				target.CBNZLabel(target.X17, exit),
				target.ANDI(target.X16, r64.Reg(1), types.VMask),
			},
			target.LDI(target.X17, types.Tag(types.KindI64)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.LSLI(target.X16, target.X27, 5),
				target.ADD(target.X16, target.Ctx, target.X16),
				target.ADDI(target.X17, target.SP, 0),
				target.STR(target.X17, target.X16, record(jit.RecordSP)),
			},
			target.LDI(target.X17, 7),
			[]asm.Instruction{
				target.STR(target.X17, target.X16, record(jit.RecordExit)),
				target.ADDI(target.X25, target.X25, 32),
				target.MOV(target.X0, r64.Reg(1)),
				target.USE(target.X0),
				target.BLLabel(entry),
				target.SUBI(target.X25, target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("declines a frame beyond the reach of an immediate", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, nil, 0, true, 0, nil, nil)
		require.False(t, m.Call(a, compile.Call{Address: 5, Callee: 2, Base: 510, Size: 2}, r))
	})
}

func TestMachine_Budget(t *testing.T) {
	m, a := arm64.New(), asm.New(target.New())
	m.Prologue(a, nil, 0, true, 0, nil, nil)
	start := len(a.Rows())
	m.Budget(a, exit)
	require.Equal(t, []asm.Instruction{
		target.SUBSI(target.X24, target.X24, 1),
		target.BCondLabel(target.OpBLE, exit),
	}, a.Rows()[start:])
}

func TestMachine_Exit(t *testing.T) {
	uses := []asm.VReg{asm.NewVReg(1, asm.RegTypeInt, asm.Width64), asm.NewVReg(2, asm.RegTypeFloat, asm.Width64)}
	rows := func(id uint64, trap jit.Trap) []asm.Instruction {
		return slices.Concat(
			[]asm.Instruction{
				target.STR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
				target.STR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
			},
			target.LDI(target.X16, id),
			[]asm.Instruction{target.STR(target.X16, target.Ctx, int16(jit.OffsetExit))},
			target.LDI(target.X16, uint64(trap)),
			[]asm.Instruction{
				target.STR(target.X16, target.Ctx, int16(jit.OffsetTrap)),
				target.LDR(target.X16, target.Ctx, int16(asm.OffsetStub)),
				target.EXIT(target.X16),
				target.USE(uses[0]),
				target.USE(uses[1]),
			},
		)
	}

	t.Run("suspends a resumable exit", func(t *testing.T) {
		a := asm.New(target.New())
		arm64.New().Exit(a, 3, jit.ExitBridge, uses)
		require.Equal(t, append(rows(3, jit.TrapBridge), target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget))), a.Rows())
	})

	t.Run("never returns from a deopt", func(t *testing.T) {
		a := asm.New(target.New())
		arm64.New().Exit(a, 4, jit.ExitDeopt, uses)
		require.Equal(t, append(rows(4, jit.TrapDeopt), target.BRK(0)), a.Rows())
	})
}

func TestMachine_Results(t *testing.T) {
	a := asm.New(target.New())
	w := asm.NewVReg(1, asm.RegTypeInt, asm.Width32)
	d := asm.NewVReg(2, asm.RegTypeFloat, asm.Width64)
	arm64.New().Results(a, []asm.VReg{w, d})
	require.Equal(t, []asm.Instruction{
		target.LDR(w, target.Ctx, int16(jit.OffsetResults)),
		target.LDR(d, target.Ctx, int16(jit.OffsetResults)+8),
	}, a.Rows())
}

func TestMachine_Move(t *testing.T) {
	a := asm.New(target.New())
	m := arm64.New()
	w1, w2 := asm.NewVReg(1, asm.RegTypeInt, asm.Width32), asm.NewVReg(2, asm.RegTypeInt, asm.Width32)
	d1, d2 := asm.NewVReg(3, asm.RegTypeFloat, asm.Width64), asm.NewVReg(4, asm.RegTypeFloat, asm.Width64)
	m.Move(a, w1, w2)
	m.Move(a, d1, d2)
	require.Equal(t, []asm.Instruction{target.MOV(w1, w2), target.FMOV(d1, d2)}, a.Rows())
}

func TestMachine_Const(t *testing.T) {
	w := asm.NewVReg(1, asm.RegTypeInt, asm.Width32)
	x := asm.NewVReg(2, asm.RegTypeInt, asm.Width64)
	for _, c := range []struct {
		name string
		dst  asm.VReg
		c    types.Boxed
		rows []asm.Instruction
	}{
		{"i1", w, types.BoxI1(true), target.LDI(w, 1)},
		{"i8", w, types.BoxI8(-2), target.LDI(w, uint64(uint32(0xFFFFFFFE)))},
		{"i32", w, types.BoxI32(-7), target.LDI(w, uint64(uint32(0xFFFFFFF9)))},
		{"i64", x, types.BoxI64(1 << 40), target.LDI(x, 1<<40)},
		{"ref", x, types.BoxRef(3), target.LDI(x, uint64(types.BoxRef(3)))},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := asm.New(target.New())
			require.True(t, arm64.New().Const(a, c.dst, c.c))
			require.Equal(t, c.rows, a.Rows())
		})
	}
	t.Run("declines a float", func(t *testing.T) {
		a := asm.New(target.New())
		require.False(t, arm64.New().Const(a, x, types.BoxF64(1.5)))
		require.Empty(t, a.Rows())
	})
}
