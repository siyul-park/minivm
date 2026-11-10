package registry

import (
	"cmp"
	"slices"
	"sync"

	"github.com/siyul-park/minivm/types"
)

// Spec is the complete executable contract for one benchmark.
type Spec struct {
	Name   string
	Result func() types.Value
	Source Source
	Native Native
	Wazero *Wazero
	Host   *Host
}

// Native contains the direct Go implementation of a benchmark.
type Native struct {
	I32 func() int32
	I64 func() int64
}

// Host is a Go function a guest workload calls on every iteration.
// The script runtimes bind it as the global function Name; minivm binds it to
// global 0, which the fixture's .mvm declares as its type. Native and Yaegi
// run the fixture's Go source, which calls it directly.
type Host struct {
	Name string
	I32  func(a, b int32) int32
}

// Wazero describes a benchmark exported by a WebAssembly module.
type Wazero struct {
	Module func() []byte
	Export string
	Args   []uint64
}

// Source contains the executable fixture source for each runtime.
type Source struct {
	MVM     string
	Go      string
	Support string
	Tengo   string
	Lua     string
	JS      string
	Python  string
}

var (
	mu       sync.RWMutex
	specs    = make(map[string]Spec)
	sourceMu sync.RWMutex
	sources  = map[string]Source{}
)

// Register adds one benchmark to the registry.
func Register(spec Spec) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := specs[spec.Name]; ok {
		panic("duplicate benchmark: " + spec.Name)
	}
	specs[spec.Name] = spec
}

// SetSources supplies the runtime source fixtures.
func SetSources(values map[string]Source) {
	copy := make(map[string]Source, len(values))
	for name, value := range values {
		copy[name] = value
	}
	sourceMu.Lock()
	sources = copy
	sourceMu.Unlock()
}

// Get returns a benchmark by name.
func Get(name string) (Spec, bool) {
	mu.RLock()
	spec, ok := specs[name]
	mu.RUnlock()
	if !ok {
		return Spec{}, false
	}
	spec.Source = source(name, spec.Source)
	return spec, true
}

// All returns benchmarks in name order.
func All() []Spec {
	mu.RLock()
	out := make([]Spec, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec)
	}
	mu.RUnlock()

	for index := range out {
		out[index].Source = source(out[index].Name, out[index].Source)
	}
	slices.SortFunc(out, func(a, b Spec) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

func source(name string, value Source) Source {
	if value.MVM != "" || value.Go != "" || value.Support != "" || value.Tengo != "" || value.Lua != "" || value.JS != "" || value.Python != "" {
		return value
	}
	sourceMu.RLock()
	value = sources[name]
	sourceMu.RUnlock()
	return value
}
