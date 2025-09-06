package instr

type Opcode byte

const (
	NOP Opcode = iota
	UNREACHABLE

	DROP
	DUP
	SWAP

	I32_CONST
	I64_CONST
	F32_CONST
	F64_CONST
)
