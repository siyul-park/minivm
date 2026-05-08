package prof

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	p := New()
	require.NotNil(t, p)
}

func TestProfile_Record(t *testing.T) {
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
		p := New()
		for range tt.n {
			p.Record(tt.funcIdx, tt.ip)
		}
		require.Equal(t, uint64(tt.n), p.Calls(tt.funcIdx))
		require.Equal(t, uint64(tt.n), p.Hits(tt.funcIdx, tt.ip))
	}
}

func TestProfile_Calls(t *testing.T) {
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
		p := New()
		for _, rec := range tt.records {
			p.Record(rec[0], rec[1])
		}
		require.Equal(t, tt.want, p.Calls(tt.idx))
	}
}

func TestProfile_Hits(t *testing.T) {
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
		p := New()
		for _, rec := range tt.records {
			p.Record(rec[0], rec[1])
		}
		require.Equal(t, tt.want, p.Hits(tt.idx, tt.ip))
	}
}

func TestProfile_HitsInRange(t *testing.T) {
	tests := []struct {
		records [][2]int
		idx     int
		start   int
		end     int
		want    uint64
	}{
		{records: [][2]int{{0, 0}, {0, 1}, {0, 2}}, idx: 0, start: 0, end: 3, want: 3},
		{records: [][2]int{{0, 1}, {0, 1}, {0, 2}}, idx: 0, start: 1, end: 3, want: 3},
		{records: [][2]int{{0, 0}}, idx: 0, start: 5, end: 10, want: 0},
		{records: [][2]int{{0, 0}}, idx: 0, start: 0, end: 0, want: 0},
		{records: nil, idx: 99, start: 0, end: 10, want: 0},
	}
	for _, tt := range tests {
		p := New()
		for _, rec := range tt.records {
			p.Record(rec[0], rec[1])
		}
		require.Equal(t, tt.want, p.HitsInRange(tt.idx, tt.start, tt.end))
	}
}

func TestProfile_Funcs(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		p := New()
		require.Empty(t, p.Funcs())
	})

	t.Run("populated", func(t *testing.T) {
		p := New()
		p.Record(0, 1)
		p.Record(0, 1)
		p.Record(0, 3)

		funcs := p.Funcs()
		require.Len(t, funcs, 1)
		require.Equal(t, uint64(3), funcs[0].Calls)
		require.Equal(t, uint64(2), funcs[0].Blocks[1])
		require.Equal(t, uint64(1), funcs[0].Blocks[3])
	})

	t.Run("deep copy", func(t *testing.T) {
		p := New()
		p.Record(0, 0)
		funcs := p.Funcs()

		p.Record(0, 0)

		require.Equal(t, uint64(1), funcs[0].Calls)
		require.Equal(t, uint64(2), p.Calls(0))
	})
}
