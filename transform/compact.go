package transform

import (
	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// CompactPass removes unused and duplicate constants and types.
type CompactPass struct{}

var _ pass.Pass[*program.Program] = (*CompactPass)(nil)

// NewCompactPass returns the pass.
func NewCompactPass() *CompactPass {
	return &CompactPass{}
}

// Run compacts a program's constant and type pools.
func (p *CompactPass) Run(_ *pass.Manager, prog *program.Program) (bool, error) {
	codes := [][]byte{prog.Code}
	for _, v := range prog.Constants {
		if function, ok := v.(*types.Function); ok {
			codes = append(codes, function.Code)
		}
	}

	constants := prog.Constants
	typs := prog.Types
	constantLen, typeLen := len(constants), len(typs)

	constUsed := make([]bool, len(constants))
	typeUsed := make([]bool, len(typs))
	for _, code := range codes {
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				constUsed[inst.Operand(0)] = true
			case instr.REF_TEST, instr.REF_CAST,
				instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT,
				instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT,
				instr.MAP_NEW, instr.MAP_NEW_DEFAULT:
				typeUsed[inst.Operand(0)] = true
			default:
			}
			ip += inst.Width()
		}
	}

	constIndex, constSize := compactValues(constants, constUsed)
	typeIndex, typesSize := compactTypes(typs, typeUsed)

	for i, v := range constIndex {
		if v >= 0 {
			constants[v] = constants[i]
		}
	}
	for i, v := range typeIndex {
		if v >= 0 {
			typs[v] = typs[i]
		}
	}

	constants = constants[:constSize]
	typs = typs[:typesSize]
	if len(constants) == 0 {
		constants = nil
	}
	if len(typs) == 0 {
		typs = nil
	}

	for _, code := range codes {
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				idx := inst.Operand(0)
				inst.SetOperand(0, uint64(constIndex[idx]))
			case instr.REF_TEST, instr.REF_CAST,
				instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT,
				instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT,
				instr.MAP_NEW, instr.MAP_NEW_DEFAULT:
				idx := inst.Operand(0)
				inst.SetOperand(0, uint64(typeIndex[idx]))
			default:
			}
			ip += inst.Width()
		}
	}

	prog.Constants = constants
	prog.Types = typs

	return constantLen == constSize && typeLen == typesSize, nil
}

func compactValues(items []types.Value, used []bool) ([]int, int) {
	index := make([]int, len(items))
	seen := make(map[types.Value]int, len(items))
	size := 0
	for i, v := range items {
		index[i] = -1
		if !used[i] {
			continue
		}
		if valueType := reflect.TypeOf(v); valueType != nil && !valueType.Comparable() {
			index[i] = size
			size++
			continue
		}
		if j, ok := seen[v]; ok {
			index[i] = j
			continue
		}
		seen[v] = size
		index[i] = size
		size++
	}
	return index, size
}

func compactTypes(items []types.Type, used []bool) ([]int, int) {
	index := make([]int, len(items))
	for i := range index {
		index[i] = -1
		if used[i] {
			index[i] = i
		}
	}

	for i := range items {
		if index[i] == -1 {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if used[j] && items[j].Equals(items[i]) {
				index[j] = index[i]
			}
		}
	}

	size := 0
	for i := range index {
		if index[i] == -1 {
			continue
		}
		if index[i] != i {
			index[i] = index[index[i]]
		} else {
			index[i] = size
			size++
		}
	}
	return index, size
}
