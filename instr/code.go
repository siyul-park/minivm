package instr

import (
	"fmt"
	"strings"
)

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

func Disassemble(code []byte) string {
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
