package asm

import (
	"errors"
	"fmt"
)

// Label identifies a position in the emitted instruction stream. Labels are
// allocated with Assembler.Label and anchored with Assembler.Bind. Build
// resolves every label reference, so a reference to a label that was never
// bound is an error.
type Label int

// Assembler emits target-architecture instructions into a single-shot
// buffer. Allocate vregs with Reg, declare labels with Label/Bind, pin
// vregs to specific pregs with Pin, append instructions with Emit, and
// finalize with Build.
//
// Each Assembler builds exactly one machine-code block. Reuse is not
// supported — discard after Build returns.
type Assembler struct {
	arch     Arch
	insts    []Instruction
	pins     map[int32]PReg
	labels   map[Label]int
	nextVReg int32
	nextLbl  Label
	err      error
}

var (
	ErrConflictingPin  = errors.New("conflicting pin")
	ErrUnresolvedLabel = errors.New("unresolved label")
)

// New constructs an Assembler targeting the given architecture.
func New(arch Arch) *Assembler {
	a := &Assembler{
		arch:   arch,
		pins:   make(map[int32]PReg),
		labels: make(map[Label]int),
	}
	return a
}

// Reg allocates a fresh virtual register of the given type and width.
func (a *Assembler) Reg(typ RegType, w RegWidth) VReg {
	r := NewVReg(a.nextVReg, typ, w)
	a.nextVReg++
	return r
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

// Pin forces v to occupy preg. A vreg can be pinned to only one preg; a
// conflicting Pin records an error returned from Build.
func (a *Assembler) Pin(v VReg, preg PReg) error {
	if a.arch == nil {
		return a.fail(fmt.Errorf("%w: nil architecture", ErrInvalidArgs))
	}
	if v.ID() < 0 || v.ID() >= a.nextVReg || v.Type() != preg.Type() || !a.arch.Registers().valid(preg) {
		return a.fail(fmt.Errorf("%w: pin %v to %v", ErrInvalidOperand, v, preg))
	}
	if existing, ok := a.pins[v.ID()]; ok && (existing.ID() != preg.ID() || existing.Type() != preg.Type()) {
		err := fmt.Errorf("%w: %v already pinned to %v, got %v",
			ErrConflictingPin, v, existing, preg)
		return a.fail(err)
	}
	a.pins[v.ID()] = preg
	return nil
}

// Emit appends one or more instructions.
func (a *Assembler) Emit(insts ...Instruction) {
	a.insts = append(a.insts, insts...)
}

// Build finalizes the instruction list into machine code: it rewrites
// operands from virtual to physical registers, relaxes out-of-range label
// branches, and encodes every instruction with its label references
// resolved.
func (a *Assembler) Build() ([]byte, error) {
	if a.err != nil {
		return nil, a.err
	}
	if a.arch == nil {
		return nil, fmt.Errorf("%w: nil architecture", ErrInvalidArgs)
	}

	rw, err := newRewriter(a.arch, a.insts, a.pins, int(a.nextVReg))
	if err != nil {
		return nil, err
	}
	insts, labels, err := rw.run(a.insts, a.labels)
	if err != nil {
		return nil, err
	}
	return a.encode(insts, labels)
}

func (a *Assembler) fail(err error) error {
	if a.err == nil {
		a.err = err
	}
	return err
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
		if inst.Op == OpPseudoUse {
			offsets[i+1] = offsets[i]
			continue
		}
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
