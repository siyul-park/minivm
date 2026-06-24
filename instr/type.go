package instr

// Type is the static metadata of an opcode: its mnemonic, operand encoding
// widths, and the stack effect (operand kinds it pops and pushes). Pop is
// listed top-of-stack first; Push lists results bottom first so the last entry
// ends on top. A KindAny entry matches or yields any kind. When both Pop and
// Push are nil the opcode has no statically fixed effect (its effect depends on
// operands, constants, declared types, or the runtime stack) and a verifier
// must resolve it from context.
type Type struct {
	Mnemonic string
	Widths   []int
	Pop      []Kind
	Push     []Kind
}

var types = map[Opcode]Type{
	NOP:         {Mnemonic: "nop"},
	UNREACHABLE: {Mnemonic: "unreachable"},

	DROP: {Mnemonic: "drop", Pop: []Kind{KindAny}},
	DUP:  {Mnemonic: "dup"},
	SWAP: {Mnemonic: "swap"},

	BR:       {Mnemonic: "br", Widths: []int{2}},
	BR_IF:    {Mnemonic: "br_if", Widths: []int{2}, Pop: []Kind{KindI32}},
	BR_TABLE: {Mnemonic: "br_table", Widths: []int{-2, 2}, Pop: []Kind{KindI32}},

	SELECT: {Mnemonic: "select"},

	CALL:        {Mnemonic: "call"},
	RETURN:      {Mnemonic: "return"},
	RETURN_CALL: {Mnemonic: "return_call"},

	YIELD:      {Mnemonic: "yield", Pop: []Kind{KindAny}, Push: []Kind{KindAny}},
	RESUME:     {Mnemonic: "resume", Pop: []Kind{KindAny, KindRef}, Push: []Kind{KindRef}},
	CORO_DONE:  {Mnemonic: "coro.done", Pop: []Kind{KindRef}, Push: []Kind{KindI32}},
	CORO_VALUE: {Mnemonic: "coro.value", Pop: []Kind{KindRef}, Push: []Kind{KindAny}},

	GLOBAL_GET: {Mnemonic: "global.get", Widths: []int{2}, Push: []Kind{KindAny}},
	GLOBAL_SET: {Mnemonic: "global.set", Widths: []int{2}, Pop: []Kind{KindAny}},
	GLOBAL_TEE: {Mnemonic: "global.tee", Widths: []int{2}},

	LOCAL_GET: {Mnemonic: "local.get", Widths: []int{1}},
	LOCAL_SET: {Mnemonic: "local.set", Widths: []int{1}, Pop: []Kind{KindAny}},
	LOCAL_TEE: {Mnemonic: "local.tee", Widths: []int{1}},

	CONST_GET: {Mnemonic: "const.get", Widths: []int{2}},

	REF_NULL: {Mnemonic: "ref.null", Push: []Kind{KindRef}},

	REF_TEST: {Mnemonic: "ref.test", Widths: []int{2}, Pop: []Kind{KindAny}, Push: []Kind{KindI32}},
	REF_CAST: {Mnemonic: "ref.cast", Widths: []int{2}, Pop: []Kind{KindAny}, Push: []Kind{KindAny}},

	REF_IS_NULL: {Mnemonic: "ref.is_null", Pop: []Kind{KindRef}, Push: []Kind{KindI32}},
	REF_EQ:      {Mnemonic: "ref.eq", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},
	REF_NE:      {Mnemonic: "ref.ne", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},

	I32_CONST: {Mnemonic: "i32.const", Widths: []int{4}, Push: []Kind{KindI32}},

	I32_XOR: {Mnemonic: "i32.xor", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_AND: {Mnemonic: "i32.and", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_OR:  {Mnemonic: "i32.or", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},

	I32_CLZ:    {Mnemonic: "i32.clz", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
	I32_CTZ:    {Mnemonic: "i32.ctz", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
	I32_POPCNT: {Mnemonic: "i32.popcnt", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
	I32_ROTL:   {Mnemonic: "i32.rotl", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_ROTR:   {Mnemonic: "i32.rotr", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},

	I32_EXTEND8_S:  {Mnemonic: "i32.extend8_s", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
	I32_EXTEND16_S: {Mnemonic: "i32.extend16_s", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},

	I32_ADD:   {Mnemonic: "i32.add", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_SUB:   {Mnemonic: "i32.sub", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_MUL:   {Mnemonic: "i32.mul", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_DIV_S: {Mnemonic: "i32.div_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_DIV_U: {Mnemonic: "i32.div_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_REM_S: {Mnemonic: "i32.rem_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_REM_U: {Mnemonic: "i32.rem_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_SHL:   {Mnemonic: "i32.shl", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_SHR_S: {Mnemonic: "i32.shr_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_SHR_U: {Mnemonic: "i32.shr_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},

	I32_EQZ:  {Mnemonic: "i32.eqz", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
	I32_EQ:   {Mnemonic: "i32.eq", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_NE:   {Mnemonic: "i32.ne", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_LT_S: {Mnemonic: "i32.lt_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_LT_U: {Mnemonic: "i32.lt_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_GT_S: {Mnemonic: "i32.gt_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_GT_U: {Mnemonic: "i32.gt_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_LE_S: {Mnemonic: "i32.le_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_LE_U: {Mnemonic: "i32.le_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_GE_S: {Mnemonic: "i32.ge_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},
	I32_GE_U: {Mnemonic: "i32.ge_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI32}},

	I32_TO_I64_S: {Mnemonic: "i32.to_i64_s", Pop: []Kind{KindI32}, Push: []Kind{KindI64}},
	I32_TO_I64_U: {Mnemonic: "i32.to_i64_u", Pop: []Kind{KindI32}, Push: []Kind{KindI64}},
	I32_TO_F32_S: {Mnemonic: "i32.to_f32_s", Pop: []Kind{KindI32}, Push: []Kind{KindF32}},
	I32_TO_F32_U: {Mnemonic: "i32.to_f32_u", Pop: []Kind{KindI32}, Push: []Kind{KindF32}},
	I32_TO_F64_S: {Mnemonic: "i32.to_f64_s", Pop: []Kind{KindI32}, Push: []Kind{KindF64}},
	I32_TO_F64_U: {Mnemonic: "i32.to_f64_u", Pop: []Kind{KindI32}, Push: []Kind{KindF64}},

	I32_REINTERPRET_F32: {Mnemonic: "i32.reinterpret_f32", Pop: []Kind{KindF32}, Push: []Kind{KindI32}},

	I64_CONST: {Mnemonic: "i64.const", Widths: []int{8}, Push: []Kind{KindI64}},

	I64_ADD:   {Mnemonic: "i64.add", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_SUB:   {Mnemonic: "i64.sub", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_MUL:   {Mnemonic: "i64.mul", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_DIV_S: {Mnemonic: "i64.div_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_DIV_U: {Mnemonic: "i64.div_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_REM_S: {Mnemonic: "i64.rem_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_REM_U: {Mnemonic: "i64.rem_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_SHL:   {Mnemonic: "i64.shl", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_SHR_S: {Mnemonic: "i64.shr_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_SHR_U: {Mnemonic: "i64.shr_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},

	I64_XOR: {Mnemonic: "i64.xor", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_AND: {Mnemonic: "i64.and", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_OR:  {Mnemonic: "i64.or", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},

	I64_CLZ:    {Mnemonic: "i64.clz", Pop: []Kind{KindI64}, Push: []Kind{KindI64}},
	I64_CTZ:    {Mnemonic: "i64.ctz", Pop: []Kind{KindI64}, Push: []Kind{KindI64}},
	I64_POPCNT: {Mnemonic: "i64.popcnt", Pop: []Kind{KindI64}, Push: []Kind{KindI64}},
	I64_ROTL:   {Mnemonic: "i64.rotl", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},
	I64_ROTR:   {Mnemonic: "i64.rotr", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI64}},

	I64_EXTEND8_S:  {Mnemonic: "i64.extend8_s", Pop: []Kind{KindI64}, Push: []Kind{KindI64}},
	I64_EXTEND16_S: {Mnemonic: "i64.extend16_s", Pop: []Kind{KindI64}, Push: []Kind{KindI64}},
	I64_EXTEND32_S: {Mnemonic: "i64.extend32_s", Pop: []Kind{KindI64}, Push: []Kind{KindI64}},

	I64_EQZ:  {Mnemonic: "i64.eqz", Pop: []Kind{KindI64}, Push: []Kind{KindI32}},
	I64_EQ:   {Mnemonic: "i64.eq", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_NE:   {Mnemonic: "i64.ne", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_LT_S: {Mnemonic: "i64.lt_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_LT_U: {Mnemonic: "i64.lt_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_GT_S: {Mnemonic: "i64.gt_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_GT_U: {Mnemonic: "i64.gt_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_LE_S: {Mnemonic: "i64.le_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_LE_U: {Mnemonic: "i64.le_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_GE_S: {Mnemonic: "i64.ge_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},
	I64_GE_U: {Mnemonic: "i64.ge_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI32}},

	I64_TO_I32:   {Mnemonic: "i64.to_i32", Pop: []Kind{KindI64}, Push: []Kind{KindI32}},
	I64_TO_F32_S: {Mnemonic: "i64.to_f32_s", Pop: []Kind{KindI64}, Push: []Kind{KindF32}},
	I64_TO_F32_U: {Mnemonic: "i64.to_f32_u", Pop: []Kind{KindI64}, Push: []Kind{KindF32}},
	I64_TO_F64_S: {Mnemonic: "i64.to_f64_s", Pop: []Kind{KindI64}, Push: []Kind{KindF64}},
	I64_TO_F64_U: {Mnemonic: "i64.to_f64_u", Pop: []Kind{KindI64}, Push: []Kind{KindF64}},

	I64_REINTERPRET_F64: {Mnemonic: "i64.reinterpret_f64", Pop: []Kind{KindF64}, Push: []Kind{KindI64}},

	F32_CONST: {Mnemonic: "f32.const", Widths: []int{4}, Push: []Kind{KindF32}},

	F32_ADD: {Mnemonic: "f32.add", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},
	F32_SUB: {Mnemonic: "f32.sub", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},
	F32_MUL: {Mnemonic: "f32.mul", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},
	F32_DIV: {Mnemonic: "f32.div", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},

	F32_ABS:      {Mnemonic: "f32.abs", Pop: []Kind{KindF32}, Push: []Kind{KindF32}},
	F32_NEG:      {Mnemonic: "f32.neg", Pop: []Kind{KindF32}, Push: []Kind{KindF32}},
	F32_SQRT:     {Mnemonic: "f32.sqrt", Pop: []Kind{KindF32}, Push: []Kind{KindF32}},
	F32_CEIL:     {Mnemonic: "f32.ceil", Pop: []Kind{KindF32}, Push: []Kind{KindF32}},
	F32_FLOOR:    {Mnemonic: "f32.floor", Pop: []Kind{KindF32}, Push: []Kind{KindF32}},
	F32_TRUNC:    {Mnemonic: "f32.trunc", Pop: []Kind{KindF32}, Push: []Kind{KindF32}},
	F32_NEAREST:  {Mnemonic: "f32.nearest", Pop: []Kind{KindF32}, Push: []Kind{KindF32}},
	F32_MIN:      {Mnemonic: "f32.min", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},
	F32_MAX:      {Mnemonic: "f32.max", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},
	F32_COPYSIGN: {Mnemonic: "f32.copysign", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},

	F32_EQ: {Mnemonic: "f32.eq", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI32}},
	F32_NE: {Mnemonic: "f32.ne", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI32}},
	F32_LT: {Mnemonic: "f32.lt", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI32}},
	F32_GT: {Mnemonic: "f32.gt", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI32}},
	F32_LE: {Mnemonic: "f32.le", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI32}},
	F32_GE: {Mnemonic: "f32.ge", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI32}},

	F32_TO_I32_S: {Mnemonic: "f32.to_i32_s", Pop: []Kind{KindF32}, Push: []Kind{KindI32}},
	F32_TO_I32_U: {Mnemonic: "f32.to_i32_u", Pop: []Kind{KindF32}, Push: []Kind{KindI32}},
	F32_TO_I64_S: {Mnemonic: "f32.to_i64_s", Pop: []Kind{KindF32}, Push: []Kind{KindI64}},
	F32_TO_I64_U: {Mnemonic: "f32.to_i64_u", Pop: []Kind{KindF32}, Push: []Kind{KindI64}},
	F32_TO_F64:   {Mnemonic: "f32.to_f64", Pop: []Kind{KindF32}, Push: []Kind{KindF64}},

	F32_REINTERPRET_I32: {Mnemonic: "f32.reinterpret_i32", Pop: []Kind{KindI32}, Push: []Kind{KindF32}},

	F64_CONST: {Mnemonic: "f64.const", Widths: []int{8}, Push: []Kind{KindF64}},

	F64_ADD: {Mnemonic: "f64.add", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},
	F64_SUB: {Mnemonic: "f64.sub", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},
	F64_MUL: {Mnemonic: "f64.mul", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},
	F64_DIV: {Mnemonic: "f64.div", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},

	F64_ABS:      {Mnemonic: "f64.abs", Pop: []Kind{KindF64}, Push: []Kind{KindF64}},
	F64_NEG:      {Mnemonic: "f64.neg", Pop: []Kind{KindF64}, Push: []Kind{KindF64}},
	F64_SQRT:     {Mnemonic: "f64.sqrt", Pop: []Kind{KindF64}, Push: []Kind{KindF64}},
	F64_CEIL:     {Mnemonic: "f64.ceil", Pop: []Kind{KindF64}, Push: []Kind{KindF64}},
	F64_FLOOR:    {Mnemonic: "f64.floor", Pop: []Kind{KindF64}, Push: []Kind{KindF64}},
	F64_TRUNC:    {Mnemonic: "f64.trunc", Pop: []Kind{KindF64}, Push: []Kind{KindF64}},
	F64_NEAREST:  {Mnemonic: "f64.nearest", Pop: []Kind{KindF64}, Push: []Kind{KindF64}},
	F64_MIN:      {Mnemonic: "f64.min", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},
	F64_MAX:      {Mnemonic: "f64.max", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},
	F64_COPYSIGN: {Mnemonic: "f64.copysign", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},

	F64_EQ: {Mnemonic: "f64.eq", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI32}},
	F64_NE: {Mnemonic: "f64.ne", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI32}},
	F64_LT: {Mnemonic: "f64.lt", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI32}},
	F64_GT: {Mnemonic: "f64.gt", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI32}},
	F64_LE: {Mnemonic: "f64.le", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI32}},
	F64_GE: {Mnemonic: "f64.ge", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI32}},

	F64_TO_I32_S: {Mnemonic: "f64.to_i32_s", Pop: []Kind{KindF64}, Push: []Kind{KindI32}},
	F64_TO_I32_U: {Mnemonic: "f64.to_i32_u", Pop: []Kind{KindF64}, Push: []Kind{KindI32}},
	F64_TO_I64_S: {Mnemonic: "f64.to_i64_s", Pop: []Kind{KindF64}, Push: []Kind{KindI64}},
	F64_TO_I64_U: {Mnemonic: "f64.to_i64_u", Pop: []Kind{KindF64}, Push: []Kind{KindI64}},
	F64_TO_F32:   {Mnemonic: "f64.to_f32", Pop: []Kind{KindF64}, Push: []Kind{KindF32}},

	F64_REINTERPRET_I64: {Mnemonic: "f64.reinterpret_i64", Pop: []Kind{KindI64}, Push: []Kind{KindF64}},

	STRING_NEW_UTF32: {Mnemonic: "string.new_utf32", Pop: []Kind{KindRef}, Push: []Kind{KindRef}},

	STRING_LEN:    {Mnemonic: "string.len", Pop: []Kind{KindRef}, Push: []Kind{KindI32}},
	STRING_CONCAT: {Mnemonic: "string.concat", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindRef}},

	STRING_EQ: {Mnemonic: "string.eq", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},
	STRING_NE: {Mnemonic: "string.ne", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},
	STRING_LT: {Mnemonic: "string.lt", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},
	STRING_GT: {Mnemonic: "string.gt", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},
	STRING_LE: {Mnemonic: "string.le", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},
	STRING_GE: {Mnemonic: "string.ge", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI32}},

	STRING_ENCODE_UTF32: {Mnemonic: "string.encode_utf32", Pop: []Kind{KindRef}, Push: []Kind{KindRef}},

	ARRAY_NEW:         {Mnemonic: "array.new", Widths: []int{2}, Pop: []Kind{KindI32, KindAny}, Push: []Kind{KindRef}},
	ARRAY_NEW_DEFAULT: {Mnemonic: "array.new_default", Widths: []int{2}, Pop: []Kind{KindI32}, Push: []Kind{KindRef}},

	ARRAY_GET:  {Mnemonic: "array.get", Pop: []Kind{KindI32, KindRef}, Push: []Kind{KindAny}},
	ARRAY_SET:  {Mnemonic: "array.set", Pop: []Kind{KindAny, KindI32, KindRef}},
	ARRAY_LEN:  {Mnemonic: "array.len", Pop: []Kind{KindRef}, Push: []Kind{KindI32}},
	ARRAY_FILL: {Mnemonic: "array.fill", Pop: []Kind{KindAny, KindI32, KindI32, KindRef}},
	ARRAY_COPY: {Mnemonic: "array.copy", Pop: []Kind{KindI32, KindI32, KindRef, KindI32, KindRef}},

	STRUCT_NEW:         {Mnemonic: "struct.new", Widths: []int{2}},
	STRUCT_NEW_DEFAULT: {Mnemonic: "struct.new_default", Widths: []int{2}, Push: []Kind{KindRef}},

	STRUCT_GET: {Mnemonic: "struct.get", Pop: []Kind{KindI32, KindRef}, Push: []Kind{KindAny}},
	STRUCT_SET: {Mnemonic: "struct.set", Pop: []Kind{KindAny, KindI32, KindRef}},

	MAP_NEW:         {Mnemonic: "map.new", Widths: []int{2}},
	MAP_NEW_DEFAULT: {Mnemonic: "map.new_default", Widths: []int{2}, Pop: []Kind{KindI32}, Push: []Kind{KindRef}},

	MAP_LEN:    {Mnemonic: "map.len", Pop: []Kind{KindRef}, Push: []Kind{KindI32}},
	MAP_GET:    {Mnemonic: "map.get", Pop: []Kind{KindAny, KindRef}, Push: []Kind{KindAny}},
	MAP_LOOKUP: {Mnemonic: "map.lookup", Pop: []Kind{KindAny, KindRef}, Push: []Kind{KindAny, KindI32}},
	MAP_SET:    {Mnemonic: "map.set", Pop: []Kind{KindAny, KindAny, KindRef}},
	MAP_DELETE: {Mnemonic: "map.delete", Pop: []Kind{KindAny, KindRef}},
	MAP_CLEAR:  {Mnemonic: "map.clear", Pop: []Kind{KindRef}},
	MAP_KEYS:   {Mnemonic: "map.keys", Pop: []Kind{KindRef}, Push: []Kind{KindRef}},
	MAP_ITER:   {Mnemonic: "map.iter", Pop: []Kind{KindRef}, Push: []Kind{KindRef}},

	REF_NEW: {Mnemonic: "ref.new", Pop: []Kind{KindAny}, Push: []Kind{KindRef}},
	REF_GET: {Mnemonic: "ref.get", Pop: []Kind{KindRef}, Push: []Kind{KindAny}},
	REF_SET: {Mnemonic: "ref.set", Pop: []Kind{KindAny, KindRef}},

	CLOSURE_NEW: {Mnemonic: "closure.new"},

	THROW: {Mnemonic: "throw", Pop: []Kind{KindAny}},

	ERROR_NEW: {Mnemonic: "error.new", Pop: []Kind{KindAny}, Push: []Kind{KindRef}},
	ERROR_GET: {Mnemonic: "error.get", Pop: []Kind{KindRef}, Push: []Kind{KindAny}},

	UPVAL_GET: {Mnemonic: "upval.get", Widths: []int{1}},
	UPVAL_SET: {Mnemonic: "upval.set", Widths: []int{1}, Pop: []Kind{KindAny}},

	EXT: {Mnemonic: "ext", Widths: []int{2, -8}},
}

func TypeOf(op Opcode) Type {
	if t, ok := types[op]; ok {
		return t
	}
	return Type{}
}

// Valid reports whether op is a defined opcode with encoding metadata.
func Valid(op Opcode) bool {
	_, ok := types[op]
	return ok
}
