package instr

import (
	"errors"
	"fmt"
	"math"
)

// Builder assembles a single code stream with symbolic branch targets. Emit
// instructions and Label positions in program order, branch to labels with
// Br/BrIf/BrTable, then Assemble to back-patch each branch into the signed
// 16-bit byte offset the interpreter expects (relative to the end of the
// branch instruction). Builders are mutable; discard after Assemble.
type Builder struct {
	instrs []Instruction
	labels []int
	fixups []fixup
}

// Label is an opaque handle to a branch target. Allocate with Builder.Label
// and place with Builder.Bind.
type Label int

type fixup struct {
	branch  int
	operand int
	label   Label
}

var (
	ErrUnboundLabel = errors.New("unbound label")
	ErrOffsetRange  = errors.New("branch offset out of range")
)

func NewBuilder() *Builder {
	return &Builder{}
}

// Label allocates an unbound target. Place it later with Bind.
func (b *Builder) Label() Label {
	b.labels = append(b.labels, -1)
	return Label(len(b.labels) - 1)
}

// Bind fixes l to the next instruction emitted.
func (b *Builder) Bind(l Label) *Builder {
	b.labels[l] = len(b.instrs)
	return b
}

// Emit appends one instruction built from op and operands.
func (b *Builder) Emit(op Opcode, operands ...uint64) *Builder {
	return b.Append(New(op, operands...))
}

// Append appends pre-built instructions verbatim.
func (b *Builder) Append(instrs ...Instruction) *Builder {
	b.instrs = append(b.instrs, instrs...)
	return b
}

// Br emits an unconditional branch to l.
func (b *Builder) Br(l Label) *Builder {
	return b.branch(BR, l)
}

// BrIf emits a conditional branch to l.
func (b *Builder) BrIf(l Label) *Builder {
	return b.branch(BR_IF, l)
}

// BrTable emits a jump table: targets are selected by the index on the stack,
// def is taken when the index is out of range.
func (b *Builder) BrTable(def Label, targets ...Label) *Builder {
	operands := make([]uint64, len(targets)+2)
	operands[0] = uint64(len(targets))
	b.instrs = append(b.instrs, New(BR_TABLE, operands...))

	branch := len(b.instrs) - 1
	for i, target := range targets {
		b.fixups = append(b.fixups, fixup{branch: branch, operand: i + 1, label: target})
	}
	b.fixups = append(b.fixups, fixup{branch: branch, operand: len(targets) + 1, label: def})
	return b
}

// Assemble resolves every branch and returns the patched instructions. It
// fails when a target label was never bound or a branch displacement does not
// fit a signed 16-bit operand.
func (b *Builder) Assemble() ([]Instruction, error) {
	pos := make([]int, len(b.instrs)+1)
	for i, inst := range b.instrs {
		pos[i+1] = pos[i] + inst.Width()
	}

	for _, fx := range b.fixups {
		target := b.labels[fx.label]
		if target < 0 {
			return nil, fmt.Errorf("%w: %d", ErrUnboundLabel, fx.label)
		}
		offset := pos[target] - (pos[fx.branch] + b.instrs[fx.branch].Width())
		if offset < math.MinInt16 || offset > math.MaxInt16 {
			return nil, fmt.Errorf("%w: %d", ErrOffsetRange, offset)
		}
		b.instrs[fx.branch].SetOperand(fx.operand, uint64(uint16(int16(offset))))
	}
	return b.instrs, nil
}

func (b *Builder) branch(op Opcode, l Label) *Builder {
	b.instrs = append(b.instrs, New(op, 0))
	b.fixups = append(b.fixups, fixup{branch: len(b.instrs) - 1, operand: 0, label: l})
	return b
}
