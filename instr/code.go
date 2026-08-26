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

// Format disassembles code into one line per instruction, prefixed with its
// byte offset. Every branch target that lands exactly on an instruction
// boundary is also given a stable "L%04d:" label line ahead of that
// instruction, and BR/BR_IF/BR_TABLE operands reaching such a target render
// as the label name instead of a raw relative offset; ParseAll(Format(code))
// reproduces code exactly. A target that is out of range or falls inside
// another instruction (only reachable from malformed code) has no label and
// falls back to the numeric rendering Instruction.String() uses. BR_TABLE
// renders in the same field order as Instruction.String(), "count case0
// case1 ... default", so Format's dialect is a strict superset of
// Instruction.String()'s.
func Format(code []byte) string {
	instrs := Unmarshal(code)
	labelAt := branchLabelNames(code, instrs)

	var sb strings.Builder
	ip := 0
	for _, inst := range instrs {
		if name, ok := labelAt[ip]; ok {
			sb.WriteString(name)
			sb.WriteString(":\n")
		}
		line := fmt.Sprintf("%04d:\t", ip)
		if inst == nil {
			line += "<invalid>\n"
			sb.WriteString(line)
			break
		}
		if inst.Opcode().IsBranch() {
			line += formatBranch(inst, ip, labelAt) + "\n"
		} else {
			line += inst.String() + "\n"
		}
		sb.WriteString(line)
		ip += len(inst)
	}
	if name, ok := labelAt[ip]; ok {
		sb.WriteString(name)
		sb.WriteString(":\n")
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

// branchLabelNames computes a deterministic "L%04d" name for every branch
// target that lands exactly on an instruction boundary in instrs. The offset
// one past the last instruction is a boundary too: a loop exit branches there
// to leave the code, and program/verify.go accepts it. A target that lands
// anywhere else is left unnamed; Format renders it numerically instead.
func branchLabelNames(code []byte, instrs []Instruction) map[int]string {
	starts := make(map[int]bool, len(instrs)+1)
	var targets []int
	ip := 0
	for _, inst := range instrs {
		starts[ip] = true
		targets = append(targets, Targets(code, ip)...)
		ip += len(inst)
	}
	starts[ip] = true

	names := make(map[int]string, len(targets))
	for _, t := range targets {
		if starts[t] {
			names[t] = fmt.Sprintf("L%04d", t)
		}
	}
	return names
}

// formatBranch renders a BR/BR_IF/BR_TABLE instruction at ip, substituting
// labelAt's name for any operand whose absolute target has one. BR_TABLE is
// rendered "count case0 case1 ... default", matching the raw encoding order
// (and Instruction.String()) with the count kept numeric and every target
// position eligible for label substitution.
func formatBranch(inst Instruction, ip int, labelAt map[int]string) string {
	next := ip + inst.Width()
	target := func(rel uint64) string {
		if name, ok := labelAt[next+ReadI16(rel)]; ok {
			return name
		}
		return fmt.Sprintf("0x%04X", rel)
	}

	mnemonic := inst.Type().Mnemonic
	operands := inst.Operands()
	switch inst.Opcode() {
	case BR, BR_IF:
		return mnemonic + " " + target(operands[0])
	case BR_TABLE:
		var sb strings.Builder
		sb.WriteString(mnemonic)
		sb.WriteString(fmt.Sprintf(" 0x%02x", operands[0]))
		for _, rel := range operands[1:] {
			sb.WriteByte(' ')
			sb.WriteString(target(rel))
		}
		return sb.String()
	default:
		return inst.String()
	}
}
