package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

func TestNewPromotePass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewPromotePass()
		require.NotNil(t, p)
	})
}

func TestPromotePass_Run(t *testing.T) {
	t.Run("keeps an unpromoted ref local beside a promoted i32 local", func(t *testing.T) {
		fn, _ := loopFunction(t)
		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		_, err = transform.NewPromotePass().Run(pass.NewManager(), out)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		formatted := ssa.Format(out)
		require.Contains(t, formatted, "load local[0]")
		require.Contains(t, formatted, "load local[1]")
	})

	t.Run("carries a loop-carried counter on the back edge as a block parameter", func(t *testing.T) {
		fn := slotFunction()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))

		require.Equal(t, 1, countOperations(fn, ssa.OpLoad))
		require.Zero(t, countOperations(fn, ssa.OpStore))

		header, _ := findBlock(fn, func(blk ssa.Block) bool { return len(blk.Params) == 1 })
		require.NotEqual(t, -1, header)
		counter := fn.Block(header).Params[0]
		require.Equal(t, ssa.TypeI32, fn.Type(counter))

		body, advanced := findBlock(fn, func(blk ssa.Block) bool { return hasCode(blk.Operations, instr.I32_ADD) })
		require.NotEqual(t, -1, body)
		var next ssa.Value
		for _, op := range advanced.Operations {
			if op.Op == ssa.OpExec && op.Code == instr.I32_ADD {
				next = op.Results[0]
				require.Equal(t, []ssa.Value{counter, op.Args[1]}, op.Args)
			}
		}
		require.Equal(t, []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}, advanced.Terminator.Edges)
	})

	t.Run("state a deopt with the value the promoted slot held there", func(t *testing.T) {
		fn := slotFunction()
		_, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))

		header, _ := findBlock(fn, func(blk ssa.Block) bool { return len(blk.Params) == 1 })
		counter := fn.Block(header).Params[0]

		_, body := findBlock(fn, func(blk ssa.Block) bool { return hasCode(blk.Operations, instr.I32_ADD) })
		state, ok := deoptStateOf(body)
		require.True(t, ok)
		require.Equal(t, []ssa.Frame{{Address: 1, IP: 9, Locals: []ssa.Local{{Index: 0, Value: counter}}}}, state.Frames)

		entry := fn.Block(0)
		state, ok = deoptStateOf(entry)
		require.True(t, ok)
		require.Equal(t, []ssa.Frame{{Address: 1, IP: 1, Locals: []ssa.Local{{Index: 0, Value: entry.Operations[0].Results[0]}}}}, state.Frames)
		require.Equal(t, ssa.OpLoad, entry.Operations[0].Op)
	})

	t.Run("guards a promoted i64 local once, at block 0, aliasing every inner guard.kind away", func(t *testing.T) {
		fn := i64SlotFunction()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))

		require.Equal(t, 1, countOperations(fn, ssa.OpGuardKind))
		require.Equal(t, 1, countOperations(fn, ssa.OpLoad))
		require.Zero(t, countOperations(fn, ssa.OpStore))

		entry := fn.Block(0)
		require.Equal(t, ssa.OpState, entry.Operations[0].Op)
		require.Equal(t, ssa.OpLoad, entry.Operations[1].Op)
		require.Equal(t, ssa.OpGuardKind, entry.Operations[2].Op)
		require.Equal(t, entry.Operations[0].Results[0], entry.Operations[2].State)
		require.Equal(t, []ssa.Frame{{Address: 1, IP: 0, Returns: 1}}, entry.Operations[0].Frames)

		require.Equal(t, `func f
blk0: ()
	v1:state = state {addr=1 base=0 ip=0 returns=1 stack=[]}
	v2:i64 = load local[0]
	v3:i64 = guard.kind v2 state v1
	v4:i64 = const 1234
	v5:state = state {addr=1 base=0 ip=1 returns=0 stack=[] locals=[0=v3]}
	v6:state = state {addr=1 base=0 ip=0 returns=0 stack=[] locals=[0=v4]}
	v7:state = state {addr=1 base=0 ip=0 returns=0 stack=[] locals=[0=v4]}
	v8:state = state {addr=1 base=0 ip=0 returns=0 stack=[] locals=[0=v4]}
	v9:state = state {addr=1 base=0 ip=0 returns=0 stack=[] locals=[0=v4]}
	jump blk1(v4)
blk1: (v10:i64) <-- (blk0, blk2)
	v11:i64 = const 10
	v12:i1 = i64.lt_s v10, v11 state v7
	br v12, blk2(), blk3()
blk2: () <-- (blk1)
	v13:i64 = const 1
	v14:i64 = i64.xor v10, v13 state v9
	v15:state = state {addr=1 base=0 ip=9 returns=0 stack=[] locals=[0=v10]}
	jump blk1(v14)
blk3: () <-- (blk1)
	return
`, ssa.Format(fn))
	})

	t.Run("promotes an i32-only local with no entry guard or state", func(t *testing.T) {
		fn := slotFunction()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))

		require.Zero(t, countOperations(fn, ssa.OpGuardKind))
		require.Equal(t, ssa.OpLoad, fn.Block(0).Operations[0].Op)
	})

	t.Run("leaves a slot alone when nothing stores it", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		held := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("leaves a slot alone when its accesses disagree on a type", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		stored := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{stored}})
		addStore(b, entry, ssa.Slot{Index: 0}, stored)
		held := b.Value(ssa.TypeF64)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("leaves a slot alone when it holds a reference", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		stored := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxedNull), Results: []ssa.Value{stored}})
		addStore(b, entry, ssa.Slot{Index: 0}, stored)
		held := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("leaves a slot alone when it belongs to an inlined frame", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		stored := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{stored}})
		addStore(b, entry, ssa.Slot{Index: 0, Base: 4}, stored)
		held := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0, Base: 4}, Results: []ssa.Value{held}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("leaves a slot alone when it is not a local", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		stored := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{stored}})
		addStore(b, entry, ssa.Slot{Space: ssa.SpaceGlobal}, stored)
		held := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal}, Results: []ssa.Value{held}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("declines a function that hands a local opcode to the interpreter", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		stored := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{stored}})
		addStore(b, entry, ssa.Slot{Index: 0}, stored)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 3}}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.LOCAL_GET, State: state})
		held := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

}

func slotFunction() *ssa.Function {
	b := ssa.New("f")
	entry, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block()

	zero := b.Value(ssa.TypeI32)
	b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
	state := b.Value(ssa.TypeState)
	b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 1}}, Results: []ssa.Value{state}})
	b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{zero}, State: state})
	b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

	held := b.Value(ssa.TypeI32)
	b.Add(header, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
	bound := b.Value(ssa.TypeI32)
	b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{bound}})
	cond := b.Value(ssa.TypeI1)
	b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{held, bound}, State: deoptState(b, entry), Results: []ssa.Value{cond}})
	b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

	counter := b.Value(ssa.TypeI32)
	b.Add(body, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{counter}})
	one := b.Value(ssa.TypeI32)
	b.Add(body, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
	next := b.Value(ssa.TypeI32)
	b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, one}, State: deoptState(b, entry), Results: []ssa.Value{next}})
	advanced := b.Value(ssa.TypeState)
	b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 9}}, Results: []ssa.Value{advanced}})
	b.Add(body, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{next}, State: advanced})
	b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

	b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})
	return b.Build()
}

// i64SlotFunction mirrors slotFunction's shape (an entry that stores once, a
// header that loads/checks/branches, a body that loads/updates/stores) but
// with an i64 local: FNV-shaped, one read and one rewrite of the same
// accumulator per iteration, each load guarded exactly as translate's own
// frontend produces it, ahead of promotion.
func i64SlotFunction() *ssa.Function {
	b := ssa.New("f")
	entry, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block()
	b.Entry(ssa.Frame{Address: 1, IP: 0, Returns: 1})

	basis := b.Value(ssa.TypeI64)
	b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(1234), Results: []ssa.Value{basis}})
	state := b.Value(ssa.TypeState)
	b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 1}}, Results: []ssa.Value{state}})
	b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{basis}, State: state})
	b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

	held := b.Value(ssa.TypeI64)
	b.Add(header, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
	guarded := b.Value(ssa.TypeI64)
	b.Add(header, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{held}, State: deoptState(b, entry), Results: []ssa.Value{guarded}})
	bound := b.Value(ssa.TypeI64)
	b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: uint64(10), Results: []ssa.Value{bound}})
	cond := b.Value(ssa.TypeI1)
	b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_LT_S, Args: []ssa.Value{guarded, bound}, State: deoptState(b, entry), Results: []ssa.Value{cond}})
	b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

	counter := b.Value(ssa.TypeI64)
	b.Add(body, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{counter}})
	guardedCounter := b.Value(ssa.TypeI64)
	b.Add(body, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{counter}, State: deoptState(b, entry), Results: []ssa.Value{guardedCounter}})
	one := b.Value(ssa.TypeI64)
	b.Add(body, ssa.Operation{Op: ssa.OpConst, Const: uint64(1), Results: []ssa.Value{one}})
	next := b.Value(ssa.TypeI64)
	b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_XOR, Args: []ssa.Value{guardedCounter, one}, State: deoptState(b, entry), Results: []ssa.Value{next}})
	advanced := b.Value(ssa.TypeState)
	b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 9}}, Results: []ssa.Value{advanced}})
	b.Add(body, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{next}, State: advanced})
	b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

	b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})
	return b.Build()
}

func addStore(b *ssa.Builder, block int, slot ssa.Slot, held ssa.Value) {
	state := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 1}}, Results: []ssa.Value{state}})
	b.Add(block, ssa.Operation{Op: ssa.OpStore, Slot: slot, Args: []ssa.Value{held}, State: state})
}

func findBlock(function *ssa.Function, predicate func(ssa.Block) bool) (int, ssa.Block) {
	for id := range function.Len() {
		if blk := function.Block(id); predicate(blk) {
			return id, blk
		}
	}
	return -1, ssa.Block{}
}

func deoptStateOf(block ssa.Block) (ssa.Operation, bool) {
	for _, op := range block.Operations {
		if op.Op == ssa.OpState {
			return op, true
		}
	}
	return ssa.Operation{}, false
}

func countOperations(function *ssa.Function, want ssa.Op) int {
	n := 0
	for id := range function.Len() {
		for _, op := range function.Block(id).Operations {
			if op.Op == want {
				n++
			}
		}
	}
	return n
}
