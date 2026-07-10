package transform

import (
	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type ConstantDeduplicationPass struct{}

var _ pass.Pass[*program.Program] = (*ConstantDeduplicationPass)(nil)

func NewConstantDeduplicationPass() *ConstantDeduplicationPass {
	return &ConstantDeduplicationPass{}
}

func (p *ConstantDeduplicationPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
	fns := functions(prog)

	constants := prog.Constants
	typs := prog.Types

	constUsed := make([]bool, len(constants))
	typeUsed := make([]bool, len(typs))
	for _, fn := range fns {
		code := fn.Code
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				constUsed[inst.Operand(0)] = true
			case instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT, instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT:
				typeUsed[inst.Operand(0)] = true
			default:
			}
			ip += inst.Width()
		}
	}

	constIndex, constSize := dedupValues(constants, constUsed)
	typeIndex, typesSize := dedupTypes(typs, typeUsed)

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

	for _, fn := range fns {
		code := fn.Code
		ip := 0
		for ip < len(code) {
			inst := instr.Instruction(code[ip:])
			switch inst.Opcode() {
			case instr.CONST_GET:
				idx := inst.Operand(0)
				inst.SetOperand(0, uint64(constIndex[idx]))
			case instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT, instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT:
				idx := inst.Operand(0)
				inst.SetOperand(0, uint64(typeIndex[idx]))
			default:
			}
			ip += inst.Width()
		}
	}

	prog.Constants = constants
	prog.Types = typs

	return pass.PreserveNone(), nil
}

// dedupValues builds a compaction index for constants: each used entry is
// renumbered into a dense range. Entries whose dynamic type supports ==
// collapse to one slot per distinct value via a single map pass; entries
// backed by an uncomparable dynamic type (e.g. an array or map value) keep
// their own slot, since inserting one into a Go map panics. Unused entries
// map to -1. Returns the index and compacted size.
func dedupValues(items []types.Value, used []bool) ([]int, int) {
	index := make([]int, len(items))
	seen := make(map[types.Value]int, len(items))
	size := 0
	for i, v := range items {
		index[i] = -1
		if !used[i] {
			continue
		}
		if typ := reflect.TypeOf(v); typ != nil && !typ.Comparable() {
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

// dedupTypes builds a compaction index for types: each used entry is
// renumbered into a dense range with structurally equal types (per
// types.Type.Equals) collapsed to one slot. Types have no cheap canonical key
// to use as a map key, so this stays a pairwise scan. Unused entries map to
// -1. Returns the index and compacted size.
func dedupTypes(items []types.Type, used []bool) ([]int, int) {
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
