package verify

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// signature returns the fixed stack effect of op as the kinds it pops
// (top-of-stack first) and pushes (bottom first, so the last entry ends on
// top). ok is false for opcodes whose effect depends on operands, constants,
// declared types, or the runtime stack; those are resolved by checker.step.
func signature(op instr.Opcode) (pops, pushes []types.Kind, ok bool) {
	switch op {
	case instr.DROP:
		return consume(anyKind)
	case instr.BR_IF, instr.BR_TABLE:
		return consume(types.KindI32)
	case instr.THROW:
		return consume(anyKind)

	case instr.ERROR_NEW:
		return unary(anyKind, types.KindRef)
	case instr.ERROR_VALUE:
		return unary(types.KindRef, anyKind)

	case instr.YIELD:
		return unary(anyKind, anyKind)
	case instr.RESUME:
		return []types.Kind{anyKind, types.KindRef}, []types.Kind{types.KindRef}, true
	case instr.CORO_DONE:
		return unary(types.KindRef, types.KindI32)
	case instr.CORO_VALUE:
		return unary(types.KindRef, anyKind)

	case instr.GLOBAL_GET:
		return produce(anyKind)
	case instr.GLOBAL_SET, instr.LOCAL_SET, instr.UPVAL_SET:
		return consume(anyKind)

	case instr.REF_NULL:
		return produce(types.KindRef)
	case instr.REF_NEW:
		return unary(anyKind, types.KindRef)
	case instr.REF_GET:
		return unary(types.KindRef, anyKind)
	case instr.REF_SET:
		return consume(anyKind, types.KindRef)
	case instr.REF_TEST:
		return unary(anyKind, types.KindI32)
	case instr.REF_CAST:
		return unary(anyKind, anyKind)
	case instr.REF_IS_NULL:
		return unary(types.KindRef, types.KindI32)
	case instr.REF_EQ, instr.REF_NE:
		return compare(types.KindRef)

	case instr.I32_CONST:
		return produce(types.KindI32)
	case instr.I32_ADD, instr.I32_SUB, instr.I32_MUL,
		instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U,
		instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U,
		instr.I32_XOR, instr.I32_AND, instr.I32_OR, instr.I32_ROTL, instr.I32_ROTR,
		instr.I32_EQ, instr.I32_NE,
		instr.I32_LT_S, instr.I32_LT_U, instr.I32_GT_S, instr.I32_GT_U,
		instr.I32_LE_S, instr.I32_LE_U, instr.I32_GE_S, instr.I32_GE_U:
		return binary(types.KindI32)
	case instr.I32_CLZ, instr.I32_CTZ, instr.I32_POPCNT,
		instr.I32_EXTEND8_S, instr.I32_EXTEND16_S, instr.I32_EQZ:
		return unary(types.KindI32, types.KindI32)
	case instr.I32_TO_I64_S, instr.I32_TO_I64_U:
		return unary(types.KindI32, types.KindI64)
	case instr.I32_TO_F32_S, instr.I32_TO_F32_U:
		return unary(types.KindI32, types.KindF32)
	case instr.I32_TO_F64_S, instr.I32_TO_F64_U:
		return unary(types.KindI32, types.KindF64)
	case instr.I32_REINTERPRET_F32:
		return unary(types.KindF32, types.KindI32)

	case instr.I64_CONST:
		return produce(types.KindI64)
	case instr.I64_ADD, instr.I64_SUB, instr.I64_MUL,
		instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U,
		instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U,
		instr.I64_XOR, instr.I64_AND, instr.I64_OR, instr.I64_ROTL, instr.I64_ROTR:
		return binary(types.KindI64)
	case instr.I64_CLZ, instr.I64_CTZ, instr.I64_POPCNT,
		instr.I64_EXTEND8_S, instr.I64_EXTEND16_S, instr.I64_EXTEND32_S:
		return unary(types.KindI64, types.KindI64)
	case instr.I64_EQZ:
		return unary(types.KindI64, types.KindI32)
	case instr.I64_EQ, instr.I64_NE,
		instr.I64_LT_S, instr.I64_LT_U, instr.I64_GT_S, instr.I64_GT_U,
		instr.I64_LE_S, instr.I64_LE_U, instr.I64_GE_S, instr.I64_GE_U:
		return compare(types.KindI64)
	case instr.I64_TO_I32:
		return unary(types.KindI64, types.KindI32)
	case instr.I64_TO_F32_S, instr.I64_TO_F32_U:
		return unary(types.KindI64, types.KindF32)
	case instr.I64_TO_F64_S, instr.I64_TO_F64_U:
		return unary(types.KindI64, types.KindF64)
	case instr.I64_REINTERPRET_F64:
		return unary(types.KindF64, types.KindI64)

	case instr.F32_CONST:
		return produce(types.KindF32)
	case instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV,
		instr.F32_MIN, instr.F32_MAX, instr.F32_COPYSIGN:
		return binary(types.KindF32)
	case instr.F32_ABS, instr.F32_NEG, instr.F32_SQRT,
		instr.F32_CEIL, instr.F32_FLOOR, instr.F32_TRUNC, instr.F32_NEAREST:
		return unary(types.KindF32, types.KindF32)
	case instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_GT, instr.F32_LE, instr.F32_GE:
		return compare(types.KindF32)
	case instr.F32_TO_I32_S, instr.F32_TO_I32_U:
		return unary(types.KindF32, types.KindI32)
	case instr.F32_TO_I64_S, instr.F32_TO_I64_U:
		return unary(types.KindF32, types.KindI64)
	case instr.F32_TO_F64:
		return unary(types.KindF32, types.KindF64)
	case instr.F32_REINTERPRET_I32:
		return unary(types.KindI32, types.KindF32)

	case instr.F64_CONST:
		return produce(types.KindF64)
	case instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV,
		instr.F64_MIN, instr.F64_MAX, instr.F64_COPYSIGN:
		return binary(types.KindF64)
	case instr.F64_ABS, instr.F64_NEG, instr.F64_SQRT,
		instr.F64_CEIL, instr.F64_FLOOR, instr.F64_TRUNC, instr.F64_NEAREST:
		return unary(types.KindF64, types.KindF64)
	case instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_GT, instr.F64_LE, instr.F64_GE:
		return compare(types.KindF64)
	case instr.F64_TO_I32_S, instr.F64_TO_I32_U:
		return unary(types.KindF64, types.KindI32)
	case instr.F64_TO_I64_S, instr.F64_TO_I64_U:
		return unary(types.KindF64, types.KindI64)
	case instr.F64_TO_F32:
		return unary(types.KindF64, types.KindF32)
	case instr.F64_REINTERPRET_I64:
		return unary(types.KindI64, types.KindF64)

	case instr.STRING_NEW_UTF32, instr.STRING_ENCODE_UTF32:
		return unary(types.KindRef, types.KindRef)
	case instr.STRING_LEN:
		return unary(types.KindRef, types.KindI32)
	case instr.STRING_CONCAT:
		return binary(types.KindRef)
	case instr.STRING_EQ, instr.STRING_NE,
		instr.STRING_LT, instr.STRING_GT, instr.STRING_LE, instr.STRING_GE:
		return compare(types.KindRef)

	case instr.ARRAY_NEW:
		return []types.Kind{types.KindI32, anyKind}, []types.Kind{types.KindRef}, true
	case instr.ARRAY_NEW_DEFAULT:
		return unary(types.KindI32, types.KindRef)
	case instr.ARRAY_LEN:
		return unary(types.KindRef, types.KindI32)
	case instr.ARRAY_GET:
		return []types.Kind{types.KindI32, types.KindRef}, []types.Kind{anyKind}, true
	case instr.ARRAY_SET:
		return consume(anyKind, types.KindI32, types.KindRef)
	case instr.ARRAY_FILL:
		return consume(anyKind, types.KindI32, types.KindI32, types.KindRef)
	case instr.ARRAY_COPY:
		return consume(types.KindI32, types.KindI32, types.KindRef, types.KindI32, types.KindRef)

	case instr.STRUCT_NEW_DEFAULT:
		return produce(types.KindRef)
	case instr.STRUCT_GET:
		return []types.Kind{types.KindI32, types.KindRef}, []types.Kind{anyKind}, true
	case instr.STRUCT_SET:
		return consume(anyKind, types.KindI32, types.KindRef)

	case instr.MAP_NEW_DEFAULT:
		return unary(types.KindI32, types.KindRef)
	case instr.MAP_LEN:
		return unary(types.KindRef, types.KindI32)
	case instr.MAP_GET:
		return []types.Kind{anyKind, types.KindRef}, []types.Kind{anyKind}, true
	case instr.MAP_LOOKUP:
		return []types.Kind{anyKind, types.KindRef}, []types.Kind{anyKind, types.KindI32}, true
	case instr.MAP_SET:
		return consume(anyKind, anyKind, types.KindRef)
	case instr.MAP_DELETE:
		return consume(anyKind, types.KindRef)
	case instr.MAP_CLEAR:
		return consume(types.KindRef)
	case instr.MAP_KEYS, instr.MAP_ITER:
		return unary(types.KindRef, types.KindRef)

	default:
		return nil, nil, false
	}
}

func produce(k types.Kind) ([]types.Kind, []types.Kind, bool) {
	return nil, []types.Kind{k}, true
}

func consume(ks ...types.Kind) ([]types.Kind, []types.Kind, bool) {
	return ks, nil, true
}

func unary(in, out types.Kind) ([]types.Kind, []types.Kind, bool) {
	return []types.Kind{in}, []types.Kind{out}, true
}

func binary(k types.Kind) ([]types.Kind, []types.Kind, bool) {
	return []types.Kind{k, k}, []types.Kind{k}, true
}

func compare(k types.Kind) ([]types.Kind, []types.Kind, bool) {
	return []types.Kind{k, k}, []types.Kind{types.KindI32}, true
}
