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

	BR:    {"br", []int{2}},
	BR_IF: {"br_if", []int{2}},

	CALL:   {"call", []int{}},
	RETURN: {"return", []int{}},

	GLOBAL_GET: {"global.get", []int{2}},
	GLOBAL_SET: {"global.set", []int{2}},

	LOCAL_GET: {"local.get", []int{1}},
	LOCAL_SET: {"local.set", []int{1}},

	CONST_GET: {"const.get", []int{2}},

	RTT_CANON: {"rtt.canon", []int{2}},

	I32_CONST: {"i32.const", []int{4}},

	I32_XOR: {"i32.xor", []int{}},
	I32_AND: {"i32.and", []int{}},
	I32_OR:  {"i32.or", []int{}},

	I32_ADD:   {"i32.add", []int{}},
	I32_SUB:   {"i32.sub", []int{}},
	I32_MUL:   {"i32.mul", []int{}},
	I32_DIV_S: {"i32.div_s", []int{}},
	I32_DIV_U: {"i32.div_u", []int{}},
	I32_REM_S: {"i32.rem_s", []int{}},
	I32_REM_U: {"i32.rem_u", []int{}},
	I32_SHL:   {"i32.shl", []int{}},
	I32_SHR_S: {"i32.shr_s", []int{}},
	I32_SHR_U: {"i32.shr_u", []int{}},

	I32_EQ:   {"i32.eq", []int{}},
	I32_NE:   {"i32.ne", []int{}},
	I32_LT_S: {"i32.lt_s", []int{}},
	I32_LT_U: {"i32.lt_u", []int{}},
	I32_GT_S: {"i32.gt_s", []int{}},
	I32_GT_U: {"i32.gt_u", []int{}},
	I32_LE_S: {"i32.le_s", []int{}},
	I32_LE_U: {"i32.le_u", []int{}},
	I32_GE_S: {"i32.ge_s", []int{}},
	I32_GE_U: {"i32.ge_u", []int{}},

	I32_TO_I64_S: {"i32.to_i64_s", []int{}},
	I32_TO_I64_U: {"i32.to_i64_u", []int{}},
	I32_TO_F32_S: {"i32.to_f32_s", []int{}},
	I32_TO_F32_U: {"i32.to_f32_u", []int{}},
	I32_TO_F64_S: {"i32.to_f64_s", []int{}},
	I32_TO_F64_U: {"i32.to_f64_u", []int{}},

	I64_CONST: {"i64.const", []int{8}},

	I64_ADD:   {"i64.add", []int{}},
	I64_SUB:   {"i64.sub", []int{}},
	I64_MUL:   {"i64.mul", []int{}},
	I64_DIV_S: {"i64.div_s", []int{}},
	I64_DIV_U: {"i64.div_u", []int{}},
	I64_REM_S: {"i64.rem_s", []int{}},
	I64_REM_U: {"i64.rem_u", []int{}},
	I64_SHL:   {"i64.shl", []int{}},
	I64_SHR_S: {"i64.shr_s", []int{}},
	I64_SHR_U: {"i64.shr_u", []int{}},

	I64_EQ:   {"i64.eq", []int{}},
	I64_NE:   {"i64.ne", []int{}},
	I64_LT_S: {"i64.lt_s", []int{}},
	I64_LT_U: {"i64.lt_u", []int{}},
	I64_GT_S: {"i64.gt_s", []int{}},
	I64_GT_U: {"i64.gt_u", []int{}},
	I64_LE_S: {"i64.le_s", []int{}},
	I64_LE_U: {"i64.le_u", []int{}},
	I64_GE_S: {"i64.ge_s", []int{}},
	I64_GE_U: {"i64.ge_u", []int{}},

	I64_TO_I32:   {"i64.to_i32", []int{}},
	I64_TO_F32_S: {"i64.to_f32_s", []int{}},
	I64_TO_F32_U: {"i64.to_f32_u", []int{}},
	I64_TO_F64_S: {"i64.to_f64_s", []int{}},
	I64_TO_F64_U: {"i64.to_f64_u", []int{}},

	F32_CONST: {"f32.const", []int{4}},

	F32_ADD: {"f32.add", []int{}},
	F32_SUB: {"f32.sub", []int{}},
	F32_MUL: {"f32.mul", []int{}},
	F32_DIV: {"f32.div", []int{}},

	F32_EQ: {"f32.eq", []int{}},
	F32_NE: {"f32.ne", []int{}},
	F32_LT: {"f32.lt", []int{}},
	F32_GT: {"f32.gt", []int{}},
	F32_LE: {"f32.le", []int{}},
	F32_GE: {"f32.ge", []int{}},

	F32_TO_I32_S: {"f32.to_i32_s", []int{}},
	F32_TO_I32_U: {"f32.to_i32_u", []int{}},
	F32_TO_I64_S: {"f32.to_i64_s", []int{}},
	F32_TO_I64_U: {"f32.to_i64_u", []int{}},
	F32_TO_F64:   {"f32.to_f64", []int{}},

	F64_CONST: {"f64.const", []int{8}},

	F64_ADD: {"f64.add", []int{}},
	F64_SUB: {"f64.sub", []int{}},
	F64_MUL: {"f64.mul", []int{}},
	F64_DIV: {"f64.div", []int{}},

	F64_EQ: {"f64.eq", []int{}},
	F64_NE: {"f64.ne", []int{}},
	F64_LT: {"f64.lt", []int{}},
	F64_GT: {"f64.gt", []int{}},
	F64_LE: {"f64.le", []int{}},
	F64_GE: {"f64.ge", []int{}},

	F64_TO_I32_S: {"f64.to_i32_s", []int{}},
	F64_TO_I32_U: {"f64.to_i32_u", []int{}},
	F64_TO_I64_S: {"f64.to_i64_s", []int{}},
	F64_TO_I64_U: {"f64.to_i64_u", []int{}},
	F64_TO_F32:   {"f64.to_f32", []int{}},

	STRING_LEN:    {"string.len", []int{}},
	STRING_CONCAT: {"string.concat", []int{}},

	STRING_EQ: {"string.eq", []int{}},
	STRING_NE: {"string.ne", []int{}},
	STRING_LT: {"string.lt", []int{}},
	STRING_GT: {"string.gt", []int{}},
	STRING_LE: {"string.le", []int{}},
	STRING_GE: {"string.ge", []int{}},

	ARRAY_NEW:         {"array.new", []int{}},
	ARRAY_NEW_DEFAULT: {"array.new_default", []int{}},

	ARRAY_GET:  {"array.get", []int{}},
	ARRAY_SET:  {"array.set", []int{}},
	ARRAY_LEN:  {"array.len", []int{}},
	ARRAY_FILL: {"array.fill", []int{}},
	ARRAY_COPY: {"array.copy", []int{}},

	STRUCT_NEW:         {"struct.new", []int{}},
	STRUCT_NEW_DEFAULT: {"struct.new_default", []int{}},

	STRUCT_GET: {"struct.get", []int{}},
	STRUCT_SET: {"struct.set", []int{}},
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
