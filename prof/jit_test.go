package prof_test

import (
	"testing"

	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

func TestCollector_RecordCapture(t *testing.T) {
	c := prof.NewCollector()
	c.RecordCapture(2, 11, prof.CaptureOutcomePartial, prof.CaptureReasonOpLimit)
	c.RecordCapture(2, 11, prof.CaptureOutcomePartial, prof.CaptureReasonOpLimit)

	value, ok := c.Metric("vm_jit_trace_captures_total",
		prof.Label{Key: "func", Value: "2"}, prof.Label{Key: "ip", Value: "11"},
		prof.Label{Key: "outcome", Value: "partial"}, prof.Label{Key: "reason", Value: "op-limit"})
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}

func TestCollector_RecordCompile(t *testing.T) {
	c := prof.NewCollector()
	c.RecordCompile(2, 11, prof.TriggerSideExit, prof.FrontendTrace, prof.CompileOutcomeEmpty, prof.CompileReasonNoPlan)
	c.RecordCompile(2, 11, prof.TriggerSideExit, prof.FrontendTrace, prof.CompileOutcomeEmpty, prof.CompileReasonNoPlan)

	value, ok := c.Metric("vm_jit_compiles_total",
		prof.Label{Key: "func", Value: "2"}, prof.Label{Key: "ip", Value: "11"},
		prof.Label{Key: "trigger", Value: "side-exit"}, prof.Label{Key: "frontend", Value: "trace"},
		prof.Label{Key: "outcome", Value: "empty"}, prof.Label{Key: "reason", Value: "no-plan"})
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}

func TestCollector_RecordEmit(t *testing.T) {
	c := prof.NewCollector()
	c.RecordEmit(2, 11, prof.EntryStart, prof.FrontendTrace, 64)
	c.RecordEmit(2, 11, prof.EntryStart, prof.FrontendTrace, 64)
	labels := []prof.Label{
		{Key: "func", Value: "2"}, {Key: "ip", Value: "11"},
		{Key: "kind", Value: "start"}, {Key: "frontend", Value: "trace"},
	}

	emits, ok := c.Metric("vm_jit_entry_emits_total", labels...)
	require.True(t, ok)
	require.Equal(t, float64(2), emits)
	bytes, ok := c.Metric("vm_jit_entry_bytes_total", labels...)
	require.True(t, ok)
	require.Equal(t, float64(128), bytes)
}

func TestCounter_Inc(t *testing.T) {
	s := prof.NewCollector()
	entry := s.RegisterEntry(2, 11, prof.EntryCall, prof.FrontendStatic)
	exit := s.RegisterExit(2, 11, prof.EntryCall, prof.FrontendStatic, prof.ExitColdBranch, prof.OpcodeNone)
	yield := s.RegisterYield(2, 11, prof.EntryCall, prof.FrontendStatic)

	allocs := testing.AllocsPerRun(100, func() {
		entry.Inc()
		exit.Inc()
		yield.Inc()
	})
	require.Zero(t, allocs)
}

func TestCollector_RegisterEntry(t *testing.T) {
	local := prof.NewCollector()
	entry := local.RegisterEntry(3, 17, prof.EntryLoop, prof.FrontendTrace)
	p := prof.New()

	entry.Inc()
	p.Flush(local)
	entry.Inc()
	p.Flush(local)
	p.Flush(local)

	value, ok := p.Metric(
		"vm_jit_native_entries_total",
		prof.Label{Key: "func", Value: "3"},
		prof.Label{Key: "ip", Value: "17"},
		prof.Label{Key: "kind", Value: "loop"},
		prof.Label{Key: "frontend", Value: "trace"},
	)
	require.True(t, ok)
	require.Equal(t, float64(2), value)

	allocs := testing.AllocsPerRun(100, func() {
		entry.Inc()
		p.Flush(local)
	})
	require.Zero(t, allocs)
}

func TestCollector_RegisterExit(t *testing.T) {
	s := prof.NewCollector()
	s.RegisterExit(2, 11, prof.EntryStart, prof.FrontendTrace, prof.ExitTerminalOp, -2).Inc()
	s.RegisterExit(2, 11, prof.EntryStart, prof.FrontendTrace, prof.ExitTerminalOp, 256).Inc()

	value, ok := s.Metric(
		"vm_jit_native_exits_total",
		prof.Label{Key: "func", Value: "2"},
		prof.Label{Key: "ip", Value: "11"},
		prof.Label{Key: "kind", Value: "start"},
		prof.Label{Key: "frontend", Value: "trace"},
		prof.Label{Key: "reason", Value: "terminal-op"},
		prof.Label{Key: "opcode", Value: "none"},
	)
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}

func TestCollector_RegisterYield(t *testing.T) {
	local := prof.NewCollector()
	yield := local.RegisterYield(3, 17, prof.EntryLoop, prof.FrontendTrace)
	p := prof.New()

	yield.Inc()
	p.Flush(local)
	yield.Inc()
	p.Flush(local)
	p.Flush(local)

	value, ok := p.Metric("vm_jit_native_yields_total",
		prof.Label{Key: "func", Value: "3"}, prof.Label{Key: "ip", Value: "17"},
		prof.Label{Key: "kind", Value: "loop"}, prof.Label{Key: "frontend", Value: "trace"})
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}
