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
			name: "function",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{offset: 0, term: terminator{kind: terminateReturn}}}},
			want: true,
		},
		{
			name: "loop",
			plan: plan{entry: entry{anchor: anchor{addr: 1, ip: 4}, kind: entryLoop}, blocks: []block{{offset: 4, term: terminator{kind: terminateBranch, targets: []int{4}}}}},
			want: true,
		},
		{
			name: "module",
			plan: plan{entry: entry{anchor: anchor{}, kind: entryModule}, blocks: []block{{offset: 0, term: terminator{kind: terminateComplete}}}},
			want: true,
		},
		{
			name: "missing entry",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{offset: 4, term: terminator{kind: terminateReturn}}}},
			want: false,
		},
		{
			name: "duplicate block",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{offset: 0}, {offset: 0}}},
			want: false,
		},
		{
			name: "missing target",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{offset: 0, term: terminator{kind: terminateBranch, targets: []int{4}}}}},
			want: false,
		},
		{
			name: "invalid function anchor",
			plan: plan{entry: entry{anchor: anchor{addr: 1, ip: 4}, kind: entryFunction}, blocks: []block{{offset: 4}}},
			want: false,
		},
		{
			name: "invalid loop anchor",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryLoop}, blocks: []block{{offset: 0}}},
			want: false,
		},
		{
			name: "mixed block state",
			plan: plan{entry: entry{anchor: anchor{addr: 1}, kind: entryFunction}, blocks: []block{{offset: 0, canonical: true}, {offset: 4}}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.plan.valid())
		})
	}

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
}

func TestTracePlan(t *testing.T) {
	root := &trace{
		anchor: anchor{addr: 1},
		ops: []step{
			{op: instr.I32_CONST, fn: 1, ip: 0},
			{op: instr.BR_IF, fn: 1, ip: 5, target: 12, taken: true},
		},
		kind: returned,
	}
	continuation := &trace{
		anchor: anchor{addr: 1, ip: 12},
		ops:    []step{{op: instr.RETURN, fn: 1, ip: 12}},
		kind:   returned,
	}
	tracer := NewTracer()
	tracer.trees[anchor{addr: 1}] = &tree{
		root:     root,
		branches: map[int]*trace{0: continuation},
		hits:     []int64{9},
		exits:    map[branch]int{{fn: 1, ip: 12}: 0},
	}
	input := &compileInput{
		interpreter: &Interpreter{tracer: tracer},
		address:     1,
		function:    &types.Function{Code: []byte{byte(instr.NOP)}},
	}

	plans, err := (tracePlanner{}).plan(input)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.True(t, plans[0].valid())
	require.Len(t, plans[0].blocks, 2)
	require.Equal(t, int64(9), plans[0].blocks[1].hits)
}
