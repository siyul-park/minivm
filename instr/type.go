package instr

type Type struct {
	Mnemonic string
	Widths   []int
}

var types = map[Opcode]Type{
	NOP:         {"nop", []int{}},
	UNREACHABLE: {"unreachable", []int{}},

	DROP: {"drop", []int{}},
	DUP:  {"dup", []int{}},
	SWAP: {"swap", []int{}},

	I32_CONST: {"i32.const", []int{4}},
	I32_ADD:   {"i32.add", []int{}},
	I32_SUB:   {"i32.sub", []int{}},
	I32_MUL:   {"i32.mul", []int{}},
	I32_DIV_S: {"i32.div_s", []int{}},
	I32_DIV_U: {"i32.div_u", []int{}},
	I32_REM_S: {"i32.rem_s", []int{}},
	I32_REM_U: {"i32.rem_u", []int{}},

	I64_CONST: {"i64.const", []int{8}},
	I64_ADD:   {"i64.add", []int{}},
	I64_SUB:   {"i64.sub", []int{}},
	I64_MUL:   {"i64.mul", []int{}},
	I64_DIV_S: {"i64.div_s", []int{}},
	I64_DIV_U: {"i64.div_u", []int{}},
	I64_REM_S: {"i64.rem_s", []int{}},
	I64_REM_U: {"i64.rem_u", []int{}},

	F32_CONST: {"f32.const", []int{4}},
	F32_ADD:   {"f32.add", []int{}},
	F32_SUB:   {"f32.sub", []int{}},
	F32_MUL:   {"f32.mul", []int{}},
	F32_DIV:   {"f32.div", []int{}},

	F64_CONST: {"f64.const", []int{8}},
	F64_ADD:   {"f64.add", []int{}},
	F64_SUB:   {"f64.sub", []int{}},
	F64_MUL:   {"f64.mul", []int{}},
	F64_DIV:   {"f64.div", []int{}},
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
