package instr

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unsafe"
)

// Instruction is one encoded instruction: the opcode byte followed by its operands.
type Instruction []byte

var (
	// ErrUnknownOpcode reports an opcode with no encoding.
	ErrUnknownOpcode = errors.New("unknown opcode")
	// ErrOperandCount reports an operand list that does not match the opcode's shape.
	ErrOperandCount = errors.New("operand count mismatch")
	// ErrOperandRange reports an operand that does not fit its encoded width.
	ErrOperandRange = errors.New("operand out of range")
)

// MustNew is like New but panics on failure. Use it only for statically
// known-good instructions, such as in tests and fixtures.
func MustNew(op Opcode, operands ...uint64) Instruction {
	inst, err := New(op, operands...)
	if err != nil {
		panic(err)
	}
	return inst
}

// New encodes op with operands. It fails when op is unknown, the operand count
// does not match the opcode exactly, or an operand does not fit its width. An
// operand fits when it is unsigned within the width or a sign-extended negative
// within the signed width. A variable-length operand list is led by a one-byte
// count of the operands that follow it.
func New(op Opcode, operands ...uint64) (Instruction, error) {
	typ, ok := types[op]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownOpcode, op)
	}

	width := 1
	idx := 0
	for _, w := range typ.Widths {
		n := 1
		if w < 0 {
			if idx >= len(operands) {
				return nil, fmt.Errorf("%w: %s needs a count operand, got %d operands", ErrOperandCount, typ.Mnemonic, len(operands))
			}
			if operands[idx] > math.MaxUint8 {
				return nil, fmt.Errorf("%w: %s operand %d: count %d exceeds %d", ErrOperandRange, typ.Mnemonic, idx, operands[idx], math.MaxUint8)
			}
			n, w = int(operands[idx]), -w
			width++
			idx++
		}
		if idx+n > len(operands) {
			return nil, fmt.Errorf("%w: %s needs %d operands, got %d", ErrOperandCount, typ.Mnemonic, idx+n, len(operands))
		}
		for ; n > 0; n-- {
			if !fits(operands[idx], w) {
				return nil, fmt.Errorf("%w: %s operand %d: %d does not fit %d bytes", ErrOperandRange, typ.Mnemonic, idx, int64(operands[idx]), w)
			}
			width += w
			idx++
		}
	}
	if idx != len(operands) {
		return nil, fmt.Errorf("%w: %s takes %d operands, got %d", ErrOperandCount, typ.Mnemonic, idx, len(operands))
	}

	code := make(Instruction, width)
	code[0] = byte(op)

	offset := 1
	idx = 0
	for _, w := range typ.Widths {
		count := idx + 1
		if w < 0 {
			code[offset] = byte(operands[idx])
			count += int(operands[idx])
			w *= -1
			offset++
			idx++
		}
		for ; idx < count; idx++ {
			switch w {
			case 1:
				code[offset] = byte(operands[idx])
			case 2:
				*(*uint16)(unsafe.Pointer(&code[offset])) = uint16(operands[idx])
			case 4:
				*(*uint32)(unsafe.Pointer(&code[offset])) = uint32(operands[idx])
			case 8:
				*(*uint64)(unsafe.Pointer(&code[offset])) = operands[idx]
			}
			offset += w
		}
	}
	return code, nil
}

// SetOperand writes value into the operand at index, ignoring an index out of range.
func (i Instruction) SetOperand(index int, value uint64) {
	typ := i.Type()

	offset := 1
	idx := 0
	for _, w := range typ.Widths {
		count := idx + 1
		if w < 0 {
			if index == idx {
				i[offset] = byte(value)
				return
			}
			count += int(i[offset])
			w *= -1
			offset++
			idx++
		}

		if index >= idx && index < count {
			switch w {
			case 1:
				i[offset+(index-idx)] = byte(value)
			case 2:
				*(*uint16)(unsafe.Pointer(&i[offset+(index-idx)*2])) = uint16(value)
			case 4:
				*(*uint32)(unsafe.Pointer(&i[offset+(index-idx)*4])) = uint32(value)
			case 8:
				*(*uint64)(unsafe.Pointer(&i[offset+(index-idx)*8])) = value
			}
			return
		}

		offset += w * (count - idx)
		idx = count
	}
}

// Operand returns the operand at index, or 0 when out of range.
func (i Instruction) Operand(index int) uint64 {
	operands := i.Operands()
	if index < 0 || index >= len(operands) {
		return 0
	}
	return operands[index]
}

// Width returns the encoded size in bytes.
func (i Instruction) Width() int {
	typ := i.Type()
	offset := 1
	for _, w := range typ.Widths {
		if w > 0 {
			offset += w
		} else {
			count := int(i[offset])
			offset += 1 + count*-w
		}
	}
	return offset
}

// Operands decodes all operands, with a variable-width operand list flattened after its count.
func (i Instruction) Operands() []uint64 {
	typ := i.Type()

	var operands []uint64
	offset := 1
	idx := 0
	for _, w := range typ.Widths {
		count := idx + 1
		if w < 0 {
			operands = append(operands, uint64(i[offset]))
			count += int(i[offset])
			w *= -1
			offset++
			idx++
		}
		for ; idx < count; idx++ {
			switch w {
			case 1:
				operands = append(operands, uint64(i[offset]))
			case 2:
				operands = append(operands, uint64(*(*uint16)(unsafe.Pointer(&i[offset]))))
			case 4:
				operands = append(operands, uint64(*(*uint32)(unsafe.Pointer(&i[offset]))))
			case 8:
				operands = append(operands, *(*uint64)(unsafe.Pointer(&i[offset])))
			default:
				return nil
			}
			offset += w
		}
	}
	return operands
}

// Opcode returns the leading opcode.
func (i Instruction) Opcode() Opcode {
	return Opcode(i[0])
}

// Type returns the value type.
func (i Instruction) Type() Type {
	return TypeOf(i.Opcode())
}

// String returns the textual representation.
func (i Instruction) String() string {
	typ := i.Type()

	var sb strings.Builder
	sb.WriteString(typ.Mnemonic)

	operands := i.Operands()
	offset := 0
	for _, w := range typ.Widths {
		count := offset + 1
		if w < 0 {
			sb.WriteByte(' ')
			sb.WriteString(fmt.Sprintf("0x%02x", operands[offset]))
			count += int(operands[offset])
			w *= -1
			offset++
		}
		for ; offset < count; offset++ {
			sb.WriteByte(' ')
			sb.WriteString(fmt.Sprintf("0x%0*X", w*2, operands[offset]))
		}
	}
	return sb.String()
}

// fits reports whether v is an unsigned value within w bytes or a sign-extended
// negative within the signed w-byte range.
func fits(v uint64, w int) bool {
	if w >= 8 {
		return true
	}
	bits := uint(w) * 8
	return v>>bits == 0 || (int64(v) < 0 && int64(v) >= -(int64(1)<<(bits-1)))
}
