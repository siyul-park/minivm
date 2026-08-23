package instr

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// labelTable maps label identifiers to Builder labels for ParseAll, and
// remembers where each name was defined and first referenced so a
// resolution failure can name the label and the line instead of the opaque
// handle Builder's own errors carry.
type labelTable struct {
	b       *Builder
	byName  map[string]Label
	names   []string
	defLine map[string]int
	refLine map[string]int
}

// namedFixup pairs a pending branch fixup with the source line that
// referenced it, for error reporting only; it parallels Builder.fixups.
type namedFixup struct {
	label Label
	line  int
}

// branchRef is one symbolic operand of a placeholder branch instruction,
// awaiting resolution to the label's byte offset.
type branchRef struct {
	operand int
	label   Label
}

const maxParseLineBytes = 1 << 20 // 1 MiB

// ErrDuplicateLabel reports a label identifier bound by more than one
// definition line in the same ParseAll input.
var ErrDuplicateLabel = errors.New("duplicate label")

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

// ParseI16 reads a little-endian signed 16-bit value from code[offset:].
func ParseI16(code []byte, offset int) int {
	return int(int16(uint16(ParseU16(code, offset))))
}

// ParseU16 reads a little-endian unsigned 16-bit value from code[offset:].
func ParseU16(code []byte, offset int) int {
	return int(uint16(code[offset]) |
		uint16(code[offset+1])<<8)
}

// ParseI32 reads a little-endian signed 32-bit value from code[offset:].
func ParseI32(code []byte, offset int) int {
	return int(int32(uint32(ParseU32(code, offset))))
}

// ParseU32 reads a little-endian unsigned 32-bit value from code[offset:].
func ParseU32(code []byte, offset int) int {
	return int(uint32(code[offset]) |
		uint32(code[offset+1])<<8 |
		uint32(code[offset+2])<<16 |
		uint32(code[offset+3])<<24)
}

// ParseAll reads from r line by line and parses each non-empty line as an
// assembly instruction, a label definition ("name:"), or a branch mnemonic
// carrying symbolic label operands (e.g. "br loop", "br_table 0x02 case0
// case1 done"). Labels may be referenced before they are defined; ParseAll
// resolves every reference once the whole input has been read, using
// instr.Builder to back-patch each branch into the signed 16-bit relative
// offset the interpreter expects. It returns the first error encountered
// with the line number for context.
func ParseAll(r io.Reader) ([]Instruction, error) {
	b := NewBuilder()
	lt := newLabelTable(b)
	var refs []namedFixup

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxParseLineBytes)
	line := 1
	for ; scanner.Scan(); line++ {
		text := strings.TrimSpace(stripOffsetPrefix(scanner.Text()))
		if text == "" {
			continue
		}

		if name, ok := parseLabelLine(text); ok {
			if err := lt.define(name, line); err != nil {
				return nil, err
			}
			continue
		}

		fields := strings.Fields(text)
		op, ok := mnemonicMap[fields[0]]
		if !ok {
			return nil, fmt.Errorf("line %d: unknown mnemonic: %q", line, fields[0])
		}

		if op.IsBranch() {
			inst, brefs, err := parseBranch(op, fields[0], fields[1:], lt, line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			idx := len(b.instrs)
			b.instrs = append(b.instrs, inst)
			for _, br := range brefs {
				b.fixups = append(b.fixups, fixup{branch: idx, operand: br.operand, label: br.label})
				refs = append(refs, namedFixup{label: br.label, line: line})
			}
			continue
		}

		inst, err := Parse(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if inst != nil {
			b.Append(inst)
		}
	}
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			return nil, fmt.Errorf("line %d exceeds maximum allowed size of %d bytes", line, maxParseLineBytes)
		}
		return nil, fmt.Errorf("line %d: %w", line, err)
	}

	instrs, err := b.Assemble()
	if err != nil {
		return nil, describeAssembleErr(b, lt, refs, err)
	}
	return instrs, nil
}

// Parse parses a single assembly instruction line.
// Accepts both plain format ("i32.const 42") and the offset-prefixed format
// produced by Format ("0000:  i32.const 0x2a"). Returns nil, nil for
// blank lines. Branch operands must be numeric; symbolic labels are resolved
// only by ParseAll, which sees the whole program and can back-patch a
// forward reference.
func Parse(line string) (Instruction, error) {
	line = strings.TrimSpace(stripOffsetPrefix(line))
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

// stripOffsetPrefix removes a leading "NNNN:" or "NNNN:\t" byte-offset
// prefix of the kind Format emits ahead of each instruction, if present.
func stripOffsetPrefix(line string) string {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return line
	}
	prefix := strings.TrimSpace(line[:idx])
	if prefix == "" {
		return line
	}
	for _, c := range prefix {
		if c < '0' || c > '9' {
			return line
		}
	}
	return line[idx+1:]
}

// parseLabelLine reports whether line declares a label: a bare identifier
// followed by a colon and nothing else (e.g. "loop:"). It is distinguished
// from the offset-prefixed instruction lines produced by Format ("0005:\tbr
// loop") because stripOffsetPrefix has already removed any leading numeric
// offset, and a label identifier can never be all digits.
func parseLabelLine(line string) (name string, ok bool) {
	if !strings.HasSuffix(line, ":") {
		return "", false
	}
	name = line[:len(line)-1]
	if !isLabelIdent(name) {
		return "", false
	}
	return name, true
}

// isLabelIdent reports whether s is a valid label identifier: a letter or
// underscore followed by letters, digits, or underscores.
func isLabelIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

func newLabelTable(b *Builder) *labelTable {
	return &labelTable{
		b:       b,
		byName:  map[string]Label{},
		defLine: map[string]int{},
		refLine: map[string]int{},
	}
}

// get returns name's label handle, allocating one on first mention.
func (lt *labelTable) get(name string) Label {
	if l, ok := lt.byName[name]; ok {
		return l
	}
	l := lt.b.Label()
	lt.byName[name] = l
	lt.names = append(lt.names, name)
	return l
}

// define binds name to the next instruction Builder emits, failing if an
// earlier line already defined it.
func (lt *labelTable) define(name string, line int) error {
	if def, ok := lt.defLine[name]; ok {
		return fmt.Errorf("line %d: %w: %q (first defined at line %d)", line, ErrDuplicateLabel, name, def)
	}
	lt.b.Bind(lt.get(name))
	lt.defLine[name] = line
	return nil
}

// reference records line as a use site of name, the first if there are
// several, and returns its label handle.
func (lt *labelTable) reference(name string, line int) Label {
	if _, ok := lt.refLine[name]; !ok {
		lt.refLine[name] = line
	}
	return lt.get(name)
}

// name returns the identifier a label handle was created from.
func (lt *labelTable) name(l Label) string {
	return lt.names[int(l)]
}

// parseBranch parses the operand fields of a br/br_if/br_table line. Each
// target operand accepts either a numeric literal, resolved exactly as
// before, or a label identifier, resolved by lt. BR_TABLE is written "count
// case0 case1 ... default", matching the raw encoding order: the leading
// count is always numeric (it is not a branch target) and fixes how many of
// the remaining fields are cases versus the trailing default.
func parseBranch(op Opcode, mnemonic string, fields []string, lt *labelTable, line int) (Instruction, []branchRef, error) {
	switch op {
	case BR, BR_IF:
		if len(fields) != 1 {
			return nil, nil, fmt.Errorf("%s: expected 1 operand, got %d", mnemonic, len(fields))
		}
		val, ref, err := parseBranchOperand(fields[0], lt, line)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", mnemonic, err)
		}
		inst := New(op, val)
		if ref == nil {
			return inst, nil, nil
		}
		return inst, []branchRef{{operand: 0, label: *ref}}, nil

	case BR_TABLE:
		if len(fields) == 0 {
			return nil, nil, fmt.Errorf("%s: expected a case count", mnemonic)
		}
		count, err := parseOperand(fields[0], 1)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: count: %w", mnemonic, err)
		}
		want := int(count) + 1
		if len(fields)-1 != want {
			return nil, nil, fmt.Errorf("%s: count %d requires %d targets, got %d", mnemonic, count, want, len(fields)-1)
		}

		operands := make([]uint64, want+1)
		operands[0] = count

		var refs []branchRef
		for i, f := range fields[1:] {
			val, ref, err := parseBranchOperand(f, lt, line)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: operand %d: %w", mnemonic, i, err)
			}
			operands[1+i] = val
			if ref != nil {
				refs = append(refs, branchRef{operand: 1 + i, label: *ref})
			}
		}
		return New(op, operands...), refs, nil

	default:
		return nil, nil, fmt.Errorf("%s: not a branch opcode", mnemonic)
	}
}

// isNumericToken reports whether tok looks like the start of a numeric
// literal (as parseOperand accepts) rather than a label identifier.
func isNumericToken(tok string) bool {
	return tok != "" && (tok[0] == '-' || (tok[0] >= '0' && tok[0] <= '9'))
}

// parseBranchOperand parses one branch operand token, returning either its
// resolved numeric value or a pending label reference.
func parseBranchOperand(tok string, lt *labelTable, line int) (uint64, *Label, error) {
	if isNumericToken(tok) {
		v, err := parseOperand(tok, 2)
		if err != nil {
			return 0, nil, err
		}
		return v, nil, nil
	}
	if !isLabelIdent(tok) {
		return 0, nil, fmt.Errorf("invalid branch operand %q", tok)
	}
	l := lt.reference(tok, line)
	return 0, &l, nil
}

// describeAssembleErr replaces a Builder.Assemble failure, which names an
// unresolved label only by its opaque handle, with one naming the
// identifier and the line ParseAll could not resolve.
func describeAssembleErr(b *Builder, lt *labelTable, refs []namedFixup, err error) error {
	switch {
	case errors.Is(err, ErrUnboundLabel):
		for _, name := range lt.names {
			if b.labels[lt.byName[name]] < 0 {
				return fmt.Errorf("line %d: %w: %q", lt.refLine[name], ErrUnboundLabel, name)
			}
		}
	case errors.Is(err, ErrOffsetRange):
		pos := make([]int, len(b.instrs)+1)
		for i, inst := range b.instrs {
			pos[i+1] = pos[i] + inst.Width()
		}
		for i, fx := range b.fixups {
			target := b.labels[fx.label]
			if target < 0 {
				continue
			}
			offset := pos[target] - (pos[fx.branch] + b.instrs[fx.branch].Width())
			if offset < math.MinInt16 || offset > math.MaxInt16 {
				return fmt.Errorf("line %d: %w: %q", refs[i].line, ErrOffsetRange, lt.name(fx.label))
			}
		}
	}
	return err
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
