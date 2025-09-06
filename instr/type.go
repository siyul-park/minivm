package instr

type Type struct {
	Mnemonic string
	Widths   []int
}

var types = map[Opcode]Type{
	NOP: {"nop", []int{}},
}

func TypeOf(op Opcode) Type {
	if t, ok := types[op]; ok {
		return t
	}
	return Type{}
}

func (t *Type) Size() int {
	size := 1
	for _, w := range t.Widths {
		size += w
	}
	return size
}
