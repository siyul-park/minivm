package asm

import (
	"errors"
	"fmt"
)

// Label identifies a position in the emitted instruction stream. Labels are
// allocated via Assembler.Label and bound via Assembler.Bind. Cross-Code
// references remain unresolved inside Code.Relocs until Link supplies their
// target addresses.
type Label int

// Assembler emits target-architecture instructions into a single-shot
// buffer. Allocate vregs with Reg, declare labels with Label/Bind, pin
// vregs to specific pregs with Pin, append instructions with Emit, and
// finalize with Build.
//
// Each Assembler builds exactly one Code. Reuse is not supported — discard
// after Build returns.
type Assembler struct {
	arch     Arch
	insts    []Instruction
	pins     map[int32]PReg
	labels   map[Label]int
	entries  []Label
	nextVReg int32
	nextLbl  Label
	err      error
}

var (
	ErrConflictingPin     = errors.New("conflicting pin")
	ErrEntryRequiresFrame = errors.New("non-primary entry cannot use spill frame")
)

// New constructs an Assembler targeting the given architecture.
func New(arch Arch) *Assembler {
	return &Assembler{
		arch:   arch,
		pins:   make(map[int32]PReg),
		labels: make(map[Label]int),
	}
}

// Reg allocates a fresh virtual register of the given type and width.
func (a *Assembler) Reg(typ RegType, w RegWidth) VReg {
	r := NewVReg(a.nextVReg, typ, w)
	a.nextVReg++
	return r
}

// Label reserves a label identifier. Bind it later with Bind.
func (a *Assembler) Label() Label {
	id := a.nextLbl
	a.nextLbl++
	return id
}

// Bind anchors a label at the current instruction index.
func (a *Assembler) Bind(id Label) {
	a.labels[id] = len(a.insts)
}

// Entry marks the current position as a named callable entry. The label is bound
// to the current instruction index. Multiple entries allow one Code to expose
// several callables at distinct offsets.
//
// A non-primary entry (bound after instruction 0) is incompatible with a
// spill frame: only the primary entry at offset 0 runs the frame prologue,
// so a call through a later entry would hit the shared epilogue without
// ever reserving the spill area. Build returns ErrEntryRequiresFrame when
// spilling occurs and a non-primary entry exists.
func (a *Assembler) Entry(id Label) {
	a.Bind(id)
	a.entries = append(a.entries, id)
}

// Pin forces v to occupy preg. A vreg can be pinned to only one preg; a
// conflicting Pin records an error returned from Build.
func (a *Assembler) Pin(v VReg, preg PReg) error {
	if existing, ok := a.pins[v.ID()]; ok && (existing.ID() != preg.ID() || existing.Type() != preg.Type()) {
		err := fmt.Errorf("%w: %v already pinned to %v, got %v",
			ErrConflictingPin, v, existing, preg)
		if a.err == nil {
			a.err = err
		}
		return err
	}
	a.pins[v.ID()] = preg
	return nil
}

// Emit appends one or more instructions.
func (a *Assembler) Emit(insts ...Instruction) {
	a.insts = append(a.insts, insts...)
}

// Build finalizes the instruction list into a Code: rewrites operands
// from virtual to physical registers, encodes every instruction, and
// resolves intra-Code label references. External label references survive
// in Code.Relocs for Link to patch.
func (a *Assembler) Build() (*Code, error) {
	if a.err != nil {
		return nil, a.err
	}

	rw := newRewriter(a.arch, a.insts, a.pins)
	rewritten, labels, err := rw.run(a.insts, a.labels, a.entries)
	if err != nil {
		return nil, err
	}

	bytes, labels, relocs, err := a.encode(rewritten, labels)
	if err != nil {
		return nil, err
	}

	code := &Code{
		Bytes:  bytes,
		Labels: labels,
		Relocs: relocs,
	}
	if len(a.entries) > 0 {
		code.Entries = append([]Label(nil), a.entries...)
	}
	return code, nil
}

// encode produces the final byte stream from phys-allocated instructions.
// It runs in two passes: draft encodes instructions with placeholder label
// operands and records byte offsets; final patches intra-Code labels and
// records external labels as relocations.
func (a *Assembler) encode(insts []Instruction, labels map[Label]int) ([]byte, map[Label]int, []Relocation, error) {
	insts, labels, err := a.relax(insts, labels)
	if err != nil {
		return nil, nil, nil, err
	}

	encoded, offsets, err := a.draft(insts)
	if err != nil {
		return nil, nil, nil, err
	}

	pos := make(map[Label]int, len(labels))
	for id, idx := range labels {
		pos[id] = offsets[idx]
	}

	out, relocs, err := a.final(insts, encoded, offsets, pos)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, pos, relocs, nil
}

// relax rewrites out-of-range intra-Code label branches into equivalent
// in-range multi-instruction sequences when the target Arch implements
// Relaxer. It runs a fixpoint loop: draft the current instruction list to
// measure offsets, collect every label branch Relaxer reports as
// out-of-range, splice all of their replacements into one rebuilt
// instruction list, and repeat. Each replacement is constructed by Relax to
// already be in range, so a branch relaxes at most once; batching the
// splices within a pass turns the O(branches × instructions) drafts of a
// one-at-a-time loop into one draft per pass.
func (a *Assembler) relax(insts []Instruction, labels map[Label]int) ([]Instruction, map[Label]int, error) {
	relaxer, ok := a.arch.(Relaxer)
	if !ok {
		return insts, labels, nil
	}

	for {
		_, offsets, err := a.draft(insts)
		if err != nil {
			return nil, nil, err
		}

		idx, repl := a.collectRelaxations(relaxer, insts, labels, offsets)
		if len(idx) == 0 {
			return insts, labels, nil
		}
		insts, labels = spliceRelaxations(insts, labels, idx, repl)
	}
}

// collectRelaxations drafts every out-of-range intra-Code label branch's
// Relaxer replacement in instruction order. idx and repl are parallel:
// idx[k] is the original index of the branch replaced by repl[k].
func (a *Assembler) collectRelaxations(
	relaxer Relaxer, insts []Instruction, labels map[Label]int, offsets []int,
) (idx []int, repl [][]Instruction) {
	for i, inst := range insts {
		lbl, isLabel := inst.Src2.(LabelOperand)
		if !isLabel {
			continue
		}
		target, intra := labels[lbl.ID]
		if !intra {
			continue
		}
		disp := int64(offsets[target] - offsets[i])
		replacement, relaxed := relaxer.Relax(inst, disp)
		if !relaxed {
			continue
		}
		idx = append(idx, i)
		repl = append(repl, replacement)
	}
	return idx, repl
}

// spliceRelaxations rebuilds insts with every collected replacement spliced
// in and rebases labels across the resulting per-instruction length deltas.
func spliceRelaxations(insts []Instruction, labels map[Label]int, idx []int, repl [][]Instruction) ([]Instruction, map[Label]int) {
	prefix := make([]int, len(insts)+1)
	for k, i := range idx {
		prefix[i+1] = len(repl[k]) - 1
	}
	for i := 0; i < len(insts); i++ {
		prefix[i+1] += prefix[i]
	}

	out := make([]Instruction, 0, len(insts)+prefix[len(insts)])
	k := 0
	for i, inst := range insts {
		if k < len(idx) && idx[k] == i {
			out = append(out, repl[k]...)
			k++
			continue
		}
		out = append(out, inst)
	}

	newLabels := make(map[Label]int, len(labels))
	for id, pos := range labels {
		newLabels[id] = pos + prefix[pos]
	}
	return out, newLabels
}

// draft encodes each instruction with #0 substituted for label operands so
// widths can be measured without knowing label offsets.
func (a *Assembler) draft(insts []Instruction) ([][]byte, []int, error) {
	enc := a.arch.Encoder()
	encoded := make([][]byte, len(insts))
	offsets := make([]int, len(insts)+1)

	for i, inst := range insts {
		if inst.Op == OpPseudoLabel {
			offsets[i+1] = offsets[i]
			continue
		}
		toEncode := inst
		if _, ok := toEncode.Src2.(LabelOperand); ok {
			toEncode.Src2 = Imm(0)
		}
		bytes, err := enc.Encode(toEncode)
		if err != nil {
			return nil, nil, err
		}
		encoded[i] = bytes
		offsets[i+1] = offsets[i] + len(bytes)
	}
	return encoded, offsets, nil
}

// final walks the encoded list, patching intra-Code label references with
// resolved deltas and recording external references as linker relocations.
func (a *Assembler) final(
	insts []Instruction,
	encoded [][]byte,
	offsets []int,
	labels map[Label]int,
) ([]byte, []Relocation, error) {
	enc := a.arch.Encoder()
	out := make([]byte, 0, offsets[len(insts)])
	var relocs []Relocation
	for i, inst := range insts {
		if inst.Op == OpPseudoLabel {
			continue
		}
		lbl, isLabel := inst.Src2.(LabelOperand)
		if !isLabel {
			out = append(out, encoded[i]...)
			continue
		}
		target, intra := labels[lbl.ID]
		if !intra {
			relocs = append(relocs, Relocation{
				InstrIdx: i,
				Offset:   offsets[i],
				Label:    lbl.ID,
				Inst:     inst,
			})
			out = append(out, encoded[i]...)
			continue
		}
		patched := inst
		patched.Src2 = Imm(int64(target - offsets[i]))
		bytes, err := enc.Encode(patched)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, bytes...)
	}
	return out, relocs, nil
}
