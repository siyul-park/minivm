package analysis

import (
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

type BasicBlocksPass struct{}

type BasicBlock struct {
	Start int
	End   int
	Succs []int
	Preds []int
}

var _ pass.Pass[[]*BasicBlock] = (*BasicBlocksPass)(nil)

func NewBasicBlocksPass() pass.Pass[[]*BasicBlock] {
	return &BasicBlocksPass{}
}

func (p *BasicBlocksPass) Run(m *pass.Manager) ([]*BasicBlock, error) {
	var fn *types.Function
	if err := m.Load(&fn); err != nil {
		return nil, err
	}

	offsets := []int{0}
	for ip := 0; ip < len(fn.Code); {
		inst := instr.Instruction(fn.Code[ip:])
		next := ip + inst.Width()
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.RETURN:
			if next < len(fn.Code) {
				offsets = append(offsets, next)
			}
		case instr.BR, instr.BR_IF:
			offset := ip + inst.Width() + int(inst.Operand(0))
			if offset < 0 || offset >= len(fn.Code) {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
			}
			offsets = append(offsets, offset)
			if next < len(fn.Code) {
				offsets = append(offsets, next)
			}
		case instr.BR_TABLE:
			operands := inst.Operands()
			count := int(operands[0])
			for j := 0; j < count; j++ {
				offset := ip + inst.Width() + int(operands[j+1])
				if offset < 0 || offset >= len(fn.Code) {
					return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
				}
				offsets = append(offsets, offset)
			}
			offset := ip + inst.Width() + int(operands[len(operands)-1])
			if offset < 0 || offset >= len(fn.Code) {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
			}
			offsets = append(offsets, offset)
		default:
		}
		ip = next
	}

	slices.Sort(offsets)
	offsets = slices.Compact(offsets)

	blocks := make([]*BasicBlock, len(offsets))
	for j := range offsets {
		end := len(fn.Code)
		if j+1 < len(offsets) {
			end = offsets[j+1]
		}
		blocks[j] = &BasicBlock{
			Start: offsets[j],
			End:   end,
		}
	}

	for j, blk := range blocks {
		ip := blk.Start
		for ip < blk.End {
			inst := instr.Instruction(fn.Code[ip:])
			if ip+inst.Width() >= blk.End {
				break
			}
			ip += inst.Width()
		}
		if ip >= len(fn.Code) {
			continue
		}

		inst := instr.Instruction(fn.Code[ip:])
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.RETURN:
		case instr.BR, instr.BR_IF:
			offset := ip + inst.Width() + int(inst.Operand(0))
			if !p.link(blocks, j, offset) {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
			}
			if inst.Opcode() == instr.BR_IF && j+1 < len(blocks) {
				blk.Succs = append(blk.Succs, j+1)
				blocks[j+1].Preds = append(blocks[j+1].Preds, j)
			}
		case instr.BR_TABLE:
			width := inst.Width()
			operands := inst.Operands()
			count := int(operands[0])
			for k := 0; k < count; k++ {
				offset := ip + int(operands[k+1]) + width
				if !p.link(blocks, j, offset) {
					return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
				}
			}
			offset := ip + int(operands[len(operands)-1]) + width
			if !p.link(blocks, j, offset) {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
			}
		default:
			if j+1 < len(blocks) {
				blk.Succs = append(blk.Succs, j+1)
				blocks[j+1].Preds = append(blocks[j+1].Preds, j)
			}
		}
	}
	for _, b := range blocks {
		slices.Sort(b.Succs)
		slices.Sort(b.Preds)
	}
	return blocks, nil
}

func (p *BasicBlocksPass) link(blocks []*BasicBlock, src, dst int) bool {
	for i, b := range blocks {
		if b.Start <= dst && dst < b.End {
			blocks[src].Succs = append(blocks[src].Succs, i)
			blocks[i].Preds = append(blocks[i].Preds, src)
			return true
		}
	}
	return false
}
