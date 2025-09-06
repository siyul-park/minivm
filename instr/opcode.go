package instr

type Opcode byte

const (
	NOP Opcode = iota
	UNREACHABLE

	I32_CONST
	I64_CONST
	F32_CONST
	F64_CONST
)
