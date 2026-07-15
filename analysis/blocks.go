package analysis

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

type BlocksAnalysis struct{}

type BasicBlock struct {
	Start int
	End   int
	Succs []int
	Preds []int
}

var ErrInvalidJump = errors.New("invalid jump")

var _ pass.Analysis[*types.Function, []*BasicBlock] = (*BlocksAnalysis)(nil)

func NewBlocksAnalysis() *BlocksAnalysis {
	return &BlocksAnalysis{}
}

// Blocks builds the control-flow blocks for fn.
func Blocks(fn *types.Function) ([]*BasicBlock, error) {
	offsets := []int{0}
	mark := func(ip, target int) error {
		if target < 0 || target > len(fn.Code) {
			return invalidJumpError(ip, target)
		}
		if target < len(fn.Code) {
			offsets = append(offsets, target)
		}
		return nil
	}
	for ip := 0; ip < len(fn.Code); {
		inst := instr.Instruction(fn.Code[ip:])
		next := ip + inst.Width()
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.RETURN, instr.RETURN_CALL, instr.THROW:
			if next < len(fn.Code) {
				offsets = append(offsets, next)
			}
		case instr.BR, instr.BR_IF, instr.BR_TABLE:
			for _, offset := range instr.Targets(fn.Code, ip) {
				if err := mark(ip, offset); err != nil {
					return nil, err
				}
			}
			if inst.Opcode() != instr.BR_TABLE && next < len(fn.Code) {
				offsets = append(offsets, next)
			}
		default:
		}
		ip = next
	}

	// Protected-region and catch boundaries start their own blocks so the
	// exception table aligns with the CFG. Throws/traps transfer out of band, so
	// no explicit edges are added; the verifier seeds catch blocks directly.
	for _, h := range fn.Handlers {
		for _, off := range []int{h.Start, h.End, h.Catch} {
			if off > 0 && off < len(fn.Code) {
				offsets = append(offsets, off)
			}
		}
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

	indexByStart := make(map[int]int, len(blocks))
	for idx, block := range blocks {
		indexByStart[block.Start] = idx
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
		case instr.UNREACHABLE, instr.RETURN, instr.RETURN_CALL, instr.THROW:
		case instr.BR, instr.BR_IF, instr.BR_TABLE:
			for _, offset := range instr.Targets(fn.Code, ip) {
				if !link(blocks, indexByStart, j, offset) {
					return nil, invalidJumpError(ip, offset)
				}
			}
			if inst.Opcode() == instr.BR_IF && j+1 < len(blocks) {
				blk.Succs = append(blk.Succs, j+1)
				blocks[j+1].Preds = append(blocks[j+1].Preds, j)
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
		b.Succs = slices.Compact(b.Succs)
		slices.Sort(b.Preds)
		b.Preds = slices.Compact(b.Preds)
	}
	return blocks, nil
}

func (p *BlocksAnalysis) Run(m *pass.Manager, fn *types.Function) ([]*BasicBlock, error) {
	return Blocks(fn)
}

func link(blocks []*BasicBlock, indexByStart map[int]int, src, dst int) bool {
	// The past-the-end offset is a virtual exit, not an empty basic block.
	if dst == blocks[len(blocks)-1].End {
		return true
	}
	if i, ok := indexByStart[dst]; ok {
		blocks[src].Succs = append(blocks[src].Succs, i)
		blocks[i].Preds = append(blocks[i].Preds, src)
		return true
	}
	return false
}

func invalidJumpError(ip, target int) error {
	return fmt.Errorf("%w: at=%d target=%d", ErrInvalidJump, ip, target)
}
