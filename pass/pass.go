package pass

// Analysis lazily computes a cached result of type R for an IR unit of type U.
type Analysis[U, R any] interface {
	Run(*Manager, U) (R, error)
}

// Pass transforms an IR unit of type U in place, reporting which analyses survive.
type Pass[U any] interface {
	Run(*Manager, U) (Preserved, error)
}

// Preserved records which analysis results remain valid after a transform, so
// the AnalysisManager can drop the rest. It mirrors LLVM's PreservedAnalyses.
type Preserved struct {
	all bool
}

// PreserveAll keeps every cached analysis result.
func PreserveAll() Preserved {
	return Preserved{all: true}
}

// PreserveNone invalidates every cached analysis result. Transforms that mutate
// code return this; finer-grained preservation is a future refinement.
func PreserveNone() Preserved {
	return Preserved{}
}
