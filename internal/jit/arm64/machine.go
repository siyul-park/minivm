// Package arm64 lowers SSA to ARM64 rows.
package arm64

import (
	"math/bits"
	"slices"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
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
	// slices is the element pointer and length each OpSlice result's array
	// had where the slice ran; array ops naming the result read these.
	slices map[ssa.Value][2]asm.VReg
	// bounds is the index register and slice each OpBound result names.
	bounds map[ssa.Value]bound
	// registers is this function's register-convention results (see compile's
	// registers), empty when OpReturn boxes to the VM frame instead.
	registers []types.Kind
	// borrows is per-parameter, from transform.Borrows: OpReturn releases a
	// borrowed slot only at depth 1, the Go-entered activation, since
	// threaded CALL pushed it owned; a native caller lent it instead. Empty
	// for an OSR unit, whose threaded-entered frame owns every slot.
	borrows []bool
	// upvals holds the activation's upvals base once Prologue loads it
	// (Layout.Upvals); upval slots address it.
	upvals asm.VReg
	// flag is the compare Site.Fuse left in the condition flags, true
	// under cond, for the OpBranch right after it.
	flag ssa.Value
	cond uint8
}

// bound is an index register proven within the slice an OpBound names.
type bound struct {
	slice ssa.Value
	index asm.VReg
}

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

// recordShift is jit.Record's size expressed as a left-shift amount, so the
// activation depth (X27) turns into a Records byte offset by shifting
// instead of multiplying; jit's own layout test pins the size to 32.
var recordShift = uint8(bits.TrailingZeros64(uint64(unsafe.Sizeof(jit.Record{}))))

// New returns an ARM64 machine.
func New() *Machine {
	return &Machine{}
}

// Arch returns the ARM64 assembler target.
func (m *Machine) Arch() asm.Arch { return target.New() }

// Reserve returns the scratch, budget, frame-base, and depth registers.
func (m *Machine) Reserve() []asm.PReg {
	return []asm.PReg{target.X16, target.X17, target.X24, target.X25, target.X27}
}

// Prologue builds the frame, records the activation, counts the entry when
// enabled, and starts non-parameter locals at their zeros, loading each
// distinct zero once. Register-convention arguments move from X0/X1 into args
// before those registers are repurposed; the upvals base loads after them.
// A Machine lowers many functions in sequence (a Queue worker reuses one), so
// Prologue resets all per-function state.
func (m *Machine) Prologue(a *asm.Assembler, address int, count bool, l compile.Layout, args []asm.VReg) {
	// upvals, flag, and cond restart zero, which is unreadable: the upval
	// read guards on a nonzero VReg, and ssa.NoValue names no branch
	// argument, so nothing observes them before this function writes them.
	*m = Machine{kinds: l.Kinds, temp: -1, end: a.Label(), entry: a.Label(), guards: map[ssa.Value]ssa.Shape{}, slices: map[ssa.Value][2]asm.VReg{}, bounds: map[ssa.Value]bound{}, registers: l.Registers, borrows: l.Borrows}
	a.Bind(m.entry)
	a.Emit(
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		asm.Instruction{Op: uint16(target.OpSUBI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LSLI(target.X17, target.X27, recordShift),
		target.ADD(target.X17, target.Ctx, target.X17),
		target.STR(target.X25, target.X17, int16(jit.OffsetRecords+jit.RecordFB)),
		target.STR(target.LR, target.X17, int16(jit.OffsetRecords+jit.RecordPC)),
		target.ADDI(target.X27, target.X27, 1),
	)
	if count {
		a.Emit(target.LDR(target.X16, target.Ctx, int16(jit.OffsetEntries)))
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
		a.Emit(target.LDR(m.upvals, target.Ctx, int16(jit.OffsetUpvals)))
	}
}

// Epilogue ends a function: every return branches here to pop the record
// and the frame.
func (m *Machine) Epilogue(a *asm.Assembler) {
	a.Bind(m.end)
	a.Emit(
		target.SUBI(target.X27, target.X27, 1),
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
		target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
		target.LDR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
		target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
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
		target.STR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
	)
	for i, k := range l.Registers {
		src := register(i)
		switch k.Repr() {
		case types.KindRef, types.KindF64, types.KindI64:
			a.Emit(target.STR(src, target.X25, int16(8*i)))
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
	case ssa.OpExec:
		return m.exec(a, op, s)
	case ssa.OpGuardKind:
		return m.guard(a, op, s)
	case ssa.OpGuardShape:
		return m.shape(a, op, s)
	case ssa.OpGuardValue:
		return m.value(a, op, s)
	case ssa.OpSlice:
		return m.slice(a, op, s)
	case ssa.OpBound:
		return m.bound(op, s)
	case ssa.OpGuardBounds:
		return limit(a, op, s)
	case ssa.OpRetain:
		m.retain(a, s.Reg(op.Args[0]))
		return true
	case ssa.OpRelease:
		m.release(a, s.Reg(op.Args[0]), s)
		return true
	default:
		return false
	}
}

// Branch transfers control to labels: OpBranch takes labels[0] on nonzero,
// OpTable takes labels[i] for index i and the last label out of range. An
// edge to next falls through; an OpBranch whose nonzero edge is next
// branches on the inverted condition instead. A fused compare's condition
// is read from the flags.
func (m *Machine) Branch(a *asm.Assembler, t ssa.Terminator, s compile.Site, labels []asm.Label, next asm.Label) {
	switch t.Op {
	case ssa.OpJump:
	case ssa.OpBranch:
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
	case ssa.OpTable:
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

// Return boxes results into the interpreter's return slots. OpReturn first
// releases reference-capable frame slots, matching threaded RETURN.
func (m *Machine) Return(a *asm.Assembler, t ssa.Terminator, s compile.Site) {
	base := len(m.kinds)
	if t.Op == ssa.OpReturn {
		base = 0
		if slices.Contains(m.borrows, true) {
			skip := a.Label()
			a.Emit(target.CMPI(target.X27, 1), target.BCondLabel(target.OpBNE, skip))
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
	if t.Op == ssa.OpReturn && len(m.registers) > 0 {
		// USE(X0)/USE(X1) after every move extends their fixed intervals
		// through the whole sequence, so the allocator never assigns a
		// later Arg's own value to a register a prior move already wrote.
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

// Exit stores X27 to Context.Depth and X24 to Context.Budget (their only
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
		target.STR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
		target.STR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
	)
	a.Emit(target.LDI(target.X16, uint64(id))...)
	a.Emit(target.STR(target.X16, target.Ctx, int16(jit.OffsetExit)))
	a.Emit(target.LDI(target.X16, uint64(trap))...)
	a.Emit(
		target.STR(target.X16, target.Ctx, int16(jit.OffsetTrap)),
		target.LDR(target.X16, target.Ctx, int16(asm.OffsetStub)),
		target.EXIT(target.X16),
	)
	for _, u := range uses {
		a.Emit(target.USE(u))
	}
	if !k.Resumes() {
		a.Emit(target.BRK(0))
		return
	}
	a.Emit(target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)))
	if k == jit.ExitBox {
		a.Emit(target.LDR(target.X16, target.Ctx, int16(jit.OffsetResults)))
	}
}

// Spill saves a value in a fixed spill slot.
func (m *Machine) Spill(a *asm.Assembler, reg asm.VReg, slot int) {
	a.Emit(target.New().Spill(reg, slot))
}

// Results loads each exit result from Context.Results.
func (m *Machine) Results(a *asm.Assembler, regs []asm.VReg) {
	for i, r := range regs {
		a.Emit(target.LDR(r, target.Ctx, int16(int(jit.OffsetResults)+8*i)))
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
	record := func(field uintptr) int16 { return int16(jit.OffsetRecords - unsafe.Sizeof(jit.Record{}) + field) }
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
		a.Emit(target.LDR(code, target.Ctx, int16(jit.OffsetNatives)))
		a.Emit(target.LDI(target.X16, uint64(c.Callee))...)
		a.Emit(target.LDRR(code, code, target.X16), target.CBZLabel(code, c.Stub))
	}
	a.Emit(
		target.ADDI(target.X16, target.X25, uint16(8*(c.Base+c.Size))),
		target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
		target.CMP(target.X16, target.X17),
		target.BCondLabel(target.OpBHI, c.Stub),
		target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
		target.CMP(target.X27, target.X17),
		target.BCondLabel(target.OpBCS, c.Stub),
		target.LSLI(target.X16, target.X27, recordShift),
		target.ADD(target.X16, target.Ctx, target.X16),
		target.ADDI(target.X17, target.SP, 0),
		target.STR(target.X17, target.X16, record(jit.RecordSP)),
	)
	a.Emit(target.LDI(target.X17, uint64(c.Exit))...)
	a.Emit(target.STR(target.X17, target.X16, record(jit.RecordExit)))
	// Spend before changing X25 so a safepoint resumes with the caller frame.
	a.Emit(target.SUBSI(target.X24, target.X24, 1), target.BCondLabel(target.OpBLE, c.Safepoint))
	a.Bind(c.Resume)
	if c.Upvals {
		upvals := m.container(a, s.Reg(c.Target))
		a.Emit(
			target.LDR(upvals, upvals, int16(jit.OffsetClosureUpvals)),
			target.STR(upvals, target.Ctx, int16(jit.OffsetUpvals)),
		)
	}
	a.Emit(target.ADDI(target.X25, target.X25, uint16(8*c.Base)))
	// A register-passed argument also moves into X0/X1 raw, on top of its
	// boxed slot store (exits read only the slot). The moves sit right
	// before the branch so no other value is allocated X0/X1 in between.
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
	// DEF marks X0 (and X1) written by the call: BL/BLR's Flow is FlowCall,
	// so the default Writes rule (Dst set, Flow FlowNext) never sees it.
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
	// Box first: a box exit that deopts leaves the old occupant to threaded.
	word := box(a, s, op.Args[0])
	// An i64 slot may hold a heap-promoted ref; release skips inline words.
	if k := s.Slot(op.Slot); k == ssa.TypeRef || k == ssa.TypeI64 {
		if _, ok := word.(asm.VReg); !ok {
			// Release clobbers the X16 box result.
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
		a.Emit(target.LDR(base, target.Ctx, int16(jit.OffsetGlobals)))
		return base, true
	case slot.Space == ssa.SpaceUpval && m.upvals != asm.VReg{}:
		return m.upvals, true
	default:
		return nil, false
	}
}

// exec lowers an OpExec by its instruction code.
func (m *Machine) exec(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	switch op.Code {
	case instr.I32_ADD, instr.I64_ADD:
		return binary(a, op, s, target.ADD)
	case instr.I32_SUB, instr.I64_SUB:
		return binary(a, op, s, target.SUB)
	case instr.I32_MUL, instr.I64_MUL:
		return binary(a, op, s, target.MUL)
	case instr.I32_AND, instr.I64_AND:
		return binary(a, op, s, target.AND)
	case instr.I32_OR, instr.I64_OR:
		return binary(a, op, s, target.ORR)
	case instr.I32_XOR, instr.I64_XOR:
		return binary(a, op, s, target.EOR)
	case instr.I32_SHL, instr.I64_SHL:
		return binary(a, op, s, target.LSL)
	case instr.I32_SHR_S, instr.I64_SHR_S:
		return binary(a, op, s, target.ASR)
	case instr.I32_SHR_U, instr.I64_SHR_U:
		return binary(a, op, s, target.LSR)
	case instr.I32_ROTR, instr.I64_ROTR:
		return binary(a, op, s, target.ROR)
	case instr.I32_ROTL:
		return m.rotl(a, op, s, asm.Width32)
	case instr.I64_ROTL:
		return m.rotl(a, op, s, asm.Width64)
	case instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U:
		return divide(a, op, s, asm.Width32)
	case instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U:
		return divide(a, op, s, asm.Width64)
	case instr.I32_EQZ:
		return m.eqz(a, op, s, asm.Width32)
	case instr.I64_EQZ:
		return m.eqz(a, op, s, asm.Width64)
	case instr.I32_EQ, instr.F32_EQ:
		return m.compare(a, op, s, target.CondEQ, asm.Width32)
	case instr.I32_NE, instr.F32_NE:
		return m.compare(a, op, s, target.CondNE, asm.Width32)
	case instr.I32_LT_S:
		return m.compare(a, op, s, target.CondLT, asm.Width32)
	case instr.I32_LT_U:
		return m.compare(a, op, s, target.CondCC, asm.Width32)
	case instr.I32_GT_S, instr.F32_GT:
		return m.compare(a, op, s, target.CondGT, asm.Width32)
	case instr.I32_GT_U:
		return m.compare(a, op, s, target.CondHI, asm.Width32)
	case instr.I32_LE_S:
		return m.compare(a, op, s, target.CondLE, asm.Width32)
	case instr.I32_LE_U:
		return m.compare(a, op, s, target.CondLS, asm.Width32)
	case instr.I32_GE_S, instr.F32_GE:
		return m.compare(a, op, s, target.CondGE, asm.Width32)
	case instr.I32_GE_U:
		return m.compare(a, op, s, target.CondCS, asm.Width32)
	case instr.I64_EQ, instr.F64_EQ:
		return m.compare(a, op, s, target.CondEQ, asm.Width64)
	case instr.I64_NE, instr.F64_NE:
		return m.compare(a, op, s, target.CondNE, asm.Width64)
	case instr.I64_LT_S:
		return m.compare(a, op, s, target.CondLT, asm.Width64)
	case instr.I64_LT_U:
		return m.compare(a, op, s, target.CondCC, asm.Width64)
	case instr.I64_GT_S, instr.F64_GT:
		return m.compare(a, op, s, target.CondGT, asm.Width64)
	case instr.I64_GT_U:
		return m.compare(a, op, s, target.CondHI, asm.Width64)
	case instr.I64_LE_S:
		return m.compare(a, op, s, target.CondLE, asm.Width64)
	case instr.I64_LE_U:
		return m.compare(a, op, s, target.CondLS, asm.Width64)
	case instr.I64_GE_S, instr.F64_GE:
		return m.compare(a, op, s, target.CondGE, asm.Width64)
	case instr.I64_GE_U:
		return m.compare(a, op, s, target.CondCS, asm.Width64)
	case instr.F32_LT:
		return m.compare(a, op, s, target.CondMI, asm.Width32)
	case instr.F32_LE:
		return m.compare(a, op, s, target.CondLS, asm.Width32)
	case instr.F64_LT:
		return m.compare(a, op, s, target.CondMI, asm.Width64)
	case instr.F64_LE:
		return m.compare(a, op, s, target.CondLS, asm.Width64)
	case instr.I32_EXTEND8_S, instr.I64_EXTEND8_S:
		return unary(a, op, s, target.SXTB)
	case instr.I32_EXTEND16_S, instr.I64_EXTEND16_S:
		return unary(a, op, s, target.SXTH)
	case instr.I64_EXTEND32_S:
		return unary(a, op, s, target.SXTW)
	case instr.I32_CLZ, instr.I64_CLZ:
		return unary(a, op, s, target.CLZ)
	case instr.I32_CTZ:
		return m.ctz(a, op, s, asm.Width32)
	case instr.I64_CTZ:
		return m.ctz(a, op, s, asm.Width64)
	case instr.I32_POPCNT:
		return m.popcnt(a, op, s, asm.Width32)
	case instr.I64_POPCNT:
		return m.popcnt(a, op, s, asm.Width64)
	case instr.I32_TO_I64_S:
		return convert(a, op, s, target.SXTW)
	case instr.I32_TO_I64_U:
		return convert(a, op, s, target.UXTW)
	case instr.I32_TO_F32_S, instr.I32_TO_F64_S, instr.I64_TO_F32_S, instr.I64_TO_F64_S:
		return convert(a, op, s, target.SCVTF)
	case instr.I32_TO_F32_U, instr.I32_TO_F64_U, instr.I64_TO_F32_U, instr.I64_TO_F64_U:
		return convert(a, op, s, target.UCVTF)
	case instr.F32_TO_F64, instr.F64_TO_F32:
		return convert(a, op, s, target.FCVT)
	case instr.I64_TO_I32:
		return narrow(a, op, s)
	case instr.I32_REINTERPRET_F32, instr.I64_REINTERPRET_F64, instr.F32_REINTERPRET_I32, instr.F64_REINTERPRET_I64:
		return reinterpret(a, op, s)
	case instr.F32_ADD, instr.F64_ADD:
		return binary(a, op, s, target.FADD)
	case instr.F32_SUB, instr.F64_SUB:
		return binary(a, op, s, target.FSUB)
	case instr.F32_MUL, instr.F64_MUL:
		return binary(a, op, s, target.FMUL)
	case instr.F32_DIV, instr.F64_DIV:
		return binary(a, op, s, target.FDIV)
	case instr.F32_MIN, instr.F64_MIN:
		return binary(a, op, s, target.FMIN)
	case instr.F32_MAX, instr.F64_MAX:
		return binary(a, op, s, target.FMAX)
	case instr.F32_ABS, instr.F64_ABS:
		return unary(a, op, s, target.FABS)
	case instr.F32_NEG, instr.F64_NEG:
		return unary(a, op, s, target.FNEG)
	case instr.F32_SQRT, instr.F64_SQRT:
		return unary(a, op, s, target.FSQRT)
	case instr.F32_CEIL, instr.F64_CEIL:
		return unary(a, op, s, target.FRINTP)
	case instr.F32_FLOOR, instr.F64_FLOOR:
		return unary(a, op, s, target.FRINTM)
	case instr.F32_TRUNC, instr.F64_TRUNC:
		return unary(a, op, s, target.FRINTZ)
	case instr.F32_NEAREST, instr.F64_NEAREST:
		return unary(a, op, s, target.FRINTN)
	case instr.F32_COPYSIGN:
		return m.copysign(a, op, s, asm.Width32)
	case instr.F64_COPYSIGN:
		return m.copysign(a, op, s, asm.Width64)
	case instr.F32_TO_I32_S:
		return truncate(a, op, s, target.FCVTZS, asm.Width32, asm.Width32)
	case instr.F32_TO_I32_U:
		return truncate(a, op, s, target.FCVTZU, asm.Width32, asm.Width32)
	case instr.F32_TO_I64_S:
		return truncate(a, op, s, target.FCVTZS, asm.Width32, asm.Width64)
	case instr.F32_TO_I64_U:
		return truncate(a, op, s, target.FCVTZU, asm.Width32, asm.Width64)
	case instr.F64_TO_I32_S:
		return truncate(a, op, s, target.FCVTZS, asm.Width64, asm.Width32)
	case instr.F64_TO_I32_U:
		return truncate(a, op, s, target.FCVTZU, asm.Width64, asm.Width32)
	case instr.F64_TO_I64_S:
		return truncate(a, op, s, target.FCVTZS, asm.Width64, asm.Width64)
	case instr.F64_TO_I64_U:
		return truncate(a, op, s, target.FCVTZU, asm.Width64, asm.Width64)
	case instr.SELECT:
		return choose(a, op, s)
	case instr.REF_IS_NULL:
		return refIsNull(a, op, s)
	case instr.ARRAY_GET:
		return m.arrayGet(a, op, s)
	case instr.ARRAY_SET:
		return m.arraySet(a, op, s)
	case instr.ARRAY_LEN:
		return m.arrayLen(a, op, s)
	case instr.STRUCT_GET:
		return m.structGet(a, op, s)
	case instr.STRUCT_SET:
		return m.structSet(a, op, s)
	default:
		return false
	}
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
	switch op.Code {
	case instr.I32_DIV_S, instr.I64_DIV_S:
		div = target.SDIV
	case instr.I32_REM_S, instr.I64_REM_S:
		div, rem = target.SDIV, true
	case instr.I32_REM_U, instr.I64_REM_U:
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

func (m *Machine) eqz(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if !has(op, 1, 1) {
		return false
	}
	src, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if !match(asm.RegTypeInt, width, src) || !match(asm.RegTypeInt, asm.Width32, dst) {
		return false
	}
	a.Emit(target.CMPI(src, 0))
	m.set(a, s, op.Results[0], dst, target.CondEQ)
	return true
}

// compare lowers integer and float comparisons.
func (m *Machine) compare(a *asm.Assembler, op ssa.Operation, s compile.Site, cond uint8, width asm.RegWidth) bool {
	if !has(op, 2, 1) {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if !match(x.Type(), width, x, y) || !match(asm.RegTypeInt, asm.Width32, dst) {
		return false
	}
	switch x.Type() {
	case asm.RegTypeFloat:
		// FCMP sets NZCV to 0011 on unordered, where every cond but NE
		// reads false, as the comparison requires.
		a.Emit(target.FCMP(x, y))
	case asm.RegTypeInt:
		a.Emit(target.CMP(x, y))
	default:
		return false
	}
	m.set(a, s, op.Results[0], dst, cond)
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
	case x.Type() == asm.RegTypeFloat && dst.Type() == asm.RegTypeFloat && (op.Code == instr.F32_TO_F64 || op.Code == instr.F64_TO_F32):
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

// retain counts one more reference to ref, as the interpreter's retainBox:
// any reference, the null one included.
func (m *Machine) retain(a *asm.Assembler, ref asm.VReg) {
	skip := a.Label()
	m.count(a, ref, skip, true)
	a.Emit(target.ADDI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0))
	a.Bind(skip)
}

// release counts one reference to ref less, as the interpreter's releaseBox:
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
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
	)
}

// box returns the register holding v as a boxed word. An i64 outside the
// inline range exits through Box and resumes with its heap ref in X16.
func box(a *asm.Assembler, s compile.Site, v ssa.Value) asm.Reg {
	src, k := s.Reg(v), s.Type(v).Kind()
	switch k {
	case types.KindF64, types.KindRef:
		return src
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

// expect branches to miss unless X16 holds word; X17 is scratch.
func expect(a *asm.Assembler, word uint64, miss asm.Label) {
	a.Emit(target.LDI(target.X17, word)...)
	a.Emit(target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, miss))
}

// expectRef branches to miss unless ref is tagged Ref, leaving its tag in X16.
func expectRef(a *asm.Assembler, ref asm.Reg, miss asm.Label) {
	a.Emit(target.LSRI(target.X16, ref, types.VBits))
	expect(a, types.Tag(types.KindRef)>>types.VBits, miss)
}

// has reports whether op has exactly args operands and results results.
func has(op ssa.Operation, args, results int) bool {
	return len(op.Args) == args && len(op.Results) == results
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

// same reports whether every register shares the first one's bank and width.
func same(regs ...asm.Reg) bool {
	return match(regs[0].Type(), regs[0].Width(), regs[1:]...)
}

// join binds c.Join and loads c's results from the callee frame's slots.
func (m *Machine) join(a *asm.Assembler, c compile.Call, s compile.Site) {
	a.Bind(c.Join)
	for j, v := range c.Results {
		a.Emit(target.LDR(s.Reg(v), target.X25, int16((c.Base+j)*8)))
	}
}

// register is the register-convention register at index i (0 or 1).
func register(i int) asm.PReg {
	if i == 1 {
		return target.X1
	}
	return target.X0
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
			// reg is full width; MOVW takes its low 32 bits into v.
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

// vreg is a fresh 64-bit register no SSA value names.
func (m *Machine) vreg() asm.VReg { return m.ivreg(asm.Width64) }

// ivreg returns a fresh integer scratch register at width, tracked by the
// allocator like any SSA temp.
func (m *Machine) ivreg(width asm.RegWidth) asm.VReg {
	m.temp--
	return asm.NewVReg(m.temp, asm.RegTypeInt, width)
}

// fvreg returns a fresh float scratch register at width.
func (m *Machine) fvreg(width asm.RegWidth) asm.VReg {
	m.temp--
	return asm.NewVReg(m.temp, asm.RegTypeFloat, width)
}
