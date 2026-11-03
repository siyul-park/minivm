package instr

import (
	"errors"
	"fmt"
	"math"
)

// Label is an opaque handle to a branch target. Allocate with Builder.Label
// and place with Builder.Bind. The zero Label is invalid, and a Label is usable
// only with the Builder that allocated it.
type Label struct {
	b *Builder
	n int
}

// Builder assembles a single code stream with symbolic branch targets. Emit
// instructions and Label positions in program order, branch to labels with
// Br/BrIf/BrTable, then Assemble to back-patch each branch into the signed
// 16-bit byte offset the interpreter expects (relative to the end of the
// branch instruction).
//
// Errors are sticky: the builder records the first one, every later call is a
// no-op, and Assemble returns it. A Builder is single-use and must not be
// reused after Assemble.
type Builder struct {
	instrs   []Instruction
	labels   []int
	fixups   []fixup
	handlers []handlerFixup
	resolved []Handler
	err      error
}

type fixup struct {
	branch  int
	operand int
	label   Label
}

// handlerFixup records a protected region declared with symbolic labels.
// Assemble resolves each label to a byte offset and emits a Handler.
type handlerFixup struct {
	start Label
	end   Label
	catch Label
	depth int
}

var (
	// ErrUnboundLabel reports a branch to a label that was never bound.
	ErrUnboundLabel = errors.New("unbound label")
	// ErrBoundLabel reports a label bound more than once.
	ErrBoundLabel = errors.New("label already bound")
	// ErrForeignLabel reports a zero label or one allocated by another Builder.
	ErrForeignLabel = errors.New("foreign label")
	// ErrInvalidInstruction reports an appended instruction with no known opcode or whose length does not match its operands.
	ErrInvalidInstruction = errors.New("invalid instruction")
	// ErrOffsetRange reports a branch whose offset does not fit its operand width.
	ErrOffsetRange = errors.New("branch offset out of range")
)

// NewBuilder returns an empty instruction builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Label allocates an unbound target. Place it later with Bind.
func (b *Builder) Label() Label {
	b.labels = append(b.labels, -1)
	return Label{b: b, n: len(b.labels) - 1}
}

// Bind fixes l to the next instruction emitted.
func (b *Builder) Bind(l Label) *Builder {
	if !b.own(l) {
		return b
	}
	if b.labels[l.n] >= 0 {
		b.err = fmt.Errorf("%w: %d", ErrBoundLabel, l.n)
		return b
	}
	b.labels[l.n] = len(b.instrs)
	return b
}

// Emit appends one instruction built from op and operands.
func (b *Builder) Emit(op Opcode, operands ...uint64) *Builder {
	if b.err != nil {
		return b
	}
	inst, err := New(op, operands...)
	if err != nil {
		b.err = err
		return b
	}
	b.instrs = append(b.instrs, inst)
	return b
}

// Append appends pre-built instructions verbatim.
func (b *Builder) Append(instrs ...Instruction) *Builder {
	if b.err != nil {
		return b
	}
	for i, inst := range instrs {
		if len(inst) == 0 || !Valid(inst.Opcode()) {
			b.err = fmt.Errorf("%w: instruction %d has no known opcode", ErrInvalidInstruction, i)
			return b
		}
		width := 1
		for _, w := range inst.Type().Widths {
			if w < 0 {
				if width >= len(inst) {
					width = -1
					break
				}
				width += int(inst[width]) * -w
				w = 1
			}
			width += w
		}
		if width != len(inst) {
			b.err = fmt.Errorf("%w: instruction %d is %d bytes, its operands need %d", ErrInvalidInstruction, i, len(inst), width)
			return b
		}
	}
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
	if !b.own(def) {
		return b
	}
	for _, target := range targets {
		if !b.own(target) {
			return b
		}
	}

	operands := make([]uint64, len(targets)+2)
	operands[0] = uint64(len(targets))
	if b.Emit(BR_TABLE, operands...); b.err != nil {
		return b
	}

	branch := len(b.instrs) - 1
	for i, target := range targets {
		b.fixups = append(b.fixups, fixup{branch: branch, operand: i + 1, label: target})
	}
	b.fixups = append(b.fixups, fixup{branch: branch, operand: len(targets) + 1, label: def})
	return b
}

// Try declares a protected region [start, end) whose throws and traps land on
// catch, with depth the operand-stack height at the region's entry. Declare
// inner regions before the outer ones enclosing them so the assembled table
// stays innermost-first. The labels are resolved by Assemble; read the table
// back with Handlers.
func (b *Builder) Try(start, end, catch Label, depth int) *Builder {
	if !b.own(start) || !b.own(end) || !b.own(catch) {
		return b
	}
	b.handlers = append(b.handlers, handlerFixup{start: start, end: end, catch: catch, depth: depth})
	return b
}

// Assemble resolves every branch and protected region, returning the patched
// instructions. It returns the first recorded error, and fails when a target
// label was never bound or a branch displacement does not fit a signed 16-bit
// operand. Read the assembled exception table with Handlers afterwards.
func (b *Builder) Assemble() ([]Instruction, error) {
	if b.err != nil {
		return nil, b.err
	}
	pos := make([]int, len(b.instrs)+1)
	for i, inst := range b.instrs {
		pos[i+1] = pos[i] + inst.Width()
	}

	for _, fx := range b.fixups {
		target, err := b.resolve(pos, fx.label)
		if err != nil {
			return nil, err
		}
		offset := target - pos[fx.branch+1]
		if offset < math.MinInt16 || offset > math.MaxInt16 {
			return nil, fmt.Errorf("%w: %d", ErrOffsetRange, offset)
		}
		b.instrs[fx.branch].SetOperand(fx.operand, uint64(uint16(int16(offset))))
	}

	b.resolved = b.resolved[:0]
	for _, hx := range b.handlers {
		start, err := b.resolve(pos, hx.start)
		if err != nil {
			return nil, err
		}
		end, err := b.resolve(pos, hx.end)
		if err != nil {
			return nil, err
		}
		catch, err := b.resolve(pos, hx.catch)
		if err != nil {
			return nil, err
		}
		b.resolved = append(b.resolved, Handler{Start: start, End: end, Catch: catch, Depth: hx.depth})
	}
	return b.instrs, nil
}

// Handlers returns the exception table assembled from Try declarations. Call it
// after Assemble; the result is nil when no protected region was declared.
func (b *Builder) Handlers() []Handler {
	if len(b.resolved) == 0 {
		return nil
	}
	return append([]Handler(nil), b.resolved...)
}

func (b *Builder) resolve(pos []int, l Label) (int, error) {
	target := b.labels[l.n]
	if target < 0 {
		return 0, fmt.Errorf("%w: %d", ErrUnboundLabel, l.n)
	}
	return pos[target], nil
}

func (b *Builder) branch(op Opcode, l Label) *Builder {
	if !b.own(l) {
		return b
	}
	if b.Emit(op, 0); b.err != nil {
		return b
	}
	b.fixups = append(b.fixups, fixup{branch: len(b.instrs) - 1, operand: 0, label: l})
	return b
}

// own reports whether l belongs to b. A foreign or zero label records
// ErrForeignLabel; once any error is recorded it reports false.
func (b *Builder) own(l Label) bool {
	if b.err != nil {
		return false
	}
	if l.b != b {
		b.err = ErrForeignLabel
		return false
	}
	return true
}
