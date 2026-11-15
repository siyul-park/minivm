package transform

import (
	"math"
	"math/bits"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// FoldPass evaluates pure operations with constant operands, reduces integer
// identities and same-operand forms, and turns a branch or table whose
// successor is decided into a jump, which drops the blocks it leaves
// unreachable. A block param that every surviving edge into its block feeds
// the same constant is that constant; a block a retreating edge also enters
// has no such param.
type FoldPass struct{}

var _ pass.Pass[*ssa.Function] = (*FoldPass)(nil)

// NewFoldPass returns the pass.
func NewFoldPass() *FoldPass {
	return &FoldPass{}
}

// Run applies the pass to one SSA function.
func (p *FoldPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	next, changed, ok := foldBlocks(function, true)
	if !ok {
		// Collapsing a terminator reached a graph it cannot keep valid: fold
		// the operations alone.
		next, changed, _ = foldBlocks(function, false)
	}
	if !changed {
		return true, nil
	}
	*function = *next
	return false, nil
}

// foldBlocks returns function rebuilt with its operations folded, and its
// terminators collapsed when collapsing. ok is false when collapsing would
// leave a block only a retreating edge reaches: the graph is irreducible.
func foldBlocks(function *ssa.Function, collapsing bool) (next *ssa.Function, changed, ok bool) {
	constants := map[ssa.Value]uint64{}
	computed := map[ssa.Value]bool{}
	r := newRebuilder(function)

	order := graph.Order(function)
	index := make(map[int]int, len(order))
	for i, block := range order {
		index[block] = i
	}
	retreating := map[int]bool{}
	for _, block := range order {
		for _, edge := range function.Block(block).Terminator.Edges {
			if i, ok := index[edge.Block]; ok && i <= index[block] {
				retreating[edge.Block] = true
			}
		}
	}
	incoming := map[int][][]ssa.Value{}

	reached, skipped := map[int]bool{0: true}, map[int]bool{}
	for _, block := range order {
		if collapsing && !reached[block] {
			skipped[block] = true
			continue
		}
		id := r.open(block)
		b := function.Block(block)
		if block != 0 && !retreating[block] {
			for i, param := range b.Params {
				if w, ok := agreed(r, constants, incoming[block], i, r.from.Type(param)); ok {
					v := r.value(param)
					constants[v], computed[v] = w, true
				}
			}
		}
		for _, operation := range b.Operations {
			operation = r.operation(operation)
			if next, ok := fold(r, id, constants, computed, operation); ok {
				changed = true
				if len(next.Results) == 0 {
					continue
				}
				operation = next
			}
			operation = r.define(operation)
			r.builder.Add(id, operation)
			switch operation.Op {
			case ssa.OpConst:
				constants[operation.Results[0]] = operation.Const
				computed[operation.Results[0]] = true
			case ssa.OpExec:
				for _, result := range operation.Results {
					computed[result] = true
				}
			}
		}
		term := b.Terminator
		if collapsing {
			if collapsed, ok := collapse(r, constants, term); ok {
				changed = true
				term = collapsed
			}
		}
		for _, edge := range term.Edges {
			if skipped[edge.Block] {
				return nil, false, false
			}
			reached[edge.Block] = true
		}
		rebuilt := r.terminator(term)
		for i, edge := range term.Edges {
			incoming[edge.Block] = append(incoming[edge.Block], rebuilt.Edges[i].Args)
		}
		r.builder.Term(id, rebuilt)
	}
	return r.builder.Build(), changed, true
}

// agreed returns the word every edge passes as its i-th arg when each is a
// constant of type t with that same word.
func agreed(r *rebuilder, constants map[ssa.Value]uint64, edges [][]ssa.Value, i int, t ssa.Type) (uint64, bool) {
	var w uint64
	for n, args := range edges {
		c, ok := constants[args[i]]
		if !ok || r.builder.Type(args[i]) != t || (n > 0 && c != w) {
			return 0, false
		}
		w = c
	}
	return w, len(edges) > 0
}

// collapse rewrites a branch or table whose successor is decided into a jump to
// it, decoding the condition as the interpreter does: from its i32 lane.
func collapse(r *rebuilder, constants map[ssa.Value]uint64, t ssa.Terminator) (ssa.Terminator, bool) {
	var edge ssa.Edge
	switch t.Op {
	case ssa.OpBranch:
		if len(t.Edges) == 2 && t.Edges[0].Block == t.Edges[1].Block && slices.Equal(t.Edges[0].Args, t.Edges[1].Args) {
			edge = t.Edges[0]
			break
		}
		w, ok := constants[r.value(t.Args[0])]
		if !ok {
			return t, false
		}
		edge = t.Edges[1]
		if uint32(w) != 0 {
			edge = t.Edges[0]
		}
	case ssa.OpTable:
		w, ok := constants[r.value(t.Args[0])]
		if !ok {
			return t, false
		}
		edge = t.Edges[len(t.Edges)-1]
		if v := int32(uint32(w)); v >= 0 && int(v) < len(t.Edges)-1 {
			edge = t.Edges[v]
		}
	default:
		return t, false
	}
	return ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{edge}}, true
}

func fold(r *rebuilder, id int, constants map[ssa.Value]uint64, computed map[ssa.Value]bool, operation ssa.Operation) (ssa.Operation, bool) {
	if operation.Op != ssa.OpExec || !operation.Code.IsPure() {
		return operation, false
	}
	if tagged(operation.Code) && r.builder.Type(operation.Args[0]) != r.builder.Type(operation.Args[1]) {
		return operation, false
	}
	// The interpreter keeps the kind tag of a bitwise operation's operands, so
	// such a constant takes the operands' type, not the opcode's i32.
	result := func(w uint64) (ssa.Operation, bool) {
		t := r.from.Type(operation.Results[0])
		if tagged(operation.Code) {
			t = r.builder.Type(operation.Args[0])
		}
		if t == r.from.Type(operation.Results[0]) {
			return ssa.Operation{Op: ssa.OpConst, Const: w, Results: operation.Results}, true
		}
		v := r.builder.Value(t)
		r.builder.Add(id, ssa.Operation{Op: ssa.OpConst, Const: w, Results: []ssa.Value{v}})
		constants[v], computed[v] = w, true
		r.alias(operation.Results[0], v)
		return ssa.Operation{}, true
	}
	args := make([]uint64, 0, len(operation.Args))
	for _, a := range operation.Args {
		w, ok := constants[a]
		if !ok {
			break
		}
		args = append(args, w)
	}
	if len(args) == len(operation.Args) {
		if w, ok := eval(operation.Code, args); ok {
			return result(w)
		}
		return operation, false
	}
	if len(operation.Args) != 2 || len(operation.Results) != 1 {
		return operation, false
	}
	x, y := operation.Args[0], operation.Args[1]
	if x == y {
		w, alias, ok := reflexive(operation.Code)
		if ok && !alias {
			return result(w)
		}
		if ok && computed[x] && r.builder.Type(x) == r.from.Type(operation.Results[0]) {
			r.alias(operation.Results[0], x)
			return ssa.Operation{Op: operation.Op, Code: operation.Code, Args: operation.Args}, true
		}
		return operation, false
	}
	_, left := constants[x]
	if _, right := constants[y]; left && !right && commutative(operation.Code) {
		x, y = y, x
	}
	right, ok := constants[y]
	if !ok {
		return operation, false
	}
	if w, ok := absorb(operation.Code, right); ok {
		return result(w)
	}
	if identity(operation.Code, right) && computed[x] && r.builder.Type(x) == r.from.Type(operation.Results[0]) {
		r.alias(operation.Results[0], x)
		return ssa.Operation{Op: operation.Op, Code: operation.Code, Args: operation.Args}, true
	}
	code, amount, ok := shift(operation.Code, right)
	if !ok {
		return operation, false
	}
	count := r.builder.Value(r.builder.Type(y))
	r.builder.Add(id, ssa.Operation{Op: ssa.OpConst, Const: amount, Results: []ssa.Value{count}})
	constants[count] = amount
	return ssa.Operation{Op: operation.Op, Code: code, Args: []ssa.Value{x, count}, State: operation.State, Results: operation.Results}, true
}

// tagged reports whether code keeps its operands' kind tag in its result.
func tagged(code instr.Opcode) bool {
	switch code {
	case instr.I32_XOR, instr.I32_AND, instr.I32_OR:
		return true
	default:
		return false
	}
}

// reflexive returns what code gives when both operands are the same value: a
// constant word, or alias when the result is that value.
func reflexive(code instr.Opcode) (w uint64, alias, ok bool) {
	switch code {
	case instr.I32_SUB, instr.I32_XOR, instr.I64_SUB, instr.I64_XOR:
		return 0, false, true
	case instr.I32_NE, instr.I32_LT_S, instr.I32_LT_U, instr.I32_GT_S, instr.I32_GT_U,
		instr.I64_NE, instr.I64_LT_S, instr.I64_LT_U, instr.I64_GT_S, instr.I64_GT_U:
		return word(false), false, true
	case instr.I32_EQ, instr.I32_LE_S, instr.I32_LE_U, instr.I32_GE_S, instr.I32_GE_U,
		instr.I64_EQ, instr.I64_LE_S, instr.I64_LE_U, instr.I64_GE_S, instr.I64_GE_U:
		return word(true), false, true
	case instr.I32_AND, instr.I32_OR, instr.I64_AND, instr.I64_OR:
		return 0, true, true
	default:
		return 0, false, false
	}
}

// commutative reports whether code gives the same result for swapped integer operands.
func commutative(code instr.Opcode) bool {
	switch code {
	case instr.I32_ADD, instr.I32_MUL, instr.I32_AND, instr.I32_OR, instr.I32_XOR, instr.I32_EQ, instr.I32_NE,
		instr.I64_ADD, instr.I64_MUL, instr.I64_AND, instr.I64_OR, instr.I64_XOR, instr.I64_EQ, instr.I64_NE:
		return true
	default:
		return false
	}
}

// absorb returns the result code gives for any left operand when w is its
// absorbing right operand, decoded by the opcode's own family.
func absorb(code instr.Opcode, w uint64) (uint64, bool) {
	switch code {
	case instr.I32_MUL, instr.I32_AND:
		return 0, uint32(w) == 0
	case instr.I32_OR:
		return word(int32(-1)), uint32(w) == 0xFFFFFFFF
	case instr.I64_MUL, instr.I64_AND:
		return 0, w == 0
	case instr.I64_OR:
		return math.MaxUint64, w == math.MaxUint64
	default:
		return 0, false
	}
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

func eval(code instr.Opcode, args []uint64) (uint64, bool) {
	switch code {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_MUL, instr.I32_DIV_S, instr.I32_DIV_U,
		instr.I32_REM_S, instr.I32_REM_U, instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U,
		instr.I32_XOR, instr.I32_AND, instr.I32_OR,
		instr.I32_EQ, instr.I32_NE, instr.I32_LT_S, instr.I32_LT_U, instr.I32_GT_S, instr.I32_GT_U,
		instr.I32_LE_S, instr.I32_LE_U, instr.I32_GE_S, instr.I32_GE_U:
		return evalI32(code, int32(uint32(args[0])), int32(uint32(args[1])))
	case instr.I32_EQZ:
		return word(int32(uint32(args[0])) == 0), true

	case instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_DIV_S, instr.I64_DIV_U,
		instr.I64_REM_S, instr.I64_REM_U, instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U,
		instr.I64_XOR, instr.I64_AND, instr.I64_OR,
		instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_LT_U, instr.I64_GT_S, instr.I64_GT_U,
		instr.I64_LE_S, instr.I64_LE_U, instr.I64_GE_S, instr.I64_GE_U:
		return evalI64(code, int64(args[0]), int64(args[1]))
	case instr.I64_EQZ:
		return word(int64(args[0]) == 0), true

	case instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV, instr.F32_REM, instr.F32_MOD,
		instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_GT, instr.F32_LE, instr.F32_GE:
		return evalF32(code, math.Float32frombits(uint32(args[0])), math.Float32frombits(uint32(args[1])))

	case instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV, instr.F64_REM, instr.F64_MOD,
		instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_GT, instr.F64_LE, instr.F64_GE:
		return evalF64(code, math.Float64frombits(args[0]), math.Float64frombits(args[1]))

	case instr.I32_TO_F32_S:
		return word(float32(int32(uint32(args[0])))), true
	case instr.I32_TO_F32_U:
		return word(float32(uint32(args[0]))), true
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
	case instr.F32_TO_I32_S, instr.F32_TO_I32_U:
		return truncate(code, float64(math.Float32frombits(uint32(args[0]))))
	case instr.F64_TO_I32_S, instr.F64_TO_I32_U, instr.F64_TO_I64_S, instr.F64_TO_I64_U:
		return truncate(code, math.Float64frombits(args[0]))
	case instr.F64_TO_F32:
		return word(float32(math.Float64frombits(args[0]))), true
	default:
		return 0, false
	}
}

// truncate evaluates a float-to-integer conversion, failing when f is NaN or
// outside the target range.
func truncate(code instr.Opcode, f float64) (uint64, bool) {
	switch code {
	case instr.F32_TO_I32_S, instr.F64_TO_I32_S:
		if math.IsNaN(f) || f < -(1<<31) || f >= 1<<31 {
			return 0, false
		}
		return word(int32(f)), true
	case instr.F32_TO_I32_U, instr.F64_TO_I32_U:
		if math.IsNaN(f) || f < 0 || f >= 1<<32 {
			return 0, false
		}
		return word(int32(uint32(f))), true
	case instr.F64_TO_I64_S:
		if math.IsNaN(f) || f < -(1<<63) || f >= 1<<63 {
			return 0, false
		}
		return uint64(int64(f)), true
	default:
		if math.IsNaN(f) || f < 0 || f >= 1<<64 {
			return 0, false
		}
		return uint64(f), true
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

// word encodes a folded result as its native word.
func word[v bool | int32 | float32](x v) uint64 {
	switch v := any(x).(type) {
	case bool:
		return types.BoxI1(v).Word()
	case int32:
		return types.BoxI32(v).Word()
	case float32:
		return types.BoxF32(v).Word()
	}
	return 0
}

func mod(a, b float64, floored bool) float64 {
	m := math.Mod(a, b)
	if floored && m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}
