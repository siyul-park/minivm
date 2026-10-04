package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/stretchr/testify/require"
)

func TestLedger_Charge(t *testing.T) {
	tests := []struct {
		name  string
		class jit.Class
		work  int64
		exits int
		want  bool
	}{
		{"a bridge with three units of work before it pays", jit.ClassBridge, 3, 1000, true},
		{"a bridge with two units of work before it does not", jit.ClassBridge, 2, 1000, false},
		{"a release with one unit of work before it pays", jit.ClassRelease, 1, 1000, true},
		{"a release with no work before it does not", jit.ClassRelease, 0, 1000, false},
		{"a call with four units of work before it pays", jit.ClassCall, 4, 1000, true},
		{"a call with three units of work before it does not", jit.ClassCall, 3, 1000, false},
		{"a callout with two units of work before it pays", jit.ClassCallout, 2, 1000, true},
		{"a callout with one unit of work before it does not", jit.ClassCallout, 1, 1000, false},
		{"slack absorbs 36 unpaid callouts", jit.ClassCallout, 0, 36, true},
		{"the 37th unpaid callout exceeds slack", jit.ClassCallout, 0, 37, false},
		{"a guard costs nothing", jit.ClassGuard, 0, 1000, true},
		{"a trap costs nothing", jit.ClassTrap, 0, 1000, true},
		{"slack absorbs 26 unpaid bridges", jit.ClassBridge, 0, 26, true},
		{"the 27th unpaid bridge exceeds slack", jit.ClassBridge, 0, 27, false},
		{"slack absorbs 18 unpaid calls", jit.ClassCall, 0, 18, true},
		{"the 19th unpaid call exceeds slack", jit.ClassCall, 0, 19, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ledger jit.Ledger
			pays := true
			for range tt.exits {
				ledger.Spend(tt.work)
				pays = ledger.Charge(tt.class)
			}
			require.Equal(t, tt.want, pays)
		})
	}
}

func TestLedger_Spend(t *testing.T) {
	tests := []struct {
		name  string
		work  int64
		exits int
		want  bool
	}{
		{"credit banks up to slack: 53 unpaid bridges after long work pay", 1 << 20, 53, true},
		{"credit stops at slack: the 54th does not", 1 << 20, 54, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ledger jit.Ledger
			ledger.Spend(tt.work)
			pays := true
			for range tt.exits {
				pays = ledger.Charge(jit.ClassBridge)
			}
			require.Equal(t, tt.want, pays)
		})
	}
}
