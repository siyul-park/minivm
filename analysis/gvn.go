package analysis

import (
	"strconv"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// GlobalValueNumberingAnalysis extends local value numbering across basic-block
// boundaries. It assigns every value-producing instruction a function-wide
// value number, then runs an available-expression dataflow over those numbers
// (intersection across predecessors, with an optimistic ⊤ initialization that
// converges through loop back-edges) to find expressions that are recomputed
// even though an equal value was already produced on every path that reaches
// them. Each such recomputation is reported as redundant, reusing the same
// Redundancy shape as the block-local analysis so the transform can eliminate
// both with one mechanism.
//
// Cross-block value identity is tracked conservatively: only constants, the
// constant pool, the null reference, and locals that are never reassigned carry
// a stable global key. Any value built from a mutable load (global, upvalue,
// heap) or a reassigned local is opaque across blocks and can still be matched
// only within its own block by the embedded local numbering. This is sound —
// an opaque value never matches another — and it is the precision the common
// cases (expressions over parameters and SSA-like temporaries, loop-invariant
// recomputation) need.
type GlobalValueNumberingAnalysis struct{}

// GlobalValueNumbering is the per-function result. Redundant maps each redundant
// recomputation's finalizing offset to how it is eliminated; Defs maps a captured
// value's group id to the definition offsets that must receive a LOCAL_TEE so the
// value is available to reload at every redundant use of that group.
type GlobalValueNumbering struct {
	Redundant map[int]Redundancy
	Defs      map[int][]int
}

// Redundancy describes a contiguous, side-effect-free instruction range whose
// result value was already produced on every path that reaches it. The range
// [Start, End) can be replaced by a load of the value: from Home when a local
// already holds it, otherwise from a fresh slot captured at every definition
// recorded in Defs[Def], where Def is the value's group id.
type Redundancy struct {
	Start int
	End   int
	Kind  instr.Kind
	Home  int
	Def   int
}

// gslot is one operand-stack entry: its block-local number (within-block
// identity, exactly the local analysis' semantics), its global group id (or -1
// when the value is opaque across blocks), the byte range that computed it, and
// whether that range is a self-contained, side-effect-free expression.
type gslot struct {
	num   int
	gid   int
	start int
	end   int
	kind  instr.Kind
	pure  bool
}

// gcompute records the first computation of a global group id within a block:
// the finalizing offset, the expression byte range, its kind, and whether the
// range is contiguous (safe to delete and reload).
type gcompute struct {
	ip     int
	start  int
	end    int
	kind   instr.Kind
	contig bool
}

// gnumbering holds the interpreter and dataflow state for one function. The
// keys table, gen sets, and id counters accumulate across the whole function;
// the stack and the block-local numbering tables reset at every block.
type gnumbering struct {
	out *GlobalValueNumbering

	blocks []*BasicBlock
	locals []types.Type
	stable []bool

	keys map[string]int
	gen  []map[int]gcompute

	stack    []gslot
	exprs    map[string]int
	localNum map[int]int
	home     map[int]int
	first    map[int]gslot
	ver      map[string]int

	next  int
	seq   int
	block int
}

var _ pass.Analysis[*types.Function, *GlobalValueNumbering] = (*GlobalValueNumberingAnalysis)(nil)

func NewGlobalValueNumberingAnalysis() *GlobalValueNumberingAnalysis {
	return &GlobalValueNumberingAnalysis{}
}

func (a *GlobalValueNumberingAnalysis) Run(m *pass.Manager, fn *types.Function) (*GlobalValueNumbering, error) {
	blocks, err := pass.GetResult[[]*BasicBlock](m, fn)
	if err != nil {
		return nil, err
	}

	g := newGNumbering(fn, blocks)
	for block, blk := range blocks {
		g.reset(block)
		for ip := blk.Start; ip < blk.End; {
			inst := instr.Instruction(fn.Code[ip:])
			if !g.step(ip, inst) {
				break
			}
			ip += inst.Width()
		}
	}
	g.available()
	return g.out, nil
}

func newGNumbering(fn *types.Function, blocks []*BasicBlock) *gnumbering {
	var locals []types.Type
	if fn.Typ != nil {
		locals = append(locals, fn.Typ.Params...)
	}
	locals = append(locals, fn.Locals...)

	stable := make([]bool, len(locals))
	for i := range stable {
		stable[i] = true
	}
	for ip := 0; ip < len(fn.Code); {
		inst := instr.Instruction(fn.Code[ip:])
		switch inst.Opcode() {
		case instr.LOCAL_SET, instr.LOCAL_TEE:
			if slot := int(inst.Operand(0)); slot < len(stable) {
				stable[slot] = false
			}
		}
		ip += inst.Width()
	}

	return &gnumbering{
		out:    &GlobalValueNumbering{Redundant: map[int]Redundancy{}, Defs: map[int][]int{}},
		blocks: blocks,
		locals: locals,
		stable: stable,
		keys:   map[string]int{},
		gen:    make([]map[int]gcompute, len(blocks)),
	}
}

// available converts every block-first computation of an already-available value
// into a cross-block redundancy and records the rest as definitions to capture.
func (g *gnumbering) available() {
	in := g.solve()
	for block, gen := range g.gen {
		for gid, c := range gen {
			if in[block][gid] {
				// Available on every path here, so this materialization is a
				// redundant recomputation: replace it with a load, never tee it.
				if c.contig {
					g.out.Redundant[c.ip] = Redundancy{Start: c.start, End: c.end, Kind: c.kind, Home: -1, Def: gid}
				}
				continue
			}
			// First materialization on some path: capture it for later reloads.
			g.define(gid, c.end)
		}
	}
}

// solve computes AVAIL_in per block by fixpoint: AVAIL_in is the intersection of
// predecessors' AVAIL_out, AVAIL_out is AVAIL_in plus the block's generated ids.
// Non-entry blocks start optimistically at the universe of all generated ids so
// the intersection converges correctly across loop back-edges.
func (g *gnumbering) solve() []map[int]bool {
	universe := map[int]bool{}
	for _, gen := range g.gen {
		for gid := range gen {
			universe[gid] = true
		}
	}

	out := make([]map[int]bool, len(g.blocks))
	for block := range g.blocks {
		out[block] = map[int]bool{}
		if len(g.blocks[block].Preds) != 0 {
			for gid := range universe {
				out[block][gid] = true
			}
		}
	}

	in := make([]map[int]bool, len(g.blocks))
	for changed := true; changed; {
		changed = false
		for block, blk := range g.blocks {
			in[block] = g.meet(out, blk.Preds)
			next := map[int]bool{}
			for gid := range in[block] {
				next[gid] = true
			}
			for gid := range g.gen[block] {
				next[gid] = true
			}
			if len(next) != len(out[block]) {
				out[block] = next
				changed = true
			}
		}
	}
	return in
}

// meet intersects the predecessors' available sets; an empty predecessor list
// (entry, catch, or dead block) yields the empty set.
func (g *gnumbering) meet(out []map[int]bool, preds []int) map[int]bool {
	if len(preds) == 0 {
		return map[int]bool{}
	}
	in := map[int]bool{}
	for gid := range out[preds[0]] {
		in[gid] = true
	}
	for _, p := range preds[1:] {
		for gid := range in {
			if !out[p][gid] {
				delete(in, gid)
			}
		}
	}
	return in
}

// define records that group id is materialized at off (the byte after the
// producing expression), where a LOCAL_TEE captures it for later reloads.
func (g *gnumbering) define(gid, off int) {
	for _, o := range g.out.Defs[gid] {
		if o == off {
			return
		}
	}
	g.out.Defs[gid] = append(g.out.Defs[gid], off)
}

// step folds one instruction into the abstract stack, mirroring the block-local
// analysis but also threading the global id of each value. It returns false when
// the effect is indeterminate, ending numbering for the rest of the block.
func (g *gnumbering) step(ip int, inst instr.Instruction) bool {
	op := inst.Opcode()
	end := ip + inst.Width()

	switch op {
	case instr.NOP, instr.UNREACHABLE, instr.BR:
		return true
	case instr.DROP, instr.BR_IF, instr.BR_TABLE:
		g.pop()
		return true

	case instr.LOCAL_GET:
		slot := int(inst.Operand(0))
		num, ok := g.localNum[slot]
		if !ok {
			num = g.fresh()
			g.localNum[slot] = num
		}
		g.home[num] = slot
		key := ""
		if slot < len(g.stable) && g.stable[slot] {
			key = "L:" + strconv.Itoa(slot)
		}
		g.push(gslot{num: num, gid: g.idOf(key), start: ip, end: end, kind: g.kindOf(slot), pure: true})
		return true
	case instr.LOCAL_SET:
		slot := int(inst.Operand(0))
		s := g.pop()
		if old, ok := g.localNum[slot]; ok && g.home[old] == slot {
			delete(g.home, old)
		}
		g.localNum[slot] = s.num
		g.home[s.num] = slot
		return true
	case instr.LOCAL_TEE:
		slot := int(inst.Operand(0))
		if len(g.stack) == 0 {
			return false
		}
		s := g.stack[len(g.stack)-1]
		g.localNum[slot] = s.num
		g.home[s.num] = slot
		return true

	case instr.GLOBAL_GET:
		g.pushLeaf(ip, end, g.leaf("g", int(inst.Operand(0))), "", instr.KindAny)
		return true
	case instr.GLOBAL_SET:
		g.pop()
		g.ver["g:"+strconv.Itoa(int(inst.Operand(0)))]++
		return true
	case instr.GLOBAL_TEE:
		g.ver["g:"+strconv.Itoa(int(inst.Operand(0)))]++
		return true
	case instr.UPVAL_GET:
		g.pushLeaf(ip, end, g.leaf("u", int(inst.Operand(0))), "", instr.KindAny)
		return true
	case instr.UPVAL_SET:
		g.pop()
		g.ver["u:"+strconv.Itoa(int(inst.Operand(0)))]++
		return true

	case instr.CONST_GET:
		key := "c:" + strconv.Itoa(int(inst.Operand(0)))
		g.pushLeaf(ip, end, key, key, instr.KindAny)
		return true
	case instr.I32_CONST, instr.I64_CONST, instr.F32_CONST, instr.F64_CONST:
		key := strconv.Itoa(int(op)) + ":" + strconv.FormatUint(inst.Operand(0), 16)
		g.pushLeaf(ip, end, key, key, instr.TypeOf(op).Push[0])
		return true
	case instr.REF_NULL:
		g.pushLeaf(ip, end, "refnull", "refnull", instr.KindRef)
		return true

	case instr.DUP:
		if len(g.stack) == 0 {
			return false
		}
		s := g.stack[len(g.stack)-1]
		g.push(gslot{num: s.num, gid: s.gid, start: ip, end: end, kind: s.kind})
		return true
	case instr.SWAP:
		if len(g.stack) < 2 {
			return false
		}
		top := len(g.stack) - 1
		g.stack[top], g.stack[top-1] = g.stack[top-1], g.stack[top]
		g.stack[top].pure = false
		g.stack[top-1].pure = false
		return true
	case instr.SELECT:
		if len(g.stack) < 3 {
			return false
		}
		g.pop()
		g.pop()
		g.pop()
		g.push(gslot{num: g.fresh(), gid: -1, start: ip, end: end, kind: instr.KindAny})
		return true

	case instr.CALL, instr.RETURN_CALL, instr.RETURN, instr.THROW,
		instr.STRUCT_NEW, instr.MAP_NEW, instr.CLOSURE_NEW,
		instr.YIELD, instr.RESUME:
		return false
	}

	if isPure(op) {
		return g.pure(ip, end, inst)
	}

	t := instr.TypeOf(op)
	if t.Pop == nil && t.Push == nil {
		return false
	}
	if len(g.stack) < len(t.Pop) {
		return false
	}
	for range t.Pop {
		g.pop()
	}
	for _, k := range t.Push {
		g.push(gslot{num: g.fresh(), gid: -1, start: ip, end: end, kind: k})
	}
	return true
}

// pure folds a side-effect-free arithmetic instruction. It hash-conses the
// block-local number for within-block redundancy (with a usable local home) and
// the global id for cross-block redundancy, records the first computation of a
// new global id in the block's gen set, and flags later identical computations.
func (g *gnumbering) pure(ip, end int, inst instr.Instruction) bool {
	op := inst.Opcode()
	t := instr.TypeOf(op)
	if len(g.stack) < len(t.Pop) {
		return false
	}

	base := len(g.stack) - len(t.Pop)
	start := g.stack[base].start
	contig := true
	nums := make([]int, len(t.Pop))
	ids := make([]int, len(t.Pop))
	for i := range t.Pop {
		s := g.stack[base+i]
		nums[i] = s.num
		ids[i] = s.gid
		next := ip
		if i+1 < len(t.Pop) {
			next = g.stack[base+i+1].start
		}
		if !s.pure || s.end != next {
			contig = false
		}
	}
	g.stack = g.stack[:base]

	kind := t.Push[0]
	gid := g.idOf(g.globalKey(op, ids))
	num, seen := g.exprs[g.numKey(op, nums)]
	if !seen {
		num = g.fresh()
		g.exprs[g.numKey(op, nums)] = num
		g.first[num] = gslot{start: start, end: end, kind: kind, pure: contig}
		if gid >= 0 {
			if _, ok := g.gen[g.block][gid]; !ok {
				g.gen[g.block][gid] = gcompute{ip: ip, start: start, end: end, kind: kind, contig: contig}
			}
		}
	} else if contig {
		if f, ok := g.first[num]; ok && f.pure {
			home := g.holder(num)
			group := gid
			if group < 0 {
				// Opaque value: matchable only within this block, so capture it
				// from this block's own first computation. Stable (non-opaque)
				// values are captured globally by available() at their true defs.
				group = g.idOf("⊥:" + strconv.Itoa(g.block) + ":" + strconv.Itoa(f.start))
				if home < 0 {
					g.define(group, f.end)
				}
			}
			g.out.Redundant[ip] = Redundancy{Start: start, End: end, Kind: f.kind, Home: home, Def: group}
		}
	}

	g.push(gslot{num: num, gid: gid, start: start, end: end, kind: kind, pure: contig})
	return true
}

func (g *gnumbering) reset(block int) {
	g.block = block
	g.gen[block] = map[int]gcompute{}
	g.stack = g.stack[:0]
	g.exprs = map[string]int{}
	g.localNum = map[int]int{}
	g.home = map[int]int{}
	g.first = map[int]gslot{}
	g.ver = map[string]int{}
}

func (g *gnumbering) push(s gslot) {
	g.stack = append(g.stack, s)
}

func (g *gnumbering) pop() gslot {
	if len(g.stack) == 0 {
		return gslot{num: g.fresh(), gid: -1}
	}
	s := g.stack[len(g.stack)-1]
	g.stack = g.stack[:len(g.stack)-1]
	return s
}

func (g *gnumbering) pushLeaf(ip, end int, numKey, globalKey string, kind instr.Kind) {
	num, ok := g.exprs[numKey]
	if !ok {
		num = g.fresh()
		g.exprs[numKey] = num
	}
	g.push(gslot{num: num, gid: g.idOf(globalKey), start: ip, end: end, kind: kind, pure: true})
}

func (g *gnumbering) fresh() int {
	num := g.seq
	g.seq++
	return num
}

// idOf interns a global key into a stable group id, or returns -1 for the empty
// key (an opaque value with no cross-block identity).
func (g *gnumbering) idOf(key string) int {
	if key == "" {
		return -1
	}
	if id, ok := g.keys[key]; ok {
		return id
	}
	id := g.next
	g.next++
	g.keys[key] = id
	return id
}

// holder returns a local slot that currently holds block-local number num, or
// -1 when no live local does.
func (g *gnumbering) holder(num int) int {
	if slot, ok := g.home[num]; ok && g.localNum[slot] == num {
		return slot
	}
	return -1
}

// leaf builds a version-qualified block-local key for a mutable load so a store
// to the same slot ends its reuse window within the block.
func (g *gnumbering) leaf(space string, index int) string {
	id := space + ":" + strconv.Itoa(index)
	return id + ":" + strconv.Itoa(g.ver[id])
}

func (g *gnumbering) numKey(op instr.Opcode, nums []int) string {
	if commutative(op) && len(nums) == 2 && nums[0] > nums[1] {
		nums[0], nums[1] = nums[1], nums[0]
	}
	var sb strings.Builder
	sb.WriteString(strconv.Itoa(int(op)))
	for _, in := range nums {
		sb.WriteByte('#')
		sb.WriteString(strconv.Itoa(in))
	}
	return sb.String()
}

// globalKey builds the cross-block key for a pure op from its inputs' group ids,
// returning "" when any input is opaque (the result is then opaque too).
func (g *gnumbering) globalKey(op instr.Opcode, ids []int) string {
	for _, id := range ids {
		if id < 0 {
			return ""
		}
	}
	if commutative(op) && len(ids) == 2 && ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	var sb strings.Builder
	sb.WriteByte('o')
	sb.WriteString(strconv.Itoa(int(op)))
	for _, id := range ids {
		sb.WriteByte('#')
		sb.WriteString(strconv.Itoa(id))
	}
	return sb.String()
}

func (g *gnumbering) kindOf(slot int) instr.Kind {
	if slot < 0 || slot >= len(g.locals) || g.locals[slot] == nil {
		return instr.KindAny
	}
	return g.locals[slot].Kind()
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
