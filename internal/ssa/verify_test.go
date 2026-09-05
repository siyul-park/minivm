package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	t.Run("accepts a guarded read that resumes into an interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		array := b.Value(ssa.TypeRef)
		length := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{array}, Results: []ssa.Value{length}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("accepts module code completing with operands still on the stack", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		value := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete, Args: []ssa.Value{value}})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a function with no entry block", func(t *testing.T) {
		require.ErrorIs(t, ssa.Verify(ssa.New("f").Build()), ssa.ErrForm)
	})

	t.Run("rejects a block the entry cannot reach", func(t *testing.T) {
		b := ssa.New("f")
		entry, orphan := b.Block(), b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		b.Term(orphan, ssa.Terminator{Op: ssa.OpComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an edge naming a block that does not exist", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: 7}}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an edge count its terminator cannot have", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}, {Block: join}}})
		b.Term(join, ssa.Terminator{Op: ssa.OpComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an operation used where a terminator belongs", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpConst})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects a value defined twice", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{one}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDefine)
	})

	t.Run("rejects a value no instruction defines", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Value(ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDefine)
	})

	t.Run("rejects a use ahead of its definition in the same block", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		doubled := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{one, one}, Results: []ssa.Value{doubled}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{doubled}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDominate)
	})

	t.Run("rejects a use its definition does not dominate", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right := b.Block(), b.Block(), b.Block()
		cond := b.Value(ssa.TypeI32)
		one := b.Value(ssa.TypeI32)
		doubled := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{cond}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Add(left, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Term(left, ssa.Terminator{Op: ssa.OpComplete})
		b.Add(right, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{one, one}, Results: []ssa.Value{doubled}})
		b.Term(right, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{doubled}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDominate)
	})

	t.Run("rejects an edge that misses a block parameter", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join}}})
		param := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{param}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an edge argument of another type than its parameter", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		null := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxedNull, Results: []ssa.Value{null}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{null}}}})
		param := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{param}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a guard with no interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Value(ssa.TypeRef)
		checked := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Args: []ssa.Value{array}, Results: []ssa.Value{checked}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{checked}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects an interpreter state on an operation that cannot resume", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		one := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), State: state, Results: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{one}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects a suspension from an inlined frame", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}, {Addr: 2}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpSuspend, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("accepts a frame owning the reference it resumes with", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		array := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, Stack: []ssa.Operand{{Value: array, Owned: true}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a frame owning a value that holds no reference", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		count := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{count}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, Stack: []ssa.Operand{{Value: count, Owned: true}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects an interpreter state with no frame", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects a result typed as interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		fake := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{fake}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a reference operation over a scalar", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects an operand count the opcode cannot have", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		sum := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{one}, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{sum}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an operation performing no opcode", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.Opcode(0xff)})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an operand of a kind the opcode does not pop", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		null := b.Value(ssa.TypeRef)
		one := b.Value(ssa.TypeI32)
		sum := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxedNull, Results: []ssa.Value{null}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{null, one}, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{sum}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a result of a kind the opcode does not push", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		sum := b.Value(ssa.TypeF64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{one, one}, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{sum}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a heap overwrite with no interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Value(ssa.TypeRef)
		index := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{index}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_SET, Args: []ssa.Value{array, index, index}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects a select whose arms disagree", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		cond := b.Value(ssa.TypeI32)
		null := b.Value(ssa.TypeRef)
		picked := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{cond}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxedNull, Results: []ssa.Value{null}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.SELECT, Args: []ssa.Value{cond, cond, null}, Results: []ssa.Value{picked}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{picked}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})
}
