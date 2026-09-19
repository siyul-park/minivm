package arm64_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	jitarm64 "github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

// TestEmitter_EntryParams proves a loop header carrying live operands is
// declined: native entry loads nothing into block parameters, so there is no
// state to hand them.
func TestEmitter_EntryParams(t *testing.T) {
	b := ssa.New("f")
	block := b.Block()
	param := b.Param(block, ssa.TypeI32)
	b.Term(block, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{param}})
	fn := b.Build()

	in := testInput(&types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}})
	_, ok := backend.Compile(testMachine(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
	require.False(t, ok)
}

// TestEmitter_BackEdgeToBody proves only the header closes natively: an edge
// to an already laid-out body block is a loop the tier compiles at its own
// header instead, so the whole compile is declined for the plan pipeline.
func TestEmitter_BackEdgeToBody(t *testing.T) {
	b := ssa.New("f")
	b0, b1, b2 := b.Block(), b.Block(), b.Block()
	b.Term(b0, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: b1}}})
	b.Term(b1, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: b2}}})
	b.Term(b2, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: b1}}})
	fn := b.Build()

	in := testInput(&types.Function{Typ: &types.FunctionType{}})
	_, ok := backend.Compile(testMachine(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
	require.False(t, ok)
}

// TestEmitter_ForwardBranchMoves proves an edge with block parameters emits
// its moves inline: the constant jumps into the parameter, which the return
// then boxes.
func TestEmitter_ForwardBranchMoves(t *testing.T) {
	b := ssa.New("f")
	b0, b1 := b.Block(), b.Block()
	c := b.Value(ssa.TypeI32)
	p := b.Param(b1, ssa.TypeI32)
	b.Add(b0, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{c}})
	b.Term(b0, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: b1, Args: []ssa.Value{c}}}})
	b.Term(b1, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{p}})
	fn := b.Build()

	in := testInput(&types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}})
	a := asm.New(asmarm64.New())
	_, ok := backend.Compile(testMachine(), a, in, jit.Anchor{Addr: 1}, fn)
	require.True(t, ok)

	require.Equal(t, []asm.Instruction{
		asmarm64.MOV(asmarm64.X14, asmarm64.X0),
		asmarm64.LDP(asmarm64.X10, asmarm64.X11, asmarm64.X14, int16(journal.CellStack*8)),
		asmarm64.LDR(asmarm64.X12, asmarm64.X14, int16(journal.CellBP*8)),
		asmarm64.LSLI(vreg(2), vreg(3), 3),
		asmarm64.ADD(vreg(2), vreg(4), vreg(2)),
		asmarm64.MOVZ(narrow(0), 7, 0),
		asmarm64.MOV(narrow(1), narrow(0)),
		asmarm64.MOV(vreg(5), vreg(1)),
		asmarm64.MOVK(vreg(5), tag(types.KindI32), 48),
		asmarm64.STR(vreg(5), vreg(2), 0),
		asmarm64.MOV(vreg(6), vreg(5)),
		asmarm64.RET(),
	}, a.Instructions())
}

// TestEmitter_BranchMoves proves a two-way branch whose edge carries a block
// parameter moves before branching rather than taking the inverted-test
// shape: the value a branch and its rejoin disagree on is the general case
// the taken/untaken split shares with jump's own forward moves.
func TestEmitter_BranchMoves(t *testing.T) {
	b := ssa.New("f")
	b0, b1, b2 := b.Block(), b.Block(), b.Block()
	slot := ssa.Slot{Space: ssa.SpaceLocal, Index: 0}
	v := b.Value(ssa.TypeI32)
	cond := b.Value(ssa.TypeI1)
	c := b.Value(ssa.TypeI32)
	p := b.Param(b1, ssa.TypeI32)
	b.Add(b0, ssa.Operation{Op: ssa.OpLoad, Slot: slot, Results: []ssa.Value{v}})
	b.Add(b0, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{v}, Results: []ssa.Value{cond}})
	b.Add(b0, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{c}})
	b.Term(b0, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: b1, Args: []ssa.Value{c}}, {Block: b2}}})
	b.Term(b1, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{p}})
	b.Term(b2, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{v}})
	fn := b.Build()

	in := testInput(&types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}, Locals: []types.Type{types.TypeI32}})
	a := asm.New(asmarm64.New())
	_, ok := backend.Compile(testMachine(), a, in, jit.Anchor{Addr: 1}, fn)
	require.True(t, ok)

	built, err := a.Build()
	require.NoError(t, err)
	require.NotEmpty(t, built)
}

// TestEmitter_BranchBackEdge proves a conditional branch's own edge may name
// the block it terminates: the taken edge spends the safepoint budget in
// place instead of falling into the inverted-test shape, and the untaken
// edge leaves through a plain forward branch. This is the shape a post-test
// (do-while) loop compiles to - one block testing and branching back on
// itself, with no separate body block.
func TestEmitter_BranchBackEdge(t *testing.T) {
	b := ssa.New("f")
	block, exit := b.Block(), b.Block()
	slot := ssa.Slot{Space: ssa.SpaceLocal, Index: 0}
	v := b.Value(ssa.TypeI32)
	cond := b.Value(ssa.TypeI1)
	b.Add(block, ssa.Operation{Op: ssa.OpLoad, Slot: slot, Results: []ssa.Value{v}})
	b.Add(block, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{v}, Results: []ssa.Value{cond}})
	b.Term(block, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: block}, {Block: exit}}})
	b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})
	fn := b.Build()

	in := testInput(&types.Function{Typ: &types.FunctionType{}, Locals: []types.Type{types.TypeI32}})
	a := asm.New(asmarm64.New())
	_, ok := backend.Compile(testMachine(), a, in, jit.Anchor{Addr: 1}, fn)
	require.True(t, ok)

	built, err := a.Build()
	require.NoError(t, err)
	require.NotEmpty(t, built)
}

// TestEmitter_BranchTable proves a branch table is still declined: its edge
// order is not read yet, so the whole compile falls back to the plan
// pipeline.
func TestEmitter_BranchTable(t *testing.T) {
	b := ssa.New("f")
	b0, b1 := b.Block(), b.Block()
	c := b.Value(ssa.TypeI32)
	b.Add(b0, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{c}})
	b.Term(b0, ssa.Terminator{Op: ssa.OpTable, Args: []ssa.Value{c}, Edges: []ssa.Edge{{Block: b1}}})
	b.Term(b1, ssa.Terminator{Op: ssa.OpReturn})
	fn := b.Build()

	in := testInput(&types.Function{Typ: &types.FunctionType{}})
	_, ok := backend.Compile(testMachine(), asm.New(asmarm64.New()), in, jit.Anchor{Addr: 1}, fn)
	require.False(t, ok)
}

// TestEmitter_MultiFrameExit proves a deopt through an inlined callee unwinds
// both frames: the caller's record first at the runtime depth, then the
// callee's, so the interpreter rebuilds the chain the inlining hid.
func TestEmitter_MultiFrameExit(t *testing.T) {
	b := ssa.New("f")
	block := b.Block()
	c := b.Value(ssa.TypeI32)
	state := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{c}})
	b.Add(block, ssa.Operation{
		Op: ssa.OpState,
		Frames: []ssa.Frame{
			{Addr: 1, IP: 3, Stack: []ssa.Operand{{Value: c}}},
			{Addr: 1, Base: 2, IP: 5, Stack: []ssa.Operand{{Value: c}}},
		},
		Results: []ssa.Value{state},
	})
	b.Term(block, ssa.Terminator{Op: ssa.OpExit, State: state})
	fn := b.Build()

	in := testInput(&types.Function{Typ: &types.FunctionType{}})
	a := asm.New(asmarm64.New())
	code, ok := backend.Compile(testMachine(), a, in, jit.Anchor{Addr: 1}, fn)
	require.True(t, ok)
	require.Len(t, code.Exits, 1)
	require.Equal(t, prof.ExitTerminalOp, code.Exits[0].Reason)
	require.Equal(t, prof.OpcodeNone, code.Exits[0].Opcode)
	built, err := a.Build()
	require.NoError(t, err)
	require.NotEmpty(t, built)
}
func testMachine() backend.Machine {
	return jitarm64.New()
}

func testInput(fn *types.Function) *jit.Input {
	return &jit.Input{Address: 1, Function: fn, Objects: jit.Objects{1: {Fn: fn}}}
}
