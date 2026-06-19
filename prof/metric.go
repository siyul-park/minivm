package prof

// Label is one name/value pair attached to a Metric. Labels are kept in a
// deterministic order by the collector that emits them so metric output is
// stable across runs.
type Label struct {
	Key   string
	Value string
}

// Metric is one named measurement exported by a Collector. It is a plain value
// that crosses the package boundary; consumers read it without locking.
type Metric struct {
	Name   string
	Labels []Label
	Value  float64
}

func sameLabels(a, b []Label) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
