package instr

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type Instruction []byte

func New(op Opcode, operands ...uint64) Instruction {
	typ, ok := types[op]
	if !ok {
		return nil
	}

	width := 1
	idx := 0
	for _, w := range typ.Widths {
		if w > 0 {
			width += w
			idx++
		} else {
			count := int(operands[idx])
			width += 1 + count*-w
			idx += count + 1
		}
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
				binary.LittleEndian.PutUint16(code[offset:], uint16(operands[idx]))
			case 4:
				binary.LittleEndian.PutUint32(code[offset:], uint32(operands[idx]))
			case 8:
				binary.LittleEndian.PutUint64(code[offset:], operands[idx])
			default:
				return nil
			}
			offset += w
		}
	}
	return code
}

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
				binary.LittleEndian.PutUint16(i[offset+(index-idx)*2:], uint16(value))
			case 4:
				binary.LittleEndian.PutUint32(i[offset+(index-idx)*4:], uint32(value))
			case 8:
				binary.LittleEndian.PutUint64(i[offset+(index-idx)*8:], value)
			}
			return
		}

		offset += w * (count - idx)
		idx = count
	}
}

func (i Instruction) Operand(index int) uint64 {
	operands := i.Operands()
	if index < 0 || index >= len(operands) {
		return 0
	}
	return operands[index]
}

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
				operands = append(operands, uint64(binary.LittleEndian.Uint16(i[offset:])))
			case 4:
				operands = append(operands, uint64(binary.LittleEndian.Uint32(i[offset:])))
			case 8:
				operands = append(operands, binary.LittleEndian.Uint64(i[offset:]))
			default:
				return nil
			}
			offset += w
		}
	}
	return operands
}

func (i Instruction) Type() Type {
	return TypeOf(i.Opcode())
}

func (i Instruction) Opcode() Opcode {
	return Opcode(i[0])
}
