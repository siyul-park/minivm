package pass

type Pass[T any] interface {
	Run(*Manager) (T, error)
}
