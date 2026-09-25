package pass

import (
	"errors"
	"fmt"
	"reflect"
)

// Manager owns analysis registration and cache state.
type Manager struct {
	analyses map[reflect.Type]func(*Manager, any) (any, error)
	cache    map[cacheKey]any
}

type cacheKey struct {
	result reflect.Type
	unit   any
}

// ErrUnregisteredAnalysis reports a result type with no registered analysis.
var ErrUnregisteredAnalysis = errors.New("unregistered analysis")

// NewManager returns a pass manager.
func NewManager() *Manager {
	return &Manager{
		analyses: make(map[reflect.Type]func(*Manager, any) (any, error)),
		cache:    make(map[cacheKey]any),
	}
}

// Register adds an analysis, keyed by its result type R.
func Register[U, R any](m *Manager, a Analysis[U, R]) {
	m.register(reflect.TypeFor[R](), func(m *Manager, unit any) (any, error) {
		return a.Run(m, unit.(U))
	})
}

// GetResult returns the result of type R for unit, computing and caching it on a miss.
func GetResult[R any](m *Manager, unit any) (R, error) {
	var zero R
	res, err := m.result(reflect.TypeFor[R](), unit)
	if err != nil {
		return zero, err
	}
	if res == nil {
		return zero, nil
	}
	return res.(R), nil
}

func (m *Manager) register(result reflect.Type, run func(*Manager, any) (any, error)) {
	m.analyses[result] = run
}

func (m *Manager) result(result reflect.Type, unit any) (any, error) {
	key := cacheKey{result: result, unit: unit}
	res, ok := m.cache[key]
	if !ok {
		run, ok := m.analyses[key.result]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnregisteredAnalysis, key.result)
		}
		var err error
		res, err = run(m, unit)
		if err != nil {
			return nil, err
		}
		m.cache[key] = res
	}
	return res, nil
}

// Invalidate drops cached analyses when they may be stale.
func (m *Manager) Invalidate(preserved bool) {
	if !preserved {
		clear(m.cache)
	}
}
