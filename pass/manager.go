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

// Register adds an analysis, keyed by its result type R.
func Register[U, R any](m *Manager, a Analysis[U, R]) {
	m.analyses[reflect.TypeFor[R]()] = func(m *Manager, unit any) (any, error) {
		return a.Run(m, unit.(U))
	}
}

// GetResult returns the result of type R for unit, computing and caching it on a miss.
func GetResult[R any](m *Manager, unit any) (R, error) {
	var zero R
	key := cacheKey{result: reflect.TypeFor[R](), unit: unit}
	res, ok := m.cache[key]
	if !ok {
		run, ok := m.analyses[key.result]
		if !ok {
			return zero, fmt.Errorf("%w: %s", ErrUnregisteredAnalysis, key.result)
		}
		var err error
		res, err = run(m, unit)
		if err != nil {
			return zero, err
		}
		m.cache[key] = res
	}
	if res == nil {
		return zero, nil
	}
	return res.(R), nil
}

func NewManager() *Manager {
	return &Manager{
		analyses: make(map[reflect.Type]func(*Manager, any) (any, error)),
		cache:    make(map[cacheKey]any),
	}
}

// Invalidate drops cached results unless the transform preserved everything.
func (m *Manager) Invalidate(p Preserved) {
	if !p.all {
		clear(m.cache)
	}
}
