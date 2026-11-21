// Package arm64 lowers SSA to ARM64 rows.
package arm64

import (
	"slices"

	"github.com/siyul-park/minivm/internal/asm"
	target "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// Machine emits ARM64 rows for compile.Lower. X25 holds the frame base,
// the address of the activation's VM slot 0; X16 and X17 are scratch inside
// one lowered row sequence.
type Machine struct {
	kinds []types.Kind
	temp  int32
	end   asm.Label
	// entry is the function's own first row, what Context.Natives would
	// hold for it: a self call branches here directly.
	entry asm.Label
	// guards is the shape every OpGuardShape result admits, so a container
	// exec op can assert its own container argument is one (compile.Site
	// carries no such query).
	guards map[ssa.Value]ssa.Shape
	// slices is the element pointer and length each OpArrayView result's array
	// had where the slice ran; array ops naming the result read these.
	slices map[ssa.Value][2]asm.VReg
	// bounds is the index register and slice each OpArrayBound result names.
	bounds map[ssa.Value]bound
	// registers is this function's register-convention results (see compile's
	// registers), empty when TermReturn boxes to the VM frame instead.
	registers []types.Kind
	// borrows is per-parameter, from transform.Borrows: TermReturn releases a
	// borrowed slot only at depth 1, the Go-entered activation, since
	// threaded CALL pushed it owned; a native caller lent it instead. Empty
	// for an OSR unit, whose threaded-entered frame owns every slot.
	borrows []bool
	// upvals holds the activation's upvals base once Prologue loads it
	// (Layout.Upvals); upval slots address it.
	upvals asm.VReg
	// flag is the compare Site.Fuse left in the condition flags, true
	// under cond, for the TermBrIf right after it.
	flag ssa.Value
	cond uint8
}

// bound is an index register proven within the slice an OpArrayBound names.
type bound struct {
	slice ssa.Value
	index asm.VReg
}

// table is the words of a guarded map's table that a probe reads: the map's
// own data word, the control bytes, the key and value words, and the mask.
type table struct {
	data, ctrl, slots, mask asm.VReg
}

// Ctx holds the *jit.Context for as long as native code runs: the trampoline
// (internal/jit/enter_arm64.s) hands it over in R26 and reads it back on every
// exit, so native code never writes it.
const Ctx = target.X26

// imm12 is the largest immediate the lowerings place in one 12-bit field.
const imm12 = 0xFFF

// jumps is the conditional branch taken under each condition code.
var jumps = [...]target.Op{
	target.CondEQ: target.OpBEQ, target.CondNE: target.OpBNE,
	target.CondCS: target.OpBCS, target.CondCC: target.OpBCC,
	target.CondMI: target.OpBMI, target.CondPL: target.OpBPL,
	target.CondVS: target.OpBVS, target.CondVC: target.OpBVC,
	target.CondHI: target.OpBHI, target.CondLS: target.OpBLS,
	target.CondGE: target.OpBGE, target.CondLT: target.OpBLT,
	target.CondGT: target.OpBGT, target.CondLE: target.OpBLE,
}

// integer and float map a condition to the flags test that decides it. A float
// condition absent from float has no lowering: the unordered ones exist so
// every condition has an inverse, and no opcode compares by them.
var (
	integer = map[ssa.Cond]uint8{
		ssa.CondEQ: target.CondEQ, ssa.CondNE: target.CondNE,
		ssa.CondSLT: target.CondLT, ssa.CondSLE: target.CondLE, ssa.CondSGT: target.CondGT, ssa.CondSGE: target.CondGE,
		ssa.CondULT: target.CondCC, ssa.CondULE: target.CondLS, ssa.CondUGT: target.CondHI, ssa.CondUGE: target.CondCS,
	}
	float = map[ssa.Cond]uint8{
		ssa.CondOEQ: target.CondEQ, ssa.CondUNE: target.CondNE,
		ssa.CondOLT: target.CondMI, ssa.CondOLE: target.CondLS, ssa.CondOGT: target.CondGT, ssa.CondOGE: target.CondGE,
	}
)

// New returns an ARM64 machine.
func New() *Machine {
	return &Machine{}
}

// Arch returns the ARM64 assembler target.
func (m *Machine) Arch() asm.Arch {
	return target.New()
}

// Reserve returns the scratch, budget, frame-base, context, and record-pointer
// registers, and Go's goroutine register X28, which the trampoline does not
// restore on its way back to Go.
func (m *Machine) Reserve() []asm.PReg {
	return []asm.PReg{target.X16, target.X17, target.X24, target.X25, Ctx, target.X27, target.X28}
}

// Prologue builds the frame, records the activation (its caller's spill base
// and return address), counts the entry when
// enabled, and starts non-parameter locals at their zeros, loading each
// distinct zero once. Register-convention arguments move from X0/X1 into args
// before those registers are repurposed; the upvals base loads after them.
// A Machine lowers many functions in sequence (a Queue worker reuses one), so
// Prologue resets all per-function state.
func (m *Machine) Prologue(a *asm.Assembler, address int, count bool, l compile.Layout, args []asm.VReg) {

	*m = Machine{kinds: l.Kinds, temp: -1, end: a.Label(), entry: a.Label(), guards: map[ssa.Value]ssa.Shape{}, slices: map[ssa.Value][2]asm.VReg{}, bounds: map[ssa.Value]bound{}, registers: l.Registers, borrows: l.Borrows}
	a.Bind(m.entry)
	a.Emit(
		target.ADDI(target.X16, target.SP, 0),
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		asm.Instruction{Op: uint16(target.OpSUBI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.STP(target.X25, target.X16, target.X27, int16(jit.RecordFB)),
		target.STR(target.LR, target.X27, int16(jit.RecordPC)),
		target.ADDI(target.X27, target.X27, uint16(jit.SizeofRecord)),
	)
	if count {
		a.Emit(target.LDR(target.X16, Ctx, int16(jit.OffsetEntries)))
		off := int16(8 * address)
		if 8*address > imm12 {
			a.Emit(target.LDI(target.X17, uint64(8*address))...)
			a.Emit(target.ADD(target.X16, target.X16, target.X17))
			off = 0
		}
		a.Emit(
			target.LDR(target.X17, target.X16, off),
			target.ADDI(target.X17, target.X17, 1),
			target.STR(target.X17, target.X16, off),
		)
	}
	base := len(l.Kinds) - len(l.Zeros)
	for i, word := range l.Zeros {
		if slices.Contains(l.Zeros[:i], word) {
			continue
		}
		src := target.XZR
		if word != 0 {
			a.Emit(target.LDI(target.X16, uint64(word))...)
			src = target.X16
		}
		for j := i; j < len(l.Zeros); j++ {
			if l.Zeros[j] == word {
				a.Emit(target.STR(src, target.X25, int16((base+j)*8)))
			}
		}
	}
	for i := range args {
		a.Emit(target.DEF(register(i)))
	}
	for i, dst := range args {
		convention(a, dst, i, true)
	}
	if l.Upvals {
		m.upvals = m.vreg()
		a.Emit(target.LDR(m.upvals, Ctx, int16(jit.OffsetUpvals)))
	}
}

// Epilogue ends a function: every return branches here to pop the record
// and the frame.
func (m *Machine) Epilogue(a *asm.Assembler) {
	a.Bind(m.end)
	a.Emit(
		target.SUBI(target.X27, target.X27, uint16(jit.SizeofRecord)),
		asm.Instruction{Op: uint16(target.OpADDI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LDR(target.LR, target.SP, 8),
		target.ADDI(target.SP, target.SP, 16),
		target.RET(),
	)
}

// Enter emits the Go entry stub after the epilogue so native-to-native calls
// still enter at offset 0. The stub loads pinned state and register arguments,
// calls the body, then boxes register results into the VM frame.
func (m *Machine) Enter(a *asm.Assembler, l compile.Layout) asm.Label {
	label := a.Label()
	a.Bind(label)
	a.Emit(
		target.LDR(target.X25, Ctx, int16(jit.OffsetFB)),
		target.LDR(target.X27, Ctx, int16(jit.OffsetNext)),
		target.LDR(target.X24, Ctx, int16(jit.OffsetBudget)),
	)
	for i, k := range l.Arguments {
		dst := register(i)
		switch k.Repr() {
		case types.KindRef, types.KindF64:
			a.Emit(target.LDR(dst, target.X25, int16(8*i)))
		case types.KindI64:
			a.Emit(target.LDR(dst, target.X25, int16(8*i)))
			a.Emit(target.SBFX(dst, dst, 0, types.VBits))
		default:
			a.Emit(target.LDR(asm.NewPReg(dst.ID(), asm.RegTypeInt, asm.Width32), target.X25, int16(8*i)))
		}
	}
	a.Emit(
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		target.BLLabel(m.entry),
		target.STR(target.X24, Ctx, int16(jit.OffsetBudget)),
	)
	for i, k := range l.Registers {
		src := register(i)
		switch k.Repr() {
		case types.KindRef, types.KindI64:
			a.Emit(target.STR(src, target.X25, int16(8*i)))
		case types.KindF64:
			a.Emit(target.MOV(target.X16, src))
			canonical(a)
			a.Emit(target.STR(target.X16, target.X25, int16(8*i)))
		default:
			a.Emit(target.UXTW(target.X16, src))
			a.Emit(target.LDI(target.X17, types.Tag(k))...)
			a.Emit(target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, target.X25, int16(8*i)))
		}
	}
	a.Emit(
		target.LDR(target.LR, target.SP, 8),
		target.ADDI(target.SP, target.SP, 16),
		target.RET(),
	)
	return label
}

// Lower emits op and reports false when it has no ARM64 lowering.
func (m *Machine) Lower(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	switch op.Op {
	case ssa.OpConst:
		if len(op.Results) != 1 {
			return false
		}
		m.Const(a, s.Reg(op.Results[0]), op.Const)
		return true
	case ssa.OpLoad:
		return m.load(a, op, s)
	case ssa.OpStore:
		return m.store(a, op, s)
	case ssa.OpGuardKind:
		return m.guard(a, op, s)
	case ssa.OpGuardShape:
		return m.shape(a, op, s)
	case ssa.OpGuardValue:
		return m.value(a, op, s)
	case ssa.OpArrayView:
		return m.slice(a, op, s)
	case ssa.OpArrayBound:
		return m.bound(op, s)
	case ssa.OpGuardBounds:
		return limit(a, op, s)
	case ssa.OpRefRetain:
		m.retain(a, s.Reg(op.Args[0]))
		return true
	case ssa.OpRefRelease:
		m.release(a, s.Reg(op.Args[0]), s)
		return true
	default:
		return op.Op.Data() && m.data(a, op, s)
	}
}

// Branch transfers control to labels: TermBrIf takes labels[0] on nonzero,
// TermBrTable takes labels[i] for index i and the last label out of range. An
// edge to next falls through; an TermBrIf whose nonzero edge is next
// branches on the inverted condition instead. A fused compare's condition
// is read from the flags.
func (m *Machine) Branch(a *asm.Assembler, t ssa.Terminator, s compile.Site, labels []asm.Label, next asm.Label) {
	switch t.Term {
	case ssa.TermJump:
	case ssa.TermBrIf:
		yes, no, invert := labels[0], labels[1], labels[0] == next
		if invert {
			yes, no = no, yes
		}
		switch {
		case t.Args[0] == m.flag && invert:
			a.Emit(target.BCondLabel(jumps[m.cond^1], yes))
		case t.Args[0] == m.flag:
			a.Emit(target.BCondLabel(jumps[m.cond], yes))
		case invert:
			a.Emit(target.CBZLabel(s.Reg(t.Args[0]), yes))
		default:
			a.Emit(target.CBNZLabel(s.Reg(t.Args[0]), yes))
		}
		labels = []asm.Label{no}
	case ssa.TermBrTable:
		index := s.Reg(t.Args[0])
		scratch := target.X16
		if index.Width() == asm.Width32 {
			scratch = target.W16
		}
		for i, label := range labels[:len(labels)-1] {
			if i <= imm12 {
				a.Emit(target.CMPI(index, uint16(i)), target.BCondLabel(target.OpBEQ, label))
			} else {
				a.Emit(target.LDI(scratch, uint64(i))...)
				a.Emit(target.CMP(index, scratch), target.BCondLabel(target.OpBEQ, label))
			}
		}
	}
	if last := labels[len(labels)-1]; last != next {
		a.Emit(target.BLabel(last))
	}
}

// Return boxes results into the interpreter's return slots. TermReturn first
// releases reference-capable frame slots, matching threaded RETURN.
func (m *Machine) Return(a *asm.Assembler, t ssa.Terminator, s compile.Site) {
	base := len(m.kinds)
	if t.Term == ssa.TermReturn {
		base = 0
		if slices.Contains(m.borrows, true) {
			skip := a.Label()
			a.Emit(
				target.ADDI(target.X16, Ctx, uint16(jit.OffsetRecords+jit.SizeofRecord)),
				target.CMP(target.X27, target.X16),
				target.BCondLabel(target.OpBNE, skip),
			)
			for i, borrowed := range m.borrows {
				if !borrowed {
					continue
				}
				word := m.vreg()
				a.Emit(target.LDR(word, target.X25, int16(i*8)))
				m.release(a, word, s)
			}
			a.Bind(skip)
		}
		for i, k := range m.kinds {
			switch k.Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				continue
			}
			if i < len(m.borrows) && m.borrows[i] {
				a.Emit(target.STR(target.XZR, target.X25, int16(i*8)))
				continue
			}
			word := m.vreg()
			a.Emit(target.LDR(word, target.X25, int16(i*8)))
			m.release(a, word, s)
			a.Emit(target.STR(target.XZR, target.X25, int16(i*8)))
		}
	}
	if t.Term == ssa.TermReturn && len(m.registers) > 0 {

		for i, v := range t.Args {
			convention(a, s.Reg(v), i, false)
		}
		a.Emit(target.USE(target.X0))
		if len(t.Args) > 1 {
			a.Emit(target.USE(target.X1))
		}
	} else {
		for i, v := range t.Args {
			a.Emit(target.STR(box(a, s, v), target.X25, int16((base+i)*8)))
		}
	}
	a.Emit(target.SUBI(target.X24, target.X24, 1), target.BLabel(m.end))
}

// Budget counts X24, the pinned budget, down and branches to safepoint when
// it is spent.
func (m *Machine) Budget(a *asm.Assembler, safepoint asm.Label) {
	a.Emit(
		target.SUBSI(target.X24, target.X24, 1),
		target.BCondLabel(target.OpBLE, safepoint),
	)
}

// Exit stores X27 to Context.Next and X24 to Context.Budget (their only
// writer, so both are exact at every trap), writes the exit id and trap,
// then calls the preserving stub through EXIT; a resumed exit reloads X24,
// which Go may have refilled. EXIT has BLR encoding with FlowNext, so use
// intervals stay live across the stub; a non-resuming exit does not resume
// and other exits resume in native code.
func (m *Machine) Exit(a *asm.Assembler, id int, k jit.Kind, uses []asm.VReg) {
	trap := jit.TrapBridge
	if k == jit.ExitDeopt {
		trap = jit.TrapDeopt
	}
	a.Emit(
		target.STR(target.X27, Ctx, int16(jit.OffsetNext)),
		target.STR(target.X24, Ctx, int16(jit.OffsetBudget)),
	)
	a.Emit(target.LDI(target.X16, uint64(id))...)
	a.Emit(target.STR(target.X16, Ctx, int16(jit.OffsetExit)))
	a.Emit(target.LDI(target.X16, uint64(trap))...)
	a.Emit(
		target.STR(target.X16, Ctx, int16(jit.OffsetTrap)),
		target.LDR(target.X16, Ctx, int16(jit.OffsetStub)),
		target.EXIT(target.X16),
	)
	for _, u := range uses {
		a.Emit(target.USE(u))
	}
	if !k.Resumes() {
		a.Emit(target.BRK(0))
		return
	}
	a.Emit(target.LDR(target.X24, Ctx, int16(jit.OffsetBudget)))
	if k == jit.ExitBox {
		a.Emit(target.LDR(target.X16, Ctx, int16(jit.OffsetResults)))
	}
}

// Spill saves a value in a fixed spill slot.
func (m *Machine) Spill(a *asm.Assembler, reg asm.VReg, slot int) {
	a.Emit(target.New().Spill(reg, slot))
}

// Results loads each exit result from Context.Results.
func (m *Machine) Results(a *asm.Assembler, regs []asm.VReg) {
	for i, r := range regs {
		a.Emit(target.LDR(r, Ctx, int16(int(jit.OffsetResults)+8*i)))
	}
}

// Call writes boxed arguments at the callee frame base and, when Generic,
// branches to Stub, which serves the call and resumes at Join. Otherwise it
// passes a closure callee's upvals base through Context.Upvals when Upvals,
// and dispatches through Context.Natives, or, when Self, branches directly to
// the unit's own entry. Missing code, depth, or space takes ExitCall, which
// resumes at Join; an owned Target is released once a native callee returns,
// a borrowed one left alone.
func (m *Machine) Call(a *asm.Assembler, c compile.Call, s compile.Site) bool {
	if 8*(c.Base+c.Size) > imm12 {
		return false
	}
	for i, v := range c.Args {
		a.Emit(target.STR(box(a, s, v), target.X25, int16((c.Base+i)*8)))
	}
	if c.Generic {
		a.Emit(target.BLabel(c.Stub))
		m.join(a, c, s)
		return true
	}
	var code asm.VReg
	if !c.Self {
		code = m.vreg()
		a.Emit(target.LDR(code, Ctx, int16(jit.OffsetNatives)))
		a.Emit(target.LDI(target.X16, uint64(c.Callee))...)
		a.Emit(target.LDRR(code, code, target.X16), target.CBZLabel(code, c.Stub))
	}
	a.Emit(
		target.ADDI(target.X16, target.X25, uint16(8*(c.Base+c.Size))),
		target.LDR(target.X17, Ctx, int16(jit.OffsetTop)),
		target.CMP(target.X16, target.X17),
		target.BCondLabel(target.OpBHI, c.Stub),
		target.LDR(target.X17, Ctx, int16(jit.OffsetEdge)),
		target.CMP(target.X27, target.X17),
		target.BCondLabel(target.OpBCS, c.Stub),
	)
	a.Emit(target.SUBSI(target.X24, target.X24, 1), target.BCondLabel(target.OpBLE, c.Safepoint))
	a.Bind(c.Resume)
	if c.Upvals {
		upvals := m.container(a, s.Reg(c.Target))
		a.Emit(
			target.LDR(upvals, upvals, int16(jit.OffsetClosureUpvals)),
			target.STR(upvals, Ctx, int16(jit.OffsetUpvals)),
		)
	}
	a.Emit(target.ADDI(target.X25, target.X25, uint16(8*c.Base)))

	for i := range c.Arguments {
		convention(a, s.Reg(c.Args[i]), i, false)
	}
	if len(c.Arguments) > 0 {
		a.Emit(target.USE(target.X0))
		if len(c.Arguments) > 1 {
			a.Emit(target.USE(target.X1))
		}
	}
	if c.Self {
		a.Emit(target.BLLabel(m.entry))
	} else {
		a.Emit(target.BLR(code))
	}
	a.Bind(c.Return)

	for i := range c.Registers {
		a.Emit(target.DEF(register(i)))
	}
	for _, u := range c.Live {
		a.Emit(target.USE(u))
	}
	a.Emit(target.SUBI(target.X25, target.X25, uint16(8*c.Base)))
	if c.Owned {
		m.release(a, s.Reg(c.Target), s)
	}
	if len(c.Registers) == 0 {
		m.join(a, c, s)
		return true
	}
	for j, v := range c.Results {
		convention(a, s.Reg(v), j, true)
	}
	a.Bind(c.Join)
	return true
}

// Move copies src into dst of the same bank.
func (m *Machine) Move(a *asm.Assembler, dst, src asm.VReg) {
	if dst.Type() == asm.RegTypeFloat {
		a.Emit(target.FMOV(dst, src))
		return
	}
	a.Emit(target.MOV(dst, src))
}

// Const loads word into dst, by dst's register bank.
func (m *Machine) Const(a *asm.Assembler, dst asm.VReg, word uint64) {
	if dst.Type() != asm.RegTypeFloat {
		a.Emit(target.LDI(dst, word)...)
		return
	}
	if dst.Width() == asm.Width32 {
		a.Emit(target.LDI(target.X16, uint64(uint32(word)))...)
		a.Emit(target.FMOV(dst, target.W16))
		return
	}
	a.Emit(target.LDI(target.X16, word)...)
	a.Emit(target.FMOV(dst, target.X16))
}

// load unboxes a slot: a narrow or f32 payload is the slot's low 32 bits,
// f64 and ref are the whole word. An i64 slot holds the word unchecked; its
// kind guard unboxes it.
func (m *Machine) load(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Results) != 1 {
		return false
	}
	base, ok := m.base(a, op.Slot)
	if !ok {
		return false
	}
	a.Emit(target.LDR(s.Reg(op.Results[0]), base, int16(op.Slot.Index*8)))
	return true
}

// store overwrites a slot, releasing its old reference when its declared
// representation is boxed.
func (m *Machine) store(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 1 {
		return false
	}
	base, ok := m.base(a, op.Slot)
	if !ok {
		return false
	}

	word := box(a, s, op.Args[0])

	if k := s.Slot(op.Slot); k == ssa.TypeRef || k == ssa.TypeI64 {
		if _, ok := word.(asm.VReg); !ok {

			boxed := m.vreg()
			a.Emit(target.MOV(boxed, word))
			word = boxed
		}
		old := m.vreg()
		a.Emit(target.LDR(old, base, int16(op.Slot.Index*8)))
		m.release(a, old, s)
	}
	a.Emit(target.STR(word, base, int16(op.Slot.Index*8)))
	return true
}

// base is the register a slot is addressed from: X25 for a local of this
// activation, the globals base for a global, the upvals base for an upval.
func (m *Machine) base(a *asm.Assembler, slot ssa.Slot) (asm.Reg, bool) {
	if slot.Index < 0 || slot.Index > imm12 {
		return nil, false
	}
	switch {
	case slot.Space == ssa.SpaceLocal && slot.Base == 0:
		return target.X25, true
	case slot.Space == ssa.SpaceGlobal:
		base := m.vreg()
		a.Emit(target.LDR(base, Ctx, int16(jit.OffsetGlobals)))
		return base, true
	case slot.Space == ssa.SpaceUpval && m.upvals != asm.VReg{}:
		return m.upvals, true
	default:
		return nil, false
	}
}

// data lowers a data operation. Width comes from the types of its values: a
// 32-bit lane or f32 is 32 bits wide, an i64 or f64 64.
func (m *Machine) data(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	switch op.Op {
	case ssa.OpIntAdd:
		return arith(a, op, s, result(s, op), true)
	case ssa.OpIntSub:
		return arith(a, op, s, result(s, op), false)
	case ssa.OpIntMul:
		return binary(a, op, s, target.MUL)
	case ssa.OpIntSAddOvf, ssa.OpIntSSubOvf, ssa.OpIntSMulOvf, ssa.OpIntUAddOvf, ssa.OpIntUSubOvf, ssa.OpIntUMulOvf:
		return checked(a, op, s)
	case ssa.OpIntAnd:
		return binary(a, op, s, target.AND)
	case ssa.OpIntOr:
		return binary(a, op, s, target.ORR)
	case ssa.OpIntXor:
		return binary(a, op, s, target.EOR)
	case ssa.OpIntShl:
		return binary(a, op, s, target.LSL)
	case ssa.OpIntAShr:
		return binary(a, op, s, target.ASR)
	case ssa.OpIntLShr:
		return binary(a, op, s, target.LSR)
	case ssa.OpIntRotr:
		return binary(a, op, s, target.ROR)
	case ssa.OpIntRotl:
		return m.rotl(a, op, s, result(s, op))
	case ssa.OpIntSDiv, ssa.OpIntUDiv, ssa.OpIntSRem, ssa.OpIntURem:
		return divide(a, op, s, result(s, op))
	case ssa.OpIntCmp:
		cond, ok := integer[op.Cond]
		return ok && m.compare(a, op, s, cond, operand(s, op))
	case ssa.OpFloatCmp:
		cond, ok := float[op.Cond]
		return ok && m.compare(a, op, s, cond, operand(s, op))
	case ssa.OpRefCmp:
		cond, ok := integer[op.Cond]
		return ok && m.compare(a, op, s, cond, asm.Width64)
	case ssa.OpStringCmp:
		cond, ok := integer[op.Cond]
		return ok && m.stringCompare(a, op, s, cond)
	case ssa.OpIntSExt8:
		return unary(a, op, s, target.SXTB)
	case ssa.OpIntSExt16:
		return unary(a, op, s, target.SXTH)
	case ssa.OpIntSExt32:
		return unary(a, op, s, target.SXTW)
	case ssa.OpIntClz:
		return unary(a, op, s, target.CLZ)
	case ssa.OpIntCtz:
		return m.ctz(a, op, s, result(s, op))
	case ssa.OpIntPopcnt:
		return m.popcnt(a, op, s, result(s, op))
	case ssa.OpIntSExt:
		return convert(a, op, s, target.SXTW)
	case ssa.OpIntZExt:
		return convert(a, op, s, target.UXTW)
	case ssa.OpIntSIToFP:
		return convert(a, op, s, target.SCVTF)
	case ssa.OpIntUIToFP:
		return convert(a, op, s, target.UCVTF)
	case ssa.OpFloatFPExt, ssa.OpFloatFPTrunc:
		return convert(a, op, s, target.FCVT)
	case ssa.OpIntWrap:
		return narrow(a, op, s)
	case ssa.OpIntBitcast, ssa.OpFloatBitcast:
		return reinterpret(a, op, s)
	case ssa.OpFloatAdd:
		return binary(a, op, s, target.FADD)
	case ssa.OpFloatSub:
		return binary(a, op, s, target.FSUB)
	case ssa.OpFloatMul:
		return binary(a, op, s, target.FMUL)
	case ssa.OpFloatDiv:
		return binary(a, op, s, target.FDIV)
	case ssa.OpFloatMin:
		return binary(a, op, s, target.FMIN)
	case ssa.OpFloatMax:
		return binary(a, op, s, target.FMAX)
	case ssa.OpFloatAbs:
		return unary(a, op, s, target.FABS)
	case ssa.OpFloatNeg:
		return unary(a, op, s, target.FNEG)
	case ssa.OpFloatSqrt:
		return unary(a, op, s, target.FSQRT)
	case ssa.OpFloatCeil:
		return unary(a, op, s, target.FRINTP)
	case ssa.OpFloatFloor:
		return unary(a, op, s, target.FRINTM)
	case ssa.OpFloatTrunc:
		return unary(a, op, s, target.FRINTZ)
	case ssa.OpFloatNearest:
		return unary(a, op, s, target.FRINTN)
	case ssa.OpFloatCopysign:
		return m.copysign(a, op, s, result(s, op))
	case ssa.OpFloatFPToSISat:
		return truncate(a, op, s, target.FCVTZS, operand(s, op), result(s, op))
	case ssa.OpFloatFPToUISat:
		return truncate(a, op, s, target.FCVTZU, operand(s, op), result(s, op))
	case ssa.OpSelect:
		return choose(a, op, s)
	case ssa.OpRefIsNull:
		return refIsNull(a, op, s)
	case ssa.OpRefTest, ssa.OpRefCast:
		return m.refType(a, op, s)
	case ssa.OpArrayGet:
		return m.arrayGet(a, op, s)
	case ssa.OpArraySet:
		return m.arraySet(a, op, s)
	case ssa.OpArrayLen:
		return m.arrayLen(a, op, s)
	case ssa.OpStringLen:
		return m.stringLen(a, op, s)
	case ssa.OpStructNewDefault:
		return m.reserve(a, op, s, 0)
	case ssa.OpArrayNewDefault:
		return m.reserve(a, op, s, 1)
	case ssa.OpStructGet:
		return m.structGet(a, op, s)
	case ssa.OpStructSet:
		return m.structSet(a, op, s)
	case ssa.OpMapLen:
		return m.mapLen(a, op, s)
	case ssa.OpMapGet, ssa.OpMapLookup:
		return m.mapGet(a, op, s)
	case ssa.OpMapSet:
		return m.mapSet(a, op, s)
	case ssa.OpMapDelete:
		return m.mapDelete(a, op, s)
	default:
		return false
	}
}

// result is the width of op's first result, operand that of its first argument.
func result(s compile.Site, op ssa.Operation) asm.RegWidth {
	if len(op.Results) == 0 {
		return asm.Width32
	}
	return width(s.Type(op.Results[0]))
}

func operand(s compile.Site, op ssa.Operation) asm.RegWidth {
	if len(op.Args) == 0 {
		return asm.Width32
	}
	return width(s.Type(op.Args[0]))
}

// width returns the register width a value of type t fills.
func width(t ssa.Type) asm.RegWidth {
	if t == ssa.TypeI64 || t == ssa.TypeF64 || t == ssa.TypeRef {
		return asm.Width64
	}
	return asm.Width32
}

// ctz counts trailing zeros as RBIT then CLZ: bit-reversal turns the
// trailing run into a leading one, matching bits.TrailingZeros for every
// input including zero (RBIT(0)=0, CLZ(0)=width).
func (m *Machine) ctz(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if !has(op, 1, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, width, x, dst) {
		return false
	}
	t := m.ivreg(width)
	a.Emit(target.RBIT(t, x))
	a.Emit(target.CLZ(dst, t))
	return true
}

// popcnt counts set bits via the SIMD byte-wise CNT, summed by ADDV and
// moved back through the general registers: ARM64 has no scalar popcount.
func (m *Machine) popcnt(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if !has(op, 1, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, width, x, dst) {
		return false
	}
	v, c, sum := m.fvreg(width), m.fvreg(width), m.fvreg(width)
	a.Emit(target.FMOV(v, x))
	a.Emit(target.CNT(c, v))
	a.Emit(target.ADDV(sum, c))
	a.Emit(target.FMOV(dst, sum))
	return true
}

// rotl rotates left by negating the count and reusing RORV: ARM64 has no
// left-rotate register form, and RORV's right rotate by -k is a left
// rotate by k mod width, matching bits.RotateLeft's own negation rule.
func (m *Machine) rotl(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if !has(op, 2, 1) {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, width, x, y, dst) {
		return false
	}
	t := m.ivreg(width)
	a.Emit(target.NEG(t, y))
	a.Emit(target.ROR(dst, x, t))
	return true
}

// copysign combines x's magnitude with y's sign through the integer
// registers, the same bit trick math.Copysign uses, so it is exact for
// every input including NaN payloads.
func (m *Machine) copysign(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if !has(op, 2, 1) {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if !match(asm.RegTypeFloat, width, x, y, dst) {
		return false
	}
	signBit, magMask := uint64(1)<<63, uint64(1)<<63-1
	if width == asm.Width32 {
		signBit, magMask = uint64(1)<<31, uint64(1)<<31-1
	}
	mag, sign := m.ivreg(width), m.ivreg(width)
	a.Emit(target.FMOV(mag, x))
	a.Emit(target.FMOV(sign, y))
	a.Emit(target.ANDI(mag, mag, magMask))
	a.Emit(target.ANDI(sign, sign, signBit))
	a.Emit(target.ORR(mag, mag, sign))
	a.Emit(target.FMOV(dst, mag))
	return true
}

// compare lowers integer and float comparisons.
func (m *Machine) compare(a *asm.Assembler, op ssa.Operation, s compile.Site, cond uint8, width asm.RegWidth) bool {
	if !has(op, 2, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if !match(x.Type(), width, x) || !match(asm.RegTypeInt, asm.Width32, dst) {
		return false
	}
	switch n, ok := immediate(s, op.Args[1], width); {
	case x.Type() == asm.RegTypeFloat:
		y := s.Reg(op.Args[1])
		if !match(asm.RegTypeFloat, width, y) {
			return false
		}
		a.Emit(target.FCMP(x, y))
	case ok && n >= 0:
		a.Emit(target.CMPI(x, uint16(n)))
	case ok && signed(cond):
		// CMN x, c leaves the flags of x - (-c) but its carry, which only an
		// unsigned condition reads.
		a.Emit(target.CMNI(x, uint16(-n)))
	default:
		y := s.Reg(op.Args[1])
		if !match(asm.RegTypeInt, width, y) {
			return false
		}
		a.Emit(target.CMP(x, y))
	}
	m.set(a, s, op.Results[0], dst, cond)
	return true
}

// guard unboxes an i64 operand as threaded borrowI64 does: a word tagged
// Ref reads the heap-promoted I64 through its itab (any other object
// deopts) and data word, a borrow; every other word sign-extends its 49-bit
// payload. I64 and Ref tags differ only in bit 49, so the inline path tests
// that bit and branches past the ref path.
func (m *Machine) guard(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	word, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	inline, done := a.Label(), a.Label()
	a.Emit(target.LSRI(target.X16, word, types.VBits), target.TSTI(target.X16, 1), target.BCondLabel(target.OpBEQ, inline))
	expect(a, types.Tag(types.KindRef)>>types.VBits, inline)
	heap := m.heap(a, word)
	a.Emit(target.LDR(target.X16, heap, 0))
	expect(a, uint64(jit.Itab(types.I64(0))), s.Deopt())
	a.Emit(target.LDR(dst, heap, int16(jit.OffsetData)), target.LDR(dst, dst, 0), target.BLabel(done))

	a.Bind(inline)
	a.Emit(target.SBFX(dst, word, 0, types.VBits))
	a.Bind(done)
	return true
}

// value lowers guard.value: Args[0] must equal the admitted word Args[1],
// or the guard deopts. Result is Args[0]'s own word.
func (m *Machine) value(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 1) {
		return false
	}
	got, want := s.Reg(op.Args[0]), s.Reg(op.Args[1])
	a.Emit(target.CMP(got, want), target.BCondLabel(target.OpBNE, s.Deopt()))
	m.Move(a, s.Reg(op.Results[0]), got)
	return true
}

// join binds c.Join and loads c's results from the callee frame's slots.
func (m *Machine) join(a *asm.Assembler, c compile.Call, s compile.Site) {
	a.Bind(c.Join)
	for j, v := range c.Results {
		a.Emit(target.LDR(s.Reg(v), target.X25, int16((c.Base+j)*8)))
	}
}

// fvreg returns a fresh float scratch register at width.
func (m *Machine) fvreg(width asm.RegWidth) asm.VReg {
	m.temp--
	return asm.NewVReg(m.temp, asm.RegTypeFloat, width)
}

// shape lowers guard.shape. It admits only the representation and optional
// concrete type encoded by Shape; null, host, or mismatched containers deopt.
// A closure guard first deopts a word that is no reference at all, since a
// dynamic callee may hold any value.
func (m *Machine) shape(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	expected := itab(op.Shape)
	if expected == 0 {
		return false
	}
	ref, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if op.Shape.Form == ssa.FormClosure {
		expectRef(a, ref, s.Deopt())
	}
	addr := m.heap(a, ref)
	a.Emit(target.LDR(target.X16, addr, 0))
	expect(a, uint64(expected), s.Deopt())
	if op.Shape.Form == ssa.FormStruct && op.Shape.Type != 0 {
		a.Emit(target.LDR(target.X16, addr, int16(jit.OffsetData)))
		a.Emit(target.LDR(target.X16, target.X16, int16(jit.OffsetStructTyp)))
		expect(a, uint64(op.Shape.Type), s.Deopt())
	}
	if op.Shape.Form == ssa.FormMap {
		a.Emit(target.LDR(target.X16, addr, int16(jit.OffsetData)))
		a.Emit(target.LDR(target.X16, target.X16, int16(jit.OffsetMapTyp)))
		a.Emit(target.LDR(target.W16, target.X16, int16(jit.OffsetMapTypeKinds)))
		a.Emit(target.ANDI(target.X16, target.X16, 0xFFFFFF))
		expect(a, uint64(op.Shape.Key)|uint64(op.Shape.Elem)<<8|uint64(op.Shape.Class)<<16, s.Deopt())
	}
	if op.Shape.Form == ssa.FormClosure {
		m.closure(a, addr, op.Shape, s)
	}
	m.Move(a, dst, ref)
	m.guards[op.Results[0]] = op.Shape
	return true
}

// closure deopts unless the *types.Closure at heap slot addr calls
// shape.Function, has type shape.Type, and holds at least shape.Captures
// upvals.
func (m *Machine) closure(a *asm.Assembler, addr asm.VReg, shape ssa.Shape, s compile.Site) {
	data := m.vreg()
	a.Emit(target.LDR(data, addr, int16(jit.OffsetData)))
	a.Emit(target.LDR(target.W16, data, int16(jit.OffsetClosureFn)))
	expect(a, uint64(shape.Function), s.Deopt())
	a.Emit(target.LDR(target.X16, data, int16(jit.OffsetClosureTyp)))
	expect(a, uint64(shape.Type), s.Deopt())
	if shape.Captures == 0 {
		return
	}
	a.Emit(target.LDR(target.X16, data, int16(jit.OffsetClosureUpvals+jit.OffsetSliceLen)))
	if shape.Captures <= imm12 {
		a.Emit(target.CMPI(target.X16, uint16(shape.Captures)))
	} else {
		a.Emit(target.LDI(target.X17, uint64(shape.Captures))...)
		a.Emit(target.CMP(target.X16, target.X17))
	}
	a.Emit(target.BCondLabel(target.OpBCC, s.Deopt()))
}

// slice loads a guarded array's element pointer and length for the array
// ops naming op's result, which is the same reference.
func (m *Machine) slice(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok || shape.Form != ssa.FormArray {
		return false
	}
	ptr, ln := m.elements(a, s, op.Args[0], shape)
	m.Move(a, s.Reg(op.Results[0]), s.Reg(op.Args[0]))
	m.guards[op.Results[0]] = shape
	m.slices[op.Results[0]] = [2]asm.VReg{ptr, ln}
	return true
}

// bound records op's index as proven within the slice op names, for the
// array ops indexed by op's result: it emits no rows, and nothing else reads
// the result's register.
func (m *Machine) bound(op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 1) {
		return false
	}
	if _, ok := m.slices[op.Args[1]]; !ok {
		return false
	}
	m.bounds[op.Results[0]] = bound{slice: op.Args[1], index: s.Reg(op.Args[0])}
	return true
}

// arrayLen loads the element count of a guarded array.
func (m *Machine) arrayLen(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok {
		return false
	}
	_, ln := m.elements(a, s, op.Args[0], shape)
	a.Emit(target.MOVW(s.Reg(op.Results[0]), ln))
	return true
}

// stringLen loads a string's length, as threaded STRING_LEN does: a slot
// holding anything else, null included, is the operation's own type
// mismatch, a trap. The W load of the length word is int32(len).
func (m *Machine) stringLen(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	addr := m.heap(a, s.Reg(op.Args[0]))
	a.Emit(target.LDR(target.X16, addr, 0))
	expect(a, uint64(jit.Itab(types.String(""))), s.Trap())
	data := m.vreg()
	a.Emit(target.LDR(data, addr, int16(jit.OffsetData)))
	a.Emit(target.LDR(s.Reg(op.Results[0]), data, int16(jit.OffsetStringLen)))
	return true
}

// stringCompare orders two strings' bytes, as threaded STRING_EQ through
// STRING_GE do: a slot holding anything else, null included, is the
// operation's own type mismatch, a trap. Interning is gone, so equal contents
// may sit behind different refs: a shared data pointer skips the bytes, and
// otherwise a word loop compares 8 bytes while that many of the shorter
// length remain and a byte loop the tail. The first differing word, byte-
// reversed to big-endian, or byte leaves the flags of left against right; no
// difference leaves those of the lengths, and cond reads them unsigned. Equality
// relations leave early on unequal lengths. It only loads, so no Go pointer
// is written.
func (m *Machine) stringCompare(a *asm.Assembler, op ssa.Operation, s compile.Site, cond uint8) bool {
	if !has(op, 2, 1) {
		return false
	}
	var data, size, ptr [2]asm.VReg
	for i := range data {
		addr := m.heap(a, s.Reg(op.Args[i]))
		a.Emit(target.LDR(target.X16, addr, 0))
		expect(a, uint64(jit.Itab(types.String(""))), s.Trap())
		data[i], size[i], ptr[i] = m.vreg(), m.vreg(), m.vreg()
		a.Emit(
			target.LDR(data[i], addr, int16(jit.OffsetData)),
			target.LDR(size[i], data[i], int16(jit.OffsetStringLen)),
			target.LDR(ptr[i], data[i], 0),
		)
	}
	out, tail, step, words, bytes := a.Label(), a.Label(), a.Label(), a.Label(), a.Label()
	rest := m.vreg()
	a.Emit(target.CMP(size[0], size[1]))
	if cond == target.CondEQ || cond == target.CondNE {
		a.Emit(target.BCondLabel(target.OpBNE, out))
	}
	a.Emit(
		target.CSEL(rest, size[0], size[1], target.CondCC),
		target.CMP(ptr[0], ptr[1]), target.BCondLabel(target.OpBEQ, tail),
	)
	a.Bind(words)
	a.Emit(
		target.CMPI(rest, 8), target.BCondLabel(target.OpBCC, bytes),
		target.LDR(target.X16, ptr[0], 0), target.LDR(target.X17, ptr[1], 0),
		target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBEQ, step),
		target.REV(target.X16, target.X16), target.REV(target.X17, target.X17),
		target.CMP(target.X16, target.X17), target.BLabel(out),
	)
	a.Bind(step)
	a.Emit(
		target.ADDI(ptr[0], ptr[0], 8), target.ADDI(ptr[1], ptr[1], 8),
		target.SUBI(rest, rest, 8), target.BLabel(words),
	)
	a.Bind(bytes)
	a.Emit(
		target.CBZLabel(rest, tail),
		target.LDRB(target.X16, ptr[0], 0), target.LDRB(target.X17, ptr[1], 0),
		target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, out),
		target.ADDI(ptr[0], ptr[0], 1), target.ADDI(ptr[1], ptr[1], 1),
		target.SUBI(rest, rest, 1), target.BLabel(bytes),
	)
	a.Bind(tail)
	a.Emit(target.CMP(size[0], size[1]))
	a.Bind(out)
	m.set(a, s, op.Results[0], s.Reg(op.Results[0]), cond)
	return true
}

// set materializes cond into dst, v's register, unless v is fused into the
// branch right after it: then the flags carry cond to Branch.
func (m *Machine) set(a *asm.Assembler, s compile.Site, v ssa.Value, dst asm.VReg, cond uint8) {
	if s.Fuse(v) {
		m.flag, m.cond = v, cond
		return
	}
	a.Emit(target.CSET(dst, cond))
}

// arrayGet loads the element at a bounds-checked index off a guarded array.
// A ref element is retained, matching interp.(*Interpreter).arrayGet.
func (m *Machine) arrayGet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 1) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok {
		return false
	}
	ptr, ln := m.elements(a, s, op.Args[0], shape)
	dst := s.Reg(op.Results[0])
	switch stride(shape.Elem) {
	case 1:
		at := m.element(a, s, op, ptr, ln, 0)
		if shape.Elem == types.KindI8 {
			a.Emit(at.row(target.LDRSB, target.LDRSBR, dst))
		} else {
			a.Emit(at.row(target.LDRB, target.LDRBR, dst))
		}
	case 4:
		a.Emit(m.element(a, s, op, ptr, ln, 2).row(target.LDR, target.LDRR, dst))
	default:
		a.Emit(m.element(a, s, op, ptr, ln, 3).row(target.LDR, target.LDRR, dst))
		if shape.Elem == types.KindRef {
			m.retain(a, dst)
		}
	}
	return true
}

// arraySet writes val at a bounds-checked index into a guarded array. A ref
// element releases the old one and adopts val, matching
// interp.(*Interpreter).arraySet.
func (m *Machine) arraySet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 3, 0) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok {
		return false
	}
	ptr, ln := m.elements(a, s, op.Args[0], shape)
	val := s.Reg(op.Args[2])
	switch stride(shape.Elem) {
	case 1:
		at := m.element(a, s, op, ptr, ln, 0)
		if shape.Elem == types.KindI1 && s.Type(op.Args[2]).Kind() != types.KindI1 {

			bit := m.vreg()
			a.Emit(target.CMPI(val, 0), target.CSET(bit, target.CondNE))
			val = bit
		}
		a.Emit(at.row(target.STRB, target.STRBR, val))
	case 4:

		at := m.element(a, s, op, ptr, ln, 2)
		if shape.Elem == types.KindI32 {
			a.Emit(at.row(target.STRW, target.STRWR, val))
		} else {
			a.Emit(at.row(target.STR, target.STRR, val))
		}
	default:
		at := m.element(a, s, op, ptr, ln, 3)
		if shape.Elem == types.KindRef {

			m.replace(a, s, at, box(a, s, op.Args[2]))
		} else {
			a.Emit(at.row(target.STR, target.STRR, val))
		}
	}
	return true
}

// reserve takes the default object of a type from the type's bin, the last
// of its N boxed refs, and lowers N: a ref native code takes is its alone, with
// count 1, so neither a count nor a Go pointer is written. A type has jit.Classes
// bins, and a struct uses the first. An empty bin takes
// the slow path: the interpreter builds the object and fills the bin. A length
// operand (args is 1) picks the bin of its class, the length itself below
// the last class; a bin holds arrays of one length, so a length that is not
// that length, a negative one included, bridges too. The type index is the
// bytecode operand, and the interpreter sizes the table to the program's
// types, which verification bounds it by.
func (m *Machine) reserve(a *asm.Assembler, op ssa.Operation, s compile.Site, args int) bool {
	index := op.Shape.Index
	if !has(op, args, 1) {
		return false
	}
	empty, resume := s.Bridge()

	entry, n, refs := m.vreg(), m.vreg(), m.vreg()
	a.Emit(target.LDR(entry, Ctx, int16(jit.OffsetBins)))
	at := index * jit.Classes * int(jit.SizeofBin)
	if at+int(jit.OffsetBinLen) > 8*imm12 {
		a.Emit(target.LDI(target.X16, uint64(at))...)
		a.Emit(target.ADD(entry, entry, target.X16))
		at = 0
	}
	var length, held asm.VReg
	if args == 1 {
		// The class is the length below the last class and the last class
		// from there up; a negative length compares as huge, so it lands in
		// the last class, whose Len no negative length equals.
		length, held = m.vreg(), m.vreg()
		class := m.vreg()
		a.Emit(target.SXTW(length, s.Reg(op.Args[0])))
		a.Emit(target.LDI(target.X17, jit.Classes-1)...)
		a.Emit(
			target.CMPI(length, jit.Classes-1),
			target.CSEL(class, length, target.X17, target.CondCC),
		)
		a.Emit(target.LDI(target.X16, uint64(jit.SizeofBin))...)
		a.Emit(target.MADD(entry, class, target.X16, entry))
	}
	a.Emit(
		target.LDR(n, entry, int16(at+int(jit.OffsetBinN))),
		target.CBZLabel(n, empty),
	)
	if args == 1 {
		a.Emit(
			target.LDR(held, entry, int16(at+int(jit.OffsetBinLen))),
			target.CMP(length, held),
			target.BCondLabel(target.OpBNE, empty),
		)
	}
	a.Emit(
		target.SUBI(n, n, 1),
		target.STR(n, entry, int16(at+int(jit.OffsetBinN))),
		target.LDR(refs, entry, int16(at)),
	)
	taken := m.offset(a, refs, n, 3)
	a.Emit(target.LDR(s.Reg(op.Results[0]), taken, 0))
	a.Bind(resume)
	return true
}

// structGet loads the field at an index off a guarded struct, at an
// immediate offset when the index is a constant whose offset fits the
// load. The shape guard pinned the struct type, so the field kind is
// Results[0]'s own static type and no bounds check applies. A ref field is
// retained, matching threaded STRUCT_GET.
func (m *Machine) structGet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 1) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok || shape.Form != ssa.FormStruct {
		return false
	}
	var load func(dst, base asm.Reg, offset int16) asm.Instruction
	size := 8
	switch s.Type(op.Results[0]).Kind() {
	case types.KindI64, types.KindF64, types.KindRef:
		load = target.LDR
	case types.KindF32, types.KindI32:
		load, size = target.LDR, 4
	case types.KindI8:
		load, size = target.LDRSB, 1
	case types.KindI1:
		load, size = target.LDRB, 1
	default:
		return false
	}

	data := m.container(a, s.Reg(op.Args[0]))
	at := address{base: m.vreg()}
	a.Emit(target.LDR(at.base, data, int16(jit.OffsetStructData)))
	if c, ok := s.Const(op.Args[1]); ok && int32(c) >= 0 && int(int32(c))*8/size <= imm12 {
		at.offset = int16(c * 8)
	} else {
		idx := m.vreg()
		a.Emit(target.SXTW(idx, s.Reg(op.Args[1])))
		at.base = m.offset(a, at.base, idx, 3)
	}

	dst := s.Reg(op.Results[0])
	a.Emit(load(dst, at.base, at.offset))
	if s.Type(op.Results[0]) == ssa.TypeRef {
		m.retain(a, dst)
	}
	return true
}

// structSet writes a statically typed value to a dynamically indexed guarded
// struct. Bounds use the runtime field count; a field whose declared kind
// differs from the value's deopts, since threaded SetField converts by the
// declared kind; ref fields release the old value and adopt the new one.
func (m *Machine) structSet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 3, 0) {
		return false
	}
	shape, ok := m.guards[op.Args[0]]
	if !ok || shape.Form != ssa.FormStruct {
		return false
	}
	kind := s.Type(op.Args[2]).Kind()
	if !kind.IsNumeric() && kind != types.KindRef {
		return false
	}

	data := m.container(a, s.Reg(op.Args[0]))
	typ := m.vreg()
	a.Emit(target.LDR(typ, data, int16(jit.OffsetStructTyp)))
	length := m.vreg()
	a.Emit(target.LDR(length, typ, int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)))
	idx := m.index(a, s, s.Reg(op.Args[1]), length)
	fields, row := m.vreg(), m.vreg()
	a.Emit(target.LDR(fields, typ, int16(jit.OffsetStructTypeFields)))
	a.Emit(target.LDI(target.X16, uint64(jit.SizeofStructField))...)
	a.Emit(target.MUL(row, idx, target.X16), target.ADD(row, fields, row))
	a.Emit(target.LDRB(target.X16, row, int16(jit.OffsetStructFieldKind)))
	a.Emit(target.CMPI(target.X16, uint16(kind)), target.BCondLabel(target.OpBNE, s.Deopt()))

	base := m.vreg()
	a.Emit(target.LDR(base, data, int16(jit.OffsetStructData)))
	off := m.offset(a, base, idx, 3)

	val := field(a, s, op.Args[2])
	if kind == types.KindRef {
		m.replace(a, s, address{base: off}, val)
	} else {
		a.Emit(target.STR(val, off, 0))
	}
	return true
}

// mapLen loads the entry count of a guarded map.
func (m *Machine) mapLen(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	if shape, ok := m.mapOf(op.Args[0]); !ok || !shape.Serves(op.Op, 0, 0) || !match(asm.RegTypeInt, asm.Width32, s.Reg(op.Results[0])) {
		return false
	}
	data := m.container(a, s.Reg(op.Args[0]))
	a.Emit(target.LDR(s.Reg(op.Results[0]), data, int16(jit.OffsetMapCount)))
	return true
}

// mapGet looks up a key in a guarded map, as interp's MAP_GET and MAP_LOOKUP
// do with a types.Map: the element at a full slot holding the key, else the map's
// zero, retained when it is a reference; MAP_LOOKUP also yields whether the key
// was found. It mirrors Map.Load.
func (m *Machine) mapGet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	lookup := op.Op == ssa.OpMapLookup
	results := 1
	if lookup {
		results = 2
	}
	if len(op.Args) != 2 || len(op.Results) != results {
		return false
	}
	shape, ok := m.mapOf(op.Args[0])
	if !ok || !shape.Serves(op.Op, s.Type(op.Args[1]).Kind(), 0) || s.Type(op.Results[0]).Kind() != shape.Elem {
		return false
	}
	if lookup && s.Type(op.Results[1]) != ssa.TypeI1 {
		return false
	}
	dst := s.Reg(op.Results[0])
	exit, resume := s.Bridge()
	hit, miss, done := a.Label(), a.Label(), a.Label()
	data := m.container(a, s.Reg(op.Args[0]))
	key := m.key(a, s, shape, op.Args[1], exit)
	t := m.table(a, data)
	index, tag := m.home(a, t, key)
	slot := m.probe(a, t, key, index, tag, asm.VReg{}, hit, miss)

	a.Bind(hit)
	a.Emit(target.LDR(dst, slot, 8))
	if lookup {
		m.Const(a, s.Reg(op.Results[1]), 1)
	}
	a.Emit(target.BLabel(done))
	a.Bind(miss)
	a.Emit(target.LDR(dst, data, int16(jit.OffsetMapZero)))
	if lookup {
		m.Const(a, s.Reg(op.Results[1]), 0)
	}
	a.Bind(done)
	if shape.Elem == types.KindRef {
		m.retain(a, dst)
	}
	a.Bind(resume)
	return true
}

// retain counts one more reference to ref, as Heap.Retain:
// any reference, the null one included.
func (m *Machine) retain(a *asm.Assembler, ref asm.VReg) {
	skip := a.Label()
	m.count(a, ref, skip, true)
	a.Emit(target.ADDI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0))
	a.Bind(skip)
}

// mapSet stores a value under a key of a guarded map, as Map.Swap does: it
// replaces the value of a full slot holding the key, else fills the first
// tombstone on the probe, or the empty slot that ended it, and appends that
// slot to the insertion order. An insert that would rehash changes nothing and
// bridges, so the Go set owns growth. A map of references (or wide i64s) adopts
// the value and releases the one it replaced.
func (m *Machine) mapSet(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 3, 0) {
		return false
	}
	shape, ok := m.mapOf(op.Args[0])
	if !ok || !shape.Serves(op.Op, s.Type(op.Args[1]).Kind(), s.Type(op.Args[2]).Kind()) {
		return false
	}
	exit, resume := s.Bridge()
	hit, empty := a.Label(), a.Label()
	word := box(a, s, op.Args[2])
	if _, ok := word.(asm.VReg); !ok {
		boxed := m.vreg()
		a.Emit(target.MOV(boxed, word))
		word = boxed
	}
	data := m.container(a, s.Reg(op.Args[0]))
	key := m.key(a, s, shape, op.Args[1], exit)
	t := m.table(a, data)
	index, tag := m.home(a, t, key)
	free := m.vreg()
	a.Emit(target.MOVN(free, 0, 0))
	slot := m.probe(a, t, key, index, tag, free, hit, empty)

	a.Bind(empty)
	used, want, limit, order, pos, count := m.vreg(), m.vreg(), m.vreg(), m.vreg(), m.vreg(), m.vreg()
	a.Emit(
		// (used+1)*4 > (mask+1)*3 is the order's capacity, at which Map.insert rehashes.
		target.LDR(used, data, int16(jit.OffsetMapUsed)), target.ADDI(want, used, 1), target.LSLI(want, want, 2),
		target.ADDI(limit, t.mask, 1), target.LSLI(target.X16, limit, 1), target.ADD(limit, limit, target.X16),
		target.CMP(want, limit), target.BCondLabel(target.OpBGT, exit),
		target.CMPI(free, 0), target.CSEL(index, free, index, target.CondGE),
		target.STRBR(tag, t.ctrl, index),
		target.LSLI(slot, index, 4), target.ADD(slot, t.slots, slot),
		target.STR(key, slot, 0), target.STR(word, slot, 8),
		target.LDR(order, data, int16(jit.OffsetMapOrder)), target.STRWR(index, order, used),
		target.LDR(pos, data, int16(jit.OffsetMapPos)), target.STRWR(used, pos, index),
		target.ADDI(used, used, 1), target.STR(used, data, int16(jit.OffsetMapUsed)),
		target.LDR(count, data, int16(jit.OffsetMapCount)), target.ADDI(count, count, 1), target.STR(count, data, int16(jit.OffsetMapCount)),
		target.BLabel(resume),
	)
	a.Bind(hit)
	if owns(shape.Elem) {
		m.replace(a, s, address{base: slot, offset: 8}, word)
	} else {
		a.Emit(target.STR(word, slot, 8))
	}
	a.Bind(resume)
	return true
}

// mapDelete removes a key from a guarded map, as Map.Delete does: the slot
// becomes a tombstone the order passes over and the count drops, leaving the key
// and value words in place. A map of references releases the removed value.
func (m *Machine) mapDelete(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 0) {
		return false
	}
	shape, ok := m.mapOf(op.Args[0])
	if !ok || !shape.Serves(op.Op, s.Type(op.Args[1]).Kind(), 0) {
		return false
	}
	exit, resume := s.Bridge()
	hit, done := a.Label(), a.Label()
	data := m.container(a, s.Reg(op.Args[0]))
	key := m.key(a, s, shape, op.Args[1], exit)
	t := m.table(a, data)
	index, tag := m.home(a, t, key)
	slot := m.probe(a, t, key, index, tag, asm.VReg{}, hit, done)

	a.Bind(hit)
	state, count := m.vreg(), m.vreg()
	a.Emit(target.LDI(state, jit.SlotDead)...)
	a.Emit(
		target.STRBR(state, t.ctrl, index),
		target.LDR(count, data, int16(jit.OffsetMapCount)), target.SUBI(count, count, 1), target.STR(count, data, int16(jit.OffsetMapCount)),
	)
	if owns(shape.Elem) {
		old := m.vreg()
		a.Emit(target.LDR(old, slot, 8))
		m.release(a, old, s)
	}
	a.Bind(done)
	a.Bind(resume)
	return true
}

// mapOf is the shape of map operand v when a map guard admitted it.
func (m *Machine) mapOf(v ssa.Value) (ssa.Shape, bool) {
	shape, ok := m.guards[v]
	return shape, ok && shape.Form == ssa.FormMap
}

// key is the table's key word of operand v, as Map.resolve makes it: an i64 is
// its own register, which bridges through exit when it is outside the inline
// range, since the table holds such a key as a heap cell native code neither
// compares nor creates; an i32 zero-extends, an i8 keeps its low byte, and an
// i1 is 0 or 1 (a Boxed.Bool is any nonzero payload).
func (m *Machine) key(a *asm.Assembler, s compile.Site, shape ssa.Shape, v ssa.Value, exit asm.Label) asm.VReg {
	word := s.Reg(v)
	if shape.Key == types.KindI64 {
		a.Emit(target.SBFX(target.X16, word, 0, types.VBits), target.CMP(target.X16, word), target.BCondLabel(target.OpBNE, exit))
		return word
	}
	key := m.vreg()
	switch shape.Key {
	case types.KindI32:
		a.Emit(target.UXTW(key, word))
	case types.KindI8:
		a.Emit(target.UXTB(key, word))
	default:
		a.Emit(target.CMPI(word, 0), target.CSET(key, target.CondNE))
	}
	return key
}

// table loads the words of the map table whose data word is data.
func (m *Machine) table(a *asm.Assembler, data asm.VReg) table {
	t := table{data: data, ctrl: m.vreg(), slots: m.vreg(), mask: m.vreg()}
	a.Emit(
		target.LDR(t.ctrl, data, int16(jit.OffsetMapCtrl)),
		target.LDR(t.slots, data, int16(jit.OffsetMapSlots)),
		target.LDR(t.mask, data, int16(jit.OffsetMapMask)),
	)
	return t
}

// home is the home slot and control byte of key, as Map.find computes them
// from the hash (key ^ seed) * MapHash: the slot is hash >> shift & mask, and
// the control byte SlotFull with the seven hash bits below the slot's.
func (m *Machine) home(a *asm.Assembler, t table, key asm.VReg) (index, tag asm.VReg) {
	index, tag = m.vreg(), m.vreg()
	a.Emit(target.LDR(target.X16, t.data, int16(jit.OffsetMapSeed)), target.EOR(tag, key, target.X16))
	a.Emit(target.LDI(target.X17, jit.MapHash)...)
	a.Emit(
		target.MUL(tag, tag, target.X17),
		target.LDR(target.X16, t.data, int16(jit.OffsetMapShift)),
		target.LSR(index, tag, target.X16),
		target.AND(index, index, t.mask),
		target.SUBI(target.X16, target.X16, 7),
		target.LSR(tag, tag, target.X16),
		target.ANDI(tag, tag, 0x7F),
		target.ORRI(tag, tag, jit.SlotFull),
	)
	return index, tag
}

// probe walks the table linearly from index, as Map.find does. It branches
// to miss at an empty slot and to hit at a full slot holding key, leaving the
// slot's index in index and its address in the returned register. A slot whose
// control byte is not tag, a dead one included, is passed over. A set passes
// free, holding -1, and probe leaves in it the first dead slot it passed.
func (m *Machine) probe(a *asm.Assembler, t table, key, index, tag, free asm.VReg, hit, miss asm.Label) asm.VReg {
	loop, next := a.Label(), a.Label()
	state, slot, word := m.vreg(), m.vreg(), m.vreg()
	a.Bind(loop)
	a.Emit(
		target.LDRBR(state, t.ctrl, index), target.CBZLabel(state, miss),
		target.CMP(state, tag), target.BCondLabel(target.OpBNE, next),
		target.LSLI(slot, index, 4), target.ADD(slot, t.slots, slot),
		target.LDR(word, slot, 0), target.CMP(word, key), target.BCondLabel(target.OpBEQ, hit),
	)
	a.Bind(next)
	if free != (asm.VReg{}) {
		step := a.Label()
		a.Emit(
			target.CMPI(state, jit.SlotDead), target.BCondLabel(target.OpBNE, step),
			target.CMPI(free, 0), target.BCondLabel(target.OpBGE, step),
			target.MOV(free, index),
		)
		a.Bind(step)
	}
	a.Emit(target.ADDI(index, index, 1), target.AND(index, index, t.mask), target.BLabel(loop))
	return slot
}

// elements is the element pointer and length of guarded array v: the ones
// its slice loaded, or loads of a boxed slice header for a scalar Kind's
// TypedArray[T], or the Elems header embedded in *types.Array for KindRef.
func (m *Machine) elements(a *asm.Assembler, s compile.Site, v ssa.Value, shape ssa.Shape) (ptr, ln asm.VReg) {
	if sliced, ok := m.slices[v]; ok {
		return sliced[0], sliced[1]
	}
	header := m.container(a, s.Reg(v))
	if shape.Elem == types.KindRef {
		a.Emit(target.ADDI(header, header, uint16(jit.OffsetArrayElems)))
	}
	ptr, ln = m.vreg(), m.vreg()
	a.Emit(target.LDR(ptr, header, 0))
	a.Emit(target.LDR(ln, header, int16(jit.OffsetSliceLen)))
	return ptr, ln
}

// container is the heap slot's data word for a guarded ref: *types.Array or
// *types.Struct, the heap's own pointer-shaped representation.
func (m *Machine) container(a *asm.Assembler, ref asm.Reg) asm.VReg {
	addr := m.heap(a, ref)
	data := m.vreg()
	a.Emit(target.LDR(data, addr, int16(jit.OffsetData)))
	return data
}

// heap is the address of ref's own heap slot: Context.Heap plus its index
// scaled by SizeofValue (a shift, since SizeofValue is a power of two).
func (m *Machine) heap(a *asm.Assembler, ref asm.Reg) asm.VReg {
	addr := m.vreg()
	a.Emit(target.SBFX(addr, ref, 0, 32), target.LSLI(addr, addr, 4))
	a.Emit(target.LDR(target.X16, Ctx, int16(jit.OffsetHeap)))
	a.Emit(target.ADD(addr, target.X16, addr))
	return addr
}

// element deopts unless op's array index is within [0, ln) and addresses
// its element of 1<<shift bytes off ptr: through the index's own 32-bit
// register, sign-extended, with no check for an index a bound proves within
// op's slice; at an immediate offset for a constant index the 12-bit compare
// and scaled load fields both hold; else through the checked index register
// the access scales.
func (m *Machine) element(a *asm.Assembler, s compile.Site, op ssa.Operation, ptr, ln asm.Reg, shift uint8) address {
	v := op.Args[1]
	if b, ok := m.bounds[v]; ok && b.slice == op.Args[0] {
		return address{base: ptr, index: b.index}
	}
	if c, ok := s.Const(v); ok && int32(c) >= 0 && int32(c) <= imm12 {
		a.Emit(target.CMPI(ln, uint16(c)), target.BCondLabel(target.OpBLS, s.Trap()))
		return address{base: ptr, offset: int16(c << shift)}
	}
	return address{base: ptr, index: m.index(a, s, s.Reg(v), ln)}
}

// index sign-extends a native i32 index and deopts unless it is within
// [0, length), matching an array or struct bounds guard: one unsigned
// compare, since a negative index extends to above any length.
func (m *Machine) index(a *asm.Assembler, s compile.Site, at, length asm.Reg) asm.VReg {
	idx := m.vreg()
	a.Emit(target.SXTW(idx, at))
	a.Emit(target.CMP(idx, length), target.BCondLabel(target.OpBCS, s.Trap()))
	return idx
}

// offset is ptr advanced by idx elements of 1<<shift bytes.
func (m *Machine) offset(a *asm.Assembler, ptr, idx asm.Reg, shift uint8) asm.VReg {
	off := m.vreg()
	a.Emit(target.LSLI(off, idx, shift))
	a.Emit(target.ADD(off, ptr, off))
	return off
}

// replace stores ref word at p, then releases the word it overwrote.
func (m *Machine) replace(a *asm.Assembler, s compile.Site, p address, word asm.Reg) {
	old := m.vreg()
	a.Emit(p.row(target.LDR, target.LDRR, old))
	a.Emit(p.row(target.STR, target.STRR, word))
	m.release(a, old, s)
}

// release counts one reference to ref less, as Heap.Release:
// never the null one. The last reference exits for the interpreter to
// release the object and what it holds.
func (m *Machine) release(a *asm.Assembler, ref asm.VReg, s compile.Site) {
	last, resume := s.Release(ref)
	m.count(a, ref, resume, false)
	a.Emit(
		target.CMPI(target.X17, 1),
		target.BCondLabel(target.OpBLE, last),
		target.SUBI(target.X17, target.X17, 1),
		target.STR(target.X17, target.X16, 0),
	)
	a.Bind(resume)
}

// count points X16 at the reference count of ref and loads it into X17. A
// value that is no reference skips; so does the null reference, a zero index,
// unless null is counted.
func (m *Machine) count(a *asm.Assembler, ref asm.VReg, skip asm.Label, null bool) {
	expectRef(a, ref, skip)
	a.Emit(target.SBFX(target.X17, ref, 0, 32))
	if !null {
		a.Emit(target.CBZLabel(target.X17, skip))
	}
	a.Emit(
		target.LDR(target.X16, Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
	)
}

// vreg is a fresh 64-bit register no SSA value names.
func (m *Machine) vreg() asm.VReg {
	return m.ivreg(asm.Width64)
}

// ivreg returns a fresh integer scratch register at width, tracked by the
// allocator like any SSA temp.
func (m *Machine) ivreg(width asm.RegWidth) asm.VReg {
	m.temp--
	return asm.NewVReg(m.temp, asm.RegTypeInt, width)
}

// owns reports whether a map of elements of kind owns references through them:
// a ref, or an i64 that is a reference to a heap-promoted value when it is wide.
func owns(kind types.Kind) bool {
	return kind == types.KindRef || kind == types.KindI64
}

// arith lowers add or sub at width. A constant operand within imm12 becomes
// an immediate, a negative one through the opposite operation; only add
// takes it first.
func arith(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth, add bool) bool {
	if !has(op, 2, 1) {
		return false
	}
	src, k := op.Args[0], op.Args[1]
	n, ok := immediate(s, k, width)
	if !ok && add {
		src, k = k, src
		n, ok = immediate(s, k, width)
	}
	if !ok {
		if add {
			return binary(a, op, s, target.ADD)
		}
		return binary(a, op, s, target.SUB)
	}
	x, dst := s.Reg(src), s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, width, x, dst) {
		return false
	}
	if add == (n >= 0) {
		a.Emit(target.ADDI(dst, x, uint16(max(n, -n))))
	} else {
		a.Emit(target.SUBI(dst, x, uint16(max(n, -n))))
	}
	return true
}

// signed reports whether cond reads no carry flag, so CMN stands for CMP.
func signed(cond uint8) bool {
	switch cond {
	case target.CondEQ, target.CondNE, target.CondLT, target.CondGT, target.CondLE, target.CondGE:
		return true
	}
	return false
}

// immediate reports the constant v as a signed number at width when its
// magnitude fits one imm12 field.
func immediate(s compile.Site, v ssa.Value, width asm.RegWidth) (int64, bool) {
	word, ok := s.Const(v)
	if !ok {
		return 0, false
	}
	n := int64(word)
	if width == asm.Width32 {
		n = int64(int32(word))
	}
	return n, n >= -imm12 && n <= imm12
}

// checked lowers the overflow-checked add, sub, and mul: the wrapped result,
// then an i1 set when the exact result does not fit the lane, read signed or
// unsigned. Add and sub read the overflow flag, or for unsigned the carry. A
// mul compares the high half of the full product with the sign of the low half
// (unsigned: with zero), in scratch X16 and X17 so the operands survive
// a result that shares their register.
func checked(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 2, 2) {
		return false
	}
	x, y, dst, flag := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0]), s.Reg(op.Results[1])
	if !same(x, y, dst) || x.Type() != asm.RegTypeInt || !match(asm.RegTypeInt, asm.Width32, flag) {
		return false
	}
	wide := x.Width() == asm.Width64
	unsigned := op.Op == ssa.OpIntUAddOvf || op.Op == ssa.OpIntUSubOvf || op.Op == ssa.OpIntUMulOvf
	// Signed add and sub read the overflow flag; the unsigned forms read the
	// carry: set after an add, clear after a sub.
	overflow, borrow := target.CondVS, target.CondVS
	if unsigned {
		overflow, borrow = target.CondCS, target.CondCC
	}
	switch op.Op {
	case ssa.OpIntSAddOvf, ssa.OpIntUAddOvf:
		a.Emit(target.ADDS(dst, x, y), target.CSET(flag, overflow))
	case ssa.OpIntSSubOvf, ssa.OpIntUSubOvf:
		a.Emit(target.SUBS(dst, x, y), target.CSET(flag, borrow))
	case ssa.OpIntSMulOvf, ssa.OpIntUMulOvf:
		switch {
		case wide && unsigned:
			a.Emit(target.UMULH(target.X16, x, y), target.MUL(dst, x, y), target.CMPI(target.X16, 0))
		case wide:
			a.Emit(target.SMULH(target.X16, x, y), target.MUL(dst, x, y), target.ASRI(target.X17, dst, 63), target.CMP(target.X16, target.X17))
		case unsigned:
			a.Emit(target.UMULL(target.X16, x, y), target.UXTW(target.X17, target.W16), target.CMP(target.X16, target.X17), target.MOVW(dst, target.X16))
		default:
			a.Emit(target.SMULL(target.X16, x, y), target.SXTW(target.X17, target.W16), target.CMP(target.X16, target.X17), target.MOVW(dst, target.X16))
		}
		a.Emit(target.CSET(flag, target.CondNE))
	default:
		return false
	}
	return true
}

func binary(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !has(op, 2, 1) {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if !same(x, y, dst) {
		return false
	}
	a.Emit(emit(dst, x, y))
	return true
}

func unary(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if !has(op, 1, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if !same(x, dst) {
		return false
	}
	a.Emit(emit(dst, x))
	return true
}

// divide traps on a zero divisor, then emits the quotient, or for a remainder
// x - quotient*y.
func divide(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if !has(op, 2, 1) {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, width, x, y, dst) {
		return false
	}
	a.Emit(target.CBZLabel(y, s.Trap()))
	div, rem := target.UDIV, false
	switch op.Op {
	case ssa.OpIntSDiv:
		div = target.SDIV
	case ssa.OpIntSRem:
		div, rem = target.SDIV, true
	case ssa.OpIntURem:
		rem = true
	}
	if !rem {
		a.Emit(div(dst, x, y))
		return true
	}
	q := target.W16
	if width == asm.Width64 {
		q = target.X16
	}
	a.Emit(div(q, x, y))
	a.Emit(target.MSUB(dst, q, y, x))
	return true
}

// convert lowers an int-to-int or int-to-float width change, or a float
// conversion between f32 and f64.
func convert(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if !has(op, 1, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	switch {
	case x.Type() == asm.RegTypeInt && dst.Type() == asm.RegTypeFloat:
	case x.Type() == asm.RegTypeInt && dst.Type() == asm.RegTypeInt && x.Width() != dst.Width():
	case x.Type() == asm.RegTypeFloat && dst.Type() == asm.RegTypeFloat && (op.Op == ssa.OpFloatFPExt || op.Op == ssa.OpFloatFPTrunc):
	default:
		return false
	}
	a.Emit(emit(dst, x))
	return true
}

func truncate(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src asm.Reg) asm.Instruction, from, to asm.RegWidth) bool {
	if !has(op, 1, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if !match(asm.RegTypeFloat, from, x) || !match(asm.RegTypeInt, to, dst) {
		return false
	}
	a.Emit(emit(dst, x))
	return true
}

func narrow(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, asm.Width64, x) || !match(asm.RegTypeInt, asm.Width32, dst) {
		return false
	}
	a.Emit(target.MOVW(dst, x))
	return true
}

func reinterpret(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 1, 1) {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if x.Type() == dst.Type() || x.Width() != dst.Width() {
		return false
	}
	a.Emit(target.FMOV(dst, x))
	return true
}

func choose(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if !has(op, 3, 1) {
		return false
	}
	yes, no, cond := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Args[2])
	dst := s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, asm.Width32, cond) || !same(yes, no, dst) {
		return false
	}
	a.Emit(target.CMPI(cond, 0))
	if dst.Type() == asm.RegTypeFloat {
		a.Emit(target.FCSEL(dst, yes, no, target.CondNE))
	} else {
		a.Emit(target.CSEL(dst, yes, no, target.CondNE))
	}
	return true
}

// convention moves v to its register-convention slot i when into, or from it
// otherwise, by v's own type and width: FMOV for a float v, a width-narrowed
// move for a 32-bit v, a plain move otherwise. The slot register is always a
// full-width X0/X1; only v may be float or 32-bit.
func convention(a *asm.Assembler, v asm.Reg, i int, into bool) {
	reg := register(i)
	switch {
	case v.Type() == asm.RegTypeFloat:
		if into {
			a.Emit(target.FMOV(v, reg))
		} else {
			a.Emit(target.FMOV(reg, v))
		}
	case v.Width() == asm.Width32:
		if into {

			a.Emit(target.MOVW(v, reg))
		} else {
			a.Emit(target.MOV(asm.NewPReg(reg.ID(), asm.RegTypeInt, asm.Width32), v))
		}
	default:
		if into {
			a.Emit(target.MOV(v, reg))
		} else {
			a.Emit(target.MOV(reg, v))
		}
	}
}

// box returns the register holding v as a boxed word. An i64 outside the
// inline range exits through Box and resumes with its heap ref in X16.
func box(a *asm.Assembler, s compile.Site, v ssa.Value) asm.Reg {
	src, k := s.Reg(v), s.Type(v).Kind()
	switch k {
	case types.KindRef:
		return src
	case types.KindF64:
		a.Emit(target.FMOV(target.X16, src))
		canonical(a)
		return target.X16
	case types.KindI64:
		a.Emit(target.LDI(target.X16, 1<<(types.VBits-1))...)
		a.Emit(target.ADD(target.X17, src, target.X16), target.LSRI(target.X17, target.X17, types.VBits))
		exit, resume := s.Box(src)
		a.Emit(
			target.CBNZLabel(target.X17, exit),
			target.ANDI(target.X16, src, types.VMask),
		)
		a.Emit(target.LDI(target.X17, types.Tag(k))...)
		a.Emit(target.ORR(target.X16, target.X16, target.X17))
		a.Bind(resume)
		return target.X16
	case types.KindF32:
		a.Emit(target.FMOV(target.W16, src))
	default:
		a.Emit(target.UXTW(target.X16, src))
	}
	a.Emit(target.LDI(target.X17, types.Tag(k))...)
	a.Emit(target.ORR(target.X16, target.X16, target.X17))
	return target.X16
}

// canonical replaces the f64 bits in X16 with types.BoxedNaN when they are a
// positive NaN, as BoxF64 does: one signed compare against +Inf, whose
// successor is BoxedNaN. X17 is scratch.
func canonical(a *asm.Assembler) {
	a.Emit(target.LDI(target.X17, uint64(types.BoxedNaN)-1)...)
	a.Emit(target.CMP(target.X16, target.X17), target.CSINC(target.X16, target.X16, target.X17, target.CondLE))
}

// expectRef branches to miss unless ref is tagged Ref, leaving its tag in X16.
func expectRef(a *asm.Assembler, ref asm.Reg, miss asm.Label) {
	a.Emit(target.LSRI(target.X16, ref, types.VBits))
	expect(a, types.Tag(types.KindRef)>>types.VBits, miss)
}

// same reports whether every register shares the first one's bank and width.
func same(regs ...asm.Reg) bool {
	return match(regs[0].Type(), regs[0].Width(), regs[1:]...)
}

// match reports whether every register is of bank typ and width.
func match(typ asm.RegType, width asm.RegWidth, regs ...asm.Reg) bool {
	for _, r := range regs {
		if r.Type() != typ || r.Width() != width {
			return false
		}
	}
	return true
}

// register is the register-convention register at index i (0 or 1).
func register(i int) asm.PReg {
	if i == 1 {
		return target.X1
	}
	return target.X0
}

// has reports whether op has exactly args operands and results results.
func has(op ssa.Operation, args, results int) bool {
	return len(op.Args) == args && len(op.Results) == results
}

// expect branches to miss unless X16 holds word; X17 is scratch.
func expect(a *asm.Assembler, word uint64, miss asm.Label) {
	a.Emit(target.LDI(target.X17, word)...)
	a.Emit(target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, miss))
}
