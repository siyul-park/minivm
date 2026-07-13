package prof_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

func TestCollector_Metrics(t *testing.T) {
	s := prof.NewCollector()
	s.AddMetric("vm_jit_emits_total", 3)
	s.RecordCapture(2, 11, prof.CaptureOutcomePartial, prof.CaptureReasonOpLimit)
	s.RecordCompile(2, 11, prof.TriggerHot, prof.FrontendTrace, prof.CompileOutcomeEmitted, prof.CompileReasonNone)
	s.RecordEmit(2, 11, prof.EntryStart, prof.FrontendTrace, 64)
	entry := s.RegisterEntry(2, 11, prof.EntryStart, prof.FrontendTrace)
	entry.Inc()
	entry.Inc()
	yield := s.RegisterYield(2, 11, prof.EntryStart, prof.FrontendTrace)
	yield.Inc()
	yield.Inc()
	yield.Inc()
	exit := s.RegisterExit(2, 11, prof.EntryStart, prof.FrontendTrace, prof.ExitGuardKind, int(instr.I32_ADD))
	exit.Inc()
	exit.Inc()
	s.RegisterExit(2, 11, prof.EntryStart, prof.FrontendTrace, prof.ExitTerminalOp, prof.OpcodeNone).Inc()

	want := []prof.Metric{
		{Name: "vm_samples_total", Value: 0},
		{Name: "vm_jit_emits_total", Value: 3},
		{
			Name: "vm_jit_trace_captures_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "outcome", Value: "partial"},
				{Key: "reason", Value: "op-limit"},
			},
			Value: 1,
		},
		{
			Name: "vm_jit_compiles_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "trigger", Value: "hot"},
				{Key: "frontend", Value: "trace"},
				{Key: "outcome", Value: "emitted"},
				{Key: "reason", Value: "none"},
			},
			Value: 1,
		},
		{
			Name: "vm_jit_entry_emits_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "kind", Value: "start"},
				{Key: "frontend", Value: "trace"},
			},
			Value: 1,
		},
		{
			Name: "vm_jit_entry_bytes_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "kind", Value: "start"},
				{Key: "frontend", Value: "trace"},
			},
			Value: 64,
		},
		{
			Name: "vm_jit_native_entries_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "kind", Value: "start"},
				{Key: "frontend", Value: "trace"},
			},
			Value: 2,
		},
		{
			Name: "vm_jit_native_yields_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "kind", Value: "start"},
				{Key: "frontend", Value: "trace"},
			},
			Value: 3,
		},
		{
			Name: "vm_jit_native_exits_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "kind", Value: "start"},
				{Key: "frontend", Value: "trace"},
				{Key: "reason", Value: "guard-kind"},
				{Key: "opcode", Value: "i32.add"},
			},
			Value: 2,
		},
		{
			Name: "vm_jit_native_exits_total",
			Labels: []prof.Label{
				{Key: "func", Value: "2"},
				{Key: "ip", Value: "11"},
				{Key: "kind", Value: "start"},
				{Key: "frontend", Value: "trace"},
				{Key: "reason", Value: "terminal-op"},
				{Key: "opcode", Value: "none"},
			},
			Value: 1,
		},
	}
	require.Equal(t, want, s.Metrics())
	require.Equal(t, want, s.Metrics())
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
