package compile_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	compile "github.com/siyul-park/minivm/internal/jit/compile"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

type machine struct {
	ops      []ssa.Operation
	regs     map[ssa.Value]asm.VReg
	moves    [][2]asm.VReg
	budget   int
	returns  []ssa.Type
	complete []ssa.Type
}

func (m *machine) Arch() asm.Arch                    { return arm64.New() }
func (m *machine) Reserve() []asm.PReg               { return nil }
func (m *machine) Prologue(*asm.Assembler, int, int) {}
func (m *machine) Epilogue(*asm.Assembler)           {}

func (m *machine) Lower(_ *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	m.ops = append(m.ops, op)
	if m.regs == nil {
		m.regs = make(map[ssa.Value]asm.VReg)
	}
	for _, value := range op.Results {
		m.regs[value] = r.Reg(value)
	}
	return true
}

func (m *machine) Branch(_ *asm.Assembler, _ ssa.Terminator, _ compile.Regs, _ []asm.Label) {}
func (m *machine) Return(_ *asm.Assembler, _ []asm.VReg, types []ssa.Type) {
	m.returns = append([]ssa.Type(nil), types...)
}
func (m *machine) Complete(_ *asm.Assembler, _ []asm.VReg, types []ssa.Type) {
	m.complete = append([]ssa.Type(nil), types...)
}
func (m *machine) Budget(_ *asm.Assembler, _ asm.Label) { m.budget++ }
func (m *machine) Move(_ *asm.Assembler, dst, src asm.VReg) {
	m.moves = append(m.moves, [2]asm.VReg{dst, src})
}

func build(name string, fn func(*ssa.Builder)) *ssa.Function {
	b := ssa.New(name)
	fn(b)
	return b.Build()
}

func boxed(kind types.Kind) types.Boxed {
	switch kind {
	case types.KindI1:
		return types.BoxI1(true)
	case types.KindI8:
		return types.BoxI8(1)
	case types.KindI32:
		return types.BoxI32(1)
	case types.KindI64:
		return types.BoxI64(1)
	case types.KindF32:
		return types.BoxF32(1)
	case types.KindF64:
		return types.BoxF64(1)
	default:
		return types.BoxRef(1)
	}
}

func TestLower(t *testing.T) {
	t.Run("maps values by representation", testRepresentation)
	t.Run("loads entry parameters", testParams)
	t.Run("preserves return types", testReturn)
	t.Run("lowers complete", testComplete)
	t.Run("uses reverse postorder", testOrder)
	t.Run("counts loop-header edges", testBudget)
	t.Run("separates edge params", testEdges)
	t.Run("resolves a swap edge", testSwap)
	t.Run("rejects a stateless loop header", testHeader)
	t.Run("rejects an edge with mismatched arity", testArity)
}

func testRepresentation(t *testing.T) {
	f := build("types", func(b *ssa.Builder) {
		entry := b.Block()
		for _, kind := range []types.Kind{
			types.KindI1, types.KindI8, types.KindI32, types.KindI64,
			types.KindF32, types.KindF64, types.KindRef,
		} {
			value := b.Value(ssa.TypeOf(kind))
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: boxed(kind), Results: []ssa.Value{value}})
		}
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 0, 0)
	require.NoError(t, err)
	require.Len(t, m.ops, 7)

	for _, kind := range []types.Kind{
		types.KindI1, types.KindI8, types.KindI32, types.KindI64,
		types.KindF32, types.KindF64, types.KindRef,
	} {
		for value, reg := range m.regs {
			if f.Type(value) != ssa.TypeOf(kind) {
				continue
			}
			require.Equal(t, int32(value), reg.ID())
			switch kind {
			case types.KindF32:
				require.Equal(t, asm.RegTypeFloat, reg.Type())
				require.Equal(t, asm.Width32, reg.Width())
			case types.KindF64:
				require.Equal(t, asm.RegTypeFloat, reg.Type())
				require.Equal(t, asm.Width64, reg.Width())
			case types.KindI1, types.KindI8, types.KindI32:
				require.Equal(t, asm.RegTypeInt, reg.Type())
				require.Equal(t, asm.Width32, reg.Width())
			default:
				require.Equal(t, asm.RegTypeInt, reg.Type())
				require.Equal(t, asm.Width64, reg.Width())
			}
		}
	}
}
func testParams(t *testing.T) {
	f := build("params", func(b *ssa.Builder) {
		entry := b.Block()
		b.Param(entry, ssa.TypeI8)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 1, 0)
	require.NoError(t, err)
	require.Len(t, m.ops, 1)
	require.Equal(t, ssa.OpLoad, m.ops[0].Op)
	require.Equal(t, 0, m.ops[0].Slot.Index)
}

func testReturn(t *testing.T) {
	f := build("return", func(b *ssa.Builder) {
		entry := b.Block()
		v := b.Value(ssa.TypeI8)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI8(1), Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{v}})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 0, 0)
	require.NoError(t, err)
	require.Equal(t, []ssa.Type{ssa.TypeI8}, m.returns)
}

func testComplete(t *testing.T) {
	f := build("complete", func(b *ssa.Builder) {
		entry := b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete, Args: []ssa.Value{v}})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 0, 0)
	require.NoError(t, err)
	require.Equal(t, []ssa.Type{ssa.TypeI32}, m.complete)
}

func testOrder(t *testing.T) {
	f := build("order", func(b *ssa.Builder) {
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		value := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{
			Op: ssa.OpBranch, Args: []ssa.Value{value},
			Edges: []ssa.Edge{{Block: left}, {Block: right}},
		})
		for _, block := range []int{left, right} {
			result := b.Value(ssa.TypeI32)
			b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(int32(block)), Results: []ssa.Value{result}})
			b.Term(block, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		}
		b.Term(join, ssa.Terminator{Op: ssa.OpReturn})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 0, 0)
	require.NoError(t, err)
	require.Equal(t, []int32{0, 2, 1}, []int32{
		m.ops[0].Const.I32(), m.ops[1].Const.I32(), m.ops[2].Const.I32(),
	})
}

func testBudget(t *testing.T) {
	f := build("loop", func(b *ssa.Builder) {
		header, exit := b.Block(), b.Block()
		state := b.Value(ssa.TypeState)
		value := b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpState, State: state})
		b.Add(header, ssa.Operation{Op: ssa.OpStore, State: state, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Args: []ssa.Value{value}})
		b.Term(header, ssa.Terminator{
			Op: ssa.OpBranch, Args: []ssa.Value{value},
			Edges: []ssa.Edge{{Block: header}, {Block: exit}},
		})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 1, 0)
	require.NoError(t, err)
	require.Equal(t, 1, m.budget)
}

func testEdges(t *testing.T) {
	f := build("edges", func(b *ssa.Builder) {
		entry, left, right := b.Block(), b.Block(), b.Block()
		value := b.Value(ssa.TypeI32)
		leftParam := b.Param(left, ssa.TypeI32)
		rightParam := b.Param(right, ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{value}, Edges: []ssa.Edge{
			{Block: left, Args: []ssa.Value{value}},
			{Block: right, Args: []ssa.Value{value}},
		}})
		b.Term(left, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{leftParam}})
		b.Term(right, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{rightParam}})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 0, 0)
	require.NoError(t, err)
	require.Equal(t, [][2]asm.VReg{
		{asm.NewVReg(2, asm.RegTypeInt, asm.Width32), asm.NewVReg(1, asm.RegTypeInt, asm.Width32)},
		{asm.NewVReg(3, asm.RegTypeInt, asm.Width32), asm.NewVReg(1, asm.RegTypeInt, asm.Width32)},
	}, m.moves)
}

func testSwap(t *testing.T) {
	f := build("swap", func(b *ssa.Builder) {
		entry, loop := b.Block(), b.Block()
		a := b.Value(ssa.TypeI32)
		c := b.Value(ssa.TypeI32)
		p0 := b.Param(loop, ssa.TypeI32)
		p1 := b.Param(loop, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: loop, Args: []ssa.Value{a, c}}}})
		state := b.Value(ssa.TypeState)
		b.Add(loop, ssa.Operation{Op: ssa.OpState, State: state})
		b.Add(loop, ssa.Operation{Op: ssa.OpStore, State: state, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Args: []ssa.Value{p0}})
		b.Term(loop, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{p0}, Edges: []ssa.Edge{{Block: loop, Args: []ssa.Value{p1, p0}}, {Block: loop, Args: []ssa.Value{p0, p1}}}})
	})
	m := new(machine)
	_, err := compile.Lower(f, m, 0, 0)
	require.NoError(t, err)
	require.Equal(t, [][2]asm.VReg{
		{asm.NewVReg(3, asm.RegTypeInt, asm.Width32), asm.NewVReg(1, asm.RegTypeInt, asm.Width32)},
		{asm.NewVReg(4, asm.RegTypeInt, asm.Width32), asm.NewVReg(2, asm.RegTypeInt, asm.Width32)},
		{asm.NewVReg(5, asm.RegTypeInt, asm.Width32), asm.NewVReg(4, asm.RegTypeInt, asm.Width32)},
		{asm.NewVReg(4, asm.RegTypeInt, asm.Width32), asm.NewVReg(3, asm.RegTypeInt, asm.Width32)},
		{asm.NewVReg(3, asm.RegTypeInt, asm.Width32), asm.NewVReg(5, asm.RegTypeInt, asm.Width32)},
	}, m.moves)
}

func testHeader(t *testing.T) {
	f := build("header", func(b *ssa.Builder) {
		header := b.Block()
		value := b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{value}})
		b.Term(header, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})
	})
	_, err := compile.Lower(f, new(machine), 0, 0)
	require.ErrorIs(t, err, compile.ErrUnsupported)
}

func testArity(t *testing.T) {
	f := build("arity", func(b *ssa.Builder) {
		entry, left, right := b.Block(), b.Block(), b.Block()
		leftParam := b.Param(left, ssa.TypeI32)
		b.Param(right, ssa.TypeI32)
		value := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{value}, Edges: []ssa.Edge{{Block: left, Args: []ssa.Value{value}}, {Block: right}}})
		b.Term(left, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{leftParam}})
		b.Term(right, ssa.Terminator{Op: ssa.OpReturn})
	})
	_, err := compile.Lower(f, new(machine), 0, 0)
	require.ErrorIs(t, err, compile.ErrUnsupported)
}
