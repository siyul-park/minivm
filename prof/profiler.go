package prof

import "sync"

// Profiler aggregates collector snapshots from interpreters.
type Profiler struct {
	data Collector
	mu   sync.Mutex
}

func New() *Profiler {
	return &Profiler{}
}

func (p *Profiler) Flush(local *Collector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data.merge(local)
	local.reset()
}

func (p *Profiler) Metrics() []Metric {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.data.Metrics()
}

func (p *Profiler) Metric(name string, labels ...Label) (float64, bool) {
	for _, m := range p.Metrics() {
		if m.Name == name && sameLabels(m.Labels, labels) {
			return m.Value, true
		}
	}
	return 0, false
}
