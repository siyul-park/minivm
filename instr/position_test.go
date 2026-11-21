package instr_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestFind(t *testing.T) {
	positions := []instr.Position{{IP: 2, Source: 7}, {IP: 5, Source: 0}, {IP: 9, Source: 4}}

	tests := []struct {
		name      string
		positions []instr.Position
		ip        int
		want      uint32
	}{
		{name: "empty", ip: 3, want: 0},
		{name: "before first", positions: positions, ip: 1, want: 0},
		{name: "exact ip", positions: positions, ip: 2, want: 7},
		{name: "inside run", positions: positions, ip: 4, want: 7},
		{name: "source zero run", positions: positions, ip: 6, want: 0},
		{name: "exact later ip", positions: positions, ip: 9, want: 4},
		{name: "past last", positions: positions, ip: 100, want: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, instr.Find(tt.positions, tt.ip))
		})
	}
}

func TestParsePosition(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		want := instr.Position{IP: 12, Source: 4294967295}
		got, err := instr.ParsePosition(want.String())
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	for name, s := range map[string]string{
		"empty":           "",
		"missing source":  "ip=1",
		"missing ip":      "source=1",
		"unknown key":     "ip=1 source=2 line=3",
		"no equals":       "ip=1 source",
		"bad ip":          "ip=x source=1",
		"negative ip":     "ip=-1 source=1",
		"bad source":      "ip=1 source=-1",
		"source overflow": "ip=1 source=4294967296",
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := instr.ParsePosition(s)
			require.Error(t, err)
		})
	}
}

func TestPosition_String(t *testing.T) {
	require.Equal(t, "ip=0 source=7", instr.Position{IP: 0, Source: 7}.String())
	require.Equal(t, "ip=12 source=0", instr.Position{IP: 12}.String())
}
