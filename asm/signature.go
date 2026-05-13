package asm

// Signature describes the calling convention of a compiled block.
//
// Each PReg carries its physical register ID, type (int/float), and width,
// so no separate type or width slices are needed.
//
// ABI layout (inputs = Params, outputs = Returns):
//
//	inputs:  Params[0], Params[1], …   — physical registers X0/D0, X1/D1, …
//	outputs: Returns[0], Returns[1], … — same registers (different direction)
//
// Scratch registers (Arch.Scratch) live outside the ABI range and carry
// out-of-band inputs/outputs (e.g. VM context pointers in, next interpreter IP
// out).
type Signature struct {
	Entry   int
	Inputs  map[int][]PReg
	Outputs map[int][]PReg
	Scratch []PReg
}

func (s *Signature) Params(idx int) []PReg {
	return append([]PReg(nil), s.Inputs[idx]...)
}

func (s *Signature) Returns(idx int) []PReg {
	return append([]PReg(nil), s.Outputs[idx]...)
}

func (s *Signature) MaxReturns() int {
	n := 0
	for _, regs := range s.Outputs {
		if len(regs) > n {
			n = len(regs)
		}
	}
	return n
}
