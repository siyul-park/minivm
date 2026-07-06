package program

import (
	"fmt"
	"io"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/textparse"
	"github.com/siyul-park/minivm/types"
)

// Parse parses the output of Program.String() back into a Program.
// Format:
//
//	<disassembly lines>   — code section
//	                      — blank line separator
//	<constants section>   — "NNNN:\t<line0>\n\t<continuation>…" (func values)
//	                      — blank line separator
//	<types section>       — "N:\t<type-string>"
func Parse(r io.Reader) (*Program, error) {
	scanner := textparse.NewScanner(r)

	// Phase 1: code section (lines until first blank line or EOF).
	var codeLines []string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			break
		}
		codeLines = append(codeLines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, textparse.LineError(lineNum+1, err)
	}

	codeInstrs, err := instr.ParseAll(strings.NewReader(strings.Join(codeLines, "\n")))
	if err != nil {
		return nil, fmt.Errorf("code: %w", err)
	}

	// Phases 2+: read all remaining multi-line entries (blank line terminates each
	// section; EOF ends reading). Classify each entry by content:
	//   - starts with "func(" → constant (*Function)
	//   - otherwise           → type (single-line Type string)
	var entries [][]string
	var block []string

	flushBlock := func() {
		if len(block) > 0 {
			entries = append(entries, block)
			block = nil
		}
	}

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			flushBlock()
			continue
		}
		if strings.HasPrefix(line, "\t") {
			block = append(block, strings.TrimPrefix(line, "\t"))
		} else {
			flushBlock()
			// Strip "NNNN:\t" or "N:\t" index prefix.
			if idx := strings.Index(line, ":\t"); idx >= 0 {
				line = line[idx+2:]
			}
			block = []string{line}
		}
	}
	flushBlock()
	if err := scanner.Err(); err != nil {
		return nil, textparse.LineError(lineNum+1, err)
	}

	var constants []types.Value
	var typs []types.Type
	for _, entry := range entries {
		if len(entry) == 0 {
			continue
		}
		if strings.HasPrefix(entry[0], "func(") {
			v, err := types.ParseFunction(entry)
			if err != nil {
				return nil, fmt.Errorf("constant: %w", err)
			}
			constants = append(constants, v)
		} else {
			t, err := types.Parse(entry[0])
			if err != nil {
				return nil, fmt.Errorf("type: %w", err)
			}
			typs = append(typs, t)
		}
	}

	var opts []func(*Program)
	if len(constants) > 0 {
		opts = append(opts, WithConstants(constants...))
	}
	if len(typs) > 0 {
		opts = append(opts, WithTypes(typs...))
	}
	return New(codeInstrs, opts...), nil
}
