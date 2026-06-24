package analysis

import (
	"strconv"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// ValueNumberingAnalysis assigns a value number to every value a function
// computes, by abstractly interpreting the operand stack one basic block at a
// time (local value numbering). Equal expressions receive the same number, so
// the second and later computations of a value are reported as redundant. The
// result is reusable by transforms (common-subexpression elimination) and any
// other pass that needs to reason about value identity.
type ValueNumberingAnalysis struct{}

// ValueNumbering is the per-function result: the value number produced at each
// value-producing instruction offset, plus the redundant recomputations found
// within basic blocks.
type ValueNumbering struct {
	Values    map[int]int
	Redundant map[int]Redundancy
}

// Redundancy describes a contiguous, side-effect-free instruction range whose
// result value was already computed earlier in the same basic block. The range
// [Start, End) can be replaced by a load of the value: from Home when a local
// already holds it, otherwise from a fresh slot captured at Def.
type Redundancy struct {
	Start int
	End   int
	Kind  instr.Kind
	Home  int
	Def   int
}

// vslot is one operand-stack entry during interpretation: the value number it
// holds plus the byte range that computed it. pure marks a value built only by
// straight-line, side-effect-free, contiguous instructions, so [Start, End) is
// a self-contained expression safe to delete and reload.
type vslot struct {
	num   int
	start int
	end   int
	kind  instr.Kind
	pure  bool
}

// numbering holds the interpreter state for one function. The expr/home/localNum
// tables are local-value-numbering scope and reset at every block boundary; out
// and next accumulate across the whole function.
type numbering struct {
	out *ValueNumbering

	locals []types.Type

	stack    []vslot
	exprs    map[string]int
	localNum map[int]int
	home     map[int]int
	first    map[int]vslot
	gen      map[string]int

	next int
}

var _ pass.Analysis[*types.Function, *ValueNumbering] = (*ValueNumberingAnalysis)(nil)

func NewValueNumberingAnalysis() *ValueNumberingAnalysis {
	return &ValueNumberingAnalysis{}
}

func newNumbering(fn *types.Function) *numbering {
	var locals []types.Type
	if fn.Typ != nil {
		locals = append(locals, fn.Typ.Params...)
	}
	locals = append(locals, fn.Locals...)
	return &numbering{
		out:    &ValueNumbering{Values: map[int]int{}, Redundant: map[int]Redundancy{}},
		locals: locals,
	}
}

func (a *ValueNumberingAnalysis) Run(m *pass.Manager, fn *types.Function) (*ValueNumbering, error) {
	blocks, err := pass.GetResult[[]*BasicBlock](m, fn)
	if err != nil {
		return nil, err
	}

	n := newNumbering(fn)
	for _, blk := range blocks {
		n.reset()
		for ip := blk.Start; ip < blk.End; {
			inst := instr.Instruction(fn.Code[ip:])
			if !n.step(ip, inst) {
				break
			}
			ip += inst.Width()
		}
	}
	return n.out, nil
}

// step folds one instruction into the abstract stack. It returns false when the
// effect is indeterminate (calls, throws, variable-arity constructors), which
// ends value numbering for the rest of the block.
func (n *numbering) step(ip int, inst instr.Instruction) bool {
	op := inst.Opcode()
	end := ip + inst.Width()

	switch op {
	case instr.NOP, instr.UNREACHABLE, instr.BR:
		return true
	case instr.DROP, instr.BR_IF, instr.BR_TABLE:
		n.pop()
		return true

	case instr.LOCAL_GET:
		slot := int(inst.Operand(0))
		num, ok := n.localNum[slot]
		if !ok {
			num = n.fresh()
			n.localNum[slot] = num
		}
		n.home[num] = slot
		n.push(vslot{num: num, start: ip, end: end, kind: n.kindOf(slot), pure: true})
		n.out.Values[ip] = num
		return true
	case instr.LOCAL_SET:
		slot := int(inst.Operand(0))
		s := n.pop()
		if old, ok := n.localNum[slot]; ok && n.home[old] == slot {
			delete(n.home, old)
		}
		n.localNum[slot] = s.num
		n.home[s.num] = slot
		return true
	case instr.LOCAL_TEE:
		slot := int(inst.Operand(0))
		if len(n.stack) == 0 {
			return false
		}
		s := n.stack[len(n.stack)-1]
		n.localNum[slot] = s.num
		n.home[s.num] = slot
		return true

	case instr.GLOBAL_GET:
		n.pushLeaf(ip, end, n.leaf("g", int(inst.Operand(0))), instr.KindAny)
		return true
	case instr.GLOBAL_SET:
		n.pop()
		n.gen["g:"+strconv.Itoa(int(inst.Operand(0)))]++
		return true
	case instr.GLOBAL_TEE:
		n.gen["g:"+strconv.Itoa(int(inst.Operand(0)))]++
		return true
	case instr.UPVAL_GET:
		n.pushLeaf(ip, end, n.leaf("u", int(inst.Operand(0))), instr.KindAny)
		return true
	case instr.UPVAL_SET:
		n.pop()
		n.gen["u:"+strconv.Itoa(int(inst.Operand(0)))]++
		return true

	case instr.CONST_GET:
		n.pushLeaf(ip, end, "c:"+strconv.Itoa(int(inst.Operand(0))), instr.KindAny)
		return true
	case instr.I32_CONST, instr.I64_CONST, instr.F32_CONST, instr.F64_CONST:
		key := strconv.Itoa(int(op)) + ":" + strconv.FormatUint(inst.Operand(0), 16)
		n.pushLeaf(ip, end, key, instr.TypeOf(op).Push[0])
		return true
	case instr.REF_NULL:
		n.pushLeaf(ip, end, "refnull", instr.KindRef)
		return true

	case instr.DUP:
		if len(n.stack) == 0 {
			return false
		}
		s := n.stack[len(n.stack)-1]
		n.push(vslot{num: s.num, start: ip, end: end, kind: s.kind})
		n.out.Values[ip] = s.num
		return true
	case instr.SWAP:
		if len(n.stack) < 2 {
			return false
		}
		top := len(n.stack) - 1
		n.stack[top], n.stack[top-1] = n.stack[top-1], n.stack[top]
		n.stack[top].pure = false
		n.stack[top-1].pure = false
		return true
	case instr.SELECT:
		if len(n.stack) < 3 {
			return false
		}
		n.pop()
		n.pop()
		n.pop()
		num := n.fresh()
		n.push(vslot{num: num, start: ip, end: end, kind: instr.KindAny})
		n.out.Values[ip] = num
		return true

	case instr.CALL, instr.RETURN_CALL, instr.RETURN, instr.THROW,
		instr.STRUCT_NEW, instr.MAP_NEW, instr.CLOSURE_NEW, instr.EXT,
		instr.YIELD, instr.RESUME:
		return false
	}

	if isPure(op) {
		return n.pure(ip, end, inst)
	}

	t := instr.TypeOf(op)
	if t.Pop == nil && t.Push == nil {
		return false
	}
	if len(n.stack) < len(t.Pop) {
		return false
	}
	for range t.Pop {
		n.pop()
	}
	for _, k := range t.Push {
		num := n.fresh()
		n.push(vslot{num: num, start: ip, end: end, kind: k})
		n.out.Values[ip] = num
	}
	return true
}

// pure folds a side-effect-free arithmetic instruction: it hash-conses the
// expression key, records the first definition, and flags later identical
// computations as redundant when the consumed operands form a contiguous range.
func (n *numbering) pure(ip, end int, inst instr.Instruction) bool {
	op := inst.Opcode()
	t := instr.TypeOf(op)
	if len(n.stack) < len(t.Pop) {
		return false
	}

	base := len(n.stack) - len(t.Pop)
	start := n.stack[base].start
	contig := true
	inputs := make([]int, len(t.Pop))
	for i := range t.Pop {
		s := n.stack[base+i]
		inputs[i] = s.num
		next := ip
		if i+1 < len(t.Pop) {
			next = n.stack[base+i+1].start
		}
		if !s.pure || s.end != next {
			contig = false
		}
	}
	n.stack = n.stack[:base]

	key := n.pureKey(op, inputs)
	kind := t.Push[0]
	num, seen := n.exprs[key]
	if !seen {
		num = n.fresh()
		n.exprs[key] = num
		n.first[num] = vslot{start: start, end: end, kind: kind, pure: contig}
	} else if contig {
		if f, ok := n.first[num]; ok && f.pure {
			n.out.Redundant[ip] = Redundancy{Start: start, End: end, Kind: f.kind, Home: n.holder(num), Def: f.end}
		}
	}

	n.push(vslot{num: num, start: start, end: end, kind: kind, pure: contig})
	n.out.Values[ip] = num
	return true
}

func (n *numbering) reset() {
	n.stack = n.stack[:0]
	n.exprs = map[string]int{}
	n.localNum = map[int]int{}
	n.home = map[int]int{}
	n.first = map[int]vslot{}
	n.gen = map[string]int{}
}

func (n *numbering) push(s vslot) {
	n.stack = append(n.stack, s)
}

func (n *numbering) pop() vslot {
	if len(n.stack) == 0 {
		return vslot{num: n.fresh()}
	}
	s := n.stack[len(n.stack)-1]
	n.stack = n.stack[:len(n.stack)-1]
	return s
}

func (n *numbering) pushLeaf(ip, end int, key string, kind instr.Kind) {
	num, ok := n.exprs[key]
	if !ok {
		num = n.fresh()
		n.exprs[key] = num
	}
	n.push(vslot{num: num, start: ip, end: end, kind: kind, pure: true})
	n.out.Values[ip] = num
}

func (n *numbering) fresh() int {
	num := n.next
	n.next++
	return num
}

// holder returns a local slot that currently holds value num, or -1 when no
// live local does.
func (n *numbering) holder(num int) int {
	if slot, ok := n.home[num]; ok && n.localNum[slot] == num {
		return slot
	}
	return -1
}

// leaf builds a version-qualified key for a mutable load (global, upvalue) so a
// store to the same slot ends its reuse window.
func (n *numbering) leaf(space string, index int) string {
	id := space + ":" + strconv.Itoa(index)
	return id + ":" + strconv.Itoa(n.gen[id])
}

func (n *numbering) pureKey(op instr.Opcode, inputs []int) string {
	if commutative(op) && len(inputs) == 2 && inputs[0] > inputs[1] {
		inputs[0], inputs[1] = inputs[1], inputs[0]
	}
	var sb strings.Builder
	sb.WriteString(strconv.Itoa(int(op)))
	for _, in := range inputs {
		sb.WriteByte('#')
		sb.WriteString(strconv.Itoa(in))
	}
	return sb.String()
}

func (n *numbering) kindOf(slot int) instr.Kind {
	if slot < 0 || slot >= len(n.locals) || n.locals[slot] == nil {
		return instr.KindAny
	}
	return n.locals[slot].Kind()
}

// isPure reports whether op is a deterministic, side-effect-free, non-allocating
// value computation: the numeric ALU/compare/convert ops (whose operands and
// result are all numeric) plus the reference comparisons.
func isPure(op instr.Opcode) bool {
	switch op {
	case instr.REF_EQ, instr.REF_NE, instr.REF_IS_NULL:
		return true
	}
	t := instr.TypeOf(op)
	if len(t.Pop) == 0 || len(t.Push) != 1 || !t.Push[0].IsNumeric() {
		return false
	}
	for _, k := range t.Pop {
		if !k.IsNumeric() {
			return false
		}
	}
	return true
}

// commutative reports whether op's two operands may be reordered without
// changing its result, so the value key can canonicalize their order.
func commutative(op instr.Opcode) bool {
	switch op {
	case instr.I32_ADD, instr.I32_MUL, instr.I32_AND, instr.I32_OR, instr.I32_XOR,
		instr.I32_EQ, instr.I32_NE,
		instr.I64_ADD, instr.I64_MUL, instr.I64_AND, instr.I64_OR, instr.I64_XOR,
		instr.I64_EQ, instr.I64_NE,
		instr.F32_ADD, instr.F32_MUL, instr.F32_EQ, instr.F32_NE,
		instr.F64_ADD, instr.F64_MUL, instr.F64_EQ, instr.F64_NE,
		instr.REF_EQ, instr.REF_NE:
		return true
	default:
		return false
	}
}
