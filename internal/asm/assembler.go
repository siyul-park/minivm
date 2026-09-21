package asm

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Label identifies a position in the emitted instruction stream. Labels are
// allocated with Assembler.Label and anchored with Assembler.Bind. Build
// resolves every label reference, so a reference to a label that was never
// bound is an error.
type Label int

// Assembler emits target-architecture instructions into a single-shot
// buffer. Append instructions with Emit, declare labels with Label/Bind, and
// finalize with Build.
//
// Each Assembler builds exactly one machine-code block. Reuse is not
// supported — discard after Build returns.
type Assembler struct {
	arch     Arch
	reserved map[PReg]bool
	insts    []Instruction
	labels   map[Label]int
	nextLbl  Label
	locs     map[VReg]Loc
}

var (
	ErrUnallocated          = errors.New("unallocated register")
	ErrUnresolvedLabel      = errors.New("unresolved label")
	ErrNoRegistersAvailable = errors.New("no registers available")
)

// New constructs an Assembler targeting the given architecture.
func New(arch Arch) *Assembler {
	return &Assembler{
		arch:     arch,
		reserved: make(map[PReg]bool),
		labels:   make(map[Label]int),
	}
}

// Label reserves a label identifier. Anchor it later with Bind.
func (a *Assembler) Label() Label {
	id := a.nextLbl
	a.nextLbl++
	return id
}

// Bind anchors a label at the current instruction index.
func (a *Assembler) Bind(id Label) {
	a.labels[id] = len(a.insts)
}

// Emit appends one or more instructions.
func (a *Assembler) Emit(insts ...Instruction) {
	a.insts = append(a.insts, insts...)
}

// Rows returns the instruction rows before allocation and encoding.
func (a *Assembler) Rows() []Instruction {
	return slices.Clone(a.insts)
}

// Reserve removes registers from the allocator's usable banks.
func (a *Assembler) Reserve(regs ...PReg) {
	for _, reg := range regs {
		a.reserved[reg] = true
	}
}

// Build finalizes the instruction list into machine code. When the
// architecture is a Frame, every virtual register is allocated first and
// every Slots operand becomes the spill area's size; otherwise a virtual
// register is ErrUnallocated.
func (a *Assembler) Build() ([]byte, error) {
	if a.arch == nil {
		return nil, fmt.Errorf("%w: nil architecture", ErrInvalidArgs)
	}
	insts, labels, slots := slices.Clone(a.insts), maps.Clone(a.labels), 0
	if frame, ok := a.arch.(Frame); ok && virtual(insts) {
		alloc := newAllocator(frame, insts, labels, a.reserved)
		var err error
		if insts, labels, err = alloc.allocate(); err != nil {
			return nil, err
		}
		a.locs, slots = alloc.locs, alloc.slots
	}
	size := int64((slots*8 + 15) &^ 15)
	for i, inst := range insts {
		ops := [4]*Operand{&inst.Dst, &inst.Src1, &inst.Src2, &inst.Src3}
		for _, op := range ops {
			if _, ok := (*op).(SlotsOperand); ok {
				*op = Imm(size)
			}
			if err := validate(*op); err != nil {
				return nil, err
			}
		}
		insts[i] = inst
	}
	return a.encode(insts, labels)
}

// Loc reports where a virtual register of the rows Build allocated lives.
func (a *Assembler) Loc(v VReg) (Loc, bool) {
	loc, ok := a.locs[v]
	return loc, ok
}

// virtual reports whether any row names a virtual register.
func virtual(insts []Instruction) bool {
	for _, inst := range insts {
		for _, op := range [4]Operand{inst.Dst, inst.Src1, inst.Src2, inst.Src3} {
			if _, ok := register(op).(VReg); ok {
				return true
			}
		}
	}
	return false
}

func validate(op Operand) error {
	switch op := op.(type) {
	case nil, PRegOperand, ImmOperand, LabelOperand:
		return nil
	case VRegOperand:
		return fmt.Errorf("%w: %v", ErrUnallocated, op.Reg)
	case MemOperand:
		return validate(op.Base)
	default:
		return fmt.Errorf("%w: %T", ErrInvalidOperand, op)
	}
}

// encode turns phys-allocated instructions into the final byte stream.
//
// Each pass drafts the instruction list — encoding every instruction with a
// placeholder for its label operand — to measure byte offsets, then asks the
// architecture's Relaxer to rewrite label branches whose displacement no
// longer fits. Relax returns a replacement that is already in range, so a
// branch relaxes at most once and the loop terminates; batching every splice
// within a pass keeps drafting proportional to the number of relaxation
// rounds rather than to the number of branches.
func (a *Assembler) encode(insts []Instruction, labels map[Label]int) ([]byte, error) {
	relaxer, relaxes := a.arch.(Relaxer)
	for {
		draft, offsets, err := a.draft(insts)
		if err != nil {
			return nil, err
		}
		if relaxes {
			if at, repl := a.collect(relaxer, insts, labels, offsets); len(at) > 0 {
				insts, labels = splice(insts, labels, at, repl)
				continue
			}
		}
		return a.resolve(insts, draft, offsets, labels)
	}
}

// draft encodes each instruction with #0 substituted for label operands so
// widths can be measured before label offsets are known. offsets holds the
// start of every instruction plus the total length.
func (a *Assembler) draft(insts []Instruction) ([][]byte, []int, error) {
	enc := a.arch.Encoder()
	draft := make([][]byte, len(insts))
	offsets := make([]int, len(insts)+1)

	for i, inst := range insts {
		if _, ok := inst.Src2.(LabelOperand); ok {
			inst.Src2 = Imm(0)
		}
		bytes, err := enc.Encode(inst)
		if err != nil {
			return nil, nil, err
		}
		draft[i] = bytes
		offsets[i+1] = offsets[i] + len(bytes)
	}
	return draft, offsets, nil
}

// collect drafts a Relaxer replacement for every label branch whose
// displacement is out of range, in instruction order. at and repl are
// parallel: at[k] is the index of the branch replaced by repl[k].
func (a *Assembler) collect(
	relaxer Relaxer, insts []Instruction, labels map[Label]int, offsets []int,
) (at []int, repl [][]Instruction) {
	for i, inst := range insts {
		lbl, ok := inst.Src2.(LabelOperand)
		if !ok {
			continue
		}
		target, bound := labels[lbl.ID]
		if !bound {
			continue
		}
		replacement, relaxed := relaxer.Relax(inst, int64(offsets[target]-offsets[i]))
		if !relaxed {
			continue
		}
		at = append(at, i)
		repl = append(repl, replacement)
	}
	return at, repl
}

// resolve concatenates the drafted encodings, re-encoding each label branch
// with its resolved displacement.
func (a *Assembler) resolve(
	insts []Instruction, draft [][]byte, offsets []int, labels map[Label]int,
) ([]byte, error) {
	enc := a.arch.Encoder()
	out := make([]byte, 0, offsets[len(insts)])
	for i, inst := range insts {
		lbl, isLabel := inst.Src2.(LabelOperand)
		if !isLabel {
			out = append(out, draft[i]...)
			continue
		}
		target, bound := labels[lbl.ID]
		if !bound {
			return nil, fmt.Errorf("%w: label %d", ErrUnresolvedLabel, lbl.ID)
		}
		inst.Src2 = Imm(int64(offsets[target] - offsets[i]))
		bytes, err := enc.Encode(inst)
		if err != nil {
			return nil, err
		}
		out = append(out, bytes...)
	}
	return out, nil
}

// splice rebuilds insts with every collected replacement spliced in and
// rebases labels across the resulting per-instruction length deltas.
func splice(insts []Instruction, labels map[Label]int, at []int, repl [][]Instruction) ([]Instruction, map[Label]int) {
	shift := make([]int, len(insts)+1)
	for k, i := range at {
		shift[i+1] = len(repl[k]) - 1
	}
	for i := range insts {
		shift[i+1] += shift[i]
	}

	out := make([]Instruction, 0, len(insts)+shift[len(insts)])
	k := 0
	for i, inst := range insts {
		if k < len(at) && at[k] == i {
			out = append(out, repl[k]...)
			k++
			continue
		}
		out = append(out, inst)
	}

	rebased := make(map[Label]int, len(labels))
	for id, pos := range labels {
		rebased[id] = pos + shift[pos]
	}
	return out, rebased
}
