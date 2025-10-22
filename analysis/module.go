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

type ModulePass struct{}

type Module struct {
	EntryPoint *Function
	Functions  []*Function
	Constants  []types.Value
	Types      []types.Type
}

type Function struct {
	*types.Function
	Blocks []*BasicBlock
}

type BasicBlock struct {
	Start int
	End   int
	Succs []int
	Preds []int
}

var _ pass.Pass[*Module] = (*ModulePass)(nil)

var ErrInvalidJump = errors.New("invalid jump")

func NewModulePass() pass.Pass[*Module] {
	return &ModulePass{}
}

func (p *ModulePass) Run(m *pass.Manager) (*Module, error) {
	var prog *program.Program
	if err := m.Load(&prog); err != nil {
		return nil, err
	}

	var fns []*Function
	fns = append(fns, &Function{
		Function: &types.Function{
			Signature: types.NewFunctionSignature(),
			Code:      prog.Code,
		},
	})
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, &Function{Function: fn})
		}
	}

	for _, fn := range fns {
		blocks, err := p.Blocks(fn.Code)
		if err != nil {
			return nil, err
		}
		fn.Blocks = blocks
	}

	mdl := &Module{
		EntryPoint: fns[0],
		Constants:  prog.Constants,
		Types:      prog.Types,
	}
	if len(fns) > 1 {
		mdl.Functions = fns[1:]
	}
	return mdl, nil
}

func (p *ModulePass) Blocks(code []byte) ([]*BasicBlock, error) {
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
			offset := ip + inst.Width() + int(inst.Operand(0))
			if offset < 0 || offset >= len(code) {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
			}
			offsets = append(offsets, offset)
			if next < len(code) {
				offsets = append(offsets, next)
			}
		case instr.BR_TABLE:
			operands := inst.Operands()
			count := int(operands[0])
			for j := 0; j < count; j++ {
				offset := ip + inst.Width() + int(operands[j+1])
				if offset < 0 || offset >= len(code) {
					return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
				}
				offsets = append(offsets, offset)
			}
			offset := ip + inst.Width() + int(operands[len(operands)-1])
			if offset < 0 || offset >= len(code) {
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
		end := len(code)
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
			inst := instr.Instruction(code[ip:])
			if ip+inst.Width() >= blk.End {
				break
			}
			ip += inst.Width()
		}
		if ip >= len(code) {
			continue
		}

		inst := instr.Instruction(code[ip:])
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

func (p *ModulePass) link(blocks []*BasicBlock, src, dst int) bool {
	for i, b := range blocks {
		if b.Start <= dst && dst < b.End {
			blocks[src].Succs = append(blocks[src].Succs, i)
			blocks[i].Preds = append(blocks[i].Preds, src)
			return true
		}
	}
	return false
}

func (m *Module) AllFunctions() []*Function {
	fns := make([]*Function, 0, len(m.Functions)+1)
	if m.EntryPoint != nil {
		fns = append(fns, m.EntryPoint)
	}
	fns = append(fns, m.Functions...)
	return fns
}
