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
	entries  map[Label]Signature
	nextVReg int32
	nextLbl  Label
	err      error
}

var ErrConflictingPin = errors.New("conflicting pin")

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

// Entry marks the current position as a named callable entry with its own
// signature. The label is bound to the current instruction index. Multiple
// entries allow one Code to expose several callables at distinct offsets.
func (a *Assembler) Entry(id Label, sig Signature) {
	a.Bind(id)
	if a.entries == nil {
		a.entries = make(map[Label]Signature)
	}
	a.entries[id] = sig
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
func (a *Assembler) Build(sig Signature) (*Code, error) {
	if a.err != nil {
		return nil, a.err
	}

	rw := newRewriter(a.arch, a.insts, a.pins)
	rewritten, labels, err := rw.run(a.insts, a.labels)
	if err != nil {
		return nil, err
	}

	bytes, labels, relocs, err := a.encode(rewritten, labels)
	if err != nil {
		return nil, err
	}

	code := &Code{
		Bytes:     bytes,
		Labels:    labels,
		Relocs:    relocs,
		Signature: sig,
	}
	if len(a.entries) > 0 {
		code.Entries = make(map[Label]Signature, len(a.entries))
		for k, v := range a.entries {
			code.Entries[k] = v
		}
	}
	return code, nil
}

// encode produces the final byte stream from phys-allocated instructions.
// It runs in two passes: draft encodes instructions with placeholder label
// operands and records byte offsets; final patches intra-Code labels and
// records external labels as relocations.
func (a *Assembler) encode(insts []Instruction, labels map[Label]int) ([]byte, map[Label]int, []Relocation, error) {
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
