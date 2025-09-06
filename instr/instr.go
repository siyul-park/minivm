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
	for _, w := range typ.Widths {
		width += w
	}

	bytecode := make(Instruction, width)
	bytecode[0] = byte(op)

	offset := 1
	for i, o := range operands {
		w := typ.Widths[i]
		switch w {
		case 1:
			bytecode[offset] = byte(o)
		case 2:
			binary.BigEndian.PutUint16(bytecode[offset:], uint16(o))
		case 4:
			binary.BigEndian.PutUint32(bytecode[offset:], uint32(o))
		case 8:
			binary.BigEndian.PutUint64(bytecode[offset:], o)
		default:
			return nil
		}
		offset += w
	}
	return bytecode
}

func (i Instruction) Type() Type {
	return TypeOf(i.Opcode())
}

func (i Instruction) Opcode() Opcode {
	return Opcode(i[0])
}

func (i Instruction) Operands() []uint64 {
	typ := i.Type()
	operands := make([]uint64, len(typ.Widths))
	offset := 0
	for j, w := range typ.Widths {
		switch w {
		case 1:
			operands[j] = uint64(i[1+offset])
		case 2:
			operands[j] = uint64(binary.BigEndian.Uint16(i[1+offset:]))
		case 4:
			operands[j] = uint64(binary.BigEndian.Uint32(i[1+offset:]))
		case 8:
			operands[j] = binary.BigEndian.Uint64(i[1+offset:])
		default:
			continue
		}
		offset += w
	}
	return operands
}

func (i Instruction) String() string {
	typ := i.Type()
	if len(typ.Widths) == 0 {
		return typ.Mnemonic
	}

	operands := i.Operands()
	widths := typ.Widths

	var ops []string
	for idx, operand := range operands {
		ops = append(ops, fmt.Sprintf("0x%0*X", widths[idx]*2, operand))
	}
	return fmt.Sprintf("%s %s", typ.Mnemonic, strings.Join(ops, " "))
}
