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
