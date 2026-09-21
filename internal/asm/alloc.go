package asm

import (
	"fmt"
	"slices"
)

// Loc is where a virtual register lives for its whole life once Build has
// allocated: in Reg or, when Spilled, in spill slot Slot.
type Loc struct {
	Reg     PReg
	Slot    int
	Spilled bool
}

// allocator assigns every virtual register a location by linear scan over
// live intervals, with no interval splitting: a value that cannot stay in one
// register for its whole life is spilled everywhere instead, its rows
// rewritten to reload it into a fresh two-row register before each read and
// park it after each write, and the scan is rerun over the rewritten rows.
// Those fresh registers never cross a call or a block boundary, so the rerun
// only ever has less to do; the loop ends when a scan spills nothing.
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

// interval is the range of rows a value occupies, and, for a virtual value,
// whether it is live across a call.
type interval struct {
	value      value
	start, end int
	call       bool
}

func newAllocator(frame Frame, insts []Instruction, labels map[Label]int, reserved map[PReg]bool) *allocator {
	a := &allocator{
		frame:  frame,
		insts:  insts,
		labels: labels,
		usable: map[value]bool{},
		locs:   map[VReg]Loc{},
		tiny:   map[VReg]bool{},
	}
	for _, typ := range []RegType{RegTypeInt, RegTypeFloat} {
		registers := slices.Clone(frame.Registers(typ))
		a.registers[typ] = slices.DeleteFunc(registers, func(r PReg) bool { return reserved[r] })
		for _, r := range a.registers[typ] {
			a.usable[physical(r)] = true
		}
	}
	for _, inst := range insts {
		for _, op := range [4]Operand{inst.Dst, inst.Src1, inst.Src2, inst.Src3} {
			if v, ok := register(op).(VReg); ok {
				a.next = max(a.next, v.id+1)
			}
		}
	}
	return a
}

// allocate runs scans and rewrites until one scan assigns every virtual
// register, then substitutes the spill-area size wherever a row asks for it.
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

// intervals computes every value's occupied row range: the rows where it is
// live or written, taken as one range with any holes ignored, which is the
// imprecision linear scan without splitting accepts.
func (a *allocator) intervals() []interval {
	blocks, out := a.liveness()
	ivs := map[value]*interval{}
	occupy := func(v value, row int) {
		iv, ok := ivs[v]
		if !ok {
			iv = &interval{value: v, start: row, end: row}
			ivs[v] = iv
		}
		iv.start = min(iv.start, row)
		iv.end = max(iv.end, row)
	}
	for b, blk := range blocks {
		after := out[b]
		for row := blk[1] - 1; row >= blk[0]; row-- {
			reads, writes := a.operands(row)
			live := map[value]bool{}
			for v := range after {
				live[v] = true
			}
			for _, v := range writes {
				occupy(v, row)
				delete(live, v)
			}
			for _, v := range reads {
				live[v] = true
			}
			for v := range live {
				occupy(v, row)
				if v.virtual && after[v] && a.frame.Flow(a.insts[row]) == FlowCall {
					ivs[v].call = true
				}
			}
			after = live
		}
	}

	result := make([]interval, 0, len(ivs))
	for _, iv := range ivs {
		result = append(result, *iv)
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
			reads, writes := a.operands(row)
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

// operands lists the values row reads and the values it writes.
func (a *allocator) operands(row int) (reads, writes []value) {
	inst := a.insts[row]
	written := a.frame.Writes(inst)
	for i, op := range [4]Operand{inst.Dst, inst.Src1, inst.Src2, inst.Src3} {
		v, ok := a.value(register(op))
		if !ok {
			continue
		}
		if _, mem := op.(MemOperand); !mem && written[i] {
			writes = append(writes, v)
		} else {
			reads = append(reads, v)
		}
	}
	return reads, writes
}

// value keys the register an operand names, and reports false for a physical
// register the allocator never assigns and therefore never tracks.
func (a *allocator) value(r Reg) (value, bool) {
	switch r := r.(type) {
	case VReg:
		return value{virtual: true, id: r.id, typ: r.typ, width: r.width}, true
	case PReg:
		v := physical(r)
		return v, a.usable[v]
	default:
		return value{}, false
	}
}

// scan assigns a register to every virtual interval in start order and
// returns the assignment, or the values that must spill first: those live
// across a call, and those evicted when a bank runs out. An eviction takes
// the interval that ends last, never a tiny reload or park register.
func (a *allocator) scan(ivs []interval) (map[value]PReg, []value, error) {
	var spilled []value
	fixed := map[value][]interval{}
	for _, iv := range ivs {
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
			r, ok := a.free(iv, active, assigned, fixed)
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

// free returns the first register of iv's bank that no active interval holds
// and no fixed interval occupies anywhere within iv.
func (a *allocator) free(iv interval, active []interval, assigned map[value]PReg, fixed map[value][]interval) (PReg, bool) {
	held := map[uint8]bool{}
	for _, o := range active {
		if r, ok := assigned[o.value]; ok && o.value.typ == iv.value.typ {
			held[r.id] = true
		}
	}
next:
	for _, r := range a.registers[iv.value.typ] {
		if held[r.id] {
			continue
		}
		for _, f := range fixed[physical(r)] {
			if f.start <= iv.end && iv.start <= f.end {
				continue next
			}
		}
		return r, true
	}
	return PReg{}, false
}

// victim picks the non-tiny interval among iv and the actives of its bank
// that ends last.
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
		consider(o)
	}
	return best, ok
}

// rewrite spills every value in spilled: each read of it reloads a fresh
// register just before the row and each write parks one just after.
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
		ops := [4]*Operand{&inst.Dst, &inst.Src1, &inst.Src2, &inst.Src3}
		for i, op := range ops {
			v, ok := a.value(register(*op))
			if !ok || !v.virtual {
				continue
			}
			n, ok := slot[v]
			if !ok {
				continue
			}
			t := a.fresh(v)
			*op = replace(*op, t)
			if _, mem := (*op).(MemOperand); !mem && written[i] {
				after = append(after, a.frame.Spill(t, n))
			} else {
				before = append(before, a.frame.Reload(t, n))
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
			a.locs[vreg(v)] = Loc{Reg: NewPReg(r.id, v.typ, v.width)}
		}
	}
	for row, inst := range a.insts {
		ops := [4]*Operand{&inst.Dst, &inst.Src1, &inst.Src2, &inst.Src3}
		for _, op := range ops {
			v, ok := register(*op).(VReg)
			if !ok {
				continue
			}
			r := assigned[value{virtual: true, id: v.id, typ: v.typ, width: v.width}]
			*op = replace(*op, NewPReg(r.id, v.typ, v.width))
		}
		a.insts[row] = inst
	}
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

func physical(r PReg) value {
	return value{id: int32(r.id), typ: r.typ}
}

func vreg(v value) VReg {
	return VReg{id: v.id, typ: v.typ, width: v.width}
}
