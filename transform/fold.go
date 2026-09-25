package transform

import (
	"math"
	"math/bits"

	"github.com/siyul-park/minivm/internal/graph"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
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
	constants := map[ssa.Value]uint64{}
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

func fold(rebuilder *rebuilder, id int, function *ssa.Function, constants map[ssa.Value]uint64, computed map[ssa.Value]bool, operation ssa.Operation) (ssa.Operation, bool) {
	args := make([]uint64, 0, len(operation.Args))
	for _, a := range operation.Args {
		w, ok := constants[a]
		if !ok {
			break
		}
		args = append(args, w)
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

// identity reports whether w is code's identity operand, decoded by the
// opcode's own family: the i32 lane for an i32 op, the full word for i64.
func identity(code instr.Opcode, w uint64) bool {
	switch code {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_OR, instr.I32_XOR,
		instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U:
		return uint32(w) == 0
	case instr.I32_AND:
		return uint32(w) == 0xFFFFFFFF
	case instr.I32_MUL, instr.I32_DIV_S, instr.I32_DIV_U:
		return uint32(w) == 1
	case instr.I64_ADD, instr.I64_SUB, instr.I64_OR, instr.I64_XOR,
		instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U:
		return w == 0
	case instr.I64_AND:
		return w == math.MaxUint64
	case instr.I64_MUL, instr.I64_DIV_S, instr.I64_DIV_U:
		return w == 1
	default:
		return false
	}
}

// shift rewrites a power-of-two mul/div-u into a shift, decoding the operand
// by code's own family.
func shift(code instr.Opcode, w uint64) (instr.Opcode, uint64, bool) {
	switch code {
	case instr.I32_MUL:
		n, ok := log2(uint64(uint32(w)))
		return instr.I32_SHL, word(int32(n)), ok
	case instr.I32_DIV_U:
		n, ok := log2(uint64(uint32(w)))
		return instr.I32_SHR_U, word(int32(n)), ok
	case instr.I64_MUL:
		n, ok := log2(w)
		return instr.I64_SHL, n, ok
	case instr.I64_DIV_U:
		n, ok := log2(w)
		return instr.I64_SHR_U, n, ok
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

// word encodes a folded result as its native word.
func word[T bool | int32 | float32](v T) uint64 {
	switch v := any(v).(type) {
	case bool:
		if v {
			return 1
		}
	case int32:
		return uint64(uint32(v))
	case float32:
		return uint64(math.Float32bits(v))
	}
	return 0
}

func eval(code instr.Opcode, args []uint64) (uint64, bool) {
	switch code {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_MUL, instr.I32_DIV_S, instr.I32_DIV_U,
		instr.I32_REM_S, instr.I32_REM_U, instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U,
		instr.I32_XOR, instr.I32_AND, instr.I32_OR:
		return evalI32(code, int32(uint32(args[0])), int32(uint32(args[1])))
	case instr.I32_EQ, instr.I32_NE, instr.I32_LT_S, instr.I32_LT_U, instr.I32_GT_S, instr.I32_GT_U,
		instr.I32_LE_S, instr.I32_LE_U, instr.I32_GE_S, instr.I32_GE_U:
		return evalI32Cmp(code, int32(uint32(args[0])), int32(uint32(args[1])))
	case instr.I32_EQZ:
		return word(int32(uint32(args[0])) == 0), true
	case instr.I32_TO_F32_S:
		return word(float32(int32(uint32(args[0])))), true
	case instr.I32_TO_F32_U:
		return word(float32(uint32(args[0]))), true

	case instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_DIV_S, instr.I64_DIV_U,
		instr.I64_REM_S, instr.I64_REM_U, instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U,
		instr.I64_XOR, instr.I64_AND, instr.I64_OR:
		return evalI64(code, int64(args[0]), int64(args[1]))
	case instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_LT_U, instr.I64_GT_S, instr.I64_GT_U,
		instr.I64_LE_S, instr.I64_LE_U, instr.I64_GE_S, instr.I64_GE_U:
		return evalI64Cmp(code, int64(args[0]), int64(args[1]))
	case instr.I64_EQZ:
		return word(int64(args[0]) == 0), true
	case instr.I64_TO_I32:
		return word(int32(int64(args[0]))), true
	case instr.I64_TO_F32_S:
		return word(float32(int64(args[0]))), true
	case instr.I64_TO_F32_U:
		return word(float32(uint64(args[0]))), true
	case instr.I64_TO_F64_S:
		return math.Float64bits(float64(int64(args[0]))), true
	case instr.I64_TO_F64_U:
		return math.Float64bits(float64(uint64(args[0]))), true

	case instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV, instr.F32_REM, instr.F32_MOD:
		return evalF32(code, math.Float32frombits(uint32(args[0])), math.Float32frombits(uint32(args[1])))
	case instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_GT, instr.F32_LE, instr.F32_GE:
		return evalF32Cmp(code, math.Float32frombits(uint32(args[0])), math.Float32frombits(uint32(args[1])))
	// Threaded saturates NaN and out-of-range float-to-int conversions, where
	// Go's result is implementation-specific: fold leaves those to it.
	case instr.F32_TO_I32_S:
		f := float64(math.Float32frombits(uint32(args[0])))
		if math.IsNaN(f) || f < -(1<<31) || f >= 1<<31 {
			return 0, false
		}
		return word(int32(f)), true
	case instr.F32_TO_I32_U:
		f := float64(math.Float32frombits(uint32(args[0])))
		if math.IsNaN(f) || f < 0 || f >= 1<<32 {
			return 0, false
		}
		return word(int32(uint32(f))), true

	case instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV, instr.F64_REM, instr.F64_MOD:
		return evalF64(code, math.Float64frombits(args[0]), math.Float64frombits(args[1]))
	case instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_GT, instr.F64_LE, instr.F64_GE:
		return evalF64Cmp(code, math.Float64frombits(args[0]), math.Float64frombits(args[1]))
	case instr.F64_TO_I32_S:
		f := math.Float64frombits(args[0])
		if math.IsNaN(f) || f < -(1<<31) || f >= 1<<31 {
			return 0, false
		}
		return word(int32(f)), true
	case instr.F64_TO_I32_U:
		f := math.Float64frombits(args[0])
		if math.IsNaN(f) || f < 0 || f >= 1<<32 {
			return 0, false
		}
		return word(int32(uint32(f))), true
	case instr.F64_TO_I64_S:
		f := math.Float64frombits(args[0])
		if math.IsNaN(f) || f < -(1<<63) || f >= 1<<63 {
			return 0, false
		}
		return uint64(int64(f)), true
	case instr.F64_TO_I64_U:
		f := math.Float64frombits(args[0])
		if math.IsNaN(f) || f < 0 || f >= 1<<64 {
			return 0, false
		}
		return uint64(f), true
	case instr.F64_TO_F32:
		return word(float32(math.Float64frombits(args[0]))), true

	default:
		return 0, false
	}
}

func evalI32(code instr.Opcode, a, b int32) (uint64, bool) {
	switch code {
	case instr.I32_ADD:
		return word(a + b), true
	case instr.I32_SUB:
		return word(a - b), true
	case instr.I32_MUL:
		return word(a * b), true
	case instr.I32_DIV_S:
		if b == 0 {
			return 0, false
		}
		return word(a / b), true
	case instr.I32_DIV_U:
		if b == 0 {
			return 0, false
		}
		return word(int32(uint32(a) / uint32(b))), true
	case instr.I32_REM_S:
		if b == 0 {
			return 0, false
		}
		return word(a % b), true
	case instr.I32_REM_U:
		if b == 0 {
			return 0, false
		}
		return word(int32(uint32(a) % uint32(b))), true
	case instr.I32_SHL:
		return word(a << (uint32(b) & 0x1F)), true
	case instr.I32_SHR_S:
		return word(a >> (uint32(b) & 0x1F)), true
	case instr.I32_SHR_U:
		return word(int32(uint32(a) >> (uint32(b) & 0x1F))), true
	case instr.I32_XOR:
		return word(a ^ b), true
	case instr.I32_AND:
		return word(a & b), true
	case instr.I32_OR:
		return word(a | b), true
	default:
		return 0, false
	}
}

func evalI32Cmp(code instr.Opcode, a, b int32) (uint64, bool) {
	switch code {
	case instr.I32_EQ:
		return word(a == b), true
	case instr.I32_NE:
		return word(a != b), true
	case instr.I32_LT_S:
		return word(a < b), true
	case instr.I32_LT_U:
		return word(uint32(a) < uint32(b)), true
	case instr.I32_GT_S:
		return word(a > b), true
	case instr.I32_GT_U:
		return word(uint32(a) > uint32(b)), true
	case instr.I32_LE_S:
		return word(a <= b), true
	case instr.I32_LE_U:
		return word(uint32(a) <= uint32(b)), true
	case instr.I32_GE_S:
		return word(a >= b), true
	case instr.I32_GE_U:
		return word(uint32(a) >= uint32(b)), true
	default:
		return 0, false
	}
}

func evalI64(code instr.Opcode, a, b int64) (uint64, bool) {
	switch code {
	case instr.I64_ADD:
		return uint64(a + b), true
	case instr.I64_SUB:
		return uint64(a - b), true
	case instr.I64_MUL:
		return uint64(a * b), true
	case instr.I64_DIV_S:
		if b == 0 {
			return 0, false
		}
		return uint64(a / b), true
	case instr.I64_DIV_U:
		if b == 0 {
			return 0, false
		}
		return uint64(uint64(a) / uint64(b)), true
	case instr.I64_REM_S:
		if b == 0 {
			return 0, false
		}
		return uint64(a % b), true
	case instr.I64_REM_U:
		if b == 0 {
			return 0, false
		}
		return uint64(uint64(a) % uint64(b)), true
	case instr.I64_SHL:
		return uint64(a << (uint64(b) & 0x3F)), true
	case instr.I64_SHR_S:
		return uint64(a >> (uint64(b) & 0x3F)), true
	case instr.I64_SHR_U:
		return uint64(a) >> (uint64(b) & 0x3F), true
	case instr.I64_XOR:
		return uint64(a ^ b), true
	case instr.I64_AND:
		return uint64(a & b), true
	case instr.I64_OR:
		return uint64(a | b), true
	default:
		return 0, false
	}
}

func evalI64Cmp(code instr.Opcode, a, b int64) (uint64, bool) {
	switch code {
	case instr.I64_EQ:
		return word(a == b), true
	case instr.I64_NE:
		return word(a != b), true
	case instr.I64_LT_S:
		return word(a < b), true
	case instr.I64_LT_U:
		return word(uint64(a) < uint64(b)), true
	case instr.I64_GT_S:
		return word(a > b), true
	case instr.I64_GT_U:
		return word(uint64(a) > uint64(b)), true
	case instr.I64_LE_S:
		return word(a <= b), true
	case instr.I64_LE_U:
		return word(uint64(a) <= uint64(b)), true
	case instr.I64_GE_S:
		return word(a >= b), true
	case instr.I64_GE_U:
		return word(uint64(a) >= uint64(b)), true
	default:
		return 0, false
	}
}

func evalF32(code instr.Opcode, a, b float32) (uint64, bool) {
	switch code {
	case instr.F32_ADD:
		return word(a + b), true
	case instr.F32_SUB:
		return word(a - b), true
	case instr.F32_MUL:
		return word(a * b), true
	case instr.F32_DIV:
		if b == 0 {
			return 0, false
		}
		return word(a / b), true
	case instr.F32_REM:
		if b == 0 {
			return 0, false
		}
		return word(float32(mod(float64(a), float64(b), false))), true
	case instr.F32_MOD:
		if b == 0 {
			return 0, false
		}
		return word(float32(mod(float64(a), float64(b), true))), true
	default:
		return 0, false
	}
}

func evalF32Cmp(code instr.Opcode, a, b float32) (uint64, bool) {
	switch code {
	case instr.F32_EQ:
		return word(a == b), true
	case instr.F32_NE:
		return word(a != b), true
	case instr.F32_LT:
		return word(a < b), true
	case instr.F32_GT:
		return word(a > b), true
	case instr.F32_LE:
		return word(a <= b), true
	case instr.F32_GE:
		return word(a >= b), true
	default:
		return 0, false
	}
}

func evalF64(code instr.Opcode, a, b float64) (uint64, bool) {
	switch code {
	case instr.F64_ADD:
		return math.Float64bits(a + b), true
	case instr.F64_SUB:
		return math.Float64bits(a - b), true
	case instr.F64_MUL:
		return math.Float64bits(a * b), true
	case instr.F64_DIV:
		if b == 0 {
			return 0, false
		}
		return math.Float64bits(a / b), true
	case instr.F64_REM:
		if b == 0 {
			return 0, false
		}
		return math.Float64bits(mod(a, b, false)), true
	case instr.F64_MOD:
		if b == 0 {
			return 0, false
		}
		return math.Float64bits(mod(a, b, true)), true
	default:
		return 0, false
	}
}

func evalF64Cmp(code instr.Opcode, a, b float64) (uint64, bool) {
	switch code {
	case instr.F64_EQ:
		return word(a == b), true
	case instr.F64_NE:
		return word(a != b), true
	case instr.F64_LT:
		return word(a < b), true
	case instr.F64_GT:
		return word(a > b), true
	case instr.F64_LE:
		return word(a <= b), true
	case instr.F64_GE:
		return word(a >= b), true
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
