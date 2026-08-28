package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestStaticPlan(t *testing.T) {
	builder := instr.NewBuilder()
	other := builder.Label()
	done := builder.Label()
	builder.Emit(instr.I32_CONST, 1).BrIf(other)
	builder.Emit(instr.I32_CONST, 2).Br(done)
	builder.Bind(other).Emit(instr.I32_CONST, 3)
	builder.Bind(done).Emit(instr.RETURN)
	instructions, err := builder.Assemble()
	require.NoError(t, err)

	fn := &types.Function{
		Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(instructions),
	}
	input := &jit.Input{Address: 1, Function: fn}
	plans, err := jit.StaticPlan(input)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.True(t, plans[0].Valid())
	require.Equal(t, jit.EntryFunction, plans[0].Kind)
	require.Equal(t, jit.TerminateBranchIf, plans[0].Blocks[0].Term.Kind)
	require.Len(t, plans[0].Blocks[0].Term.Edges, 2)

	t.Run("direct call facts", func(t *testing.T) {
		callee := &types.Function{Typ: &types.FunctionType{}}
		caller := &types.Function{
			Typ:  &types.FunctionType{},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN)}),
		}
		input := &jit.Input{
			Address:   1,
			Function:  caller,
			Constants: []types.Boxed{types.BoxRef(2)},
			Heap:      []types.Value{nil, nil, callee},
		}

		plans, err := jit.StaticPlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Equal(t, uint64(0), plans[0].Blocks[0].Steps[0].Args[0])
		require.Equal(t, 2, plans[0].Blocks[0].Steps[1].Callee)
	})

	t.Run("struct get resolves the field kind", func(t *testing.T) {
		structTyp := types.NewStructType(types.NewStructField(types.TypeF64))
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{structTyp}, Returns: []types.Type{types.TypeF64}},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.STRUCT_GET),
				instr.New(instr.RETURN),
			}),
		}
		plans, err := jit.StaticPlan(&jit.Input{Address: 1, Function: fn})
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Equal(t, types.KindF64, plans[0].Blocks[0].Steps[2].Seen.Kind())
	})

	t.Run("a store plans like any other step", func(t *testing.T) {
		// Plan.NoSpill forced the whole build off the spill frame for any
		// function holding a container store, because the allocator's own
		// spill-eligibility check could not yet tell a sound spill from an
		// unsound one around a store's branches. Now that the allocator
		// judges each spill by dominance (internal/asm/dominance.go), a
		// store carries no plan-level restriction of its own: it is just
		// another step, and asm declines an unsound spill on its own merits.
		store := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI32Array}},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_SET),
				instr.New(instr.RETURN),
			}),
		}
		plans, err := jit.StaticPlan(&jit.Input{Address: 1, Function: store})
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].Valid())
		require.Equal(t, instr.ARRAY_SET, plans[0].Blocks[0].Steps[3].Op)
	})

	t.Run("converging paths share one block", func(t *testing.T) {
		require.Len(t, plans[0].Blocks[0].Term.Edges, 2)
		left := plans[0].Blocks[0].Term.Edges[0].Index
		right := plans[0].Blocks[0].Term.Edges[1].Index
		require.NotEqual(t, left, right)
		require.Equal(t,
			plans[0].Blocks[left].Term.Edges[0].Index,
			plans[0].Blocks[right].Term.Edges[0].Index,
			"both arms fall through to the same join, which must be interned as one block")
	})

	t.Run("a loop root is pruned to what it reaches", func(t *testing.T) {
		b := instr.NewBuilder()
		loop := b.Label()
		done := b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 64).Emit(instr.I32_GE_S).BrIf(done).
			Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).
			Br(loop).
			Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		instructions, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(instructions),
		}

		plans, err := jit.StaticPlan(&jit.Input{Address: 1, Function: fn})
		require.NoError(t, err)
		require.Len(t, plans, 2)

		entry, header := plans[0], plans[1]
		require.Equal(t, jit.EntryFunction, entry.Kind)
		require.Equal(t, jit.EntryLoop, header.Kind)

		// The loop plan is anchored at the header and drops the
		// initialization block the entry plan starts with, so it emits less
		// than the whole function for the same body.
		require.Equal(t, 0, entry.Anchor.IP)
		require.NotEqual(t, 0, header.Anchor.IP)
		require.Less(t, len(header.Blocks), len(entry.Blocks))
		require.True(t, header.Valid())

		// The accumulator is read and written every iteration, so both plans
		// carry it in a register across the back edge.
		require.Equal(t, []int{0}, entry.Carried)
		require.Equal(t, []int{0}, header.Carried)
	})

	t.Run("a straight-line function carries nothing", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.RETURN),
			}),
		}
		plans, err := jit.StaticPlan(&jit.Input{Address: 1, Function: fn})
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Nil(t, plans[0].Carried, "with no back edge there is nothing to carry")
	})

	t.Run("struct get with an unknown index rejects the plan", func(t *testing.T) {
		structTyp := types.NewStructType(types.NewStructField(types.TypeF64))
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{structTyp, types.TypeI32}, Returns: []types.Type{types.TypeF64}},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 1),
				instr.New(instr.STRUCT_GET),
				instr.New(instr.RETURN),
			}),
		}
		plans, err := jit.StaticPlan(&jit.Input{Address: 1, Function: fn})
		require.NoError(t, err)
		require.Empty(t, plans)
	})
}
