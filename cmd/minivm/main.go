package main

import (
	"os"

	"github.com/siyul-park/minivm/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
