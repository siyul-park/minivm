package transform_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

type countedLoop struct {
	b                         *ssa.Builder
	pre, header, body, exit   int
	bound, one, counter, cond ssa.Value
}

func TestNewHoistPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewHoistPass()
		require.NotNil(t, p)
	})
}

func TestHoistPass_Run(t *testing.T) {
	t.Run("hoists a pure computation over invariant arguments into the loop's preheader", func(t *testing.T) {
		l := newCountedLoop()
		x, y := l.b.Value(ssa.TypeI32), l.b.Value(ssa.TypeI32)
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{x}})
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 3, Results: []ssa.Value{y}})
		sum := l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, State: deoptState(l.b, l.pre), Results: []ssa.Value{sum}})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		after := ssa.Format(fn)
		require.NotEqual(t, before, after)
		require.Equal(t, 1, strings.Count(blockChunk(after, 0), "i32.add"))
		require.Equal(t, 2, strings.Count(after, "i32.add"))
	})

	t.Run("does not hoist an operation with loop-local state", func(t *testing.T) {
		l := newCountedLoop()
		state := l.b.Value(ssa.TypeState)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 4}}, Results: []ssa.Value{state}})
		x, y := l.b.Value(ssa.TypeI32), l.b.Value(ssa.TypeI32)
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{x}})
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 3, Results: []ssa.Value{y}})
		sum := l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, State: state, Results: []ssa.Value{sum}})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.True(t, hasCode(fn.Block(l.body).Operations, instr.I32_ADD))
		require.False(t, hasCode(fn.Block(l.pre).Operations, instr.I32_ADD))
	})

	t.Run("does not hoist an operation whose argument is loop-variant", func(t *testing.T) {
		l := newCountedLoop()
		doubled := l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{l.counter, l.counter}, State: deoptState(l.b, l.pre), Results: []ssa.Value{doubled}})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.True(t, hasCode(fn.Block(l.body).Operations, instr.I32_ADD))
	})

	t.Run("does not hoist a heap read a loop's own heap write could invalidate", func(t *testing.T) {
		l := newCountedLoop()
		array := l.b.Param(l.pre, ssa.TypeRef)
		length := l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{array}, State: deoptState(l.b, l.pre), Results: []ssa.Value{length}})
		state := l.b.Value(ssa.TypeState)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_SET, Args: []ssa.Value{array, l.one, length}, State: state})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.True(t, hasCode(fn.Block(l.body).Operations, instr.ARRAY_LEN))
	})

	t.Run("does not hoist a loop-invariant division that could fault on a zero divisor", func(t *testing.T) {
		l := newCountedLoop()
		divisor := l.b.Value(ssa.TypeI32)
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{divisor}})
		quot := l.b.Value(ssa.TypeI32)
		state := l.b.Value(ssa.TypeState)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{state}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{l.bound, divisor}, State: state, Results: []ssa.Value{quot}})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.True(t, hasCode(fn.Block(l.body).Operations, instr.I32_DIV_S))
	})

	t.Run("does not hoist out of a loop with no suitable preheader", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block(), b.Block(), b.Block()

		pick := b.Value(ssa.TypeI1)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{pick}})
		x := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{x}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{pick}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})

		zero := b.Value(ssa.TypeI32)
		b.Add(left, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Term(left, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}}})

		zero2 := b.Value(ssa.TypeI32)
		b.Add(right, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero2}})
		b.Term(right, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero2}}}})

		counter := b.Param(header, ssa.TypeI32)
		cond := b.Value(ssa.TypeI1)
		bound := b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{bound}})
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{counter, bound}, State: deoptState(b, entry), Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

		sum := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, x}, State: deoptState(b, entry), Results: []ssa.Value{sum}})
		next := b.Value(ssa.TypeI32)
		one := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, one}, State: deoptState(b, entry), Results: []ssa.Value{next}})
		b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}})

		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{counter}})

		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("hoists a pure operation but leaves a shape guard in a loop that calls", func(t *testing.T) {
		l := newCountedLoop()
		array := l.b.Param(l.pre, ssa.TypeRef)
		callee := l.b.Param(l.pre, ssa.TypeRef)
		x, y := l.b.Value(ssa.TypeI32), l.b.Value(ssa.TypeI32)
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{x}})
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 3, Results: []ssa.Value{y}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{callee}, State: deoptState(l.b, l.pre)})

		sum := l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, State: deoptState(l.b, l.pre), Results: []ssa.Value{sum}})
		state := l.b.Value(ssa.TypeState)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 4}}, Results: []ssa.Value{state}})
		guarded := l.b.Value(ssa.TypeRef)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{guarded}})
		length := l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{guarded}, State: deoptState(l.b, l.pre), Results: []ssa.Value{length}})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))

		out := ssa.Format(fn)
		require.Contains(t, out, "guard.shape")
		require.Contains(t, out, "state v")
		require.Equal(t, 1, strings.Count(blockChunk(out, 0), "i32.add"))
		require.Equal(t, 2, strings.Count(out, "i32.add"))
		require.NotContains(t, blockChunk(out, 0), "guard.shape")
	})

	t.Run("hoists a shape guard on an invariant ref into the preheader under the loop's entry state, and slices it there", func(t *testing.T) {
		b := ssa.New("f")
		pre, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block()
		array := b.Param(pre, ssa.TypeRef)
		zero, bound, one := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{bound}})
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Term(pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}}})

		counter := b.Param(header, ssa.TypeI32)
		entry := b.Value(ssa.TypeState)
		b.Add(header, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 2, Stack: []ssa.Operand{{Value: counter}, {Value: bound}}}}, Results: []ssa.Value{entry}})
		cond := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{counter, bound}, State: entry, Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

		at := b.Value(ssa.TypeState)
		b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 5, Stack: []ssa.Operand{{Value: array}, {Value: counter}}}}, Results: []ssa.Value{at}})
		guarded, element := b.Value(ssa.TypeRef), b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{array}, State: at, Results: []ssa.Value{guarded}})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{guarded, counter}, State: at, Results: []ssa.Value{element}})
		step := b.Value(ssa.TypeState)
		b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 6, Stack: []ssa.Operand{{Value: counter}, {Value: one}}}}, Results: []ssa.Value{step}})
		next := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, one}, State: step, Results: []ssa.Value{next}})
		b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{counter}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, `func f
blk0: (v1:ref)
	v2:i32 = const 0
	v3:i32 = const 10
	v4:i32 = const 1
	v9:state = state {addr=1 base=0 ip=2 returns=0 stack=[v2, v3]}
	v10:ref = guard.shape v1 kind i32 state v9
	v11:ref = slice v10
	jump blk1(v2)
blk1: (v5:i32) <-- (blk0, blk2)
	v6:state = state {addr=1 base=0 ip=2 returns=0 stack=[v5, v3]}
	v7:i1 = i32.lt_s v5, v3 state v6
	br v7, blk2(), blk3()
blk2: () <-- (blk1)
	v8:state = state {addr=1 base=0 ip=5 returns=0 stack=[v1, v5]}
	v12:i32 = array.get v11, v5 state v8
	v13:state = state {addr=1 base=0 ip=6 returns=0 stack=[v5, v4]}
	v14:i32 = i32.add v5, v4 state v13
	jump blk1(v14)
blk3: () <-- (blk1)
	return v5
`, ssa.Format(fn))
	})

	t.Run("leaves a shape guard in a conditional block of a quiet loop", func(t *testing.T) {
		b := ssa.New("f")
		pre, header, body, then, latch, exit := b.Block(), b.Block(), b.Block(), b.Block(), b.Block(), b.Block()
		array := b.Param(pre, ssa.TypeRef)
		zero, bound, one := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{bound}})
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Term(pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}}})

		counter := b.Param(header, ssa.TypeI32)
		entry := b.Value(ssa.TypeState)
		b.Add(header, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 2, Stack: []ssa.Operand{{Value: counter}, {Value: bound}}}}, Results: []ssa.Value{entry}})
		cond := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{counter, bound}, State: entry, Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

		taken := b.Value(ssa.TypeI1)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{counter, zero}, State: entry, Results: []ssa.Value{taken}})
		b.Term(body, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{taken}, Edges: []ssa.Edge{{Block: then}, {Block: latch}}})

		at := b.Value(ssa.TypeState)
		b.Add(then, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 5, Stack: []ssa.Operand{{Value: array}, {Value: counter}}}}, Results: []ssa.Value{at}})
		guarded, element := b.Value(ssa.TypeRef), b.Value(ssa.TypeI32)
		b.Add(then, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{array}, State: at, Results: []ssa.Value{guarded}})
		b.Add(then, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{guarded, counter}, State: at, Results: []ssa.Value{element}})
		b.Term(then, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: latch}}})

		next := b.Value(ssa.TypeI32)
		b.Add(latch, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, one}, State: entry, Results: []ssa.Value{next}})
		b.Term(latch, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{counter}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.NotContains(t, blockChunk(out, pre), "guard.shape")
		require.Contains(t, out, "guard.shape")
	})

	t.Run("hoists a load of a local the loop never stores, and the shape guard over it", func(t *testing.T) {
		l := newCountedLoop()
		array, guarded, element := l.b.Value(ssa.TypeRef), l.b.Value(ssa.TypeRef), l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Results: []ssa.Value{array}})
		state := l.b.Value(ssa.TypeState)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 4, Stack: []ssa.Operand{{Value: array}, {Value: l.counter}}}}, Results: []ssa.Value{state}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{guarded}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_SET, Args: []ssa.Value{guarded, l.counter, l.one}, State: state})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{guarded, l.counter}, State: state, Results: []ssa.Value{element}})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		pre := blockChunk(out, l.pre)
		require.Contains(t, pre, "load local[0]")
		require.Contains(t, pre, "guard.shape")
		require.Equal(t, 1, strings.Count(out, "slice"))
		require.Contains(t, pre, "slice")
		var sliced ssa.Value
		for _, op := range fn.Block(l.pre).Operations {
			if op.Op == ssa.OpSlice {
				sliced = op.Results[0]
			}
		}
		for _, op := range fn.Block(l.body).Operations {
			if op.Op == ssa.OpExec && (op.Code == instr.ARRAY_SET || op.Code == instr.ARRAY_GET) {
				require.Equal(t, sliced, op.Args[0])
			}
		}
	})

	t.Run("does not slice an invariant guarded array in a loop that releases", func(t *testing.T) {
		l := newCountedLoop()
		array, guarded, element := l.b.Param(l.pre, ssa.TypeRef), l.b.Value(ssa.TypeRef), l.b.Value(ssa.TypeRef)
		entry := l.b.Value(ssa.TypeState)
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{entry}})
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindRef}, Args: []ssa.Value{array}, State: entry, Results: []ssa.Value{guarded}})
		state := l.b.Value(ssa.TypeState)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 4}}, Results: []ssa.Value{state}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{guarded, l.counter}, State: state, Results: []ssa.Value{element}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{element}, State: state})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.NotContains(t, ssa.Format(fn), "slice")
	})

	t.Run("does not slice an invariant container no array guard admits", func(t *testing.T) {
		l := newCountedLoop()
		record, guarded, field := l.b.Param(l.pre, ssa.TypeRef), l.b.Value(ssa.TypeRef), l.b.Value(ssa.TypeI32)
		array, length := l.b.Param(l.pre, ssa.TypeRef), l.b.Value(ssa.TypeI32)
		entry := l.b.Value(ssa.TypeState)
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{entry}})
		l.b.Add(l.pre, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Struct: true, Type: 0x40}, Args: []ssa.Value{record}, State: entry, Results: []ssa.Value{guarded}})
		state := l.b.Value(ssa.TypeState)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 4}}, Results: []ssa.Value{state}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_GET, Args: []ssa.Value{guarded, l.one}, State: state, Results: []ssa.Value{field}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{length}})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.NotContains(t, ssa.Format(fn), "slice")
	})

	t.Run("does not hoist a load of a local the loop stores", func(t *testing.T) {
		l := newCountedLoop()
		loaded, sum := l.b.Value(ssa.TypeI32), l.b.Value(ssa.TypeI32)
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 1}, Results: []ssa.Value{loaded}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{loaded, l.counter}, State: deoptState(l.b, l.pre), Results: []ssa.Value{sum}})
		l.b.Add(l.body, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 1}, Args: []ssa.Value{sum}, State: deoptState(l.b, l.pre)})
		fn := l.close()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.Contains(t, blockChunk(ssa.Format(fn), l.body), "load local[1]")
	})

	t.Run("does not hoist a shape guard whose loop entry state names a loop-defined value", func(t *testing.T) {
		b := ssa.New("f")
		pre, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block()
		array := b.Param(pre, ssa.TypeRef)
		zero, bound, one := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{bound}})
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Term(pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}}})

		counter, total := b.Param(header, ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 1}, Results: []ssa.Value{total}})
		entry := b.Value(ssa.TypeState)
		b.Add(header, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 2, Stack: []ssa.Operand{{Value: total}, {Value: counter}}}}, Results: []ssa.Value{entry}})
		cond := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{counter, bound}, State: entry, Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

		at := b.Value(ssa.TypeState)
		b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 5, Stack: []ssa.Operand{{Value: array}, {Value: counter}}}}, Results: []ssa.Value{at}})
		guarded, element, sum, next := b.Value(ssa.TypeRef), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{array}, State: at, Results: []ssa.Value{guarded}})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{guarded, counter}, State: at, Results: []ssa.Value{element}})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{total, element}, State: at, Results: []ssa.Value{sum}})
		b.Add(body, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 1}, Args: []ssa.Value{sum}, State: at})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, one}, State: at, Results: []ssa.Value{next}})
		b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{counter}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.Contains(t, blockChunk(ssa.Format(fn), body), "guard.shape")
	})

	t.Run("cascades a doubly loop-invariant operation out of a nested loop in one run", func(t *testing.T) {
		b := ssa.New("f")
		pre, outer, mid, inner, innerBody, exit := b.Block(), b.Block(), b.Block(), b.Block(), b.Block(), b.Block()

		x, y := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{x}})
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 3, Results: []ssa.Value{y}})
		bound := b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{bound}})
		one := b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		zero := b.Value(ssa.TypeI32)
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Term(pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: outer, Args: []ssa.Value{zero}}}})

		oc := b.Param(outer, ssa.TypeI32)
		ocond := b.Value(ssa.TypeI1)
		b.Add(outer, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{oc, bound}, State: deoptState(b, pre), Results: []ssa.Value{ocond}})
		b.Term(outer, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{ocond}, Edges: []ssa.Edge{{Block: mid}, {Block: exit}}})

		b.Term(mid, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: inner, Args: []ssa.Value{zero}}}})

		ic := b.Param(inner, ssa.TypeI32)
		icond := b.Value(ssa.TypeI1)
		b.Add(inner, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{ic, bound}, State: deoptState(b, pre), Results: []ssa.Value{icond}})
		outerNext := b.Value(ssa.TypeI32)
		b.Add(inner, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{oc, one}, State: deoptState(b, pre), Results: []ssa.Value{outerNext}})
		b.Term(inner, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{icond}, Edges: []ssa.Edge{
			{Block: innerBody},
			{Block: outer, Args: []ssa.Value{outerNext}},
		}})

		sum := b.Value(ssa.TypeI32)
		b.Add(innerBody, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, State: deoptState(b, pre), Results: []ssa.Value{sum}})
		innerNext := b.Value(ssa.TypeI32)
		b.Add(innerBody, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{ic, one}, State: deoptState(b, pre), Results: []ssa.Value{innerNext}})
		b.Term(innerBody, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: inner, Args: []ssa.Value{innerNext}}}})

		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{oc}})

		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))

		out := ssa.Format(fn)
		require.Equal(t, 1, strings.Count(blockChunk(out, 0), "i32.add"))
		require.Equal(t, 3, strings.Count(out, "i32.add"))
	})

	t.Run("is correct on a function carrying no loop at all", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{x}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewHoistPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})
}
func newCountedLoop() *countedLoop {
	b := ssa.New("f")
	l := &countedLoop{b: b, pre: b.Block(), header: b.Block(), body: b.Block(), exit: b.Block()}

	l.bound = b.Value(ssa.TypeI32)
	b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{l.bound}})
	l.one = b.Value(ssa.TypeI32)
	b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{l.one}})
	zero := b.Value(ssa.TypeI32)
	b.Add(l.pre, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
	b.Term(l.pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: l.header, Args: []ssa.Value{zero}}}})

	l.counter = b.Param(l.header, ssa.TypeI32)
	l.cond = b.Value(ssa.TypeI1)
	b.Add(l.header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{l.counter, l.bound}, State: deoptState(l.b, l.pre), Results: []ssa.Value{l.cond}})
	b.Term(l.header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{l.cond}, Edges: []ssa.Edge{{Block: l.body}, {Block: l.exit}}})

	b.Term(l.exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{l.counter}})
	return l
}

func (l *countedLoop) close() *ssa.Function {
	next := l.b.Value(ssa.TypeI32)
	l.b.Add(l.body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{l.counter, l.one}, State: deoptState(l.b, l.pre), Results: []ssa.Value{next}})
	l.b.Term(l.body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: l.header, Args: []ssa.Value{next}}}})
	return l.b.Build()
}

func hasCode(ops []ssa.Operation, code instr.Opcode) bool {
	for _, op := range ops {
		if op.Op == ssa.OpExec && op.Code == code {
			return true
		}
	}
	return false
}

func blockChunk(format string, id int) string {
	marker := fmt.Sprintf("blk%d:", id)
	idx := strings.Index(format, marker)
	if idx < 0 {
		return ""
	}
	rest := format[idx+len(marker):]
	if next := strings.Index(rest, "\nblk"); next >= 0 {
		rest = rest[:next]
	}
	return rest
}
