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
		op := Opcode(code[ip])
		typ, ok := types[op]
		if !ok {
			break
		}
		size := 1
		for _, w := range typ.Widths {
			size += w
		}
		if ip+size > len(code) {
			break
		}
		instrs = append(instrs, code[ip:ip+size])
		ip += size
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
			ptr := (*uint16)(unsafe.Pointer(&bytecode[offset]))
			*ptr = uint16(o)
		case 4:
			ptr := (*uint32)(unsafe.Pointer(&bytecode[offset]))
			*ptr = uint32(o)
		case 8:
			ptr := (*uint64)(unsafe.Pointer(&bytecode[offset]))
			*ptr = o
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

func (i Instruction) Operand(idx int) uint64 {
	operands := i.Operands()
	if idx < 0 || idx > len(operands) {
		return 0
	}
	return operands[idx]
}

func (i Instruction) Operands() []uint64 {
	typ := i.Type()
	operands := make([]uint64, len(typ.Widths))
	offset := 0
	for j, w := range typ.Widths {
		ptr := unsafe.Pointer(&i[1+offset])
		switch w {
		case 1:
			operands[j] = uint64(*(*byte)(ptr))
		case 2:
			operands[j] = uint64(*(*uint16)(ptr))
		case 4:
			operands[j] = uint64(*(*uint32)(ptr))
		case 8:
			operands[j] = *(*uint64)(ptr)
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
