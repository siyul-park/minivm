package analysis

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Module struct {
	Functions []*Function
	Constants []types.Value
	Types     []types.Type
}

type Function struct {
	*types.Function
	Blocks []*BasicBlock
}

type BasicBlock struct {
	Offset int
	Code   []byte
	Succs  []int
	Preds  []int
}

var ErrInvalidJump = errors.New("invalid jump")

func NewModulePass() pass.Pass[*Module] {
	return pass.NewPass[*Module](func(m *pass.Manager) (*Module, error) {
		var prog *program.Program
		if err := m.Load(&prog); err != nil {
			return nil, err
		}

		fns := []*types.Function{{Signature: &types.FunctionSignature{}, Code: prog.Code}}
		for _, v := range prog.Constants {
			if fn, ok := v.(*types.Function); ok {
				fns = append(fns, fn)
			}
		}

		mdl := &Module{
			Functions: make([]*Function, len(fns)),
			Constants: prog.Constants,
			Types:     prog.Types,
		}
		for i, f := range fns {
			fn := &Function{Function: f}
			code := fn.Code

			offsets := []int{0}
			for ip := 0; ip < len(code); {
				inst := instr.Instruction(code[ip:])
				next := ip + inst.Width()
				switch inst.Opcode() {
				case instr.UNREACHABLE, instr.RETURN:
					if next < len(code) {
						offsets = append(offsets, next)
					}
				case instr.BR, instr.BR_IF:
					offset := int(inst.Operand(0))
					target := ip + inst.Width() + offset
					if target < 0 || target >= len(code) {
						return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
					}
					offsets = append(offsets, target)
					if next < len(code) {
						offsets = append(offsets, next)
					}
				default:
				}

				ip = next
			}

			slices.Sort(offsets)
			offsets = slices.Compact(offsets)

			blocks := make([]*BasicBlock, len(offsets))
			for i := range offsets {
				end := len(code)
				if i+1 < len(offsets) {
					end = offsets[i+1]
				}
				blocks[i] = &BasicBlock{Offset: offsets[i], Code: code[offsets[i]:end]}
			}

			for i, blk := range blocks {
				ip := 0
				for ip < len(code) {
					inst := instr.Instruction(blk.Code[ip:])
					if ip+inst.Width() >= len(blk.Code) {
						break
					}
					ip += inst.Width()
				}
				if ip == len(code) {
					continue
				}

				inst := instr.Instruction(blk.Code[ip:])

				switch inst.Opcode() {
				case instr.UNREACHABLE, instr.RETURN:
				case instr.BR, instr.BR_IF:
					offset := int(inst.Operand(0))
					target := ip + inst.Width() + offset

					ok := false
					for j, blk := range blocks {
						if blk.Offset <= target && target < blk.Offset+len(blk.Code) {
							blocks[i].Succs = append(blocks[i].Succs, j)
							blocks[j].Preds = append(blocks[j].Preds, i)
							ok = true
							break
						}
					}
					if !ok {
						return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, blk.Offset+ip)
					}

					if inst.Opcode() == instr.BR_IF && i+1 < len(blocks) {
						blocks[i].Succs = append(blocks[i].Succs, i+1)
						blocks[i+1].Preds = append(blocks[i+1].Preds, i)
					}
				default:
					if i+1 < len(blocks) {
						blocks[i].Succs = append(blocks[i].Succs, i+1)
						blocks[i+1].Preds = append(blocks[i+1].Preds, i)
					}
				}
			}

			for _, b := range blocks {
				slices.Sort(b.Succs)
				slices.Sort(b.Preds)
			}

			fn.Blocks = blocks
			mdl.Functions[i] = fn
		}

		return mdl, nil
	})
}
