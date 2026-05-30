package asm

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
