package pass

import (
	"errors"
	"fmt"
	"reflect"
)

// Manager lazily runs analyses and caches their results per IR unit, keyed by
// result type and unit identity. It mirrors LLVM's AnalysisManager.
type Manager struct {
	analyses map[reflect.Type]func(*Manager, any) (any, error)
	cache    map[cacheKey]any
}

type cacheKey struct {
	result reflect.Type
	unit   any
}

var ErrUnregisteredAnalysis = errors.New("unregistered analysis")

func NewManager() *Manager {
	return &Manager{
		analyses: make(map[reflect.Type]func(*Manager, any) (any, error)),
		cache:    make(map[cacheKey]any),
	}
}

// Register adds an analysis, keyed by its result type R.
func Register[U, R any](m *Manager, a Analysis[U, R]) {
	m.analyses[reflect.TypeFor[R]()] = func(m *Manager, unit any) (any, error) {
		return a.Run(m, unit.(U))
	}
}

// GetResult returns the result of type R for unit, computing and caching it on a miss.
func GetResult[R any](m *Manager, unit any) (R, error) {
	key := cacheKey{result: reflect.TypeFor[R](), unit: unit}
	if v, ok := m.cache[key]; ok {
		return v.(R), nil
	}

	run, ok := m.analyses[key.result]
	if !ok {
		var zero R
		return zero, fmt.Errorf("%w: %s", ErrUnregisteredAnalysis, key.result)
	}

	res, err := run(m, unit)
	if err != nil {
		var zero R
		return zero, err
	}
	m.cache[key] = res
	return res.(R), nil
}

// Invalidate drops cached results unless the transform preserved everything.
func (m *Manager) Invalidate(p Preserved) {
	if !p.all {
		clear(m.cache)
	}
}
