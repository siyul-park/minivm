package asm

import "fmt"

// scanner is the linear-scan register-allocation policy that orchestrates
// regPool over an instruction list. For each instruction it ensures every
// vreg referenced has a binding (auto-allocated, or reserved when pinned),
// then releases vregs whose last use has passed.
//
// scanner produces two outputs:
//
//   - assigned: the persistent vreg → preg map (survives free).
//   - widths: the effective RegWidth per vreg, back-filled from defining
//     sites and pin metadata so undefined-width operand uses can resolve.
type scanner struct {
	pool     *regPool
	pins     map[int32]PReg
	last     map[int32]int
	assigned map[int32]PReg
	widths   map[int32]RegWidth
}

func newScanner(info RegInfo, insts []Instruction, pins map[int32]PReg) *scanner {
	pool := newRegPool(info)
	for i := uint8(0); i < 64; i++ {
		if info.Scratch.Contains(i) {
			pool.exclude(NewPReg(i, RegTypeInt, Width64))
			pool.exclude(NewPReg(i, RegTypeFloat, Width64))
		}
	}
	return &scanner{
		pool:     pool,
		pins:     pins,
		last:     lastUses(insts),
		assigned: make(map[int32]PReg),
		widths:   make(map[int32]RegWidth),
	}
}

// run walks insts in order, binding vregs as they are first used or
// defined and releasing them once their last use has been processed.
func (s *scanner) run(insts []Instruction) error {
	for i, inst := range insts {
		for _, v := range inst.Uses() {
			if err := s.ensure(v); err != nil {
				return err
			}
		}
		if dst, ok := inst.Def(); ok {
			if err := s.ensure(dst); err != nil {
				return err
			}
		}
		for _, v := range inst.Uses() {
			if s.last[v.ID()] == i {
				s.pool.free(v)
			}
		}
	}
	s.backfillPinWidths()
	return nil
}

// ensure binds v to a preg if it is not already bound. Pinned vregs evict
// any conflicting holder of their target slot before reserving.
func (s *scanner) ensure(v VReg) error {
	s.recordWidth(v)
	if _, ok := s.pool.bindings[v.ID()]; ok {
		return nil
	}
	if pin, ok := s.pins[v.ID()]; ok {
		if id, busy := s.pool.owner(pin); busy && id != v.ID() {
			s.pool.free(NewVReg(id, pin.Type(), pin.Width()))
		}
		if err := s.pool.reserve(v, pin); err != nil {
			return fmt.Errorf("%w: vreg %v pin %v: %w",
				ErrConflictingPin, v, pin, err)
		}
		s.assigned[v.ID()] = pin
		return nil
	}
	pr, err := s.pool.alloc(v)
	if err != nil {
		return err
	}
	s.assigned[v.ID()] = pr
	return nil
}

func (s *scanner) recordWidth(v VReg) {
	if v.Width() == WidthUndefined {
		return
	}
	if _, ok := s.widths[v.ID()]; ok {
		return
	}
	s.widths[v.ID()] = v.Width()
}

func (s *scanner) backfillPinWidths() {
	for id, pr := range s.pins {
		if w, ok := s.widths[id]; !ok || w == WidthUndefined {
			s.widths[id] = pr.Width()
		}
	}
}

// lastUses returns the highest instruction index at which each vreg is
// referenced (use or def).
func lastUses(insts []Instruction) map[int32]int {
	last := make(map[int32]int)
	for i, inst := range insts {
		if dst, ok := inst.Def(); ok {
			last[dst.ID()] = i
		}
		for _, v := range inst.Uses() {
			last[v.ID()] = i
		}
	}
	return last
}

// rewrite returns a copy of insts with every VReg / MemOperand-base
// rewritten to its assigned PReg. Width defaults to the operand's
// declared width; undefined widths fall back to the per-vreg widths map.
func rewrite(insts []Instruction, phys map[int32]PReg, widths map[int32]RegWidth) []Instruction {
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
		if pr, ok := resolveVReg(v.Reg, phys, widths); ok {
			return P(pr)
		}
	case MemOperand:
		base, isVReg := v.Base.(VRegOperand)
		if !isVReg {
			break
		}
		if pr, ok := resolveVReg(base.Reg, phys, widths); ok {
			return Mem(P(pr), v.Offset)
		}
	}
	return op
}

func resolveVReg(v VReg, phys map[int32]PReg, widths map[int32]RegWidth) (PReg, bool) {
	pr, ok := phys[v.ID()]
	if !ok {
		return PReg{}, false
	}
	w := v.Width()
	if w == WidthUndefined {
		w = widths[v.ID()]
	}
	return NewPReg(pr.ID(), pr.Type(), w), true
}

// encode produces the final byte stream from a sequence of phys-allocated
// instructions. It runs in two passes: the first encodes each
// instruction with placeholder zeros for label operands and records
// cumulative byte offsets, the second emits final bytes — patching
// intra-Code label references and recording external ones as Relocations.
func encode(enc Encoder, insts []Instruction, labels map[Label]int) ([]byte, map[Label]int, []Relocation, error) {
	encoded, offsets, err := encodeWithPlaceholders(enc, insts)
	if err != nil {
		return nil, nil, nil, err
	}

	byteLabels := make(map[Label]int, len(labels))
	for id, idx := range labels {
		byteLabels[id] = offsets[idx]
	}

	out, relocs, err := emitFinal(enc, insts, encoded, offsets, byteLabels)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, byteLabels, relocs, nil
}

// encodeWithPlaceholders encodes each instruction with #0 substituted for
// label operands so we can measure widths without knowing label offsets.
func encodeWithPlaceholders(enc Encoder, insts []Instruction) ([][]byte, []int, error) {
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

// emitFinal walks the encoded list, patching intra-Code label references
// with their resolved delta and recording external references as
// Relocations the linker will patch later.
func emitFinal(
	enc Encoder,
	insts []Instruction,
	encoded [][]byte,
	offsets []int,
	byteLabels map[Label]int,
) ([]byte, []Relocation, error) {
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
		bytes, err := enc.Encode(patched)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, bytes...)
	}
	return out, relocs, nil
}
