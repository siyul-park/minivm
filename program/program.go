package program

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

type Program struct {
	Code []byte
}

func New(instrs ...instr.Instruction) *Program {
	var code []byte
	for _, inst := range instrs {
		code = append(code, inst...)
	}
	return &Program{Code: code}
}

func (p *Program) Instruction(ip int) instr.Instruction {
	if ip < 0 || ip >= len(p.Code) {
		return nil
	}
	op := instr.Opcode(p.Code[ip])
	typ := instr.TypeOf(op)
	size := typ.Size()
	if ip+size > len(p.Code) {
		return nil
	}
	return p.Code[ip : ip+size]
}

func (p *Program) Instructions() []instr.Instruction {
	instrs := make([]instr.Instruction, 0, len(p.Code)/2)
	for ip := 0; ip < len(p.Code); {
		inst := p.Instruction(ip)
		if inst == nil {
			break
		}
		instrs = append(instrs, inst)
		ip += len(inst)
	}
	return instrs
}

func (p *Program) String() string {
	var sb strings.Builder
	ip := 0
	for ip < len(p.Code) {
		inst := p.Instruction(ip)
		if inst == nil {
			sb.WriteString(fmt.Sprintf("%04d: <invalid>\n", ip))
			break
		}
		sb.WriteString(fmt.Sprintf("%04d: %s\n", ip, inst.String()))
		ip += len(inst)
	}
	return sb.String()
}

func (p *Program) Size() int {
	return len(p.Code)
}
