package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

// ParseFunction parses lines from Function.String() output:
//
//	line 0:       FunctionType string ("func(params) returns")
//	lines 1..k-1: local type strings (one per line)
//	lines k..:    disassembly lines ("0000:\t…")
func ParseFunction(lines []string) (*Function, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty function definition")
	}

	typ, err := Parse(lines[0])
	if err != nil {
		return nil, fmt.Errorf("function type: %w", err)
	}
	ft, ok := typ.(*FunctionType)
	if !ok {
		return nil, fmt.Errorf("expected func type, got %q", lines[0])
	}

	// Captures are emitted first, each prefixed with "capture ".
	capturesEnd := 1
	var captures []Type
	for capturesEnd < len(lines) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(lines[capturesEnd]), "capture ")
		if !ok {
			break
		}
		t, err := Parse(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("capture type: %w", err)
		}
		captures = append(captures, t)
		capturesEnd++
	}

	// Find where disassembly starts: first line that is either offset-prefixed
	// ("NNNN:\t…") or a plain instruction (not parseable as a type).
	localsEnd := capturesEnd
	for localsEnd < len(lines) {
		line := strings.TrimSpace(lines[localsEnd])
		if isFormatLine(line) || isInstrLine(line) {
			break
		}
		localsEnd++
	}

	var locals []Type
	for _, l := range lines[capturesEnd:localsEnd] {
		t, err := Parse(strings.TrimSpace(l))
		if err != nil {
			return nil, fmt.Errorf("local type: %w", err)
		}
		locals = append(locals, t)
	}

	codeInstrs, err := instr.ParseAll(strings.NewReader(strings.Join(lines[localsEnd:], "\n")))
	if err != nil {
		return nil, fmt.Errorf("function code: %w", err)
	}

	fn := NewFunction(ft, locals, codeInstrs)
	fn.Captures = captures
	return fn, nil
}

// Parse parses a type string produced by Type.String().
// Supported: "i32", "i64", "f32", "f64", "ref", "[]<elem>",
// "iterator[elem]", "map[key]elem", "func(params) returns", "struct {fields}".
func Parse(s string) (Type, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "i1":
		return TypeI1, nil
	case "i8":
		return TypeI8, nil
	case "i32":
		return TypeI32, nil
	case "i64":
		return TypeI64, nil
	case "f32":
		return TypeF32, nil
	case "f64":
		return TypeF64, nil
	case "ref":
		return TypeRef, nil
	case "string":
		return TypeString, nil
	case "error":
		return TypeError, nil
	}
	if strings.HasPrefix(s, "[]") {
		elem, err := Parse(s[2:])
		if err != nil {
			return nil, err
		}
		return NewArrayType(elem), nil
	}
	if strings.HasPrefix(s, "iterator[") {
		return parseIteratorType(s)
	}
	if strings.HasPrefix(s, "map[") {
		return parseMapType(s)
	}
	if strings.HasPrefix(s, "func(") {
		return parseFunctionType(s)
	}
	if strings.HasPrefix(s, "struct {") {
		return parseStructType(s)
	}
	return nil, fmt.Errorf("unknown type: %q", s)
}

func parseIteratorType(s string) (*IteratorType, error) {
	if !strings.HasSuffix(s, "]") || len(s) == len("iterator[]") {
		return nil, fmt.Errorf("invalid iterator type: %q", s)
	}
	elem, err := Parse(s[len("iterator[") : len(s)-1])
	if err != nil {
		return nil, fmt.Errorf("iterator elem type: %w", err)
	}
	return NewIteratorType(elem), nil
}

func parseMapType(s string) (*MapType, error) {
	end := -1
	depth := 0
	for i := len("map["); i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			if depth == 0 {
				end = i
				i = len(s)
			} else {
				depth--
			}
		}
	}
	if end < 0 || end == len("map[") || end == len(s)-1 {
		return nil, fmt.Errorf("invalid map type: %q", s)
	}
	key, err := Parse(s[len("map["):end])
	if err != nil {
		return nil, fmt.Errorf("map key type: %w", err)
	}
	elem, err := Parse(s[end+1:])
	if err != nil {
		return nil, fmt.Errorf("map elem type: %w", err)
	}
	return NewMapType(key, elem), nil
}

func parseFunctionType(s string) (*FunctionType, error) {
	// Strip "func("
	rest := s[5:]
	// Find matching ")"
	depth := 1
	end := -1
	for i, c := range rest {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("missing ) in func type: %q", s)
	}
	paramStr := rest[:end]
	suffix := strings.TrimSpace(rest[end+1:])

	var params []Type
	if paramStr != "" {
		for _, p := range strings.Split(paramStr, ",") {
			t, err := Parse(strings.TrimSpace(p))
			if err != nil {
				return nil, fmt.Errorf("param type: %w", err)
			}
			params = append(params, t)
		}
	}

	var returns []Type
	if suffix != "" {
		if strings.HasPrefix(suffix, "(") {
			inner := suffix[1:]
			if idx := strings.LastIndex(inner, ")"); idx >= 0 {
				inner = inner[:idx]
			}
			for _, r := range strings.Split(inner, ",") {
				t, err := Parse(strings.TrimSpace(r))
				if err != nil {
					return nil, fmt.Errorf("return type: %w", err)
				}
				returns = append(returns, t)
			}
		} else {
			t, err := Parse(suffix)
			if err != nil {
				return nil, fmt.Errorf("return type: %w", err)
			}
			returns = []Type{t}
		}
	}

	return &FunctionType{Params: params, Returns: returns}, nil
}

func parseStructType(s string) (*StructType, error) {
	// "struct {i32; f64}"
	inner := strings.TrimPrefix(s, "struct {")
	inner = strings.TrimSuffix(inner, "}")
	if strings.TrimSpace(inner) == "" {
		return NewStructType(), nil
	}
	var fields []StructField
	for _, f := range strings.Split(inner, ";") {
		t, err := Parse(strings.TrimSpace(f))
		if err != nil {
			return nil, fmt.Errorf("struct field type: %w", err)
		}
		fields = append(fields, NewStructField(t))
	}
	return NewStructType(fields...), nil
}

// isInstrLine reports whether s looks like a plain instruction line without an
// offset prefix. A non-empty line that cannot be parsed as a type declaration
// is treated as an instruction.
func isInstrLine(s string) bool {
	if s == "" {
		return false
	}
	_, err := Parse(s)
	return err != nil
}

// isFormatLine reports whether s looks like a Format output line ("NNNx:\t…").
func isFormatLine(s string) bool {
	if len(s) < 5 || s[4] != ':' {
		return false
	}
	for _, c := range s[:4] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
