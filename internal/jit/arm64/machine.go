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

const (
	mask32 = uint64(0xffffffff)
	mask49 = uint64(types.VMask)
)

type machine struct {
	tempID   int32
	slots    int
	end      asm.Label
	deopt    asm.Label
	deoptSet bool
}

// New returns the ARM64 lowering machine.
func New() *machine {
	return &machine{tempID: -1}
}

func (m *machine) Arch() asm.Arch { return target.New() }

func (m *machine) Reserve() []asm.PReg { return []asm.PReg{target.X16, target.X17, target.X25} }

func (m *machine) Prologue(a *asm.Assembler, slots, params int) {
	m.slots = slots
	m.end = a.Label()
	a.Emit(
		target.SUBI(target.SP, target.SP, 16),
		target.STR(target.LR, target.SP, 8),
		asm.Instruction{
			Op:   uint16(target.OpSUBI),
			Dst:  asm.Physical(target.SP),
			Src1: asm.Physical(target.SP),
			Src2: asm.Slots(),
		},
		target.LDR(target.X25, target.Ctx, int16(jit.OffsetFB)),
	)
	m.record(a)
	for i := params; i < slots; i++ {
		a.Emit(target.STR(target.XZR, target.X25, int16(i*8)))
	}
}

func (m *machine) record(a *asm.Assembler) {
	a.Emit(
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
		target.ADDI(target.X17, target.Ctx, uint16(jit.OffsetRecords)),
		target.LSLI(target.X16, target.X16, 5),
		target.ADD(target.X17, target.X17, target.X16),
		target.STR(target.X25, target.X17, int16(jit.RecordFB)),
		target.ADDI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetDepth)),
	)
}

func (m *machine) Epilogue(a *asm.Assembler) {
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
	if m.deoptSet {
		a.Bind(m.deopt)
		a.Emit(target.BRK(0))
	}
}

func (m *machine) Move(a *asm.Assembler, dst, src asm.VReg) {
	if dst.Type() == asm.RegTypeFloat {
		a.Emit(target.FMOV(dst, src))
		return
	}
	a.Emit(target.MOV(dst, src))
}
func (m *machine) Lower(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	switch op.Op {
	case ssa.OpConst:
		return m.constant(a, op, r)
	case ssa.OpLoad:
		return m.load(a, op, r)
	case ssa.OpStore:
		return m.store(a, op, r)
	case ssa.OpExec:
		return m.exec(a, op, r)
	default:
		return false
	}
}

func (m *machine) Branch(a *asm.Assembler, t ssa.Terminator, r compile.Regs, labels []asm.Label) {
	switch t.Op {
	case ssa.OpJump:
		a.Emit(target.BLabel(labels[0]))
	case ssa.OpBranch:
		a.Emit(target.CBNZLabel(r.Reg(t.Args[0]), labels[0]), target.BLabel(labels[1]))
	case ssa.OpTable:
		index := r.Reg(t.Args[0])
		for i := 0; i < len(labels)-1; i++ {
			a.Emit(target.CMPI(index, uint16(i)), target.BCondLabel(target.OpBEQ, labels[i]))
		}
		a.Emit(target.BLabel(labels[len(labels)-1]))
	}
}

func (m *machine) Return(a *asm.Assembler, args []asm.VReg, types []ssa.Type) {
	m.write(a, args, types, 0)
	a.Emit(target.BLabel(m.end))
}

func (m *machine) Complete(a *asm.Assembler, args []asm.VReg, types []ssa.Type) {
	m.write(a, args, types, m.slots)
	a.Emit(target.BLabel(m.end))
}

func (m *machine) write(a *asm.Assembler, args []asm.VReg, types []ssa.Type, base int) {
	for i, src := range args {
		m.box(a, src, kind(types[i]), target.X16)
		a.Emit(target.STR(target.X16, target.X25, int16((base+i)*8)))
	}
}

func (m *machine) Budget(a *asm.Assembler, safepoint asm.Label) {
	a.Emit(
		target.LDR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.SUBSI(target.X16, target.X16, 1),
		target.STR(target.X16, target.Ctx, int16(jit.OffsetBudget)),
		target.BCondLabel(target.OpBLE, safepoint),
	)
}

func (m *machine) deoptLabel(a *asm.Assembler) asm.Label {
	if !m.deoptSet {
		m.deopt = a.Label()
		m.deoptSet = true
	}
	return m.deopt
}
func (m *machine) constant(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	if len(op.Results) != 1 {
		return false
	}
	dst := r.Reg(op.Results[0])
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

func (m *machine) load(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	if len(op.Results) != 1 {
		return false
	}
	dst := r.Reg(op.Results[0])
	base, ok := m.base(a, op.Slot)
	if !ok {
		return false
	}
	kind := kind(r.Type(op.Results[0]))
	off := int16(op.Slot.Index * 8)
	switch kind {
	case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindRef:
		a.Emit(target.LDR(dst, base, off))
	case types.KindI64:
		a.Emit(target.LDR(dst, base, off), target.SBFX(dst, dst, 0, 49))
	default:
		return false
	}
	return true
}
func (m *machine) store(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	if len(op.Args) != 1 {
		return false
	}
	src := r.Reg(op.Args[0])
	kind := kind(r.Type(op.Args[0]))
	base, ok := m.base(a, op.Slot)
	if !ok {
		return false
	}
	off := int16(op.Slot.Index * 8)
	switch kind {
	case types.KindI1:
		a.Emit(target.UXTW(target.X16, src))
		a.Emit(target.ANDI(target.X16, target.X16, 1))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindI1))...)
		a.Emit(target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, base, off))
	case types.KindI8:
		a.Emit(target.UXTW(target.X16, src))
		a.Emit(target.ANDI(target.X16, target.X16, mask32))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindI8))...)
		a.Emit(target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, base, off))
	case types.KindI32:
		a.Emit(target.UXTW(target.X16, src))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindI32))...)
		a.Emit(target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, base, off))
	case types.KindI64:
		return m.wide(a, src, base, off)
	case types.KindF32:
		a.Emit(target.FMOV(target.X16, src))
		a.Emit(target.ANDI(target.X16, target.X16, mask32))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindF32))...)
		a.Emit(target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, base, off))
	case types.KindF64, types.KindRef:
		a.Emit(target.STR(src, base, off))
	default:
		return false
	}
	return true
}

func (m *machine) base(a *asm.Assembler, slot ssa.Slot) (asm.Reg, bool) {
	if slot.Index < 0 || slot.Index > 4095 {
		return asm.PReg{}, false
	}
	switch slot.Space {
	case ssa.SpaceLocal:
		if slot.Base != 0 {
			return asm.PReg{}, false
		}
		return target.X25, true
	case ssa.SpaceGlobal:
		base := m.temp(asm.RegTypeInt, asm.Width64)
		a.Emit(target.LDR(base, target.Ctx, int16(jit.OffsetGlobals)))
		return base, true
	default:
		return asm.PReg{}, false
	}
}

func (m *machine) wide(a *asm.Assembler, src asm.VReg, base asm.Reg, off int16) bool {
	label := m.deoptLabel(a)
	a.Emit(target.LDI(target.X16, 1<<48)...)
	a.Emit(target.ADD(target.X17, src, target.X16))
	a.Emit(target.LSRI(target.X17, target.X17, 49))
	a.Emit(target.CBNZLabel(target.X17, label))
	a.Emit(target.ANDI(target.X16, src, mask49))
	a.Emit(target.LDI(target.X17, types.Tag(types.KindI64))...)
	a.Emit(target.ORR(target.X16, target.X16, target.X17))
	a.Emit(target.STR(target.X16, base, off))
	return true
}

func (m *machine) exec(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	switch op.Code {
	case instr.I32_ADD:
		return m.binary(a, op, r, target.ADD)
	case instr.I32_SUB:
		return m.binary(a, op, r, target.SUB)
	case instr.I32_MUL:
		return m.binary(a, op, r, target.MUL)
	case instr.I32_AND:
		return m.binary(a, op, r, target.AND)
	case instr.I32_OR:
		return m.binary(a, op, r, target.ORR)
	case instr.I32_XOR:
		return m.binary(a, op, r, target.EOR)
	case instr.I32_SHL:
		return m.binary(a, op, r, target.LSL)
	case instr.I32_SHR_S:
		return m.binary(a, op, r, target.ASR)
	case instr.I32_SHR_U:
		return m.binary(a, op, r, target.LSR)
	case instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U:
		return m.divide(a, op, r, asm.Width32)
	case instr.I32_EQZ:
		return m.eqz(a, op, r, asm.Width32)
	case instr.I32_EQ:
		return m.compare(a, op, r, target.CondEQ, asm.Width32)
	case instr.I32_NE:
		return m.compare(a, op, r, target.CondNE, asm.Width32)
	case instr.I32_LT_S:
		return m.compare(a, op, r, target.CondLT, asm.Width32)
	case instr.I32_LT_U:
		return m.compare(a, op, r, target.CondCC, asm.Width32)
	case instr.I32_GT_S:
		return m.compare(a, op, r, target.CondGT, asm.Width32)
	case instr.I32_GT_U:
		return m.compare(a, op, r, target.CondHI, asm.Width32)
	case instr.I32_LE_S:
		return m.compare(a, op, r, target.CondLE, asm.Width32)
	case instr.I32_LE_U:
		return m.compare(a, op, r, target.CondLS, asm.Width32)
	case instr.I32_GE_S:
		return m.compare(a, op, r, target.CondGE, asm.Width32)
	case instr.I32_GE_U:
		return m.compare(a, op, r, target.CondCS, asm.Width32)
	case instr.I32_EXTEND8_S:
		return m.unary(a, op, r, target.SXTB)
	case instr.I32_EXTEND16_S:
		return m.unary(a, op, r, target.SXTH)
	case instr.I32_TO_I64_S:
		return m.convert(a, op, r, target.SXTW)
	case instr.I32_TO_I64_U:
		return m.convert(a, op, r, target.UXTW)
	case instr.I32_TO_F32_S:
		return m.convert(a, op, r, target.SCVTF)
	case instr.I32_TO_F32_U:
		return m.convert(a, op, r, target.UCVTF)
	case instr.I32_TO_F64_S:
		return m.convert(a, op, r, target.SCVTF)
	case instr.I32_TO_F64_U:
		return m.convert(a, op, r, target.UCVTF)
	case instr.I32_REINTERPRET_F32:
		return m.reinterpret(a, op, r)
	case instr.I64_ADD:
		return m.binary(a, op, r, target.ADD)
	case instr.I64_SUB:
		return m.binary(a, op, r, target.SUB)
	case instr.I64_MUL:
		return m.binary(a, op, r, target.MUL)
	case instr.I64_AND:
		return m.binary(a, op, r, target.AND)
	case instr.I64_OR:
		return m.binary(a, op, r, target.ORR)
	case instr.I64_XOR:
		return m.binary(a, op, r, target.EOR)
	case instr.I64_SHL:
		return m.binary(a, op, r, target.LSL)
	case instr.I64_SHR_S:
		return m.binary(a, op, r, target.ASR)
	case instr.I64_SHR_U:
		return m.binary(a, op, r, target.LSR)
	case instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U:
		return m.divide(a, op, r, asm.Width64)
	case instr.I64_EQZ:
		return m.eqz(a, op, r, asm.Width64)
	case instr.I64_EQ:
		return m.compare(a, op, r, target.CondEQ, asm.Width64)
	case instr.I64_NE:
		return m.compare(a, op, r, target.CondNE, asm.Width64)
	case instr.I64_LT_S:
		return m.compare(a, op, r, target.CondLT, asm.Width64)
	case instr.I64_LT_U:
		return m.compare(a, op, r, target.CondCC, asm.Width64)
	case instr.I64_GT_S:
		return m.compare(a, op, r, target.CondGT, asm.Width64)
	case instr.I64_GT_U:
		return m.compare(a, op, r, target.CondHI, asm.Width64)
	case instr.I64_LE_S:
		return m.compare(a, op, r, target.CondLE, asm.Width64)
	case instr.I64_LE_U:
		return m.compare(a, op, r, target.CondLS, asm.Width64)
	case instr.I64_GE_S:
		return m.compare(a, op, r, target.CondGE, asm.Width64)
	case instr.I64_GE_U:
		return m.compare(a, op, r, target.CondCS, asm.Width64)
	case instr.I64_EXTEND8_S:
		return m.unary(a, op, r, target.SXTB)
	case instr.I64_EXTEND16_S:
		return m.unary(a, op, r, target.SXTH)
	case instr.I64_EXTEND32_S:
		return m.unary(a, op, r, target.SXTW)
	case instr.I64_TO_I32:
		return m.narrow(a, op, r)
	case instr.I64_TO_F32_S:
		return m.convert(a, op, r, target.SCVTF)
	case instr.I64_TO_F32_U:
		return m.convert(a, op, r, target.UCVTF)
	case instr.I64_TO_F64_S:
		return m.convert(a, op, r, target.SCVTF)
	case instr.I64_TO_F64_U:
		return m.convert(a, op, r, target.UCVTF)
	case instr.I64_REINTERPRET_F64:
		return m.reinterpret(a, op, r)

	case instr.F32_ADD:
		return m.binary(a, op, r, target.FADD)
	case instr.F32_SUB:
		return m.binary(a, op, r, target.FSUB)
	case instr.F32_MUL:
		return m.binary(a, op, r, target.FMUL)
	case instr.F32_DIV:
		return m.binary(a, op, r, target.FDIV)
	case instr.F32_ABS:
		return m.unary(a, op, r, target.FABS)
	case instr.F32_NEG:
		return m.unary(a, op, r, target.FNEG)
	case instr.F32_SQRT:
		return m.unary(a, op, r, target.FSQRT)
	case instr.F32_CEIL:
		return m.unary(a, op, r, target.FRINTP)
	case instr.F32_FLOOR:
		return m.unary(a, op, r, target.FRINTM)
	case instr.F32_TRUNC:
		return m.unary(a, op, r, target.FRINTZ)
	case instr.F32_NEAREST:
		return m.unary(a, op, r, target.FRINTN)
	case instr.F32_MIN:
		return m.binary(a, op, r, target.FMIN)
	case instr.F32_MAX:
		return m.binary(a, op, r, target.FMAX)
	case instr.F32_EQ:
		return m.compare(a, op, r, target.CondEQ, asm.Width32)
	case instr.F32_NE:
		return m.compare(a, op, r, target.CondNE, asm.Width32)
	case instr.F32_LT:
		return m.compare(a, op, r, target.CondMI, asm.Width32)
	case instr.F32_LE:
		return m.compare(a, op, r, target.CondLS, asm.Width32)
	case instr.F32_GT:
		return m.compare(a, op, r, target.CondGT, asm.Width32)
	case instr.F32_GE:
		return m.compare(a, op, r, target.CondGE, asm.Width32)
	case instr.F32_TO_I32_S:
		return m.truncate(a, op, r, target.FCVTZS, asm.Width32)
	case instr.F32_TO_I32_U:
		return m.truncate(a, op, r, target.FCVTZU, asm.Width32)
	case instr.F32_TO_I64_S:
		return m.truncate(a, op, r, target.FCVTZS, asm.Width64)
	case instr.F32_TO_I64_U:
		return m.truncate(a, op, r, target.FCVTZU, asm.Width64)
	case instr.F32_TO_F64:
		return m.convert(a, op, r, target.FCVT)
	case instr.F32_REINTERPRET_I32:
		return m.reinterpret(a, op, r)

	case instr.F64_ADD:
		return m.binary(a, op, r, target.FADD)
	case instr.F64_SUB:
		return m.binary(a, op, r, target.FSUB)
	case instr.F64_MUL:
		return m.binary(a, op, r, target.FMUL)
	case instr.F64_DIV:
		return m.binary(a, op, r, target.FDIV)
	case instr.F64_ABS:
		return m.unary(a, op, r, target.FABS)
	case instr.F64_NEG:
		return m.unary(a, op, r, target.FNEG)
	case instr.F64_SQRT:
		return m.unary(a, op, r, target.FSQRT)
	case instr.F64_CEIL:
		return m.unary(a, op, r, target.FRINTP)
	case instr.F64_FLOOR:
		return m.unary(a, op, r, target.FRINTM)
	case instr.F64_TRUNC:
		return m.unary(a, op, r, target.FRINTZ)
	case instr.F64_NEAREST:
		return m.unary(a, op, r, target.FRINTN)
	case instr.F64_MIN:
		return m.binary(a, op, r, target.FMIN)
	case instr.F64_MAX:
		return m.binary(a, op, r, target.FMAX)
	case instr.F64_EQ:
		return m.compare(a, op, r, target.CondEQ, asm.Width64)
	case instr.F64_NE:
		return m.compare(a, op, r, target.CondNE, asm.Width64)
	case instr.F64_LT:
		return m.compare(a, op, r, target.CondMI, asm.Width64)
	case instr.F64_LE:
		return m.compare(a, op, r, target.CondLS, asm.Width64)
	case instr.F64_GT:
		return m.compare(a, op, r, target.CondGT, asm.Width64)
	case instr.F64_GE:
		return m.compare(a, op, r, target.CondGE, asm.Width64)
	case instr.F64_TO_I32_S:
		return m.truncate(a, op, r, target.FCVTZS, asm.Width32)
	case instr.F64_TO_I32_U:
		return m.truncate(a, op, r, target.FCVTZU, asm.Width32)
	case instr.F64_TO_I64_S:
		return m.truncate(a, op, r, target.FCVTZS, asm.Width64)
	case instr.F64_TO_I64_U:
		return m.truncate(a, op, r, target.FCVTZU, asm.Width64)
	case instr.F64_TO_F32:
		return m.convert(a, op, r, target.FCVT)
	case instr.F64_REINTERPRET_I64:
		return m.reinterpret(a, op, r)
	case instr.SELECT:
		return m.choose(a, op, r)
	default:
		return false
	}
}
func (m *machine) binary(a *asm.Assembler, op ssa.Operation, r compile.Regs, emit func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	x, y, dst := r.Reg(op.Args[0]), r.Reg(op.Args[1]), r.Reg(op.Results[0])
	if x.Type() != y.Type() || dst.Type() != x.Type() || x.Width() != y.Width() || x.Width() != dst.Width() {
		return false
	}
	a.Emit(emit(dst, x, y))
	return true
}

func (m *machine) unary(a *asm.Assembler, op ssa.Operation, r compile.Regs, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := r.Reg(op.Args[0]), r.Reg(op.Results[0])
	if x.Type() != dst.Type() || x.Width() != dst.Width() {
		return false
	}
	a.Emit(emit(dst, x))
	return true
}

func (m *machine) divide(a *asm.Assembler, op ssa.Operation, r compile.Regs, width asm.RegWidth) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	x, y, dst := r.Reg(op.Args[0]), r.Reg(op.Args[1]), r.Reg(op.Results[0])
	if x.Type() != asm.RegTypeInt || y.Type() != asm.RegTypeInt || dst.Type() != asm.RegTypeInt || x.Width() != width || y.Width() != width || dst.Width() != width {
		return false
	}
	a.Emit(target.CBZLabel(y, m.deoptLabel(a)))
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

func (m *machine) eqz(a *asm.Assembler, op ssa.Operation, r compile.Regs, width asm.RegWidth) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	src, dst := r.Reg(op.Args[0]), r.Reg(op.Results[0])
	if src.Type() != asm.RegTypeInt || dst.Type() != asm.RegTypeInt || src.Width() != width || dst.Width() != asm.Width32 {
		return false
	}
	a.Emit(target.CMPI(src, 0), target.CSET(dst, target.CondEQ))
	return true
}

func (m *machine) compare(a *asm.Assembler, op ssa.Operation, r compile.Regs, cond uint8, width asm.RegWidth) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	x, y, dst := r.Reg(op.Args[0]), r.Reg(op.Args[1]), r.Reg(op.Results[0])
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

func (m *machine) convert(a *asm.Assembler, op ssa.Operation, r compile.Regs, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := r.Reg(op.Args[0]), r.Reg(op.Results[0])
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

func (m *machine) truncate(a *asm.Assembler, op ssa.Operation, r compile.Regs, emit func(dst, src asm.Reg) asm.Instruction, width asm.RegWidth) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := r.Reg(op.Args[0]), r.Reg(op.Results[0])
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

func (m *machine) narrow(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := r.Reg(op.Args[0]), r.Reg(op.Results[0])
	if x.Type() != asm.RegTypeInt || x.Width() != asm.Width64 || dst.Type() != asm.RegTypeInt || dst.Width() != asm.Width32 {
		return false
	}
	a.Emit(target.MOVW(dst, x))
	return true
}

func (m *machine) reinterpret(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	x, dst := r.Reg(op.Args[0]), r.Reg(op.Results[0])
	if x.Type() == dst.Type() || x.Width() != dst.Width() {
		return false
	}
	a.Emit(target.FMOV(dst, x))
	return true
}

func (m *machine) choose(a *asm.Assembler, op ssa.Operation, r compile.Regs) bool {
	if len(op.Args) != 3 || len(op.Results) != 1 {
		return false
	}
	yes, no, cond := r.Reg(op.Args[0]), r.Reg(op.Args[1]), r.Reg(op.Args[2])
	dst := r.Reg(op.Results[0])
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
func (m *machine) box(a *asm.Assembler, src asm.VReg, kind types.Kind, dst asm.PReg) {
	switch kind {
	case types.KindI1:
		a.Emit(target.UXTW(dst, src))
		a.Emit(target.ANDI(dst, dst, 1))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindI1))...)
	case types.KindI8:
		a.Emit(target.UXTW(dst, src))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindI8))...)
	case types.KindI32:
		a.Emit(target.UXTW(dst, src))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindI32))...)
	case types.KindI64:
		a.Emit(target.ANDI(dst, src, mask49))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindI64))...)
	case types.KindF32:
		a.Emit(target.FMOV(dst, src))
		a.Emit(target.ANDI(dst, dst, mask32))
		a.Emit(target.LDI(target.X17, types.Tag(types.KindF32))...)
	case types.KindF64:
		a.Emit(target.FMOV(dst, src))
	case types.KindRef:
		a.Emit(target.MOV(dst, src))
		return
	default:
		return
	}
	a.Emit(target.ORR(dst, dst, target.X17))
}

func kind(t ssa.Type) types.Kind {
	switch t {
	case ssa.TypeI1:
		return types.KindI1
	case ssa.TypeI8:
		return types.KindI8
	case ssa.TypeI32:
		return types.KindI32
	case ssa.TypeI64:
		return types.KindI64
	case ssa.TypeF32:
		return types.KindF32
	case ssa.TypeF64:
		return types.KindF64
	case ssa.TypeRef:
		return types.KindRef
	default:
		return 0
	}
}
func (m *machine) temp(typ asm.RegType, width asm.RegWidth) asm.VReg {
	m.tempID--
	return asm.NewVReg(m.tempID, typ, width)
}
