package transform

import (
	"math"
	"math/bits"
	"slices"

	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// FoldPass evaluates pure operations with constant operands, reduces integer
// identities and same-operand forms, rewrites a multiply or unsigned divide by
// a power of two into a shift, and turns a branch or table whose
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
			switch {
			case operation.Op == ssa.OpConst:
				constants[operation.Results[0]] = operation.Const
				computed[operation.Results[0]] = true
			case operation.Op == ssa.OpExec || operation.Op.Data():
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
	switch t.Term {
	case ssa.TermBrIf:
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
	case ssa.TermBrTable:
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
	return ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{edge}}, true
}

func fold(r *rebuilder, id int, constants map[ssa.Value]uint64, computed map[ssa.Value]bool, operation ssa.Operation) (ssa.Operation, bool) {
	if !operation.Op.Data() || !operation.Pure() {
		return operation, false
	}
	if tagged(operation.Op) && r.builder.Type(operation.Args[0]) != r.builder.Type(operation.Args[1]) {
		return operation, false
	}
	// The interpreter keeps the kind tag of a bitwise operation's operands, so
	// such a constant takes the operands' type, not the opcode's i32.
	result := func(w uint64) (ssa.Operation, bool) {
		t := r.from.Type(operation.Results[0])
		if tagged(operation.Op) {
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
		if w, ok := eval(operation, r.builder.Type(operation.Args[0]), r.from.Type(operation.Results[0]), args); ok {
			return result(w)
		}
		return operation, false
	}
	if len(operation.Args) != 2 || len(operation.Results) != 1 {
		return operation, false
	}
	x, y := operation.Args[0], operation.Args[1]
	lane := r.builder.Type(x).Lane()
	if x == y {
		w, alias, ok := reflexive(operation)
		if ok && !alias {
			return result(w)
		}
		if ok && computed[x] && r.builder.Type(x) == r.from.Type(operation.Results[0]) {
			r.alias(operation.Results[0], x)
			return ssa.Operation{Op: operation.Op, Args: operation.Args}, true
		}
		return operation, false
	}
	_, left := constants[x]
	if _, right := constants[y]; left && !right && commutative(operation) {
		x, y = y, x
	}
	right, ok := constants[y]
	if !ok {
		return operation, false
	}
	if w, ok := absorb(operation.Op, right, lane); ok {
		return result(w)
	}
	if identity(operation.Op, right, lane) && computed[x] && r.builder.Type(x) == r.from.Type(operation.Results[0]) {
		r.alias(operation.Results[0], x)
		return ssa.Operation{Op: operation.Op, Args: operation.Args}, true
	}
	op, amount, ok := shift(operation.Op, right, lane)
	if !ok {
		return operation, false
	}
	count := r.builder.Value(r.builder.Type(y))
	r.builder.Add(id, ssa.Operation{Op: ssa.OpConst, Const: amount, Results: []ssa.Value{count}})
	constants[count] = amount
	return ssa.Operation{Op: op, Args: []ssa.Value{x, count}, State: operation.State, Results: operation.Results}, true
}

// tagged reports whether op keeps its operands' kind tag in its result.
func tagged(op ssa.Op) bool {
	switch op {
	case ssa.OpIntXor, ssa.OpIntAnd, ssa.OpIntOr:
		return true
	default:
		return false
	}
}

// reflexive returns what an integer operation gives when both operands are the
// same value: a constant word, or alias when the result is that value.
func reflexive(o ssa.Operation) (w uint64, alias, ok bool) {
	switch o.Op {
	case ssa.OpIntSub, ssa.OpIntXor:
		return 0, false, true
	case ssa.OpIntAnd, ssa.OpIntOr:
		return 0, true, true
	case ssa.OpIntCmp:
		switch o.Cond {
		case ssa.CondNE, ssa.CondSLT, ssa.CondULT, ssa.CondSGT, ssa.CondUGT:
			return word(false), false, true
		default:
			return word(true), false, true
		}
	default:
		return 0, false, false
	}
}

// commutative reports whether o gives the same result for swapped integer operands.
func commutative(o ssa.Operation) bool {
	switch o.Op {
	case ssa.OpIntAdd, ssa.OpIntMul, ssa.OpIntSAddOvf, ssa.OpIntUAddOvf, ssa.OpIntSMulOvf, ssa.OpIntUMulOvf, ssa.OpIntAnd, ssa.OpIntOr, ssa.OpIntXor:
		return true
	case ssa.OpIntCmp:
		return o.Cond == ssa.CondEQ || o.Cond == ssa.CondNE
	default:
		return false
	}
}

// absorb returns the result op gives for any left operand when w is its
// absorbing right operand, decoded by the lane of the operands.
func absorb(op ssa.Op, w uint64, lane ssa.Type) (uint64, bool) {
	wide := lane == ssa.TypeI64
	switch op {
	case ssa.OpIntMul, ssa.OpIntAnd:
		return 0, mask(w, wide) == 0
	case ssa.OpIntOr:
		if wide {
			return math.MaxUint64, w == math.MaxUint64
		}
		return word(int32(-1)), uint32(w) == 0xFFFFFFFF
	default:
		return 0, false
	}
}

// identity reports whether w is op's identity operand, decoded by the lane of
// the operands: the i32 lane for an i32 operation, the full word for i64.
func identity(op ssa.Op, w uint64, lane ssa.Type) bool {
	wide := lane == ssa.TypeI64
	switch op {
	case ssa.OpIntAdd, ssa.OpIntSub, ssa.OpIntOr, ssa.OpIntXor, ssa.OpIntShl, ssa.OpIntAShr, ssa.OpIntLShr:
		return mask(w, wide) == 0
	case ssa.OpIntAnd:
		if wide {
			return w == math.MaxUint64
		}
		return uint32(w) == 0xFFFFFFFF
	case ssa.OpIntMul, ssa.OpIntSDiv, ssa.OpIntUDiv:
		return mask(w, wide) == 1
	default:
		return false
	}
}

// shift rewrites a power-of-two mul or unsigned div into a shift, decoding the
// operand by the lane of the operands.
func shift(op ssa.Op, w uint64, lane ssa.Type) (ssa.Op, uint64, bool) {
	n, ok := log2(mask(w, lane == ssa.TypeI64))
	switch op {
	case ssa.OpIntMul:
		return ssa.OpIntShl, n, ok
	case ssa.OpIntUDiv:
		return ssa.OpIntLShr, n, ok
	default:
		return 0, 0, false
	}
}

// mask returns w decoded as its lane: the full word when wide, else its i32 lane.
func mask(w uint64, wide bool) uint64 {
	if wide {
		return w
	}
	return uint64(uint32(w))
}

func log2(v uint64) (uint64, bool) {
	if v < 2 || v&(v-1) != 0 {
		return 0, false
	}
	return uint64(bits.TrailingZeros64(v)), true
}

// eval evaluates operation o over its constant args, whose first has type arg
// and whose result has type res, failing for an operation it does not model or
// one that would trap or leave the target range.
func eval(o ssa.Operation, arg, res ssa.Type, args []uint64) (uint64, bool) {
	switch o.Op {
	case ssa.OpIntAdd, ssa.OpIntSub, ssa.OpIntMul, ssa.OpIntSDiv, ssa.OpIntUDiv, ssa.OpIntSRem, ssa.OpIntURem,
		ssa.OpIntShl, ssa.OpIntAShr, ssa.OpIntLShr, ssa.OpIntXor, ssa.OpIntAnd, ssa.OpIntOr, ssa.OpIntCmp:
		if arg.Lane() == ssa.TypeI64 {
			return evalI64(o, int64(args[0]), int64(args[1]))
		}
		return evalI32(o, int32(uint32(args[0])), int32(uint32(args[1])))
	case ssa.OpFloatAdd, ssa.OpFloatSub, ssa.OpFloatMul, ssa.OpFloatDiv, ssa.OpFloatCmp:
		if arg == ssa.TypeF32 {
			return evalF32(o, math.Float32frombits(uint32(args[0])), math.Float32frombits(uint32(args[1])))
		}
		return evalF64(o, math.Float64frombits(args[0]), math.Float64frombits(args[1]))
	case ssa.OpIntSIToFP:
		switch {
		case arg.Lane() == ssa.TypeI32 && res == ssa.TypeF32:
			return word(float32(int32(uint32(args[0])))), true
		case res == ssa.TypeF32:
			return word(float32(int64(args[0]))), true
		case arg.Lane() == ssa.TypeI64:
			return math.Float64bits(float64(int64(args[0]))), true
		}
	case ssa.OpIntUIToFP:
		switch {
		case arg.Lane() == ssa.TypeI32 && res == ssa.TypeF32:
			return word(float32(uint32(args[0]))), true
		case res == ssa.TypeF32:
			return word(float32(uint64(args[0]))), true
		case arg.Lane() == ssa.TypeI64:
			return math.Float64bits(float64(uint64(args[0]))), true
		}
	case ssa.OpIntWrap:
		return word(int32(int64(args[0]))), true
	case ssa.OpFloatFPTrunc:
		return word(float32(math.Float64frombits(args[0]))), true
	case ssa.OpFloatFPToSISat, ssa.OpFloatFPToUISat:
		f := math.Float64frombits(args[0])
		if arg == ssa.TypeF32 {
			f = float64(math.Float32frombits(uint32(args[0])))
			if res != ssa.TypeI32 {
				return 0, false
			}
		}
		return truncate(o.Op == ssa.OpFloatFPToSISat, res == ssa.TypeI64, f)
	}
	return 0, false
}

// truncate evaluates a float-to-integer conversion, failing when f is NaN or
// outside the target range.
func truncate(signed, wide bool, f float64) (uint64, bool) {
	switch {
	case signed && !wide:
		if math.IsNaN(f) || f < -(1<<31) || f >= 1<<31 {
			return 0, false
		}
		return word(int32(f)), true
	case !signed && !wide:
		if math.IsNaN(f) || f < 0 || f >= 1<<32 {
			return 0, false
		}
		return word(int32(uint32(f))), true
	case signed:
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

func evalI32(o ssa.Operation, a, b int32) (uint64, bool) {
	switch o.Op {
	case ssa.OpIntAdd:
		return word(a + b), true
	case ssa.OpIntSub:
		return word(a - b), true
	case ssa.OpIntMul:
		return word(a * b), true
	case ssa.OpIntSDiv:
		if b == 0 {
			return 0, false
		}
		return word(a / b), true
	case ssa.OpIntUDiv:
		if b == 0 {
			return 0, false
		}
		return word(int32(uint32(a) / uint32(b))), true
	case ssa.OpIntSRem:
		if b == 0 {
			return 0, false
		}
		return word(a % b), true
	case ssa.OpIntURem:
		if b == 0 {
			return 0, false
		}
		return word(int32(uint32(a) % uint32(b))), true
	case ssa.OpIntShl:
		return word(a << (uint32(b) & 0x1F)), true
	case ssa.OpIntAShr:
		return word(a >> (uint32(b) & 0x1F)), true
	case ssa.OpIntLShr:
		return word(int32(uint32(a) >> (uint32(b) & 0x1F))), true
	case ssa.OpIntXor:
		return word(a ^ b), true
	case ssa.OpIntAnd:
		return word(a & b), true
	case ssa.OpIntOr:
		return word(a | b), true
	case ssa.OpIntCmp:
		switch o.Cond {
		case ssa.CondEQ:
			return word(a == b), true
		case ssa.CondNE:
			return word(a != b), true
		case ssa.CondSLT:
			return word(a < b), true
		case ssa.CondULT:
			return word(uint32(a) < uint32(b)), true
		case ssa.CondSGT:
			return word(a > b), true
		case ssa.CondUGT:
			return word(uint32(a) > uint32(b)), true
		case ssa.CondSLE:
			return word(a <= b), true
		case ssa.CondULE:
			return word(uint32(a) <= uint32(b)), true
		case ssa.CondSGE:
			return word(a >= b), true
		case ssa.CondUGE:
			return word(uint32(a) >= uint32(b)), true
		}
	}
	return 0, false
}

func evalI64(o ssa.Operation, a, b int64) (uint64, bool) {
	switch o.Op {
	case ssa.OpIntAdd:
		return uint64(a + b), true
	case ssa.OpIntSub:
		return uint64(a - b), true
	case ssa.OpIntMul:
		return uint64(a * b), true
	case ssa.OpIntSDiv:
		if b == 0 {
			return 0, false
		}
		return uint64(a / b), true
	case ssa.OpIntUDiv:
		if b == 0 {
			return 0, false
		}
		return uint64(a) / uint64(b), true
	case ssa.OpIntSRem:
		if b == 0 {
			return 0, false
		}
		return uint64(a % b), true
	case ssa.OpIntURem:
		if b == 0 {
			return 0, false
		}
		return uint64(a) % uint64(b), true
	case ssa.OpIntShl:
		return uint64(a << (uint64(b) & 0x3F)), true
	case ssa.OpIntAShr:
		return uint64(a >> (uint64(b) & 0x3F)), true
	case ssa.OpIntLShr:
		return uint64(a) >> (uint64(b) & 0x3F), true
	case ssa.OpIntXor:
		return uint64(a ^ b), true
	case ssa.OpIntAnd:
		return uint64(a & b), true
	case ssa.OpIntOr:
		return uint64(a | b), true
	case ssa.OpIntCmp:
		switch o.Cond {
		case ssa.CondEQ:
			return word(a == b), true
		case ssa.CondNE:
			return word(a != b), true
		case ssa.CondSLT:
			return word(a < b), true
		case ssa.CondULT:
			return word(uint64(a) < uint64(b)), true
		case ssa.CondSGT:
			return word(a > b), true
		case ssa.CondUGT:
			return word(uint64(a) > uint64(b)), true
		case ssa.CondSLE:
			return word(a <= b), true
		case ssa.CondULE:
			return word(uint64(a) <= uint64(b)), true
		case ssa.CondSGE:
			return word(a >= b), true
		case ssa.CondUGE:
			return word(uint64(a) >= uint64(b)), true
		}
	}
	return 0, false
}

func evalF32(o ssa.Operation, a, b float32) (uint64, bool) {
	switch o.Op {
	case ssa.OpFloatAdd:
		return word(a + b), true
	case ssa.OpFloatSub:
		return word(a - b), true
	case ssa.OpFloatMul:
		return word(a * b), true
	case ssa.OpFloatDiv:
		if b == 0 {
			return 0, false
		}
		return word(a / b), true
	case ssa.OpFloatCmp:
		switch o.Cond {
		case ssa.CondOEQ:
			return word(a == b), true
		case ssa.CondUNE:
			return word(a != b), true
		case ssa.CondOLT:
			return word(a < b), true
		case ssa.CondOGT:
			return word(a > b), true
		case ssa.CondOLE:
			return word(a <= b), true
		case ssa.CondOGE:
			return word(a >= b), true
		}
	}
	return 0, false
}

func evalF64(o ssa.Operation, a, b float64) (uint64, bool) {
	switch o.Op {
	case ssa.OpFloatAdd:
		return math.Float64bits(a + b), true
	case ssa.OpFloatSub:
		return math.Float64bits(a - b), true
	case ssa.OpFloatMul:
		return math.Float64bits(a * b), true
	case ssa.OpFloatDiv:
		if b == 0 {
			return 0, false
		}
		return math.Float64bits(a / b), true
	case ssa.OpFloatCmp:
		switch o.Cond {
		case ssa.CondOEQ:
			return word(a == b), true
		case ssa.CondUNE:
			return word(a != b), true
		case ssa.CondOLT:
			return word(a < b), true
		case ssa.CondOGT:
			return word(a > b), true
		case ssa.CondOLE:
			return word(a <= b), true
		case ssa.CondOGE:
			return word(a >= b), true
		}
	}
	return 0, false
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
