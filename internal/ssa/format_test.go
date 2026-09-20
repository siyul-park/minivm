package ssa_test

import (
	"testing"

	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	t.Run("prints a loop over a guarded array", func(t *testing.T) {
		b := ssa.New("sum")
		entry, header, body, done := b.Block(), b.Block(), b.Block(), b.Block()

		state := b.Value(ssa.TypeState)
		array := b.Value(ssa.TypeRef)
		checked := b.Value(ssa.TypeRef)
		zero := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 0, Returns: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Itab: 0x2a}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{checked}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{zero}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero, zero}}}})

		index := b.Param(header, ssa.TypeI32)
		total := b.Param(header, ssa.TypeI32)
		length := b.Value(ssa.TypeI32)
		more := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{checked}, Results: []ssa.Value{length}})
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{index, length}, Results: []ssa.Value{more}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{more}, Edges: []ssa.Edge{{Block: body}, {Block: done, Args: []ssa.Value{total}}}})

		inner := b.Value(ssa.TypeState)
		elem := b.Value(ssa.TypeI32)
		next := b.Value(ssa.TypeI32)
		one := b.Value(ssa.TypeI32)
		step := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 12, Returns: 1, Stack: []ssa.Operand{{Value: index}}}}, Results: []ssa.Value{inner}})
		b.Add(body, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{index, length}, State: inner})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{checked, index}, Results: []ssa.Value{elem}})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{total, elem}, Results: []ssa.Value{next}})
		b.Add(body, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{index, one}, Results: []ssa.Value{step}})
		b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{step, next}}}})

		result := b.Param(done, ssa.TypeI32)
		b.Term(done, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func sum\n"+
			"blk0: ()\n"+
			"\tv1:state = state {addr=1 base=0 ip=0 returns=1 stack=[]}\n"+
			"\tv2:ref = load local[0]\n"+
			"\tv3:ref = guard.shape v2 itab 0x2a state v1\n"+
			"\tv4:i32 = const 0\n"+
			"\tjump blk1(v4, v4)\n"+
			"blk1: (v5:i32, v6:i32) <-- (blk0, blk2)\n"+
			"\tv7:i32 = array.len v3\n"+
			"\tv8:i1 = i32.lt_s v5, v7\n"+
			"\tbr v8, blk2(), blk3(v6)\n"+
			"blk2: () <-- (blk1)\n"+
			"\tv9:state = state {addr=1 base=0 ip=12 returns=1 stack=[v5]}\n"+
			"\tguard.bounds v5, v7 state v9\n"+
			"\tv10:i32 = array.get v3, v5\n"+
			"\tv11:i32 = i32.add v6, v10\n"+
			"\tv12:i32 = const 1\n"+
			"\tv13:i32 = i32.add v5, v12\n"+
			"\tjump blk1(v13, v11)\n"+
			"blk3: (v14:i32) <-- (blk1)\n"+
			"\treturn v14\n", ssa.Format(f))
	})

	t.Run("marks the stack entries a deopt frame owns", func(t *testing.T) {
		b := ssa.New("own")
		entry := b.Block()
		array := b.Value(ssa.TypeRef)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{array}})
		// One value in two stack positions, retained once: ownership is the
		// entry's, so the two positions print differently.
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Addr: 1, Stack: []ssa.Operand{{Value: array}, {Value: array, Owned: true}}},
		}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func own\n"+
			"blk0: ()\n"+
			"\tv1:ref = load local[0]\n"+
			"\tretain v1\n"+
			"\tv2:state = state {addr=1 base=0 ip=0 returns=0 stack=[v1, v1 owned]}\n"+
			"\texit state v2\n", ssa.Format(f))
	})

	t.Run("names the local slots a deopt frame is written back with", func(t *testing.T) {
		b := ssa.New("promoted")
		entry := b.Block()
		counter := b.Value(ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{counter}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Addr: 1, Locals: []ssa.Local{{Index: 2, Value: counter}}},
		}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func promoted\n"+
			"blk0: ()\n"+
			"\tv1:i32 = const 7\n"+
			"\tv2:state = state {addr=1 base=0 ip=0 returns=0 stack=[] locals=[2=v1]}\n"+
			"\texit state v2\n", ssa.Format(f))
	})

	t.Run("prints every other operation form", func(t *testing.T) {
		b := ssa.New("forms")
		entry, stop, give, end := b.Block(), b.Block(), b.Block(), b.Block()

		state := b.Value(ssa.TypeState)
		callee := b.Value(ssa.TypeRef)
		slot := b.Value(ssa.TypeRef)
		target := b.Value(ssa.TypeRef)
		kind := b.Value(ssa.TypeI32)
		three := b.Value(ssa.TypeI32)
		picked := b.Value(ssa.TypeI32)
		returned := b.Value(ssa.TypeI32)
		fresh := b.Value(ssa.TypeRef)
		field := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 2, Base: 4, IP: 3}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxRef(7), Results: []ssa.Value{callee}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceUpval, Index: 1}, Results: []ssa.Value{slot}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{slot, callee}, State: state, Results: []ssa.Value{target}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{target}, State: state, Results: []ssa.Value{kind}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{target}})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 2}, Args: []ssa.Value{target}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(3), Results: []ssa.Value{three}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.SELECT, Args: []ssa.Value{kind, three, three}, Results: []ssa.Value{picked}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_SET, Args: []ssa.Value{target, three, picked}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{callee, three}, State: state, Results: []ssa.Value{returned}})
		b.Add(entry, ssa.Operation{Op: ssa.OpBridge, Code: instr.ARRAY_NEW_DEFAULT, Args: []ssa.Value{returned}, State: state, Results: []ssa.Value{fresh}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{fresh}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_GET, Shape: ssa.Shape{Typ: 0x40, Host: reflect.Int16}, Args: []ssa.Value{target, three}, Results: []ssa.Value{field}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpTable, Args: []ssa.Value{field}, Edges: []ssa.Edge{{Block: stop}, {Block: give}, {Block: end}}})

		b.Term(stop, ssa.Terminator{Op: ssa.OpSuspend, State: state})
		b.Term(give, ssa.Terminator{Op: ssa.OpExit, State: state})
		b.Term(end, ssa.Terminator{Op: ssa.OpComplete})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func forms\n"+
			"blk0: ()\n"+
			"\tv1:state = state {addr=2 base=4 ip=3 returns=0 stack=[]}\n"+
			"\tv2:ref = const 7\n"+
			"\tv3:ref = load upval[1]\n"+
			"\tv4:ref = guard.value v3, v2 state v1\n"+
			"\tv5:i32 = guard.kind v4 state v1\n"+
			"\tretain v4\n"+
			"\tstore global[2], v4 state v1\n"+
			"\tv6:i32 = const 3\n"+
			"\tv7:i32 = select v5, v6, v6\n"+
			"\tarray.set v4, v6, v7 state v1\n"+
			"\tv8:i32 = call v2, v6 state v1\n"+
			"\tv9:ref = bridge array.new_default v8 state v1\n"+
			"\trelease v9 state v1\n"+
			"\tv10:i32 = struct.get v4, v6 type 0x40 host int16\n"+
			"\ttable v10, blk1(), blk2(), blk3()\n"+
			"blk1: () <-- (blk0)\n"+
			"\tsuspend state v1\n"+
			"blk2: () <-- (blk0)\n"+
			"\texit state v1\n"+
			"blk3: () <-- (blk0)\n"+
			"\tcomplete\n", ssa.Format(f))
	})
}
