package transform

import (
	"math"
	"math/bits"

	"github.com/siyul-park/minivm/internal/graph"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// FoldPass evaluates pure operations with constant operands.
type FoldPass struct{}

var _ pass.Pass[*ssa.Function] = (*FoldPass)(nil)

// NewFoldPass returns the pass.
func NewFoldPass() *FoldPass {
	return &FoldPass{}
}

// Run applies the pass to one SSA function.
func (p *FoldPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	constants := map[ssa.Value]types.Boxed{}
	computed := map[ssa.Value]bool{}
	rebuilder := newRebuilder(function)
	changed := false

	for _, block := range graph.Order(function) {
		id := rebuilder.block(block)
		currentBlock := function.Block(block)
		for _, param := range currentBlock.Params {
			rebuilder.alias(param, rebuilder.builder.Param(id, function.Type(param)))
		}
		for _, operation := range currentBlock.Operations {
			operation = rebuilder.operation(operation)
			if operation.Op == ssa.OpExec && operation.Code.IsPure() {
				if next, ok := fold(rebuilder, id, function, constants, computed, operation); ok {
					changed = true
					if len(next.Results) == 0 {
						continue
					}
					operation = next
				}
			}
			operation = rebuilder.define(function, operation)
			rebuilder.builder.Add(id, operation)
			switch operation.Op {
			case ssa.OpConst:
				constants[operation.Results[0]] = operation.Const
				computed[operation.Results[0]] = true
			case ssa.OpExec:
				for _, r := range operation.Results {
					computed[r] = true
				}
			}
		}
		rebuilder.builder.Term(id, rebuilder.terminator(currentBlock.Terminator))
	}

	if !changed {
		return true, nil
	}
	*function = *rebuilder.builder.Build()
	return false, nil
}

func fold(rebuilder *rebuilder, id int, function *ssa.Function, constants map[ssa.Value]types.Boxed, computed map[ssa.Value]bool, operation ssa.Operation) (ssa.Operation, bool) {
	args := make([]types.Boxed, 0, len(operation.Args))
	for _, a := range operation.Args {
		c, ok := constants[a]
		if !ok {
			break
		}
		args = append(args, c)
	}
	if len(args) == len(operation.Args) {
		if result, ok := eval(operation.Code, args); ok {
			return ssa.Operation{Op: ssa.OpConst, Const: result, Results: operation.Results}, true
		}
		return operation, false
	}
	if len(operation.Args) != 2 || len(operation.Results) != 1 {
		return operation, false
	}
	right, ok := constants[operation.Args[1]]
	if !ok {
		return operation, false
	}
	if identity(operation.Code, right) && computed[operation.Args[0]] && rebuilder.builder.Type(operation.Args[0]) == function.Type(operation.Results[0]) {
		rebuilder.alias(operation.Results[0], operation.Args[0])
		return ssa.Operation{Op: operation.Op, Code: operation.Code, Args: operation.Args}, true
	}
	code, amount, ok := shift(operation.Code, right)
	if !ok {
		return operation, false
	}
	shift := rebuilder.builder.Value(rebuilder.builder.Type(operation.Args[1]))
	rebuilder.builder.Add(id, ssa.Operation{Op: ssa.OpConst, Const: amount, Results: []ssa.Value{shift}})
	constants[shift] = amount
	return ssa.Operation{Op: operation.Op, Code: code, Args: []ssa.Value{operation.Args[0], shift}, State: operation.State, Results: operation.Results}, true
}

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

func shift(code instr.Opcode, c types.Boxed) (instr.Opcode, types.Boxed, bool) {
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

func log2(v uint64) (uint64, bool) {
	if v < 2 || v&(v-1) != 0 {
		return 0, false
	}
	return uint64(bits.TrailingZeros64(v)), true
}

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

func mod(a, b float64, floored bool) float64 {
	m := math.Mod(a, b)
	if floored && m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

func i64(v int64) (types.Boxed, bool) {
	b := types.BoxI64(v)
	return b, b.I64() == v
}
