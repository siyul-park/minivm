package analysis

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// BlocksAnalysis computes the control-flow blocks of a function.
type BlocksAnalysis struct{}

// BasicBlock is one control-flow region and its predecessor/successor ids.
type BasicBlock struct {
	Start int
	End   int
	Succs []int
	Preds []int
}

// ErrInvalidJump reports a jump target that is not a valid instruction boundary.
var ErrInvalidJump = errors.New("invalid jump")

var _ pass.Analysis[*types.Function, []*BasicBlock] = (*BlocksAnalysis)(nil)

// Blocks builds the control-flow blocks for function.
func Blocks(function *types.Function) ([]*BasicBlock, error) {
	offsets := []int{0}
	for ip := 0; ip < len(function.Code); {
		inst := instr.Instruction(function.Code[ip:])
		next := ip + inst.Width()
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.RETURN, instr.RETURN_CALL, instr.THROW:
			if next < len(function.Code) {
				offsets = append(offsets, next)
			}
		case instr.BR, instr.BR_IF, instr.BR_TABLE:
			for _, offset := range instr.Targets(function.Code, ip) {
				if offset < 0 || offset > len(function.Code) {
					return nil, invalidJumpError(ip, offset)
				}
				if offset < len(function.Code) {
					offsets = append(offsets, offset)
				}
			}
			if inst.Opcode() != instr.BR_TABLE && next < len(function.Code) {
				offsets = append(offsets, next)
			}
		default:
		}
		ip = next
	}

	// Protected-region and catch boundaries start their own blocks so the
	// exception table aligns with the CFG. Throws/traps transfer out of band, so
	// no explicit edges are added; the verifier seeds catch blocks directly.
	for _, h := range function.Handlers {
		for _, off := range []int{h.Start, h.End, h.Catch} {
			if off > 0 && off < len(function.Code) {
				offsets = append(offsets, off)
			}
		}
	}

	slices.Sort(offsets)
	offsets = slices.Compact(offsets)

	blocks := make([]*BasicBlock, len(offsets))
	for j := range offsets {
		end := len(function.Code)
		if j+1 < len(offsets) {
			end = offsets[j+1]
		}
		blocks[j] = &BasicBlock{
			Start: offsets[j],
			End:   end,
		}
	}

	indexByStart := make(map[int]int, len(blocks))
	for j, block := range blocks {
		indexByStart[block.Start] = j
	}

	for j, block := range blocks {
		ip := block.Start
		for ip < block.End {
			inst := instr.Instruction(function.Code[ip:])
			if ip+inst.Width() >= block.End {
				break
			}
			ip += inst.Width()
		}
		if ip >= len(function.Code) {
			continue
		}

		inst := instr.Instruction(function.Code[ip:])
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.RETURN, instr.RETURN_CALL, instr.THROW:
		case instr.BR, instr.BR_IF, instr.BR_TABLE:
			for _, offset := range instr.Targets(function.Code, ip) {
				if !link(blocks, indexByStart, j, offset) {
					return nil, invalidJumpError(ip, offset)
				}
			}
			if inst.Opcode() == instr.BR_IF && j+1 < len(blocks) {
				link(blocks, indexByStart, j, blocks[j+1].Start)
			}
		default:
			if j+1 < len(blocks) {
				link(blocks, indexByStart, j, blocks[j+1].Start)
			}
		}
	}
	for _, block := range blocks {
		slices.Sort(block.Succs)
		block.Succs = slices.Compact(block.Succs)
		slices.Sort(block.Preds)
		block.Preds = slices.Compact(block.Preds)
	}
	return blocks, nil
}

// NewBlocksAnalysis returns a blocks analysis.
func NewBlocksAnalysis() *BlocksAnalysis {
	return &BlocksAnalysis{}
}

// Run computes the control-flow blocks of function.
func (p *BlocksAnalysis) Run(_ *pass.Manager, function *types.Function) ([]*BasicBlock, error) {
	return Blocks(function)
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
