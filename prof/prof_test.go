package prof_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

func TestCollector(t *testing.T) {
	t.Run("records samples", func(t *testing.T) {
		s := prof.NewCollector()
		s.Add(0, 0, byte(instr.I32_CONST))
		s.Add(0, 5, byte(instr.DROP))
		s.AddMetric("vm_jit_attempts_total", 1)
		s.AddMetric("vm_jit_emits_total", 2)
		s.AddMetric("vm_jit_bytes_total", 16)

		require.Equal(t, uint64(2), s.Total())
		require.Equal(t, uint64(2), s.Samples(0))
		require.Equal(t, []int{0, 5}, s.IPs(0))
		require.Equal(t, uint64(1), s.IP(0, 0))
		require.Equal(t, uint64(1), s.IP(0, 5))
		require.Equal(t, uint64(1), s.Opcode(byte(instr.I32_CONST)))
	})

	t.Run("accumulates metrics", func(t *testing.T) {
		s := prof.NewCollector()
		s.AddMetric("vm_jit_attempts_total", 1)
		s.AddMetric("vm_jit_emits_total", 2)
		s.AddMetric("vm_jit_emits_total", 3)
		require.Equal(t, float64(1), s.Value("vm_jit_attempts_total"))
		require.Equal(t, float64(5), s.Value("vm_jit_emits_total"))
	})

	t.Run("ignores negative indices", func(t *testing.T) {
		s := prof.NewCollector()
		s.Add(-1, 0, 0)
		s.Add(0, -1, 0)
		require.Equal(t, uint64(0), s.Total())
	})

	t.Run("exports metrics", func(t *testing.T) {
		s := prof.NewCollector()
		s.Add(0, 0, byte(instr.I32_CONST))
		s.Add(0, 5, byte(instr.DROP))
		s.AddMetric("vm_jit_attempts_total", 1)
		s.AddMetric("vm_jit_emits_total", 2)
		s.AddMetric("vm_jit_bytes_total", 16)

		require.Equal(t, float64(2), s.Value("vm_samples_total"))
		require.Equal(t, float64(2), s.Value("vm_func_samples_total", prof.Label{Key: "func", Value: "0"}))
		require.Equal(t, float64(1), s.Value("vm_func_ip_samples_total", prof.Label{Key: "func", Value: "0"}, prof.Label{Key: "ip", Value: "5"}))
		require.Equal(t, float64(1), s.Value("vm_opcode_samples_total", prof.Label{Key: "opcode", Value: "i32.const"}))
	})

	t.Run("exports detailed JIT lifecycle rows", func(t *testing.T) {
		s := prof.NewCollector()
		s.AddMetric("vm_jit_emits_total", 3)
		s.RecordCapture(2, 11, prof.CaptureOutcomePartial, prof.CaptureReasonOpLimit)
		s.RecordCompile(2, 11, prof.TriggerHot, prof.FrontendTrace, prof.CompileOutcomeEmitted, prof.CompileReasonNone)
		s.RecordEmit(2, 11, prof.EntryStart, prof.FrontendTrace, 64)
		s.RegisterEntry(2, 11, prof.EntryStart, prof.FrontendTrace).Inc()
		s.RegisterYield(2, 11, prof.EntryStart, prof.FrontendTrace).Inc()
		s.RegisterExit(2, 11, prof.EntryStart, prof.FrontendTrace, prof.ExitGuardKind, int(instr.I32_ADD)).Inc()
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
				Value: 1,
			},
			{
				Name: "vm_jit_native_yields_total",
				Labels: []prof.Label{
					{Key: "func", Value: "2"},
					{Key: "ip", Value: "11"},
					{Key: "kind", Value: "start"},
					{Key: "frontend", Value: "trace"},
				},
				Value: 1,
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
				Value: 1,
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
	})

	t.Run("registered counters allocate zero", func(t *testing.T) {
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
	})
}

func TestNew(t *testing.T) {
	s := prof.NewCollector()
	s.Add(0, 0, byte(instr.I32_CONST))
	s.Add(0, 5, byte(instr.DROP))
	s.AddMetric("vm_jit_attempts_total", 1)
	s.AddMetric("vm_jit_emits_total", 2)
	s.AddMetric("vm_jit_bytes_total", 16)

	p := prof.New()
	p.Flush(s)

	v, ok := p.Metric("vm_samples_total")
	require.True(t, ok)
	require.Equal(t, float64(2), v)
	v, ok = p.Metric("vm_jit_attempts_total")
	require.True(t, ok)
	require.Equal(t, float64(1), v)
}

func TestProfiler_Flush(t *testing.T) {
	t.Run("folds samples and resets the member", func(t *testing.T) {
		local := prof.NewCollector()
		local.Add(0, 0, byte(instr.I32_CONST))
		local.Add(0, 5, byte(instr.DROP))
		local.AddMetric("vm_jit_attempts_total", 1)
		local.AddMetric("vm_jit_emits_total", 2)
		local.AddMetric("vm_jit_bytes_total", 16)

		p := prof.New()
		p.Flush(local)

		require.Equal(t, uint64(0), local.Total())
		v, ok := p.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(2), v)
	})

	t.Run("does not double count across flushes", func(t *testing.T) {
		local := prof.NewCollector()
		local.Add(0, 0, byte(instr.I32_CONST))
		local.Add(0, 5, byte(instr.DROP))
		local.AddMetric("vm_jit_attempts_total", 1)
		local.AddMetric("vm_jit_emits_total", 2)
		local.AddMetric("vm_jit_bytes_total", 16)

		p := prof.New()
		p.Flush(local)
		p.Flush(local) // local was reset, so this adds nothing

		v, ok := p.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(2), v)
	})

	t.Run("merges growing sparse instruction ranges", func(t *testing.T) {
		local := prof.NewCollector()
		p := prof.New()

		local.Add(7, 10_000, byte(instr.NOP))
		p.Flush(local)
		local.Add(7, 20_000, byte(instr.NOP))
		p.Flush(local)

		v, ok := p.Metric(
			"vm_func_ip_samples_total",
			prof.Label{Key: "func", Value: "7"},
			prof.Label{Key: "ip", Value: "10000"},
		)
		require.True(t, ok)
		require.Equal(t, float64(1), v)
		v, ok = p.Metric(
			"vm_func_ip_samples_total",
			prof.Label{Key: "func", Value: "7"},
			prof.Label{Key: "ip", Value: "20000"},
		)
		require.True(t, ok)
		require.Equal(t, float64(1), v)
	})

	t.Run("retains local capacity across repeated flushes", func(t *testing.T) {
		local := prof.NewCollector()
		p := prof.New()

		// Grow the local collector once so its backing arrays reach a stable
		// size, then flush repeatedly at that size. A flush that reset local
		// by nil-ing its slices would reallocate on every one of these calls.
		local.Add(3, 1_000, byte(instr.NOP))
		p.Flush(local)

		allocs := testing.AllocsPerRun(100, func() {
			local.Add(3, 1_000, byte(instr.NOP))
			p.Flush(local)
		})
		require.Zero(t, allocs)
	})

	t.Run("merges detailed rows and retains registered counters", func(t *testing.T) {
		local := prof.NewCollector()
		entry := local.RegisterEntry(3, 17, prof.EntryLoop, prof.FrontendTrace)
		p := prof.New()

		entry.Inc()
		p.Flush(local)
		entry.Inc()
		p.Flush(local)
		p.Flush(local)

		v, ok := p.Metric(
			"vm_jit_native_entries_total",
			prof.Label{Key: "func", Value: "3"},
			prof.Label{Key: "ip", Value: "17"},
			prof.Label{Key: "kind", Value: "loop"},
			prof.Label{Key: "frontend", Value: "trace"},
		)
		require.True(t, ok)
		require.Equal(t, float64(2), v)

		allocs := testing.AllocsPerRun(100, func() {
			entry.Inc()
			p.Flush(local)
		})
		require.Zero(t, allocs)
	})
}

func TestProfiler_Metrics(t *testing.T) {
	s := prof.NewCollector()
	s.Add(0, 0, byte(instr.I32_CONST))
	s.Add(0, 5, byte(instr.DROP))
	s.AddMetric("vm_jit_attempts_total", 1)
	s.AddMetric("vm_jit_emits_total", 2)
	s.AddMetric("vm_jit_bytes_total", 16)

	p := prof.New()
	p.Flush(s)

	metrics := p.Metrics()
	require.Contains(t, metrics, prof.Metric{
		Name:   "vm_func_samples_total",
		Labels: []prof.Label{{Key: "func", Value: "0"}},
		Value:  2,
	})
}

func TestProfiler_Metric(t *testing.T) {
	s := prof.NewCollector()
	s.Add(0, 0, byte(instr.I32_CONST))
	s.Add(0, 5, byte(instr.DROP))
	s.AddMetric("vm_jit_attempts_total", 1)
	s.AddMetric("vm_jit_emits_total", 2)
	s.AddMetric("vm_jit_bytes_total", 16)

	p := prof.New()
	p.Flush(s)

	v, ok := p.Metric("vm_samples_total")
	require.True(t, ok)
	require.Equal(t, float64(2), v)

	_, ok = p.Metric("vm_missing")
	require.False(t, ok)
}
