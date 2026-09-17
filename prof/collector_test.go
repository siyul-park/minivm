package prof_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	collector := prof.NewCollector()
	require.NotNil(t, collector)
	require.Zero(t, collector.Total())
}

func TestCollector_Value(t *testing.T) {
	collector := prof.NewCollector()
	collector.AddMetric("custom", 2)
	require.Equal(t, float64(2), collector.Value("custom"))
	require.Zero(t, collector.Value("missing"))
}

func TestCollector_Metric(t *testing.T) {
	collector := prof.NewCollector()
	collector.AddMetric("custom", 2)
	value, ok := collector.Metric("custom")
	require.True(t, ok)
	require.Equal(t, float64(2), value)
	_, ok = collector.Metric("missing")
	require.False(t, ok)

	labels := []prof.Label{{Key: "mode", Value: "jit"}, {Key: "phase", Value: "run"}}
	collector.AddMetric("labeled", 3, labels...)
	value, ok = collector.Metric("labeled", labels...)
	require.True(t, ok)
	require.Equal(t, float64(3), value)
	_, ok = collector.Metric("labeled", labels[1], labels[0])
	require.False(t, ok)
	_, ok = collector.Metric("labeled", labels[:1]...)
	require.False(t, ok)
	_, ok = collector.Metric("labeled", prof.Label{Key: "mode", Value: "threaded"}, labels[1])
	require.False(t, ok)
	value, ok = collector.Metric("custom", []prof.Label{}...)
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}

func TestCollector_Metrics(t *testing.T) {
	t.Run("execution metrics", func(t *testing.T) {
		collector := prof.NewCollector()
		collector.Add(0, 5, byte(instr.I32_CONST))
		collector.AddMetric("custom", 2)

		metrics := collector.Metrics()
		require.Contains(t, metrics, prof.Metric{Name: "vm_samples_total", Value: 1})
		require.Contains(t, metrics, prof.Metric{Name: "custom", Value: 2})
	})

	t.Run("custom labels are owned by each snapshot", func(t *testing.T) {
		collector := prof.NewCollector()
		labels := []prof.Label{{Key: "mode", Value: "jit"}}
		collector.AddMetric("custom", 2, labels...)
		labels[0].Value = "caller"
		first := collector.Metrics()
		second := collector.Metrics()
		for i := range first {
			if first[i].Name == "custom" {
				first[i].Labels[0].Value = "snapshot"
				first[i].Value = 99
			}
		}
		require.Contains(t, second, prof.Metric{Name: "custom", Value: 2, Labels: []prof.Label{{Key: "mode", Value: "jit"}}})
		require.Equal(t, second, collector.Metrics())
		collector.AddMetric("custom", 3, prof.Label{Key: "mode", Value: "jit"})
		require.Equal(t, float64(5), collector.Value("custom", prof.Label{Key: "mode", Value: "jit"}))
	})

	t.Run("jit metrics", func(t *testing.T) {
		c := prof.NewCollector()
		c.RecordCapture(1, 2, prof.CaptureOutcomePartial, prof.CaptureReasonOpLimit)
		c.RecordCompile(1, 2, prof.TriggerHot, prof.FrontendTrace, prof.CompileOutcomeEmitted, prof.CompileReasonNone)
		c.RegisterYield(1, 2, prof.EntryStart, prof.FrontendTrace).Inc()

		first := c.Metrics()
		require.Equal(t, first, c.Metrics())
		names := make([]string, len(first))
		for index, metric := range first {
			names[index] = metric.Name
		}
		require.Equal(t, []string{
			"vm_samples_total",
			"vm_jit_trace_captures_total",
			"vm_jit_compiles_total",
			"vm_jit_native_yields_total",
		}, names)
	})
}

func TestCollector_Add(t *testing.T) {
	collector := prof.NewCollector()
	collector.Add(0, 0, byte(instr.I32_CONST))
	collector.Add(0, 5, byte(instr.DROP))
	collector.Add(-1, 0, 0)
	collector.Add(0, -1, 0)

	require.Equal(t, uint64(2), collector.Total())
	require.Equal(t, uint64(2), collector.Samples(0))
}

func TestCollector_AddMetric(t *testing.T) {
	collector := prof.NewCollector()
	collector.AddMetric("custom", 2, prof.Label{Key: "mode", Value: "jit"})
	collector.AddMetric("custom", 3, prof.Label{Key: "mode", Value: "jit"})
	require.Equal(t, float64(5), collector.Value("custom", prof.Label{Key: "mode", Value: "jit"}))

	first := prof.Label{Key: "mode", Value: "jit"}
	second := prof.Label{Key: "phase", Value: "run"}
	collector.AddMetric("ordered", 2, first, second)
	collector.AddMetric("ordered", 3, first, second)
	collector.AddMetric("ordered", 7, second, first)
	collector.AddMetric("ordered", 11, first)
	collector.AddMetric("ordered", 13, prof.Label{Key: "mode", Value: "threaded"})
	require.Equal(t, float64(5), collector.Value("ordered", first, second))
	require.Equal(t, float64(7), collector.Value("ordered", second, first))
	require.Equal(t, float64(11), collector.Value("ordered", first))
	require.Equal(t, float64(13), collector.Value("ordered", prof.Label{Key: "mode", Value: "threaded"}))
	collector.AddMetric("unlabeled", 2)
	collector.AddMetric("unlabeled", 3, []prof.Label{}...)
	require.Equal(t, float64(5), collector.Value("unlabeled"))
}

func TestCollector_Total(t *testing.T) {
	collector := prof.NewCollector()
	collector.Add(0, 0, byte(instr.NOP))
	collector.Add(1, 0, byte(instr.NOP))
	require.Equal(t, uint64(2), collector.Total())
}

func TestCollector_Samples(t *testing.T) {
	collector := prof.NewCollector()
	collector.Add(3, 0, byte(instr.NOP))
	require.Equal(t, uint64(1), collector.Samples(3))
	require.Zero(t, collector.Samples(-1))
	require.Zero(t, collector.Samples(4))
}

func TestCollector_IPs(t *testing.T) {
	collector := prof.NewCollector()
	collector.Add(3, 10, byte(instr.NOP))
	collector.Add(3, 20, byte(instr.NOP))
	require.Equal(t, []int{10, 20}, collector.IPs(3))
	require.Nil(t, collector.IPs(-1))
}

func TestCollector_IP(t *testing.T) {
	collector := prof.NewCollector()
	collector.Add(3, 10, byte(instr.NOP))
	require.Equal(t, uint64(1), collector.IP(3, 10))
	require.Zero(t, collector.IP(3, 11))
	require.Zero(t, collector.IP(-1, 0))
}

func TestCollector_Opcode(t *testing.T) {
	collector := prof.NewCollector()
	collector.Add(0, 0, byte(instr.I32_CONST))
	require.Equal(t, uint64(1), collector.Opcode(byte(instr.I32_CONST)))
	require.Zero(t, collector.Opcode(byte(instr.DROP)))
}
