package asm

import (
	"errors"
	"fmt"
)

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
	if existing, ok := a.pins[v.ID()]; ok && existing.ID() != preg.ID() {
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

// Build finalizes the instruction list into a Code: runs regalloc, encodes
// every instruction, and resolves intra-Code label references. External
// label references survive in Code.Relocs for Link to patch.
func (a *Assembler) Build(sig Signature) (*Code, error) {
	if a.err != nil {
		return nil, a.err
	}

	sc := newScanner(a.arch.Registers(), a.insts, a.pins)
	if err := sc.run(a.insts); err != nil {
		return nil, err
	}

	rewritten := rewrite(a.insts, sc.assigned, sc.widths)
	bytes, byteLabels, relocs, err := encode(a.arch.Encoder(), rewritten, a.labels)
	if err != nil {
		return nil, err
	}

	code := &Code{
		Bytes:     bytes,
		Labels:    byteLabels,
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
