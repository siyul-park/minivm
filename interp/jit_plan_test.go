package interp

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestPlan_Valid(t *testing.T) {
	tests := []struct {
		name string
		plan plan
		want bool
	}{
		{
			name: "invalid entry",
			plan: plan{anchor: anchor{addr: 1}, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateReturn}}}},
			want: false,
		},
		{
			name: "invalid branch targets",
			plan: plan{anchor: anchor{addr: 1}, kind: entryFunction, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateBranchIf, edges: []edge{jump(1, 4)}}}}},
			want: false,
		},
		{
			name: "invalid tail",
			plan: plan{
				anchor: anchor{addr: 1}, kind: entryFunction,
				blocks: []block{{
					anchor: anchor{addr: 1},
					term: terminator{
						kind: terminateBranch,
						edges: []edge{{
							anchor: anchor{addr: 1, ip: 4},
							block:  noBlock,
							tail:   []int{1},
						}},
					},
				}},
			},
			want: false,
		},
		{
			name: "function",
			plan: plan{anchor: anchor{addr: 1}, kind: entryFunction, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateReturn}}}},
			want: true,
		},
		{
			name: "loop",
			plan: plan{anchor: anchor{addr: 1, ip: 4}, kind: entryLoop, blocks: []block{{anchor: anchor{addr: 1, ip: 4}, term: terminator{kind: terminateBranch, edges: []edge{jump(1, 4)}}}}},
			want: true,
		},
		{
			name: "module loop",
			plan: plan{anchor: anchor{ip: 4}, kind: entryLoop, blocks: []block{{anchor: anchor{ip: 4}, term: terminator{kind: terminateBranch, edges: []edge{jump(0, 4)}}}}},
			want: true,
		},
		{
			name: "module",
			plan: plan{anchor: anchor{}, kind: entryModule, blocks: []block{{anchor: anchor{}, term: terminator{kind: terminateComplete}}}},
			want: true,
		},
		{
			name: "missing entry",
			plan: plan{anchor: anchor{addr: 1}, kind: entryFunction, blocks: []block{{anchor: anchor{addr: 1, ip: 4}, term: terminator{kind: terminateReturn}}}},
			want: false,
		},
		{
			name: "context blocks",
			plan: plan{
				anchor: anchor{addr: 1},
				kind:   entryFunction,
				blocks: []block{
					{anchor: anchor{addr: 1}, term: terminator{kind: terminateReturn}},
					{anchor: anchor{addr: 1, ip: 4}, term: terminator{kind: terminateReturn}},
					{anchor: anchor{addr: 1, ip: 4}, term: terminator{kind: terminateReturn}},
				},
			},
			want: true,
		},
		{
			name: "fallback target",
			plan: plan{anchor: anchor{addr: 1}, kind: entryFunction, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateBranch, edges: []edge{jump(1, 4)}}}}},
			want: true,
		},
		{
			name: "invalid function anchor",
			plan: plan{anchor: anchor{addr: 1, ip: 4}, kind: entryFunction, blocks: []block{{anchor: anchor{addr: 1, ip: 4}}}},
			want: false,
		},
		{
			name: "invalid loop anchor",
			plan: plan{anchor: anchor{addr: 1}, kind: entryLoop, blocks: []block{{anchor: anchor{addr: 1}}}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.plan.valid())
		})
	}

	t.Run("normalized blocks", func(t *testing.T) {
		static := plan{
			anchor: anchor{addr: 1}, kind: entryFunction,
			blocks: []block{{
				anchor: anchor{addr: 1},
				state:  []slot{},
				term:   terminator{kind: terminateReturn},
			}},
		}
		observed := plan{
			anchor: anchor{addr: 1}, kind: entryFunction,
			blocks: []block{{
				anchor: anchor{addr: 1},
				term:   terminator{kind: terminateReturn},
			}},
		}

		require.True(t, static.valid())
		require.True(t, observed.valid())
		require.NotNil(t, static.blocks[0].state)
		require.Nil(t, observed.blocks[0].state)
	})

}

func TestMergeSlot(t *testing.T) {
	dst := slot{kind: types.KindRef, ref: 7, refKnown: true, callee: 7, calleeKnown: true}
	changed, ok := mergeSlot(&dst, slot{kind: types.KindRef, ref: 8, refKnown: true, callee: 8, calleeKnown: true})
	require.True(t, ok)
	require.True(t, changed)
	require.False(t, dst.refKnown)
	require.False(t, dst.calleeKnown)

	_, ok = mergeSlot(&dst, slot{kind: types.KindI32})
	require.False(t, ok)
}

func TestStore(t *testing.T) {
	same := anchor{addr: 1, ip: 4}
	planned := plan{}
	ids := store(&planned, []block{
		{anchor: anchor{addr: 1}, term: terminator{kind: terminateBranch, edges: []edge{{anchor: same, block: local(1)}}}},
		{anchor: same, term: terminator{kind: terminateBranch, edges: []edge{{anchor: same, block: local(2)}}}},
		{anchor: same, term: terminator{kind: terminateReturn}},
	}, false)

	require.Equal(t, ids[1], planned.blocks[ids[0]].term.edges[0].block)
	require.Equal(t, ids[2], planned.blocks[ids[1]].term.edges[0].block)
}

func TestNoSpill(t *testing.T) {
	require.False(t, noSpill([]block{{steps: []step{{op: instr.I32_ADD}}}}))
	require.True(t, noSpill([]block{
		{steps: []step{{op: instr.I32_ADD}}},
		{steps: []step{{op: instr.I32_CONST}, {op: instr.STRUCT_SET}}},
	}))
	require.True(t, noSpill([]block{{steps: []step{
		{op: instr.ARRAY_SET},
		{op: instr.I32_ADD},
	}}}))
}

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
	input := &compileInput{address: 1, function: fn}
	plans, err := staticPlan(input)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.True(t, plans[0].valid())
	require.Equal(t, entryFunction, plans[0].kind)
	require.Equal(t, terminateBranchIf, plans[0].blocks[0].term.kind)
	require.Len(t, plans[0].blocks[0].term.edges, 2)

	t.Run("direct call facts", func(t *testing.T) {
		callee := &types.Function{Typ: &types.FunctionType{}}
		caller := &types.Function{
			Typ:  &types.FunctionType{},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN)}),
		}
		input := &compileInput{
			address:   1,
			function:  caller,
			constants: []types.Boxed{types.BoxRef(2)},
			heap:      []types.Value{nil, nil, callee},
		}

		plans, err := staticPlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Equal(t, uint64(0), plans[0].blocks[0].steps[0].args[0])
		require.Equal(t, 2, plans[0].blocks[0].steps[1].callee)
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
		plans, err := staticPlan(&compileInput{address: 1, function: fn})
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Equal(t, types.KindF64, plans[0].blocks[0].steps[2].seen.Kind())
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
		plans, err := staticPlan(&compileInput{address: 1, function: fn})
		require.NoError(t, err)
		require.Empty(t, plans)
	})
}

func TestTracePlan(t *testing.T) {
	t.Run("folds a hot returned leg", func(t *testing.T) {
		root := &trace{
			anchor: anchor{addr: 1},
			ops: []record{
				{step: step{op: instr.I32_CONST, fn: 1, ip: 0}},
				{step: step{op: instr.BR_IF, fn: 1, ip: 5}, target: 12, taken: true},
			},
			kind: returned,
		}
		continuation := &trace{
			anchor: anchor{addr: 1, ip: 12},
			ops:    []record{{step: step{op: instr.RETURN, fn: 1, ip: 12}}},
			kind:   returned,
		}
		tracer := NewTracer()
		tracer.trees[anchor{addr: 1}] = &tree{
			root:     root,
			branches: map[int]*trace{0: continuation},
			hits:     []int64{9},
			exits:    map[anchor]int{{addr: 1, ip: 12}: 0},
		}
		input := &compileInput{
			tracer:   tracer,
			address:  1,
			function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := tracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].valid())
		require.GreaterOrEqual(t, len(plans[0].blocks), 2)
		entry := plans[0].blocks[plans[0].root]
		require.Equal(t, terminateBranchIf, entry.term.kind)
		require.Equal(t, uint64(0), entry.steps[0].args[0])
		for _, op := range entry.steps {
			require.NotEqual(t, instr.BR_IF, op.op)
		}
		require.Equal(t, continuation.anchor, plans[0].blocks[len(plans[0].blocks)-1].anchor)
	})

	t.Run("a leg cut at the loop header folds into the back-edge", func(t *testing.T) {
		root := &trace{
			anchor: anchor{addr: 1, ip: 2},
			ops:    []record{{step: step{op: instr.I32_CONST, fn: 1, ip: 2}}},
			kind:   loop,
		}
		leg := &trace{
			anchor: anchor{addr: 1, ip: 20},
			ops: []record{
				{step: step{op: instr.I32_CONST, fn: 1, ip: 20}},
				{step: step{fn: 1}, target: 2, cut: true},
			},
			kind: partial,
		}
		tracer := NewTracer()
		tracer.trees[anchor{addr: 1, ip: 2}] = &tree{
			root:     root,
			branches: map[int]*trace{0: leg},
			hits:     []int64{9},
			exits:    map[anchor]int{{addr: 1, ip: 20}: 0},
		}
		input := &compileInput{
			tracer:   tracer,
			address:  1,
			function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := tracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].valid())
		last := plans[0].blocks[len(plans[0].blocks)-1]
		require.Equal(t, anchor{addr: 1, ip: 20}, last.anchor)
		require.Equal(t, terminateBranch, last.term.kind)
		require.Equal(t, plans[0].root, last.term.edges[0].block)
	})

	t.Run("an explicit back-edge branch before the cut leaves no spurious block", func(t *testing.T) {
		root := &trace{
			anchor: anchor{addr: 1, ip: 2},
			ops:    []record{{step: step{op: instr.I32_CONST, fn: 1, ip: 2}}},
			kind:   loop,
		}
		leg := &trace{
			anchor: anchor{addr: 1, ip: 20},
			ops: []record{
				{step: step{op: instr.I32_CONST, fn: 1, ip: 20}},
				{step: step{op: instr.BR, fn: 1, ip: 25}, target: 2},
				{step: step{fn: 1}, target: 2, cut: true},
			},
			kind: partial,
		}
		tracer := NewTracer()
		tracer.trees[anchor{addr: 1, ip: 2}] = &tree{
			root:     root,
			branches: map[int]*trace{0: leg},
			hits:     []int64{9},
			exits:    map[anchor]int{{addr: 1, ip: 20}: 0},
		}
		input := &compileInput{
			tracer:   tracer,
			address:  1,
			function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := tracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].valid())
		require.Len(t, plans[0].blocks, 2)
		last := plans[0].blocks[1]
		require.Equal(t, anchor{addr: 1, ip: 20}, last.anchor)
		require.Equal(t, terminateBranch, last.term.kind)
		require.Equal(t, plans[0].root, last.term.edges[0].block)
	})

	t.Run("an inlined-frame branch before a matching cut stays a fallback", func(t *testing.T) {
		root := &trace{
			anchor: anchor{addr: 1, ip: 2},
			ops:    []record{{step: step{op: instr.I32_CONST, fn: 1, ip: 2}}},
			kind:   loop,
		}
		leg := &trace{
			anchor: anchor{addr: 1, ip: 20},
			ops: []record{
				{step: step{op: instr.I32_CONST, fn: 1, ip: 20}},
				{step: step{op: instr.BR, fn: 1, ip: 25, depth: 1}, target: 2},
				{step: step{fn: 1, ip: 2, depth: 1}, target: 2, cut: true},
			},
			kind: partial,
		}
		tracer := NewTracer()
		tracer.trees[anchor{addr: 1, ip: 2}] = &tree{
			root:     root,
			branches: map[int]*trace{0: leg},
			hits:     []int64{9},
			exits:    map[anchor]int{{addr: 1, ip: 20}: 0},
		}
		input := &compileInput{
			tracer:   tracer,
			address:  1,
			function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := tracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].valid())
		require.Len(t, plans[0].blocks, 3)
		branch := plans[0].blocks[1]
		fallback := plans[0].blocks[2]
		require.Equal(t, terminateBranch, branch.term.kind)
		require.Equal(t, 2, branch.term.edges[0].block)
		require.Equal(t, anchor{addr: 1, ip: 2}, fallback.anchor)
		require.Equal(t, terminateFallback, fallback.term.kind)
		require.Equal(t, 2, fallback.term.ip)
	})

	t.Run("a cut elsewhere stays a fallback", func(t *testing.T) {
		root := &trace{
			anchor: anchor{addr: 1, ip: 2},
			ops:    []record{{step: step{op: instr.I32_CONST, fn: 1, ip: 2}}},
			kind:   loop,
		}
		leg := &trace{
			anchor: anchor{addr: 1, ip: 20},
			ops: []record{
				{step: step{op: instr.I32_CONST, fn: 1, ip: 20}},
				{step: step{fn: 1}, target: 50, cut: true},
			},
			kind: partial,
		}
		tracer := NewTracer()
		tracer.trees[anchor{addr: 1, ip: 2}] = &tree{
			root:     root,
			branches: map[int]*trace{0: leg},
			hits:     []int64{9},
			exits:    map[anchor]int{{addr: 1, ip: 20}: 0},
		}
		input := &compileInput{
			tracer:   tracer,
			address:  1,
			function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := tracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].valid())
		last := plans[0].blocks[len(plans[0].blocks)-1]
		require.Equal(t, terminateFallback, last.term.kind)
		require.Equal(t, 50, last.term.ip)
	})

	t.Run("a loop-kind leg is not split", func(t *testing.T) {
		root := &trace{
			anchor: anchor{addr: 1, ip: 2},
			ops:    []record{{step: step{op: instr.I32_CONST, fn: 1, ip: 2}}},
			kind:   loop,
		}
		other := &trace{
			anchor: anchor{addr: 1, ip: 40},
			ops:    []record{{step: step{op: instr.I32_CONST, fn: 1, ip: 40}}},
			kind:   loop,
		}
		tracer := NewTracer()
		tracer.trees[anchor{addr: 1, ip: 2}] = &tree{
			root:     root,
			branches: map[int]*trace{0: root, 1: other},
			hits:     []int64{9, 9},
			exits:    map[anchor]int{{addr: 1, ip: 2}: 0, {addr: 1, ip: 40}: 1},
		}
		input := &compileInput{
			tracer:   tracer,
			address:  1,
			function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := tracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].valid())
		require.Len(t, plans[0].blocks, 1)
	})
}

func TestHoistable(t *testing.T) {
	i32 := itab(types.TypedArray[int32](nil))
	fn := &types.Function{Locals: []types.Type{types.TypeI32Array, types.TypeI32}}
	access := []step{
		{op: instr.LOCAL_GET, args: [2]uint64{0}},
		{op: instr.LOCAL_GET, args: [2]uint64{1}},
		{op: instr.I32_CONST},
		{op: instr.ARRAY_SET, shape: shape{itab: i32}},
	}

	t.Run("picks the loop-invariant container", func(t *testing.T) {
		got := hoistable(fn, []block{{steps: access}})
		require.Equal(t, &hoist{local: 0, want: i32}, got)
	})

	t.Run("preserves a selected local source", func(t *testing.T) {
		steps := []step{
			{op: instr.LOCAL_GET, args: [2]uint64{0}},
			{op: instr.LOCAL_GET, args: [2]uint64{0}},
			{op: instr.I32_CONST},
			{op: instr.SELECT},
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.ARRAY_GET, shape: shape{itab: i32}},
		}
		require.Equal(t, &hoist{local: 0, want: i32}, hoistable(fn, []block{{steps: steps}}))
	})

	t.Run("a written container local is rejected", func(t *testing.T) {
		steps := append(append([]step{}, access...), step{op: instr.LOCAL_SET, args: [2]uint64{0}})
		require.Nil(t, hoistable(fn, []block{{steps: steps}}))
	})

	t.Run("a call rejects the plan", func(t *testing.T) {
		steps := append(append([]step{}, access...), step{op: instr.CALL})
		require.Nil(t, hoistable(fn, []block{{steps: steps}}))
	})

	t.Run("conflicting itabs are rejected", func(t *testing.T) {
		other := append(append([]step{}, access...), []step{
			{op: instr.LOCAL_GET, args: [2]uint64{0}},
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.ARRAY_GET, shape: shape{itab: itab(types.TypedArray[int64](nil))}},
		}...)
		require.Nil(t, hoistable(fn, []block{{steps: other}}))
	})

	t.Run("a scalar local is rejected", func(t *testing.T) {
		steps := []step{
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.I32_CONST},
			{op: instr.ARRAY_SET, shape: shape{itab: i32}},
		}
		require.Nil(t, hoistable(fn, []block{{steps: steps}}))
	})

	t.Run("an unsupported candidate cannot displace a primitive candidate", func(t *testing.T) {
		ref := itab((*types.Array)(nil))
		fn := &types.Function{Locals: []types.Type{types.TypeI32Array, types.TypeRef, types.TypeI32}}
		steps := []step{
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.LOCAL_GET, args: [2]uint64{2}},
			{op: instr.ARRAY_GET, shape: shape{itab: ref}},
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.LOCAL_GET, args: [2]uint64{2}},
			{op: instr.ARRAY_GET, shape: shape{itab: ref}},
			{op: instr.LOCAL_GET, args: [2]uint64{0}},
			{op: instr.LOCAL_GET, args: [2]uint64{2}},
			{op: instr.ARRAY_GET, shape: shape{itab: i32}},
		}
		require.Equal(t, &hoist{local: 0, want: i32}, hoistable(fn, []block{{steps: steps}}))
	})

	t.Run("a terminal store cannot displace a usable candidate", func(t *testing.T) {
		fn := &types.Function{Locals: []types.Type{types.TypeI32Array, types.TypeI32Array, types.TypeI32}}
		steps := []step{
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.LOCAL_GET, args: [2]uint64{2}},
			{op: instr.I32_CONST},
			{op: instr.ARRAY_SET, shape: shape{itab: i32}, terminal: true},
			{op: instr.LOCAL_GET, args: [2]uint64{1}},
			{op: instr.LOCAL_GET, args: [2]uint64{2}},
			{op: instr.I32_CONST},
			{op: instr.ARRAY_SET, shape: shape{itab: i32}, terminal: true},
			{op: instr.LOCAL_GET, args: [2]uint64{0}},
			{op: instr.LOCAL_GET, args: [2]uint64{2}},
			{op: instr.ARRAY_GET, shape: shape{itab: i32}},
		}
		require.Equal(t, &hoist{local: 0, want: i32}, hoistable(fn, []block{{steps: steps}}))
	})

	t.Run("the local offset must fit the ARM64 immediate", func(t *testing.T) {
		locals := make([]types.Type, 4097)
		locals[4095] = types.TypeI32Array
		locals[4096] = types.TypeI32Array
		fn := &types.Function{Locals: locals}
		steps := []step{
			{op: instr.LOCAL_GET, args: [2]uint64{4095}},
			{op: instr.I32_CONST},
			{op: instr.ARRAY_GET, shape: shape{itab: i32}},
		}
		require.Equal(t, &hoist{local: 4095, want: i32}, hoistable(fn, []block{{steps: steps}}))

		steps[0].args[0] = 4096
		require.Nil(t, hoistable(fn, []block{{steps: steps}}))
	})
}
