package instr

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var mnemonicMap map[string]Opcode

func init() {
	mnemonicMap = make(map[string]Opcode)
	for i := 0; i <= 0xFF; i++ {
		op := Opcode(i)
		t := TypeOf(op)
		if t.Mnemonic != "" {
			mnemonicMap[t.Mnemonic] = op
		}
	}
}

// Parse parses a single assembly instruction line.
// Accepts both plain format ("i32.const 42") and the offset-prefixed format
// produced by Disassemble ("0000:  i32.const 0x2a"). Returns nil, nil for
// blank lines.
func Parse(line string) (Instruction, error) {
	// Strip optional offset prefix of the form "NNNN:" or "NNNN:\t"
	if idx := strings.IndexByte(line, ':'); idx >= 0 {
		prefix := strings.TrimSpace(line[:idx])
		allDigits := len(prefix) > 0
		for _, c := range prefix {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			line = line[idx+1:]
		}
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	fields := strings.Fields(line)
	mnemonic := fields[0]

	op, ok := mnemonicMap[mnemonic]
	if !ok {
		return nil, fmt.Errorf("unknown mnemonic: %q", mnemonic)
	}

	typ := TypeOf(op)
	operands, err := parseOperands(fields[1:], typ.Widths)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mnemonic, err)
	}

	return New(op, operands...), nil
}

// ParseAll parses multiple newline-separated assembly instruction lines.
// Empty lines are skipped. Returns the first error encountered.
func ParseAll(text string) ([]Instruction, error) {
	var instrs []Instruction
	for lineNum, line := range strings.Split(text, "\n") {
		inst, err := Parse(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum+1, err)
		}
		if inst != nil {
			instrs = append(instrs, inst)
		}
	}
	return instrs, nil
}

func parseOperands(fields []string, widths []int) ([]uint64, error) {
	var operands []uint64
	fi := 0
	for _, w := range widths {
		if w > 0 {
			if fi >= len(fields) {
				return nil, fmt.Errorf("expected operand, got end of input")
			}
			v, err := parseOperand(fields[fi], w)
			if err != nil {
				return nil, fmt.Errorf("operand %d: %w", fi, err)
			}
			operands = append(operands, v)
			fi++
		} else {
			// Variable-length: count byte followed by count × |w| elements
			if fi >= len(fields) {
				return nil, fmt.Errorf("expected count, got end of input")
			}
			count, err := parseOperand(fields[fi], 1)
			if err != nil {
				return nil, fmt.Errorf("count: %w", err)
			}
			operands = append(operands, count)
			fi++
			elemWidth := -w
			for j := uint64(0); j < count; j++ {
				if fi >= len(fields) {
					return nil, fmt.Errorf("expected element %d of %d, got end of input", j, count)
				}
				v, err := parseOperand(fields[fi], elemWidth)
				if err != nil {
					return nil, fmt.Errorf("element %d: %w", j, err)
				}
				operands = append(operands, v)
				fi++
			}
		}
	}
	if fi != len(fields) {
		return nil, fmt.Errorf("unexpected operand %q", fields[fi])
	}
	return operands, nil
}

// parseOperand parses a single token as a uint64.
// Supported formats:
//   - hex: 0x2a or 0X2a
//   - decimal float (for 4- or 8-byte widths): 1.0, -3.14
//   - signed decimal: -1, 42
func parseOperand(s string, width int) (uint64, error) {
	// Hex
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		v, err := strconv.ParseUint(s[2:], 16, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid hex %q: %w", s, err)
		}
		return v, nil
	}
	// Float literal (contains '.' or 'e'/'E') → encode as IEEE 754 bits
	if strings.ContainsAny(s, ".eE") {
		switch width {
		case 4:
			f, err := strconv.ParseFloat(s, 32)
			if err != nil {
				return 0, fmt.Errorf("invalid float32 %q: %w", s, err)
			}
			return uint64(math.Float32bits(float32(f))), nil
		case 8:
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid float64 %q: %w", s, err)
			}
			return math.Float64bits(f), nil
		}
	}
	// Signed decimal (handles negative integers)
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q: %w", s, err)
	}
	return uint64(v), nil
}
