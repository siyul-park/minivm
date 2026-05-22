// Package fsx extends io/fs with a minimal write surface.
//
// The standard library exposes a stable read-only filesystem abstraction
// (io/fs.FS), but no write counterpart. fsx adds the smallest interface
// needed for tools that both read and write files (e.g., load/save in a
// REPL): WriteFS embeds fs.FS and adds Create.
package fsx

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFS extends io/fs.FS with file creation. Implementations may treat
// names as filesystem paths or virtual keys; callers should pass cleaned
// relative paths the same way they would to fs.FS.
type WriteFS interface {
	fs.FS
	Create(name string) (io.WriteCloser, error)
}

type osFS struct{}

var _ WriteFS = osFS{}

// OS returns a WriteFS backed by the host filesystem. Names are
// interpreted as paths on the local filesystem.
func OS() WriteFS { return osFS{} }

func (osFS) Open(name string) (fs.File, error) {
	return os.Open(filepath.FromSlash(name))
}

func (osFS) Create(name string) (io.WriteCloser, error) {
	return os.Create(filepath.FromSlash(name))
}
