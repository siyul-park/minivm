package analysis

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type ModuleBuilder struct {
	code      []byte
	constants []types.Value
}

type Module struct {
	Functions []*Function
	Constants []types.Value
}

type Function struct {
	*types.Function
	CFG *CFG
}

type CFG struct {
	Blocks []*Block
}

type Block struct {
	Offset int
	Code   []byte
	Succs  []int
	Preds  []int
}

var ErrInvalidJump = errors.New("invalid jump")

func NewModuleBuilder(prog *program.Program) *ModuleBuilder {
	return &ModuleBuilder{
		code:      prog.Code,
		constants: prog.Constants,
	}
}

func (b *ModuleBuilder) Build() (*Module, error) {
	fns := []*types.Function{{Typ: &types.FunctionType{}, Code: b.code}}
	for _, v := range b.constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, fn)
		}
	}

	m := &Module{
		Functions: make([]*Function, len(fns)),
		Constants: b.constants,
	}
	for i, fn := range fns {
		cfg, err := b.buildCFG(fn)
		if err != nil {
			return nil, err
		}
		m.Functions[i] = &Function{
			Function: fn,
			CFG:      cfg,
		}
	}
	return m, nil
}

func (b *ModuleBuilder) buildCFG(fn *types.Function) (*CFG, error) {
	code := fn.Code

	offsets := []int{0}
	for ip := 0; ip < len(code); {
		op := instr.Opcode(code[ip])
		typ := instr.TypeOf(instr.Opcode(code[ip]))
		next := ip + typ.Size()

		switch op {
		case instr.UNREACHABLE, instr.RETURN:
			if next < len(code) {
				offsets = append(offsets, next)
			}
		case instr.BR, instr.BR_IF:
			offset := int(int32(binary.BigEndian.Uint16(code[ip+1:])))
			target := ip + typ.Size() + offset
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

	blocks := make([]*Block, len(offsets))
	for i := range offsets {
		end := len(code)
		if i+1 < len(offsets) {
			end = offsets[i+1]
		}
		blocks[i] = &Block{Offset: offsets[i], Code: code[offsets[i]:end]}
	}

	for i, blk := range blocks {
		ip := 0
		for ip < len(code) {
			op := instr.Opcode(blk.Code[ip])
			typ := instr.TypeOf(op)
			if ip+typ.Size() >= len(blk.Code) {
				break
			}
			ip += typ.Size()
		}
		if ip == len(code) {
			continue
		}

		op := instr.Opcode(blk.Code[ip])
		typ := instr.TypeOf(op)

		switch op {
		case instr.UNREACHABLE, instr.RETURN:
		case instr.BR, instr.BR_IF:
			offset := int(int32(binary.BigEndian.Uint16(blk.Code[ip+1:])))
			target := ip + typ.Size() + offset

			found := false
			for j, blk := range blocks {
				if blk.Offset <= target && target < blk.Offset+len(blk.Code) {
					blocks[i].Succs = append(blocks[i].Succs, j)
					blocks[j].Preds = append(blocks[j].Preds, i)
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: at=%d", ErrInvalidJump, blk.Offset+ip)
			}

			if op == instr.BR_IF && i+1 < len(blocks) {
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

	return &CFG{Blocks: blocks}, nil
}
