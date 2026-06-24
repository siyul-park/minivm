package transform

import (
	"fmt"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
)

type DeadCodeEliminationPass struct{}

var _ pass.Pass[*program.Program] = (*DeadCodeEliminationPass)(nil)

func NewDeadCodeEliminationPass() *DeadCodeEliminationPass {
	return &DeadCodeEliminationPass{}
}

func (p *DeadCodeEliminationPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
	for i, fn := range functions(prog) {
		code := fn.Code

		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		if err != nil {
			return pass.PreserveNone(), err
		}
		catch := map[int]bool{}
		for _, h := range fn.Handlers {
			catch[h.Catch] = true
		}
		for i := 1; i < len(blocks); i++ {
			blk := blocks[i]
			// Catch blocks are entered out of band, so the CFG gives them no
			// predecessors; they are live roots, not dead code.
			if len(blk.Preds) == 0 && !catch[blk.Start] {
				for j := blk.Start; j < blk.End; j++ {
					code[j] = byte(instr.UNREACHABLE)
				}
			}
		}

		offsets := make([]int, len(code))
		for j := range offsets {
			offsets[j] = -1
		}

		write := 0
		for read := 0; read < len(code); {
			inst := instr.Instruction(code[read:])
			width := inst.Width()
			if inst.Opcode() != instr.NOP && inst.Opcode() != instr.UNREACHABLE {
				offsets[read] = write
				if write != read {
					copy(code[write:write+width], code[read:read+width])
				}
				write += width
			}
			read += width
		}

		code = code[:write]
		if len(code) == 0 {
			code = nil
		}

		read := 0
		write = 0
		for write < len(code) {
			inst := instr.Instruction(code[write:])

			switch inst.Opcode() {
			case instr.BR, instr.BR_IF:
				target := read + instr.ReadI16(inst.Operand(0)) + inst.Width()
				for target >= 0 && target < len(offsets) && offsets[target] == -1 {
					target++
				}
				if target < 0 || target >= len(offsets) {
					return pass.PreserveNone(), fmt.Errorf("%w: at=%d", analysis.ErrInvalidJump, read)
				}
				inst.SetOperand(0, uint64(offsets[target]-write-inst.Width()))
			case instr.BR_TABLE:
				width := inst.Width()
				operands := inst.Operands()
				count := int(operands[0])
				for j := 0; j <= count; j++ {
					target := read + instr.ReadI16(operands[j+1]) + width
					for target >= 0 && target < len(offsets) && offsets[target] == -1 {
						target++
					}
					if target < 0 || target >= len(offsets) {
						return pass.PreserveNone(), fmt.Errorf("%w: at=%d", analysis.ErrInvalidJump, read)
					}
					inst.SetOperand(j+1, uint64(offsets[target]-write-width))
				}
			default:
			}

			write += inst.Width()
			for ; read < len(offsets) && offsets[read] != write; read++ {
			}
		}

		handlers := p.rehandle(fn.Handlers, offsets, len(code))

		fn.Code = code
		fn.Handlers = handlers
		if i == 0 {
			prog.Code = code
			prog.Handlers = handlers
		}
	}

	return pass.PreserveNone(), nil
}

// rehandle remaps an exception table through the compaction offset map: each
// boundary moves to the first surviving instruction at or after its old offset,
// so a region whose body was removed collapses cleanly. offsets[i] is the new
// position of the instruction that began at old offset i, or -1 if removed.
func (p *DeadCodeEliminationPass) rehandle(handlers []instr.Handler, offsets []int, size int) []instr.Handler {
	if len(handlers) == 0 {
		return handlers
	}
	remapped := make([]instr.Handler, len(handlers))
	for i, h := range handlers {
		remapped[i] = instr.Handler{
			Start: p.relocate(offsets, h.Start, size),
			End:   p.relocate(offsets, h.End, size),
			Catch: p.relocate(offsets, h.Catch, size),
			Depth: h.Depth,
		}
	}
	return remapped
}

func (p *DeadCodeEliminationPass) relocate(offsets []int, off, size int) int {
	for off < len(offsets) && offsets[off] == -1 {
		off++
	}
	if off >= len(offsets) {
		return size
	}
	return offsets[off]
}
