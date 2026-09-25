// Package arm64 lowers SSA to ARM64 rows.
package arm64

import (
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
	// carries no such query, and the fact is Machine-local across the whole
	// function, like kinds and temp).
	guards map[ssa.Value]ssa.Shape
	// results is this function's register-convention results (see compile's
	// registers), empty when OpReturn boxes to the VM frame instead.
	results []types.Kind
}

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

// Prologue begins a function at address, builds its frame, pushes the
// activation record, optionally counts the entry, and clears locals after
// params. X25 (frame base) and X27 (activation depth) already hold this
// activation's values: a self call leaves them set, and Enter loads them
// from Context before the outermost call. results is the function's
// register-convention results (see compile's registers): Return keeps it to
// decide whether OpReturn moves results to X0/X1 or boxes them to the frame.
// arguments is the function's register-convention parameters: Prologue
// moves each out of X0/X1 into a fresh vreg, returned in order. It captures
// them after SP is lowered (a spilled capture stores SP-relative) and emits
// every DEF before any move (no capture may take a register not yet read).
func (m *Machine) Prologue(a *asm.Assembler, kinds []types.Kind, params int, count bool, address int, arguments, results []types.Kind) []asm.VReg {
	*m = Machine{kinds: kinds, temp: -1, end: a.Label(), entry: a.Label(), guards: map[ssa.Value]ssa.Shape{}, results: results}
	a.Bind(m.entry)
	a.Emit(
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		asm.Instruction{Op: uint16(target.OpSUBI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LSLI(target.X17, target.X27, 5),
		target.ADD(target.X17, target.Ctx, target.X17),
		target.STR(target.X25, target.X17, int16(jit.OffsetRecords+jit.RecordFB)),
		target.STR(target.LR, target.X17, int16(jit.OffsetRecords+jit.RecordPC)),
		target.ADDI(target.X27, target.X27, 1),
	)
	if count {
		a.Emit(
			target.LDR(target.X16, target.Ctx, int16(jit.OffsetEntries)),
			target.LDR(target.X17, target.X16, int16(8*address)),
			target.ADDI(target.X17, target.X17, 1),
			target.STR(target.X17, target.X16, int16(8*address)),
		)
	}
	for i := params; i < len(kinds); i++ {
		a.Emit(target.STR(target.XZR, target.X25, int16(i*8)))
	}
	for i := range arguments {
		a.Emit(target.DEF(register(i)))
	}
	regs := make([]asm.VReg, len(arguments))
	for i, k := range arguments {
		src := register(i)
		m.temp--
		switch k.Repr() {
		case types.KindF32:
			regs[i] = asm.NewVReg(m.temp, asm.RegTypeFloat, asm.Width32)
			a.Emit(target.FMOV(regs[i], src))
		case types.KindF64:
			regs[i] = asm.NewVReg(m.temp, asm.RegTypeFloat, asm.Width64)
			a.Emit(target.FMOV(regs[i], src))
		case types.KindRef, types.KindI64:
			regs[i] = asm.NewVReg(m.temp, asm.RegTypeInt, asm.Width64)
			a.Emit(target.MOV(regs[i], src))
		default:
			regs[i] = asm.NewVReg(m.temp, asm.RegTypeInt, asm.Width32)
			a.Emit(target.MOVW(regs[i], src))
		}
	}
	return regs
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

// Enter emits the Go entry stub after the epilogue, at code offset > 0 so
// the function body stays at offset 0 for native-to-native calls. It loads
// X25 and X27 from Context (the interpreter writes both before every Enter
// and Resume), loads each register-convention parameter from its slot into
// X0/X1 (low 32 bits for a narrow or f32 payload, the whole word for f64 and
// ref, an i64's SBFX-extracted 49-bit payload: the caller declines Enter
// when the slot holds a heap ref instead), calls the function's own entry,
// boxes each register-convention result from X0/X1 into the VM frame by its
// declared kind (ref, f64 and i64 stored raw; the caller boxes an i64), and
// returns to Go. Enter returns the stub's label so Lower can resolve its
// byte offset after Build.
func (m *Machine) Enter(a *asm.Assembler, arguments, results []types.Kind) asm.Label {
	label := a.Label()
	a.Bind(label)
	a.Emit(
		target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
		target.LDR(target.X27, target.Ctx, int16(jit.OffsetDepth)),
		target.LDR(target.X24, target.Ctx, int16(jit.OffsetBudget)),
	)
	for i, k := range arguments {
		dst := register(i)
		switch k.Repr() {
		case types.KindRef, types.KindF64:
			a.Emit(target.LDR(dst, target.X25, int16(8*i)))
		case types.KindI64:
			a.Emit(target.LDR(dst, target.X25, int16(8*i)))
			a.Emit(target.SBFX(dst, dst, 0, 49))
		default:
			a.Emit(target.LDR(asm.NewPReg(dst.ID(), asm.RegTypeInt, asm.Width32), target.X25, int16(8*i)))
		}
	}
	a.Emit(
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		target.BLLabel(m.entry),
	)
	for i, k := range results {
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
// OpTable takes labels[i] for index i and the last label out of range.
// Branch emits a branch for a SSA terminator.
func (m *Machine) Branch(a *asm.Assembler, t ssa.Terminator, s compile.Site, labels []asm.Label) {
	switch t.Op {
	case ssa.OpJump:
		a.Emit(target.BLabel(labels[0]))
	case ssa.OpBranch:
		a.Emit(target.CBNZLabel(s.Reg(t.Args[0]), labels[0]), target.BLabel(labels[1]))
	case ssa.OpTable:
		index := s.Reg(t.Args[0])
		for i, label := range labels[:len(labels)-1] {
			a.Emit(target.CMPI(index, uint16(i)), target.BCondLabel(target.OpBEQ, label))
		}
		a.Emit(target.BLabel(labels[len(labels)-1]))
	}
}

// Return boxes results into the interpreter's return slots. OpReturn first
// releases reference-capable frame slots, matching threaded RETURN.
// Return emits a native return.
func (m *Machine) Return(a *asm.Assembler, t ssa.Terminator, s compile.Site) {
	base := len(m.kinds)
	if t.Op == ssa.OpReturn {
		base = 0
		for i, k := range m.kinds {
			switch k.Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				continue
			}
			word := m.vreg()
			a.Emit(target.LDR(word, target.X25, int16(i*8)))
			m.release(a, word, s)
			a.Emit(target.STR(target.XZR, target.X25, int16(i*8)))
		}
	}
	if t.Op == ssa.OpReturn && len(m.results) > 0 {
		// USE(X0)/USE(X1) after every move extends their fixed intervals
		// through the whole sequence, so the allocator never assigns a
		// later Arg's own value to a register a prior move already wrote.
		for i, v := range t.Args {
			src, dst := s.Reg(v), register(i)
			switch {
			case src.Type() == asm.RegTypeFloat:
				a.Emit(target.FMOV(dst, src))
			case src.Width() == asm.Width32:
				a.Emit(target.MOV(asm.NewPReg(dst.ID(), asm.RegTypeInt, asm.Width32), src))
			default:
				a.Emit(target.MOV(dst, src))
			}
		}
		a.Emit(target.USE(target.X0))
		if len(t.Args) > 1 {
			a.Emit(target.USE(target.X1))
		}
		a.Emit(target.BLabel(m.end))
		return
	}
	for i, v := range t.Args {
		a.Emit(target.STR(m.box(a, s, v), target.X25, int16((base+i)*8)))
	}
	a.Emit(target.BLabel(m.end))
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
// which Go may have refilled. EXIT has BLR encoding with FlowNext, so use intervals stay
// live across the stub; deopt does not resume and other exits resume in
// native code.
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
	if k == jit.ExitDeopt {
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

// Results loads each bridge result from Context.Results.
func (m *Machine) Results(a *asm.Assembler, regs []asm.VReg) {
	for i, r := range regs {
		a.Emit(target.LDR(r, target.Ctx, int16(int(jit.OffsetResults)+8*i)))
	}
}

// Call writes boxed arguments at the callee frame base and dispatches
// through Context.Natives, or, when Self, branches directly to the unit's
// own entry. Missing code, depth, or space takes ExitCall; an owned Callee
// is released once the callee returns, a borrowed one left alone.
// Call emits a native call.
func (m *Machine) Call(a *asm.Assembler, c compile.Call, s compile.Site) bool {
	if 8*(c.Base+c.Size) > 4095 {
		return false
	}
	record := func(field uintptr) int16 { return int16(jit.OffsetRecords - unsafe.Sizeof(jit.Record{}) + field) }
	for i, v := range c.Args {
		a.Emit(target.STR(m.box(a, s, v), target.X25, int16((c.Base+i)*8)))
	}
	var code asm.VReg
	if !c.Self {
		code = m.vreg()
		a.Emit(target.LDR(code, target.Ctx, int16(jit.OffsetNatives)))
		a.Emit(target.LDI(target.X16, uint64(c.Address))...)
		a.Emit(target.LDRR(code, code, target.X16), target.CBZLabel(code, c.Bridge))
	}
	a.Emit(
		target.ADDI(target.X16, target.X25, uint16(8*(c.Base+c.Size))),
		target.LDR(target.X17, target.Ctx, int16(jit.OffsetTop)),
		target.CMP(target.X16, target.X17),
		target.BCondLabel(target.OpBHI, c.Bridge),
		target.LDR(target.X17, target.Ctx, int16(jit.OffsetLimit)),
		target.CMP(target.X27, target.X17),
		target.BCondLabel(target.OpBCS, c.Bridge),
		target.LSLI(target.X16, target.X27, 5),
		target.ADD(target.X16, target.Ctx, target.X16),
		target.ADDI(target.X17, target.SP, 0),
		target.STR(target.X17, target.X16, record(jit.RecordSP)),
	)
	a.Emit(target.LDI(target.X17, uint64(c.Exit))...)
	a.Emit(
		target.STR(target.X17, target.X16, record(jit.RecordExit)),
		target.ADDI(target.X25, target.X25, uint16(8*c.Base)),
	)
	// A register-passed argument also moves into X0/X1 raw, on top of its
	// boxed slot store (exits read only the slot). The moves sit right
	// before the branch so no other value is allocated X0/X1 in between.
	for i := range c.Arguments {
		src, dst := s.Reg(c.Args[i]), register(i)
		switch {
		case src.Type() == asm.RegTypeFloat:
			a.Emit(target.FMOV(dst, src))
		case src.Width() == asm.Width32:
			a.Emit(target.MOV(asm.NewPReg(dst.ID(), asm.RegTypeInt, asm.Width32), src))
		default:
			a.Emit(target.MOV(dst, src))
		}
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
		m.release(a, s.Reg(c.Callee), s)
	}
	a.Bind(c.Resume)
	if len(c.Registers) > 0 {
		for j, v := range c.Results {
			dst := s.Reg(v)
			src := register(j)
			switch {
			case dst.Type() == asm.RegTypeFloat:
				a.Emit(target.FMOV(dst, src))
			case dst.Width() == asm.Width32:
				a.Emit(target.MOV(dst, asm.NewPReg(src.ID(), asm.RegTypeInt, asm.Width32)))
			default:
				a.Emit(target.MOV(dst, src))
			}
		}
		return true
	}
	for j, v := range c.Results {
		a.Emit(target.LDR(s.Reg(v), target.X25, int16((c.Base+j)*8)))
	}
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

// register is the register-convention result register at index i (0 or 1).
func register(i int) asm.PReg {
	if i == 1 {
		return target.X1
	}
	return target.X0
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
	word := m.box(a, s, op.Args[0])
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
// activation, the globals base for a global.
func (m *Machine) base(a *asm.Assembler, slot ssa.Slot) (asm.Reg, bool) {
	if slot.Index < 0 || slot.Index > 4095 {
		return nil, false
	}
	switch {
	case slot.Space == ssa.SpaceLocal && slot.Base == 0:
		return target.X25, true
	case slot.Space == ssa.SpaceGlobal:
		base := m.vreg()
		a.Emit(target.LDR(base, target.Ctx, int16(jit.OffsetGlobals)))
		return base, true
	default:
		return nil, false
	}
}

func (m *Machine) exec(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	switch op.Code {
	case instr.I32_ADD:
		return m.binary(a, op, s, target.ADD)
	case instr.I32_SUB:
		return m.binary(a, op, s, target.SUB)
	case instr.I32_MUL:
		return m.binary(a, op, s, target.MUL)
	case instr.I32_AND:
		return m.binary(a, op, s, target.AND)
	case instr.I32_OR:
		return m.binary(a, op, s, target.ORR)
	case instr.I32_XOR:
		return m.binary(a, op, s, target.EOR)
	case instr.I32_SHL:
		return m.binary(a, op, s, target.LSL)
	case instr.I32_SHR_S:
		return m.binary(a, op, s, target.ASR)
	case instr.I32_SHR_U:
		return m.binary(a, op, s, target.LSR)
	case instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U:
		return m.divide(a, op, s, asm.Width32)
	case instr.I32_EQZ:
		return m.eqz(a, op, s, asm.Width32)
	case instr.I32_EQ:
		return m.compare(a, op, s, target.CondEQ, asm.Width32)
	case instr.I32_NE:
		return m.compare(a, op, s, target.CondNE, asm.Width32)
	case instr.I32_LT_S:
		return m.compare(a, op, s, target.CondLT, asm.Width32)
	case instr.I32_LT_U:
		return m.compare(a, op, s, target.CondCC, asm.Width32)
	case instr.I32_GT_S:
		return m.compare(a, op, s, target.CondGT, asm.Width32)
	case instr.I32_GT_U:
		return m.compare(a, op, s, target.CondHI, asm.Width32)
	case instr.I32_LE_S:
		return m.compare(a, op, s, target.CondLE, asm.Width32)
	case instr.I32_LE_U:
		return m.compare(a, op, s, target.CondLS, asm.Width32)
	case instr.I32_GE_S:
		return m.compare(a, op, s, target.CondGE, asm.Width32)
	case instr.I32_GE_U:
		return m.compare(a, op, s, target.CondCS, asm.Width32)
	case instr.I32_EXTEND8_S:
		return m.unary(a, op, s, target.SXTB)
	case instr.I32_EXTEND16_S:
		return m.unary(a, op, s, target.SXTH)
	case instr.I32_TO_I64_S:
		return m.convert(a, op, s, target.SXTW)
	case instr.I32_TO_I64_U:
		return m.convert(a, op, s, target.UXTW)
	case instr.I32_TO_F32_S:
		return m.convert(a, op, s, target.SCVTF)
	case instr.I32_TO_F32_U:
		return m.convert(a, op, s, target.UCVTF)
	case instr.I32_TO_F64_S:
		return m.convert(a, op, s, target.SCVTF)
	case instr.I32_TO_F64_U:
		return m.convert(a, op, s, target.UCVTF)
	case instr.I32_REINTERPRET_F32:
		return m.reinterpret(a, op, s)
	case instr.I64_ADD:
		return m.binary(a, op, s, target.ADD)
	case instr.I64_SUB:
		return m.binary(a, op, s, target.SUB)
	case instr.I64_MUL:
		return m.binary(a, op, s, target.MUL)
	case instr.I64_AND:
		return m.binary(a, op, s, target.AND)
	case instr.I64_OR:
		return m.binary(a, op, s, target.ORR)
	case instr.I64_XOR:
		return m.binary(a, op, s, target.EOR)
	case instr.I64_SHL:
		return m.binary(a, op, s, target.LSL)
	case instr.I64_SHR_S:
		return m.binary(a, op, s, target.ASR)
	case instr.I64_SHR_U:
		return m.binary(a, op, s, target.LSR)
	case instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U:
		return m.divide(a, op, s, asm.Width64)
	case instr.I64_EQZ:
		return m.eqz(a, op, s, asm.Width64)
	case instr.I64_EQ:
		return m.compare(a, op, s, target.CondEQ, asm.Width64)
	case instr.I64_NE:
		return m.compare(a, op, s, target.CondNE, asm.Width64)
	case instr.I64_LT_S:
		return m.compare(a, op, s, target.CondLT, asm.Width64)
	case instr.I64_LT_U:
		return m.compare(a, op, s, target.CondCC, asm.Width64)
	case instr.I64_GT_S:
		return m.compare(a, op, s, target.CondGT, asm.Width64)
	case instr.I64_GT_U:
		return m.compare(a, op, s, target.CondHI, asm.Width64)
	case instr.I64_LE_S:
		return m.compare(a, op, s, target.CondLE, asm.Width64)
	case instr.I64_LE_U:
		return m.compare(a, op, s, target.CondLS, asm.Width64)
	case instr.I64_GE_S:
		return m.compare(a, op, s, target.CondGE, asm.Width64)
	case instr.I64_GE_U:
		return m.compare(a, op, s, target.CondCS, asm.Width64)
	case instr.I64_EXTEND8_S:
		return m.unary(a, op, s, target.SXTB)
	case instr.I64_EXTEND16_S:
		return m.unary(a, op, s, target.SXTH)
	case instr.I64_EXTEND32_S:
		return m.unary(a, op, s, target.SXTW)
	case instr.I64_TO_I32:
		return m.narrow(a, op, s)
	case instr.I64_TO_F32_S:
		return m.convert(a, op, s, target.SCVTF)
	case instr.I64_TO_F32_U:
		return m.convert(a, op, s, target.UCVTF)
	case instr.I64_TO_F64_S:
		return m.convert(a, op, s, target.SCVTF)
	case instr.I64_TO_F64_U:
		return m.convert(a, op, s, target.UCVTF)
	case instr.I64_REINTERPRET_F64:
		return m.reinterpret(a, op, s)

	case instr.F32_ADD:
		return m.binary(a, op, s, target.FADD)
	case instr.F32_SUB:
		return m.binary(a, op, s, target.FSUB)
	case instr.F32_MUL:
		return m.binary(a, op, s, target.FMUL)
	case instr.F32_DIV:
		return m.binary(a, op, s, target.FDIV)
	case instr.F32_ABS:
		return m.unary(a, op, s, target.FABS)
	case instr.F32_NEG:
		return m.unary(a, op, s, target.FNEG)
	case instr.F32_SQRT:
		return m.unary(a, op, s, target.FSQRT)
	case instr.F32_CEIL:
		return m.unary(a, op, s, target.FRINTP)
	case instr.F32_FLOOR:
		return m.unary(a, op, s, target.FRINTM)
	case instr.F32_TRUNC:
		return m.unary(a, op, s, target.FRINTZ)
	case instr.F32_NEAREST:
		return m.unary(a, op, s, target.FRINTN)
	case instr.F32_MIN:
		return m.binary(a, op, s, target.FMIN)
	case instr.F32_MAX:
		return m.binary(a, op, s, target.FMAX)
	case instr.F32_EQ:
		return m.compare(a, op, s, target.CondEQ, asm.Width32)
	case instr.F32_NE:
		return m.compare(a, op, s, target.CondNE, asm.Width32)
	case instr.F32_LT:
		return m.compare(a, op, s, target.CondMI, asm.Width32)
	case instr.F32_LE:
		return m.compare(a, op, s, target.CondLS, asm.Width32)
	case instr.F32_GT:
		return m.compare(a, op, s, target.CondGT, asm.Width32)
	case instr.F32_GE:
		return m.compare(a, op, s, target.CondGE, asm.Width32)
	case instr.F32_TO_I32_S:
		return m.truncate(a, op, s, target.FCVTZS, asm.Width32)
	case instr.F32_TO_I32_U:
		return m.truncate(a, op, s, target.FCVTZU, asm.Width32)
	case instr.F32_TO_I64_S:
		return m.truncate(a, op, s, target.FCVTZS, asm.Width64)
	case instr.F32_TO_I64_U:
		return m.truncate(a, op, s, target.FCVTZU, asm.Width64)
	case instr.F32_TO_F64:
		return m.convert(a, op, s, target.FCVT)
	case instr.F32_REINTERPRET_I32:
		return m.reinterpret(a, op, s)

	case instr.F64_ADD:
		return m.binary(a, op, s, target.FADD)
	case instr.F64_SUB:
		return m.binary(a, op, s, target.FSUB)
	case instr.F64_MUL:
		return m.binary(a, op, s, target.FMUL)
	case instr.F64_DIV:
		return m.binary(a, op, s, target.FDIV)
	case instr.F64_ABS:
		return m.unary(a, op, s, target.FABS)
	case instr.F64_NEG:
		return m.unary(a, op, s, target.FNEG)
	case instr.F64_SQRT:
		return m.unary(a, op, s, target.FSQRT)
	case instr.F64_CEIL:
		return m.unary(a, op, s, target.FRINTP)
	case instr.F64_FLOOR:
		return m.unary(a, op, s, target.FRINTM)
	case instr.F64_TRUNC:
		return m.unary(a, op, s, target.FRINTZ)
	case instr.F64_NEAREST:
		return m.unary(a, op, s, target.FRINTN)
	case instr.F64_MIN:
		return m.binary(a, op, s, target.FMIN)
	case instr.F64_MAX:
		return m.binary(a, op, s, target.FMAX)
	case instr.F64_EQ:
		return m.compare(a, op, s, target.CondEQ, asm.Width64)
	case instr.F64_NE:
		return m.compare(a, op, s, target.CondNE, asm.Width64)
	case instr.F64_LT:
		return m.compare(a, op, s, target.CondMI, asm.Width64)
	case instr.F64_LE:
		return m.compare(a, op, s, target.CondLS, asm.Width64)
	case instr.F64_GT:
		return m.compare(a, op, s, target.CondGT, asm.Width64)
	case instr.F64_GE:
		return m.compare(a, op, s, target.CondGE, asm.Width64)
	case instr.F64_TO_I32_S:
		return m.truncate(a, op, s, target.FCVTZS, asm.Width32)
	case instr.F64_TO_I32_U:
		return m.truncate(a, op, s, target.FCVTZU, asm.Width32)
	case instr.F64_TO_I64_S:
		return m.truncate(a, op, s, target.FCVTZS, asm.Width64)
	case instr.F64_TO_I64_U:
		return m.truncate(a, op, s, target.FCVTZU, asm.Width64)
	case instr.F64_TO_F32:
		return m.convert(a, op, s, target.FCVT)
	case instr.F64_REINTERPRET_I64:
		return m.reinterpret(a, op, s)
	case instr.SELECT:
		return m.choose(a, op, s)
	case instr.REF_IS_NULL:
		return m.refIsNull(a, op, s)
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

func (m *Machine) binary(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if x.Type() != y.Type() || dst.Type() != x.Type() || x.Width() != y.Width() || x.Width() != dst.Width() {
		return false
	}
	a.Emit(emit(dst, x, y))
	return true
}

func (m *Machine) unary(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if x.Type() != dst.Type() || x.Width() != dst.Width() {
		return false
	}
	a.Emit(emit(dst, x))
	return true
}

func (m *Machine) divide(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if x.Type() != asm.RegTypeInt || y.Type() != asm.RegTypeInt || dst.Type() != asm.RegTypeInt || x.Width() != width || y.Width() != width || dst.Width() != width {
		return false
	}
	a.Emit(target.CBZLabel(y, s.Deopt()))
	rem := op.Code == instr.I32_REM_S || op.Code == instr.I32_REM_U || op.Code == instr.I64_REM_S || op.Code == instr.I64_REM_U
	if rem {
		q := target.W16
		if width == asm.Width64 {
			q = target.X16
		}
		if op.Code == instr.I32_REM_S || op.Code == instr.I64_REM_S {
			a.Emit(target.SDIV(q, x, y))
		} else {
			a.Emit(target.UDIV(q, x, y))
		}
		a.Emit(target.MSUB(dst, q, y, x))
		return true
	}
	if op.Code == instr.I32_DIV_S || op.Code == instr.I64_DIV_S {
		a.Emit(target.SDIV(dst, x, y))
	} else {
		a.Emit(target.UDIV(dst, x, y))
	}
	return true
}

func (m *Machine) eqz(a *asm.Assembler, op ssa.Operation, s compile.Site, width asm.RegWidth) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	src, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if src.Type() != asm.RegTypeInt || dst.Type() != asm.RegTypeInt || src.Width() != width || dst.Width() != asm.Width32 {
		return false
	}
	a.Emit(target.CMPI(src, 0), target.CSET(dst, target.CondEQ))
	return true
}

// compare lowers integer comparisons and masks unordered float results.
func (m *Machine) compare(a *asm.Assembler, op ssa.Operation, s compile.Site, cond uint8, width asm.RegWidth) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	x, y, dst := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Results[0])
	if x.Type() != y.Type() || dst.Type() != asm.RegTypeInt || x.Width() != width || y.Width() != width || dst.Width() != asm.Width32 {
		return false
	}
	if x.Type() == asm.RegTypeFloat {
		a.Emit(target.FCMP(x, y))
		// FCMP marks unordered with VS; EQ/LE otherwise read unordered as true.
		switch cond {
		case target.CondEQ, target.CondLS:
			a.Emit(target.CSET(dst, cond), target.CSETM(target.W16, target.CondVS), target.BIC(dst, dst, target.W16))
		case target.CondNE:
			a.Emit(target.CSET(dst, cond), target.CSET(target.W16, target.CondVS), target.ORR(dst, dst, target.W16))
		default:
			a.Emit(target.CSET(dst, cond))
		}
		return true
	}
	if x.Type() != asm.RegTypeInt {
		return false
	}
	a.Emit(target.CMP(x, y), target.CSET(dst, cond))
	return true
}

func (m *Machine) convert(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if x.Type() == dst.Type() {
		if x.Type() == asm.RegTypeFloat && op.Code != instr.F32_TO_F64 && op.Code != instr.F64_TO_F32 {
			return false
		}
		if x.Type() == asm.RegTypeInt && x.Width() == dst.Width() {
			return false
		}
	} else if !(x.Type() == asm.RegTypeInt && dst.Type() == asm.RegTypeFloat) {
		return false
	}
	a.Emit(emit(dst, x))
	return true
}

func (m *Machine) truncate(a *asm.Assembler, op ssa.Operation, s compile.Site, emit func(dst, src asm.Reg) asm.Instruction, width asm.RegWidth) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	fw := asm.Width32
	switch op.Code {
	case instr.F64_TO_I32_S, instr.F64_TO_I32_U, instr.F64_TO_I64_S, instr.F64_TO_I64_U:
		fw = asm.Width64
	}
	if x.Type() != asm.RegTypeFloat || dst.Type() != asm.RegTypeInt || x.Width() != fw || dst.Width() != width {
		return false
	}
	a.Emit(emit(dst, x))
	return true
}

func (m *Machine) narrow(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if x.Type() != asm.RegTypeInt || x.Width() != asm.Width64 || dst.Type() != asm.RegTypeInt || dst.Width() != asm.Width32 {
		return false
	}
	a.Emit(target.MOVW(dst, x))
	return true
}

func (m *Machine) reinterpret(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	if x.Type() == dst.Type() || x.Width() != dst.Width() {
		return false
	}
	a.Emit(target.FMOV(dst, x))
	return true
}

func (m *Machine) choose(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 3 || len(op.Results) != 1 {
		return false
	}
	yes, no, cond := s.Reg(op.Args[0]), s.Reg(op.Args[1]), s.Reg(op.Args[2])
	dst := s.Reg(op.Results[0])
	if cond.Type() != asm.RegTypeInt || cond.Width() != asm.Width32 || dst.Type() != yes.Type() || yes.Type() != no.Type() || dst.Width() != yes.Width() || yes.Width() != no.Width() {
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
	a.Emit(target.LSRI(target.X16, word, 49), target.TSTI(target.X16, 1), target.BCondLabel(target.OpBEQ, inline))
	a.Emit(target.LDI(target.X17, types.Tag(types.KindRef)>>49)...)
	a.Emit(target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, inline))
	heap := m.vreg()
	a.Emit(target.SBFX(heap, word, 0, 32), target.LSLI(heap, heap, 4))
	a.Emit(target.LDR(target.X16, target.Ctx, int16(jit.OffsetHeap)))
	a.Emit(target.ADD(heap, target.X16, heap))
	a.Emit(target.LDR(target.X16, heap, 0))
	a.Emit(target.LDI(target.X17, uint64(jit.Itab(types.I64(0))))...)
	a.Emit(target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, s.Deopt()))
	a.Emit(target.LDR(dst, heap, int16(jit.OffsetData)), target.LDR(dst, dst, 0), target.BLabel(done))

	a.Bind(inline)
	a.Emit(target.SBFX(dst, word, 0, 49))
	a.Bind(done)
	return true
}

// value lowers guard.value: Args[0] must equal the admitted word Args[1],
// or the guard deopts. Result is Args[0]'s own word.
func (m *Machine) value(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
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
	a.Emit(target.LSRI(target.X16, ref, 49))
	a.Emit(target.LDI(target.X17, types.Tag(types.KindRef)>>49)...)
	a.Emit(
		target.CMP(target.X16, target.X17),
		target.BCondLabel(target.OpBNE, skip),
		target.SBFX(target.X17, ref, 0, 32),
	)
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

// vreg is a fresh 64-bit register no SSA value names.
func (m *Machine) vreg() asm.VReg {
	m.temp--
	return asm.NewVReg(m.temp, asm.RegTypeInt, asm.Width64)
}

// box returns the register holding v as a boxed word. An i64 outside the
// inline range exits through Box and resumes with its heap ref in X16.
func (m *Machine) box(a *asm.Assembler, s compile.Site, v ssa.Value) asm.Reg {
	src, k := s.Reg(v), s.Type(v).Kind()
	switch k {
	case types.KindF64, types.KindRef:
		return src
	case types.KindI64:
		a.Emit(target.LDI(target.X16, 1<<48)...)
		a.Emit(target.ADD(target.X17, src, target.X16), target.LSRI(target.X17, target.X17, 49))
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
