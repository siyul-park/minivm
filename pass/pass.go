package pass

// Analysis lazily computes a cached result of type R for an IR unit of type U.
type Analysis[U, R any] interface {
	Run(*Manager, U) (R, error)
}

// Pass transforms an IR unit of type U in place, reporting whether all analyses survive.
type Pass[U any] interface {
	Run(*Manager, U) (bool, error)
}
