package instr

import (
	"fmt"
	"strings"
)

func Marshal(instrs []Instruction) []byte {
	var code []byte
	for _, instr := range instrs {
		code = append(code, instr...)
	}
	return code
}

// Targets returns the absolute byte offsets branchable from the instruction at
// ip: one target for BR/BR_IF, one target per case plus the default for
// BR_TABLE, or nil for any non-branching opcode. Offsets are not
// bounds-checked against len(code); callers validate them.
func Targets(code []byte, ip int) []int {
	inst := Instruction(code[ip:])
	next := ip + inst.Width()
	switch inst.Opcode() {
	case BR, BR_IF:
		return []int{next + ReadI16(inst.Operand(0))}
	case BR_TABLE:
		operands := inst.Operands()
		targets := make([]int, 0, len(operands)-1)
		for j := 1; j < len(operands); j++ {
			targets = append(targets, next+ReadI16(operands[j]))
		}
		return targets
	default:
		return nil
	}
}

func Format(code []byte) string {
	var sb strings.Builder
	ip := 0
	for _, inst := range Unmarshal(code) {
		line := fmt.Sprintf("%04d:\t", ip)
		if inst == nil {
			line += "<invalid>\n"
			sb.WriteString(line)
			break
		}
		line += inst.String() + "\n"
		sb.WriteString(line)
		ip += len(inst)
	}
	return sb.String()
}

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
