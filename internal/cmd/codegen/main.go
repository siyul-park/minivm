package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/siyul-park/minivm/internal/codegen"
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
	files, err := codegen.Generate()
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}
	for _, file := range files {
		if check {
			if err := verify(file); err != nil {
				return err
			}
			continue
		}
		if err := write(file, stdout); err != nil {
			return err
		}
	}
	return nil
}

func verify(file codegen.File) error {
	actual, err := os.ReadFile(file.Path)
	if err != nil {
		return fmt.Errorf("read %s: %w", file.Path, err)
	}
	if !bytes.Equal(actual, file.Data) {
		return fmt.Errorf("%s is stale", file.Path)
	}
	return nil
}

func write(file codegen.File, stdout io.Writer) error {
	dir := filepath.Dir(file.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(file.Path, file.Data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", file.Path, err)
	}
	if _, err := fmt.Fprintln(stdout, file.Path); err != nil {
		return fmt.Errorf("report %s: %w", file.Path, err)
	}
	return nil
}
