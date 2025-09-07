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

	BR:    {"br", []int{4}},
	BR_IF: {"br_if", []int{4}},

	CALL:   {"call", []int{}},
	RETURN: {"return", []int{}},

	GLOBAL_GET: {"global.get", []int{4}},
	GLOBAL_SET: {"global.set", []int{4}},
	GLOBAL_TEE: {"global.tee", []int{4}},

	LOCAL_GET: {"local.get", []int{4}},
	LOCAL_SET: {"local.set", []int{4}},
	LOCAL_TEE: {"local.tee", []int{4}},

	FN_CONST: {"fn_const", []int{4}},

	I32_CONST: {"i32.const", []int{4}},
	I32_LOAD:  {"i32.load", []int{}},
	I32_STORE: {"i32.store", []int{}},

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

	I64_CONST: {"i64.const", []int{8}},
	I64_LOAD:  {"i64.load", []int{}},
	I64_STORE: {"i64.store", []int{}},

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

	F32_CONST: {"f32.const", []int{4}},
	F32_LOAD:  {"f32.load", []int{}},
	F32_STORE: {"f32.store", []int{}},

	F32_ADD:      {"f32.add", []int{}},
	F32_SUB:      {"f32.sub", []int{}},
	F32_MUL:      {"f32.mul", []int{}},
	F32_DIV:      {"f32.div", []int{}},
	F32_NEG:      {"f32.neg", []int{}},
	F32_ABS:      {"f32.abs", []int{}},
	F32_CEIL:     {"f32.ceil", []int{}},
	F32_FLOOR:    {"f32.floor", []int{}},
	F32_TRUNC:    {"f32.trunc", []int{}},
	F32_NEAREST:  {"f32.nearest", []int{}},
	F32_SQRT:     {"f32.sqrt", []int{}},
	F32_COPYSIGN: {"f32.copysign", []int{}},

	F32_EQ: {"f32.eq", []int{}},
	F32_NE: {"f32.ne", []int{}},
	F32_LT: {"f32.lt", []int{}},
	F32_GT: {"f32.gt", []int{}},
	F32_LE: {"f32.le", []int{}},
	F32_GE: {"f32.ge", []int{}},

	F64_CONST: {"f64.const", []int{8}},
	F64_LOAD:  {"f64.load", []int{}},
	F64_STORE: {"f64.store", []int{}},

	F64_ADD:      {"f64.add", []int{}},
	F64_SUB:      {"f64.sub", []int{}},
	F64_MUL:      {"f64.mul", []int{}},
	F64_DIV:      {"f64.div", []int{}},
	F64_NEG:      {"f64.neg", []int{}},
	F64_ABS:      {"f64.abs", []int{}},
	F64_CEIL:     {"f64.ceil", []int{}},
	F64_FLOOR:    {"f64.floor", []int{}},
	F64_TRUNC:    {"f64.trunc", []int{}},
	F64_NEAREST:  {"f64.nearest", []int{}},
	F64_SQRT:     {"f64.sqrt", []int{}},
	F64_COPYSIGN: {"f64.copysign", []int{}},

	F64_EQ: {"f64.eq", []int{}},
	F64_NE: {"f64.ne", []int{}},
	F64_LT: {"f64.lt", []int{}},
	F64_GT: {"f64.gt", []int{}},
	F64_LE: {"f64.le", []int{}},
	F64_GE: {"f64.ge", []int{}},
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
