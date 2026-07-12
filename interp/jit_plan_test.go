package interp

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestPlan(t *testing.T) {
	tests := []struct {
		name string
		plan plan
		want bool
	}{
		{
			name: "invalid entry",
			plan: plan{entry: entry{anchor: anchor{addr: 1}}, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateReturn}}}},
			want: false,
		},
		{
			name: "invalid branch targets",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateBranchIf, targets: []int{4}}}}},
			want: false,
		},
		{
			name: "invalid tail",
			plan: plan{
				entry: entry{anchor: anchor{addr: 1}, kind: entryFunction},
				blocks: []block{{
					anchor: anchor{addr: 1},
					term: terminator{
						kind:    terminateBranch,
						targets: []int{4},
						tail:    []block{{term: terminator{kind: terminateBranchIf, targets: []int{8}}}},
					},
				}},
			},
			want: false,
		},
		{
			name: "function",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateReturn}}}},
			want: true,
		},
		{
			name: "loop",
			plan: plan{entry: entry{anchor: anchor{addr: 1, ip: 4}, kind: entryLoop}, blocks: []block{{anchor: anchor{addr: 1, ip: 4}, term: terminator{kind: terminateBranch, targets: []int{4}}}}},
			want: true,
		},
		{
			name: "module",
			plan: plan{entry: entry{anchor: anchor{}, kind: entryModule}, blocks: []block{{anchor: anchor{}, term: terminator{kind: terminateComplete}}}},
			want: true,
		},
		{
			name: "missing entry",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{anchor: anchor{addr: 1, ip: 4}, term: terminator{kind: terminateReturn}}}},
			want: false,
		},
		{
			name: "duplicate block",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{anchor: anchor{addr: 1}}, {anchor: anchor{addr: 1}}}},
			want: false,
		},
		{
			name: "fallback target",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{anchor: anchor{addr: 1}, term: terminator{kind: terminateBranch, targets: []int{4}}}}},
			want: true,
		},
		{
			name: "invalid function anchor",
			plan: plan{entry: entry{anchor: anchor{addr: 1, ip: 4}, kind: entryFunction}, blocks: []block{{anchor: anchor{addr: 1, ip: 4}}}},
			want: false,
		},
		{
			name: "invalid loop anchor",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryLoop}, blocks: []block{{anchor: anchor{addr: 1}}}},
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
			entry: entry{anchor: anchor{addr: 1}, kind: entryFunction},
			blocks: []block{{
				anchor: anchor{addr: 1},
				state:  &state{},
				term:   terminator{kind: terminateReturn},
			}},
		}
		observed := plan{
			entry: entry{anchor: anchor{addr: 1}, kind: entryFunction},
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

	t.Run("slot merge", func(t *testing.T) {
		dst := slot{kind: types.KindRef, ref: 7, refKnown: true, callee: 7, calleeKnown: true}
		changed, ok := mergeSlot(&dst, slot{kind: types.KindRef, ref: 8, refKnown: true, callee: 8, calleeKnown: true})
		require.True(t, ok)
		require.True(t, changed)
		require.False(t, dst.refKnown)
		require.False(t, dst.calleeKnown)

		_, ok = mergeSlot(&dst, slot{kind: types.KindI32})
		require.False(t, ok)
	})

	t.Run("spill policy", func(t *testing.T) {
		require.Equal(t, spillAllowed, planSpill([]block{{steps: []step{{op: instr.I32_ADD}}}}))
		require.Equal(t, spillForbidden, planSpill([]block{
			{steps: []step{{op: instr.I32_ADD}}},
			{steps: []step{{op: instr.I32_CONST}, {op: instr.STRUCT_SET}}},
		}))
	})

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
	plans, err := (staticPlanner{}).plan(input)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.True(t, plans[0].valid())
	require.Equal(t, entryFunction, plans[0].entry.kind)
	require.Equal(t, terminateBranchIf, plans[0].blocks[0].term.kind)
	require.Len(t, plans[0].blocks[0].term.targets, 2)

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

		plans, err := (staticPlanner{}).plan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Equal(t, uint64(0), plans[0].blocks[0].steps[0].args[0])
		require.Equal(t, 2, plans[0].blocks[0].steps[1].callee)
	})
}

func TestTracePlan(t *testing.T) {
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

	plans, err := (tracePlanner{}).plan(input)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.True(t, plans[0].valid())
	require.GreaterOrEqual(t, len(plans[0].blocks), 2)
	require.Equal(t, terminateBranchIf, plans[0].blocks[0].term.kind)
	require.Equal(t, uint64(0), plans[0].blocks[0].steps[0].args[0])
	for _, op := range plans[0].blocks[0].steps {
		require.NotEqual(t, instr.BR_IF, op.op)
	}
	require.Equal(t, int64(9), plans[0].blocks[len(plans[0].blocks)-1].hits)
}
