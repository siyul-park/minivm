package prof

import "sync"

// Profiler aggregates collector snapshots from interpreters.
type Profiler struct {
	data Collector
	mu   sync.Mutex
}

// New returns an empty profiler.
func New() *Profiler {
	return &Profiler{}
}

// Flush merges local into the profiler and resets local.
func (p *Profiler) Flush(local *Collector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data.merge(local)
	local.reset()
}

// Metric returns the aggregated metric matching name and labels and whether it exists.
func (p *Profiler) Metric(name string, labels ...Label) (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.data.Metric(name, labels...)
}

// Metrics returns all aggregated metrics.
func (p *Profiler) Metrics() []Metric {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.data.Metrics()
}
