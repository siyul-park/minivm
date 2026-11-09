package asm

import (
	"fmt"
	"math"
	"slices"
)

// Loc is where a virtual register lives for its whole life once Build has
// allocated: in Reg or, when Spilled, in spill slot Slot.
type Loc struct {
	Reg     PReg
	Slot    int
	Spilled bool
}

// allocator assigns whole-life locations by linear scan. Spilled values are
// rewritten around each use/definition, then allocation repeats.
type allocator struct {
	frame     Frame
	insts     []Instruction
	labels    map[Label]int
	usable    map[value]bool
	registers [2][]PReg

	locs  map[VReg]Loc
	tiny  map[VReg]bool
	slots int
	next  int32
}

// value is what an interval belongs to: a virtual register, or an
// allocatable physical register a row names, whose interval is fixed.
type value struct {
	virtual bool
	id      int32
	typ     RegType
	width   RegWidth
}

// interval is the range of rows a value occupies, the disjoint row ranges
// within it where the value is live, and, for a virtual value, whether it
// is live across a call.
type interval struct {
	value      value
	start, end int
	ranges     [][2]int
	call       bool
}

// partner is the value on the other side of a copy at row.
type partner struct {
	value value
	row   int
}

func newAllocator(frame Frame, insts []Instruction, labels map[Label]int, reserved map[PReg]bool, slots int) *allocator {
	a := &allocator{
		frame:  frame,
		insts:  insts,
		labels: labels,
		usable: map[value]bool{},
		locs:   map[VReg]Loc{},
		tiny:   map[VReg]bool{},
		slots:  slots,
	}
	for _, typ := range []RegType{RegTypeInt, RegTypeFloat} {
		registers := slices.Clone(frame.Registers(typ))
		a.registers[typ] = slices.DeleteFunc(registers, func(r PReg) bool { return reserved[r] })
		for _, r := range a.registers[typ] {
			a.usable[physical(r)] = true
		}
	}
	for _, inst := range insts {
		for _, op := range inst.operands() {
			if v, ok := register(*op).(VReg); ok {
				a.next = max(a.next, v.ID()+1)
			}
		}
	}
	return a
}

// allocate runs scans and rewrites until one scan assigns every virtual
// register, then rewrites the rows to physical registers.
func (a *allocator) allocate() ([]Instruction, map[Label]int, error) {
	for {
		assigned, spilled, err := a.scan(a.intervals())
		if err != nil {
			return nil, nil, err
		}
		if len(spilled) == 0 {
			a.assign(assigned)
			return a.insts, a.labels, nil
		}
		a.rewrite(spilled)
	}
}

// intervals computes each value's whole-life range and the rows within it
// where the value is live: a value holds one register for its whole life,
// but another value may share it through the holes.
func (a *allocator) intervals() []interval {
	blocks, out := a.liveness()
	rows := map[value][]int{}
	calls := map[value]bool{}
	for b, blk := range blocks {
		after := out[b]
		for row := blk[1] - 1; row >= blk[0]; row-- {
			reads, writes := a.access(row)
			live := map[value]bool{}
			for v := range after {
				live[v] = true
			}
			for _, v := range writes {
				rows[v] = append(rows[v], row)
				delete(live, v)
			}
			for _, v := range reads {
				live[v] = true
			}
			for v := range live {
				rows[v] = append(rows[v], row)
				if v.virtual && after[v] && a.frame.Flow(a.insts[row]) == FlowCall {
					calls[v] = true
				}
			}
			after = live
		}
	}

	result := make([]interval, 0, len(rows))
	for v, occupied := range rows {
		slices.Sort(occupied)
		iv := interval{value: v, call: calls[v]}
		for _, row := range slices.Compact(occupied) {
			if n := len(iv.ranges); n > 0 && iv.ranges[n-1][1] == row-1 {
				iv.ranges[n-1][1] = row
			} else {
				iv.ranges = append(iv.ranges, [2]int{row, row})
			}
		}
		iv.start, iv.end = iv.ranges[0][0], iv.ranges[len(iv.ranges)-1][1]
		result = append(result, iv)
	}
	slices.SortFunc(result, func(x, y interval) int {
		if x.start != y.start {
			return x.start - y.start
		}
		return x.value.compare(y.value)
	})
	return result
}

// liveness returns the blocks and, for each, the values live on exit from
// it, by iterating the backward dataflow to a fixed point.
func (a *allocator) liveness() ([][2]int, []map[value]bool) {
	blocks, succs := a.blocks()

	gen := make([]map[value]bool, len(blocks))
	kill := make([]map[value]bool, len(blocks))
	for b, blk := range blocks {
		gen[b], kill[b] = map[value]bool{}, map[value]bool{}
		for row := blk[0]; row < blk[1]; row++ {
			reads, writes := a.access(row)
			for _, v := range reads {
				if !kill[b][v] {
					gen[b][v] = true
				}
			}
			for _, v := range writes {
				kill[b][v] = true
			}
		}
	}

	in := make([]map[value]bool, len(blocks))
	out := make([]map[value]bool, len(blocks))
	for b := range blocks {
		in[b], out[b] = map[value]bool{}, map[value]bool{}
	}
	for changed := true; changed; {
		changed = false
		for b := len(blocks) - 1; b >= 0; b-- {
			for _, s := range succs[b] {
				for v := range in[s] {
					if !out[b][v] {
						out[b][v] = true
						changed = true
					}
				}
			}
			for v := range gen[b] {
				if !in[b][v] {
					in[b][v] = true
					changed = true
				}
			}
			for v := range out[b] {
				if !kill[b][v] && !in[b][v] {
					in[b][v] = true
					changed = true
				}
			}
		}
	}
	return blocks, out
}

// blocks splits the rows at every label and after every row that leaves,
// returning each block's [start, end) and its successor blocks.
func (a *allocator) blocks() ([][2]int, [][]int) {
	if len(a.insts) == 0 {
		return nil, nil
	}
	starts := map[int]bool{0: true}
	for _, pos := range a.labels {
		starts[pos] = true
	}
	for row, inst := range a.insts {
		switch a.frame.Flow(inst) {
		case FlowJump, FlowBranch, FlowEnd:
			starts[row+1] = true
		}
	}
	delete(starts, len(a.insts))

	var blocks [][2]int
	at := map[int]int{}
	for row := range a.insts {
		if starts[row] {
			at[row] = len(blocks)
			blocks = append(blocks, [2]int{row, row})
		}
		blocks[len(blocks)-1][1] = row + 1
	}

	succs := make([][]int, len(blocks))
	for b, blk := range blocks {
		last := a.insts[blk[1]-1]
		flow := a.frame.Flow(last)
		if flow != FlowEnd && flow != FlowJump && blk[1] < len(a.insts) {
			succs[b] = append(succs[b], at[blk[1]])
		}
		if flow == FlowJump || flow == FlowBranch {
			if lbl, ok := last.Src2.(LabelOperand); ok {
				if pos, bound := a.labels[lbl.ID]; bound {
					succs[b] = append(succs[b], at[pos])
				}
			}
		}
	}
	return blocks, succs
}

// access lists the values row reads and the values it writes.
func (a *allocator) access(row int) (reads, writes []value) {
	inst := a.insts[row]
	written := a.frame.Writes(inst)
	for i, op := range inst.operands() {
		v, ok := a.value(register(*op))
		if !ok {
			continue
		}
		if _, mem := (*op).(MemOperand); !mem && written[i] {
			writes = append(writes, v)
		} else {
			reads = append(reads, v)
		}
	}
	return reads, writes
}

// scan assigns registers in start order; values whose live rows never meet
// share a register. Call-live values spill first; under pressure it evicts
// the longest-lived non-tiny value live alongside the one being assigned.
func (a *allocator) scan(ivs []interval) (map[value]PReg, []value, error) {
	var spilled []value
	fixed := map[value][]interval{}
	by := map[value]interval{}
	for _, iv := range ivs {
		by[iv.value] = iv
		if iv.value.virtual && iv.call {
			spilled = append(spilled, iv.value)
		}
		if !iv.value.virtual {
			fixed[iv.value] = append(fixed[iv.value], iv)
		}
	}
	if len(spilled) > 0 {
		return nil, spilled, nil
	}

	assigned := map[value]PReg{}
	var active []interval
	for _, iv := range ivs {
		if !iv.value.virtual {
			continue
		}
		active = slices.DeleteFunc(active, func(o interval) bool { return o.end < iv.start })
		for {
			r, ok := a.free(iv, active, assigned, fixed, by)
			if ok {
				assigned[iv.value] = r
				active = append(active, iv)
				break
			}
			victim, ok := a.victim(iv, active)
			if !ok {
				return nil, nil, fmt.Errorf("%w: row %d", ErrNoRegistersAvailable, iv.start)
			}
			spilled = append(spilled, victim.value)
			if victim.value == iv.value {
				break
			}
			active = slices.DeleteFunc(active, func(o interval) bool { return o.value == victim.value })
			delete(assigned, victim.value)
		}
	}
	return assigned, spilled, nil
}

// free returns the register for iv: one that the value on the other side of
// a copy at iv's first or last row holds, when iv can share it there, else
// the first register of iv's bank that fits.
func (a *allocator) free(iv interval, active []interval, assigned map[value]PReg, fixed map[value][]interval, by map[value]interval) (PReg, bool) {
	for _, p := range a.partners(iv, by) {
		id, ok := int32(0), false
		if p.value.virtual {
			var r PReg
			if r, ok = assigned[p.value]; ok {
				id = int32(r.ID())
			}
		} else {
			id, ok = p.value.id, true
		}
		if !ok {
			continue
		}
		for _, r := range a.registers[iv.value.typ] {
			if int32(r.ID()) == id && a.fits(r, iv.without(p.row), active, assigned, fixed, p.value) {
				return r, true
			}
		}
	}
	for _, r := range a.registers[iv.value.typ] {
		if a.fits(r, iv, active, assigned, fixed, value{}) {
			return r, true
		}
	}
	return 0, false
}

// fits reports whether no active interval live at one of span's rows holds r
// and no fixed interval occupies r anywhere within span. A fixed interval of
// partner, the copy's other side, conflicts only where its live rows meet
// span's.
func (a *allocator) fits(r PReg, span interval, active []interval, assigned map[value]PReg, fixed map[value][]interval, partner value) bool {
	for _, o := range active {
		if h, ok := assigned[o.value]; ok && h.ID() == r.ID() && o.value.typ == span.value.typ && span.overlaps(o) {
			return false
		}
	}
	for _, f := range fixed[physical(r)] {
		if f.value == partner {
			if span.overlaps(f) {
				return false
			}
		} else if f.start <= span.end && span.start <= f.end {
			return false
		}
	}
	return true
}

// partners lists the values iv may share a register with at a copy: the
// source of the copy that defines iv, when a live range of it ends there, and
// the destination of the copy that reads iv last, when a live range of it
// starts there.
func (a *allocator) partners(iv interval, by map[value]interval) []partner {
	var out []partner
	if dst, src, ok := a.copy(iv.start); ok && dst == iv.value && src != dst {
		if o, found := by[src]; found && o.endsAt(iv.start) {
			out = append(out, partner{src, iv.start})
		}
	}
	if dst, src, ok := a.copy(iv.end); ok && src == iv.value && src != dst {
		if o, found := by[dst]; found && o.startsAt(iv.end) {
			out = append(out, partner{dst, iv.end})
		}
	}
	return out
}

// copy reports the values a copy row moves between.
func (a *allocator) copy(row int) (dst, src value, ok bool) {
	d, s, ok := a.frame.Copy(a.insts[row])
	if !ok {
		return value{}, value{}, false
	}
	dst, ok = a.value(d)
	if !ok {
		return value{}, value{}, false
	}
	src, ok = a.value(s)
	return dst, src, ok
}

// victim picks the non-tiny interval among iv and the actives of its bank
// live at one of iv's rows that ends last.
func (a *allocator) victim(iv interval, active []interval) (interval, bool) {
	best, ok := interval{}, false
	consider := func(o interval) {
		if o.value.typ != iv.value.typ || a.tiny[vreg(o.value)] {
			return
		}
		if !ok || o.end > best.end {
			best, ok = o, true
		}
	}
	consider(iv)
	for _, o := range active {
		if iv.overlaps(o) {
			consider(o)
		}
	}
	return best, ok
}

// rewrite surrounds spilled reads/writes with reload/park rows using fresh
// temporaries, preserving tied operands.
func (a *allocator) rewrite(spilled []value) {
	slot := map[value]int{}
	for _, v := range spilled {
		slot[v] = a.slots
		a.locs[vreg(v)] = Loc{Slot: a.slots, Spilled: true}
		a.slots++
	}

	var at []int
	var repl [][]Instruction
	for row, inst := range a.insts {
		var before, after []Instruction
		written := a.frame.Writes(inst)
		fresh := map[value]VReg{}
		reloaded, parked := map[value]bool{}, map[value]bool{}
		for i, op := range inst.operands() {
			v, ok := a.value(register(*op))
			if !ok || !v.virtual {
				continue
			}
			n, ok := slot[v]
			if !ok {
				continue
			}
			t, ok := fresh[v]
			if !ok {
				t = a.fresh(v)
				fresh[v] = t
			}
			*op = replace(*op, t)
			if _, mem := (*op).(MemOperand); !mem && written[i] {
				if !parked[v] {
					after = append(after, a.frame.Spill(t, n))
					parked[v] = true
				}
			} else if !reloaded[v] {
				before = append(before, a.frame.Reload(t, n))
				reloaded[v] = true
			}
		}
		if len(before) == 0 && len(after) == 0 {
			continue
		}
		at = append(at, row)
		repl = append(repl, slices.Concat(before, []Instruction{inst}, after))
	}
	a.insts, a.labels = splice(a.insts, a.labels, at, repl)
}

// value keys the register an operand names, and reports false for a physical
// register the allocator never assigns and therefore never tracks.
func (a *allocator) value(r Reg) (value, bool) {
	switch r := r.(type) {
	case VReg:
		return virtual(r), true
	case PReg:
		v := physical(r)
		return v, a.usable[v]
	default:
		return value{}, false
	}
}

// fresh returns a new tiny virtual register of v's bank and width.
func (a *allocator) fresh(v value) VReg {
	t := NewVReg(a.next, v.typ, v.width)
	a.next++
	a.tiny[t] = true
	return t
}

// assign records every virtual register's register and rewrites the rows to
// physical registers narrowed to each value's own width.
func (a *allocator) assign(assigned map[value]PReg) {
	for v, r := range assigned {
		if !a.tiny[vreg(v)] {
			a.locs[vreg(v)] = Loc{Reg: NewPReg(r.ID(), v.typ, v.width)}
		}
	}
	drop := a.redundant(assigned)
	for row, inst := range a.insts {
		for _, op := range inst.operands() {
			v, ok := register(*op).(VReg)
			if !ok {
				continue
			}
			r := assigned[virtual(v)]
			*op = replace(*op, NewPReg(r.ID(), v.Type(), v.Width()))
		}
		a.insts[row] = inst
	}
	repl := make([][]Instruction, len(drop))
	a.insts, a.labels = splice(a.insts, a.labels, drop, repl)
}

// redundant lists the rows of copies the allocator introduced whose source
// and destination received one register.
func (a *allocator) redundant(assigned map[value]PReg) []int {
	var rows []int
	for row, inst := range a.insts {
		dst, src, ok := a.frame.Copy(inst)
		if !ok || !(isVirtual(dst) || isVirtual(src)) {
			continue
		}
		x, okx := a.register(dst, assigned)
		y, oky := a.register(src, assigned)
		if okx && oky && x == y {
			rows = append(rows, row)
		}
	}
	return rows
}

// register returns the id of the register r holds once assigned allocates.
func (a *allocator) register(r Reg, assigned map[value]PReg) (uint8, bool) {
	switch r := r.(type) {
	case VReg:
		p, ok := assigned[virtual(r)]
		return p.ID(), ok
	case PReg:
		return r.ID(), true
	default:
		return 0, false
	}
}

// compare orders values totally: physical before virtual, then by bank, id,
// and width.
func (v value) compare(o value) int {
	switch {
	case v.virtual != o.virtual:
		if !v.virtual {
			return -1
		}
		return 1
	case v.typ != o.typ:
		return int(v.typ) - int(o.typ)
	case v.id != o.id:
		return int(v.id - o.id)
	default:
		return int(v.width) - int(o.width)
	}
}

// endsAt reports whether one of iv's live ranges ends at row.
func (iv interval) endsAt(row int) bool {
	return slices.ContainsFunc(iv.ranges, func(r [2]int) bool { return r[1] == row })
}

// startsAt reports whether one of iv's live ranges starts at row.
func (iv interval) startsAt(row int) bool {
	return slices.ContainsFunc(iv.ranges, func(r [2]int) bool { return r[0] == row })
}

// without returns iv with row left out of its live rows.
func (iv interval) without(row int) interval {
	out := iv
	out.ranges = nil
	for _, r := range iv.ranges {
		if row < r[0] || row > r[1] {
			out.ranges = append(out.ranges, r)
			continue
		}
		if r[0] < row {
			out.ranges = append(out.ranges, [2]int{r[0], row - 1})
		}
		if row < r[1] {
			out.ranges = append(out.ranges, [2]int{row + 1, r[1]})
		}
	}
	out.start, out.end = math.MaxInt, -1
	if n := len(out.ranges); n > 0 {
		out.start, out.end = out.ranges[0][0], out.ranges[n-1][1]
	}
	return out
}

// overlaps reports whether iv and o are live at a common row.
func (iv interval) overlaps(o interval) bool {
	i, j := 0, 0
	for i < len(iv.ranges) && j < len(o.ranges) {
		x, y := iv.ranges[i], o.ranges[j]
		if x[0] <= y[1] && y[0] <= x[1] {
			return true
		}
		if x[1] < y[1] {
			i++
		} else {
			j++
		}
	}
	return false
}

// replace returns op with its register - or its memory base - swapped for r.
func replace(op Operand, r Reg) Operand {
	var wrapped Operand
	switch r := r.(type) {
	case VReg:
		wrapped = Virtual(r)
	case PReg:
		wrapped = Physical(r)
	}
	if mem, ok := op.(MemOperand); ok {
		return Mem(wrapped, mem.Offset)
	}
	return wrapped
}

func isVirtual(r Reg) bool {
	_, ok := r.(VReg)
	return ok
}

func virtual(r VReg) value {
	return value{virtual: true, id: r.ID(), typ: r.Type(), width: r.Width()}
}

// vreg is the inverse of virtual.
func vreg(v value) VReg {
	return VReg{id: v.id, typ: v.typ, width: v.width}
}

// register returns the register an operand names, looking through a memory
// operand to its base, or nil.
func register(op Operand) Reg {
	switch op := op.(type) {
	case VRegOperand:
		return op.Reg
	case PRegOperand:
		return op.Reg
	case MemOperand:
		return register(op.Base)
	default:
		return nil
	}
}

func physical(r PReg) value {
	return value{id: int32(r.ID()), typ: r.Type()}
}
