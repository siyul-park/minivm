package jit_test

import (
	"sort"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

// fakeTraces is a minimal jit.RecordedTraces client built from a fixed set of
// trees, standing in for interp's tracer: it is a public API client of the
// interface TracePlan reads and depends on no private state.
type fakeTraces struct {
	trees map[jit.Anchor]*jit.Tree
}

func (f fakeTraces) Anchors(addr int) []int {
	var out []int
	for a, tree := range f.trees {
		if a.Addr == addr && tree.Root != nil {
			out = append(out, a.IP)
		}
	}
	sort.Ints(out)
	return out
}

func (f fakeTraces) RootAt(a jit.Anchor) *jit.Tree {
	tree, ok := f.trees[a]
	if !ok || tree.Root == nil {
		return nil
	}
	return tree
}

func TestTracePlan(t *testing.T) {
	t.Run("folds a hot returned leg", func(t *testing.T) {
		root := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1},
			Ops: []jit.Record{
				{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 0}},
				{Step: jit.Step{Op: instr.BR_IF, Fn: 1, IP: 5}, Target: 12, Taken: true},
			},
			Status: jit.StatusReturned,
		}
		continuation := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 12},
			Ops:    []jit.Record{{Step: jit.Step{Op: instr.RETURN, Fn: 1, IP: 12}}},
			Status: jit.StatusReturned,
		}
		traces := fakeTraces{trees: map[jit.Anchor]*jit.Tree{
			{Addr: 1}: {
				Root:     root,
				Branches: map[int]*jit.Trace{0: continuation},
				Hits:     []int64{9},
				Exits:    map[jit.Anchor]int{{Addr: 1, IP: 12}: 0},
			},
		}}
		input := &jit.Input{
			Traces:   traces,
			Address:  1,
			Function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := jit.TracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].Valid())
		require.GreaterOrEqual(t, len(plans[0].Blocks), 2)
		entry := plans[0].Blocks[plans[0].Root]
		require.Equal(t, jit.TerminateBranchIf, entry.Term.Kind)
		require.Equal(t, uint64(0), entry.Steps[0].Args[0])
		for _, op := range entry.Steps {
			require.NotEqual(t, instr.BR_IF, op.Op)
		}
		require.Equal(t, continuation.Anchor, plans[0].Blocks[len(plans[0].Blocks)-1].Anchor)
	})

	t.Run("a leg cut at the loop header folds into the back-edge", func(t *testing.T) {
		root := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 2},
			Ops:    []jit.Record{{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 2}}},
			Status: jit.StatusLoop,
		}
		leg := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 20},
			Ops: []jit.Record{
				{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 20}},
				{Step: jit.Step{Fn: 1}, Target: 2, Cut: true},
			},
			Status: jit.StatusPartial,
		}
		traces := fakeTraces{trees: map[jit.Anchor]*jit.Tree{
			{Addr: 1, IP: 2}: {
				Root:     root,
				Branches: map[int]*jit.Trace{0: leg},
				Hits:     []int64{9},
				Exits:    map[jit.Anchor]int{{Addr: 1, IP: 20}: 0},
			},
		}}
		input := &jit.Input{
			Traces:   traces,
			Address:  1,
			Function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := jit.TracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].Valid())
		last := plans[0].Blocks[len(plans[0].Blocks)-1]
		require.Equal(t, jit.Anchor{Addr: 1, IP: 20}, last.Anchor)
		require.Equal(t, jit.TerminateBranch, last.Term.Kind)
		require.Equal(t, plans[0].Root, last.Term.Edges[0].Index)
	})

	t.Run("an explicit back-edge branch before the cut leaves no spurious block", func(t *testing.T) {
		root := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 2},
			Ops:    []jit.Record{{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 2}}},
			Status: jit.StatusLoop,
		}
		leg := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 20},
			Ops: []jit.Record{
				{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 20}},
				{Step: jit.Step{Op: instr.BR, Fn: 1, IP: 25}, Target: 2},
				{Step: jit.Step{Fn: 1}, Target: 2, Cut: true},
			},
			Status: jit.StatusPartial,
		}
		traces := fakeTraces{trees: map[jit.Anchor]*jit.Tree{
			{Addr: 1, IP: 2}: {
				Root:     root,
				Branches: map[int]*jit.Trace{0: leg},
				Hits:     []int64{9},
				Exits:    map[jit.Anchor]int{{Addr: 1, IP: 20}: 0},
			},
		}}
		input := &jit.Input{
			Traces:   traces,
			Address:  1,
			Function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := jit.TracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].Valid())
		require.Len(t, plans[0].Blocks, 2)
		last := plans[0].Blocks[1]
		require.Equal(t, jit.Anchor{Addr: 1, IP: 20}, last.Anchor)
		require.Equal(t, jit.TerminateBranch, last.Term.Kind)
		require.Equal(t, plans[0].Root, last.Term.Edges[0].Index)
	})

	t.Run("an inlined-frame branch before a matching cut stays a fallback", func(t *testing.T) {
		root := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 2},
			Ops:    []jit.Record{{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 2}}},
			Status: jit.StatusLoop,
		}
		leg := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 20},
			Ops: []jit.Record{
				{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 20}},
				{Step: jit.Step{Op: instr.BR, Fn: 1, IP: 25, Depth: 1}, Target: 2},
				{Step: jit.Step{Fn: 1, IP: 2, Depth: 1}, Target: 2, Cut: true},
			},
			Status: jit.StatusPartial,
		}
		traces := fakeTraces{trees: map[jit.Anchor]*jit.Tree{
			{Addr: 1, IP: 2}: {
				Root:     root,
				Branches: map[int]*jit.Trace{0: leg},
				Hits:     []int64{9},
				Exits:    map[jit.Anchor]int{{Addr: 1, IP: 20}: 0},
			},
		}}
		input := &jit.Input{
			Traces:   traces,
			Address:  1,
			Function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := jit.TracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].Valid())
		require.Len(t, plans[0].Blocks, 3)
		branch := plans[0].Blocks[1]
		fallback := plans[0].Blocks[2]
		require.Equal(t, jit.TerminateBranch, branch.Term.Kind)
		require.Equal(t, 2, branch.Term.Edges[0].Index)
		require.Equal(t, jit.Anchor{Addr: 1, IP: 2}, fallback.Anchor)
		require.Equal(t, jit.TerminateFallback, fallback.Term.Kind)
		require.Equal(t, 2, fallback.Term.IP)
	})

	t.Run("a cut elsewhere stays a fallback", func(t *testing.T) {
		root := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 2},
			Ops:    []jit.Record{{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 2}}},
			Status: jit.StatusLoop,
		}
		leg := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 20},
			Ops: []jit.Record{
				{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 20}},
				{Step: jit.Step{Fn: 1}, Target: 50, Cut: true},
			},
			Status: jit.StatusPartial,
		}
		traces := fakeTraces{trees: map[jit.Anchor]*jit.Tree{
			{Addr: 1, IP: 2}: {
				Root:     root,
				Branches: map[int]*jit.Trace{0: leg},
				Hits:     []int64{9},
				Exits:    map[jit.Anchor]int{{Addr: 1, IP: 20}: 0},
			},
		}}
		input := &jit.Input{
			Traces:   traces,
			Address:  1,
			Function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := jit.TracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].Valid())
		last := plans[0].Blocks[len(plans[0].Blocks)-1]
		require.Equal(t, jit.TerminateFallback, last.Term.Kind)
		require.Equal(t, 50, last.Term.IP)
	})

	t.Run("a loop-kind leg is not split", func(t *testing.T) {
		root := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 2},
			Ops:    []jit.Record{{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 2}}},
			Status: jit.StatusLoop,
		}
		other := &jit.Trace{
			Anchor: jit.Anchor{Addr: 1, IP: 40},
			Ops:    []jit.Record{{Step: jit.Step{Op: instr.I32_CONST, Fn: 1, IP: 40}}},
			Status: jit.StatusLoop,
		}
		traces := fakeTraces{trees: map[jit.Anchor]*jit.Tree{
			{Addr: 1, IP: 2}: {
				Root:     root,
				Branches: map[int]*jit.Trace{0: root, 1: other},
				Hits:     []int64{9, 9},
				Exits:    map[jit.Anchor]int{{Addr: 1, IP: 2}: 0, {Addr: 1, IP: 40}: 1},
			},
		}}
		input := &jit.Input{
			Traces:   traces,
			Address:  1,
			Function: &types.Function{Code: []byte{byte(instr.NOP)}},
		}

		plans, err := jit.TracePlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.True(t, plans[0].Valid())
		require.Len(t, plans[0].Blocks, 1)
	})

}
