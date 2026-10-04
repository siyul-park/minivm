package registry_test

import (
	"testing"

	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	spec := registry.Spec{
		Name: "register-test",
		Result: func() types.Value {
			return types.I32(1)
		},
		Native: registry.Native{
			I32: func() int32 {
				return 1
			},
		},
	}
	registry.SetSources(map[string]registry.Source{
		spec.Name: {Go: "package fixtures"},
	})
	registry.Register(spec)

	got, ok := registry.Get(spec.Name)
	require.True(t, ok)
	require.Equal(t, spec.Name, got.Name)
	require.Equal(t, "package fixtures", got.Source.Go)
	require.Equal(t, int32(1), int32(got.Result().(types.I32)))
}

func TestSetSources(t *testing.T) {
	const name = "sources-test"
	registry.SetSources(map[string]registry.Source{
		name: {Python: "def run():\n    return 1"},
	})
	registry.Register(registry.Spec{Name: name})

	got, ok := registry.Get(name)
	require.True(t, ok)
	require.Equal(t, "def run():\n    return 1", got.Source.Python)
}

func TestGet(t *testing.T) {
	_, ok := registry.Get("missing-test")
	require.False(t, ok)
}

func TestAll(t *testing.T) {
	registry.Register(registry.Spec{Name: "all-z-test"})
	registry.Register(registry.Spec{Name: "all-a-test"})

	got := registry.All()
	first := -1
	last := -1
	for index, spec := range got {
		switch spec.Name {
		case "all-a-test":
			first = index
		case "all-z-test":
			last = index
		}
	}

	require.NotEqual(t, -1, first)
	require.NotEqual(t, -1, last)
	require.Less(t, first, last)
}
