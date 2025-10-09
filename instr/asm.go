package instr

import (
	"fmt"
	"strings"
)

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
