package cli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/prof"
)

func TestProfileReport(t *testing.T) {
	metrics := []prof.Metric{
		{Name: "vm_jit_compiles_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}, {Key: "trigger", Value: "hot"}, {Key: "frontend", Value: "trace"}, {Key: "outcome", Value: "rejected"}, {Key: "reason", Value: "no-plan"}}, Value: 2},
		{Name: "vm_jit_compiles_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}, {Key: "trigger", Value: "side-exit"}, {Key: "frontend", Value: "trace"}, {Key: "outcome", Value: "rejected"}, {Key: "reason", Value: "no-plan"}}, Value: 3},
	}

	profile := collect(metrics)
	first := profile.report()
	second := profile.report()

	require.Equal(t, first, second)
	require.Equal(t, []missRow{
		{miss: miss{anchor: anchor{fn: 1, ip: 4}, phase: "compile-side-exit", reason: "no-plan"}, count: 3},
		{miss: miss{anchor: anchor{fn: 1, ip: 4}, phase: "compile-hot", reason: "no-plan"}, count: 2},
	}, first.jit.misses)
}
