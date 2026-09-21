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

func row(t *testing.T, op ssa.Operation, rs regs) (compile.Machine, *asm.Assembler) {
	t.Helper()
	m := arm64.New()
	a := asm.New(target.New())
	require.True(t, m.Lower(a, op, rs))
	return m, a
}

func vr(id int32, typ asm.RegType, width asm.RegWidth) asm.VReg {
	return asm.NewVReg(id, typ, width)
}
func TestLower_Arithmetic(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeInt, asm.Width32),
			2: vr(2, asm.RegTypeInt, asm.Width32),
			3: vr(3, asm.RegTypeInt, asm.Width32),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeI32, 2: ssa.TypeI32, 3: ssa.TypeI32},
	}
	_, a := row(t, ssa.Operation{
		Op: ssa.OpExec, Code: instr.I32_ADD,
		Args: []ssa.Value{1, 2}, Results: []ssa.Value{3},
	}, r)
	require.Equal(t, []asm.Instruction{target.ADD(r.values[3], r.values[1], r.values[2])}, a.Rows())
}

func TestLower_Divide(t *testing.T) {
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

func TestLower_Compare(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeInt, asm.Width32),
			2: vr(2, asm.RegTypeInt, asm.Width32),
			3: vr(3, asm.RegTypeInt, asm.Width32),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeI32, 2: ssa.TypeI32, 3: ssa.TypeI1},
	}
	_, a := row(t, ssa.Operation{
		Op: ssa.OpExec, Code: instr.I32_LT_U,
		Args: []ssa.Value{1, 2}, Results: []ssa.Value{3},
	}, r)
	require.Equal(t, []asm.Instruction{
		target.CMP(r.values[1], r.values[2]),
		target.CSET(r.values[3], target.CondCC),
	}, a.Rows())
}
func TestLower_Float(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{
			1: vr(1, asm.RegTypeFloat, asm.Width64),
			2: vr(2, asm.RegTypeFloat, asm.Width64),
			3: vr(3, asm.RegTypeFloat, asm.Width64),
		},
		types: map[ssa.Value]ssa.Type{1: ssa.TypeF64, 2: ssa.TypeF64, 3: ssa.TypeF64},
	}
	_, a := row(t, ssa.Operation{
		Op: ssa.OpExec, Code: instr.F64_ADD,
		Args: []ssa.Value{1, 2}, Results: []ssa.Value{3},
	}, r)
	require.Equal(t, []asm.Instruction{
		target.FADD(r.values[3], r.values[1], r.values[2]),
	}, a.Rows())
}

func TestLower_Select(t *testing.T) {
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
	_, a := row(t, ssa.Operation{
		Op: ssa.OpExec, Code: instr.SELECT,
		Args: []ssa.Value{1, 2, 3}, Results: []ssa.Value{4},
	}, r)
	require.Equal(t, []asm.Instruction{
		target.CMPI(r.values[3], 0),
		target.CSEL(r.values[4], r.values[1], r.values[2], target.CondNE),
	}, a.Rows())
}
func TestLower_Load(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width64)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI64},
	}
	_, a := row(t, ssa.Operation{
		Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 2}, Results: []ssa.Value{1},
	}, r)
	require.Equal(t, []asm.Instruction{
		target.LDR(r.values[1], target.X25, 16),
		target.SBFX(r.values[1], r.values[1], 0, 49),
	}, a.Rows())
}

func TestLower_Store(t *testing.T) {
	r := regs{
		values: map[ssa.Value]asm.VReg{1: vr(1, asm.RegTypeInt, asm.Width32)},
		types:  map[ssa.Value]ssa.Type{1: ssa.TypeI32},
	}
	_, a := row(t, ssa.Operation{
		Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 2}, Args: []ssa.Value{1},
	}, r)
	rows := a.Rows()
	require.Len(t, rows, 4)
	require.Equal(t, target.UXTW(target.X16, r.values[1]), rows[0])
	require.Equal(t, target.STR(target.X16, target.X25, 16), rows[3])
}
func TestLower_Prologue(t *testing.T) {
	m := arm64.New()
	a := asm.New(target.New())
	m.Prologue(a, 3, 1)
	rows := a.Rows()
	require.Equal(t, target.SUBI(target.SP, target.SP, 16), rows[0])
	require.Equal(t, target.STR(target.LR, target.SP, 8), rows[1])
	require.Equal(t, asm.Slots(), rows[2].Src2)
	require.Equal(t, target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)), rows[3])
	require.Equal(t, target.STR(target.XZR, target.X25, 8), rows[11])
}

func TestLower_Budget(t *testing.T) {
	m := arm64.New()
	a := asm.New(target.New())
	label := a.Label()
	m.Budget(a, label)
	require.Equal(t, []asm.Instruction{
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.SUBI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.BCondLabel(target.OpBLE, label),
	}, a.Rows())
}
func TestLower_Execute(t *testing.T) {
	b := ssa.New("muladd")
	entry := b.Block()
	state := b.Value(ssa.TypeState)
	a := b.Value(ssa.TypeI32)
	c := b.Value(ssa.TypeI32)
	product := b.Value(ssa.TypeI32)
	one := b.Value(ssa.TypeI32)
	result := b.Value(ssa.TypeI32)
	b.Add(entry, ssa.Operation{Op: ssa.OpState, State: state})
	b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Results: []ssa.Value{a}})
	b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 1}, Results: []ssa.Value{c}})
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
