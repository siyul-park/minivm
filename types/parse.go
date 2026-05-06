package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

// Parse parses a type string produced by Type.String().
// Supported: "i32", "i64", "f32", "f64", "ref", "[]<elem>",
// "func(params) returns", "struct {fields}".
func Parse(s string) (Type, error) {
	s = strings.TrimSpace(s)
	switch s {
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
	}
	if strings.HasPrefix(s, "[]") {
		elem, err := Parse(s[2:])
		if err != nil {
			return nil, err
		}
		return NewArrayType(elem), nil
	}
	if strings.HasPrefix(s, "func(") {
		return parseFunctionType(s)
	}
	if strings.HasPrefix(s, "struct {") {
		return parseStructType(s)
	}
	return nil, fmt.Errorf("unknown type: %q", s)
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

	// Find where disassembly starts (first line matching "NNNN:\t" with hex digits).
	localsEnd := 1
	for localsEnd < len(lines) {
		if isDisassemblyLine(lines[localsEnd]) {
			break
		}
		localsEnd++
	}

	var locals []Type
	for _, l := range lines[1:localsEnd] {
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

	return NewFunction(ft, locals, codeInstrs), nil
}

// isDisassemblyLine reports whether s looks like a Disassemble output line ("NNNx:\t…").
func isDisassemblyLine(s string) bool {
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
