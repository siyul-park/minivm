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
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayLen, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{length}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("accepts module code completing with operands still on the stack", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		value := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermComplete, Args: []ssa.Value{value}})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("accepts a return resuming into an interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		value := b.Value(ssa.TypeI64)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(1), Results: []ssa.Value{value}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Stack: []ssa.Operand{{Value: value}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{value}, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a return resuming into a value that is no state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		value := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(1), Results: []ssa.Value{value}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{value}, State: value})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects a function with no entry block", func(t *testing.T) {
		require.ErrorIs(t, ssa.Verify(ssa.New("f").Build()), ssa.ErrForm)
	})

	t.Run("rejects a block the entry cannot reach", func(t *testing.T) {
		b := ssa.New("f")
		entry, orphan := b.Block(), b.Block()
		b.Term(entry, ssa.Terminator{Term: ssa.TermComplete})
		b.Term(orphan, ssa.Terminator{Term: ssa.TermComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an edge naming a block that does not exist", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: 7}}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an edge count its terminator cannot have", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join}, {Block: join}}})
		b.Term(join, ssa.Terminator{Term: ssa.TermComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an operation used where a terminator belongs", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit + 1})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects a control opcode used where an operation belongs", func(t *testing.T) {
		for _, c := range []struct {
			name string
			code instr.Opcode
		}{
			{"tail call", instr.RETURN_CALL},
			{"return", instr.RETURN},
			{"branch", instr.BR_IF},
		} {
			b := ssa.New("f")
			entry := b.Block()
			state := b.Value(ssa.TypeState)
			callee := b.Value(ssa.TypeRef)
			b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(2)), Results: []ssa.Value{callee}})
			b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: c.code, Args: []ssa.Value{callee}, State: state})
			b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
			require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm, c.name)
		}
	})

	t.Run("rejects a value defined twice", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{one}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDefine)
	})

	t.Run("rejects a value no instruction defines", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		b.Value(ssa.TypeI32)
		b.Term(entry, ssa.Terminator{Term: ssa.TermComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDefine)
	})

	t.Run("rejects a use ahead of its definition in the same block", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		one := b.Value(ssa.TypeI32)
		doubled := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{one, one}, State: state, Results: []ssa.Value{doubled}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{doubled}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDominate)
	})

	t.Run("rejects a use its definition does not dominate", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right := b.Block(), b.Block(), b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		cond := b.Value(ssa.TypeI32)
		one := b.Value(ssa.TypeI32)
		doubled := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{cond}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		b.Add(left, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Term(left, ssa.Terminator{Term: ssa.TermComplete})
		b.Add(right, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{one, one}, State: state, Results: []ssa.Value{doubled}})
		b.Term(right, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{doubled}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDominate)
	})

	t.Run("rejects an edge that misses a block parameter", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join}}})
		param := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{param}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an edge argument of another type than its parameter", func(t *testing.T) {
		b := ssa.New("f")
		entry, join := b.Block(), b.Block()
		null := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxedNull), Results: []ssa.Value{null}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join, Args: []ssa.Value{null}}}})
		param := b.Param(join, ssa.TypeI32)
		b.Term(join, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{param}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("accepts an array read through the slice of a guarded array", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		array := b.Value(ssa.TypeRef)
		checked := b.Value(ssa.TypeRef)
		sliced := b.Value(ssa.TypeRef)
		length := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindI32}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{checked}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{checked}, Results: []ssa.Value{sliced}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayLen, Args: []ssa.Value{sliced}, State: state, Results: []ssa.Value{length}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a slice of a value that is no reference", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		index := b.Value(ssa.TypeI32)
		sliced := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{index}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{index}, Results: []ssa.Value{sliced}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("accepts an array read at an index bounded within a slice a bounds guard covers", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		array, checked, sliced := b.Value(ssa.TypeRef), b.Value(ssa.TypeRef), b.Value(ssa.TypeRef)
		limit, length, index, bounded, element := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindI32}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{checked}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{checked}, Results: []ssa.Value{sliced}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 4, Results: []ssa.Value{limit}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayLen, Args: []ssa.Value{sliced}, State: state, Results: []ssa.Value{length}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{limit, length}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{index}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayBound, Args: []ssa.Value{index, sliced}, Results: []ssa.Value{bounded}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{sliced, bounded}, State: state, Results: []ssa.Value{element}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a bound whose index is no i32 or whose container is no reference", func(t *testing.T) {
		for _, typs := range [][2]ssa.Type{{ssa.TypeI64, ssa.TypeRef}, {ssa.TypeI32, ssa.TypeI32}} {
			b := ssa.New("f")
			entry := b.Block()
			state := b.Value(ssa.TypeState)
			index, container, bounded := b.Value(typs[0]), b.Value(typs[1]), b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
			b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{index}})
			b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 1}, Results: []ssa.Value{container}})
			b.Add(entry, ssa.Operation{Op: ssa.OpArrayBound, Args: []ssa.Value{index, container}, Results: []ssa.Value{bounded}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
			require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
		}
	})

	t.Run("rejects a bounds guard over a reference", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		limit, array := b.Value(ssa.TypeI32), b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 4, Results: []ssa.Value{limit}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{limit, array}, State: state})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a guard with no interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Value(ssa.TypeRef)
		checked := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Args: []ssa.Value{array}, Results: []ssa.Value{checked}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{checked}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("accepts a call reaching its callee through the reference a guard admits", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		slot := b.Value(ssa.TypeRef)
		want := b.Value(ssa.TypeRef)
		callee := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal}, Results: []ssa.Value{slot}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(2)), Results: []ssa.Value{want}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{slot, want}, State: state, Results: []ssa.Value{callee}})
		b.Add(entry, ssa.Operation{Op: ssa.OpCall, Args: []ssa.Value{callee}, State: state})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a guard admitting a value no observation fixed", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		slot := b.Value(ssa.TypeRef)
		want := b.Value(ssa.TypeRef)
		callee := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal}, Results: []ssa.Value{slot}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 1}, Results: []ssa.Value{want}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{slot, want}, State: state, Results: []ssa.Value{callee}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{callee}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an interpreter state on an operation that cannot resume", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		one := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, State: state, Results: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{one}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("accepts a frame owning the reference it resumes with", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		array := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRefRetain, Args: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Stack: []ssa.Operand{{Value: array, Owned: true}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a frame owning a value that holds no reference", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		count := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{count}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Stack: []ssa.Operand{{Value: count, Owned: true}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("accepts a frame carrying a local value", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		count := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{count}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Locals: []ssa.Local{{Index: 0, Value: count}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a local value holding a reference", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		array := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Locals: []ssa.Local{{Index: 0, Value: array}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a local value naming no slot of its frame", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		count := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{count}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Locals: []ssa.Local{{Index: -1, Value: count}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects a local value its definition does not dominate", func(t *testing.T) {
		b := ssa.New("f")
		entry, arm, join := b.Block(), b.Block(), b.Block()
		cond := b.Value(ssa.TypeI1)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{cond}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: arm}, {Block: join}}})
		count := b.Value(ssa.TypeI32)
		b.Add(arm, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{count}})
		b.Term(arm, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: join}}})
		state := b.Value(ssa.TypeState)
		b.Add(join, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Locals: []ssa.Local{{Index: 0, Value: count}}}}, Results: []ssa.Value{state}})
		b.Term(join, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrDominate)
	})

	t.Run("rejects an interpreter state with no frame", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects a result typed as interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		fake := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{fake}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a reference operation over a scalar", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRefRetain, Args: []ssa.Value{one}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects an operand count the opcode cannot have", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		one := b.Value(ssa.TypeI32)
		sum := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{one}, State: state, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an operation performing no opcode", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.Opcode(0xff), State: state})
		b.Term(entry, ssa.Terminator{Term: ssa.TermComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrForm)
	})

	t.Run("rejects an operand of a kind the opcode does not pop", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		null := b.Value(ssa.TypeRef)
		one := b.Value(ssa.TypeI32)
		sum := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxedNull), Results: []ssa.Value{null}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{null, one}, State: state, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a result of a kind the opcode does not push", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		one := b.Value(ssa.TypeI32)
		sum := b.Value(ssa.TypeF64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{one, one}, State: state, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects a heap overwrite with no interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		array := b.Value(ssa.TypeRef)
		index := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{index}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{array, index, index}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermComplete})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("rejects a select whose arms disagree", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		cond := b.Value(ssa.TypeI32)
		null := b.Value(ssa.TypeRef)
		picked := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{cond}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxedNull), Results: []ssa.Value{null}})
		b.Add(entry, ssa.Operation{Op: ssa.OpSelect, Args: []ssa.Value{cond, cond, null}, State: state, Results: []ssa.Value{picked}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{picked}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType)
	})

	t.Run("rejects an operation performing an opcode with no interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrState)
	})

	t.Run("accepts an operation resuming into an interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		x, y, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: state, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		require.NoError(t, ssa.Verify(b.Build()))
	})

	t.Run("rejects a const wider than its type", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			typ   ssa.Type
			value uint64
		}{
			{"i32 with a nonzero upper half", ssa.TypeI32, 1 << 32},
			{"i1 of 2", ssa.TypeI1, 2},
		} {
			b := ssa.New("f")
			entry := b.Block()
			x := b.Value(c.typ)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.value, Results: []ssa.Value{x}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{x}})
			require.ErrorIs(t, ssa.Verify(b.Build()), ssa.ErrType, c.name)
		}
	})

	// data builds entry with typed params, one data operation over them, and
	// an exit, returning the verification error; state omits the resume state.
	data := func(op ssa.Operation, params []ssa.Type, results []ssa.Type, state bool) error {
		b := ssa.New("f")
		entry := b.Block()
		for _, typ := range params {
			op.Args = append(op.Args, b.Param(entry, typ))
		}
		for _, typ := range results {
			op.Results = append(op.Results, b.Value(typ))
		}
		at := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{at}})
		if state {
			op.State = at
		}
		b.Add(entry, op)
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: at})
		return ssa.Verify(b.Build())
	}
	i32, i64, f64, ref, i1 := ssa.TypeI32, ssa.TypeI64, ssa.TypeF64, ssa.TypeRef, ssa.TypeI1

	t.Run("accepts well-typed data operations", func(t *testing.T) {
		require.NoError(t, data(ssa.Operation{Op: ssa.OpIntAdd}, []ssa.Type{i32, ssa.TypeI8}, []ssa.Type{i32}, true))
		require.NoError(t, data(ssa.Operation{Op: ssa.OpIntCmp, Cond: ssa.CondSLT}, []ssa.Type{i64, i64}, []ssa.Type{i1}, true))
		require.NoError(t, data(ssa.Operation{Op: ssa.OpFloatCmp, Cond: ssa.CondOLT}, []ssa.Type{f64, f64}, []ssa.Type{i1}, true))
		require.NoError(t, data(ssa.Operation{Op: ssa.OpFloatAdd}, []ssa.Type{f64, f64}, []ssa.Type{f64}, true))
		require.NoError(t, data(ssa.Operation{Op: ssa.OpRefCmp, Cond: ssa.CondEQ}, []ssa.Type{ref, ref}, []ssa.Type{i1}, true))
		require.NoError(t, data(ssa.Operation{Op: ssa.OpIntSExt}, []ssa.Type{i32}, []ssa.Type{i64}, true))
		require.NoError(t, data(ssa.Operation{Op: ssa.OpSelect}, []ssa.Type{f64, f64, i32}, []ssa.Type{f64}, true))
	})

	t.Run("restricts int.sext32 to i64", func(t *testing.T) {
		require.NoError(t, data(ssa.Operation{Op: ssa.OpIntSExt32}, []ssa.Type{i64}, []ssa.Type{i64}, true))
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpIntSExt32}, []ssa.Type{i32}, []ssa.Type{i32}, true), ssa.ErrType)
	})

	t.Run("rejects a data operation without a state", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpIntAdd}, []ssa.Type{i32, i32}, []ssa.Type{i32}, false), ssa.ErrState)
	})

	t.Run("rejects float.add on i32", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpFloatAdd}, []ssa.Type{i32, i32}, []ssa.Type{i32}, true), ssa.ErrType)
	})

	t.Run("rejects int.add on f64", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpIntAdd}, []ssa.Type{f64, f64}, []ssa.Type{f64}, true), ssa.ErrType)
	})

	t.Run("rejects int.add across lanes", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpIntAdd}, []ssa.Type{i32, i64}, []ssa.Type{i32}, true), ssa.ErrType)
	})

	t.Run("rejects int.cmp with a float condition", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpIntCmp, Cond: ssa.CondOLT}, []ssa.Type{i32, i32}, []ssa.Type{i1}, true), ssa.ErrType)
	})

	t.Run("rejects float.cmp with an integer condition", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpFloatCmp, Cond: ssa.CondSLT}, []ssa.Type{f64, f64}, []ssa.Type{i1}, true), ssa.ErrType)
	})

	t.Run("rejects ref.cmp slt", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpRefCmp, Cond: ssa.CondSLT}, []ssa.Type{ref, ref}, []ssa.Type{i1}, true), ssa.ErrType)
	})

	t.Run("rejects a condition on a non-compare operation", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpIntAdd, Cond: ssa.CondEQ}, []ssa.Type{i32, i32}, []ssa.Type{i32}, true), ssa.ErrForm)
	})

	t.Run("rejects a code on a non-exec operation", func(t *testing.T) {
		require.ErrorIs(t, data(ssa.Operation{Op: ssa.OpIntAdd, Code: instr.I32_ADD}, []ssa.Type{i32, i32}, []ssa.Type{i32}, true), ssa.ErrForm)
	})

	t.Run("rejects a call signature admitted by guard.shape", func(t *testing.T) {
		err := data(ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormSignature, Type: 1}}, []ssa.Type{ref}, []ssa.Type{ref}, true)
		require.ErrorIs(t, err, ssa.ErrForm)
	})
}
