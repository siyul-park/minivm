package ssa_test

import (
	"math"
	"testing"

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
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 0, Returns: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindI32}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{checked}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero, zero}}}})

		index := b.Param(header, ssa.TypeI32)
		total := b.Param(header, ssa.TypeI32)
		length := b.Value(ssa.TypeI32)
		more := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpArrayLen, Args: []ssa.Value{checked}, State: state, Results: []ssa.Value{length}})
		b.Add(header, ssa.Operation{Op: ssa.OpIntCmp, Cond: ssa.CondSLT, Args: []ssa.Value{index, length}, State: state, Results: []ssa.Value{more}})
		b.Term(header, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{more}, Edges: []ssa.Edge{{Block: body}, {Block: done, Args: []ssa.Value{total}}}})

		inner := b.Value(ssa.TypeState)
		elem := b.Value(ssa.TypeI32)
		next := b.Value(ssa.TypeI32)
		one := b.Value(ssa.TypeI32)
		step := b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 12, Returns: 1, Stack: []ssa.Operand{{Value: index}}}}, Results: []ssa.Value{inner}})
		b.Add(body, ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{index, length}, State: inner})
		b.Add(body, ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{checked, index}, State: state, Results: []ssa.Value{elem}})
		b.Add(body, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{total, elem}, State: state, Results: []ssa.Value{next}})
		b.Add(body, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(body, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{index, one}, State: state, Results: []ssa.Value{step}})
		b.Term(body, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{step, next}}}})

		result := b.Param(done, ssa.TypeI32)
		b.Term(done, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func sum\n"+
			"blk0: ()\n"+
			"\tv1:state = state {addr=1 base=0 ip=0 returns=1 stack=[]}\n"+
			"\tv2:ref = load local[0]\n"+
			"\tv3:ref = guard.shape v2 kind i32 state v1\n"+
			"\tv4:i32 = const 0\n"+
			"\tjump blk1(v4, v4)\n"+
			"blk1: (v5:i32, v6:i32) <-- (blk0, blk2)\n"+
			"\tv7:i32 = array.len v3 state v1\n"+
			"\tv8:i1 = int.cmp slt v5, v7 state v1\n"+
			"\tbrif v8, blk2(), blk3(v6)\n"+
			"blk2: () <-- (blk1)\n"+
			"\tv9:state = state {addr=1 base=0 ip=12 returns=1 stack=[v5]}\n"+
			"\tguard.bounds v5, v7 state v9\n"+
			"\tv10:i32 = array.get v3, v5 state v1\n"+
			"\tv11:i32 = int.add v6, v10 state v1\n"+
			"\tv12:i32 = const 1\n"+
			"\tv13:i32 = int.add v5, v12 state v1\n"+
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
		b.Add(entry, ssa.Operation{Op: ssa.OpRefRetain, Args: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Address: 1, Stack: []ssa.Operand{{Value: array}, {Value: array, Owned: true}}},
		}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func own\n"+
			"blk0: ()\n"+
			"\tv1:ref = load local[0]\n"+
			"\tref.retain v1\n"+
			"\tv2:state = state {addr=1 base=0 ip=0 returns=0 stack=[v1, v1 owned]}\n"+
			"\texit state v2\n", ssa.Format(f))
	})

	t.Run("prints promoted locals", func(t *testing.T) {
		b := ssa.New("promoted")
		entry := b.Block()
		counter := b.Value(ssa.TypeI32)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 7, Results: []ssa.Value{counter}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Address: 1, Locals: []ssa.Local{{Index: 1, Value: counter}}},
		}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func promoted\n"+
			"blk0: ()\n"+
			"\tv1:i32 = const 7\n"+
			"\tv2:state = state {addr=1 base=0 ip=0 returns=0 stack=[] locals=[1=v1]}\n"+
			"\texit state v2\n", ssa.Format(f))
	})

	t.Run("prints one constant per type, wide i64 included", func(t *testing.T) {
		b := ssa.New("consts")
		entry := b.Block()
		i1 := b.Value(ssa.TypeI1)
		i8 := b.Value(ssa.TypeI8)
		i32 := b.Value(ssa.TypeI32)
		i64 := b.Value(ssa.TypeI64)
		f32 := b.Value(ssa.TypeF32)
		f64 := b.Value(ssa.TypeF64)
		ref := b.Value(ssa.TypeRef)
		i8v, i32v, i64v := int32(-5), int32(-7), int64(-3750763034362895579)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{i1}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(uint32(i8v)), Results: []ssa.Value{i8}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(uint32(i32v)), Results: []ssa.Value{i32}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(i64v), Results: []ssa.Value{i64}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(math.Float32bits(1.5)), Results: []ssa.Value{f32}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: math.Float64bits(2.5), Results: []ssa.Value{f64}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(9)), Results: []ssa.Value{ref}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{i1}})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func consts\n"+
			"blk0: ()\n"+
			"\tv1:i1 = const true\n"+
			"\tv2:i8 = const -5\n"+
			"\tv3:i32 = const -7\n"+
			"\tv4:i64 = const -3750763034362895579\n"+
			"\tv5:f32 = const 1.5\n"+
			"\tv6:f64 = const 2.5\n"+
			"\tv7:ref = const 9\n"+
			"\treturn v1\n", ssa.Format(f))
	})

	t.Run("prints the key and element kind of a map shape", func(t *testing.T) {
		b := ssa.New("m")
		entry := b.Block()
		state, table, guarded := b.Value(ssa.TypeState), b.Value(ssa.TypeRef), b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Returns: 0}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{table}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormMap, Key: types.KindI8, Elem: types.KindF64}, Args: []ssa.Value{table}, State: state, Results: []ssa.Value{guarded}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, State: state})

		require.Contains(t, ssa.Format(b.Build()), "\tv3:ref = guard.shape v2 map key i8 elem f64 state v1\n")
	})

	t.Run("prints kind f64 even though its Kind value is the zero value", func(t *testing.T) {
		b := ssa.New("f64shape")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		array := b.Value(ssa.TypeRef)
		checked := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Returns: 1}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 0}, Results: []ssa.Value{array}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindF64}, Args: []ssa.Value{array}, State: state, Results: []ssa.Value{checked}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{checked}})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func f64shape\n"+
			"blk0: ()\n"+
			"\tv1:state = state {addr=1 base=0 ip=0 returns=1 stack=[]}\n"+
			"\tv2:ref = load local[0]\n"+
			"\tv3:ref = guard.shape v2 kind f64 state v1\n"+
			"\treturn v3\n", ssa.Format(f))
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
		record := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 2, Base: 4, IP: 3}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(7)), Results: []ssa.Value{callee}})
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceUpval, Index: 1}, Results: []ssa.Value{slot}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{slot, callee}, State: state, Results: []ssa.Value{target}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{target}, State: state, Results: []ssa.Value{kind}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRefRetain, Args: []ssa.Value{target}})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 2}, Args: []ssa.Value{target}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 3, Results: []ssa.Value{three}})
		b.Add(entry, ssa.Operation{Op: ssa.OpSelect, Args: []ssa.Value{kind, three, three}, State: state, Results: []ssa.Value{picked}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{target, three, picked}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpCall, Shape: ssa.Shape{Form: ssa.FormSignature, Type: 0x50}, Args: []ssa.Value{three, callee}, State: state, Results: []ssa.Value{returned}})
		b.Add(entry, ssa.Operation{Op: ssa.OpArrayNewDefault, Shape: ssa.Shape{Form: ssa.FormArray, Type: 0x30}, Args: []ssa.Value{returned}, State: state, Results: []ssa.Value{fresh}})
		b.Add(entry, ssa.Operation{Op: ssa.OpRefRelease, Args: []ssa.Value{fresh}, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormStruct, Type: 0x40}, Args: []ssa.Value{target}, State: state, Results: []ssa.Value{record}})
		b.Add(entry, ssa.Operation{Op: ssa.OpStructGet, Args: []ssa.Value{record, three}, State: state, Results: []ssa.Value{field}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrTable, Args: []ssa.Value{field}, Edges: []ssa.Edge{{Block: stop}, {Block: give}, {Block: end}}})

		b.Term(stop, ssa.Terminator{Term: ssa.TermReturn, State: state})
		b.Term(give, ssa.Terminator{Term: ssa.TermExit, State: state})
		b.Term(end, ssa.Terminator{Term: ssa.TermComplete})

		f := b.Build()
		require.NoError(t, ssa.Verify(f))
		require.Equal(t, "func forms\n"+
			"blk0: ()\n"+
			"\tv1:state = state {addr=2 base=4 ip=3 returns=0 stack=[]}\n"+
			"\tv2:ref = const 7\n"+
			"\tv3:ref = load upval[1]\n"+
			"\tv4:ref = guard.value v3, v2 state v1\n"+
			"\tv5:i32 = guard.kind v4 state v1\n"+
			"\tref.retain v4\n"+
			"\tstore global[2], v4 state v1\n"+
			"\tv6:i32 = const 3\n"+
			"\tv7:i32 = select v5, v6, v6 state v1\n"+
			"\tarray.set v4, v6, v7 state v1\n"+
			"\tv8:i32 = call v6, v2 callee type 0x50 state v1\n"+
			"\tv9:ref = array.new_default v8 kind f64 state v1\n"+
			"\tref.release v9 state v1\n"+
			"\tv11:ref = guard.shape v4 struct type 0x40 state v1\n"+
			"\tv10:i32 = struct.get v11, v6 state v1\n"+
			"\tbr_table v10, blk1(), blk2(), blk3()\n"+
			"blk1: () <-- (blk0)\n"+
			"\treturn state v1\n"+
			"blk2: () <-- (blk0)\n"+
			"\texit state v1\n"+
			"blk3: () <-- (blk0)\n"+
			"\tcomplete\n", ssa.Format(f))
	})

	t.Run("prints a compare with its condition", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		at := b.Value(ssa.TypeState)
		less := b.Value(ssa.TypeI1)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1}}, Results: []ssa.Value{at}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntCmp, Cond: ssa.CondSLT, Args: []ssa.Value{x, y}, State: at, Results: []ssa.Value{less}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: at})
		require.Contains(t, ssa.Format(b.Build()), "v4:i1 = int.cmp slt v1, v2 state v3")
	})
}
