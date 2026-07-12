package interp

import (
	"testing"

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.plan.valid())
		})
	}
}
