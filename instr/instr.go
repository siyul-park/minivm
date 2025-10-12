package instr

import (
	"fmt"
	"strings"
	"unsafe"
)

type Instruction []byte

func Unmarshal(code []byte) []Instruction {
	var instrs []Instruction
	for ip := 0; ip < len(code); {
		inst := Instruction(code[ip:])
		width := inst.Width()
		instrs = append(instrs, code[ip:ip+width])
		ip += width
	}
	return instrs
}

func Marshal(instrs []Instruction) []byte {
	var code []byte
	for _, instr := range instrs {
		code = append(code, instr...)
	}
	return code
}

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
				*(*uint16)(unsafe.Pointer(&code[offset])) = uint16(operands[idx])
			case 4:
				*(*uint32)(unsafe.Pointer(&code[offset])) = uint32(operands[idx])
			case 8:
				*(*uint64)(unsafe.Pointer(&code[offset])) = operands[idx]
			default:
				return nil
			}
			offset += w
		}
	}
	return code
}

func (i Instruction) Type() Type {
	return TypeOf(i.Opcode())
}

func (i Instruction) Opcode() Opcode {
	return Opcode(i[0])
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

func (i Instruction) Operand(index int) uint64 {
	operands := i.Operands()
	if index < 0 || index > len(operands) {
		return 0
	}
	return operands[index]
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

func (i Instruction) Width() int {
	typ := i.Type()
	operands := i.Operands()
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
	return width
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
