package transform

import (
	"math"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// FoldPass folds a pure operation whose arguments are all compile-time
// constants into the constant its opcode computes: the SSA counterpart of
// transform.FoldPass. It runs over any function this package's four passes
// accept - one carrying JIT guards and deopt state or one with neither - since
// the gate is instr's own op.Code.IsPure(), which is already false for
// anything the JIT alone would add (a guard, a bridge, a retain or release are
// none of them OpExec) and for anything that touches Local, Global, Upval,
// Heap, Frame, or Branch. That leaves exactly the same closed family
// transform.FoldPass folds by hand for bytecode - integer and float
// arithmetic, comparisons, eqz, and the narrowing conversions - which this
// pass computes by decoding each argument's types.Boxed into the typed value
// (I32, I64, F32, F64 are Go's own numeric types under another name) and
// letting Go's native operators do the arithmetic, rather than re-deriving it
// from raw bits the way the bytecode pass must.
//
// A division or remainder whose divisor folds to zero is left as a runtime
// operation, matching transform.FoldPass: the interpreter's own trap for that
// case is not this pass's to pre-empt. An i64 arithmetic or shift result that
// does not survive types.BoxI64's NaN-boxed round trip is left unfolded too -
// every i64 OpConst the frontend builds is already round-trip-safe, so a
// result that is not would silently corrupt the value if boxed anyway.
type FoldPass struct{}

var _ pass.Pass[*ssa.Function] = (*FoldPass)(nil)

func NewFoldPass() *FoldPass {
	return &FoldPass{}
}

func (p *FoldPass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	consts := map[ssa.Value]types.Boxed{}
	changed := false

	for _, block := range order(fn) {
		ops := fn.Block(block).Ops
		for i := range ops {
			op := &ops[i]
			if op.Op == ssa.OpConst {
				consts[op.Results[0]] = op.Const
				continue
			}
			if op.Op != ssa.OpExec || !op.Code.IsPure() {
				continue
			}
			args := make([]types.Boxed, len(op.Args))
			known := true
			for j, a := range op.Args {
				v, ok := consts[a]
				if !ok {
					known = false
					break
				}
				args[j] = v
			}
			if !known {
				continue
			}
			result, ok := eval(op.Code, args)
			if !ok {
				continue
			}
			res := op.Results[0]
			*op = ssa.Operation{Op: ssa.OpConst, Const: result, Results: []ssa.Value{res}}
			consts[res] = result
			changed = true
		}
	}

	if !changed {
		return pass.PreserveAll(), nil
	}
	return pass.PreserveNone(), nil
}

// eval computes the constant Code performs over args, or reports false when
// Code is outside the folded family or the result cannot be folded safely.
func eval(code instr.Opcode, args []types.Boxed) (types.Boxed, bool) {
	switch code {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_MUL, instr.I32_DIV_S, instr.I32_DIV_U,
		instr.I32_REM_S, instr.I32_REM_U, instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U,
		instr.I32_XOR, instr.I32_AND, instr.I32_OR:
		return evalI32(code, args[0].I32(), args[1].I32())
	case instr.I32_EQ, instr.I32_NE, instr.I32_LT_S, instr.I32_LT_U, instr.I32_GT_S, instr.I32_GT_U,
		instr.I32_LE_S, instr.I32_LE_U, instr.I32_GE_S, instr.I32_GE_U:
		return evalI32Cmp(code, args[0].I32(), args[1].I32())
	case instr.I32_EQZ:
		return types.BoxI1(args[0].I32() == 0), true
	case instr.I32_TO_F32_S:
		return types.BoxF32(float32(args[0].I32())), true
	case instr.I32_TO_F32_U:
		return types.BoxF32(float32(uint32(args[0].I32()))), true

	case instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_DIV_S, instr.I64_DIV_U,
		instr.I64_REM_S, instr.I64_REM_U, instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U,
		instr.I64_XOR, instr.I64_AND, instr.I64_OR:
		return evalI64(code, args[0].I64(), args[1].I64())
	case instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_LT_U, instr.I64_GT_S, instr.I64_GT_U,
		instr.I64_LE_S, instr.I64_LE_U, instr.I64_GE_S, instr.I64_GE_U:
		return evalI64Cmp(code, args[0].I64(), args[1].I64())
	case instr.I64_EQZ:
		return types.BoxI1(args[0].I64() == 0), true
	case instr.I64_TO_I32:
		return types.BoxI32(int32(args[0].I64())), true
	case instr.I64_TO_F32_S:
		return types.BoxF32(float32(args[0].I64())), true
	case instr.I64_TO_F32_U:
		return types.BoxF32(float32(uint64(args[0].I64()))), true
	case instr.I64_TO_F64_S:
		return types.BoxF64(float64(args[0].I64())), true
	case instr.I64_TO_F64_U:
		return types.BoxF64(float64(uint64(args[0].I64()))), true

	case instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV, instr.F32_REM, instr.F32_MOD:
		return evalF32(code, args[0].F32(), args[1].F32())
	case instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_GT, instr.F32_LE, instr.F32_GE:
		return evalF32Cmp(code, args[0].F32(), args[1].F32())
	case instr.F32_TO_I32_S:
		return types.BoxI32(int32(args[0].F32())), true
	case instr.F32_TO_I32_U:
		return types.BoxI32(int32(uint32(args[0].F32()))), true

	case instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV, instr.F64_REM, instr.F64_MOD:
		return evalF64(code, args[0].F64(), args[1].F64())
	case instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_GT, instr.F64_LE, instr.F64_GE:
		return evalF64Cmp(code, args[0].F64(), args[1].F64())
	case instr.F64_TO_I32_S:
		return types.BoxI32(int32(args[0].F64())), true
	case instr.F64_TO_I32_U:
		return types.BoxI32(int32(uint32(args[0].F64()))), true
	case instr.F64_TO_I64_S:
		return i64(int64(args[0].F64()))
	case instr.F64_TO_I64_U:
		return i64(int64(uint64(args[0].F64())))
	case instr.F64_TO_F32:
		return types.BoxF32(float32(args[0].F64())), true

	default:
		return 0, false
	}
}

func evalI32(code instr.Opcode, a, b int32) (types.Boxed, bool) {
	switch code {
	case instr.I32_ADD:
		return types.BoxI32(a + b), true
	case instr.I32_SUB:
		return types.BoxI32(a - b), true
	case instr.I32_MUL:
		return types.BoxI32(a * b), true
	case instr.I32_DIV_S:
		if b == 0 {
			return 0, false
		}
		return types.BoxI32(a / b), true
	case instr.I32_DIV_U:
		if b == 0 {
			return 0, false
		}
		return types.BoxI32(int32(uint32(a) / uint32(b))), true
	case instr.I32_REM_S:
		if b == 0 {
			return 0, false
		}
		return types.BoxI32(a % b), true
	case instr.I32_REM_U:
		if b == 0 {
			return 0, false
		}
		return types.BoxI32(int32(uint32(a) % uint32(b))), true
	case instr.I32_SHL:
		return types.BoxI32(a << (uint32(b) & 0x1F)), true
	case instr.I32_SHR_S:
		return types.BoxI32(a >> (uint32(b) & 0x1F)), true
	case instr.I32_SHR_U:
		return types.BoxI32(int32(uint32(a) >> (uint32(b) & 0x1F))), true
	case instr.I32_XOR:
		return types.BoxI32(a ^ b), true
	case instr.I32_AND:
		return types.BoxI32(a & b), true
	case instr.I32_OR:
		return types.BoxI32(a | b), true
	default:
		return 0, false
	}
}

func evalI32Cmp(code instr.Opcode, a, b int32) (types.Boxed, bool) {
	switch code {
	case instr.I32_EQ:
		return types.BoxI1(a == b), true
	case instr.I32_NE:
		return types.BoxI1(a != b), true
	case instr.I32_LT_S:
		return types.BoxI1(a < b), true
	case instr.I32_LT_U:
		return types.BoxI1(uint32(a) < uint32(b)), true
	case instr.I32_GT_S:
		return types.BoxI1(a > b), true
	case instr.I32_GT_U:
		return types.BoxI1(uint32(a) > uint32(b)), true
	case instr.I32_LE_S:
		return types.BoxI1(a <= b), true
	case instr.I32_LE_U:
		return types.BoxI1(uint32(a) <= uint32(b)), true
	case instr.I32_GE_S:
		return types.BoxI1(a >= b), true
	case instr.I32_GE_U:
		return types.BoxI1(uint32(a) >= uint32(b)), true
	default:
		return 0, false
	}
}

func evalI64(code instr.Opcode, a, b int64) (types.Boxed, bool) {
	switch code {
	case instr.I64_ADD:
		return i64(a + b)
	case instr.I64_SUB:
		return i64(a - b)
	case instr.I64_MUL:
		return i64(a * b)
	case instr.I64_DIV_S:
		if b == 0 {
			return 0, false
		}
		return i64(a / b)
	case instr.I64_DIV_U:
		if b == 0 {
			return 0, false
		}
		return i64(int64(uint64(a) / uint64(b)))
	case instr.I64_REM_S:
		if b == 0 {
			return 0, false
		}
		return i64(a % b)
	case instr.I64_REM_U:
		if b == 0 {
			return 0, false
		}
		return i64(int64(uint64(a) % uint64(b)))
	case instr.I64_SHL:
		return i64(a << (uint64(b) & 0x3F))
	case instr.I64_SHR_S:
		return i64(a >> (uint64(b) & 0x3F))
	case instr.I64_SHR_U:
		return i64(int64(uint64(a) >> (uint64(b) & 0x3F)))
	case instr.I64_XOR:
		return i64(a ^ b)
	case instr.I64_AND:
		return i64(a & b)
	case instr.I64_OR:
		return i64(a | b)
	default:
		return 0, false
	}
}

func evalI64Cmp(code instr.Opcode, a, b int64) (types.Boxed, bool) {
	switch code {
	case instr.I64_EQ:
		return types.BoxI1(a == b), true
	case instr.I64_NE:
		return types.BoxI1(a != b), true
	case instr.I64_LT_S:
		return types.BoxI1(a < b), true
	case instr.I64_LT_U:
		return types.BoxI1(uint64(a) < uint64(b)), true
	case instr.I64_GT_S:
		return types.BoxI1(a > b), true
	case instr.I64_GT_U:
		return types.BoxI1(uint64(a) > uint64(b)), true
	case instr.I64_LE_S:
		return types.BoxI1(a <= b), true
	case instr.I64_LE_U:
		return types.BoxI1(uint64(a) <= uint64(b)), true
	case instr.I64_GE_S:
		return types.BoxI1(a >= b), true
	case instr.I64_GE_U:
		return types.BoxI1(uint64(a) >= uint64(b)), true
	default:
		return 0, false
	}
}

func evalF32(code instr.Opcode, a, b float32) (types.Boxed, bool) {
	switch code {
	case instr.F32_ADD:
		return types.BoxF32(a + b), true
	case instr.F32_SUB:
		return types.BoxF32(a - b), true
	case instr.F32_MUL:
		return types.BoxF32(a * b), true
	case instr.F32_DIV:
		if b == 0 {
			return 0, false
		}
		return types.BoxF32(a / b), true
	case instr.F32_REM:
		if b == 0 {
			return 0, false
		}
		return types.BoxF32(float32(mod(float64(a), float64(b), false))), true
	case instr.F32_MOD:
		if b == 0 {
			return 0, false
		}
		return types.BoxF32(float32(mod(float64(a), float64(b), true))), true
	default:
		return 0, false
	}
}

func evalF32Cmp(code instr.Opcode, a, b float32) (types.Boxed, bool) {
	switch code {
	case instr.F32_EQ:
		return types.BoxI1(a == b), true
	case instr.F32_NE:
		return types.BoxI1(a != b), true
	case instr.F32_LT:
		return types.BoxI1(a < b), true
	case instr.F32_GT:
		return types.BoxI1(a > b), true
	case instr.F32_LE:
		return types.BoxI1(a <= b), true
	case instr.F32_GE:
		return types.BoxI1(a >= b), true
	default:
		return 0, false
	}
}

func evalF64(code instr.Opcode, a, b float64) (types.Boxed, bool) {
	switch code {
	case instr.F64_ADD:
		return types.BoxF64(a + b), true
	case instr.F64_SUB:
		return types.BoxF64(a - b), true
	case instr.F64_MUL:
		return types.BoxF64(a * b), true
	case instr.F64_DIV:
		if b == 0 {
			return 0, false
		}
		return types.BoxF64(a / b), true
	case instr.F64_REM:
		if b == 0 {
			return 0, false
		}
		return types.BoxF64(mod(a, b, false)), true
	case instr.F64_MOD:
		if b == 0 {
			return 0, false
		}
		return types.BoxF64(mod(a, b, true)), true
	default:
		return 0, false
	}
}

func evalF64Cmp(code instr.Opcode, a, b float64) (types.Boxed, bool) {
	switch code {
	case instr.F64_EQ:
		return types.BoxI1(a == b), true
	case instr.F64_NE:
		return types.BoxI1(a != b), true
	case instr.F64_LT:
		return types.BoxI1(a < b), true
	case instr.F64_GT:
		return types.BoxI1(a > b), true
	case instr.F64_LE:
		return types.BoxI1(a <= b), true
	case instr.F64_GE:
		return types.BoxI1(a >= b), true
	default:
		return 0, false
	}
}

// mod computes IEEE remainder (rem) or, when floored is true, floored
// modulo (mod): the sign of a floored result follows the divisor, matching
// transform.FoldPass's F32_MOD/F64_MOD semantics.
func mod(a, b float64, floored bool) float64 {
	m := math.Mod(a, b)
	if floored && m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

// i64 boxes v as an i64 constant, or reports failure when v does not survive
// types.BoxI64's NaN-boxed round trip - the same guard the frontend applies to
// every I64_CONST it builds, which a folded result must honor too or the fold
// would silently corrupt the value.
func i64(v int64) (types.Boxed, bool) {
	b := types.BoxI64(v)
	return b, b.I64() == v
}
