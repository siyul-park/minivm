package fixtures

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/siyul-park/minivm/benchmarks/registry"
)

//go:embed *
var files embed.FS

func init() {
	registry.SetSources(load())
}

func load() map[string]registry.Source {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		panic(fmt.Sprintf("read fixtures: %v", err))
	}

	sources := make(map[string]registry.Source)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name, ext, ok := fixture(entry.Name())
		if !ok {
			continue
		}
		source := sources[name]
		value := read(entry.Name())
		switch ext {
		case "mvm":
			source.MVM = value
		case "go":
			source.Go = value
		case "tengo":
			source.Tengo = value
		case "lua":
			source.Lua = value
		case "js":
			source.JS = value
		case "py":
			source.Python = value
		}
		sources[name] = source
	}
	for name, source := range sources {
		source.Support = read("wasm.go")
		sources[name] = source
	}
	return sources
}

func fixture(name string) (string, string, bool) {
	ext := path.Ext(name)
	switch ext {
	case ".mvm", ".go", ".tengo", ".lua", ".js", ".py":
	default:
		return "", "", false
	}
	return strings.TrimSuffix(name, ext), ext[1:], true
}

func read(name string) string {
	data, err := files.ReadFile(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			panic(fmt.Sprintf("read fixture %s: %v", name, err))
		}
	}
	return strings.TrimSpace(string(data))
}
