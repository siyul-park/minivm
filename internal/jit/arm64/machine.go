// Package arm64 lowers SSA to ARM64 rows.
package arm64

import (
	"math"

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
	slots int
	temp  int32
	end   asm.Label
}

// New returns an ARM64 machine.
func New() *Machine {
	return &Machine{}
}

// Arch returns the ARM64 assembler target.
func (m *Machine) Arch() asm.Arch { return target.New() }

// Reserve returns the scratch and frame-base registers.
func (m *Machine) Reserve() []asm.PReg { return []asm.PReg{target.X16, target.X17, target.X25} }

// Prologue begins a function: it builds the frame, loads the frame base,
// pushes the activation record, and clears the locals after params.
func (m *Machine) Prologue(a *asm.Assembler, slots, params int) {
	*m = Machine{slots: slots, temp: -1, end: a.Label()}
	a.Emit(
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		asm.Instruction{Op: uint16(target.OpSUBI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
		target.ADDI(target.X17, target.Ctx, uint16(jit.OffsetRecords)),
		target.LSLI(target.X16, target.X16, 5),
		target.ADD(target.X17, target.X17, target.X16),
		target.STR(target.X25, target.X17, int16(jit.RecordFB)),
		target.ADDI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
	)
	for i := params; i < slots; i++ {
		a.Emit(target.STR(target.XZR, target.X25, int16(i*8)))
	}
}

// Epilogue ends a function: every return branches here to pop the record
// and the frame.
func (m *Machine) Epilogue(a *asm.Assembler) {
	a.Bind(m.end)
	a.Emit(
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
		target.SUBI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
		asm.Instruction{Op: uint16(target.OpADDI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LDR(target.LR, target.SP, 8),
		target.ADDI(target.SP, target.SP, 16),
		target.RET(),
	)
}

// Lower emits op and reports false when it has no ARM64 lowering.
func (m *Machine) Lower(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	switch op.Op {
	case ssa.OpConst:
		return m.constant(a, op, s)
	case ssa.OpLoad:
		return m.load(a, op, s)
	case ssa.OpStore:
		return m.store(a, op, s)
	case ssa.OpExec:
		return m.exec(a, op, s)
	case ssa.OpGuardKind:
		return m.guard(a, op, s)
	case ssa.OpRetain:
		m.retain(a, s.Reg(op.Args[0]))
		return true
	case ssa.OpRelease:
		m.release(a, s.Reg(op.Args[0]), s.Exit(jit.ExitRelease))
		return true
	default:
		return false
	}
}

// Branch transfers control to labels: OpBranch takes labels[0] on nonzero,
// OpTable takes labels[i] for index i and the last label out of range.
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

// Return boxes t's arguments into the VM slots the interpreter reads them
// from: slot 0 on for OpReturn, past the locals for OpComplete.
func (m *Machine) Return(a *asm.Assembler, t ssa.Terminator, s compile.Site) {
	base := 0
	if t.Op == ssa.OpComplete {
		base = m.slots
	}
	for i, v := range t.Args {
		a.Emit(target.STR(m.box(a, s, v), target.X25, int16((base+i)*8)))
	}
	a.Emit(target.BLabel(m.end))
}

// Budget counts Context.Budget down and branches to safepoint when it is
// spent.
func (m *Machine) Budget(a *asm.Assembler, safepoint asm.Label) {
	a.Emit(
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.SUBSI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.BCondLabel(target.OpBLE, safepoint),
	)
}

// Exit writes the exit id and its trap to the Context and calls the exit
// stub. A deopt never returns; any other exit returns when the interpreter
// resumes.
func (m *Machine) Exit(a *asm.Assembler, id int, k jit.Kind, uses []asm.VReg) {
	for _, u := range uses {
		a.Emit(target.USE(u))
	}
	trap := jit.TrapBridge
	if k == jit.ExitDeopt {
		trap = jit.TrapDeopt
	}
	a.Emit(target.LDI(target.X16, uint64(id))...)
	a.Emit(target.STR(target.X16, target.Ctx, int16(jit.OffsetExit)))
	a.Emit(target.LDI(target.X16, uint64(trap))...)
	a.Emit(
		target.STR(target.X16, target.Ctx, int16(jit.OffsetTrap)),
		target.LDR(target.X16, target.Ctx, int16(asm.OffsetStub)),
		target.BLR(target.X16),
	)
	if k == jit.ExitDeopt {
		a.Emit(target.BRK(0))
	}
}

// Results loads each bridge result from Context.Results.
func (m *Machine) Results(a *asm.Assembler, regs []asm.VReg) {
	for i, r := range regs {
		a.Emit(target.LDR(r, target.Ctx, int16(int(jit.OffsetResults)+8*i)))
	}
}

// Move copies src into dst of the same bank.
func (m *Machine) Move(a *asm.Assembler, dst, src asm.VReg) {
	if dst.Type() == asm.RegTypeFloat {
		a.Emit(target.FMOV(dst, src))
		return
	}
	a.Emit(target.MOV(dst, src))
}

func (m *Machine) constant(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Results) != 1 {
		return false
	}
	dst := s.Reg(op.Results[0])
	switch op.Const.Kind() {
	case types.KindI1:
		value := uint64(0)
		if op.Const.Bool() {
			value = 1
		}
		a.Emit(target.LDI(dst, value)...)
	case types.KindI8, types.KindI32:
		a.Emit(target.LDI(dst, uint64(uint32(op.Const.I32())))...)
	case types.KindI64:
		a.Emit(target.LDI(dst, uint64(op.Const.I64()))...)
	case types.KindF32:
		a.Emit(target.LDI(target.X16, uint64(math.Float32bits(op.Const.F32())))...)
		a.Emit(target.FMOV(dst, target.W16))
	case types.KindF64:
		a.Emit(target.LDI(target.X16, uint64(op.Const))...)
		a.Emit(target.FMOV(dst, target.X16))
	case types.KindRef:
		a.Emit(target.LDI(dst, uint64(op.Const))...)
	default:
		return false
	}
	return true
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

func (m *Machine) store(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	if len(op.Args) != 1 {
		return false
	}
	base, ok := m.base(a, op.Slot)
	if !ok {
		return false
	}
	a.Emit(target.STR(m.box(a, s, op.Args[0]), base, int16(op.Slot.Index*8)))
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
		m.temp--
		base := asm.NewVReg(m.temp, asm.RegTypeInt, asm.Width64)
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
	a.Emit(target.CBZLabel(y, s.Exit(jit.ExitDeopt)))
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

// compare sets dst from flags. After FCMP an unordered pair sets C and V:
// EQ, MI (lt), LS (le), GT, and GE read false; NE reads true.
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
	} else if x.Type() == asm.RegTypeInt {
		a.Emit(target.CMP(x, y))
	} else {
		return false
	}
	a.Emit(target.CSET(dst, cond))
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

// guard checks that the slot word of an i64 holds an inline integer, not a
// reference to a promoted one, and unboxes it.
func (m *Machine) guard(a *asm.Assembler, op ssa.Operation, s compile.Site) bool {
	word, dst := s.Reg(op.Args[0]), s.Reg(op.Results[0])
	a.Emit(target.LSRI(target.X16, word, 49))
	a.Emit(target.LDI(target.X17, types.Tag(types.KindI64)>>49)...)
	a.Emit(
		target.CMP(target.X16, target.X17),
		target.BCondLabel(target.OpBNE, s.Exit(jit.ExitDeopt)),
		target.SBFX(dst, word, 0, 49),
	)
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
func (m *Machine) release(a *asm.Assembler, ref asm.VReg, last asm.Label) {
	skip := a.Label()
	m.count(a, ref, skip, false)
	a.Emit(
		target.CMPI(target.X17, 1),
		target.BCondLabel(target.OpBLE, last),
		target.SUBI(target.X17, target.X17, 1),
		target.STR(target.X17, target.X16, 0),
	)
	a.Bind(skip)
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

// box returns the register holding v as a boxed word. An i64 outside the
// inline range deopts.
func (m *Machine) box(a *asm.Assembler, s compile.Site, v ssa.Value) asm.Reg {
	src, k := s.Reg(v), s.Type(v).Kind()
	switch k {
	case types.KindF64, types.KindRef:
		return src
	case types.KindI64:
		a.Emit(target.LDI(target.X16, 1<<48)...)
		a.Emit(
			target.ADD(target.X17, src, target.X16),
			target.LSRI(target.X17, target.X17, 49),
			target.CBNZLabel(target.X17, s.Exit(jit.ExitDeopt)),
			target.ANDI(target.X16, src, types.VMask),
		)
	case types.KindF32:
		a.Emit(target.FMOV(target.W16, src))
	default:
		a.Emit(target.UXTW(target.X16, src))
	}
	a.Emit(target.LDI(target.X17, types.Tag(k))...)
	a.Emit(target.ORR(target.X16, target.X16, target.X17))
	return target.X16
}
