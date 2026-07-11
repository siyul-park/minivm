package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	check := flag.Bool("check", false, "report stale generated files without writing them")
	flag.Parse()
	if err := run(*check, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool, stdout io.Writer) error {
	outputs, err := generate()
	if err != nil {
		return fmt.Errorf("generate fusion: %w", err)
	}
	for _, output := range outputs {
		if check {
			actual, err := os.ReadFile(output.path)
			if err != nil {
				return fmt.Errorf("read %s: %w", output.path, err)
			}
			if !bytes.Equal(actual, output.data) {
				return fmt.Errorf("%s is stale", output.path)
			}
			continue
		}
		dir := filepath.Dir(output.path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.WriteFile(output.path, output.data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", output.path, err)
		}
		if _, err := fmt.Fprintln(stdout, output.path); err != nil {
			return fmt.Errorf("report %s: %w", output.path, err)
		}
	}
	return nil
}
