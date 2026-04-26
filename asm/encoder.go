package asm

type Encoder interface {
	Encode(inst Instruction) ([]byte, error)
}

func Encode(encoder Encoder, insts []Instruction) ([]byte, error) {
	buf := make([]byte, 0, len(insts)*4)
	for _, inst := range insts {
		b, err := encoder.Encode(inst)
		if err != nil {
			return nil, err
		}
		buf = append(buf, b...)
	}
	return buf, nil
}
