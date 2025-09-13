package analysis

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
)

type CFG struct {
	Blocks []*BasicBlock
}

type BasicBlock struct {
	Start int
	End   int
	Succs []int
	Preds []int
}

var ErrInvalidJump = errors.New("invalid jump")

func BuildCFG(code []byte) (*CFG, error) {
	cfg := &CFG{}

	starts := []int{0}
	for ip := 0; ip < len(code); {
		op := instr.Opcode(code[ip])
		typ := instr.TypeOf(op)

		next := ip + typ.Size()

		switch op {
		case instr.UNREACHABLE, instr.RETURN:
			if next < len(code) {
				starts = append(starts, next)
			}
		case instr.BR, instr.BR_IF:
			offset := int(int32(binary.BigEndian.Uint32(code[ip+1:])))
			target := ip + offset + 5
			if target < 0 || target >= len(code) {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
			}
			starts = append(starts, target)
			if next < len(code) {
				starts = append(starts, next)
			}
		default:
		}

		ip = next
	}

	slices.Sort(starts)
	starts = slices.Compact(starts)

	for i := 0; i < len(starts); i++ {
		start := starts[i]
		end := len(code)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		cfg.Blocks = append(cfg.Blocks, &BasicBlock{
			Start: start,
			End:   end,
		})
	}

	for i, b := range cfg.Blocks {
		last := b.Start
		for ip := b.Start; ip < b.End; {
			op := instr.Opcode(code[ip])
			typ := instr.TypeOf(op)
			last = ip
			ip += typ.Size()
		}

		op := instr.Opcode(code[last])
		typ := instr.TypeOf(op)

		switch op {
		case instr.UNREACHABLE, instr.RETURN:
		case instr.BR, instr.BR_IF:
			offset := int(int32(binary.BigEndian.Uint32(code[last+1:])))
			target := last + typ.Size() + offset

			found := false
			for j, blk := range cfg.Blocks {
				if blk.Start <= target && target < blk.End {
					cfg.Blocks[i].Succs = append(cfg.Blocks[i].Succs, j)
					cfg.Blocks[j].Preds = append(cfg.Blocks[j].Preds, i)
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, last)
			}

			if op == instr.BR_IF && i+1 < len(cfg.Blocks) {
				cfg.Blocks[i].Succs = append(cfg.Blocks[i].Succs, i+1)
				cfg.Blocks[i+1].Preds = append(cfg.Blocks[i+1].Preds, i)
			}
		default:
			if i+1 < len(cfg.Blocks) {
				cfg.Blocks[i].Succs = append(cfg.Blocks[i].Succs, i+1)
				cfg.Blocks[i+1].Preds = append(cfg.Blocks[i+1].Preds, i)
			}
		}
	}

	for _, b := range cfg.Blocks {
		slices.Sort(b.Succs)
		slices.Sort(b.Preds)
	}

	return cfg, nil
}
