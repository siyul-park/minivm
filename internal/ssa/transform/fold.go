package transform

import (
	"math"
	"math/bits"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// FoldPass replaces a pure operation with the result its arguments already
// decide: the constant its opcode computes when every argument is one, the
// left argument itself when the right one makes the opcode an identity, and a
// shift when it makes a multiply or a divide one. All three are the same
// policy - an operation whose answer is known before it runs does not run -
// which is why the algebraic identities live here rather than in a pass of
// their own.
//
// It runs over any function this package's passes accept, one carrying JIT
// guards and deopt state or one with neither, since the gate is instr's own
// op.Code.IsPure(), which is already false for anything the JIT alone would
// add (a guard, a bridge, a retain or release are none of them OpExec) and
// for anything that touches Local, Global, Upval, Heap, Frame, or Branch.
// That leaves exactly the closed family transform's bytecode passes folded by
// hand - integer and float arithmetic, comparisons, eqz, and the narrowing
// conversions - which this pass computes by decoding each argument's
// types.Boxed into the typed value (I32, I64, F32, F64 are Go's own numeric
// types under another name) and letting Go's native operators do the
// arithmetic, rather than re-deriving it from raw bits the way a pass over
// bytecode must.
//
// A division or remainder whose divisor folds to zero is left as a runtime
// operation: the interpreter's own trap for that case is not this pass's to
// pre-empt. An i64 arithmetic or shift result that does not survive
// types.BoxI64's NaN-boxed round trip is left unfolded too - every i64
// OpConst the frontend builds is already round-trip-safe, so a result that is
// not would silently corrupt the value if boxed anyway. Float identities are
// left alone as well, because IEEE-754's NaN and signed zero make them
// unsound, and so are annihilators such as x*0, whose result is known but
// whose left argument would have to be dropped along with them - which is
// DCEPass's judgement to make, not this pass's.
type FoldPass struct{}

var _ pass.Pass[*ssa.Function] = (*FoldPass)(nil)

func NewFoldPass() *FoldPass {
	return &FoldPass{}
}

func (p *FoldPass) Run(_ *pass.Manager, fn *ssa.Function) (pass.Preserved, error) {
	consts := map[ssa.Value]types.Boxed{}
	computed := map[ssa.Value]bool{}
	rb := newRebuilder(fn)
	changed := false

	for _, block := range order(fn) {
		id := rb.block(block)
		blk := fn.Block(block)
		for _, param := range blk.Params {
			rb.alias(param, rb.b.Param(id, fn.Type(param)))
		}
		for _, op := range blk.Ops {
			op = rb.operation(op)
			if op.Op == ssa.OpExec && op.Code.IsPure() {
				if next, ok := p.fold(rb, id, fn, consts, computed, op); ok {
					changed = true
					// An operation reduced to one of its own arguments has
					// had its result aliased onto it and leaves nothing to
					// add.
					if len(next.Results) == 0 {
						continue
					}
					op = next
				}
			}
			op = rb.define(fn, op)
			rb.b.Add(id, op)
			switch op.Op {
			case ssa.OpConst:
				consts[op.Results[0]] = op.Const
				computed[op.Results[0]] = true
			case ssa.OpExec, ssa.OpBridge:
				// A value an opcode computed carries the representation its
				// static type names; one read out of a slot need not (see
				// fold).
				for _, r := range op.Results {
					computed[r] = true
				}
			}
		}
		rb.b.Term(id, rb.terminator(blk.Term))
	}

	if !changed {
		return pass.PreserveAll(), nil
	}
	*fn = *rb.b.Build()
	return pass.PreserveNone(), nil
}

// fold returns what op becomes once its constant arguments are accounted for,
// and false when it stays as it is. An operation that reduces to one of its
// own arguments comes back with no results at all: fold has already aliased
// the value it produced onto that argument, so nothing is left to add.
func (p *FoldPass) fold(rb *rebuilder, id int, fn *ssa.Function, consts map[ssa.Value]types.Boxed, computed map[ssa.Value]bool, op ssa.Operation) (ssa.Operation, bool) {
	args := make([]types.Boxed, 0, len(op.Args))
	for _, a := range op.Args {
		c, ok := consts[a]
		if !ok {
			break
		}
		args = append(args, c)
	}
	if len(args) == len(op.Args) {
		if result, ok := eval(op.Code, args); ok {
			return ssa.Operation{Op: ssa.OpConst, Const: result, Results: op.Results}, true
		}
		return op, false
	}
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return op, false
	}
	right, ok := consts[op.Args[1]]
	if !ok {
		return op, false
	}
	// An identity hands back its left argument, and with it that argument's
	// own representation. Only a value an opcode computed carries the one its
	// static type names: a slot is zero-filled rather than written with its
	// declared type's zero, so an unwritten i32 local reads back as a raw
	// Boxed(0), whose kind is f64 (docs/memory-model.md). The arithmetic this
	// rewrite would remove is what re-tags it, and the type has to match for
	// the same reason - i1 and i8 both reach an i32 opcode, and neither may be
	// read back where the i32 result was.
	if identity(op.Code, right) && computed[op.Args[0]] && rb.b.Type(op.Args[0]) == fn.Type(op.Results[0]) {
		rb.alias(op.Results[0], op.Args[0])
		return ssa.Operation{Op: op.Op, Code: op.Code, Args: op.Args}, true
	}
	code, amount, ok := reduce(op.Code, right)
	if !ok {
		return op, false
	}
	shift := rb.b.Value(rb.b.Type(op.Args[1]))
	rb.b.Add(id, ssa.Operation{Op: ssa.OpConst, Const: amount, Results: []ssa.Value{shift}})
	consts[shift] = amount
	return ssa.Operation{Op: op.Op, Code: code, Args: []ssa.Value{op.Args[0], shift}, Results: op.Results}, true
}

// identity reports whether code hands back its left argument unchanged when
// its right one is c.
func identity(code instr.Opcode, c types.Boxed) bool {
	switch code {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_OR, instr.I32_XOR,
		instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U:
		return c.Kind() == types.KindI32 && c.I32() == 0
	case instr.I32_AND:
		return c.Kind() == types.KindI32 && c.I32() == -1
	case instr.I32_MUL, instr.I32_DIV_S, instr.I32_DIV_U:
		return c.Kind() == types.KindI32 && c.I32() == 1
	case instr.I64_ADD, instr.I64_SUB, instr.I64_OR, instr.I64_XOR,
		instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U:
		return c.Kind() == types.KindI64 && c.I64() == 0
	case instr.I64_AND:
		return c.Kind() == types.KindI64 && c.I64() == -1
	case instr.I64_MUL, instr.I64_DIV_S, instr.I64_DIV_U:
		return c.Kind() == types.KindI64 && c.I64() == 1
	default:
		return false
	}
}

// reduce returns the shift a multiply or an unsigned divide by the
// power-of-two c becomes, and its amount. A signed divide is not one of them:
// an arithmetic shift right rounds toward negative infinity where the divide
// rounds toward zero.
func reduce(code instr.Opcode, c types.Boxed) (instr.Opcode, types.Boxed, bool) {
	var shift instr.Opcode
	switch code {
	case instr.I32_MUL, instr.I64_MUL:
		shift = instr.I32_SHL
		if code == instr.I64_MUL {
			shift = instr.I64_SHL
		}
	case instr.I32_DIV_U, instr.I64_DIV_U:
		shift = instr.I32_SHR_U
		if code == instr.I64_DIV_U {
			shift = instr.I64_SHR_U
		}
	default:
		return 0, 0, false
	}
	switch c.Kind() {
	case types.KindI32:
		n, ok := log2(uint64(uint32(c.I32())))
		return shift, types.BoxI32(int32(n)), ok
	case types.KindI64:
		n, ok := log2(uint64(c.I64()))
		return shift, types.BoxI64(int64(n)), ok
	default:
		return 0, 0, false
	}
}

// log2 returns the exponent of v when v is a power of two greater than one.
func log2(v uint64) (uint64, bool) {
	if v < 2 || v&(v-1) != 0 {
		return 0, false
	}
	return uint64(bits.TrailingZeros64(v)), true
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
// the interpreter's own F32_MOD and F64_MOD.
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
