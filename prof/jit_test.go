package prof

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollector_RecordCapture(t *testing.T) {
	c := NewCollector()
	c.RecordCapture(2, 11, CaptureOutcomePartial, CaptureReasonOpLimit)
	c.RecordCapture(2, 11, CaptureOutcomePartial, CaptureReasonOpLimit)

	value, ok := c.Metric("vm_jit_trace_captures_total",
		Label{Key: "func", Value: "2"}, Label{Key: "ip", Value: "11"},
		Label{Key: "outcome", Value: "partial"}, Label{Key: "reason", Value: "op-limit"})
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}

func TestCollector_RecordCompile(t *testing.T) {
	c := NewCollector()
	c.RecordCompile(2, 11, TriggerSideExit, FrontendTrace, CompileOutcomeEmpty, CompileReasonNoPlan)
	c.RecordCompile(2, 11, TriggerSideExit, FrontendTrace, CompileOutcomeEmpty, CompileReasonNoPlan)

	value, ok := c.Metric("vm_jit_compiles_total",
		Label{Key: "func", Value: "2"}, Label{Key: "ip", Value: "11"},
		Label{Key: "trigger", Value: "side-exit"}, Label{Key: "frontend", Value: "trace"},
		Label{Key: "outcome", Value: "empty"}, Label{Key: "reason", Value: "no-plan"})
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}

func TestCollector_RecordEmit(t *testing.T) {
	c := NewCollector()
	c.RecordEmit(2, 11, EntryStart, FrontendTrace, 64)
	c.RecordEmit(2, 11, EntryStart, FrontendTrace, 64)
	labels := []Label{
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
	s := NewCollector()
	entry := s.RegisterEntry(2, 11, EntryCall, FrontendStatic)
	exit := s.RegisterExit(2, 11, EntryCall, FrontendStatic, ExitColdBranch, OpcodeNone)
	yield := s.RegisterYield(2, 11, EntryCall, FrontendStatic)

	allocs := testing.AllocsPerRun(100, func() {
		entry.Inc()
		exit.Inc()
		yield.Inc()
	})
	require.Zero(t, allocs)
}

func TestCollector_RegisterEntry(t *testing.T) {
	local := NewCollector()
	entry := local.RegisterEntry(3, 17, EntryLoop, FrontendTrace)
	p := New()

	entry.Inc()
	p.Flush(local)
	entry.Inc()
	p.Flush(local)
	p.Flush(local)

	value, ok := p.Metric(
		"vm_jit_native_entries_total",
		Label{Key: "func", Value: "3"},
		Label{Key: "ip", Value: "17"},
		Label{Key: "kind", Value: "loop"},
		Label{Key: "frontend", Value: "trace"},
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
	s := NewCollector()
	s.RegisterExit(2, 11, EntryStart, FrontendTrace, ExitTerminalOp, -2).Inc()
	s.RegisterExit(2, 11, EntryStart, FrontendTrace, ExitTerminalOp, 256).Inc()

	value, ok := s.Metric(
		"vm_jit_native_exits_total",
		Label{Key: "func", Value: "2"},
		Label{Key: "ip", Value: "11"},
		Label{Key: "kind", Value: "start"},
		Label{Key: "frontend", Value: "trace"},
		Label{Key: "reason", Value: "terminal-op"},
		Label{Key: "opcode", Value: "none"},
	)
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}

func TestCollector_RegisterYield(t *testing.T) {
	local := NewCollector()
	yield := local.RegisterYield(3, 17, EntryLoop, FrontendTrace)
	p := New()

	yield.Inc()
	p.Flush(local)
	yield.Inc()
	p.Flush(local)
	p.Flush(local)

	value, ok := p.Metric("vm_jit_native_yields_total",
		Label{Key: "func", Value: "3"}, Label{Key: "ip", Value: "17"},
		Label{Key: "kind", Value: "loop"}, Label{Key: "frontend", Value: "trace"})
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}
