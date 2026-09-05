package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

func TestNewPromotePass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewPromotePass()
		require.NotNil(t, p)
	})
}

func TestPromotePass_Run(t *testing.T) {
	t.Run("carries a loop-carried counter on the back edge as a block parameter", func(t *testing.T) {
		fn := slotLoop()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))

		// The counter is read and written nowhere but the one load the entry
		// starts its reaching definition from.
		require.Equal(t, 1, count(fn, ssa.OpLoad))
		require.Zero(t, count(fn, ssa.OpStore))

		header, _ := find(fn, func(blk ssa.Block) bool { return len(blk.Params) == 1 })
		require.NotEqual(t, -1, header, "the merge the back edge reaches takes the counter as a parameter")
		counter := fn.Block(header).Params[0]
		require.Equal(t, ssa.TypeI32, fn.Type(counter))

		body, advanced := find(fn, func(blk ssa.Block) bool { return hasCode(blk.Ops, instr.I32_ADD) })
		require.NotEqual(t, -1, body)
		var next ssa.Value
		for _, op := range advanced.Ops {
			if op.Op == ssa.OpExec && op.Code == instr.I32_ADD {
				next = op.Results[0]
				require.Equal(t, []ssa.Value{counter, op.Args[1]}, op.Args, "the body advances the counter it was entered with")
			}
		}
		require.Equal(t, []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}, advanced.Term.Edges,
			"the back edge hands the header what this iteration computed")
	})

	t.Run("resumes a deopt with the value the promoted slot held there", func(t *testing.T) {
		fn := slotLoop()
		_, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))

		header, _ := find(fn, func(blk ssa.Block) bool { return len(blk.Params) == 1 })
		counter := fn.Block(header).Params[0]

		// The body's state sits before the operation that advances the
		// counter, so a deopt from it must put back what the slot still held:
		// the counter this iteration entered with, never the advanced value.
		_, body := find(fn, func(blk ssa.Block) bool { return hasCode(blk.Ops, instr.I32_ADD) })
		state, ok := resumes(body)
		require.True(t, ok)
		require.Equal(t, []ssa.Frame{{Addr: 1, IP: 9, Locals: []ssa.Local{{Index: 0, Value: counter}}}}, state.Frames)

		// The entry's state sits before the slot is ever written, so it
		// resumes with the content the frame was entered with.
		entry := fn.Block(0)
		state, ok = resumes(entry)
		require.True(t, ok)
		require.Equal(t, []ssa.Frame{{Addr: 1, IP: 1, Locals: []ssa.Local{{Index: 0, Value: entry.Ops[0].Results[0]}}}}, state.Frames)
		require.Equal(t, ssa.OpLoad, entry.Ops[0].Op, "the entry load is what the slot held on entry")
	})

	t.Run("gives an entry that is its own loop header a block to load in", func(t *testing.T) {
		b := ssa.New("f")
		header, exit := b.Block(), b.Block()
		held := b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
		one := b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		next := b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{held, one}, Results: []ssa.Value{next}})
		state := b.Value(ssa.TypeState)
		b.Add(header, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 4}}, Results: []ssa.Value{state}})
		b.Add(header, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{next}, State: state})
		cond := b.Value(ssa.TypeI1)
		b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{next}, Results: []ssa.Value{cond}})
		b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: exit}, {Block: header}}})
		b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 2, fn.Len())

		_, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))

		require.Equal(t, 3, fn.Len(), "the loop header gains a block in front of it")
		require.Empty(t, fn.Pred(0))
		require.Equal(t, ssa.OpLoad, fn.Block(0).Ops[0].Op, "the load runs once per entry, not once per iteration")
		require.Equal(t, ssa.OpJump, fn.Block(0).Term.Op)
		require.Equal(t, 1, count(fn, ssa.OpLoad))
		require.Zero(t, count(fn, ssa.OpStore))
	})

	t.Run("leaves a slot alone", func(t *testing.T) {
		for name, build := range map[string]func(*ssa.Builder, int) ssa.Value{
			// A slot nothing stores has one definition already - the slot
			// itself - so promoting it only makes it live across blocks.
			"nothing stores": func(b *ssa.Builder, block int) ssa.Value {
				held := b.Value(ssa.TypeI32)
				b.Add(block, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
				return held
			},
			// One value has one type, so a slot written as i32 and read back
			// as f64 has no single value to stand for it.
			"its accesses disagree on a type": func(b *ssa.Builder, block int) ssa.Value {
				stored := b.Value(ssa.TypeI32)
				b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{stored}})
				store(b, block, ssa.Slot{Index: 0}, stored)
				held := b.Value(ssa.TypeF64)
				b.Add(block, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
				return held
			},
			// A ref slot holds the reference count of what is in it, and a
			// promoted local has nowhere to say where that count went.
			"it holds a reference": func(b *ssa.Builder, block int) ssa.Value {
				stored := b.Value(ssa.TypeRef)
				b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxedNull, Results: []ssa.Value{stored}})
				store(b, block, ssa.Slot{Index: 0}, stored)
				held := b.Value(ssa.TypeRef)
				b.Add(block, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
				return held
			},
			// A local of a frame a frontend inlined counts from that frame's
			// own floor, which no promoted local names.
			"it belongs to an inlined frame": func(b *ssa.Builder, block int) ssa.Value {
				stored := b.Value(ssa.TypeI32)
				b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{stored}})
				store(b, block, ssa.Slot{Index: 0, Base: 4}, stored)
				held := b.Value(ssa.TypeI32)
				b.Add(block, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0, Base: 4}, Results: []ssa.Value{held}})
				return held
			},
			// A global is visible to every callee, so a call may have written
			// it since; only a local is private to the frame.
			"it is not a local": func(b *ssa.Builder, block int) ssa.Value {
				stored := b.Value(ssa.TypeI32)
				b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{stored}})
				store(b, block, ssa.Slot{Space: ssa.SpaceGlobal}, stored)
				held := b.Value(ssa.TypeI32)
				b.Add(block, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal}, Results: []ssa.Value{held}})
				return held
			},
		} {
			t.Run(name, func(t *testing.T) {
				b := ssa.New("f")
				entry := b.Block()
				held := build(b, entry)
				b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
				fn := b.Build()
				require.NoError(t, ssa.Verify(fn))
				before := ssa.Format(fn)

				preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
				require.NoError(t, err)
				require.Equal(t, pass.PreserveAll(), preserved)
				require.Equal(t, before, ssa.Format(fn))
			})
		}
	})

	t.Run("declines a function", func(t *testing.T) {
		t.Run("that hands a local opcode to the interpreter", func(t *testing.T) {
			// A bridged opcode runs in the interpreter, which reads the frame
			// a promotion would have emptied, so no slot of this function is
			// promotable however it is accessed.
			b := ssa.New("f")
			entry := b.Block()
			stored := b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{stored}})
			store(b, entry, ssa.Slot{Index: 0}, stored)
			state := b.Value(ssa.TypeState)
			b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 3}}, Results: []ssa.Value{state}})
			b.Add(entry, ssa.Operation{Op: ssa.OpBridge, Code: instr.LOCAL_GET, State: state})
			held := b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
			b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{held}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn))
			before := ssa.Format(fn)

			preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
			require.NoError(t, err)
			require.Equal(t, pass.PreserveAll(), preserved)
			require.Equal(t, before, ssa.Format(fn))
		})

		t.Run("whose entry both takes operands and is a loop header", func(t *testing.T) {
			// The entry's parameters are the operands a native entry is handed,
			// and a block put in front of it has none of them to pass on.
			b := ssa.New("f")
			header, exit := b.Block(), b.Block()
			seed := b.Param(header, ssa.TypeI32)
			state := b.Value(ssa.TypeState)
			b.Add(header, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 1}}, Results: []ssa.Value{state}})
			b.Add(header, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{seed}, State: state})
			held := b.Value(ssa.TypeI32)
			b.Add(header, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
			cond := b.Value(ssa.TypeI1)
			b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_EQZ, Args: []ssa.Value{held}, Results: []ssa.Value{cond}})
			b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: exit}, {Block: header, Args: []ssa.Value{held}}}})
			b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn))
			before := ssa.Format(fn)

			preserved, err := transform.NewPromotePass().Run(pass.NewManager(), fn)
			require.NoError(t, err)
			require.Equal(t, pass.PreserveAll(), preserved)
			require.Equal(t, before, ssa.Format(fn))
		})
	})
}

// slotLoop builds the shape this pass exists for: a counter that lives in
// entry-frame local slot 0 instead of in a value. The entry writes its initial
// content, the header reads it back and branches on it, and the body reads it,
// advances it, and writes it back - so every iteration pays a load, a store,
// and the boxing between them. Both stores materialize the interpreter state
// they resume into, which is what a deopt from inside the loop reads the
// counter out of once the slot no longer holds it.
func slotLoop() *ssa.Function {
	b := ssa.New("f")
	entry, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block()

	zero := b.Value(ssa.TypeI32)
	b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{zero}})
	state := b.Value(ssa.TypeState)
	b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 1}}, Results: []ssa.Value{state}})
	b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{zero}, State: state})
	b.Term(entry, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

	held := b.Value(ssa.TypeI32)
	b.Add(header, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
	bound := b.Value(ssa.TypeI32)
	b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(10), Results: []ssa.Value{bound}})
	cond := b.Value(ssa.TypeI1)
	b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_LT_S, Args: []ssa.Value{held, bound}, Results: []ssa.Value{cond}})
	b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: body}, {Block: exit}}})

	counter := b.Value(ssa.TypeI32)
	b.Add(body, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{counter}})
	one := b.Value(ssa.TypeI32)
	b.Add(body, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
	next := b.Value(ssa.TypeI32)
	b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{counter, one}, Results: []ssa.Value{next}})
	advanced := b.Value(ssa.TypeState)
	b.Add(body, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 9}}, Results: []ssa.Value{advanced}})
	b.Add(body, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Index: 0}, Args: []ssa.Value{next}, State: advanced})
	b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header}}})

	b.Term(exit, ssa.Terminator{Op: ssa.OpReturn})
	return b.Build()
}

// store writes held into slot, materializing the interpreter state every store
// resumes into.
func store(b *ssa.Builder, block int, slot ssa.Slot, held ssa.Value) {
	state := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 1}}, Results: []ssa.Value{state}})
	b.Add(block, ssa.Operation{Op: ssa.OpStore, Slot: slot, Args: []ssa.Value{held}, State: state})
}

// find returns the first block matching want, or -1 and the zero block. A pass
// that rebuilds renumbers blocks as it walks, so a test that ran one names the
// block it means by what is in it.
func find(fn *ssa.Function, want func(ssa.Block) bool) (int, ssa.Block) {
	for id := range fn.Len() {
		if blk := fn.Block(id); want(blk) {
			return id, blk
		}
	}
	return -1, ssa.Block{}
}

// resumes returns the interpreter state materialized in blk.
func resumes(blk ssa.Block) (ssa.Operation, bool) {
	for _, op := range blk.Ops {
		if op.Op == ssa.OpState {
			return op, true
		}
	}
	return ssa.Operation{}, false
}

// count is how many operations fn performs of one kind.
func count(fn *ssa.Function, want ssa.Op) int {
	n := 0
	for id := range fn.Len() {
		for _, op := range fn.Block(id).Ops {
			if op.Op == want {
				n++
			}
		}
	}
	return n
}
