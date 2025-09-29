package pass

type Pass[T any] interface {
	Run(*Manager) (T, error)
}

type pass[T any] struct {
	run func(*Manager) (T, error)
}

func NewPass[T any](run func(*Manager) (T, error)) Pass[T] {
	return &pass[T]{run: run}
}

func (p *pass[T]) Run(m *Manager) (T, error) {
	return p.run(m)
}
