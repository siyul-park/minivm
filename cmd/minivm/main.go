package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/siyul-park/minivm/cmd/repl"
)

func main() {
	root := &cobra.Command{
		Use:          "minivm",
		Short:        "MiniVM — interactive assembly REPL",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return repl.New(os.Stdin, os.Stdout).Run(cmd.Context())
		},
	}

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
