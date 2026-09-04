package instr

// Type is the static metadata of an opcode: its mnemonic, operand encoding
// widths, and the stack effect (operand kinds it pops and pushes). Pop is
// listed top-of-stack first; Push lists results bottom first so the last entry
// ends on top. A KindAny entry matches or yields any kind. When both Pop and
// Push are nil the opcode has no statically fixed effect (its effect depends on
// operands, constants, declared types, or the runtime stack) and a verifier
// must resolve it from context. Reads and Writes state what the opcode touches
// beyond that stack, and every opcode declares both.
//
// Boolean-producing opcodes (comparisons, *.eqz, ref.test/is_null/eq/ne, and
// the string comparisons) push KindI1 so the verifier tracks the boolean type;
// i1 shares the i32 representation, so the result is still usable wherever an
// i32 operand is expected.
type Type struct {
	Mnemonic string
	Widths   []int
	Pop      []Kind
	Push     []Kind
	Reads    Effect
	Writes   Effect
}

// Effect names one part of the machine an opcode touches beyond the operand
// stack, which Pop and Push already describe. Every opcode states its Reads and
// Writes, so an opcode that touches nothing says so by holding neither - and is
// pure by that fact rather than by a bit of its own.
type Effect uint8

const (
	// Local is the running frame's local slots.
	Local Effect = 1 << iota
	// Global is the module's globals.
	Global
	// Upval is the running closure's captured upvalues.
	Upval
	// Heap is the contents of a heap container. Allocating one writes Heap;
	// overwriting the contents of one both reads and writes it, which is where
	// a replaced reference is released.
	Heap
	// Frame is the call frame chain: an opcode that writes it enters a
	// function named on the operand stack, and the interpreter pushes a frame
	// the callee's return tears down. Resuming a suspended coroutine is not
	// one, because no frame is pushed for it.
	Frame
	// Branch is the instruction pointer, moved other than by falling through.
	Branch
)

var types = map[Opcode]Type{
	NOP:         {Mnemonic: "nop"},
	UNREACHABLE: {Mnemonic: "unreachable", Writes: Branch},

	DROP: {Mnemonic: "drop", Pop: []Kind{KindAny}},
	DUP:  {Mnemonic: "dup"},
	SWAP: {Mnemonic: "swap"},

	BR:       {Mnemonic: "br", Widths: []int{2}, Writes: Branch},
	BR_IF:    {Mnemonic: "br_if", Widths: []int{2}, Pop: []Kind{KindI32}, Writes: Branch},
	BR_TABLE: {Mnemonic: "br_table", Widths: []int{-2, 2}, Pop: []Kind{KindI32}, Writes: Branch},

	SELECT: {Mnemonic: "select", Pop: []Kind{KindI32, KindAny, KindAny}, Push: []Kind{KindAny}},

	CALL:        {Mnemonic: "call", Reads: Global | Upval | Heap, Writes: Global | Upval | Heap | Frame | Branch},
	RETURN:      {Mnemonic: "return", Writes: Branch},
	RETURN_CALL: {Mnemonic: "return_call", Reads: Global | Upval | Heap, Writes: Global | Upval | Heap | Frame | Branch},

	YIELD:      {Mnemonic: "yield", Pop: []Kind{KindAny}, Push: []Kind{KindAny}, Writes: Branch},
	RESUME:     {Mnemonic: "resume", Pop: []Kind{KindAny, KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap | Branch},
	CORO_DONE:  {Mnemonic: "coro.done", Pop: []Kind{KindRef}, Push: []Kind{KindI1}, Reads: Heap},
	CORO_VALUE: {Mnemonic: "coro.value", Pop: []Kind{KindRef}, Push: []Kind{KindAny}, Reads: Heap},

	GLOBAL_GET: {Mnemonic: "global.get", Widths: []int{2}, Push: []Kind{KindAny}, Reads: Global},
	GLOBAL_SET: {Mnemonic: "global.set", Widths: []int{2}, Pop: []Kind{KindAny}, Writes: Global},
	GLOBAL_TEE: {Mnemonic: "global.tee", Widths: []int{2}, Writes: Global},

	LOCAL_GET: {Mnemonic: "local.get", Widths: []int{1}, Reads: Local},
	LOCAL_SET: {Mnemonic: "local.set", Widths: []int{1}, Pop: []Kind{KindAny}, Writes: Local},
	LOCAL_TEE: {Mnemonic: "local.tee", Widths: []int{1}, Writes: Local},

	CONST_GET: {Mnemonic: "const.get", Widths: []int{2}},

	REF_NULL: {Mnemonic: "ref.null", Push: []Kind{KindRef}},

	REF_TEST: {Mnemonic: "ref.test", Widths: []int{2}, Pop: []Kind{KindAny}, Push: []Kind{KindI1}, Reads: Heap},
	REF_CAST: {Mnemonic: "ref.cast", Widths: []int{2}, Pop: []Kind{KindAny}, Push: []Kind{KindAny}, Reads: Heap},

	REF_IS_NULL: {Mnemonic: "ref.is_null", Pop: []Kind{KindRef}, Push: []Kind{KindI1}},
	REF_EQ:      {Mnemonic: "ref.eq", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}},
	REF_NE:      {Mnemonic: "ref.ne", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}},

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

	I32_EQZ:  {Mnemonic: "i32.eqz", Pop: []Kind{KindI32}, Push: []Kind{KindI1}},
	I32_EQ:   {Mnemonic: "i32.eq", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_NE:   {Mnemonic: "i32.ne", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_LT_S: {Mnemonic: "i32.lt_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_LT_U: {Mnemonic: "i32.lt_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_GT_S: {Mnemonic: "i32.gt_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_GT_U: {Mnemonic: "i32.gt_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_LE_S: {Mnemonic: "i32.le_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_LE_U: {Mnemonic: "i32.le_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_GE_S: {Mnemonic: "i32.ge_s", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},
	I32_GE_U: {Mnemonic: "i32.ge_u", Pop: []Kind{KindI32, KindI32}, Push: []Kind{KindI1}},

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

	I64_EQZ:  {Mnemonic: "i64.eqz", Pop: []Kind{KindI64}, Push: []Kind{KindI1}},
	I64_EQ:   {Mnemonic: "i64.eq", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_NE:   {Mnemonic: "i64.ne", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_LT_S: {Mnemonic: "i64.lt_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_LT_U: {Mnemonic: "i64.lt_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_GT_S: {Mnemonic: "i64.gt_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_GT_U: {Mnemonic: "i64.gt_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_LE_S: {Mnemonic: "i64.le_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_LE_U: {Mnemonic: "i64.le_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_GE_S: {Mnemonic: "i64.ge_s", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},
	I64_GE_U: {Mnemonic: "i64.ge_u", Pop: []Kind{KindI64, KindI64}, Push: []Kind{KindI1}},

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
	F32_REM: {Mnemonic: "f32.rem", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},
	F32_MOD: {Mnemonic: "f32.mod", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindF32}},

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

	F32_EQ: {Mnemonic: "f32.eq", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI1}},
	F32_NE: {Mnemonic: "f32.ne", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI1}},
	F32_LT: {Mnemonic: "f32.lt", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI1}},
	F32_GT: {Mnemonic: "f32.gt", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI1}},
	F32_LE: {Mnemonic: "f32.le", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI1}},
	F32_GE: {Mnemonic: "f32.ge", Pop: []Kind{KindF32, KindF32}, Push: []Kind{KindI1}},

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
	F64_REM: {Mnemonic: "f64.rem", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},
	F64_MOD: {Mnemonic: "f64.mod", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindF64}},

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

	F64_EQ: {Mnemonic: "f64.eq", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI1}},
	F64_NE: {Mnemonic: "f64.ne", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI1}},
	F64_LT: {Mnemonic: "f64.lt", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI1}},
	F64_GT: {Mnemonic: "f64.gt", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI1}},
	F64_LE: {Mnemonic: "f64.le", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI1}},
	F64_GE: {Mnemonic: "f64.ge", Pop: []Kind{KindF64, KindF64}, Push: []Kind{KindI1}},

	F64_TO_I32_S: {Mnemonic: "f64.to_i32_s", Pop: []Kind{KindF64}, Push: []Kind{KindI32}},
	F64_TO_I32_U: {Mnemonic: "f64.to_i32_u", Pop: []Kind{KindF64}, Push: []Kind{KindI32}},
	F64_TO_I64_S: {Mnemonic: "f64.to_i64_s", Pop: []Kind{KindF64}, Push: []Kind{KindI64}},
	F64_TO_I64_U: {Mnemonic: "f64.to_i64_u", Pop: []Kind{KindF64}, Push: []Kind{KindI64}},
	F64_TO_F32:   {Mnemonic: "f64.to_f32", Pop: []Kind{KindF64}, Push: []Kind{KindF32}},

	F64_REINTERPRET_I64: {Mnemonic: "f64.reinterpret_i64", Pop: []Kind{KindI64}, Push: []Kind{KindF64}},

	STRING_NEW_UTF32: {Mnemonic: "string.new_utf32", Pop: []Kind{KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap},

	STRING_LEN:    {Mnemonic: "string.len", Pop: []Kind{KindRef}, Push: []Kind{KindI32}, Reads: Heap},
	STRING_CONCAT: {Mnemonic: "string.concat", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap},

	STRING_EQ: {Mnemonic: "string.eq", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}, Reads: Heap},
	STRING_NE: {Mnemonic: "string.ne", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}, Reads: Heap},
	STRING_LT: {Mnemonic: "string.lt", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}, Reads: Heap},
	STRING_GT: {Mnemonic: "string.gt", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}, Reads: Heap},
	STRING_LE: {Mnemonic: "string.le", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}, Reads: Heap},
	STRING_GE: {Mnemonic: "string.ge", Pop: []Kind{KindRef, KindRef}, Push: []Kind{KindI1}, Reads: Heap},

	STRING_ENCODE_UTF32: {Mnemonic: "string.encode_utf32", Pop: []Kind{KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap},
	STRING_ITER:         {Mnemonic: "string.iter", Pop: []Kind{KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap},

	ARRAY_NEW:         {Mnemonic: "array.new", Widths: []int{2}, Pop: []Kind{KindI32, KindAny}, Push: []Kind{KindRef}, Writes: Heap},
	ARRAY_NEW_DEFAULT: {Mnemonic: "array.new_default", Widths: []int{2}, Pop: []Kind{KindI32}, Push: []Kind{KindRef}, Writes: Heap},

	ARRAY_LEN:    {Mnemonic: "array.len", Pop: []Kind{KindRef}, Push: []Kind{KindI32}, Reads: Heap},
	ARRAY_GET:    {Mnemonic: "array.get", Pop: []Kind{KindI32, KindRef}, Push: []Kind{KindAny}, Reads: Heap},
	ARRAY_SET:    {Mnemonic: "array.set", Pop: []Kind{KindAny, KindI32, KindRef}, Reads: Heap, Writes: Heap},
	ARRAY_FILL:   {Mnemonic: "array.fill", Pop: []Kind{KindI32, KindAny, KindI32, KindRef}, Reads: Heap, Writes: Heap},
	ARRAY_COPY:   {Mnemonic: "array.copy", Pop: []Kind{KindI32, KindI32, KindRef, KindI32, KindRef}, Reads: Heap, Writes: Heap},
	ARRAY_APPEND: {Mnemonic: "array.append", Reads: Heap, Writes: Heap},
	ARRAY_DELETE: {Mnemonic: "array.delete", Pop: []Kind{KindI32, KindRef}, Push: []Kind{KindAny}, Reads: Heap, Writes: Heap},
	ARRAY_SLICE:  {Mnemonic: "array.slice", Pop: []Kind{KindI32, KindI32, KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap},

	STRUCT_NEW:         {Mnemonic: "struct.new", Widths: []int{2}, Writes: Heap},
	STRUCT_NEW_DEFAULT: {Mnemonic: "struct.new_default", Widths: []int{2}, Push: []Kind{KindRef}, Writes: Heap},

	STRUCT_GET: {Mnemonic: "struct.get", Pop: []Kind{KindI32, KindRef}, Push: []Kind{KindAny}, Reads: Heap},
	STRUCT_SET: {Mnemonic: "struct.set", Pop: []Kind{KindAny, KindI32, KindRef}, Reads: Heap, Writes: Heap},

	MAP_NEW:         {Mnemonic: "map.new", Widths: []int{2}, Writes: Heap},
	MAP_NEW_DEFAULT: {Mnemonic: "map.new_default", Widths: []int{2}, Pop: []Kind{KindI32}, Push: []Kind{KindRef}, Writes: Heap},

	MAP_LEN:    {Mnemonic: "map.len", Pop: []Kind{KindRef}, Push: []Kind{KindI32}, Reads: Heap},
	MAP_GET:    {Mnemonic: "map.get", Pop: []Kind{KindAny, KindRef}, Push: []Kind{KindAny}, Reads: Heap},
	MAP_LOOKUP: {Mnemonic: "map.lookup", Pop: []Kind{KindAny, KindRef}, Push: []Kind{KindAny, KindI1}, Reads: Heap},
	MAP_SET:    {Mnemonic: "map.set", Pop: []Kind{KindAny, KindAny, KindRef}, Reads: Heap, Writes: Heap},
	MAP_DELETE: {Mnemonic: "map.delete", Pop: []Kind{KindAny, KindRef}, Reads: Heap, Writes: Heap},
	MAP_CLEAR:  {Mnemonic: "map.clear", Pop: []Kind{KindRef}, Reads: Heap, Writes: Heap},
	MAP_KEYS:   {Mnemonic: "map.keys", Pop: []Kind{KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap},
	MAP_ITER:   {Mnemonic: "map.iter", Pop: []Kind{KindRef}, Push: []Kind{KindRef}, Reads: Heap, Writes: Heap},

	REF_NEW: {Mnemonic: "ref.new", Pop: []Kind{KindAny}, Push: []Kind{KindRef}, Writes: Heap},
	REF_GET: {Mnemonic: "ref.get", Pop: []Kind{KindRef}, Push: []Kind{KindAny}, Reads: Heap},
	REF_SET: {Mnemonic: "ref.set", Pop: []Kind{KindAny, KindRef}, Reads: Heap, Writes: Heap},

	CLOSURE_NEW: {Mnemonic: "closure.new", Writes: Heap},

	THROW: {Mnemonic: "throw", Pop: []Kind{KindAny}, Writes: Branch},

	ERROR_NEW:  {Mnemonic: "error.new", Pop: []Kind{KindI32, KindAny}, Push: []Kind{KindRef}, Writes: Heap},
	ERROR_GET:  {Mnemonic: "error.get", Pop: []Kind{KindRef}, Push: []Kind{KindAny}, Reads: Heap},
	ERROR_CODE: {Mnemonic: "error.code", Pop: []Kind{KindRef}, Push: []Kind{KindI32}, Reads: Heap},

	UPVAL_GET: {Mnemonic: "upval.get", Widths: []int{1}, Reads: Upval},
	UPVAL_SET: {Mnemonic: "upval.set", Widths: []int{1}, Pop: []Kind{KindAny}, Writes: Upval},
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

// Reads reports whether op reads effect.
func (op Opcode) Reads(effect Effect) bool { return TypeOf(op).Reads&effect != 0 }

// Writes reports whether op writes effect.
func (op Opcode) Writes(effect Effect) bool { return TypeOf(op).Writes&effect != 0 }

// IsPure reports whether op computes its results from its operand values
// alone: it touches no other part of the machine and yields at least one
// value, so two occurrences with equal operands compute equal results and a
// consumer may number, fold, or reorder them.
func (op Opcode) IsPure() bool {
	t := TypeOf(op)
	return t.Reads == 0 && t.Writes == 0 && len(t.Push) > 0
}
