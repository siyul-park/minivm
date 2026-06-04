package asm

type emitter struct {
	arch   Arch
	labels map[Label]int
}

// run produces the final byte stream from phys-allocated instructions.
// It runs in two passes: draft encodes instructions with placeholder label
// operands and records byte offsets; final patches intra-Code labels and
// records external labels as relocations.
func (e *emitter) run(insts []Instruction) ([]byte, map[Label]int, []Relocation, error) {
	encoded, offsets, err := e.draft(insts)
	if err != nil {
		return nil, nil, nil, err
	}

	pos := make(map[Label]int, len(e.labels))
	for id, idx := range e.labels {
		pos[id] = offsets[idx]
	}

	out, relocs, err := e.final(insts, encoded, offsets, pos)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, pos, relocs, nil
}

// draft encodes each instruction with #0 substituted for label operands so
// widths can be measured without knowing label offsets.
func (e *emitter) draft(insts []Instruction) ([][]byte, []int, error) {
	enc := e.arch.Encoder()
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
func (e *emitter) final(
	insts []Instruction,
	encoded [][]byte,
	offsets []int,
	labels map[Label]int,
) ([]byte, []Relocation, error) {
	enc := e.arch.Encoder()
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
