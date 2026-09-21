package arm64_test

import (
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

type regs struct {
	values map[ssa.Value]asm.VReg
	types  map[ssa.Value]ssa.Type
}

func (r regs) Reg(v ssa.Value) asm.VReg  { return r.values[v] }
func (r regs) Type(v ssa.Value) ssa.Type { return r.types[v] }

func vr(id int32, typ asm.RegType, width asm.RegWidth) asm.VReg {
	return asm.NewVReg(id, typ, width)
}
func TestNew(t *testing.T) {
	m := arm64.New()
	require.NotNil(t, m)
	require.Equal(t, []asm.PReg{target.X16, target.X17, target.X25}, m.Reserve())
}

func TestLower(t *testing.T) {
	t.Run("float", lowerFloat)
	t.Run("arithmetic", lowerArithmetic)
	t.Run("divide", lowerDivide)
	t.Run("compare", lowerCompare)
	t.Run("select", lowerSelect)
	t.Run("branch", lowerBranch)
	t.Run("table", lowerTable)
	t.Run("global", lowerGlobal)
	t.Run("global store", lowerGlobalStore)
	t.Run("load", lowerLoad)
	t.Run("store64", lowerStore64)
	t.Run("reinterpret", lowerReinterpret)
	t.Run("convert", lowerConvert)
	t.Run("store", lowerStore)
	t.Run("prologue", lowerPrologue)
	t.Run("complete", lowerComplete)
	t.Run("budget", lowerBudget)
	t.Run("parameter", lowerParameterReturn)
	t.Run("execute", lowerExecute)
	t.Run("floatcompare", lowerFloatCompare)
}

func lowerFloatCompare(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeFloat, asm.Width64),
			2: vr(2, asm.RegTypeFloat, asm.Width64),
			3: vr(3, asm.RegTypeInt, asm.Width32),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeF64, 2: ssa.TypeF64, 3: ssa.TypeI1},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpExec, Code: instr.F64_NE, Args: []ssa.Value{1, 2}, Results: []ssa.Value{3}}, r))
	require.Equal(t, []asm.Instruction{target.FCMP(r.values[1], r.values[2]), target.CSET(r.values[3], target.CondNE)}, a.Rows())
}

func lowerArithmetic(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeInt, asm.Width32),
			2: vr(2, asm.RegTypeInt, asm.Width32),
			3: vr(3, asm.RegTypeInt, asm.Width32),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeI32, 2: ssa.TypeI32, 3: ssa.TypeI32},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{
		Op: ssa.OpExec, Code: instr.I32_ADD,
		Args: []ssa.Value{1, 2}, Results: []ssa.Value{3},
	}, r))
	require.Equal(t, []asm.Instruction{target.ADD(r.values[3], r.values[1], r.values[2])}, a.Rows())
}

func lowerDivide(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeInt, asm.Width32),
			2: vr(2, asm.RegTypeInt, asm.Width32),
			3: vr(3, asm.RegTypeInt, asm.Width32),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeI32, 2: ssa.TypeI32, 3: ssa.TypeI32},
	}
	a := asm.New(target.New())
	a.Label()
	deopt := asm.Label(1)
	m := arm64.New()
	require.True(t, m.Lower(a, ssa.Operation{
		Op: ssa.OpExec, Code: instr.I32_DIV_S,
		Args: []ssa.Value{1, 2}, Results: []ssa.Value{3},
	}, r))
	want := []asm.Instruction{target.CBZLabel(r.values[2], deopt), target.SDIV(r.values[3], r.values[1], r.values[2])}
	require.Equal(t, want, a.Rows())
}

func lowerCompare(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeInt, asm.Width32),
			2: vr(2, asm.RegTypeInt, asm.Width32),
			3: vr(3, asm.RegTypeInt, asm.Width32),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeI32, 2: ssa.TypeI32, 3: ssa.TypeI1},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{
		Op: ssa.OpExec, Code: instr.I32_LT_U,
		Args: []ssa.Value{1, 2}, Results: []ssa.Value{3},
	}, r))
	require.Equal(t, []asm.Instruction{
		target.CMP(r.values[1], r.values[2]),
		target.CSET(r.values[3], target.CondCC),
	}, a.Rows())
}
func lowerFloat(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeFloat, asm.Width64),
			2: vr(2, asm.RegTypeFloat, asm.Width64),
			3: vr(3, asm.RegTypeFloat, asm.Width64),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeF64, 2: ssa.TypeF64, 3: ssa.TypeF64},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{
		Op: ssa.OpExec, Code: instr.F64_ADD,
		Args: []ssa.Value{1, 2}, Results: []ssa.Value{3},
	}, r))
	require.Equal(t, []asm.Instruction{
		target.FADD(r.values[3], r.values[1], r.values[2]),
	}, a.Rows())
}

func lowerSelect(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeInt, asm.Width32),
			2: vr(2, asm.RegTypeInt, asm.Width32),
			3: vr(3, asm.RegTypeInt, asm.Width32),
			4: vr(4, asm.RegTypeInt, asm.Width32),
		},
		types: map[ssa.Value]ssa.Type{
			1: ssa.TypeI32, 2: ssa.TypeI32, 3: ssa.TypeI32, 4: ssa.TypeI32,
		},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{
		Op: ssa.OpExec, Code: instr.SELECT,
		Args: []ssa.Value{1, 2, 3}, Results: []ssa.Value{4},
	}, r))
	require.Equal(t, []asm.Instruction{
		target.CMPI(r.values[3], 0),
		target.CSEL(r.values[4], r.values[1], r.values[2], target.CondNE),
	}, a.Rows())
}
func lowerBranch(t *testing.T) {
	r := regs{values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width32)}}
	m := arm64.New()
	a := asm.New(target.New())
	trueLabel, falseLabel := a.Label(), a.Label()
	m.Branch(a, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{1}}, r, []asm.Label{trueLabel, falseLabel})
	require.Equal(t, []asm.Instruction{
		target.CBNZLabel(r.values[1], trueLabel),
		target.BLabel(falseLabel),
	}, a.Rows())
}

func lowerTable(t *testing.T) {
	r := regs{values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width32)}}
	m := arm64.New()
	a := asm.New(target.New())
	first, second, defaultLabel := a.Label(), a.Label(), a.Label()
	m.Branch(a, ssa.Terminator{Op: ssa.OpTable, Args: []ssa.Value{1}}, r, []asm.Label{first, second, defaultLabel})
	require.Equal(t, []asm.Instruction{
		target.CMPI(r.values[1], 0),
		target.BCondLabel(target.OpBEQ, first),
		target.CMPI(r.values[1], 1),
		target.BCondLabel(target.OpBEQ, second),
		target.BLabel(defaultLabel),
	}, a.Rows())
}

func lowerGlobal(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width32)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI32},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 2}, Results: []ssa.Value{1}}, r))
	base := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
	require.Equal(t, []asm.Instruction{
		target.LDR(base, target.Ctx, int16(jit.OffsetGlobals)),
		target.LDR(r.values[1], base, 16),
	}, a.Rows())
}

func lowerGlobalStore(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width32)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI32},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 2}, Args: []ssa.Value{1}}, r))
	base := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
	want := []asm.Instruction{target.LDR(base, target.Ctx, int16(jit.OffsetGlobals)), target.UXTW(target.X16, r.values[1])}
	want = append(want, target.LDI(target.X17, types.Tag(types.KindI32))...)
	want = append(want, target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, base, 16))
	require.Equal(t, want, a.Rows())
}

func lowerLoad(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width64)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI64},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{
		Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 2}, Results: []ssa.Value{1},
	}, r))
	require.Equal(t, []asm.Instruction{
		target.LDR(r.values[1], target.X25, 16),
		target.SBFX(r.values[1], r.values[1], 0, 49),
	}, a.Rows())
}

func lowerStore64(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width64)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI64},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 1}, Args: []ssa.Value{1}}, r))
	deopt := asm.Label(0)
	want := append([]asm.Instruction{}, target.LDI(target.X16, 1<<48)...)
	want = append(want,
		target.ADD(target.X17, r.values[1], target.X16),
		target.LSRI(target.X17, target.X17, 49),
		target.CBNZLabel(target.X17, deopt),
		target.ANDI(target.X16, r.values[1], uint64(types.VMask)),
	)
	want = append(want, target.LDI(target.X17, types.Tag(types.KindI64))...)
	want = append(want, target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, target.X25, 8))
	require.Equal(t, want, a.Rows())
}

func lowerReinterpret(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width64), 2: vr(2, asm.RegTypeFloat, asm.Width64)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI64, 2: ssa.TypeF64},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_REINTERPRET_F64, Args: []ssa.Value{1}, Results: []ssa.Value{2}}, r))
	require.Equal(t, []asm.Instruction{target.FMOV(r.values[2], r.values[1])}, a.Rows())
}

func lowerConvert(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width32), 2: vr(2, asm.RegTypeFloat, asm.Width64)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI32, 2: ssa.TypeF64},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_TO_F64_S, Args: []ssa.Value{1}, Results: []ssa.Value{2}}, r))
	require.Equal(t, []asm.Instruction{target.SCVTF(r.values[2], r.values[1])}, a.Rows())
}

func lowerStore(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width32)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI32},
	}
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, ssa.Operation{
		Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 2}, Args: []ssa.Value{1},
	}, r))
	want := []asm.Instruction{target.UXTW(target.X16, r.values[1])}
	want = append(want, target.LDI(target.X17, types.Tag(types.KindI32))...)
	want = append(want, target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, target.X25, 16))
	require.Equal(t, want, a.Rows())
}
func lowerPrologue(t *testing.T) {
	m := arm64.New()
	a := asm.New(target.New())
	m.Prologue(a, 3, 1)
	require.Equal(t, []asm.Instruction{
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		{Op: uint16(target.OpSUBI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
		target.ADDI(target.X17, target.Ctx, uint16(jit.OffsetRecords)),
		target.LSLI(target.X16, target.X16, 5),
		target.ADD(target.X17, target.X17, target.X16),
		target.STR(target.X25, target.X17, int16(jit.RecordFB)),
		target.ADDI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
		target.STR(target.XZR, target.X25, 8),
		target.STR(target.XZR, target.X25, 16),
	}, a.Rows())
}

func lowerComplete(t *testing.T) {
	m := arm64.New()
	a := asm.New(target.New())
	m.Prologue(a, 2, 2)
	start := len(a.Rows())
	src := vr(1, asm.RegTypeInt, asm.Width32)
	m.Complete(a, []asm.VReg{src}, []ssa.Type{ssa.TypeI32})
	want := []asm.Instruction{target.UXTW(target.X16, src)}
	want = append(want, target.LDI(target.X17, types.Tag(types.KindI32))...)
	want = append(want, target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, target.X25, 16), target.BLabel(0))
	require.Equal(t, want, a.Rows()[start:])
}

func lowerBudget(t *testing.T) {
	m := arm64.New()
	a := asm.New(target.New())
	label := a.Label()
	m.Budget(a, label)
	require.Equal(t, []asm.Instruction{
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.SUBSI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.BCondLabel(target.OpBLE, label),
	}, a.Rows())
}
func lowerParameterReturn(t *testing.T) {
	b := ssa.New("identity")
	entry := b.Block()
	param := b.Param(entry, ssa.TypeI8)
	b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{param}})
	f := b.Build()
	code, err := compile.Lower(f, arm64.New(), 1, 0)
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	address, err := asm.Link(buffer, code)
	require.NoError(t, err)
	stack := []types.Boxed{types.BoxI8(-1)}
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = uintptr(unsafe.Pointer(&stack[0]))
	require.Equal(t, jit.TrapReturn, jit.Enter(address, ctx))
	require.Equal(t, types.BoxI8(-1), stack[0])
	require.Zero(t, ctx.Depth)
}

func lowerExecute(t *testing.T) {
	b := ssa.New("muladd")
	entry := b.Block()
	state := b.Value(ssa.TypeState)
	a := b.Param(entry, ssa.TypeI32)
	c := b.Param(entry, ssa.TypeI32)
	product := b.Value(ssa.TypeI32)
	one := b.Value(ssa.TypeI32)
	result := b.Value(ssa.TypeI32)
	b.Add(entry, ssa.Operation{Op: ssa.OpState, State: state})
	b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_MUL, Args: []ssa.Value{a, c}, Results: []ssa.Value{product}, State: state})
	b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
	b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{product, one}, Results: []ssa.Value{result}, State: state})
	b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
	f := b.Build()

	code, err := compile.Lower(f, arm64.New(), 2, 0)
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	address, err := asm.Link(buffer, code)
	require.NoError(t, err)

	stack := []types.Boxed{types.BoxI32(6), types.BoxI32(7)}
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = uintptr(unsafe.Pointer(&stack[0]))

	require.Equal(t, jit.TrapReturn, jit.Enter(address, ctx))
	require.Equal(t, types.BoxI32(43), stack[0])
	require.Zero(t, ctx.Depth)
}
