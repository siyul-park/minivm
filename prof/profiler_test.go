package prof

import (
	"sync"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	profiler := New()
	require.NotNil(t, profiler)
	require.Equal(t, []Metric{{Name: "vm_samples_total", Value: 0}}, profiler.Metrics())
}

func TestProfiler_Flush(t *testing.T) {
	t.Run("folds and resets collector", func(t *testing.T) {
		local := NewCollector()
		local.Add(0, 0, byte(instr.NOP))
		profiler := New()
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

	t.Run("merges sparse ranges", func(t *testing.T) {
		local := NewCollector()
		profiler := New()
		local.Add(7, 10_000, byte(instr.NOP))
		profiler.Flush(local)
		local.Add(7, 20_000, byte(instr.NOP))
		profiler.Flush(local)

		first, ok := profiler.Metric("vm_func_ip_samples_total",
			Label{Key: "func", Value: "7"}, Label{Key: "ip", Value: "10000"})
		require.True(t, ok)
		require.Equal(t, float64(1), first)
		second, ok := profiler.Metric("vm_func_ip_samples_total",
			Label{Key: "func", Value: "7"}, Label{Key: "ip", Value: "20000"})
		require.True(t, ok)
		require.Equal(t, float64(1), second)
	})

	t.Run("reuses collector storage", func(t *testing.T) {
		local := NewCollector()
		profiler := New()
		local.Add(3, 1_000, byte(instr.NOP))
		profiler.Flush(local)
		allocs := testing.AllocsPerRun(100, func() {
			local.Add(3, 1_000, byte(instr.NOP))
			profiler.Flush(local)
		})
		require.Zero(t, allocs)
	})

	t.Run("supports concurrent flush and read", func(t *testing.T) {
		profiler := New()
		const workers = 8
		var group sync.WaitGroup
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				local := NewCollector()
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

func TestProfiler_Metrics(t *testing.T) {
	local := NewCollector()
	local.Add(0, 0, byte(instr.I32_CONST))
	profiler := New()
	profiler.Flush(local)
	require.Contains(t, profiler.Metrics(), Metric{Name: "vm_samples_total", Value: 1})
}

func TestProfiler_Metric(t *testing.T) {
	local := NewCollector()
	local.AddMetric("custom", 3)
	profiler := New()
	profiler.Flush(local)
	value, ok := profiler.Metric("custom")
	require.True(t, ok)
	require.Equal(t, float64(3), value)
	_, ok = profiler.Metric("missing")
	require.False(t, ok)
}
