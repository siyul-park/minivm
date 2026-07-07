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

type textLine struct {
	num int
	val string
}

type tableEntry struct {
	line int
	body []string
}

func parseV1(r io.Reader) (*Program, error) {
	lines, err := scanProgramLines(r)
	if err != nil {
		return nil, err
	}
	sections, err := splitProgramSections(lines)
	if err != nil {
		return nil, err
	}
	code, err := instr.ParseAll(strings.NewReader(joinProgramLines(sections[".code"])))
	if err != nil {
		return nil, sectionError(firstProgramLine(sections[".code"]), ".code", "%w", err)
	}
	locals, err := parseTypeSection(".locals", sections[".locals"])
	if err != nil {
		return nil, err
	}
	constants, err := parseConstantSection(sections[".constants"])
	if err != nil {
		return nil, err
	}
	typesTable, err := parseTypeSection(".types", sections[".types"])
	if err != nil {
		return nil, err
	}
	handlers, err := parseHandlerSection(sections[".handlers"])
	if err != nil {
		return nil, err
	}
	var opts []func(*Program)
	if len(locals) > 0 {
		opts = append(opts, WithLocals(locals...))
	}
	if len(constants) > 0 {
		opts = append(opts, WithConstants(constants...))
	}
	if len(typesTable) > 0 {
		opts = append(opts, WithTypes(typesTable...))
	}
	if len(handlers) > 0 {
		opts = append(opts, WithHandlers(handlers...))
	}
	return New(code, opts...), nil
}

func scanProgramLines(r io.Reader) ([]textLine, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), maxParseLineBytes)
	var lines []textLine
	for n := 1; s.Scan(); n++ {
		lines = append(lines, textLine{num: n, val: s.Text()})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func splitProgramSections(lines []textLine) (map[string][]textLine, error) {
	sections := map[string][]textLine{}
	seen := map[string]int{}
	section := ""
	for _, line := range lines {
		text := strings.TrimSpace(line.val)
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, ".") {
			fields := strings.Fields(text)
			name := fields[0]
			if name == ".version" {
				if len(fields) != 2 || fields[1] != strconv.Itoa(textFormatVersion) {
					return nil, sectionError(line.num, name, "expected .version %d", textFormatVersion)
				}
				section = ""
				continue
			}
			if !isProgramSection(name) {
				return nil, sectionError(line.num, name, "unknown section")
			}
			if len(fields) != 1 {
				return nil, sectionError(line.num, name, "section header has operands")
			}
			if prev, ok := seen[name]; ok {
				return nil, sectionError(line.num, name, "duplicate section first declared at line %d", prev)
			}
			seen[name] = line.num
			section = name
			continue
		}
		if section == "" {
			return nil, sectionError(line.num, "program", "content outside a section")
		}
		sections[section] = append(sections[section], line)
	}
	return sections, nil
}

func isProgramSection(s string) bool {
	return s == ".code" || s == ".locals" || s == ".constants" || s == ".types" || s == ".handlers"
}

func parseTypeSection(section string, lines []textLine) ([]types.Type, error) {
	entries, err := parseTableEntries(section, lines)
	if err != nil {
		return nil, err
	}
	out := make([]types.Type, 0, len(entries))
	for i, entry := range entries {
		if len(entry.body) != 1 {
			return nil, sectionError(entry.line, section, "entry %d must be one line", i)
		}
		v, err := types.Parse(entry.body[0])
		if err != nil {
			return nil, sectionError(entry.line, section, "%w", err)
		}
		out = append(out, v)
	}
	return out, nil
}

func parseConstantSection(lines []textLine) ([]types.Value, error) {
	entries, err := parseTableEntries(".constants", lines)
	if err != nil {
		return nil, err
	}
	out := make([]types.Value, 0, len(entries))
	for _, entry := range entries {
		v, err := parseProgramConstant(entry.body)
		if err != nil {
			return nil, sectionError(entry.line, ".constants", "%w", err)
		}
		out = append(out, v)
	}
	return out, nil
}

func parseHandlerSection(lines []textLine) ([]instr.Handler, error) {
	entries, err := parseTableEntries(".handlers", lines)
	if err != nil {
		return nil, err
	}
	out := make([]instr.Handler, 0, len(entries))
	for _, entry := range entries {
		if len(entry.body) != 1 {
			return nil, sectionError(entry.line, ".handlers", "entry must be one line")
		}
		v, err := parseProgramHandler(entry.body[0])
		if err != nil {
			return nil, sectionError(entry.line, ".handlers", "%w", err)
		}
		out = append(out, v)
	}
	return out, nil
}

func parseTableEntries(section string, lines []textLine) ([]tableEntry, error) {
	var out []tableEntry
	var cur *tableEntry
	for _, line := range lines {
		if strings.TrimSpace(line.val) == "" {
			continue
		}
		if strings.HasPrefix(line.val, "\t") {
			if cur == nil {
				return nil, sectionError(line.num, section, "continuation without entry")
			}
			cur.body = append(cur.body, strings.TrimPrefix(line.val, "\t"))
			continue
		}
		if cur != nil {
			out = append(out, *cur)
		}
		idx, body, err := parseIndexedEntry(line.val)
		if err != nil {
			return nil, sectionError(line.num, section, "%w", err)
		}
		if idx != len(out) {
			return nil, sectionError(line.num, section, "expected index %d, got %d", len(out), idx)
		}
		cur = &tableEntry{line: line.num, body: []string{body}}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out, nil
}

func parseIndexedEntry(line string) (int, string, error) {
	head, body, ok := strings.Cut(line, ":")
	if !ok {
		return 0, "", fmt.Errorf("expected indexed entry")
	}
	idx, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil || idx < 0 {
		return 0, "", fmt.Errorf("invalid index %q", head)
	}
	body = strings.TrimLeft(strings.TrimPrefix(body, "\t"), " ")
	return idx, body, nil
}

func parseProgramConstant(lines []string) (types.Value, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty constant")
	}
	if strings.HasPrefix(lines[0], "func(") {
		return types.ParseFunction(lines)
	}
	kind, body, ok := strings.Cut(strings.TrimSpace(lines[0]), " ")
	if !ok || len(lines) != 1 {
		return nil, fmt.Errorf("expected single-line typed constant")
	}
	body = strings.TrimSpace(body)
	switch kind {
	case "i1":
		v, err := strconv.ParseBool(body)
		return types.I1(v), err
	case "i8":
		v, err := strconv.ParseInt(body, 10, 8)
		return types.I8(v), err
	case "i32":
		v, err := strconv.ParseInt(body, 10, 32)
		return types.I32(v), err
	case "i64":
		v, err := strconv.ParseInt(body, 10, 64)
		return types.I64(v), err
	case "f32":
		v, err := strconv.ParseFloat(body, 32)
		return types.F32(v), err
	case "f64":
		v, err := strconv.ParseFloat(body, 64)
		return types.F64(v), err
	case "ref":
		v, err := strconv.ParseInt(body, 10, 32)
		return types.Ref(v), err
	case "string":
		v, err := strconv.Unquote(body)
		return types.String(v), err
	default:
		return nil, fmt.Errorf("unknown constant kind %q", kind)
	}
}

func parseProgramHandler(line string) (instr.Handler, error) {
	values := map[string]int{}
	for _, field := range strings.Fields(line) {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			return instr.Handler{}, fmt.Errorf("expected key=value")
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return instr.Handler{}, err
		}
		values[key] = value
	}
	for _, key := range []string{"start", "end", "catch", "depth"} {
		if _, ok := values[key]; !ok {
			return instr.Handler{}, fmt.Errorf("missing %s", key)
		}
	}
	return instr.Handler{Start: values["start"], End: values["end"], Catch: values["catch"], Depth: values["depth"]}, nil
}

func joinProgramLines(lines []textLine) string {
	parts := make([]string, len(lines))
	for i, line := range lines {
		parts[i] = line.val
	}
	return strings.Join(parts, "\n")
}

func firstProgramLine(lines []textLine) int {
	if len(lines) == 0 {
		return 0
	}
	return lines[0].num
}

func sectionError(line int, section, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if line == 0 {
		return fmt.Errorf("%s: %s", section, msg)
	}
	return fmt.Errorf("line %d %s: %s", line, section, msg)
}
