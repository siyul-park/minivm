package asm

import (
	"fmt"
)

// build runs the assembler's allocation + encoding pipeline:
//
//  1. compute last-use of every vreg
//  2. linear-scan-assign physical registers, honoring pins and the arch's
//     reserved scratch set
//  3. rewrite every operand (vreg → preg)
//  4. encode each instruction; emit placeholder bytes plus a Relocation for
//     any label operand
//  5. resolve relocations whose target label is bound inside this Code
func build(
	arch Arch,
	insts []Instruction,
	pins map[int32]PReg,
	labels map[Label]int,
	sig Signature,
	entries map[Label]Signature,
) (*Code, error) {
	phys, err := assignRegs(arch.Registers(), insts, pins)
	if err != nil {
		return nil, err
	}

	rewritten := rewriteAll(insts, phys, widthMap(insts, phys, pins))

	bytes, byteLabels, relocs, err := encode(arch.Encoder(), rewritten, labels)
	if err != nil {
		return nil, err
	}

	out := &Code{
		Bytes:     bytes,
		Labels:    byteLabels,
		Relocs:    relocs,
		Signature: sig,
	}
	if len(entries) > 0 {
		out.Entries = make(map[Label]Signature, len(entries))
		for k, v := range entries {
			out.Entries[k] = v
		}
	}
	return out, nil
}

// assignRegs runs linear scan over the instruction list and returns the
// vreg-id → preg map. Pinned vregs must be reservable from the architecture
// allocatable mask; reservation failure is reported as ErrConflictingPin.
func assignRegs(info RegInfo, insts []Instruction, pins map[int32]PReg) (map[int32]PReg, error) {
	ra := newRegAlloc(info)

	// Exclude the architecture's scratch set from auto-alloc. The scratch
	// registers remain reservable via Pin so lowerers can route VM
	// context pointers through them.
	for i := uint8(0); i < 64; i++ {
		if info.Scratch.Contains(i) {
			ra.exclude(NewPReg(i, RegTypeInt, Width64))
			ra.exclude(NewPReg(i, RegTypeFloat, Width64))
		}
	}

	last := lastUses(insts)
	assigned := make(map[int32]PReg)
	live := make(map[int32]PReg)
	owner := make(map[uint16]int32)

	keyOf := func(p PReg) uint16 {
		return uint16(p.Type())<<8 | uint16(p.ID())
	}

	bind := func(v VReg, p PReg) {
		assigned[v.ID()] = p
		live[v.ID()] = p
		owner[keyOf(p)] = v.ID()
	}

	free := func(v VReg) {
		p, ok := live[v.ID()]
		if !ok {
			return
		}
		delete(live, v.ID())
		delete(owner, keyOf(p))
		ra.free(v)
	}

	ensure := func(v VReg) error {
		if _, ok := live[v.ID()]; ok {
			return nil
		}
		if pinned, ok := pins[v.ID()]; ok {
			if id, busy := owner[keyOf(pinned)]; busy && id != v.ID() {
				prev := NewVReg(id, pinned.Type(), pinned.Width())
				delete(live, id)
				delete(owner, keyOf(pinned))
				ra.free(prev)
			}
			if err := ra.reserve(v, pinned); err != nil {
				return fmt.Errorf("%w: vreg %v pin %v: %w",
					ErrConflictingPin, v, pinned, err)
			}
			bind(v, pinned)
			return nil
		}
		p, err := ra.alloc(v)
		if err != nil {
			return err
		}
		bind(v, p)
		return nil
	}

	for i, inst := range insts {
		for _, v := range useRegs(inst) {
			if err := ensure(v); err != nil {
				return nil, err
			}
		}
		if dst, ok := defReg(inst); ok {
			if err := ensure(dst); err != nil {
				return nil, err
			}
		}
		for _, v := range useRegs(inst) {
			if last[v.ID()] == i {
				free(v)
			}
		}
	}

	return assigned, nil
}

// lastUses returns the highest instruction index at which each vreg is
// referenced (use or def).
func lastUses(insts []Instruction) map[int32]int {
	last := make(map[int32]int)
	for i, inst := range insts {
		if dst, ok := defReg(inst); ok {
			last[dst.ID()] = i
		}
		for _, v := range useRegs(inst) {
			last[v.ID()] = i
		}
	}
	return last
}

func defReg(inst Instruction) (VReg, bool) {
	if v, ok := inst.Dst.(VRegOperand); ok {
		return v.Reg, true
	}
	return VReg{}, false
}

func useRegs(inst Instruction) []VReg {
	var regs []VReg
	if base, ok := memBase(inst.Dst); ok {
		regs = append(regs, base)
	}
	for _, op := range []Operand{inst.Src1, inst.Src2, inst.Src3} {
		if v, ok := op.(VRegOperand); ok {
			regs = append(regs, v.Reg)
			continue
		}
		if base, ok := memBase(op); ok {
			regs = append(regs, base)
		}
	}
	return regs
}

func memBase(op Operand) (VReg, bool) {
	mem, ok := op.(MemOperand)
	if !ok {
		return VReg{}, false
	}
	v, ok := mem.Base.(VRegOperand)
	if !ok {
		return VReg{}, false
	}
	return v.Reg, true
}

// widthMap collects each vreg's declared width so undefined-width usages
// during rewrite can be back-filled from a confident defining site.
func widthMap(insts []Instruction, phys map[int32]PReg, pins map[int32]PReg) map[int32]RegWidth {
	widths := make(map[int32]RegWidth, len(phys))
	set := func(v VReg) {
		if _, ok := widths[v.ID()]; !ok {
			widths[v.ID()] = v.Width()
		}
	}
	for _, inst := range insts {
		if dst, ok := defReg(inst); ok {
			set(dst)
		}
		for _, v := range useRegs(inst) {
			set(v)
		}
	}
	for id, p := range pins {
		if w, ok := widths[id]; !ok || w == WidthUndefined {
			widths[id] = p.Width()
		}
	}
	return widths
}

func rewriteAll(insts []Instruction, phys map[int32]PReg, widths map[int32]RegWidth) []Instruction {
	out := make([]Instruction, len(insts))
	for i, inst := range insts {
		out[i] = Instruction{
			Op:   inst.Op,
			Dst:  rewriteOp(inst.Dst, phys, widths),
			Src1: rewriteOp(inst.Src1, phys, widths),
			Src2: rewriteOp(inst.Src2, phys, widths),
			Src3: rewriteOp(inst.Src3, phys, widths),
		}
	}
	return out
}

func rewriteOp(op Operand, phys map[int32]PReg, widths map[int32]RegWidth) Operand {
	switch v := op.(type) {
	case VRegOperand:
		p, ok := phys[v.Reg.ID()]
		if !ok {
			return op
		}
		w := v.Reg.Width()
		if w == WidthUndefined {
			w = widths[v.Reg.ID()]
		}
		return P(NewPReg(p.ID(), p.Type(), w))

	case MemOperand:
		base, ok := v.Base.(VRegOperand)
		if !ok {
			return op
		}
		p, ok := phys[base.Reg.ID()]
		if !ok {
			return op
		}
		w := base.Reg.Width()
		if w == WidthUndefined {
			w = widths[base.Reg.ID()]
		}
		return Mem(P(NewPReg(p.ID(), p.Type(), w)), v.Offset)

	default:
		return op
	}
}

// encode walks the post-allocation instruction list and produces the byte
// stream plus a label-byte-offset table. Label operands emit placeholder
// bytes (encoded with #0) and record a Relocation; intra-Code references
// are resolved immediately after the first pass.
func encode(enc Encoder, insts []Instruction, labels map[Label]int) ([]byte, map[Label]int, []Relocation, error) {
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
		code, err := enc.Encode(toEncode)
		if err != nil {
			return nil, nil, nil, err
		}
		encoded[i] = code
		offsets[i+1] = offsets[i] + len(code)
	}

	byteLabels := make(map[Label]int, len(labels))
	for id, idx := range labels {
		byteLabels[id] = offsets[idx]
	}

	out := make([]byte, 0, offsets[len(insts)])
	var relocs []Relocation
	for i, inst := range insts {
		if inst.Op == OpPseudoLabel {
			continue
		}
		lbl, ok := inst.Src2.(LabelOperand)
		if !ok {
			out = append(out, encoded[i]...)
			continue
		}
		target, intra := byteLabels[lbl.ID]
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
		code, err := enc.Encode(patched)
		if err != nil {
			return nil, nil, nil, err
		}
		out = append(out, code...)
	}

	return out, byteLabels, relocs, nil
}
