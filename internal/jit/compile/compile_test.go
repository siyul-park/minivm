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
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

// machine records every call Lower makes, in order.
type machine struct {
	calls     []string
	kinds     []types.Kind
	count     bool
	arguments []types.Kind
	args      []asm.VReg
	results   []types.Kind
	regs      map[ssa.Value]asm.VReg
	moves     [][2]asm.VReg
	uses      [][]asm.VReg
	spills    []int
	sites     []compile.Call
	consts    []uint64
}

func (m *machine) Arch() asm.Arch      { return arm64.New() }
func (m *machine) Reserve() []asm.PReg { return nil }

func (m *machine) Prologue(_ *asm.Assembler, _ int, count bool, l compile.Layout, args []asm.VReg) {
	m.calls = append(m.calls, "prologue")
	m.kinds = l.Kinds
	m.count = count
	m.arguments = l.Arguments
	m.args = args
	m.results = l.Results
}

func (m *machine) Epilogue(*asm.Assembler) { m.calls = append(m.calls, "epilogue") }

func (m *machine) Enter(a *asm.Assembler, l compile.Layout) asm.Label {
	m.calls = append(m.calls, "enter")
	m.arguments = l.Arguments
	m.results = l.Results
	label := a.Label()
	a.Bind(label)
	return label
}

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
		a.Emit(arm64.CBZLabel(s.Reg(op.Args[1]), s.Deopt()))
	case op.Op == ssa.OpRelease:
		exit, resume := s.Release(s.Reg(op.Args[0]))
		a.Emit(arm64.CBZLabel(s.Reg(op.Args[0]), exit))
		a.Bind(resume)
	case op.Op == ssa.OpStore && s.Type(op.Args[0]) == ssa.TypeI64:
		exit, resume := s.Box(s.Reg(op.Args[0]))
		a.Emit(arm64.CBNZLabel(s.Reg(op.Args[0]), exit))
		a.Bind(resume)
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

func (m *machine) Spill(a *asm.Assembler, reg asm.VReg, slot int) {
	m.spills = append(m.spills, slot)
	a.Emit(arm64.STR(reg, arm64.SP, int16(8*slot)))
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

func (m *machine) Call(a *asm.Assembler, c compile.Call, s compile.Site) bool {
	m.calls = append(m.calls, "call")
	m.sites = append(m.sites, c)
	for _, u := range c.Live {
		a.Emit(arm64.USE(u))
	}
	a.Emit(arm64.BLabel(c.Bridge))
	for _, v := range c.Results {
		a.Emit(arm64.LDI(s.Reg(v), 0)...)
	}
	return true
}

func (m *machine) Move(a *asm.Assembler, dst, src asm.VReg) {
	m.calls = append(m.calls, "move")
	m.moves = append(m.moves, [2]asm.VReg{dst, src})
	a.Emit(arm64.MOV(dst, src))
}

func (m *machine) Const(a *asm.Assembler, dst asm.VReg, word uint64) {
	m.calls = append(m.calls, "const")
	m.consts = append(m.consts, word)
	a.Emit(arm64.LDI(dst, 0)...)
}

func i32(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width32) }
func i64(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeInt, asm.Width64) }

// state adds the interpreter state at ip with stack to block.
func state(b *ssa.Builder, block, ip int, stack ...ssa.Operand) ssa.Value {
	v := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: ip, Returns: 1, Stack: stack}}, Results: []ssa.Value{v}})
	return v
}

// function is a bytecode function of code whose params and locals are i32
// slots.
func function(params, locals int, code ...instr.Instruction) *types.Function {
	slots := func(n int) []types.Type {
		var out []types.Type
		for range n {
			out = append(out, types.TypeI32)
		}
		return out
	}
	return &types.Function{
		Typ:    &types.FunctionType{Params: slots(params), Returns: []types.Type{types.TypeI32}},
		Locals: slots(locals),
		Code:   instr.Marshal(code),
	}
}

func constant(b *ssa.Builder, block int, c types.Boxed) ssa.Value {
	v := b.Value(ssa.TypeOf(c.Kind()))
	b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: ssa.Word(c), Results: []ssa.Value{v}})
	return v
}

func TestLower(t *testing.T) {
	t.Run("saves a deopt-only local on the side exit", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		v := constant(b, entry, types.BoxI32(7))
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 1, Locals: []ssa.Local{{Index: 0, Value: v}}}}, Results: []ssa.Value{state}})
		one := constant(b, entry, types.BoxI32(1))
		result := b.Value(ssa.TypeI32)
		dividend := constant(b, entry, types.BoxI32(8))
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{dividend, one}, State: state, Results: []ssa.Value{result}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, exits, _, err := compile.Lower(b.Build(), m, function(0, 1), nil, 0, false, true)
		require.NoError(t, err)
		require.Len(t, exits, 1)
		require.Equal(t, []int{0}, m.spills)
		require.Empty(t, m.uses[0])
		require.Equal(t, []jit.Local{{Index: 0, Value: jit.Value{Kind: types.KindI32, Loc: asm.Loc{Slot: 0, Spilled: true}}}}, exits[0].Frame.Locals)
	})

	t.Run("keeps a deopt-only local live across a call that a callee deopt can materialize", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		arg := constant(b, entry, types.BoxI32(1))
		callee := constant(b, entry, types.BoxRef(9))
		v := constant(b, entry, types.BoxI32(7))
		at := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 0, Returns: 1, Stack: []ssa.Operand{{Value: v}, {Value: arg}, {Value: callee}}, Locals: []ssa.Local{{Index: 0, Value: v}}}}, Results: []ssa.Value{at}})
		got := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{v}})

		m := new(machine)
		_, exits, _, err := compile.Lower(b.Build(), m, function(1, 1, instr.New(instr.CALL)), transform.Objects{9: {Function: function(1, 1)}}, 0, false, true)
		require.NoError(t, err)
		require.Empty(t, m.spills)
		// v, a constant, is loaded once for the call's map and kept live
		// across the call; the local and the stack entry share it.
		require.Len(t, m.sites[0].Live, 1)
		frame := exits[0].Frame
		require.Equal(t, []jit.Local{{Index: 0, Value: frame.Stack[0].Value}}, frame.Locals)
	})

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
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
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
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "br", "jump", "jump", "complete", "epilogue", "enter"}, m.calls)
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
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, [][2]asm.VReg{{i32(1), i32(3)}, {i32(2), i32(3)}}, m.moves)
		require.Equal(t, []string{
			"prologue", "const", "br", "return", "return",
			"move", "jump", "move", "jump", "epilogue", "enter",
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
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 1), nil, 0, false, true)
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
		_, exits, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "const", "exit 0 1", "results", "return", "epilogue", "enter"}, m.calls)
		require.Equal(t, [][]asm.VReg{{i64(1), i32(2)}}, m.uses)
		require.Equal(t, []jit.Exit{{
			Kind: jit.ExitBridge,
			Code: instr.MAP_GET,
			Pops: 2,
			Frame: jit.Frame{Address: 1, IP: 7, Returns: 1, Stack: []jit.Operand{
				{Value: jit.Value{Kind: types.KindRef, Loc: asm.Loc{Reg: arm64.X0}}, Owned: true},
				{Value: jit.Value{Kind: types.KindI32, Loc: asm.Loc{Reg: arm64.W1}}},
			}},
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
		_, exits, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "const", "exec", "jump", "exit 0 0", "return", "epilogue", "enter"}, m.calls)
		require.Len(t, exits, 1)
		require.Equal(t, jit.ExitDeopt, exits[0].Kind)
		require.Equal(t, 3, exits[0].Frame.IP)
	})

	t.Run("resumes after a release exit", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := constant(b, entry, types.BoxRef(3))
		at := state(b, entry, 5)
		b.Add(entry, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{ref}, State: at})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, exits, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "release", "return", "exit 0 3", "jump", "epilogue", "enter"}, m.calls)
		require.Equal(t, []jit.Exit{{
			Kind: jit.ExitRelease,
			Word: jit.Value{Kind: types.KindRef, Loc: asm.Loc{Reg: arm64.X0}},
		}}, exits)
	})

	t.Run("maps a box exit with the word and a full state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		wide := constant(b, entry, types.BoxI64(1))
		at := state(b, entry, 9, ssa.Operand{Value: wide})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{wide}, State: at})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		_, exits, _, err := compile.Lower(b.Build(), new(machine), function(0, 1), nil, 0, false, true)
		require.NoError(t, err)
		require.Len(t, exits, 1)
		require.Equal(t, jit.ExitBox, exits[0].Kind)
		require.Equal(t, types.KindI64, exits[0].Word.Kind)
		require.Equal(t, 9, exits[0].Frame.IP)
	})

	t.Run("lowers a wide i64 constant through a remat stall in a function with a call", func(t *testing.T) {
		seed := int64(-3750763034362895579)
		b := ssa.New("f")
		entry := b.Block()
		wide := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(seed), Results: []ssa.Value{wide}})
		arg := constant(b, entry, types.BoxI32(7))
		callee := constant(b, entry, types.BoxRef(2))
		// wide sits below the call's own operands, so it stays a deopt-only
		// map entry, materialized only at the exit stall.
		at := state(b, entry, 0, ssa.Operand{Value: wide}, ssa.Operand{Value: arg}, ssa.Operand{Value: callee})
		got := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		caller := function(1, 1, instr.New(instr.CALL))
		_, _, _, err := compile.Lower(b.Build(), m, caller, transform.Objects{2: {Function: function(1, 2)}}, 0, false, true)
		require.NoError(t, err)
		require.Contains(t, m.consts, uint64(seed))
	})

	t.Run("calls a resolved function at the frame base above its operands, borrowing a callee its state does not own", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		below := constant(b, entry, types.BoxI32(5))
		arg := constant(b, entry, types.BoxI32(7))
		callee := constant(b, entry, types.BoxRef(2))
		at := state(b, entry, 0, ssa.Operand{Value: below}, ssa.Operand{Value: arg}, ssa.Operand{Value: callee})
		got := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		caller := function(1, 1, instr.New(instr.CALL))
		_, exits, _, err := compile.Lower(b.Build(), m, caller, transform.Objects{2: {Function: function(1, 2)}}, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "call", "return", "exit 0 4", "epilogue", "enter"}, m.calls)
		site := m.sites[0]
		site.Bridge = 0
		require.Equal(t, compile.Call{
			Address: 2, Callee: callee, Args: []ssa.Value{arg}, Results: []ssa.Value{got},
			Base: 3, Size: 3, Exit: 0, Live: []asm.VReg{i32(11)}, Owned: false,
			Registers: []types.Kind{types.KindI32},
			Arguments: []types.Kind{types.KindI32},
		}, site)
		require.Equal(t, []jit.Exit{{
			Kind:   jit.ExitCall,
			Callee: 2,
			Owned:  false,
			Frame: jit.Frame{Address: 1, IP: 1, Returns: 1, Stack: []jit.Operand{
				{Value: jit.Value{Kind: types.KindI32, Loc: asm.Loc{Reg: arm64.W0}}},
			}},
		}}, exits)
	})

	t.Run("lends a borrowed argument its state does not own", func(t *testing.T) {
		// target never writes param 1 (any), so transform.Borrows marks it
		// borrowed; param 0 (i32) is never borrowable regardless.
		target := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}}}
		lent := func(t *testing.T, argOwned bool) []int {
			b := ssa.New("f")
			entry := b.Block()
			n := constant(b, entry, types.BoxI32(3))
			self := constant(b, entry, types.BoxRef(2))
			callee := constant(b, entry, types.BoxRef(2))
			at := state(b, entry, 0, ssa.Operand{Value: n}, ssa.Operand{Value: self, Owned: argOwned}, ssa.Operand{Value: callee})
			got := b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{n, self, callee}, State: at, Results: []ssa.Value{got}})
			b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

			m := new(machine)
			caller := function(0, 0, instr.New(instr.CALL))
			_, exits, _, err := compile.Lower(b.Build(), m, caller, transform.Objects{2: {Function: target}}, 0, false, true)
			require.NoError(t, err)
			return exits[0].Lent
		}

		require.Equal(t, []int{1}, lent(t, false))
		require.Empty(t, lent(t, true))
	})

	t.Run("register-passes an i64 argument", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		arg := constant(b, entry, types.BoxI64(7))
		callee := constant(b, entry, types.BoxRef(2))
		at := state(b, entry, 0, ssa.Operand{Value: arg}, ssa.Operand{Value: callee})
		got := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		caller := function(0, 0, instr.New(instr.CALL))
		target := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI32}}}
		_, _, _, err := compile.Lower(b.Build(), m, caller, transform.Objects{2: {Function: target}}, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []types.Kind{types.KindI64}, m.sites[0].Arguments)
	})

	t.Run("releases a callee its state owns once the call returns", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		arg := constant(b, entry, types.BoxI32(7))
		callee := constant(b, entry, types.BoxRef(2))
		b.Add(entry, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{callee}})
		at := state(b, entry, 0, ssa.Operand{Value: arg}, ssa.Operand{Value: callee, Owned: true})
		got := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		caller := function(1, 1, instr.New(instr.CALL))
		_, exits, _, err := compile.Lower(b.Build(), m, caller, transform.Objects{2: {Function: function(1, 2)}}, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "retain", "call", "return", "exit 0 4", "epilogue", "enter"}, m.calls)
		require.True(t, m.sites[0].Owned)
		require.True(t, exits[0].Owned)
	})

	t.Run("branches to its own entry directly on a self call", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		arg := constant(b, entry, types.BoxI32(7))
		callee := constant(b, entry, types.BoxRef(9))
		at := state(b, entry, 0, ssa.Operand{Value: arg}, ssa.Operand{Value: callee})
		got := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		caller := function(1, 1, instr.New(instr.CALL))
		_, _, _, err := compile.Lower(b.Build(), m, caller, transform.Objects{9: {Function: caller}}, 9, false, true)
		require.NoError(t, err)
		site := m.sites[0]
		require.Equal(t, 9, site.Address)
		require.True(t, site.Self)
	})

	t.Run("dispatches an OSR unit's call to its own address through the natives table", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		arg := constant(b, entry, types.BoxI32(7))
		callee := constant(b, entry, types.BoxRef(9))
		at := state(b, entry, 0, ssa.Operand{Value: arg}, ssa.Operand{Value: callee})
		got := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		caller := function(1, 1, instr.New(instr.CALL))
		_, _, _, err := compile.Lower(b.Build(), m, caller, transform.Objects{9: {Function: caller}}, 9, true, false)
		require.NoError(t, err)
		site := m.sites[0]
		require.Equal(t, 9, site.Address)
		require.False(t, site.Self)
	})
	t.Run("hands the machine one register of each register-passed parameter's class", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		fn := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI8, types.TypeF64}}}
		_, _, _, err := compile.Lower(b.Build(), m, fn, nil, 0, false, true)
		require.NoError(t, err)
		require.Len(t, m.args, 2)
		require.NotEqual(t, m.args[0].ID(), m.args[1].ID())
		require.Equal(t, [2]any{asm.RegTypeInt, asm.Width32}, [2]any{m.args[0].Type(), m.args[0].Width()})
		require.Equal(t, [2]any{asm.RegTypeFloat, asm.Width64}, [2]any{m.args[1].Type(), m.args[1].Width()})
	})

	t.Run("hands the machine the kinds of its slots", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		fn := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}}, Locals: []types.Type{types.TypeString}}
		_, _, _, err := compile.Lower(b.Build(), m, fn, nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []types.Kind{types.KindI64, types.KindRef}, m.kinds)
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
		_, exits, _, err := compile.Lower(b.Build(), m, function(0, 1), nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "const", "budget", "store", "br", "return", "exit 0 2", "jump", "epilogue", "enter"}, m.calls)
		require.Equal(t, jit.ExitSafepoint, exits[0].Kind)
		require.Equal(t, 9, exits[0].Frame.IP)
	})

	t.Run("keeps a loop's constant in one register even when the function calls", func(t *testing.T) {
		b := ssa.New("f")
		header, exit := b.Block(), b.Block()
		value := constant(b, header, types.BoxI32(1))
		at := state(b, header, 9, ssa.Operand{Value: value})
		b.Add(header, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{value}, State: at})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{value}, Edges: []ssa.Edge{{Block: header}, {Block: exit}}})
		arg := constant(b, exit, types.BoxI32(7))
		callee := constant(b, exit, types.BoxRef(2))
		call := state(b, exit, 0, ssa.Operand{Value: arg}, ssa.Operand{Value: callee})
		got := b.Value(ssa.TypeI32)
		b.Add(exit, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg, callee}, State: call, Results: []ssa.Value{got}})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}})

		m := new(machine)
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 1, instr.New(instr.CALL)), transform.Objects{2: {Function: function(1, 2)}}, 0, false, true)
		require.NoError(t, err)
		// value is lowered once at its definition, not at the store and the
		// branch; arg, outside the loop, is loaded at the call only.
		require.Equal(t, []string{"prologue", "const", "budget", "store", "br", "call"}, m.calls[:6])
	})

	t.Run("deopts at an exit terminator", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		at := state(b, entry, 4)
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: at})

		m := new(machine)
		_, exits, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "exit 0 0", "epilogue", "enter"}, m.calls)
		require.Equal(t, jit.ExitDeopt, exits[0].Kind)
	})

	t.Run("rejects a guard.kind of an already-raw i64", func(t *testing.T) {
		// Promote aliases away every guard.kind whose arg is already a raw
		// i64 (its own reaching value), so a genuine one always arrives
		// with a fresh, unguarded slot word as its arg. A second guard on
		// an already-guarded value is exactly the case promote must never
		// produce: refusing it here, instead of silently moving the value
		// through, catches that bug at compile time rather than
		// reinterpreting a raw int's bits as a boxed tag downstream.
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

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(1, 0), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("moves a guard.kind of a register-passed i64 parameter instead of unboxing it", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		word := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Results: []ssa.Value{word}})
		at := state(b, entry, 0)
		value := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{word}, State: at, Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{value}})

		m := new(machine)
		caller := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}}}
		_, _, _, err := compile.Lower(b.Build(), m, caller, nil, 0, false, true)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "move", "move", "return", "epilogue", "enter"}, m.calls)
	})

	t.Run("rejects an unguarded i64 slot word", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		word := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{word}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{word}})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(1, 0), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects a call of a function it cannot resolve", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		callee := constant(b, entry, types.BoxRef(2))
		at := state(b, entry, 0, ssa.Operand{Value: callee})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{callee}, State: at})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 0, instr.New(instr.CALL)), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("register-passes a call returning an i64", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		callee := constant(b, entry, types.BoxRef(2))
		at := state(b, entry, 0, ssa.Operand{Value: callee})
		got := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{callee}, State: at, Results: []ssa.Value{got}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		target := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI64}}}
		m := new(machine)
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 0, instr.New(instr.CALL)), transform.Objects{2: {Function: target}}, 0, false, true)
		require.NoError(t, err)
		require.Len(t, m.sites, 1)
		require.Equal(t, []types.Kind{types.KindI64}, m.sites[0].Registers)
	})

	t.Run("rejects a call returning an i64 the callee cannot register-pass", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		callee := constant(b, entry, types.BoxRef(2))
		at := state(b, entry, 0, ssa.Operand{Value: callee})
		got := [3]ssa.Value{b.Value(ssa.TypeI64), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)}
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{callee}, State: at, Results: got[:]})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		target := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI64, types.TypeI32, types.TypeI32}}}
		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 0, instr.New(instr.CALL)), transform.Objects{2: {Function: target}}, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects a return over an owned operand", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := constant(b, entry, types.BoxRef(2))
		value := constant(b, entry, types.BoxI32(1))
		at := state(b, entry, 0, ssa.Operand{Value: ref, Owned: true}, ssa.Operand{Value: value})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{value}, State: at})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 0), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("checks only the returning frame", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := constant(b, entry, types.BoxRef(2))
		value := constant(b, entry, types.BoxI32(1))
		at := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Address: 2, Stack: []ssa.Operand{{Value: ref, Owned: true}}},
			{Address: 1, Stack: []ssa.Operand{{Value: value}}},
		}, Results: []ssa.Value{at}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{value}, State: at})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
	})

	t.Run("routes a shape guard to Machine.Lower", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := constant(b, entry, types.BoxRef(1))
		at := state(b, entry, 0)
		guarded := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Args: []ssa.Value{ref}, State: at, Results: []ssa.Value{guarded}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Contains(t, m.calls, ssa.OpGuardShape.String())
	})

	t.Run("routes a value guard to Machine.Lower", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		v := constant(b, entry, types.BoxRef(1))
		c := constant(b, entry, types.BoxRef(1))
		at := state(b, entry, 0)
		guarded := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{v, c}, State: at, Results: []ssa.Value{guarded}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		m := new(machine)
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 0), nil, 0, false, true)
		require.NoError(t, err)
		require.Contains(t, m.calls, ssa.OpGuardValue.String())
	})

	t.Run("rejects an unsupported terminator", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		at := state(b, entry, 0)
		b.Term(entry, ssa.Terminator{Op: ssa.OpGuardKind, State: at})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 0), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects entry block parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(1, 0), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("accepts block 0 parameters on an OSR unit", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		p := b.Param(entry, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{p}})

		m := new(machine)
		_, _, _, err := compile.Lower(b.Build(), m, function(0, 1), nil, 0, true, false)
		require.NoError(t, err)
		require.Equal(t, []string{"prologue", "load", "return", "epilogue", "enter"}, m.calls)
	})

	t.Run("rejects an OSR unit whose block 0 parameter is an i64 operand", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		p := b.Param(entry, ssa.TypeI64)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{p}})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 1), nil, 0, true, false)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects a loop header without state", func(t *testing.T) {
		b := ssa.New("f")
		header := b.Block()
		constant(b, header, types.BoxI32(1))
		b.Term(header, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 0), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects an edge whose arguments do not match the parameters", func(t *testing.T) {
		b := ssa.New("f")
		entry, target := b.Block(), b.Block()
		b.Param(target, ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: target}}})
		b.Term(target, ssa.Terminator{Op: ssa.OpReturn})

		_, _, _, err := compile.Lower(b.Build(), new(machine), function(0, 0), nil, 0, false, true)
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})
}
