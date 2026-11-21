package transform

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
)

// row relates one guest opcode to the SSA operation that models it. The
// operation's width is not part of its Op: arg and res are the lanes of its
// first argument and first result for the int and float domains, and zero for
// every other operation, whose operand types do not select the opcode.
type row struct {
	code instr.Opcode
	op   ssa.Op
	cond ssa.Cond
	arg  ssa.Type
	res  ssa.Type
	// zero marks the opcode that compares its operand with zero: the SSA
	// operation carries the zero as a second, constant argument.
	zero bool
}

// key selects the opcode that performs an operation.
type key struct {
	op   ssa.Op
	cond ssa.Cond
	arg  ssa.Type
	res  ssa.Type
}

// rows lists every modelled opcode: the ones the native tier lowers, and so the
// only ones whose meaning SSA owns. An opcode absent from it stays OpExec. Where
// several opcodes share one operation, the key of the row decides: i1, i8, and
// i32 operands all perform the I32 opcode, and I32_EQZ is the one opcode with
// no row of its own in performers, since int.cmp eq against a zero constant is
// emitted as it.
var rows = func() []row {
	var out []row
	add := func(code instr.Opcode, op ssa.Op, cond ssa.Cond, arg, res ssa.Type) {
		out = append(out, row{code: code, op: op, cond: cond, arg: arg, res: res})
	}
	ints := func(i32, i64 instr.Opcode, op ssa.Op, cond ssa.Cond) {
		res32, res64 := ssa.TypeI32, ssa.TypeI64
		if op == ssa.OpIntCmp {
			res64 = ssa.TypeI32
		}
		add(i32, op, cond, ssa.TypeI32, res32)
		add(i64, op, cond, ssa.TypeI64, res64)
	}
	floats := func(f32, f64 instr.Opcode, op ssa.Op, cond ssa.Cond) {
		res32, res64 := ssa.TypeF32, ssa.TypeF64
		if op == ssa.OpFloatCmp {
			res32, res64 = ssa.TypeI32, ssa.TypeI32
		}
		add(f32, op, cond, ssa.TypeF32, res32)
		add(f64, op, cond, ssa.TypeF64, res64)
	}
	plain := func(code instr.Opcode, op ssa.Op, cond ssa.Cond) {
		add(code, op, cond, 0, 0)
	}

	ints(instr.I32_ADD, instr.I64_ADD, ssa.OpIntAdd, 0)
	ints(instr.I32_SUB, instr.I64_SUB, ssa.OpIntSub, 0)
	ints(instr.I32_MUL, instr.I64_MUL, ssa.OpIntMul, 0)
	ints(instr.I32_ADD_OVF_S, instr.I64_ADD_OVF_S, ssa.OpIntSAddOvf, 0)
	ints(instr.I32_SUB_OVF_S, instr.I64_SUB_OVF_S, ssa.OpIntSSubOvf, 0)
	ints(instr.I32_MUL_OVF_S, instr.I64_MUL_OVF_S, ssa.OpIntSMulOvf, 0)
	ints(instr.I32_ADD_OVF_U, instr.I64_ADD_OVF_U, ssa.OpIntUAddOvf, 0)
	ints(instr.I32_SUB_OVF_U, instr.I64_SUB_OVF_U, ssa.OpIntUSubOvf, 0)
	ints(instr.I32_MUL_OVF_U, instr.I64_MUL_OVF_U, ssa.OpIntUMulOvf, 0)
	ints(instr.I32_DIV_S, instr.I64_DIV_S, ssa.OpIntSDiv, 0)
	ints(instr.I32_DIV_U, instr.I64_DIV_U, ssa.OpIntUDiv, 0)
	ints(instr.I32_REM_S, instr.I64_REM_S, ssa.OpIntSRem, 0)
	ints(instr.I32_REM_U, instr.I64_REM_U, ssa.OpIntURem, 0)
	ints(instr.I32_AND, instr.I64_AND, ssa.OpIntAnd, 0)
	ints(instr.I32_OR, instr.I64_OR, ssa.OpIntOr, 0)
	ints(instr.I32_XOR, instr.I64_XOR, ssa.OpIntXor, 0)
	ints(instr.I32_SHL, instr.I64_SHL, ssa.OpIntShl, 0)
	ints(instr.I32_SHR_S, instr.I64_SHR_S, ssa.OpIntAShr, 0)
	ints(instr.I32_SHR_U, instr.I64_SHR_U, ssa.OpIntLShr, 0)
	ints(instr.I32_ROTL, instr.I64_ROTL, ssa.OpIntRotl, 0)
	ints(instr.I32_ROTR, instr.I64_ROTR, ssa.OpIntRotr, 0)
	ints(instr.I32_CLZ, instr.I64_CLZ, ssa.OpIntClz, 0)
	ints(instr.I32_CTZ, instr.I64_CTZ, ssa.OpIntCtz, 0)
	ints(instr.I32_POPCNT, instr.I64_POPCNT, ssa.OpIntPopcnt, 0)
	ints(instr.I32_EXTEND8_S, instr.I64_EXTEND8_S, ssa.OpIntSExt8, 0)
	ints(instr.I32_EXTEND16_S, instr.I64_EXTEND16_S, ssa.OpIntSExt16, 0)
	add(instr.I64_EXTEND32_S, ssa.OpIntSExt32, 0, ssa.TypeI64, ssa.TypeI64)

	ints(instr.I32_EQ, instr.I64_EQ, ssa.OpIntCmp, ssa.CondEQ)
	ints(instr.I32_NE, instr.I64_NE, ssa.OpIntCmp, ssa.CondNE)
	ints(instr.I32_LT_S, instr.I64_LT_S, ssa.OpIntCmp, ssa.CondSLT)
	ints(instr.I32_LE_S, instr.I64_LE_S, ssa.OpIntCmp, ssa.CondSLE)
	ints(instr.I32_GT_S, instr.I64_GT_S, ssa.OpIntCmp, ssa.CondSGT)
	ints(instr.I32_GE_S, instr.I64_GE_S, ssa.OpIntCmp, ssa.CondSGE)
	ints(instr.I32_LT_U, instr.I64_LT_U, ssa.OpIntCmp, ssa.CondULT)
	ints(instr.I32_LE_U, instr.I64_LE_U, ssa.OpIntCmp, ssa.CondULE)
	ints(instr.I32_GT_U, instr.I64_GT_U, ssa.OpIntCmp, ssa.CondUGT)
	ints(instr.I32_GE_U, instr.I64_GE_U, ssa.OpIntCmp, ssa.CondUGE)

	add(instr.I32_TO_I64_S, ssa.OpIntSExt, 0, ssa.TypeI32, ssa.TypeI64)
	add(instr.I32_TO_I64_U, ssa.OpIntZExt, 0, ssa.TypeI32, ssa.TypeI64)
	add(instr.I64_TO_I32, ssa.OpIntWrap, 0, ssa.TypeI64, ssa.TypeI32)
	add(instr.I32_TO_F32_S, ssa.OpIntSIToFP, 0, ssa.TypeI32, ssa.TypeF32)
	add(instr.I32_TO_F64_S, ssa.OpIntSIToFP, 0, ssa.TypeI32, ssa.TypeF64)
	add(instr.I64_TO_F32_S, ssa.OpIntSIToFP, 0, ssa.TypeI64, ssa.TypeF32)
	add(instr.I64_TO_F64_S, ssa.OpIntSIToFP, 0, ssa.TypeI64, ssa.TypeF64)
	add(instr.I32_TO_F32_U, ssa.OpIntUIToFP, 0, ssa.TypeI32, ssa.TypeF32)
	add(instr.I32_TO_F64_U, ssa.OpIntUIToFP, 0, ssa.TypeI32, ssa.TypeF64)
	add(instr.I64_TO_F32_U, ssa.OpIntUIToFP, 0, ssa.TypeI64, ssa.TypeF32)
	add(instr.I64_TO_F64_U, ssa.OpIntUIToFP, 0, ssa.TypeI64, ssa.TypeF64)
	add(instr.I32_REINTERPRET_F32, ssa.OpIntBitcast, 0, ssa.TypeF32, ssa.TypeI32)
	add(instr.I64_REINTERPRET_F64, ssa.OpIntBitcast, 0, ssa.TypeF64, ssa.TypeI64)

	floats(instr.F32_ADD, instr.F64_ADD, ssa.OpFloatAdd, 0)
	floats(instr.F32_SUB, instr.F64_SUB, ssa.OpFloatSub, 0)
	floats(instr.F32_MUL, instr.F64_MUL, ssa.OpFloatMul, 0)
	floats(instr.F32_DIV, instr.F64_DIV, ssa.OpFloatDiv, 0)
	floats(instr.F32_MIN, instr.F64_MIN, ssa.OpFloatMin, 0)
	floats(instr.F32_MAX, instr.F64_MAX, ssa.OpFloatMax, 0)
	floats(instr.F32_COPYSIGN, instr.F64_COPYSIGN, ssa.OpFloatCopysign, 0)
	floats(instr.F32_NEG, instr.F64_NEG, ssa.OpFloatNeg, 0)
	floats(instr.F32_ABS, instr.F64_ABS, ssa.OpFloatAbs, 0)
	floats(instr.F32_SQRT, instr.F64_SQRT, ssa.OpFloatSqrt, 0)
	floats(instr.F32_CEIL, instr.F64_CEIL, ssa.OpFloatCeil, 0)
	floats(instr.F32_FLOOR, instr.F64_FLOOR, ssa.OpFloatFloor, 0)
	floats(instr.F32_TRUNC, instr.F64_TRUNC, ssa.OpFloatTrunc, 0)
	floats(instr.F32_NEAREST, instr.F64_NEAREST, ssa.OpFloatNearest, 0)
	floats(instr.F32_EQ, instr.F64_EQ, ssa.OpFloatCmp, ssa.CondOEQ)
	floats(instr.F32_NE, instr.F64_NE, ssa.OpFloatCmp, ssa.CondUNE)
	floats(instr.F32_LT, instr.F64_LT, ssa.OpFloatCmp, ssa.CondOLT)
	floats(instr.F32_LE, instr.F64_LE, ssa.OpFloatCmp, ssa.CondOLE)
	floats(instr.F32_GT, instr.F64_GT, ssa.OpFloatCmp, ssa.CondOGT)
	floats(instr.F32_GE, instr.F64_GE, ssa.OpFloatCmp, ssa.CondOGE)

	add(instr.F32_TO_F64, ssa.OpFloatFPExt, 0, ssa.TypeF32, ssa.TypeF64)
	add(instr.F64_TO_F32, ssa.OpFloatFPTrunc, 0, ssa.TypeF64, ssa.TypeF32)
	add(instr.F32_TO_I32_S, ssa.OpFloatFPToSISat, 0, ssa.TypeF32, ssa.TypeI32)
	add(instr.F32_TO_I64_S, ssa.OpFloatFPToSISat, 0, ssa.TypeF32, ssa.TypeI64)
	add(instr.F64_TO_I32_S, ssa.OpFloatFPToSISat, 0, ssa.TypeF64, ssa.TypeI32)
	add(instr.F64_TO_I64_S, ssa.OpFloatFPToSISat, 0, ssa.TypeF64, ssa.TypeI64)
	add(instr.F32_TO_I32_U, ssa.OpFloatFPToUISat, 0, ssa.TypeF32, ssa.TypeI32)
	add(instr.F32_TO_I64_U, ssa.OpFloatFPToUISat, 0, ssa.TypeF32, ssa.TypeI64)
	add(instr.F64_TO_I32_U, ssa.OpFloatFPToUISat, 0, ssa.TypeF64, ssa.TypeI32)
	add(instr.F64_TO_I64_U, ssa.OpFloatFPToUISat, 0, ssa.TypeF64, ssa.TypeI64)
	add(instr.F32_REINTERPRET_I32, ssa.OpFloatBitcast, 0, ssa.TypeI32, ssa.TypeF32)
	add(instr.F64_REINTERPRET_I64, ssa.OpFloatBitcast, 0, ssa.TypeI64, ssa.TypeF64)

	plain(instr.SELECT, ssa.OpSelect, 0)
	plain(instr.CALL, ssa.OpCall, 0)
	plain(instr.REF_IS_NULL, ssa.OpRefIsNull, 0)
	plain(instr.REF_TEST, ssa.OpRefTest, 0)
	plain(instr.REF_CAST, ssa.OpRefCast, 0)
	plain(instr.REF_EQ, ssa.OpRefCmp, ssa.CondEQ)
	plain(instr.REF_NE, ssa.OpRefCmp, ssa.CondNE)
	plain(instr.STRING_LEN, ssa.OpStringLen, 0)
	plain(instr.STRING_EQ, ssa.OpStringCmp, ssa.CondEQ)
	plain(instr.STRING_NE, ssa.OpStringCmp, ssa.CondNE)
	plain(instr.STRING_LT, ssa.OpStringCmp, ssa.CondULT)
	plain(instr.STRING_LE, ssa.OpStringCmp, ssa.CondULE)
	plain(instr.STRING_GT, ssa.OpStringCmp, ssa.CondUGT)
	plain(instr.STRING_GE, ssa.OpStringCmp, ssa.CondUGE)
	plain(instr.ARRAY_NEW_DEFAULT, ssa.OpArrayNewDefault, 0)
	plain(instr.ARRAY_GET, ssa.OpArrayGet, 0)
	plain(instr.ARRAY_SET, ssa.OpArraySet, 0)
	plain(instr.ARRAY_LEN, ssa.OpArrayLen, 0)
	plain(instr.STRUCT_NEW_DEFAULT, ssa.OpStructNewDefault, 0)
	plain(instr.STRUCT_GET, ssa.OpStructGet, 0)
	plain(instr.STRUCT_SET, ssa.OpStructSet, 0)
	plain(instr.MAP_LEN, ssa.OpMapLen, 0)
	plain(instr.MAP_GET, ssa.OpMapGet, 0)
	plain(instr.MAP_LOOKUP, ssa.OpMapLookup, 0)
	plain(instr.MAP_SET, ssa.OpMapSet, 0)
	plain(instr.MAP_DELETE, ssa.OpMapDelete, 0)

	out = append(out,
		row{code: instr.I32_EQZ, op: ssa.OpIntCmp, cond: ssa.CondEQ, arg: ssa.TypeI32, res: ssa.TypeI32, zero: true},
		row{code: instr.I64_EQZ, op: ssa.OpIntCmp, cond: ssa.CondEQ, arg: ssa.TypeI64, res: ssa.TypeI32, zero: true},
	)
	return out
}()

// models finds the row of a modelled opcode; performers the row that performs
// the operations with a key, never one of the zero rows, which share their key
// with the plain compare.
var (
	models = func() map[instr.Opcode]row {
		m := make(map[instr.Opcode]row, len(rows))
		for _, r := range rows {
			m[r.code] = r
		}
		return m
	}()
	performers = func() map[key]row {
		m := make(map[key]row, len(rows))
		for _, r := range rows {
			if !r.zero {
				m[key{r.op, r.cond, r.arg, r.res}] = r
			}
		}
		return m
	}()
)

// Performs reports whether the guest opcode code performs operation o of f: the
// opcode o names for OpExec, else the one its row models, with the operand
// count of the opcode. The zero compare, whose operation carries its zero as an
// argument, is performed by no opcode but its own, so it never bridges.
func Performs(f *ssa.Function, o ssa.Operation, code instr.Opcode) bool {
	if o.Op == ssa.OpExec {
		return o.Code == code
	}
	r, ok := models[code]
	if !ok || r.zero || r.op != o.Op || r.cond != o.Cond {
		return false
	}
	if pop := instr.TypeOf(code).Pop; pop != nil && len(pop) != len(o.Args) {
		return false
	}
	return keyOf(f, o) == key{r.op, r.cond, r.arg, r.res}
}

// codeOf returns the guest opcode that performs operation o of f: its own for
// OpExec, else the row it keys, which has no operand to carry.
func codeOf(f *ssa.Function, o ssa.Operation) (instr.Opcode, bool) {
	if o.Op == ssa.OpExec {
		return o.Code, true
	}
	r, ok := performers[keyOf(f, o)]
	return r.code, ok
}

// keyOf returns the key operation o is looked up by.
func keyOf(f *ssa.Function, o ssa.Operation) key {
	k := key{op: o.Op, cond: o.Cond}
	if o.Op >= ssa.OpIntAdd && o.Op <= ssa.OpFloatBitcast && len(o.Args) > 0 && len(o.Results) > 0 {
		k.arg, k.res = f.Type(o.Args[0]).Lane(), f.Type(o.Results[0]).Lane()
	}
	return k
}

// invert returns the opcode that computes the negation of the compare code, when
// one exists: an integer compare always has one, and a float compare only
// for equality, since the bytecode has no unordered form of the others.
func invert(code instr.Opcode) (instr.Opcode, bool) {
	r, ok := models[code]
	if !ok || r.zero || r.cond == 0 || r.op == ssa.OpRefCmp || r.op == ssa.OpStringCmp {
		return 0, false
	}
	inverse, ok := performers[key{r.op, r.cond.Invert(), r.arg, r.res}]
	return inverse.code, ok
}
