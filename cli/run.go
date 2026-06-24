package cli

import (
	"fmt"
	"io/fs"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/spf13/cobra"
)

// NewRunCommand returns the `minivm run <file>` subcommand. It loads
// <file> from fsys, parses it as a Program.String() dump, runs it to
// completion, and prints the final operand stack.
//
// fsys is the standard io/fs.FS so callers may pass os.DirFS, embed.FS,
// or fstest.MapFS without adapter wrappers.
func NewRunCommand(fsys fs.FS) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "run <file>",
		Short:        "Run a MiniVM assembly file",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		path := args[0]

		file, err := fsys.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer file.Close()

		prog, err := program.Parse(file)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		if err := program.Verify(prog); err != nil {
			return fmt.Errorf("verify %s: %w", path, err)
		}

		vm, err := interp.New(prog)
		if err != nil {
			return fmt.Errorf("run %s: %w", path, err)
		}
		defer vm.Close()

		if err := vm.Run(cmd.Context()); err != nil {
			return fmt.Errorf("run %s: %w", path, err)
		}

		printStack(cmd.OutOrStdout(), vm)
		return nil
	}
	return cmd
}
