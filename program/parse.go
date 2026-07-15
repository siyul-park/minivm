package program

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

const maxParseLineBytes = 1 << 20 // 1 MiB

func Parse(r io.Reader) (*Program, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	text := string(data)
	text = strings.TrimSpace(text)
	if text == "" {
		return New(nil), nil
	}

	firstLine := text
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		firstLine = text[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)

	if strings.HasPrefix(firstLine, ".") {
		return sections(text)
	}
	return legacy(text)
}

func sections(text string) (*Program, error) {
	lines := strings.Split(text, "\n")
	prog := &Program{}
	seen := map[string]int{}

	var section string
	var sectionLineStart int
	var sectionLines []string

	for i, rawLine := range lines {
		lineNum := i + 1
		line := strings.TrimSpace(rawLine)

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, ".") {
			if section != "" {
				if err := parseSection(prog, section, sectionLineStart, sectionLines); err != nil {
					return nil, err
				}
				sectionLines = nil
			}
			fields := strings.Fields(line)
			section = fields[0]
			sectionLineStart = lineNum

			if prev := seen[section]; prev > 0 {
				return nil, fmt.Errorf("line %d: duplicate section %s (first at line %d)", lineNum, section, prev)
			}
			seen[section] = lineNum
			continue
		}

		sectionLines = append(sectionLines, rawLine)
	}

	if section != "" {
		if err := parseSection(prog, section, sectionLineStart, sectionLines); err != nil {
			return nil, err
		}
	}

	return prog, nil
}

func parseSection(prog *Program, section string, lineStart int, lines []string) error {
	var err error
	switch section {
	case ".code":
		code, err := instr.ParseAll(strings.NewReader(strings.Join(lines, "\n")))
		if err != nil {
			return fmt.Errorf("%s (line %d): %w", section, lineStart, err)
		}
		prog.Code = instr.Marshal(code)
	case ".locals":
		prog.Locals, err = parseTypes(lines)
	case ".globals":
		prog.Globals, err = parseTypes(lines)
	case ".constants":
		prog.Constants, err = parseConstants(lines)
	case ".types":
		prog.Types, err = parseTypes(lines)
	case ".handlers":
		prog.Handlers, err = parseHandlers(lines)
	default:
		return fmt.Errorf("line %d: unknown section %s", lineStart, section)
	}
	if err != nil {
		return fmt.Errorf("%s (line %d): %w", section, lineStart, err)
	}
	return nil
}

func legacy(text string) (*Program, error) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), maxParseLineBytes)

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
		if strings.Contains(err.Error(), "token too long") {
			return nil, fmt.Errorf("line %d exceeds maximum allowed size of %d bytes", lineNum+1, maxParseLineBytes)
		}
		return nil, fmt.Errorf("line %d: %w", lineNum+1, err)
	}

	code, err := instr.ParseAll(strings.NewReader(strings.Join(codeLines, "\n")))
	if err != nil {
		return nil, fmt.Errorf("code: %w", err)
	}

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
			if idx := strings.Index(line, ":\t"); idx >= 0 {
				line = line[idx+2:]
			}
			block = []string{line}
		}
	}
	flushBlock()
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			return nil, fmt.Errorf("line %d exceeds maximum allowed size of %d bytes", lineNum+1, maxParseLineBytes)
		}
		return nil, fmt.Errorf("line %d: %w", lineNum+1, err)
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
	return New(code, opts...), nil
}

func parseTypes(lines []string) ([]types.Type, error) {
	var result []types.Type
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if idx := strings.Index(trimmed, ":\t"); idx >= 0 {
			trimmed = trimmed[idx+2:]
		} else if idx := strings.IndexByte(trimmed, ':'); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		}
		if trimmed == "" {
			continue
		}
		t, err := types.Parse(trimmed)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, nil
}

func parseConstants(lines []string) ([]types.Value, error) {
	var entries [][]string
	var current []string
	hasCurrent := false

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}

		if len(rawLine) > 0 && (rawLine[0] == '\t' || rawLine[0] == ' ') {
			if !hasCurrent {
				return nil, fmt.Errorf("continuation without entry start")
			}
			current = append(current, strings.TrimSpace(rawLine))
		} else {
			if hasCurrent {
				entries = append(entries, current)
			}
			content := trimmed
			if idx := strings.Index(trimmed, ":\t"); idx >= 0 {
				content = trimmed[idx+2:]
			} else if idx := strings.IndexByte(trimmed, ':'); idx >= 0 {
				content = strings.TrimSpace(trimmed[idx+1:])
			}
			current = []string{content}
			hasCurrent = true
		}
	}
	if hasCurrent {
		entries = append(entries, current)
	}

	var result []types.Value
	for _, entry := range entries {
		if len(entry) == 0 {
			continue
		}
		if strings.HasPrefix(entry[0], "func(") {
			v, err := types.ParseFunction(entry)
			if err != nil {
				return nil, err
			}
			result = append(result, v)
		} else {
			v, err := parseLiteral(entry[0])
			if err != nil {
				return nil, err
			}
			result = append(result, v)
		}
	}
	return result, nil
}

func parseHandlers(lines []string) ([]instr.Handler, error) {
	var handlers []instr.Handler
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if idx := strings.Index(trimmed, ":\t"); idx >= 0 {
			trimmed = trimmed[idx+2:]
		} else if idx := strings.IndexByte(trimmed, ':'); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		}
		if trimmed == "" {
			continue
		}

		var h instr.Handler
		for _, f := range strings.Fields(trimmed) {
			key, val, ok := strings.Cut(f, "=")
			if !ok {
				return nil, fmt.Errorf("invalid handler field %q (expected key=value)", f)
			}
			v, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid handler value %q: %w", val, err)
			}
			switch key {
			case "start":
				h.Start = v
			case "end":
				h.End = v
			case "catch":
				h.Catch = v
			case "depth":
				h.Depth = v
			default:
				return nil, fmt.Errorf("unknown handler field %q", key)
			}
		}
		handlers = append(handlers, h)
	}
	return handlers, nil
}

func parseLiteral(s string) (types.Value, error) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return nil, fmt.Errorf("expected typed literal (e.g., \"i32 42\"), got %q", s)
	}

	typeName := fields[0]
	valueStr := strings.Join(fields[1:], " ")

	switch typeName {
	case "i32":
		v, err := strconv.ParseInt(valueStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid i32 literal %q", valueStr)
		}
		return types.I32(v), nil
	case "i64":
		v, err := strconv.ParseInt(valueStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid i64 literal %q", valueStr)
		}
		return types.I64(v), nil
	case "f32":
		v, err := strconv.ParseFloat(valueStr, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid f32 literal %q", valueStr)
		}
		return types.F32(v), nil
	case "f64":
		v, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid f64 literal %q", valueStr)
		}
		return types.F64(v), nil
	case "i1":
		switch valueStr {
		case "true":
			return types.I1(true), nil
		case "false":
			return types.I1(false), nil
		default:
			return nil, fmt.Errorf("invalid i1 literal %q (expected true/false)", valueStr)
		}
	case "i8":
		v, err := strconv.ParseInt(valueStr, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid i8 literal %q", valueStr)
		}
		return types.I8(v), nil
	case "ref":
		v, err := strconv.ParseInt(valueStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid ref literal %q", valueStr)
		}
		return types.Ref(v), nil
	default:
		return nil, fmt.Errorf("unknown constant type %q", typeName)
	}
}
