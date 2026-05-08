package prof

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	r := New()
	require.NotNil(t, r)
}

func TestRecorder_Record(t *testing.T) {
	tests := []struct {
		funcIdx int
		ip      int
		n       int
	}{
		{funcIdx: 0, ip: 0, n: 1},
		{funcIdx: 0, ip: 5, n: 3},
		{funcIdx: 2, ip: 0, n: 2},
	}
	for _, tt := range tests {
		r := New()
		for range tt.n {
			r.Record(tt.funcIdx, tt.ip)
		}
		require.Equal(t, uint64(tt.n), r.Calls(tt.funcIdx))
		require.Equal(t, uint64(tt.n), r.Hits(tt.funcIdx, tt.ip))
	}
}

func TestRecorder_Calls(t *testing.T) {
	tests := []struct {
		records [][2]int
		idx     int
		want    uint64
	}{
		{records: nil, idx: 0, want: 0},
		{records: [][2]int{{0, 0}, {0, 1}, {0, 0}}, idx: 0, want: 3},
		{records: [][2]int{{1, 0}}, idx: 0, want: 0},
		{records: [][2]int{{1, 0}}, idx: 1, want: 1},
	}
	for _, tt := range tests {
		r := New()
		for _, rec := range tt.records {
			r.Record(rec[0], rec[1])
		}
		require.Equal(t, tt.want, r.Calls(tt.idx))
	}
}

func TestRecorder_Hits(t *testing.T) {
	tests := []struct {
		records [][2]int
		idx     int
		ip      int
		want    uint64
	}{
		{records: nil, idx: 0, ip: 0, want: 0},
		{records: [][2]int{{0, 3}, {0, 3}}, idx: 0, ip: 3, want: 2},
		{records: [][2]int{{0, 3}}, idx: 0, ip: 4, want: 0},
		{records: [][2]int{{0, 3}}, idx: 1, ip: 3, want: 0},
	}
	for _, tt := range tests {
		r := New()
		for _, rec := range tt.records {
			r.Record(rec[0], rec[1])
		}
		require.Equal(t, tt.want, r.Hits(tt.idx, tt.ip))
	}
}

func TestRecorder_Snapshot(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		r := New()
		p := r.Snapshot()
		require.Empty(t, p.Funcs)
	})

	t.Run("populated", func(t *testing.T) {
		r := New()
		r.Record(0, 1)
		r.Record(0, 1)
		r.Record(0, 3)

		p := r.Snapshot()
		require.Len(t, p.Funcs, 1)
		require.Equal(t, uint64(3), p.Funcs[0].Calls)
		require.Equal(t, uint64(2), p.Funcs[0].Blocks[1])
		require.Equal(t, uint64(1), p.Funcs[0].Blocks[3])
	})

	t.Run("deep copy", func(t *testing.T) {
		r := New()
		r.Record(0, 0)
		p := r.Snapshot()

		r.Record(0, 0)

		require.Equal(t, uint64(1), p.Funcs[0].Calls)
		require.Equal(t, uint64(2), r.Calls(0))
	})
}
