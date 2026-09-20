package prof_test

import (
	"sync"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	profiler := prof.New()
	require.NotNil(t, profiler)
	require.Equal(t, []prof.Metric{{Name: "vm_samples_total", Value: 0}}, profiler.Metrics())
}

func TestProfiler_Flush(t *testing.T) {
	t.Run("folds and resets collector", func(t *testing.T) {
		local := prof.NewCollector()
		local.Add(0, 0, byte(instr.NOP))
		profiler := prof.New()
		profiler.Flush(local)

		require.Zero(t, local.Total())
		value, ok := profiler.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(1), value)
		profiler.Flush(local)
		value, ok = profiler.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(1), value)
	})

	t.Run("custom metrics survive collector reuse", func(t *testing.T) {
		local := prof.NewCollector()
		profiler := prof.New()
		label := prof.Label{Key: "mode", Value: "profile"}
		local.AddMetric("custom", 2, label)
		local.AddMetric("first", 7)
		profiler.Flush(local)
		require.Equal(t, []prof.Metric{{Name: "vm_samples_total"}}, local.Metrics())
		snapshot := profiler.Metrics()

		local.AddMetric("custom", 3, label)
		profiler.Flush(local)
		profiler.Flush(local)
		require.Equal(t, []prof.Metric{{Name: "vm_samples_total"}}, local.Metrics())
		require.Contains(t, snapshot, prof.Metric{Name: "custom", Value: 2, Labels: []prof.Label{label}})
		value, ok := profiler.Metric("custom", label)
		require.True(t, ok)
		require.Equal(t, float64(5), value)
		value, ok = profiler.Metric("first")
		require.True(t, ok)
		require.Equal(t, float64(7), value)
	})

	t.Run("merges sparse ranges", func(t *testing.T) {
		local := prof.NewCollector()
		profiler := prof.New()
		local.Add(7, 10_000, byte(instr.NOP))
		profiler.Flush(local)
		local.Add(7, 20_000, byte(instr.NOP))
		profiler.Flush(local)

		first, ok := profiler.Metric("vm_func_ip_samples_total",
			prof.Label{Key: "func", Value: "7"}, prof.Label{Key: "ip", Value: "10000"})
		require.True(t, ok)
		require.Equal(t, float64(1), first)
		second, ok := profiler.Metric("vm_func_ip_samples_total",
			prof.Label{Key: "func", Value: "7"}, prof.Label{Key: "ip", Value: "20000"})
		require.True(t, ok)
		require.Equal(t, float64(1), second)
	})

	t.Run("reuses collector storage", func(t *testing.T) {
		local := prof.NewCollector()
		profiler := prof.New()
		local.Add(3, 1_000, byte(instr.NOP))
		profiler.Flush(local)
		allocs := testing.AllocsPerRun(100, func() {
			local.Add(3, 1_000, byte(instr.NOP))
			profiler.Flush(local)
		})
		require.Zero(t, allocs)
	})

	t.Run("supports concurrent flush and read", func(t *testing.T) {
		profiler := prof.New()
		const workers = 8
		var group sync.WaitGroup
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				local := prof.NewCollector()
				local.Add(0, 0, byte(instr.NOP))
				profiler.Flush(local)
				_ = profiler.Metrics()
			}()
		}
		group.Wait()
		value, ok := profiler.Metric("vm_samples_total")
		require.True(t, ok)
		require.Equal(t, float64(workers), value)
	})
}

func TestProfiler_Metric(t *testing.T) {
	local := prof.NewCollector()
	local.AddMetric("custom", 3)
	profiler := prof.New()
	profiler.Flush(local)
	value, ok := profiler.Metric("custom")
	require.True(t, ok)
	require.Equal(t, float64(3), value)
	_, ok = profiler.Metric("missing")
	require.False(t, ok)
}

func TestProfiler_Metrics(t *testing.T) {
	local := prof.NewCollector()
	local.Add(0, 0, byte(instr.I32_CONST))
	labels := []prof.Label{{Key: "mode", Value: "profile"}}
	local.AddMetric("custom", 2, labels...)
	profiler := prof.New()
	profiler.Flush(local)
	metrics := profiler.Metrics()
	require.Contains(t, metrics, prof.Metric{Name: "vm_samples_total", Value: 1})
	for i := range metrics {
		if metrics[i].Name == "custom" {
			metrics[i].Labels[0].Value = "snapshot"
		}
	}
	value, ok := profiler.Metric("custom", labels...)
	require.True(t, ok)
	require.Equal(t, float64(2), value)
}
