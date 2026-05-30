package main

import (
	"os"

	"github.com/siyul-park/minivm/cli"

	// Blank imports register the active JIT backends.
	_ "github.com/siyul-park/minivm/jit/amd64"
	_ "github.com/siyul-park/minivm/jit/arm64"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
