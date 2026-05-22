package cli

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFS extends io/fs.FS with file creation. The standard library has
// no write counterpart to fs.FS, so this is the minimal extension used
// by CLI commands that need to write files (e.g., the REPL's .save).
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
