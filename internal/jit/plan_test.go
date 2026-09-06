package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"

	"github.com/stretchr/testify/require"
)

func TestAnchor_Kind(t *testing.T) {
	tests := []struct {
		name   string
		anchor jit.Anchor
		want   jit.EntryKind
	}{
		{name: "module body", anchor: jit.Anchor{}, want: jit.EntryModule},
		{name: "function entry", anchor: jit.Anchor{Addr: 1}, want: jit.EntryFunction},
		{name: "loop header", anchor: jit.Anchor{Addr: 1, IP: 4}, want: jit.EntryLoop},
		{name: "module loop header", anchor: jit.Anchor{IP: 4}, want: jit.EntryLoop},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.anchor.Kind())
		})
	}

	t.Run("classifies a negative coordinate as no entry", func(t *testing.T) {
		require.Equal(t, jit.Anchor{Addr: -1}.Kind(), jit.Anchor{IP: -1}.Kind())
		require.NotEqual(t, jit.EntryModule, jit.Anchor{Addr: -1}.Kind())
		require.Equal(t, prof.EntryNone, jit.Anchor{Addr: -1}.Kind().Profile())
	})
}

func TestPlan_Valid(t *testing.T) {
	tests := []struct {
		name string
		plan jit.Plan
		want bool
	}{
		{
			name: "invalid entry",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1}, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1}, Term: jit.Terminator{Kind: jit.TerminateReturn}}}},
			want: false,
		},
		{
			name: "invalid branch targets",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryFunction, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1}, Term: jit.Terminator{Kind: jit.TerminateBranchIf, Edges: []jit.Edge{{Anchor: jit.Anchor{Addr: 1, IP: 4}, Index: jit.NoBlock}}}}}},
			want: false,
		},
		{
			name: "invalid tail",
			plan: jit.Plan{
				Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryFunction,
				Blocks: []jit.Block{{
					Anchor: jit.Anchor{Addr: 1},
					Term: jit.Terminator{
						Kind: jit.TerminateBranch,
						Edges: []jit.Edge{{
							Anchor: jit.Anchor{Addr: 1, IP: 4},
							Index:  jit.NoBlock,
							Tail:   []int{1},
						}},
					},
				}},
			},
			want: false,
		},
		{
			name: "function",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryFunction, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1}, Term: jit.Terminator{Kind: jit.TerminateReturn}}}},
			want: true,
		},
		{
			name: "loop",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1, IP: 4}, Kind: jit.EntryLoop, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1, IP: 4}, Term: jit.Terminator{Kind: jit.TerminateBranch, Edges: []jit.Edge{{Anchor: jit.Anchor{Addr: 1, IP: 4}, Index: 0}}}}}},
			want: true,
		},
		{
			name: "module loop",
			plan: jit.Plan{Anchor: jit.Anchor{IP: 4}, Kind: jit.EntryLoop, Blocks: []jit.Block{{Anchor: jit.Anchor{IP: 4}, Term: jit.Terminator{Kind: jit.TerminateBranch, Edges: []jit.Edge{{Anchor: jit.Anchor{IP: 4}, Index: 0}}}}}},
			want: true,
		},
		{
			name: "module",
			plan: jit.Plan{Anchor: jit.Anchor{}, Kind: jit.EntryModule, Blocks: []jit.Block{{Anchor: jit.Anchor{}, Term: jit.Terminator{Kind: jit.TerminateComplete}}}},
			want: true,
		},
		{
			name: "missing entry",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryFunction, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1, IP: 4}, Term: jit.Terminator{Kind: jit.TerminateReturn}}}},
			want: false,
		},
		{
			name: "context blocks",
			plan: jit.Plan{
				Anchor: jit.Anchor{Addr: 1},
				Kind:   jit.EntryFunction,
				Blocks: []jit.Block{
					{Anchor: jit.Anchor{Addr: 1}, Term: jit.Terminator{Kind: jit.TerminateReturn}},
					{Anchor: jit.Anchor{Addr: 1, IP: 4}, Term: jit.Terminator{Kind: jit.TerminateReturn}},
					{Anchor: jit.Anchor{Addr: 1, IP: 4}, Term: jit.Terminator{Kind: jit.TerminateReturn}},
				},
			},
			want: true,
		},
		{
			name: "fallback target",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryFunction, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1}, Term: jit.Terminator{Kind: jit.TerminateBranch, Edges: []jit.Edge{{Anchor: jit.Anchor{Addr: 1, IP: 4}, Index: jit.NoBlock}}}}}},
			want: true,
		},
		{
			name: "invalid function anchor",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1, IP: 4}, Kind: jit.EntryFunction, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1, IP: 4}}}},
			want: false,
		},
		{
			name: "invalid loop anchor",
			plan: jit.Plan{Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryLoop, Blocks: []jit.Block{{Anchor: jit.Anchor{Addr: 1}}}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.plan.Valid())
		})
	}

	t.Run("normalized blocks", func(t *testing.T) {
		static := jit.Plan{
			Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryFunction,
			Blocks: []jit.Block{{
				Anchor: jit.Anchor{Addr: 1},
				State:  []jit.Slot{},
				Term:   jit.Terminator{Kind: jit.TerminateReturn},
			}},
		}
		observed := jit.Plan{
			Anchor: jit.Anchor{Addr: 1}, Kind: jit.EntryFunction,
			Blocks: []jit.Block{{
				Anchor: jit.Anchor{Addr: 1},
				Term:   jit.Terminator{Kind: jit.TerminateReturn},
			}},
		}

		require.True(t, static.Valid())
		require.True(t, observed.Valid())
		require.NotNil(t, static.Blocks[0].State)
		require.Nil(t, observed.Blocks[0].State)
	})
}
