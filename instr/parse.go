package instr

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const maxParseLineBytes = 1 << 20 // 1 MiB

var mnemonicMap map[string]Opcode

// ReadU8 returns v truncated to 8 bits.
func ReadU8(v uint64) int {
	return int(uint8(v))
}

// ReadI8 returns v sign-extended from 8 bits.
func ReadI8(v uint64) int {
	return int(int8(uint8(v)))
}

// ReadU16 returns v truncated to 16 bits.
func ReadU16(v uint64) int {
	return int(uint16(v))
}

// ReadI16 returns v sign-extended from 16 bits.
func ReadI16(v uint64) int {
	return int(int16(uint16(v)))
}

// ReadU32 returns v truncated to 32 bits.
func ReadU32(v uint64) int {
	return int(uint32(v))
}

// ReadI32 returns v sign-extended from 32 bits.
func ReadI32(v uint64) int {
	return int(int32(uint32(v)))
}

// ParseU8 reads an unsigned 8-bit value from code[offset:].
func ParseU8(code []byte, offset int) int {
	return int(code[offset])
}

// ParseI8 reads a signed 8-bit value from code[offset:].
func ParseI8(code []byte, offset int) int {
	return int(int8(code[offset]))
}

// ParseU16 reads a little-endian unsigned 16-bit value from code[offset:].
func ParseU16(code []byte, offset int) int {
	return int(uint16(code[offset]) |
		uint16(code[offset+1])<<8)
}

// ParseI16 reads a little-endian signed 16-bit value from code[offset:].
func ParseI16(code []byte, offset int) int {
	return int(int16(uint16(ParseU16(code, offset))))
}

// ParseU32 reads a little-endian unsigned 32-bit value from code[offset:].
func ParseU32(code []byte, offset int) int {
	return int(uint32(code[offset]) |
		uint32(code[offset+1])<<8 |
		uint32(code[offset+2])<<16 |
		uint32(code[offset+3])<<24)
}

// ParseI32 reads a little-endian signed 32-bit value from code[offset:].
func ParseI32(code []byte, offset int) int {
	return int(int32(uint32(ParseU32(code, offset))))
}

// Parse parses a single assembly instruction line.
// Accepts both plain format ("i32.const 42") and the offset-prefixed format
// produced by Format ("0000:  i32.const 0x2a"). Returns nil, nil for
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

// ParseAll reads from r line by line and parses each non-empty line as an
// assembly instruction. It returns the first error encountered with the line
// number for context.
func ParseAll(r io.Reader) ([]Instruction, error) {
	var instrs []Instruction
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxParseLineBytes)
	line := 1
	for ; scanner.Scan(); line++ {
		inst, err := Parse(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if inst != nil {
			instrs = append(instrs, inst)
		}
	}
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			return nil, fmt.Errorf("line %d exceeds maximum allowed size of %d bytes", line, maxParseLineBytes)
		}
		return nil, fmt.Errorf("line %d: %w", line, err)
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
			// Variable-length: count byte followed by count x |w| elements
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
	// Float literal (contains '.' or 'e'/'E') -> encode as IEEE 754 bits
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
