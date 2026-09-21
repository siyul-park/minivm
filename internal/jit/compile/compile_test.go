package compile_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// machine records every call Lower makes, in order.
type machine struct {
	calls []string
	regs  map[ssa.Value]asm.VReg
	moves [][2]asm.VReg
}

func (m *machine) Arch() asm.Arch      { return arm64.New() }
func (m *machine) Reserve() []asm.PReg { return nil }

func (m *machine) Prologue(*asm.Assembler, int, int) { m.calls = append(m.calls, "prologue") }
func (m *machine) Epilogue(*asm.Assembler)           { m.calls = append(m.calls, "epilogue") }
func (m *machine) Budget(*asm.Assembler)             { m.calls = append(m.calls, "budget") }

func (m *machine) Lower(_ *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	m.calls = append(m.calls, op.Op.String())
	if m.regs == nil {
		m.regs = map[ssa.Value]asm.VReg{}
	}
	for _, v := range op.Results {
		m.regs[v] = r.Reg(v)
	}
	return true
}

func (m *machine) Branch(_ *asm.Assembler, t ssa.Terminator, _ compile.Regs, _ []asm.Label) {
	m.calls = append(m.calls, t.Op.String())
}

func (m *machine) Return(_ *asm.Assembler, t ssa.Terminator, _ compile.Regs) {
	m.calls = append(m.calls, t.Op.String())
}

func (m *machine) Move(_ *asm.Assembler, dst, src asm.VReg) {
	m.calls = append(m.calls, "move")
	m.moves = append(m.moves, [2]asm.VReg{dst, src})
}

func i32(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width32) }

func TestLower(t *testing.T) {
	t.Run("maps each value to a register of its representation", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		consts := []types.Boxed{
			types.BoxI1(true), types.BoxI8(1), types.BoxI32(1), types.BoxI64(1),
			types.BoxF32(1), types.BoxF64(1), types.BoxRef(1),
		}
		for _, c := range consts {
			v := b.Value(ssa.TypeOf(c.Kind()))
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c, Results: []ssa.Value{v}})
		}
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, map[ssa.Value]asm.VReg{
			1: i32(1),
			2: i32(2),
			3: i32(3),
			4: asm.NewVReg(4, asm.RegTypeInt, asm.Width64),
			5: asm.NewVReg(5, asm.RegTypeFloat, asm.Width32),
			6: asm.NewVReg(6, asm.RegTypeFloat, asm.Width64),
			7: asm.NewVReg(7, asm.RegTypeInt, asm.Width64),
		}, m.regs)
	})

	t.Run("lays blocks out in reverse postorder", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		cond := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{cond}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(right, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})

		m := new(machine)
		_, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "br", "jump", "jump", "complete", "epilogue"}, m.calls)
	})

	t.Run("counts the budget in a loop header before its first stateful operation", func(t *testing.T) {
		b := ssa.New("f")
		header, exit := b.Block(), b.Block()
		value := b.Value(ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{value}})
		b.Add(header, ssa.Operation{Op: ssa.OpState, State: state})
		b.Add(header, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{value}, State: state})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{value}, Edges: []ssa.Edge{{Block: header}, {Block: exit}}})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, err := compile.Lower(b.Build(), m, 0, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "budget", "store", "br", "return", "epilogue"}, m.calls)
	})

	t.Run("moves each edge into its own successor's parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right := b.Block(), b.Block(), b.Block()
		value := b.Value(ssa.TypeI32)
		l := b.Param(left, ssa.TypeI32)
		r := b.Param(right, ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{value}, Edges: []ssa.Edge{
			{Block: left, Args: []ssa.Value{value}},
			{Block: right, Args: []ssa.Value{value}},
		}})
		b.Term(left, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{l}})
		b.Term(right, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{r}})

		m := new(machine)
		_, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, [][2]asm.VReg{{i32(2), i32(1)}, {i32(3), i32(1)}}, m.moves)
		require.Equal(t, []string{
			"prologue", "const", "br", "return", "return",
			"move", "jump", "move", "jump", "epilogue",
		}, m.calls)
	})

	t.Run("breaks a swap cycle through one scratch register", func(t *testing.T) {
		b := ssa.New("f")
		entry, loop := b.Block(), b.Block()
		x := b.Value(ssa.TypeI32)
		y := b.Value(ssa.TypeI32)
		p := b.Param(loop, ssa.TypeI32)
		q := b.Param(loop, ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{y}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: loop, Args: []ssa.Value{x, y}}}})
		b.Add(loop, ssa.Operation{Op: ssa.OpState, State: state})
		b.Add(loop, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{p}, State: state})
		b.Term(loop, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: loop, Args: []ssa.Value{q, p}}}})

		m := new(machine)
		_, err := compile.Lower(b.Build(), m, 0, 1)
		require.NoError(t, err)
		scratch := i32(6)
		require.Equal(t, [][2]asm.VReg{
			{i32(3), i32(1)}, {i32(4), i32(2)},
			{scratch, i32(4)}, {i32(4), i32(3)}, {i32(3), scratch},
		}, m.moves)
	})

	t.Run("rejects entry block parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		_, err := compile.Lower(b.Build(), new(machine), 1, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects a loop header without state", func(t *testing.T) {
		b := ssa.New("f")
		header := b.Block()
		value := b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{value}})
		b.Term(header, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

		_, err := compile.Lower(b.Build(), new(machine), 0, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects an edge whose arguments do not match the parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry, target := b.Block(), b.Block()
		b.Param(target, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: target}}})
		b.Term(target, ssa.Terminator{Op: ssa.OpReturn})

		_, err := compile.Lower(b.Build(), new(machine), 0, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})
}
