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
		if err := output.sync(check, stdout); err != nil {
			return err
		}
	}
	return nil
}

func (o output) sync(check bool, stdout io.Writer) error {
	if check {
		actual, err := os.ReadFile(o.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", o.path, err)
		}
		if !bytes.Equal(actual, o.data) {
			return fmt.Errorf("%s is stale", o.path)
		}
		return nil
	}
	dir := filepath.Dir(o.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(o.path, o.data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", o.path, err)
	}
	if _, err := fmt.Fprintln(stdout, o.path); err != nil {
		return fmt.Errorf("report %s: %w", o.path, err)
	}
	return nil
}
