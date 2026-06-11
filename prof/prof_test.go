package prof

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	p := New()
	require.NotNil(t, p)
}

func TestStats_Add(t *testing.T) {
	t.Run("counts samples", func(t *testing.T) {
		tests := []struct {
			fn int
			ip int
			op byte
			n  int
		}{
			{fn: 0, ip: 0, op: 1, n: 1},
			{fn: 0, ip: 5, op: 2, n: 3},
			{fn: 2, ip: 0, op: 3, n: 2},
		}
		for _, tt := range tests {
			p := New()
			for range tt.n {
				p.Add(tt.fn, tt.ip, tt.op)
			}
			require.Equal(t, uint64(tt.n), p.Samples(tt.fn))
			require.Equal(t, uint64(tt.n), p.IP(tt.fn, tt.ip).Samples)
			require.Equal(t, uint64(tt.n), p.Snapshot().Opcodes[0].Samples)
		}
	})

	t.Run("ignores negative indexes", func(t *testing.T) {
		p := New()
		p.Add(-1, 0, 1)
		p.Add(0, -1, 1)

		require.Zero(t, p.Snapshot().Samples)
		require.Zero(t, p.Samples(0))
	})
}

func TestStats_Samples(t *testing.T) {
	tests := []struct {
		records [][3]int
		idx     int
		want    uint64
	}{
		{records: nil, idx: 0, want: 0},
		{records: [][3]int{{0, 0, 1}, {0, 1, 2}, {0, 0, 1}}, idx: 0, want: 3},
		{records: [][3]int{{1, 0, 1}}, idx: 0, want: 0},
		{records: [][3]int{{1, 0, 1}}, idx: 1, want: 1},
		{records: [][3]int{{1, 0, 1}}, idx: -1, want: 0},
	}
	for _, tt := range tests {
		p := New()
		for _, rec := range tt.records {
			p.Add(rec[0], rec[1], byte(rec[2]))
		}
		require.Equal(t, tt.want, p.Samples(tt.idx))
	}
}

func TestStats_Func(t *testing.T) {
	p := New()
	p.Add(0, 1, 10)
	p.Add(0, 1, 10)
	p.Add(0, 3, 11)
	p.Add(1, 0, 12)

	fn := p.Func(0)
	require.Equal(t, 0, fn.Index)
	require.Equal(t, uint64(3), fn.Samples)
	require.InDelta(t, 75, fn.Percent, 0.001)
	require.Equal(t, []IP{
		{Offset: 1, Samples: 2, Percent: 66.66666666666666},
		{Offset: 3, Samples: 1, Percent: 33.33333333333333},
	}, fn.IPs)

	missing := p.Func(99)
	require.Equal(t, 99, missing.Index)
	require.Zero(t, missing.Samples)
	require.Empty(t, missing.IPs)
}

func TestStats_IP(t *testing.T) {
	tests := []struct {
		records [][3]int
		idx     int
		ip      int
		want    uint64
	}{
		{records: nil, idx: 0, ip: 0, want: 0},
		{records: [][3]int{{0, 3, 1}, {0, 3, 1}}, idx: 0, ip: 3, want: 2},
		{records: [][3]int{{0, 3, 1}}, idx: 0, ip: 4, want: 0},
		{records: [][3]int{{0, 3, 1}}, idx: 1, ip: 3, want: 0},
		{records: [][3]int{{0, 3, 1}}, idx: 0, ip: -1, want: 0},
	}
	for _, tt := range tests {
		p := New()
		for _, rec := range tt.records {
			p.Add(rec[0], rec[1], byte(rec[2]))
		}
		require.Equal(t, tt.want, p.IP(tt.idx, tt.ip).Samples)
		require.Equal(t, tt.ip, p.IP(tt.idx, tt.ip).Offset)
	}
}

func TestStats_Range(t *testing.T) {
	tests := []struct {
		records [][3]int
		idx     int
		start   int
		end     int
		want    uint64
	}{
		{records: [][3]int{{0, 0, 1}, {0, 1, 1}, {0, 2, 1}}, idx: 0, start: 0, end: 3, want: 3},
		{records: [][3]int{{0, 1, 1}, {0, 1, 1}, {0, 2, 1}}, idx: 0, start: 1, end: 3, want: 3},
		{records: [][3]int{{0, 0, 1}}, idx: 0, start: 5, end: 10, want: 0},
		{records: [][3]int{{0, 0, 1}}, idx: 0, start: 0, end: 0, want: 0},
		{records: [][3]int{{0, 0, 1}}, idx: 0, start: -1, end: 1, want: 1},
		{records: nil, idx: 99, start: 0, end: 10, want: 0},
	}
	for _, tt := range tests {
		p := New()
		for _, rec := range tt.records {
			p.Add(rec[0], rec[1], byte(rec[2]))
		}
		require.Equal(t, tt.want, p.Range(tt.idx, tt.start, tt.end))
	}
}

func TestStats_Snapshot(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		p := New()
		snap := p.Snapshot()
		require.Zero(t, snap.Samples)
		require.Empty(t, snap.Funcs)
		require.Empty(t, snap.Opcodes)
	})

	t.Run("populated", func(t *testing.T) {
		p := New()
		p.Add(0, 1, 10)
		p.Add(0, 1, 10)
		p.Add(0, 3, 11)
		p.Add(1, 0, 10)

		snap := p.Snapshot()
		require.Equal(t, uint64(4), snap.Samples)
		require.Len(t, snap.Funcs, 2)
		require.Equal(t, uint64(3), snap.Funcs[0].Samples)
		require.Equal(t, uint64(2), snap.Funcs[0].IPs[0].Samples)
		require.InDelta(t, 75, snap.Funcs[0].Percent, 0.001)
		require.Len(t, snap.Opcodes, 2)
		require.Equal(t, Opcode{Code: 10, Samples: 3, Percent: 75}, snap.Opcodes[0])
		require.Equal(t, Opcode{Code: 11, Samples: 1, Percent: 25}, snap.Opcodes[1])
	})

	t.Run("deep copy", func(t *testing.T) {
		p := New()
		p.Add(0, 0, 1)
		snap := p.Snapshot()

		p.Add(0, 0, 1)

		require.Equal(t, uint64(1), snap.Samples)
		require.Equal(t, uint64(1), snap.Funcs[0].Samples)
		require.Equal(t, uint64(2), p.Samples(0))
	})
}

func TestStats_JITAdd(t *testing.T) {
	p := New()
	p.JITAdd(JIT{Attempts: 1, Errors: 1})
	p.JITAdd(JIT{Emits: 2, Links: 1, Skips: 1, Bytes: 12})

	require.Equal(t, JIT{
		Attempts: 1,
		Emits:    2,
		Links:    1,
		Skips:    1,
		Errors:   1,
		Bytes:    12,
	}, p.Snapshot().JIT)
}
