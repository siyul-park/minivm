package instr

type Type struct {
	Mnemonic string
	Widths   []int
}

var types = map[Opcode]Type{
	NOP:         {"nop", []int{}},
	UNREACHABLE: {"unreachable", []int{}},

	I32_CONST: {"i32.const", []int{4}},
	I64_CONST: {"i64.const", []int{8}},
	F32_CONST: {"f32.const", []int{4}},
	F64_CONST: {"f64.const", []int{8}},
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
