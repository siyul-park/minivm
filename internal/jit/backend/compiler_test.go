package backend_test

import (
	"slices"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// machine is a backend.Machine that records what it was asked to lower rather
// than emitting anything, so a test states the block order, the register
// assignment, and the deopt metadata the compiler produced. It is its own
// Lowering, since one compile is all a test runs.
type machine struct {
	declines []instr.Opcode
	fuse     int
	stops    string
	guard    prof.ExitReason
	opcode   int

	compiler *backend.Compiler
	lowered  []int
	ops      []ssa.Op
	ended    []int
	deopts   []backend.Deopt
	moves    [][]backend.Move
}

func (m *machine) Lowers(code instr.Opcode) bool {
	return !slices.Contains(m.declines, code)
}

func (m *machine) Open(c *backend.Compiler) backend.Lowering {
	m.compiler = c
	return m
}

func (m *machine) Enter() bool {
	return m.stops != "enter"
}

func (m *machine) Lower(block int, ops []ssa.Operation) (int, bool) {
	if m.stops == "lower" {
		return 0, false
	}
	m.lowered = append(m.lowered, block)
	m.ops = append(m.ops, ops[0].Op)
	if ops[0].State != ssa.NoValue {
		m.deopts = append(m.deopts, m.compiler.Exit(ops[0].State, m.guard, m.opcode))
	}
	if m.fuse > 1 && len(ops) >= m.fuse {
		return m.fuse, true
	}
	return 1, true
}

func (m *machine) Term(block int, t ssa.Terminator) bool {
	if m.stops == "term" {
		return false
	}
	m.ended = append(m.ended, block)
	for _, edge := range t.Edges {
		m.moves = append(m.moves, m.compiler.Moves(edge))
	}
	return true
}

func (m *machine) Leave() bool {
	return m.stops != "leave"
}

func TestCompile(t *testing.T) {
	t.Run("lowers every operation and ends every block in layout order", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, join := b.Block(), b.Block(), b.Block(), b.Block()
		cond := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{cond}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Term(left, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(right, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{}
		code, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, f)
		require.True(t, ok)
		require.Equal(t, []int{entry, left, right, join}, code.Order)
		require.Equal(t, []int{entry, left, right, join}, m.ended)
		require.Equal(t, []int{entry}, m.lowered)
		require.Empty(t, code.Exits)
	})

	t.Run("calls a lowering again from the first operation it left", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		for i := 0; i < 4; i++ {
			v := b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(int32(i)), Results: []ssa.Value{v}})
		}
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{fuse: 3}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, f)
		require.True(t, ok)
		require.Len(t, m.ops, 2)
	})

	t.Run("refuses a function holding an operation the machine declines", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{v, v}, Results: []ssa.Value{b.Value(ssa.TypeI32)}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

		m := &machine{declines: []instr.Opcode{instr.I32_ADD}}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, b.Build())
		require.False(t, ok)
		require.Nil(t, m.compiler)
	})

	t.Run("abandons the compile the first time lowering reports failure", func(t *testing.T) {
		for _, stop := range []string{"enter", "lower", "term", "leave"} {
			b := ssa.New("f")
			entry := b.Block()
			v := b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{v}})
			b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

			m := &machine{stops: stop}
			_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, b.Build())
			require.False(t, ok, stop)
		}
	})

	t.Run("refuses a function with no block", func(t *testing.T) {
		_, ok := backend.Compile(&machine{}, asm.New(arm64.New()), &jit.Input{}, ssa.New("f").Build())
		require.False(t, ok)
	})
}

func TestCompiler_Asm(t *testing.T) {
	b := ssa.New("f")
	entry := b.Block()
	b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

	assembler := asm.New(arm64.New())
	m := &machine{}
	_, ok := backend.Compile(m, assembler, &jit.Input{}, b.Build())
	require.True(t, ok)
	require.Same(t, assembler, m.compiler.Asm())
}

func TestCompiler_Input(t *testing.T) {
	b := ssa.New("f")
	entry := b.Block()
	b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

	input := &jit.Input{Address: 3}
	m := &machine{}
	_, ok := backend.Compile(m, asm.New(arm64.New()), input, b.Build())
	require.True(t, ok)
	require.Same(t, input, m.compiler.Input())
}

func TestCompiler_Func(t *testing.T) {
	b := ssa.New("f")
	entry := b.Block()
	b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

	f := b.Build()
	m := &machine{}
	_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, f)
	require.True(t, ok)
	require.Same(t, f, m.compiler.Func())
}

func TestCompiler_Reg(t *testing.T) {
	t.Run("binds one register of each value's own lane", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		i32, i64 := b.Value(ssa.TypeI32), b.Value(ssa.TypeI64)
		f32, f64 := b.Value(ssa.TypeF32), b.Value(ssa.TypeF64)
		ref := b.Value(ssa.TypeRef)
		for _, v := range []ssa.Value{i32, i64, f32, f64, ref} {
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{v}})
		}
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

		m := &machine{}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, b.Build())
		require.True(t, ok)

		c := m.compiler
		require.Equal(t, asm.NewVReg(0, asm.RegTypeInt, asm.Width64), c.Reg(i32))
		require.Equal(t, asm.NewVReg(1, asm.RegTypeInt, asm.Width64), c.Reg(i64))
		require.Equal(t, asm.NewVReg(2, asm.RegTypeFloat, asm.Width32), c.Reg(f32))
		require.Equal(t, asm.NewVReg(3, asm.RegTypeFloat, asm.Width64), c.Reg(f64))
		require.Equal(t, asm.NewVReg(4, asm.RegTypeInt, asm.Width64), c.Reg(ref))
	})

	t.Run("binds no register to interpreter state or an absent value", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

		m := &machine{}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{Objects: jit.Objects{1: {Fn: &types.Function{}}}}, b.Build())
		require.True(t, ok)
		require.Equal(t, asm.VReg{}, m.compiler.Reg(state))
		require.Equal(t, asm.VReg{}, m.compiler.Reg(ssa.NoValue))
		require.Equal(t, asm.VReg{}, m.compiler.Reg(ssa.Value(99)))
	})
}

func TestCompiler_Def(t *testing.T) {
	b := ssa.New("f")
	entry, join := b.Block(), b.Block()
	konst := b.Value(ssa.TypeI32)
	b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{konst}})
	b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{konst}}}})
	param := b.Param(join, ssa.TypeI32)
	b.Term(join, ssa.Terminator{Op: ssa.OpComplete})
	f := b.Build()
	require.NoError(t, ssa.Verify(f))

	m := &machine{}
	_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, f)
	require.True(t, ok)

	def, ok := m.compiler.Def(konst)
	require.True(t, ok)
	require.Equal(t, ssa.OpConst, def.Op)
	require.Equal(t, types.BoxI32(7), def.Const)

	_, ok = m.compiler.Def(param)
	require.False(t, ok)

	_, ok = m.compiler.Def(ssa.NoValue)
	require.False(t, ok)
}

func TestCompiler_Block(t *testing.T) {
	b := ssa.New("f")
	entry, join := b.Block(), b.Block()
	b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
	b.Term(join, ssa.Terminator{Op: ssa.OpComplete})

	m := &machine{}
	assembler := asm.New(arm64.New())
	_, ok := backend.Compile(m, assembler, &jit.Input{}, b.Build())
	require.True(t, ok)
	require.NotEqual(t, m.compiler.Block(entry), m.compiler.Block(join))

	// A branch to a block's label resolves, which is what proves the compiler
	// bound it at that block's own start.
	assembler.Emit(arm64.BLabel(m.compiler.Block(join)), arm64.RET())
	code, err := assembler.Build()
	require.NoError(t, err)
	require.NotEmpty(t, code)
}

func TestCompiler_Next(t *testing.T) {
	b := ssa.New("f")
	entry, join := b.Block(), b.Block()
	b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
	b.Term(join, ssa.Terminator{Op: ssa.OpComplete})

	m := &machine{}
	_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, b.Build())
	require.True(t, ok)

	next, ok := m.compiler.Next(entry)
	require.True(t, ok)
	require.Equal(t, join, next)

	_, ok = m.compiler.Next(join)
	require.False(t, ok)
}

func TestCompiler_Moves(t *testing.T) {
	t.Run("copies each argument into its parameter", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		one, two := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{two}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{one, two}}}})
		left, right := b.Param(join, ssa.TypeI32), b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, f)
		require.True(t, ok)

		c := m.compiler
		require.Equal(t, [][]backend.Move{{
			{Dst: c.Reg(left), Src: c.Reg(one)},
			{Dst: c.Reg(right), Src: c.Reg(two)},
		}}, m.moves)
	})

	t.Run("leaves out a copy the register already satisfies", func(t *testing.T) {
		b := ssa.New("f")
		entry, header := b.Block(), b.Block()
		seed := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{seed}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{seed}}}})
		carried := b.Param(header, ssa.TypeI32)
		b.Term(header, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{carried}}}})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, f)
		require.True(t, ok)
		require.Equal(t, [][]backend.Move{
			{{Dst: m.compiler.Reg(carried), Src: m.compiler.Reg(seed)}},
			nil,
		}, m.moves)
	})

	t.Run("parks one value in a temporary to break a cycle", func(t *testing.T) {
		b := ssa.New("f")
		entry, header := b.Block(), b.Block()
		one, two := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{two}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{one, two}}}})
		left, right := b.Param(header, ssa.TypeI32), b.Param(header, ssa.TypeI32)
		b.Term(header, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{right, left}}}})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, f)
		require.True(t, ok)

		c := m.compiler
		swap := m.moves[1]
		require.Len(t, swap, 3)
		tmp := swap[0].Dst
		require.NotEqual(t, c.Reg(left), tmp)
		require.NotEqual(t, c.Reg(right), tmp)
		require.Equal(t, []backend.Move{
			{Dst: tmp, Src: c.Reg(right)},
			{Dst: c.Reg(right), Src: c.Reg(left)},
			{Dst: c.Reg(left), Src: tmp},
		}, swap)
	})

	t.Run("copies nothing for an edge that names no block of this function", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

		m := &machine{}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, b.Build())
		require.True(t, ok)
		require.Nil(t, m.compiler.Moves(ssa.Edge{Block: 7}))
	})
}
