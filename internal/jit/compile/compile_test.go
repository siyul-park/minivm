package compile_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// machine records every call Lower makes, in order, and emits the fewest
// rows that give each value a location: a definition per result, a use per
// exit value, and branches that follow the labels it is given.
type machine struct {
	calls []string
	regs  map[ssa.Value]asm.VReg
	moves [][2]asm.VReg
	uses  [][]asm.VReg
}

func (m *machine) Arch() asm.Arch      { return arm64.New() }
func (m *machine) Reserve() []asm.PReg { return nil }

func (m *machine) Prologue(*asm.Assembler, int, int) { m.calls = append(m.calls, "prologue") }
func (m *machine) Epilogue(*asm.Assembler)           { m.calls = append(m.calls, "epilogue") }

func (m *machine) Lower(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if op.Op == ssa.OpExec && op.Code == instr.MAP_GET {
		return false
	}
	m.calls = append(m.calls, op.Op.String())
	if m.regs == nil {
		m.regs = map[ssa.Value]asm.VReg{}
	}
	switch {
	case op.Op == ssa.OpExec && op.Code == instr.I32_DIV_S:
		a.Emit(arm64.CBZLabel(s.Reg(op.Args[1]), s.Exit(jit.ExitDeopt)))
	case op.Op == ssa.OpRelease:
		a.Emit(arm64.CBZLabel(s.Reg(op.Args[0]), s.Exit(jit.ExitRelease)))
	}
	for _, v := range op.Results {
		m.regs[v] = s.Reg(v)
		a.Emit(arm64.LDI(s.Reg(v), 0)...)
	}
	return true
}

func (m *machine) Branch(a *asm.Assembler, t ssa.Terminator, s compile.Site, labels []asm.Label) {
	m.calls = append(m.calls, t.Op.String())
	if t.Op != ssa.OpJump {
		a.Emit(arm64.CBNZLabel(s.Reg(t.Args[0]), labels[0]))
		labels = labels[1:]
	}
	a.Emit(arm64.BLabel(labels[len(labels)-1]))
}

func (m *machine) Return(a *asm.Assembler, t ssa.Terminator, _ compile.Site) {
	m.calls = append(m.calls, t.Op.String())
	a.Emit(arm64.RET())
}

func (m *machine) Budget(a *asm.Assembler, safepoint asm.Label) {
	m.calls = append(m.calls, "budget")
	a.Emit(arm64.BCondLabel(arm64.OpBLE, safepoint))
}

func (m *machine) Exit(a *asm.Assembler, id int, k jit.Kind, uses []asm.VReg) {
	m.calls = append(m.calls, fmt.Sprintf("exit %d %d", id, k))
	m.uses = append(m.uses, uses)
	for _, u := range uses {
		a.Emit(arm64.USE(u))
	}
	a.Emit(arm64.BL(0))
}

func (m *machine) Results(a *asm.Assembler, regs []asm.VReg) {
	m.calls = append(m.calls, "results")
	for _, r := range regs {
		a.Emit(arm64.LDI(r, 0)...)
	}
}

func (m *machine) Move(a *asm.Assembler, dst, src asm.VReg) {
	m.calls = append(m.calls, "move")
	m.moves = append(m.moves, [2]asm.VReg{dst, src})
	a.Emit(arm64.MOV(dst, src))
}

func i32(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width32) }
func i64(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width64) }

// state adds the interpreter state at ip with stack to block.
func state(b *ssa.Builder, block, ip int, stack ...ssa.Operand) ssa.Value {
	v := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: ip, Returns: 1, Stack: stack}}, Results: []ssa.Value{v}})
	return v
}

func constant(b *ssa.Builder, block int, c types.Boxed) ssa.Value {
	v := b.Value(ssa.TypeOf(c.Kind()))
	b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: c, Results: []ssa.Value{v}})
	return v
}

func TestLower(t *testing.T) {
	t.Run("maps each value to a register of its representation", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		for _, c := range []types.Boxed{
			types.BoxI1(true), types.BoxI8(1), types.BoxI32(1), types.BoxI64(1), types.BoxRef(1),
		} {
			constant(b, entry, c)
		}
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, _, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, map[ssa.Value]asm.VReg{1: i32(1), 2: i32(2), 3: i32(3), 4: i64(4), 5: i64(5)}, m.regs)
	})

	t.Run("lays blocks out in reverse postorder", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		cond := constant(b, entry, types.BoxI32(0))
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(right, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})

		m := new(machine)
		_, _, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "br", "jump", "jump", "complete", "epilogue"}, m.calls)
	})

	t.Run("moves each edge into its own successor's parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right := b.Block(), b.Block(), b.Block()
		l := b.Param(left, ssa.TypeI32)
		r := b.Param(right, ssa.TypeI32)
		value := constant(b, entry, types.BoxI32(1))
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{value}, Edges: []ssa.Edge{
			{Block: left, Args: []ssa.Value{value}},
			{Block: right, Args: []ssa.Value{value}},
		}})
		b.Term(left, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{l}})
		b.Term(right, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{r}})

		m := new(machine)
		_, _, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, [][2]asm.VReg{{i32(1), i32(3)}, {i32(2), i32(3)}}, m.moves)
		require.Equal(t, []string{
			"prologue", "const", "br", "return", "return",
			"move", "jump", "move", "jump", "epilogue",
		}, m.calls)
	})

	t.Run("breaks a swap cycle through one scratch register", func(t *testing.T) {
		b := ssa.New("f")
		entry, loop := b.Block(), b.Block()
		p := b.Param(loop, ssa.TypeI32)
		q := b.Param(loop, ssa.TypeI32)
		x := constant(b, entry, types.BoxI32(1))
		y := constant(b, entry, types.BoxI32(2))
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: loop, Args: []ssa.Value{x, y}}}})
		at := state(b, loop, 0)
		b.Add(loop, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{p}, State: at})
		b.Term(loop, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: loop, Args: []ssa.Value{q, p}}}})

		m := new(machine)
		_, _, err := compile.Lower(b.Build(), m, 0, 1)
		require.NoError(t, err)
		scratch := i32(6)
		require.Equal(t, [][2]asm.VReg{
			{i32(1), i32(3)}, {i32(2), i32(4)},
			{scratch, i32(2)}, {i32(2), i32(1)}, {i32(1), scratch},
		}, m.moves)
	})

	t.Run("bridges an operation the machine declines", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		m1 := constant(b, entry, types.BoxRef(1))
		key := constant(b, entry, types.BoxI32(2))
		at := state(b, entry, 7, ssa.Operand{Value: m1, Owned: true}, ssa.Operand{Value: key})
		got := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.MAP_GET, Args: []ssa.Value{m1, key}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		_, exits, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "const", "exit 0 1", "results", "return", "epilogue"}, m.calls)
		require.Equal(t, [][]asm.VReg{{i64(1), i32(2)}}, m.uses)
		require.Equal(t, []jit.Exit{{
			Kind: jit.ExitBridge,
			Code: instr.MAP_GET,
			Frames: []jit.Frame{{Address: 1, IP: 7, Returns: 1, Stack: []jit.Operand{
				{Value: jit.Value{Kind: types.KindRef, Loc: asm.Loc{Reg: arm64.X0}}, Owned: true},
				{Value: jit.Value{Kind: types.KindI32, Loc: asm.Loc{Reg: arm64.W1}}},
			}}},
			Results: []types.Kind{types.KindRef},
		}}, exits)
	})

	t.Run("deopts a failed check at its operation's state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x := constant(b, entry, types.BoxI32(6))
		y := constant(b, entry, types.BoxI32(0))
		at := state(b, entry, 3, ssa.Operand{Value: x}, ssa.Operand{Value: y})
		q := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{x, y}, State: at, Results: []ssa.Value{q}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{q}})

		m := new(machine)
		_, exits, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "const", "exec", "return", "exit 0 0", "epilogue"}, m.calls)
		require.Len(t, exits, 1)
		require.Equal(t, jit.ExitDeopt, exits[0].Kind)
		require.Equal(t, 3, exits[0].Frames[0].IP)
	})

	t.Run("resumes after a release exit", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := constant(b, entry, types.BoxRef(3))
		at := state(b, entry, 5)
		b.Add(entry, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{ref}, State: at})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, exits, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "release", "return", "exit 0 3", "jump", "epilogue"}, m.calls)
		require.Equal(t, jit.ExitRelease, exits[0].Kind)
		require.Equal(t, types.KindRef, exits[0].Release.Kind)
	})

	t.Run("counts the budget in a loop header before its first stateful operation", func(t *testing.T) {
		b := ssa.New("f")
		header, exit := b.Block(), b.Block()
		value := constant(b, header, types.BoxI32(1))
		at := state(b, header, 9, ssa.Operand{Value: value})
		b.Add(header, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{value}, State: at})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{value}, Edges: []ssa.Edge{{Block: header}, {Block: exit}}})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, exits, err := compile.Lower(b.Build(), m, 0, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "budget", "store", "br", "return", "exit 0 2", "jump", "epilogue"}, m.calls)
		require.Equal(t, jit.ExitSafepoint, exits[0].Kind)
		require.Equal(t, 9, exits[0].Frames[0].IP)
	})

	t.Run("deopts at an exit terminator", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		at := state(b, entry, 4)
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: at})

		m := new(machine)
		_, exits, err := compile.Lower(b.Build(), m, 0, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "exit 0 0", "epilogue"}, m.calls)
		require.Equal(t, jit.ExitDeopt, exits[0].Kind)
	})

	t.Run("unboxes an i64 slot word only through its guard", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		word := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{word}})
		at := state(b, entry, 0)
		value := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{word}, State: at, Results: []ssa.Value{value}})
		again := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{value}, State: at, Results: []ssa.Value{again}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{again}})

		m := new(machine)
		_, _, err := compile.Lower(b.Build(), m, 1, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "load", "guard.kind", "move", "return", "epilogue"}, m.calls)
	})

	t.Run("rejects an unguarded i64 slot word", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		word := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{word}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{word}})

		_, _, err := compile.Lower(b.Build(), new(machine), 1, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects a shape guard", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := constant(b, entry, types.BoxRef(1))
		at := state(b, entry, 0)
		guarded := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Args: []ssa.Value{ref}, State: at, Results: []ssa.Value{guarded}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		_, _, err := compile.Lower(b.Build(), new(machine), 0, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects a suspension", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		at := state(b, entry, 0)
		b.Term(entry, ssa.Terminator{Op: ssa.OpSuspend, State: at})

		_, _, err := compile.Lower(b.Build(), new(machine), 0, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects entry block parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		_, _, err := compile.Lower(b.Build(), new(machine), 1, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects a loop header without state", func(t *testing.T) {
		b := ssa.New("f")
		header := b.Block()
		constant(b, header, types.BoxI32(1))
		b.Term(header, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

		_, _, err := compile.Lower(b.Build(), new(machine), 0, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects an edge whose arguments do not match the parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry, target := b.Block(), b.Block()
		b.Param(target, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: target}}})
		b.Term(target, ssa.Terminator{Op: ssa.OpReturn})

		_, _, err := compile.Lower(b.Build(), new(machine), 0, 0)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})
}
