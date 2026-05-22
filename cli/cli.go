// Package cli builds the minivm command tree.
//
// The thin cmd/minivm entrypoint calls Root().Execute(). Embedders may
// call Root() directly to add subcommands, or pull NewRunCommand into
// their own cobra tree.
package cli

import (
	"os"

	"github.com/siyul-park/minivm/cli/repl"
	"github.com/spf13/cobra"
)

// Option configures the Root command tree.
type Option func(*options)

type options struct {
	fs WriteFS
}

// WithFS overrides the filesystem used by `run` and the REPL's
// .load/.save commands. Defaults to OS().
func WithFS(fs WriteFS) Option {
	return func(o *options) { o.fs = fs }
}

// Root returns the configured `minivm` cobra command. With no arguments,
// running it starts the interactive REPL; subcommands (e.g., `run`)
// provide non-interactive entry points.
func Root(opts ...Option) *cobra.Command {
	o := options{fs: OS()}
	for _, opt := range opts {
		opt(&o)
	}

	cmd := &cobra.Command{
		Use:          "minivm",
		Short:        "MiniVM — interactive assembly REPL",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return repl.New(os.Stdin, os.Stdout, repl.WithFS(o.fs)).Run(cmd.Context())
		},
	}
	cmd.AddCommand(NewRunCommand(o.fs))
	return cmd
}
