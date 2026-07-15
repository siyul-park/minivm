package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// arm64Lowerer is the AArch64 JIT lowerer.
type arm64Lowerer struct{}

// Boxing masks used by scalar lowering.
const (
	maskI32 = uint64(0xFFFFFFFF)
	maskI64 = uint64(0x0001_FFFF_FFFF_FFFF)

	boxableWidth = uint8(49)
)

const (
	sliceData   = 0
	sliceLen    = 8
	arrayElems  = int(unsafe.Offsetof(types.Array{}.Elems))
	structTyp   = int(unsafe.Offsetof(types.Struct{}.Typ))
	structData  = int(unsafe.Offsetof(types.Struct{}.Data))
	closureUpvs = int(unsafe.Offsetof(types.Closure{}.Upvals))
	fieldsSlice = int(unsafe.Offsetof(types.StructType{}.Fields))
	fieldKind   = int(unsafe.Offsetof(types.StructField{}.Kind))
	fieldSize   = int(unsafe.Sizeof(types.StructField{}))
	errorValue  = types.ErrorValueOffset
	coroValue   = int(unsafe.Offsetof(Coroutine{}.value))
	coroDone    = int(unsafe.Offsetof(Coroutine{}.done))
)

const branchTableLimit = 32
const nativeBackend = true

// Boxing tags used by scalar lowering, derived from the Kind
// tag layout so they track any reordering of the Kind enum. i1/i8 share the i32
// representation and box through tagI32.
var (
	tagI1  = types.Tag(types.KindI1)
	tagI8  = types.Tag(types.KindI8)
	tagI32 = types.Tag(types.KindI32)
	tagI64 = types.Tag(types.KindI64)
	tagF32 = types.Tag(types.KindF32)
	tagRef = types.Tag(types.KindRef)
)

var (
	heapI32       = itab(types.I32(0))
	heapF32       = itab(types.F32(0))
	heapF64       = itab(types.F64(0))
	heapArrayI1   = itab(types.TypedArray[bool](nil))
	heapArrayI8   = itab(types.TypedArray[int8](nil))
	heapArrayI32  = itab(types.TypedArray[int32](nil))
	heapArrayI64  = itab(types.TypedArray[int64](nil))
	heapArrayF32  = itab(types.TypedArray[float32](nil))
	heapArrayF64  = itab(types.TypedArray[float64](nil))
	heapArrayRef  = itab((*types.Array)(nil))
	heapString    = itab(types.String(""))
	heapStruct    = itab((*types.Struct)(nil))
	heapError     = itab((*types.Error)(nil))
	heapCoroutine = itab((*Coroutine)(nil))
)

func newCompiler() (*compiler, error) {
	buffer, err := asm.NewBuffer(4096)
	if err != nil {
		return nil, err
	}
	return &compiler{
		arch:        arm64.New(),
		buffer:      buffer,
		scratchRegs: []asm.PReg{arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14},
	}, nil
}

// enter opens the framed callable: the external entry mirrors the
// journal header into the pinned scratch registers, then the internal head —
// the BL target for recursive trace calls — saves the link register.
func (l arm64Lowerer) enter(ctx *lowering) {
	a := ctx.assembler
	a.Entry(ctx.entry)
	a.Emit(
		arm64.MOV(ctx.scratch[scratchCtrl], arm64.X0),
		arm64.LDP(ctx.scratch[scratchStack], ctx.scratch[scratchGlobals], ctx.scratch[scratchCtrl], int16(journalStack*8)),
		arm64.LDP(ctx.scratch[scratchBP], ctx.scratch[scratchSP], ctx.scratch[scratchCtrl], int16(journalBP*8)),
	)
	vCtrl := ctx.pin(scratchCtrl)
	active := ctx.pinTo(arm64.X15)
	a.Emit(arm64.LDR(active, vCtrl, int16(journalActive*8)))
	a.Bind(ctx.head)
}

func (l arm64Lowerer) materializeExits(ctx *lowering) {
	for _, exit := range ctx.exits {
		ctx.reuseLocals = false
		ctx.spare = asm.VReg{}
		ctx.values = exit.values
		ctx.frames = exit.frames
		ctx.assembler.Bind(exit.label)
		if exit.retain > 0 {
			l.retain(ctx, exit.retain)
		}
		l.trapFlushed(ctx, trapFallback, exit.resume, exit.id)
	}
}

func (l arm64Lowerer) zero32(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(out, v, maskI32))
	return out
}

func (l arm64Lowerer) emitBlock(ctx *lowering, id int, tail []int) bool {
	if id < 0 || id >= len(ctx.blocks) {
		return false
	}
	block := ctx.blocks[id]
	if block.state != nil {
		l.enterBlock(ctx, block.state)
	}
	done, ok := l.steps(ctx, block.steps)
	if !ok {
		return false
	}
	if done {
		return true
	}
	if block.term.kind == terminateFallthrough && len(tail) > 0 {
		return l.follow(ctx, tail)
	}
	return l.term(ctx, block, tail)
}

func (l arm64Lowerer) term(ctx *lowering, block block, tail []int) bool {
	switch block.term.kind {
	case terminateFallthrough:
		return true
	case terminateBranch:
		if len(block.term.edges) != 1 {
			return false
		}
		target := block.term.edges[0]
		if block.term.hot == 0 {
			return l.next(ctx, block.anchor, target, tail, int(instr.BR))
		}
		if !l.flush(ctx, false) {
			return false
		}
		return l.path(ctx, block.anchor, target, tail, int(instr.BR))
	case terminateBranchIf:
		return l.conditional(ctx, block, tail)
	case terminateBranchTable:
		return l.table(ctx, block, tail)
	case terminateReturn:
		if len(ctx.frames) > 1 {
			if !l.stitch(ctx) {
				return false
			}
			if len(tail) > 0 {
				return l.follow(ctx, tail)
			}
			if ctx.kind == entryModule {
				return l.complete(ctx)
			}
			return l.ret(ctx)
		}
		return l.ret(ctx)
	case terminateComplete:
		return l.complete(ctx)
	case terminateFallback:
		return l.exit(ctx, block.term.ip, prof.ExitTraceCut, prof.OpcodeNone)
	default:
		return false
	}
}

func (l arm64Lowerer) conditional(ctx *lowering, block block, tail []int) bool {
	if len(block.term.edges) != 2 || ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	cond := ctx.pop()
	if block.term.hot >= 0 && block.term.hot < len(block.term.edges) {
		cold := 1 - block.term.hot
		clean := l.clean(ctx)
		if !clean && !l.flush(ctx, false) {
			return false
		}
		target := block.term.edges[cold]
		label, ok := l.label(ctx, target, join(target.tail, tail), int(instr.BR_IF))
		if !ok {
			return false
		}
		if block.term.hot == 1 {
			ctx.assembler.Emit(arm64.CBNZLabel(l.narrow32(cond.reg), label))
		} else {
			ctx.assembler.Emit(arm64.CBZLabel(l.narrow32(cond.reg), label))
		}
		return l.next(ctx, block.anchor, block.term.edges[block.term.hot], tail, int(instr.BR_IF))
	}

	if !l.flush(ctx, false) {
		return false
	}
	taken := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CBNZLabel(l.narrow32(cond.reg), taken))
	if !l.path(ctx, block.anchor, block.term.edges[1], tail, int(instr.BR_IF)) {
		return false
	}
	ctx.assembler.Bind(taken)
	return l.path(ctx, block.anchor, block.term.edges[0], tail, int(instr.BR_IF))
}

func (l arm64Lowerer) next(ctx *lowering, from anchor, target edge, tail []int, opcode int) bool {
	tail = join(target.tail, tail)
	target.tail = nil
	if target.anchor.addr == from.addr && target.anchor.ip <= from.ip {
		if !l.flush(ctx, false) {
			return false
		}
		return l.path(ctx, from, target, tail, opcode)
	}
	if target.block == noBlock {
		reason := prof.ExitColdBranch
		if ctx.kind == entryLoop {
			reason = prof.ExitLoop
		}
		return l.exit(ctx, target.anchor.ip, reason, opcode)
	}
	return l.emitBlock(ctx, target.block, tail)
}

// steps emits the ordinary operations of one normalized block. Control flow
// is owned by the block terminator and never appears here.
func (l arm64Lowerer) steps(ctx *lowering, ops []step) (bool, bool) {
	for idx := 0; idx < len(ops); idx++ {
		op := ops[idx]
		f := ctx.frame()
		if op.fn != f.addr {
			return false, false
		}
		consumed, ok := l.fuse(ctx, ops, idx)
		if !ok {
			return false, false
		}
		if consumed > 0 {
			idx += consumed - 1
			continue
		}
		ok = false
		switch op.op {
		case instr.NOP:
			ok = true
		case instr.I32_CONST:
			ok = l.i32Const(ctx, op)
		case instr.I64_CONST:
			ok = l.i64Const(ctx, op)
		case instr.F32_CONST:
			ok = l.f32Const(ctx, op)
		case instr.F64_CONST:
			ok = l.f64Const(ctx, op)
		case instr.CONST_GET:
			if op.known {
				ok = l.constGetKnown(ctx, op)
			} else {
				ok = l.constGet(ctx, op)
			}
		case instr.LOCAL_GET:
			ok = l.localGet(ctx, op)
		case instr.LOCAL_SET:
			ok = l.localSet(ctx, op, true)
		case instr.LOCAL_TEE:
			ok = l.localSet(ctx, op, false)
		case instr.GLOBAL_GET:
			ok = l.globalGet(ctx, op)
		case instr.GLOBAL_SET:
			ok = l.globalSet(ctx, op, true)
		case instr.GLOBAL_TEE:
			ok = l.globalSet(ctx, op, false)
		case instr.DROP:
			ok = l.drop(ctx, op)
		case instr.DUP:
			ok = l.dup(ctx)
		case instr.SWAP:
			ok = l.swap(ctx)
		case instr.SELECT:
			ok = l.selectOp(ctx)
		case instr.I32_ADD:
			ok = l.i32Binary(ctx, arm64.ADD)
		case instr.I32_SUB:
			ok = l.i32Binary(ctx, arm64.SUB)
		case instr.I32_MUL:
			ok = l.i32Binary(ctx, arm64.MUL)
		case instr.I32_AND:
			ok = l.i32Bitwise(ctx, arm64.AND)
		case instr.I32_OR:
			ok = l.i32Bitwise(ctx, arm64.ORR)
		case instr.I32_XOR:
			ok = l.i32Bitwise(ctx, arm64.EOR)
		case instr.I32_EQZ:
			ok = l.i32Eqz(ctx)
		case instr.I32_EQ:
			ok = l.i32Cmp(ctx, arm64.CondEQ)
		case instr.I32_NE:
			ok = l.i32Cmp(ctx, arm64.CondNE)
		case instr.I32_LT_S:
			ok = l.i32Cmp(ctx, arm64.CondLT)
		case instr.I32_LE_S:
			ok = l.i32Cmp(ctx, arm64.CondLE)
		case instr.I32_GT_S:
			ok = l.i32Cmp(ctx, arm64.CondGT)
		case instr.I32_GE_S:
			ok = l.i32Cmp(ctx, arm64.CondGE)
		case instr.I32_LT_U:
			ok = l.i32Cmp(ctx, arm64.CondCC)
		case instr.I32_LE_U:
			ok = l.i32Cmp(ctx, arm64.CondLS)
		case instr.I32_GT_U:
			ok = l.i32Cmp(ctx, arm64.CondHI)
		case instr.I32_GE_U:
			ok = l.i32Cmp(ctx, arm64.CondCS)
		case instr.I64_ADD:
			ok = l.i64Binary(ctx, op, arm64.ADD, true)
		case instr.I64_SUB:
			ok = l.i64Binary(ctx, op, arm64.SUB, true)
		case instr.I64_MUL:
			ok = l.i64Binary(ctx, op, arm64.MUL, true)
		case instr.I64_AND:
			ok = l.i64Binary(ctx, op, arm64.AND, false)
		case instr.I64_OR:
			ok = l.i64Binary(ctx, op, arm64.ORR, false)
		case instr.I64_XOR:
			ok = l.i64Binary(ctx, op, arm64.EOR, false)
		case instr.I64_EQZ:
			ok = l.i64Eqz(ctx)
		case instr.I64_EQ:
			ok = l.i64Cmp(ctx, arm64.CondEQ)
		case instr.I64_NE:
			ok = l.i64Cmp(ctx, arm64.CondNE)
		case instr.I64_LT_S:
			ok = l.i64Cmp(ctx, arm64.CondLT)
		case instr.I64_LE_S:
			ok = l.i64Cmp(ctx, arm64.CondLE)
		case instr.I64_GT_S:
			ok = l.i64Cmp(ctx, arm64.CondGT)
		case instr.I64_GE_S:
			ok = l.i64Cmp(ctx, arm64.CondGE)
		case instr.I64_LT_U:
			ok = l.i64Cmp(ctx, arm64.CondCC)
		case instr.I64_LE_U:
			ok = l.i64Cmp(ctx, arm64.CondLS)
		case instr.I64_GT_U:
			ok = l.i64Cmp(ctx, arm64.CondHI)
		case instr.I64_GE_U:
			ok = l.i64Cmp(ctx, arm64.CondCS)
		case instr.F32_ADD:
			ok = l.f32Binary(ctx, arm64.FADD)
		case instr.F32_SUB:
			ok = l.f32Binary(ctx, arm64.FSUB)
		case instr.F32_MUL:
			ok = l.f32Binary(ctx, arm64.FMUL)
		case instr.F32_DIV:
			ok = l.f32Binary(ctx, arm64.FDIV)
		case instr.F32_EQ:
			ok = l.f32Cmp(ctx, arm64.CondEQ)
		case instr.F32_NE:
			ok = l.f32Cmp(ctx, arm64.CondNE)
		case instr.F32_LT:
			ok = l.f32Cmp(ctx, arm64.CondMI)
		case instr.F32_GT:
			ok = l.f32Cmp(ctx, arm64.CondGT)
		case instr.F32_LE:
			ok = l.f32Cmp(ctx, arm64.CondLS)
		case instr.F32_GE:
			ok = l.f32Cmp(ctx, arm64.CondGE)
		case instr.F64_ADD:
			ok = l.f64Binary(ctx, arm64.FADD)
		case instr.F64_SUB:
			ok = l.f64Binary(ctx, arm64.FSUB)
		case instr.F64_MUL:
			ok = l.f64Binary(ctx, arm64.FMUL)
		case instr.F64_DIV:
			ok = l.f64Binary(ctx, arm64.FDIV)
		case instr.F64_EQ:
			ok = l.f64Cmp(ctx, arm64.CondEQ)
		case instr.F64_NE:
			ok = l.f64Cmp(ctx, arm64.CondNE)
		case instr.F64_LT:
			ok = l.f64Cmp(ctx, arm64.CondMI)
		case instr.F64_GT:
			ok = l.f64Cmp(ctx, arm64.CondGT)
		case instr.F64_LE:
			ok = l.f64Cmp(ctx, arm64.CondLS)
		case instr.F64_GE:
			ok = l.f64Cmp(ctx, arm64.CondGE)
		case instr.ARRAY_GET:
			if ctx.count() >= 2 && ctx.values[len(ctx.values)-2].raw && ctx.values[len(ctx.values)-2].ref > 0 {
				ok = l.arrayGetKnown(ctx, op)
			} else {
				ok = l.arrayGet(ctx, op)
			}
		case instr.UNREACHABLE:
			ok = l.unreachable(ctx, op)
		case instr.UPVAL_GET:
			ok = l.upvalGet(ctx, op)
		case instr.UPVAL_SET:
			ok = l.upvalSet(ctx, op)
		case instr.I32_DIV_S:
			ok = l.i32Divide(ctx, op, arm64.SDIV, l.sign32, false)
		case instr.I32_DIV_U:
			ok = l.i32Divide(ctx, op, arm64.UDIV, l.zero32, false)
		case instr.I32_REM_S:
			ok = l.i32Divide(ctx, op, arm64.SDIV, l.sign32, true)
		case instr.I32_REM_U:
			ok = l.i32Divide(ctx, op, arm64.UDIV, l.zero32, true)
		case instr.I32_SHL:
			ok = l.i32Shift(ctx, arm64.LSL, l.zero32)
		case instr.I32_SHR_S:
			ok = l.i32Shift(ctx, arm64.ASR, l.sign32)
		case instr.I32_SHR_U:
			ok = l.i32Shift(ctx, arm64.LSR, l.zero32)
		case instr.F64_REM, instr.F64_MOD:
			if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.F32_REM, instr.F32_MOD:
			if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.I32_TO_F64_S:
			ok = l.i32ToF64(ctx, l.sign32)
		case instr.I32_TO_F64_U:
			ok = l.i32ToF64(ctx, l.zero32)
		case instr.F64_TO_I32_S:
			ok = l.f64ToI32(ctx, arm64.FCVTZS)
		case instr.F64_TO_I32_U:
			ok = l.f64ToI32(ctx, arm64.FCVTZU)
		case instr.I32_TO_F32_S:
			ok = l.i32ToF32(ctx, l.sign32)
		case instr.I32_TO_F32_U:
			ok = l.i32ToF32(ctx, l.zero32)
		case instr.F32_TO_I32_S:
			ok = l.f32ToI32(ctx, arm64.FCVTZS)
		case instr.F32_TO_I32_U:
			ok = l.f32ToI32(ctx, arm64.FCVTZU)
		case instr.F32_TO_F64:
			ok = l.f32ToF64(ctx)
		case instr.F64_TO_F32:
			ok = l.f64ToF32(ctx)
		case instr.I64_DIV_S:
			ok = l.i64Divide(ctx, op, arm64.SDIV, false)
		case instr.I64_DIV_U:
			ok = l.i64Divide(ctx, op, arm64.UDIV, false)
		case instr.I64_REM_S:
			ok = l.i64Divide(ctx, op, arm64.SDIV, true)
		case instr.I64_REM_U:
			ok = l.i64Divide(ctx, op, arm64.UDIV, true)
		case instr.I64_SHL:
			ok = l.i64Shift(ctx, op, arm64.LSL, true)
		case instr.I64_SHR_S:
			ok = l.i64Shift(ctx, op, arm64.ASR, false)
		case instr.I64_SHR_U:
			ok = l.i64Shift(ctx, op, arm64.LSR, true)
		case instr.I32_TO_I64_S:
			ok = l.i32ToI64(ctx, l.sign32)
		case instr.I32_TO_I64_U:
			ok = l.i32ToI64(ctx, l.zero32)
		case instr.I64_TO_I32:
			ok = l.i64ToI32(ctx)
		case instr.I64_TO_F64_S:
			ok = l.i64ToF64(ctx, arm64.SCVTF)
		case instr.I64_TO_F64_U:
			ok = l.i64ToF64(ctx, arm64.UCVTF)
		case instr.I64_TO_F32_S:
			ok = l.i64ToF32(ctx, arm64.SCVTF)
		case instr.I64_TO_F32_U:
			ok = l.i64ToF32(ctx, arm64.UCVTF)
		case instr.F32_TO_I64_S:
			ok = l.f32ToI64(ctx, op, arm64.FCVTZS)
		case instr.F32_TO_I64_U:
			ok = l.f32ToI64(ctx, op, arm64.FCVTZU)
		case instr.F64_TO_I64_S:
			ok = l.f64ToI64(ctx, op, arm64.FCVTZS)
		case instr.F64_TO_I64_U:
			ok = l.f64ToI64(ctx, op, arm64.FCVTZU)
		case instr.I32_CLZ:
			ok = l.countZeros(ctx, types.KindI32, false)
		case instr.I32_CTZ:
			ok = l.countZeros(ctx, types.KindI32, true)
		case instr.I64_CLZ:
			ok = l.countZeros(ctx, types.KindI64, false)
		case instr.I64_CTZ:
			ok = l.countZeros(ctx, types.KindI64, true)
		case instr.I32_POPCNT:
			ok = l.popcnt(ctx, types.KindI32)
		case instr.I64_POPCNT:
			ok = l.popcnt(ctx, types.KindI64)
		case instr.I32_ROTL:
			ok = l.rotate(ctx, op, types.KindI32, true)
		case instr.I32_ROTR:
			ok = l.rotate(ctx, op, types.KindI32, false)
		case instr.I64_ROTL:
			ok = l.rotate(ctx, op, types.KindI64, true)
		case instr.I64_ROTR:
			ok = l.rotate(ctx, op, types.KindI64, false)
		case instr.I32_EXTEND8_S:
			ok = l.extend(ctx, types.KindI32, arm64.SXTB)
		case instr.I32_EXTEND16_S:
			ok = l.extend(ctx, types.KindI32, arm64.SXTH)
		case instr.I64_EXTEND8_S:
			ok = l.extend(ctx, types.KindI64, arm64.SXTB)
		case instr.I64_EXTEND16_S:
			ok = l.extend(ctx, types.KindI64, arm64.SXTH)
		case instr.I64_EXTEND32_S:
			ok = l.extend(ctx, types.KindI64, arm64.SXTW)
		case instr.I32_REINTERPRET_F32:
			ok = l.reinterpret(ctx, op, types.KindF32, types.KindI32)
		case instr.F32_REINTERPRET_I32:
			ok = l.reinterpret(ctx, op, types.KindI32, types.KindF32)
		case instr.I64_REINTERPRET_F64:
			ok = l.reinterpret(ctx, op, types.KindF64, types.KindI64)
		case instr.F64_REINTERPRET_I64:
			ok = l.reinterpret(ctx, op, types.KindI64, types.KindF64)
		case instr.F32_ABS:
			ok = l.f32Unary(ctx, arm64.FABS)
		case instr.F32_NEG:
			ok = l.f32Unary(ctx, arm64.FNEG)
		case instr.F32_SQRT:
			ok = l.f32Unary(ctx, arm64.FSQRT)
		case instr.F32_CEIL:
			ok = l.f32Unary(ctx, arm64.FRINTP)
		case instr.F32_FLOOR:
			ok = l.f32Unary(ctx, arm64.FRINTM)
		case instr.F32_TRUNC:
			ok = l.f32Unary(ctx, arm64.FRINTZ)
		case instr.F32_NEAREST:
			ok = l.f32Unary(ctx, arm64.FRINTN)
		case instr.F32_MIN:
			ok = l.f32Binary(ctx, arm64.FMIN)
		case instr.F32_MAX:
			ok = l.f32Binary(ctx, arm64.FMAX)
		case instr.F32_COPYSIGN:
			ok = l.copysign(ctx, types.KindF32)
		case instr.F64_ABS:
			ok = l.f64Unary(ctx, arm64.FABS)
		case instr.F64_NEG:
			ok = l.f64Unary(ctx, arm64.FNEG)
		case instr.F64_SQRT:
			ok = l.f64Unary(ctx, arm64.FSQRT)
		case instr.F64_CEIL:
			ok = l.f64Unary(ctx, arm64.FRINTP)
		case instr.F64_FLOOR:
			ok = l.f64Unary(ctx, arm64.FRINTM)
		case instr.F64_TRUNC:
			ok = l.f64Unary(ctx, arm64.FRINTZ)
		case instr.F64_NEAREST:
			ok = l.f64Unary(ctx, arm64.FRINTN)
		case instr.F64_MIN:
			ok = l.f64Binary(ctx, arm64.FMIN)
		case instr.F64_MAX:
			ok = l.f64Binary(ctx, arm64.FMAX)
		case instr.F64_COPYSIGN:
			ok = l.copysign(ctx, types.KindF64)
		case instr.REF_NULL:
			ok = l.refNull(ctx)
		case instr.REF_IS_NULL:
			ok = l.refIsNull(ctx, op)
		case instr.REF_EQ, instr.REF_NE:
			// REF_EQ/REF_NE consume two refs. Releasing both natively risks a
			// double release if the second release deopts after the first already
			// decremented a refcount inline; the interpreter releases both safely.
			ok = false
		case instr.REF_GET:
			ok = l.refGet(ctx, op)
		case instr.ARRAY_LEN:
			ok = l.arrayLen(ctx, op)
		case instr.ARRAY_SET:
			terminal := op.terminal || ctx.count() > 0 && ctx.values[len(ctx.values)-1].kind == types.KindRef
			if !l.arraySet(ctx, op) {
				return false, false
			}
			if terminal {
				return true, idx == len(ops)-1
			}
			ok = true
		case instr.STRUCT_GET:
			if !l.structGet(ctx, op) {
				return false, false
			}
			ok = true
		case instr.STRUCT_SET:
			if !l.structSet(ctx, op) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.ERROR_GET:
			ok = l.errorGet(ctx, op)
		case instr.CORO_DONE:
			ok = l.coroDone(ctx, op)
		case instr.CORO_VALUE:
			ok = l.coroValue(ctx, op)
		case instr.STRING_LEN:
			ok = l.stringLen(ctx, op)
		// STRING_EQ/STRING_NE stay threaded like REF_EQ/REF_NE above: they
		// release two refs, and a deopt after the first inline decrement would
		// double-release. REF_SET stays threaded because it needs a fresh
		// interface box (an allocation); storing in place is unsound against
		// shared static boxes. REF_TEST/REF_CAST stay threaded because they
		// need structural type equality that an itab guard cannot express.
		// MAP_* stay threaded because they reach into Go map internals the
		// lowerer has no native access to.
		case instr.STRING_ENCODE_UTF32,
			instr.STRING_ITER,
			instr.MAP_LEN,
			instr.MAP_GET,
			instr.MAP_LOOKUP,
			instr.MAP_KEYS,
			instr.MAP_ITER:
			if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.ERROR_NEW, instr.ERROR_CODE, instr.THROW:
			// Allocation and handler landing stay interpreter-owned. Resume at
			// op.ip because each threaded handler performs its own IP update or
			// handler transfer.
			if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.YIELD, instr.RESUME:
			// True suspension points: deopt to the threaded handler, which runs
			// the real suspend/resume. Resume at op.ip (not op.ip+1) because the
			// YIELD and RESUME handlers perform their own ip advance.
			if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.CALL:
			if op.known {
				ok = l.directCall(ctx, op)
			} else {
				ok = l.call(ctx, op)
			}
		case instr.RETURN_CALL:
			// A tail call back to the trace anchor closes the loop with a native
			// back-edge (terminal); a tail call to another function morphs the
			// current frame into the callee in place and keeps walking.
			if op.callee == ctx.addr {
				if !l.tailLoop(ctx, op) {
					return false, false
				}
				return true, idx == len(ops)-1
			}
			ok = l.tailMorph(ctx, op)
		case instr.RETURN:
			if len(ctx.frames) > 1 {
				ok = l.stitch(ctx)
				break
			}
			if !l.ret(ctx) {
				return false, false
			}
			return true, idx == len(ops)-1
		}
		if !ok {
			return false, false
		}
	}
	return false, true
}

// fuse lowers short sequences whose intermediate ref ownership can be
// eliminated safely. It returns the number of steps lowered; a miss leaves
// standalone lowering untouched, while false rejects native compilation.
func (l arm64Lowerer) fuse(ctx *lowering, ops []step, idx int) (int, bool) {
	if idx+1 >= len(ops) || !l.adjacent(ops[idx], ops[idx+1]) {
		return 0, true
	}
	source := ops[idx]
	consumer := ops[idx+1]
	if l.target(ctx, source, consumer) {
		return 1, true
	}
	return l.consume(ctx, ops, idx)
}

// target replaces a constant function load with a raw call marker. CALL and
// RETURN_CALL remain in steps, which owns frame-changing operations.
func (l arm64Lowerer) target(ctx *lowering, source, consumer step) bool {
	if source.op != instr.CONST_GET || (consumer.op != instr.CALL && consumer.op != instr.RETURN_CALL) {
		return false
	}
	constant := int(source.args[0])
	if constant >= len(ctx.constants) || ctx.constants[constant].Kind() != types.KindRef {
		return false
	}
	ref := ctx.constants[constant].Ref()
	if ref < 0 || ref >= len(ctx.heap) {
		return false
	}
	callee := ref
	switch fn := ctx.heap[ref].(type) {
	case *types.Closure:
		callee = int(fn.Fn)
	case *types.Function:
	default:
		return false
	}
	if callee != consumer.callee || resolve(ctx.module, ctx.heap, callee) == nil {
		return false
	}
	ctx.push(value{fn: callee, kind: types.KindRef, raw: true, ref: ref})
	return true
}

func (l arm64Lowerer) consume(ctx *lowering, ops []step, idx int) (int, bool) {
	source := ops[idx]
	consumer := ops[idx+1]
	if consumer.op != instr.DROP && consumer.op != instr.REF_IS_NULL {
		return 0, true
	}
	frame := ctx.frame()
	const consumed = 2

	var reg asm.VReg
	drop := consumer.op == instr.DROP
	switch source.op {
	case instr.LOCAL_GET:
		slot := int(source.args[0])
		if slot >= len(frame.kinds) || frame.kinds[slot] != types.KindRef {
			return 0, true
		}
		if drop {
			return consumed, true
		}
		if !l.loadLocal(ctx, frame, slot, source.ip) {
			return 0, false
		}
		reg = frame.locals[slot].reg
	case instr.GLOBAL_GET:
		slot := int(source.args[0])
		if slot >= len(ctx.globals) || slot > 4095 || ctx.globals[slot] != types.KindRef {
			return 0, true
		}
		if drop {
			return consumed, true
		}
		reg = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(reg, ctx.pin(scratchGlobals), int16(slot*8)))
	case instr.UPVAL_GET:
		slot := int(source.args[0])
		if slot >= len(frame.upvals) || frame.upvals[slot] != types.KindRef {
			return 0, true
		}
		if drop {
			return consumed, true
		}
		reg = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(reg, l.upvalBase(ctx), int16(slot*8)))
	case instr.CONST_GET:
		constant := int(source.args[0])
		if constant >= len(ctx.constants) || ctx.constants[constant].Kind() != types.KindRef {
			return 0, true
		}
		boxed := ctx.constants[constant]
		ref := boxed.Ref()
		if ref < 0 || ref >= len(ctx.heap) {
			return 0, true
		}
		if _, ok := ctx.heap[ref].(types.String); ok {
			return 0, true
		}
		if drop {
			return consumed, true
		}
		reg = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(reg, uint64(boxed))...)
	case instr.REF_NULL:
		if drop {
			return consumed, true
		}
		reg = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(reg, uint64(types.BoxedNull))...)
	case instr.DUP:
		if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
			return 0, true
		}
		if drop {
			return consumed, true
		}
		var ok bool
		reg, ok = l.box(ctx, ctx.values[len(ctx.values)-1])
		if !ok {
			return 0, false
		}
	default:
		return 0, true
	}

	null := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(null, uint64(types.BoxedNull))...)
	ctx.assembler.Emit(arm64.CMP(reg, null), arm64.CSET(result, arm64.CondEQ))
	ctx.push(value{reg: result, kind: types.KindI1, raw: true})
	return consumed, true
}

func (l arm64Lowerer) adjacent(a, b step) bool {
	if a.fn != b.fn || a.depth != b.depth {
		return false
	}
	width := 1
	for _, operand := range instr.TypeOf(a.op).Widths {
		if operand < 0 {
			return false
		}
		width += operand
	}
	return b.ip == a.ip+width
}

func (l arm64Lowerer) i32Const(ctx *lowering, op step) bool {
	val := uint32(op.args[0])
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(dst, uint64(val))...)
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true, known: true, imm: int64(int32(val))})
	return true
}

func (l arm64Lowerer) i64Const(ctx *lowering, op step) bool {
	val := int64(op.args[0])
	if !types.IsBoxable(val) {
		return false
	}
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(dst, uint64(val))...)
	ctx.push(value{reg: dst, kind: types.KindI64, raw: true, known: true, imm: val})
	return true
}

func (l arm64Lowerer) f32Const(ctx *lowering, op step) bool {
	bits := uint32(op.args[0])
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(dst, uint64(bits))...)
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

func (l arm64Lowerer) f64Const(ctx *lowering, op step) bool {
	bits := op.args[0]
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(dst, bits)...)
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l arm64Lowerer) unreachable(ctx *lowering, op step) bool {
	return l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op))
}

func (l arm64Lowerer) localGet(ctx *lowering, op step) bool {
	f := ctx.frame()
	idx := int(op.args[0])
	if idx >= len(f.kinds) {
		return false
	}
	if f.kinds[idx] == types.KindRef {
		f.loaded[idx] = false
	}
	if !l.loadLocal(ctx, f, idx, op.ip) {
		return false
	}
	if f.locals[idx].kind == types.KindRef {
		if ctx.leaf {
			l.retainKnownBox(ctx, f.locals[idx].reg)
		} else {
			l.retainBox(ctx, f.locals[idx].reg)
		}
	}
	ctx.push(f.locals[idx])
	return true
}

func (l arm64Lowerer) localSet(ctx *lowering, op step, pop bool) bool {
	f := ctx.frame()
	idx := int(op.args[0])
	if idx >= len(f.kinds) || ctx.count() < 1 {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind.Repr() != f.kinds[idx].Repr() {
		return false
	}
	if v.kind == types.KindRef {
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		pre := ctx.pre()
		vStack := ctx.pin(scratchStack)
		addr := l.base(ctx, vStack)
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, addr, int16((f.base+idx)*8)))
		// Release the overwritten ref first: it is the only deopt point, so no
		// refcount is mutated before it. TEE then retains the stored ref because
		// it keeps the stack copy alongside the slot; the retain runs only on the
		// non-deopt path, so a re-run in the interpreter cannot double-apply it.
		// SET transfers the stack's reference and skips the retain.
		l.releaseBoxUnlessEqual(ctx, old, boxed, pre, op.ip)
		if !pop {
			l.retainBoxUnlessEqual(ctx, old, boxed)
		}
		ctx.assembler.Emit(arm64.STR(boxed, addr, int16((f.base+idx)*8)))
		f.locals[idx] = value{reg: boxed, kind: types.KindRef, raw: false}
		f.loaded[idx] = true
		if pop {
			ctx.pop()
		}
		return true
	}
	if !v.raw {
		return false
	}
	f.locals[idx] = v
	f.loaded[idx] = true
	f.dirty[idx] = true
	if pop {
		ctx.pop()
	}
	return true
}

// globalGet loads a global directly from the globals base. Scalars push
// raw; refs stay boxed and retain the stack ownership created by the get.
func (l arm64Lowerer) globalGet(ctx *lowering, op step) bool {
	idx, kind, ok := l.global(ctx, op)
	if !ok {
		return false
	}
	base := ctx.pin(scratchGlobals)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, base, int16(idx*8)))
	if kind == types.KindI64 {
		if !l.guardI64(ctx, dst, op.ip) {
			return false
		}
		dst = l.sign64(ctx, dst)
	}
	if kind == types.KindRef {
		l.retainBox(ctx, dst)
		ctx.push(value{reg: dst, kind: kind, raw: false})
		return true
	}
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// globalSet boxes the top value and stores it to the global. Ref-capable
// slots release the overwritten runtime ref before the store.
func (l arm64Lowerer) globalSet(ctx *lowering, op step, pop bool) bool {
	idx, kind, ok := l.global(ctx, op)
	if !ok {
		return false
	}
	if ctx.count() < 1 {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != kind || !v.raw {
		if v.kind != types.KindRef || kind != types.KindRef {
			return false
		}
	}
	boxed, ok := l.box(ctx, v)
	if !ok {
		return false
	}
	base := ctx.pin(scratchGlobals)
	if kind == types.KindRef || kind == types.KindI64 {
		pre := ctx.pre()
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, base, int16(idx*8)))
		// Release the overwritten ref first: it is the only deopt point, so no
		// refcount is mutated before it. TEE then retains the stored ref because
		// it keeps the stack copy alongside the slot; the retain runs only on the
		// non-deopt path, so a re-run in the interpreter cannot double-apply it.
		// SET transfers the stack's reference and skips the retain.
		l.releaseBoxUnlessEqual(ctx, old, boxed, pre, op.ip)
		if !pop {
			l.retainBoxUnlessEqual(ctx, old, boxed)
		}
	}
	ctx.assembler.Emit(arm64.STR(boxed, base, int16(idx*8)))
	if pop {
		ctx.pop()
	}
	return true
}

// global decodes the global index and returns its statically observed kind.
// The lowering carries the global kinds (mirroring how Locals use declared
// LocalKinds), so GLOBAL_GET/SET see a stable kind at lower time: a per-run
// input is seeded via SetGlobal before Run, so the entry trace already observes
// it. Out-of-range indices and offsets past the 12-bit LDR/STR limit reject.
func (l arm64Lowerer) global(ctx *lowering, op step) (int, types.Kind, bool) {
	idx := int(op.args[0])
	if idx >= len(ctx.globals) || idx > 4095 {
		return 0, 0, false
	}
	kind := ctx.globals[idx]
	switch kind {
	case types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
		return idx, kind, true
	}
	return 0, 0, false
}

func (l arm64Lowerer) drop(ctx *lowering, op step) bool {
	if ctx.count() < 1 {
		return false
	}
	pre := ctx.pre()
	v := ctx.values[len(ctx.values)-1]
	if v.kind == types.KindRef {
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		l.releaseBox(ctx, boxed, pre, op.ip)
	}
	ctx.pop()
	return true
}

func (l arm64Lowerer) dup(ctx *lowering) bool {
	if ctx.count() < 1 {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind == types.KindRef {
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		l.retainBox(ctx, boxed)
		v = value{reg: boxed, kind: types.KindRef, raw: false}
	}
	ctx.push(v)
	return true
}

func (l arm64Lowerer) swap(ctx *lowering) bool {
	if ctx.count() < 2 {
		return false
	}
	last := len(ctx.values) - 1
	ctx.values[last], ctx.values[last-1] = ctx.values[last-1], ctx.values[last]
	return true
}

func (l arm64Lowerer) selectOp(ctx *lowering) bool {
	if ctx.count() < 3 {
		return false
	}
	cond := ctx.pop()
	v2 := ctx.pop()
	v1 := ctx.pop()
	if cond.kind.Repr() != types.KindI32 || v1.kind != v2.kind || v1.kind == types.KindRef {
		return false
	}
	ctx.assembler.Emit(arm64.CMPI(l.narrow32(cond.reg), 0))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSEL(dst, v1.reg, v2.reg, arm64.CondNE))
	ctx.push(value{reg: dst, kind: v1.kind, raw: true})
	return true
}

// clean reports whether a branch can skip the hot-path flush: no live operand
// or dirty local will be reloaded from VM stack homes later in the trace.
func (l arm64Lowerer) clean(ctx *lowering) bool {
	if ctx.count() != 0 {
		return false
	}
	for fi := range ctx.frames {
		f := &ctx.frames[fi]
		for idx := range f.dirty {
			if f.dirty[idx] {
				return false
			}
		}
	}
	return true
}

func (l arm64Lowerer) constGetKnown(ctx *lowering, op step) bool {
	idx := int(op.args[0])
	if idx >= len(ctx.constants) {
		return false
	}
	boxed := ctx.constants[idx]
	if boxed.Kind() != types.KindRef {
		return l.constGet(ctx, op)
	}
	ref := boxed.Ref()
	if ref <= 0 || ref >= len(ctx.heap) {
		return false
	}
	switch ctx.heap[ref].(type) {
	case types.TypedArray[bool], types.TypedArray[int8], types.TypedArray[int32],
		types.TypedArray[float32], types.TypedArray[float64]:
		ctx.push(value{kind: types.KindRef, raw: true, ref: ref})
		return true
	default:
		return l.constGet(ctx, op)
	}
}

// constGet pushes a scalar constant as an unboxed immediate. Refs retain
// ordinary standalone ownership; call-target fusion owns direct markers.
func (l arm64Lowerer) constGet(ctx *lowering, op step) bool {
	idx := int(op.args[0])
	if idx >= len(ctx.constants) {
		return false
	}
	v := ctx.constants[idx]
	switch v.Kind() {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64:
		dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		val := value{reg: dst, kind: v.Kind(), raw: true}
		if v.Kind() == types.KindI64 {
			ctx.assembler.Emit(arm64.LDI(dst, uint64(v.I64()))...)
			val.known = true
			val.imm = v.I64()
		} else {
			ctx.assembler.Emit(arm64.LDI(dst, uint64(v))...)
			if v.Kind().Repr() == types.KindI32 {
				val.known = true
				val.imm = int64(v.I32())
			}
		}
		ctx.push(val)
		return true
	case types.KindRef:
		ref := v.Ref()
		if ref < 0 || ref >= len(ctx.heap) {
			return false
		}
		boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(boxed, uint64(v))...)
		l.retain(ctx, ref)
		ctx.push(value{reg: boxed, kind: types.KindRef, raw: false, ref: ref})
		return true
	}
	return false
}

func (l arm64Lowerer) arrayGetKnown(ctx *lowering, op step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 {
		return false
	}
	marker := ctx.values[len(ctx.values)-2]
	constant := marker.ref
	if !marker.raw || constant <= 0 || constant >= len(ctx.heap) {
		return false
	}

	var kind types.Kind
	var want uintptr
	var scale uint8
	switch value := ctx.heap[constant].(type) {
	case types.TypedArray[bool]:
		kind, want = types.KindI1, itab(value)
	case types.TypedArray[int8]:
		kind, want = types.KindI8, itab(value)
	case types.TypedArray[int32]:
		kind, want, scale = types.KindI32, itab(value), 2
	case types.TypedArray[float32]:
		kind, want, scale = types.KindF32, itab(value), 2
	case types.TypedArray[float64]:
		kind, want, scale = types.KindF64, itab(value), 3
	default:
		return false
	}

	pre := append([]value(nil), ctx.values...)
	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(boxed, uint64(types.BoxRef(constant)))...)
	ctx.values[len(ctx.values)-2] = value{reg: boxed, kind: types.KindRef, raw: false, ref: constant}
	if !l.flush(ctx, false) {
		return false
	}
	clear(ctx.frame().loaded)
	clear(ctx.frame().dirty)
	fail := ctx.queueExit(nil, op.ip, constant, prof.ExitGuardValue, int(op.op))

	a := ctx.assembler
	heap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journalHeap*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(off, uint64(constant))...)
	a.Emit(arm64.LSLI(off, off, 4))
	cell := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(cell, heap, off))
	actual := a.Reg(asm.RegTypeInt, asm.Width64)
	data := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(actual, cell, 0), arm64.LDR(data, cell, 8))
	l.guardItab(ctx, actual, want, fail)

	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	dataPtr, n := l.sliceHeader(ctx, data, 0)
	l.guardIndex(ctx, idx, n, fail)
	result := a.Reg(asm.RegTypeInt, asm.Width64)
	switch kind {
	case types.KindI1:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(result, elemAddr, 0))
	case types.KindI8:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		elem := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(elem, elemAddr, 0))
		a.Emit(arm64.SXTB(result, elem))
	case types.KindI32, types.KindF32:
		elemOff := a.Reg(asm.RegTypeInt, asm.Width64)
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(elemOff, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, elemOff))
		a.Emit(arm64.LDRSW(result, elemAddr, 0))
	case types.KindF64:
		elemOff := a.Reg(asm.RegTypeInt, asm.Width64)
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(elemOff, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, elemOff))
		a.Emit(arm64.LDR(result, elemAddr, 0))
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: kind, raw: true})
	return true
}

func (l arm64Lowerer) enterBlock(ctx *lowering, state []slot) {
	ctx.reuseLocals = false
	ctx.spare = asm.VReg{}
	ctx.values = ctx.values[:0]
	for _, slot := range state {
		ctx.values = append(ctx.values, value{kind: slot.kind, ref: slot.ref, raw: slot.refKnown})
	}
	frame := ctx.frame()
	clear(frame.loaded)
	clear(frame.dirty)
	l.reload(ctx)
}

func (l arm64Lowerer) table(ctx *lowering, block block, tail []int) bool {
	if len(block.term.edges) == 0 || len(block.term.edges)-1 > branchTableLimit || ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	cond := ctx.pop()
	value := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(value, cond.reg, maskI32))
	if !l.flush(ctx, false) {
		return false
	}
	labels := make([]asm.Label, len(block.term.edges))
	for idx := range labels {
		labels[idx] = ctx.assembler.Label()
	}
	for idx := 0; idx < len(labels)-1; idx++ {
		ctx.assembler.Emit(arm64.CMPI(value, uint16(idx)))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, labels[idx]))
	}
	ctx.assembler.Emit(arm64.BLabel(labels[len(labels)-1]))
	for idx, label := range labels {
		ctx.assembler.Bind(label)
		if !l.path(ctx, block.anchor, block.term.edges[idx], tail, int(instr.BR_TABLE)) {
			return false
		}
	}
	return true
}

func (l arm64Lowerer) follow(ctx *lowering, tail []int) bool {
	if len(tail) == 0 {
		return true
	}
	if !l.flush(ctx, false) {
		return false
	}
	id := tail[0]
	if id < 0 || id >= len(ctx.blocks) || !ctx.blocks[id].tail {
		return false
	}
	label := ctx.assembler.Label()
	work := work{label: label, block: id, tail: tail[1:]}
	work.values, work.frames = ctx.snapshot()
	ctx.work = append(ctx.work, work)
	ctx.assembler.Emit(arm64.BLabel(label))
	return true
}

func (l arm64Lowerer) path(ctx *lowering, from anchor, target edge, tail []int, opcode int) bool {
	tail = join(target.tail, tail)
	target.tail = nil
	label, ok := l.label(ctx, target, tail, opcode)
	if !ok {
		return false
	}
	if target.anchor.addr == from.addr && target.anchor.ip <= from.ip {
		a := ctx.assembler
		vCtrl := ctx.pin(scratchCtrl)
		budget := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDR(budget, vCtrl, int16(journalBudget*8)))
		a.Emit(arm64.SUBI(budget, budget, 1))
		a.Emit(arm64.STR(budget, vCtrl, int16(journalBudget*8)))
		a.Emit(arm64.CBNZLabel(budget, label))
		l.trapFlushed(ctx, trapYield, target.anchor.ip, -1)
		return true
	}
	ctx.assembler.Emit(arm64.BLabel(label))
	return true
}

func (l arm64Lowerer) label(ctx *lowering, target edge, tail []int, opcode int) (asm.Label, bool) {
	if target.block == noBlock {
		reason := prof.ExitColdBranch
		if ctx.kind == entryLoop {
			reason = prof.ExitLoop
		}
		return ctx.queueExit(nil, target.anchor.ip, 0, reason, opcode), true
	}
	if target.block < 0 || target.block >= len(ctx.blocks) {
		return 0, false
	}
	block := ctx.blocks[target.block]
	if block.state != nil {
		return ctx.labels[target.block], true
	}
	if l.marked(ctx) || ctx.scheduled >= continuationLimit {
		return ctx.queueExit(nil, target.anchor.ip, 0, prof.ExitColdBranch, opcode), true
	}
	label := ctx.assembler.Label()
	work := work{label: label, block: target.block, tail: tail}
	work.values, work.frames = ctx.snapshot()
	ctx.work = append(ctx.work, work)
	ctx.scheduled++
	return label, true
}

// marked reports whether a raw ref marker blocks deferred continuation reload.
func (l arm64Lowerer) marked(ctx *lowering) bool {
	for _, v := range ctx.values {
		if v.kind == types.KindRef && v.raw {
			return true
		}
	}
	return false
}

func (l arm64Lowerer) directCall(ctx *lowering, op step) bool {
	target := resolve(ctx.module, ctx.heap, op.callee)
	if op.callee == ctx.addr || target == nil || target.Typ == nil || ctx.count() < 1 {
		return false
	}
	params := len(target.Typ.Params)
	if ctx.count() < params+1 {
		return false
	}
	for _, typ := range target.Typ.Returns {
		switch typ.Kind() {
		case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64:
		default:
			return false
		}
	}

	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	natives := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(natives, vCtrl, int16(journalNatives*8)))
	callee := a.Reg(asm.RegTypeInt, asm.Width64)
	if op.callee > 4095 {
		return false
	}
	a.Emit(arm64.LDR(callee, natives, int16(op.callee*8)))
	ready := a.Label()
	a.Emit(arm64.CBNZLabel(callee, ready))
	if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
		return false
	}
	a.Bind(ready)

	marker := ctx.pop()
	if marker.fn != op.callee || ctx.count() < params || !l.args(ctx, target, params) {
		return false
	}
	if !l.flush(ctx, false) {
		return false
	}

	active := ctx.pinTo(arm64.X15)
	limit := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(limit, vCtrl, int16(journalCap*8)))
	a.Emit(arm64.CMP(active, limit))
	hasFrame := a.Label()
	a.Emit(arm64.BCondLabel(arm64.OpBCC, hasFrame))
	l.overflow(ctx, op)
	a.Bind(hasFrame)
	a.Emit(arm64.ADDI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journalActive*8)))

	vBP := ctx.pin(scratchBP)
	nextSP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(nextSP, vBP, uint16(ctx.sp())))
	oldBP := ctx.scratch[scratchBP]
	oldSP := ctx.scratch[scratchSP]
	a.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, 32),
		arm64.STP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(arm64.LR, arm64.SP, 16),
	)
	calleeBP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.SUBI(calleeBP, nextSP, uint16(params)))
	a.Emit(arm64.MOV(ctx.pinTo(oldBP), calleeBP))

	localKinds := target.LocalKinds()
	if len(localKinds) > params {
		stack := ctx.pin(scratchStack)
		base := l.base(ctx, stack)
		for idx := params; idx < len(localKinds); idx++ {
			zero, ok := zeroValue(localKinds[idx])
			if !ok {
				return false
			}
			reg := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LDI(reg, uint64(zero))...)
			a.Emit(arm64.STR(reg, base, int16((ctx.sp()-params+idx)*8)))
		}
	}
	calleeSP := calleeBP
	if len(localKinds) > 0 {
		calleeSP = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(calleeSP, calleeBP, uint16(len(localKinds))))
	}
	a.Emit(arm64.MOV(ctx.pinTo(oldSP), calleeSP))
	a.Emit(arm64.MOV(arm64.X0, vCtrl))
	a.Emit(arm64.BLR(callee))

	vCtrl = ctx.pin(scratchCtrl)
	trap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(trap, vCtrl, int16(journalTrap*8)))
	normal := a.Label()
	a.Emit(arm64.CBZLabel(trap, normal), arm64.LDR(oldBP, arm64.SP, 0))
	l.unwind(ctx, vCtrl, op.ip+1)
	a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
		arm64.RET(),
	)
	a.Bind(normal)

	active = ctx.pinTo(arm64.X15)
	a.Emit(arm64.SUBI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journalActive*8)))
	a.Emit(
		arm64.LDP(oldBP, oldSP, arm64.SP, 0),
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
	)

	rets := target.Typ.Returns
	regs := make([]asm.VReg, len(rets))
	for idx := range rets {
		if idx >= len(arm64.IntRets) {
			return false
		}
		regs[idx] = ctx.pinTo(arm64.IntRets[idx])
	}
	ctx.values = ctx.values[:len(ctx.values)-params]
	for fi := range ctx.frames {
		clear(ctx.frames[fi].loaded)
		clear(ctx.frames[fi].dirty)
	}
	l.reload(ctx)
	for idx, typ := range rets {
		ctx.push(value{reg: regs[idx], kind: typ.Kind(), raw: true})
	}
	return true
}

func (l arm64Lowerer) i32Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(dst, a.reg, b.reg))
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

// i32Bitwise lowers a width-closed bitwise op (and/or/xor). Operands are
// accepted by representation, so i1/i8 flow in like i32; the result keeps a
// shared narrow kind (i8&i8 → i8, i1^i1 → i1) and widens to i32 only for a
// mixed pair. The op runs on the full register; the low 32 bits carry the value
// and box masks the rest.
func (l arm64Lowerer) i32Bitwise(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(dst, a.reg, b.reg))
	ctx.push(value{reg: dst, kind: a.kind & b.kind, raw: true})
	return true
}

func (l arm64Lowerer) i32Divide(
	ctx *lowering,
	op step,
	div func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*lowering, asm.VReg) asm.VReg,
	rem bool,
) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI32, 2) {
		return false
	}
	b := prep(ctx, ctx.values[len(ctx.values)-1].reg)
	a := prep(ctx, ctx.values[len(ctx.values)-2].reg)

	top := ctx.values[len(ctx.values)-1]
	observed := uint64(0)
	if op.arg.Kind().Repr() == types.KindI32 {
		observed = uint64(uint32(op.arg.I32()))
	}
	if !l.guardDivisor(ctx, top, l.narrow32(b), observed, op.ip) {
		return false
	}

	ctx.pop()
	ctx.pop()
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(div(dst, a, b))
	if rem {
		quotient := dst
		dst = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.MSUB(dst, quotient, b, a))
	}
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) i32Shift(
	ctx *lowering,
	shiftOp func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*lowering, asm.VReg) asm.VReg,
) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI32, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1]
	a := ctx.values[len(ctx.values)-2]
	shift := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if b.known {
		ctx.assembler.Emit(arm64.LDI(shift, uint64(uint32(b.imm)&0x1F))...)
	} else {
		ctx.assembler.Emit(arm64.ANDI(shift, b.reg, 0x1F))
	}
	val := prep(ctx, a.reg)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(shiftOp(dst, val, shift))
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) i32Eqz(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	ctx.assembler.Emit(arm64.CMPI(l.narrow32(a.reg), 0))
	l.setBool(ctx, arm64.CondEQ)
	return true
}

// i32Cmp compares the 32-bit lanes through W-register views: raw upper
// bits never participate, so signed and unsigned conditions both read correct
// flags from the 32-bit subtraction.
func (l arm64Lowerer) i32Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindI32)
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMP(l.narrow32(a.reg), l.narrow32(b.reg)))
	l.setBool(ctx, cond)
	return true
}

func (l arm64Lowerer) f64Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindF64)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, a.reg),
		arm64.FMOV(fb, b.reg),
		op(fr, fa, fb),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l arm64Lowerer) f64Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindF64)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, a.reg),
		arm64.FMOV(fb, b.reg),
		arm64.FCMP(fa, fb),
	)
	l.setBool(ctx, cond)
	return true
}

func (l arm64Lowerer) i32ToF64(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	val := prep(ctx, a.reg)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.SCVTF(fr, val))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l arm64Lowerer) f64ToI32(ctx *lowering, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(fa, a.reg))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(dst, fa))
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

// f32Binary lowers an f32 arithmetic opcode. A raw f32 keeps its float
// bits in the low 32 of an int register, so both inputs unbox with a 32-bit
// FMOV and the result moves back untagged — box tags it at a boundary.
func (l arm64Lowerer) f32Binary(ctx *lowering, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	b, a, ok := l.operands(ctx, types.KindF32)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		arm64.FMOV(fb, l.narrow32(b.reg)),
		op(fr, fa, fb),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

func (l arm64Lowerer) f32Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindF32)
	if !ok {
		return false
	}
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fb := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		arm64.FMOV(fb, l.narrow32(b.reg)),
		arm64.FCMP(fa, fb),
	)
	l.setBool(ctx, cond)
	return true
}

// i32ToF32 converts a raw i32 to a raw f32. prep sign- or zero-extends
// the value lane; SCVTF over the extended 64-bit value is correct for both
// signed and (non-negative, zero-extended) unsigned sources.
func (l arm64Lowerer) i32ToF32(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	val := prep(ctx, a.reg)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.SCVTF(fr, val))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

func (l arm64Lowerer) f32ToI32(ctx *lowering, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.FMOV(fa, l.narrow32(a.reg)))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(dst, fa))
	ctx.push(value{reg: dst, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) f32ToF64(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fd := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		arm64.FCVT(fd, fa),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fd))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l arm64Lowerer) f64ToF32(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.pop()
	fd := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fs := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fd, a.reg),
		arm64.FCVT(fs, fd),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fs))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

// i64Binary lowers an i64 arithmetic opcode. Raw i64 is the full signed
// value, so the op runs directly on 64-bit registers; checked ops guard that
// the result still fits the boxable range and deopt with the operands intact
// when it overflows, so the interpreter handles the heap promotion.
func (l arm64Lowerer) i64Binary(ctx *lowering, op step, opfn func(dst, src1, src2 asm.Reg) asm.Instruction, checked bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(opfn(raw, a, b))
	if checked && !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64Divide(ctx *lowering, op step, div func(dst, src1, src2 asm.Reg) asm.Instruction, rem bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg

	top := ctx.values[len(ctx.values)-1]
	observed := uint64(0)
	if op.arg.Kind() == types.KindI64 {
		observed = uint64(op.arg.I64())
	}
	if !l.guardDivisor(ctx, top, b, observed, op.ip) {
		return false
	}

	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(div(raw, a, b))
	if rem {
		quotient := raw
		raw = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.MSUB(raw, quotient, b, a))
	}
	if !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64Shift(ctx *lowering, op step, opfn func(dst, src1, src2 asm.Reg) asm.Instruction, checked bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, types.KindI64, 2) {
		return false
	}
	b := ctx.values[len(ctx.values)-1].reg
	a := ctx.values[len(ctx.values)-2].reg
	shift := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if ctx.values[len(ctx.values)-1].known {
		ctx.assembler.Emit(arm64.LDI(shift, uint64(ctx.values[len(ctx.values)-1].imm)&0x3F)...)
	} else {
		ctx.assembler.Emit(arm64.ANDI(shift, b, 0x3F))
	}
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(opfn(raw, a, shift))
	if checked && !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64Cmp(ctx *lowering, cond uint8) bool {
	b, a, ok := l.operands(ctx, types.KindI64)
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMP(a.reg, b.reg))
	l.setBool(ctx, cond)
	return true
}

// operands pops a typed binary-op pair after checking both kinds.
func (l arm64Lowerer) operands(ctx *lowering, kind types.Kind) (value, value, bool) {
	if ctx.count() < 2 || !l.kinds(ctx, kind, 2) {
		return value{}, value{}, false
	}
	b := ctx.pop()
	a := ctx.pop()
	return b, a, true
}

func (l arm64Lowerer) i64Eqz(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	ctx.assembler.Emit(arm64.CMPI(a.reg, 0))
	l.setBool(ctx, arm64.CondEQ)
	return true
}

// setBool pushes a comparison/test result as i1: every caller is an
// eqz/eq/lt/.../is_null/test whose result kind is i1 (matching the interpreter,
// which boxes these through BoxI1). The 0/1 flag still satisfies any later
// i32 operand because kinds compares by representation.
func (l arm64Lowerer) setBool(ctx *lowering, cond uint8) {
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))
	ctx.push(value{reg: flag, kind: types.KindI1, raw: true})
}

// i32ToI64 widens a raw i32 to a raw i64; the i32 range is within the
// boxable i64 range so no guard is needed.
func (l arm64Lowerer) i32ToI64(ctx *lowering, prep func(*lowering, asm.VReg) asm.VReg) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	a := ctx.pop()
	ext := prep(ctx, a.reg)
	ctx.push(value{reg: ext, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) i64ToI32(ctx *lowering) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, a.reg, maskI32))
	ctx.push(value{reg: lo, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) i64ToF64(ctx *lowering, cvtf func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(cvtf(fr, a.reg))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

func (l arm64Lowerer) i64ToF32(ctx *lowering, cvtf func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI64, 1) {
		return false
	}
	a := ctx.pop()
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(cvtf(fr, a.reg))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

func (l arm64Lowerer) f32ToI64(ctx *lowering, op step, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.values[len(ctx.values)-1]
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.FMOV(fa, l.narrow32(a.reg)))
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(raw, fa))
	if !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

func (l arm64Lowerer) f64ToI64(ctx *lowering, op step, cvt func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.values[len(ctx.values)-1]
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(fa, a.reg))
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(cvt(raw, fa))
	if !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.push(value{reg: raw, kind: types.KindI64, raw: true})
	return true
}

// countZeros lowers CLZ (reverse=false) or CTZ (reverse=true, via RBIT then
// CLZ) for an integer kind. The count is always in [0, width] so the result is
// boxable without a guard. i32 operates on the W view so the upper lane is
// ignored.
func (l arm64Lowerer) countZeros(ctx *lowering, kind types.Kind, reverse bool) bool {
	if ctx.count() < 1 || !l.kinds(ctx, kind, 1) {
		return false
	}
	a := ctx.pop()
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	src, out := a.reg, dst
	if kind == types.KindI32 {
		src, out = l.narrow32(a.reg), l.narrow32(dst)
	}
	if reverse {
		ctx.assembler.Emit(arm64.RBIT(out, src))
		ctx.assembler.Emit(arm64.CLZ(out, out))
	} else {
		ctx.assembler.Emit(arm64.CLZ(out, src))
	}
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// popcnt lowers a population count through the SIMD pipe (FMOV → CNT → ADDV →
// FMOV); ARMv8.0 has no scalar GPR popcount. The result is small and boxable.
// i32 masks the upper lane so CNT counts only the 32-bit value.
func (l arm64Lowerer) popcnt(ctx *lowering, kind types.Kind) bool {
	if ctx.count() < 1 || !l.kinds(ctx, kind, 1) {
		return false
	}
	a := ctx.pop()
	src := a.reg
	if kind == types.KindI32 {
		src = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ANDI(src, a.reg, maskI32))
	}
	v := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(v, src),
		arm64.CNT(v, v),
		arm64.ADDV(v, v),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, v))
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// rotate lowers ROTL (left=true) or ROTR for an integer kind via ROR. ROTL is
// ROR by the negated amount; the rotate width follows the register view (W for
// i32, X for i64). An i64 rotate of the full 64-bit value can leave the boxable
// range, so it guards before pushing; i32 always fits.
func (l arm64Lowerer) rotate(ctx *lowering, op step, kind types.Kind, left bool) bool {
	if ctx.count() < 2 || !l.kinds(ctx, kind, 2) {
		return false
	}
	src := ctx.values[len(ctx.values)-2].reg
	amount := ctx.values[len(ctx.values)-1].reg
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	out := raw
	if kind == types.KindI32 {
		src, amount, out = l.narrow32(src), l.narrow32(amount), l.narrow32(raw)
	}
	if left {
		neg := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		if kind == types.KindI32 {
			neg = l.narrow32(neg)
		}
		ctx.assembler.Emit(arm64.NEG(neg, amount))
		amount = neg
	}
	ctx.assembler.Emit(arm64.ROR(out, src, amount))
	if kind == types.KindI64 && !l.boxableI64(ctx, raw, op.ip) {
		return false
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: raw, kind: kind, raw: true})
	return true
}

// extend lowers a sign-extend op (SXTB/SXTH/SXTW). The 64-bit form is correct
// for both kinds: it reads only the low byte/half/word and the sign-extended
// result stays within the boxable i64 range, so no guard is needed.
func (l arm64Lowerer) extend(ctx *lowering, kind types.Kind, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, kind, 1) {
		return false
	}
	a := ctx.pop()
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(emit(dst, a.reg))
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// reinterpret reinterprets the raw bits of the top value as another kind. The
// i32/f32 pair and the i64→f64 direction share their register representation,
// so only the kind changes. Reading an f64 bit pattern as i64 can leave the
// boxable range, so that direction guards first.
func (l arm64Lowerer) reinterpret(ctx *lowering, op step, from, to types.Kind) bool {
	if ctx.count() < 1 || !l.kinds(ctx, from, 1) {
		return false
	}
	if to == types.KindI64 {
		if !l.boxableI64(ctx, ctx.values[len(ctx.values)-1].reg, op.ip) {
			return false
		}
	}
	a := ctx.pop()
	ctx.push(value{reg: a.reg, kind: to, raw: true})
	return true
}

// f32Unary lowers a single-operand f32 op. The raw f32 keeps its bits in the
// low 32 of an int register, so it unboxes with a 32-bit FMOV and the result
// moves back untagged.
func (l arm64Lowerer) f32Unary(ctx *lowering, op func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF32, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(
		arm64.FMOV(fa, l.narrow32(a.reg)),
		op(fr, fa),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF32, raw: true})
	return true
}

// f64Unary lowers a single-operand f64 op. A raw f64 is its own bit pattern.
func (l arm64Lowerer) f64Unary(ctx *lowering, op func(dst, src asm.Reg) asm.Instruction) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindF64, 1) {
		return false
	}
	a := ctx.pop()
	fa := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(
		arm64.FMOV(fa, a.reg),
		op(fr, fa),
	)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(dst, fr))
	ctx.push(value{reg: dst, kind: types.KindF64, raw: true})
	return true
}

// copysign splices the sign bit of the top operand onto the magnitude of the
// one below it with GPR bit ops, matching math.Copysign(magnitude, sign). The
// raw float bits already live in int registers, so no FP move is needed.
func (l arm64Lowerer) copysign(ctx *lowering, kind types.Kind) bool {
	if ctx.count() < 2 || !l.kinds(ctx, kind, 2) {
		return false
	}
	sign := ctx.pop()
	magnitude := ctx.pop()
	mask := uint64(0x8000_0000_0000_0000)
	if kind == types.KindF32 {
		mask = 0x8000_0000
	}
	signbit := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(signbit, mask)...)
	notSign := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(notSign, ^mask)...)
	s := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.AND(s, sign.reg, signbit))
	m := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.AND(m, magnitude.reg, notSign))
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ORR(dst, m, s))
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// kinds reports whether the top n operands are all raw and computable as kind.
// The match is by representation, so the narrow integer kinds (i1, i8) satisfy
// an i32 operand exactly as they do in the interpreter; for every other kind
// Repr is the identity, so the check stays exact.
func (l arm64Lowerer) kinds(ctx *lowering, kind types.Kind, n int) bool {
	for k := 0; k < n; k++ {
		v := ctx.values[len(ctx.values)-1-k]
		if v.kind.Repr() != kind.Repr() || !v.raw {
			return false
		}
	}
	return true
}

// guardDivisor deopts before a divide by zero. When trace recorded a non-zero
// divisor, guardRaw owns the mismatch exit; otherwise the zero check protects
// the native divide itself.
func (l arm64Lowerer) guardDivisor(ctx *lowering, divisor value, reg asm.VReg, observed uint64, ip int) bool {
	guarded := false
	if !divisor.known && observed != 0 {
		if !l.guardRaw(ctx, reg, observed, ip) {
			return false
		}
		guarded = true
	}
	if divisor.known && divisor.imm != 0 || guarded {
		return true
	}
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMPI(reg, 0))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, fail))
	return true
}

// boxableI64 keeps raw i64 values within the boxed 49-bit lane.
func (l arm64Lowerer) boxableI64(ctx *lowering, raw asm.VReg, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return false
	}
	l.guardBoxable(ctx, raw, fail)
	return true
}

// guardRaw keeps observed narrow inputs speculative: a different runtime value
// exits before the opcode, so the threaded handler owns the general case.
func (l arm64Lowerer) guardRaw(ctx *lowering, got asm.VReg, val uint64, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return false
	}
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, val)...)
	if got.Width() == asm.Width32 {
		want = l.narrow32(want)
	}
	ctx.assembler.Emit(arm64.CMP(got, want))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	return true
}

func (arm64Lowerer) narrow32(v asm.VReg) asm.VReg {
	return asm.NewVReg(v.ID(), v.Type(), asm.Width32)
}

// call lowers a recorded CALL. The callee marker must resolve to an observed
// function ref: a self-call becomes a framed native BL into this trace's own
// head, and non-self callees inline as fused frames the deopt path can rebuild.
func (l arm64Lowerer) call(ctx *lowering, op step) bool {
	if ctx.count() < 1 {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef {
		return false
	}
	target := resolve(ctx.module, ctx.heap, op.callee)
	if target == nil || target.Typ == nil {
		return false
	}
	closureRef := 0
	params := len(target.Typ.Params)
	if v.raw {
		if v.fn != op.callee {
			return false
		}
		if v.ref != op.callee {
			closureRef = v.ref
		}
	} else {
		if op.seen.Kind() != types.KindRef {
			return false
		}
		wantRef := op.seen.Ref()
		closureRef = wantRef
		if wantRef < 0 || wantRef >= len(ctx.heap) {
			return false
		}
		if cl, ok := ctx.heap[wantRef].(*types.Closure); ok {
			if int(cl.Fn) != op.callee {
				return false
			}
		} else if wantRef != op.callee {
			return false
		}
		pre := ctx.pre()
		fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardValue, int(op.op))
		if !ok {
			return false
		}
		want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(want, uint64(types.BoxRef(wantRef)))...)
		ctx.assembler.Emit(arm64.CMP(v.reg, want))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
		l.releaseBox(ctx, v.reg, pre, op.ip)
	}
	if len(target.Captures) > 0 {
		if closureRef <= 0 || closureRef >= len(ctx.heap) {
			return false
		}
		if _, ok := ctx.heap[closureRef].(*types.Closure); !ok {
			return false
		}
	} else {
		closureRef = 0
	}
	ctx.pop()
	if ctx.count() < params || !l.args(ctx, target, params) {
		return false
	}
	if op.callee == ctx.addr {
		if len(target.Captures) > 0 {
			return false
		}
		return l.selfCall(ctx, op, target, params)
	}
	if len(ctx.frames) >= 4 {
		return false
	}

	base := ctx.sp() - params
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for k := 0; k < params; k++ {
		boxed, ok := l.box(ctx, ctx.values[len(ctx.values)-params+k])
		if !ok {
			return false
		}
		ctx.assembler.Emit(arm64.STR(boxed, addr, int16((base+k)*8)))
	}

	f := ctx.frame()
	f.resume = op.ip + 1
	frame := newActivation(op.callee, target, base, len(ctx.values)-params)
	frame.upvalRef = closureRef
	ctx.values = ctx.values[:len(ctx.values)-params]
	if len(frame.kinds) > params {
		zero := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(zero, 0)...)
		for k := params; k < len(frame.kinds); k++ {
			switch frame.kinds[k] {
			case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64:
			default:
				return false
			}
			frame.locals[k] = value{reg: zero, kind: frame.kinds[k], raw: true}
			frame.loaded[k] = true
			frame.dirty[k] = true
		}
	}
	ctx.frames = append(ctx.frames, frame)
	return true
}

// selfCall emits a framed native recursion into this trace's own head:
// flush state, check the frame budget, swap bp/sp, BL, propagate traps by
// recording the live frame chain, and reload everything afterwards because the
// callee owns every allocatable register.
func (l arm64Lowerer) selfCall(ctx *lowering, op step, target *types.Function, params int) bool {
	a := ctx.assembler
	for _, typ := range target.Typ.Returns {
		switch typ.Kind() {
		case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64:
		default:
			return false
		}
	}
	if !l.flush(ctx, true) {
		return false
	}

	vCtrl := ctx.pin(scratchCtrl)
	active := ctx.pinTo(arm64.X15)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journalCap*8)))
	a.Emit(arm64.CMP(active, budget))
	hasFrame := a.Label()
	a.Emit(arm64.BCondLabel(arm64.OpBCC, hasFrame))
	l.overflow(ctx, op)
	a.Bind(hasFrame)

	a.Emit(
		arm64.ADDI(active, active, 1),
		arm64.STR(active, vCtrl, int16(journalActive*8)),
	)

	vBP := ctx.pin(scratchBP)
	nextSP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(nextSP, vBP, uint16(ctx.sp())))
	oldBP := ctx.scratch[scratchBP]
	oldSP := ctx.scratch[scratchSP]
	a.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, 32),
		arm64.STP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(arm64.LR, arm64.SP, 16),
	)
	calleeBP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.SUBI(calleeBP, nextSP, uint16(params)))
	nBP := ctx.pinTo(oldBP)
	a.Emit(arm64.MOV(nBP, calleeBP))
	calleeSP := calleeBP
	if n := len(target.LocalKinds()); n > 0 {
		calleeSP = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(calleeSP, calleeBP, uint16(n)))
	}
	nSP := ctx.pinTo(oldSP)
	a.Emit(arm64.MOV(nSP, calleeSP))

	a.Emit(arm64.BLLabel(ctx.head))

	// A trapped callee already recorded its frames; restore this caller's VM
	// bp, append the live frame chain inner-to-outer, and keep unwinding.
	vCtrl = ctx.pin(scratchCtrl)
	trap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(trap, vCtrl, int16(journalTrap*8)))
	normal := a.Label()
	a.Emit(
		arm64.CBZLabel(trap, normal),
		arm64.LDR(oldBP, arm64.SP, 0),
	)
	l.unwind(ctx, vCtrl, op.ip+1)
	a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
		arm64.RET(),
	)
	a.Bind(normal)

	active = ctx.pinTo(arm64.X15)
	a.Emit(arm64.SUBI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journalActive*8)))
	a.Emit(
		arm64.LDP(oldBP, oldSP, arm64.SP, 0),
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
	)

	// Capture returns before any reload can claim the ABI registers.
	base := ctx.sp() - params
	rets := target.Typ.Returns
	regs := make([]asm.VReg, len(rets))
	if len(rets) <= len(arm64.IntRets) {
		for k := range rets {
			regs[k] = ctx.pinTo(arm64.IntRets[k])
		}
	}
	ctx.values = ctx.values[:len(ctx.values)-params]
	for fi := range ctx.frames {
		f := &ctx.frames[fi]
		for k := range f.loaded {
			f.loaded[k] = false
			f.dirty[k] = false
		}
	}
	l.reload(ctx)
	if len(rets) > len(arm64.IntRets) {
		vStack := ctx.pin(scratchStack)
		addr := l.base(ctx, vStack)
		for k := range rets {
			regs[k] = a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LDR(regs[k], addr, int16((base+k)*8)))
		}
	}
	for k, typ := range rets {
		ctx.push(value{reg: regs[k], kind: typ.Kind(), raw: true})
	}
	return true
}

// tailLoop lowers a tail call back to the trace anchor as a native loop
// back-edge: the new arguments become the anchor entry frame's params, the
// other locals reset, everything commits to the VM stack, and iterate branches
// to the head (or yields when the safepoint budget runs out). Constant stack
// depth — no BL, no journalActive — so self/mutual tail recursion never grows.
func (l arm64Lowerer) tailLoop(ctx *lowering, op step) bool {
	target, params, ok := l.tailTarget(ctx, op)
	if !ok {
		return false
	}
	args := make([]value, params)
	for k := params - 1; k >= 0; k-- {
		args[k] = ctx.pop()
	}
	// A tail call stands in return position: no operands survive besides the
	// arguments just consumed.
	if ctx.count() != 0 {
		return false
	}
	f := newActivation(ctx.addr, target, 0, 0)
	if !l.locals(ctx, &f, args) {
		return false
	}
	ctx.frames = append(ctx.frames[:0], f)
	if !l.flush(ctx, true) {
		return false
	}
	return l.iterate(ctx, 0)
}

// tailMorph lowers a tail call to a different function by reusing the current
// frame in place: the activation is replaced by the callee at the same base,
// its params seeded from the arguments and its other locals reset, then the
// step emission continues into the callee's body. The frame record save/unwind writes
// describes the callee, so a later trap rebuilds the reused frame as the callee
// exactly as threaded tail() leaves it.
func (l arm64Lowerer) tailMorph(ctx *lowering, op step) bool {
	target, params, ok := l.tailTarget(ctx, op)
	if !ok {
		return false
	}
	old := ctx.frame()
	base := old.base
	args := make([]value, params)
	for k := params - 1; k >= 0; k-- {
		args[k] = ctx.pop()
	}
	if ctx.count() != 0 {
		return false
	}
	f := newActivation(op.callee, target, base, len(ctx.values))
	f.resume = op.ip + 1
	if !l.locals(ctx, &f, args) {
		return false
	}
	ctx.frames[len(ctx.frames)-1] = f
	return true
}

// tailTarget resolves a recorded tail call's compile-time function target and
// consumes the funcref marker, emitting the runtime funcref guard for a
// non-constant ref (mirrors call's guard). Tail calls carry no closure upvals,
// so a captured target is rejected; the trace stays threaded. On success the
// top params operands are the validated arguments, still live.
func (l arm64Lowerer) tailTarget(ctx *lowering, op step) (*types.Function, int, bool) {
	if ctx.count() < 1 {
		return nil, 0, false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef {
		return nil, 0, false
	}
	target := resolve(ctx.module, ctx.heap, op.callee)
	if target == nil || target.Typ == nil || len(target.Captures) > 0 {
		return nil, 0, false
	}
	params := len(target.Typ.Params)
	if v.raw {
		if v.fn != op.callee || v.ref != op.callee {
			return nil, 0, false
		}
	} else {
		if op.seen.Kind() != types.KindRef {
			return nil, 0, false
		}
		wantRef := op.seen.Ref()
		if wantRef != op.callee || wantRef < 0 || wantRef >= len(ctx.heap) {
			return nil, 0, false
		}
		if _, ok := ctx.heap[wantRef].(*types.Function); !ok {
			return nil, 0, false
		}
		want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(want, uint64(types.BoxRef(wantRef)))...)
		ctx.assembler.Emit(arm64.CMP(v.reg, want))
		ok := ctx.assembler.Label()
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, ok))
		if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
			return nil, 0, false
		}
		ctx.assembler.Bind(ok)
		pre := ctx.pre()
		l.releaseBox(ctx, v.reg, pre, op.ip)
	}
	ctx.pop()
	if ctx.count() < params || !l.args(ctx, target, params) {
		return nil, 0, false
	}
	return target, params, true
}

// args verifies the top params operands match the callee's declared
// parameter kinds.
func (l arm64Lowerer) args(ctx *lowering, target *types.Function, params int) bool {
	kinds := target.LocalKinds()
	if len(kinds) < params {
		return false
	}
	for k := 0; k < params; k++ {
		v := ctx.values[len(ctx.values)-params+k]
		if v.kind != kinds[k] {
			return false
		}
		if v.kind == types.KindRef {
			if v.raw {
				return false
			}
			continue
		}
		if !v.raw {
			return false
		}
	}
	return true
}

// locals fills frame f with the call arguments args in its parameter slots and
// a raw zero in every remaining local, matching threaded tail()/CALL's clear.
// Each slot is loaded and dirty so the next flush commits it to the VM stack.
func (l arm64Lowerer) locals(ctx *lowering, f *activation, args []value) bool {
	for k := range args {
		f.locals[k] = args[k]
		f.loaded[k] = true
		f.dirty[k] = true
	}
	if len(f.kinds) > len(args) {
		zero := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(zero, 0)...)
		for k := len(args); k < len(f.kinds); k++ {
			switch f.kinds[k] {
			case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64:
			default:
				return false
			}
			f.locals[k] = value{reg: zero, kind: f.kinds[k], raw: true}
			f.loaded[k] = true
			f.dirty[k] = true
		}
	}
	return true
}

// stitch retires an inlined frame at its RETURN: the top return values
// land where the interpreter would put them — on the caller's operand stack.
func (l arm64Lowerer) stitch(ctx *lowering) bool {
	f := ctx.frame()
	if ctx.count() < f.returns {
		return false
	}
	rets := append([]value(nil), ctx.values[len(ctx.values)-f.returns:]...)
	ctx.values = ctx.values[:f.opBase]
	ctx.frames = ctx.frames[:len(ctx.frames)-1]
	ctx.values = append(ctx.values, rets...)
	return true
}

// ret closes the entry frame: boxed returns land at the frame base for
// the Go wrapper and in the ABI return registers for native callers.
func (l arm64Lowerer) ret(ctx *lowering) bool {
	if ctx.count() < ctx.returns {
		return false
	}
	a := ctx.assembler
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for idx := 0; idx < ctx.returns; idx++ {
		v := ctx.values[len(ctx.values)-ctx.returns+idx]
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		a.Emit(arm64.STR(boxed, addr, int16(idx*8)))
		if idx < len(arm64.IntRets) {
			ret := ctx.pinTo(arm64.IntRets[idx])
			a.Emit(arm64.MOV(ret, boxed))
		}
	}
	a.Emit(
		arm64.RET(),
	)
	return true
}

// complete finishes top-level module code: live locals and operands are boxed
// back to the VM stack, SP is published, and the wrapper marks the frame done.
func (l arm64Lowerer) complete(ctx *lowering) bool {
	if !l.flush(ctx, false) {
		return false
	}
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp())))
	a.Emit(arm64.STR(sp, vCtrl, int16(journalSP*8)))
	l.report(ctx, vCtrl, trapNone, ctx.frame().end)
	a.Emit(
		arm64.RET(),
	)
	return true
}

// iterate spends one unit of the safepoint budget at a loop back-edge:
// decrement journalBudget and branch to the loop head while budget remains,
// otherwise yield to the safepoint at the header. The caller has already
// committed loop-carried locals to the VM stack.
func (l arm64Lowerer) iterate(ctx *lowering, header int) bool {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.SUBI(budget, budget, 1))
	a.Emit(arm64.STR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.CBNZLabel(budget, ctx.back))
	return l.trap(ctx, trapYield, header, prof.ExitNone, prof.OpcodeNone)
}

// overflow surfaces a frame-budget overflow: the consumed callee marker
// is rematerialized and retained so the rebuilt interpreter state owns the
// reference the threaded CALL expects on top of the stack.
func (l arm64Lowerer) overflow(ctx *lowering, op step) {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)

	boxed := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(boxed, uint64(types.BoxRef(op.callee)))...)
	l.retain(ctx, op.callee)
	a.Emit(arm64.STR(boxed, addr, int16(ctx.sp()*8)))

	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp()+1)))
	a.Emit(arm64.STR(sp, vCtrl, int16(journalSP*8)))
	l.unwind(ctx, vCtrl, op.ip)
	l.report(ctx, vCtrl, trapOverflow, op.ip)
	a.Emit(
		arm64.RET(),
	)
}

// reload pulls operands back from VM stack homes after a call or continuation.
func (l arm64Lowerer) reload(ctx *lowering) {
	a := ctx.assembler
	if len(ctx.values) == 0 {
		return
	}
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for j := range ctx.values {
		v := &ctx.values[j]
		if v.kind == types.KindRef && v.raw {
			continue
		}
		reg := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDR(reg, addr, int16(ctx.slot(j)*8)))
		v.reg = reg
		if v.kind != types.KindRef {
			if v.kind == types.KindI64 {
				v.reg = l.sign64(ctx, reg)
			}
			v.raw = true
		}
	}
}

// loadLocal materializes local idx from the VM stack on first use. A
// declared i32 or f64 local is unboxed for free: the boxed i32 keeps its
// value in the low lane and a boxed f64 is its own bit pattern. The narrow
// integer locals (i8, i1) share the i32 representation, so they load the same
// way and keep their kind.
func (l arm64Lowerer) loadLocal(ctx *lowering, f *activation, idx, ip int) bool {
	if f.loaded[idx] {
		return true
	}
	kind := f.kinds[idx]
	switch kind {
	case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64, types.KindRef:
	default:
		return false
	}
	vStack := ctx.pin(scratchStack)
	reg, reusable := l.localReg(ctx, f, idx)
	if reusable {
		l.baseTo(ctx, vStack, reg)
		ctx.assembler.Emit(arm64.LDR(reg, reg, int16((f.base+idx)*8)))
	} else {
		addr := l.base(ctx, vStack)
		reg = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(reg, addr, int16((f.base+idx)*8)))
	}
	if kind == types.KindI64 {
		// A heap-promoted i64 is a ref the trace cannot read as a value; guard
		// the inline tag and deopt at the load if it promoted, then sign-extend
		// the 49-bit value lane to a full raw i64 (always boxable thereafter).
		if !l.guardI64(ctx, reg, ip) {
			return false
		}
		reg = l.sign64(ctx, reg)
	}
	raw := kind != types.KindRef
	f.locals[idx] = value{reg: reg, kind: kind, raw: raw}
	f.loaded[idx] = true
	return true
}

func (l arm64Lowerer) upvalGet(ctx *lowering, op step) bool {
	f := ctx.frame()
	idx := int(op.args[0])
	if idx >= len(f.upvals) {
		return false
	}
	kind := f.upvals[idx]
	base := l.upvalBase(ctx)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, base, int16(idx*8)))
	if kind == types.KindI64 {
		if !l.guardI64(ctx, dst, op.ip) {
			return false
		}
		dst = l.sign64(ctx, dst)
	}
	if kind == types.KindRef {
		l.retainBox(ctx, dst)
		ctx.push(value{reg: dst, kind: kind, raw: false})
		return true
	}
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

func (l arm64Lowerer) upvalSet(ctx *lowering, op step) bool {
	f := ctx.frame()
	idx := int(op.args[0])
	if idx >= len(f.upvals) || ctx.count() < 1 {
		return false
	}
	kind := f.upvals[idx]
	v := ctx.values[len(ctx.values)-1]
	if v.kind != kind {
		return false
	}
	boxed, ok := l.box(ctx, v)
	if !ok {
		return false
	}
	base := l.upvalBase(ctx)
	if kind == types.KindRef || kind == types.KindI64 {
		pre := ctx.pre()
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, base, int16(idx*8)))
		l.releaseBoxUnlessEqual(ctx, old, boxed, pre, op.ip)
	}
	ctx.assembler.Emit(arm64.STR(boxed, base, int16(idx*8)))
	ctx.pop()
	return true
}

func (l arm64Lowerer) upvalBase(ctx *lowering) asm.VReg {
	f := ctx.frame()
	if f.upvalRef > 0 {
		heap := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journalHeap*8)))
		off := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(off, uint64(f.upvalRef))...)
		ctx.assembler.Emit(arm64.LSLI(off, off, 4))
		cell := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ADD(cell, heap, off))
		data := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(data, cell, 8))
		base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(base, data, int16(closureUpvs+sliceData)))
		return base
	}
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalUpvals*8)))
	return base
}

func (l arm64Lowerer) refNull(ctx *lowering) bool {
	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(boxed, uint64(types.BoxedNull))...)
	l.retainBox(ctx, boxed)
	ctx.push(value{reg: boxed, kind: types.KindRef, raw: false})
	return true
}

func (l arm64Lowerer) refIsNull(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	vNull := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(vNull, uint64(types.BoxedNull))...)
	ctx.assembler.Emit(arm64.CMP(ref, vNull))
	// Capture the flags before release clobbers them, then release the consumed
	// ref so the bool result leaves no leaked reference on the stack.
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, arm64.CondEQ))
	l.releaseBox(ctx, ref, pre, op.ip)
	ctx.pop()
	ctx.push(value{reg: flag, kind: types.KindI1, raw: true})
	return true
}

func (l arm64Lowerer) refGet(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	kind := op.seen.Kind()
	switch op.shape.itab {
	case heapI32:
		if kind != types.KindI32 {
			return false
		}
	case heapF32:
		if kind != types.KindF32 {
			return false
		}
	case heapF64:
		if kind != types.KindF64 {
			return false
		}
	default:
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, op.shape.itab, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if kind == types.KindF64 {
		ctx.assembler.Emit(arm64.LDR(result, data, 0))
	} else {
		ctx.assembler.Emit(arm64.LDRSW(result, data, 0))
	}
	l.releaseRef(ctx, addr, pre, op.ip)
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: kind, raw: true})
	return true
}

// stringLen mirrors the threaded STRING_LEN handler: a heap-boxed
// types.String has the same {data, len} header layout as a slice, so its
// length lives at the same sliceLen offset arrayLen reads. Unlike ARRAY_LEN,
// the opcode's target concrete type is always types.String, so there is no
// shape to pick among; guardItab below is the only check needed and it deopts
// at runtime instead of aborting the lowering at trace-build time.
func (l arm64Lowerer) stringLen(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapString, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(n, data, sliceLen))
	ctx.assembler.Emit(arm64.MOV(result, n))
	l.releaseRef(ctx, addr, pre, op.ip)
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) arrayLen(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	base := int16(0)
	switch op.shape.itab {
	case heapArrayI1, heapArrayI8, heapArrayI32, heapArrayI64, heapArrayF32, heapArrayF64:
	case heapArrayRef:
		base = int16(arrayElems)
	default:
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, op.shape.itab, fail)

	result := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(n, data, base+sliceLen))
	ctx.assembler.Emit(arm64.MOV(result, n))
	l.releaseRef(ctx, addr, pre, op.ip)
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: result, kind: types.KindI32, raw: true})
	return true
}

func (l arm64Lowerer) arrayGet(ctx *lowering, op step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 || ctx.values[len(ctx.values)-2].kind != types.KindRef {
		return false
	}
	kind := op.seen.Kind()
	var want uintptr
	var base int16
	var scale uint8
	var raw bool
	switch kind {
	case types.KindI1:
		want = heapArrayI1
		raw = true
	case types.KindI8:
		want = heapArrayI8
		raw = true
	case types.KindI32:
		want = heapArrayI32
		scale = 2
		raw = true
	case types.KindI64:
		want = heapArrayI64
		scale = 3
		raw = true
	case types.KindF32:
		want = heapArrayF32
		scale = 2
		raw = true
	case types.KindF64:
		want = heapArrayF64
		scale = 3
		raw = true
	case types.KindRef:
		want = heapArrayRef
		base = int16(arrayElems)
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != want {
		return false
	}
	pre := ctx.pre()
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	bounds, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardBounds, int(op.op))
	if !ok {
		return false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardValue, int(op.op))
	if !ok {
		return false
	}
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-2])
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, want, fail)

	dataPtr, n := l.sliceHeader(ctx, data, base)
	l.guardIndex(ctx, idx, n, bounds)

	result := a.Reg(asm.RegTypeInt, asm.Width64)
	switch kind {
	case types.KindI1:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(result, elemAddr, 0))
	case types.KindI8:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		elem := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(elem, elemAddr, 0))
		a.Emit(arm64.SXTB(result, elem))
	case types.KindI32, types.KindF32:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		off := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(off, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, off))
		a.Emit(arm64.LDRSW(result, elemAddr, 0))
	case types.KindI64, types.KindF64, types.KindRef:
		if scale != 0 {
			off := a.Reg(asm.RegTypeInt, asm.Width64)
			elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LSLI(off, idx, scale))
			a.Emit(arm64.ADD(elemAddr, dataPtr, off))
			a.Emit(arm64.LDR(result, elemAddr, 0))
		} else {
			a.Emit(arm64.LDRR(result, dataPtr, idx))
		}
	}
	if kind == types.KindI64 {
		l.guardBoxable(ctx, result, valueFail)
	}
	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, valueFail)
	a.Emit(arm64.SUBI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, rcBase, addr))
	if kind == types.KindRef {
		l.retainBox(ctx, result)
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: kind, raw: raw})
	return true
}

func (l arm64Lowerer) arraySet(ctx *lowering, op step) bool {
	if ctx.count() < 3 || ctx.values[len(ctx.values)-2].kind != types.KindI32 || ctx.values[len(ctx.values)-3].kind != types.KindRef {
		return false
	}
	kind := ctx.values[len(ctx.values)-1].kind
	var want uintptr
	var base int16
	var scale uint8
	switch kind {
	case types.KindI1:
		want = heapArrayI1
	case types.KindI8:
		want = heapArrayI8
	case types.KindI32:
		want = heapArrayI32
		scale = 2
	case types.KindI64:
		want = heapArrayI64
		scale = 3
	case types.KindF32:
		want = heapArrayF32
		scale = 2
	case types.KindF64:
		want = heapArrayF64
		scale = 3
	case types.KindRef:
		want = heapArrayRef
		base = int16(arrayElems)
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != want {
		return false
	}
	pre := ctx.pre()
	val := ctx.values[len(ctx.values)-1]
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-2].reg)
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-3])
	if !ok {
		return false
	}
	a := ctx.assembler
	continuable := kind != types.KindRef && !op.terminal
	var fail, bounds, valueFail asm.Label
	if !continuable {
		fail, ok = l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
		if !ok {
			return false
		}
		bounds, ok = l.sideExit(ctx, pre, op.ip, prof.ExitGuardBounds, int(op.op))
		if !ok {
			return false
		}
		valueFail, ok = l.sideExit(ctx, pre, op.ip, prof.ExitGuardValue, int(op.op))
		if !ok {
			return false
		}
	} else {
		// A continuable primitive store is a state barrier: materialize the
		// pre-op frame once, then let all guards share that resumable snapshot.
		// Clearing the local register cache bounds no-spill pressure and makes
		// subsequent operations reload from the homes just written.
		if !l.flush(ctx, false) {
			return false
		}
		for idx := range ctx.frames {
			clear(ctx.frames[idx].loaded)
			clear(ctx.frames[idx].dirty)
		}
		ctx.reuseLocals = len(ctx.values) == 3
		fail = ctx.queueExit(nil, op.ip, 0, prof.ExitGuardShape, int(op.op))
		bounds = ctx.queueExit(nil, op.ip, 0, prof.ExitGuardBounds, int(op.op))
		valueFail = ctx.queueExit(nil, op.ip, 0, prof.ExitGuardValue, int(op.op))
	}
	var addr, itab, data, scratch asm.VReg
	remat := false
	if continuable {
		work := l.localScratch(ctx)
		if work.Width() == asm.WidthUndefined && val.known && val.kind.Repr() == types.KindI32 {
			work = val.reg
			remat = true
		}
		cell := asm.VReg{}
		if ctx.leaf {
			cell = ctx.pin(scratchSP)
		}
		addr, itab, data, scratch = l.heapRef(ctx, ref, work, cell)
	} else {
		addr, itab, data = l.guardHeap(ctx, ref, fail)
	}
	if continuable {
		l.guardItabTo(ctx, itab, scratch, want, fail)
	} else {
		l.guardItab(ctx, itab, want, fail)
	}

	var dataPtr, n asm.VReg
	if continuable {
		dataPtr = itab
		n = data
		l.sliceHeaderTo(ctx, data, dataPtr, n, base)
	} else {
		dataPtr, n = l.sliceHeader(ctx, data, base)
	}
	l.guardIndex(ctx, idx, n, bounds)

	var rcBase, rc, rcAddr asm.VReg
	if continuable {
		rcBase = scratch
		l.rcBaseTo(ctx, rcBase)
		ctx.assembler.Emit(arm64.MOV(n, addr))
		rcAddr = addr
		rc = n
		l.guardRCTo(ctx, rc, rcAddr, rcBase, valueFail)
	} else {
		rcBase = l.rcBase(ctx)
		rcAddr = addr
		rc = l.guardRC(ctx, rcAddr, rcBase, valueFail)
	}

	if kind == types.KindRef {
		// The container's refcount deopt point (guardRC above) runs before the
		// overwritten element's release, which carries its own internal deopt
		// (releaseBoxUnlessEqual -> releaseRef -> guardRC). Neither refcount is
		// decremented until both checks pass, matching the release-old-ref-first
		// idiom used by globalSet.
		old := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(old, dataPtr, idx))
		l.releaseBoxUnlessEqual(ctx, old, val.reg, pre, op.ip)
		a.Emit(arm64.SUBI(rc, rc, 1))
		a.Emit(arm64.STRR(rc, rcBase, rcAddr))
		a.Emit(arm64.STRR(val.reg, dataPtr, idx))
	} else {
		// All primitive-store guards have passed, so release the consumed array
		// operand before address formation. This shortens addr/rc liveness and
		// keeps mutation loops within the no-spill register budget.
		a.Emit(arm64.SUBI(rc, rc, 1))
		a.Emit(arm64.STRR(rc, rcBase, rcAddr))
		switch kind {
		case types.KindI1, types.KindI8:
			target := n
			if remat {
				target = scratch
			}
			a.Emit(arm64.ADD(target, dataPtr, idx))
			if remat {
				a.Emit(arm64.LDI(val.reg, uint64(val.imm))...)
			}
			a.Emit(arm64.STRB(val.reg, target, 0))
		case types.KindI32, types.KindF32:
			target := n
			if remat {
				target = scratch
			}
			a.Emit(arm64.LSLI(target, idx, scale))
			a.Emit(arm64.ADD(target, dataPtr, target))
			if remat {
				a.Emit(arm64.LDI(val.reg, uint64(val.imm))...)
			}
			a.Emit(arm64.STRW(val.reg, target, 0))
		case types.KindI64, types.KindF64:
			if remat {
				a.Emit(arm64.LDI(val.reg, uint64(val.imm))...)
			}
			a.Emit(arm64.STRR(val.reg, dataPtr, idx))
		}
		if continuable && remat {
			ctx.spare = rc
		}
	}

	ctx.values = ctx.values[: len(ctx.values)-3 : len(ctx.values)-3]
	if !continuable {
		return l.exit(ctx, op.ip+1, prof.ExitTerminalOp, int(op.op))
	}
	return true
}

func (l arm64Lowerer) structGet(ctx *lowering, op step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 || ctx.values[len(ctx.values)-2].kind != types.KindRef {
		return false
	}
	out := op.seen.Kind()
	switch out {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != heapStruct {
		return false
	}
	pre := ctx.pre()
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-2])
	if !ok {
		return false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	bounds, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardBounds, int(op.op))
	if !ok {
		return false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardValue, int(op.op))
	if !ok {
		return false
	}
	kindFail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardKind, int(op.op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapStruct, fail)

	typ := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(typ, data, int16(structTyp)))
	if op.shape.typ != 0 {
		want := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(want, uint64(op.shape.typ))...)
		a.Emit(arm64.CMP(typ, want))
		a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	}
	fields, n := l.sliceHeader(ctx, typ, int16(fieldsSlice))
	l.guardIndex(ctx, idx, n, bounds)

	fieldOff := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(fieldOff, uint64(fieldSize))...)
	a.Emit(arm64.MUL(fieldOff, idx, fieldOff))
	field := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(field, fields, fieldOff))
	fieldKindReg := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRB(fieldKindReg, field, int16(fieldKind)))
	a.Emit(arm64.CMPI(fieldKindReg, uint16(out)))
	a.Emit(arm64.BCondLabel(arm64.OpBNE, kindFail))

	dataPtr, _ := l.sliceHeader(ctx, data, int16(structData))
	result := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(result, dataPtr, idx))
	if out == types.KindI64 {
		l.guardBoxable(ctx, result, valueFail)
	}
	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, valueFail)
	if out == types.KindRef {
		l.retainBox(ctx, result)
	}
	a.Emit(arm64.SUBI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, rcBase, addr))
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: out, raw: out != types.KindRef})
	return true
}

func (l arm64Lowerer) guardBoxable(ctx *lowering, v asm.VReg, fail asm.Label) {
	ext := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SBFX(ext, v, 0, boxableWidth))
	ctx.assembler.Emit(arm64.CMP(ext, v))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
}

func (l arm64Lowerer) structSet(ctx *lowering, op step) bool {
	if ctx.count() < 3 || ctx.values[len(ctx.values)-2].kind != types.KindI32 || ctx.values[len(ctx.values)-3].kind != types.KindRef {
		return false
	}
	kind := ctx.values[len(ctx.values)-1].kind
	switch kind {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
	default:
		return false
	}
	if op.shape.itab != 0 && op.shape.itab != heapStruct {
		return false
	}
	pre := ctx.pre()
	val := ctx.values[len(ctx.values)-1]
	idx := l.sign32(ctx, ctx.values[len(ctx.values)-2].reg)
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-3])
	if !ok {
		return false
	}
	a := ctx.assembler
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	bounds, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardBounds, int(op.op))
	if !ok {
		return false
	}
	valueFail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardValue, int(op.op))
	if !ok {
		return false
	}
	kindFail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardKind, int(op.op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapStruct, fail)

	typ := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(typ, data, int16(structTyp)))
	if op.shape.typ != 0 {
		want := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(want, uint64(op.shape.typ))...)
		a.Emit(arm64.CMP(typ, want))
		a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	}
	fields, n := l.sliceHeader(ctx, typ, int16(fieldsSlice))
	l.guardIndex(ctx, idx, n, bounds)

	fieldOff := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(fieldOff, uint64(fieldSize))...)
	a.Emit(arm64.MUL(fieldOff, idx, fieldOff))
	field := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(field, fields, fieldOff))
	fieldKindReg := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRB(fieldKindReg, field, int16(fieldKind)))
	a.Emit(arm64.CMPI(fieldKindReg, uint16(kind)))
	a.Emit(arm64.BCondLabel(arm64.OpBNE, kindFail))

	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, valueFail)

	dataPtr, _ := l.sliceHeader(ctx, data, int16(structData))
	if kind == types.KindRef {
		// Mirrors arraySet's ref-element handling: the container's guardRC
		// above is the deopt point for the container's own refcount, and the
		// overwritten field's release (releaseBoxUnlessEqual) carries its own
		// internal deopt. Neither refcount is decremented until both checks
		// pass.
		old := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(old, dataPtr, idx))
		l.releaseBoxUnlessEqual(ctx, old, val.reg, pre, op.ip)
	} else {
		var stored asm.VReg
		switch kind {
		case types.KindI1, types.KindI8, types.KindI32, types.KindF32:
			stored = a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.ANDI(stored, val.reg, maskI32))
		case types.KindI64, types.KindF64:
			stored = val.reg
		}
		a.Emit(arm64.STRR(stored, dataPtr, idx))
	}
	a.Emit(arm64.SUBI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, rcBase, addr))
	if kind == types.KindRef {
		a.Emit(arm64.STRR(val.reg, dataPtr, idx))
	}

	ctx.values = pre[: len(pre)-3 : len(pre)-3]
	return l.exit(ctx, op.ip+1, prof.ExitTerminalOp, int(op.op))
}

// sign32 sign-extends a raw i32's low lane for signed division and
// shifts; zero32 zero-extends it for their unsigned counterparts.
func (l arm64Lowerer) sign32(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SXTW(out, v))
	return out
}

// errorGet reads a guest Error's payload. It mirrors the threaded handler:
// retain a ref payload first, then release the consumed error handle.
func (l arm64Lowerer) errorGet(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	if op.shape.itab != heapError {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapError, fail)

	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, data, int16(errorValue)))
	kind := op.seen.Kind()
	switch kind {
	case types.KindI64:
		if !l.guardI64(ctx, dst, op.ip) {
			return false
		}
		dst = l.sign64(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	case types.KindRef:
		base := l.rcBase(ctx)
		rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
		ctx.assembler.Emit(arm64.CMPI(rc, 1))
		shared := ctx.assembler.Label()
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBGT, shared))
		ctx.values = append(ctx.values[:0], pre...)
		if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
			return false
		}
		ctx.assembler.Bind(shared)
		ctx.values = append(ctx.values[:0], pre...)
		l.retainBox(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: false})
	case types.KindI32, types.KindF32, types.KindF64:
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	default:
		return false
	}
	return true
}

// coroDone reads a coroutine handle's done flag and pushes it as an i32 (0 or
// 1). It mirrors the threaded handler: the handle ref stays owned by its stack
// slot, so no refcount changes. A constant coroutine handle is impossible, so
// a raw (unboxed constant) ref is rejected to avoid box's retain side effect.
func (l arm64Lowerer) coroDone(ctx *lowering, op step) bool {
	if ctx.count() < 1 {
		return false
	}
	if op.shape.itab != heapCoroutine {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef || v.raw {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, v)
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	_, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapCoroutine, fail)

	done := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDRB(done, data, int16(coroDone)))
	ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: done, kind: types.KindI1, raw: true})
	return true
}

// coroValue reads a coroutine handle's last yielded or returned value. It
// mirrors the threaded handler: retain the value, then release the handle.
// The stored field is a full Boxed, so its representation matches a global
// slot (see globalGet) — scalars push raw, refs stay boxed.
func (l arm64Lowerer) coroValue(ctx *lowering, op step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	if op.shape.itab != heapCoroutine {
		return false
	}
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	fail, ok := l.sideExit(ctx, pre, op.ip, prof.ExitGuardShape, int(op.op))
	if !ok {
		return false
	}
	addr, itab, data := l.guardHeap(ctx, ref, fail)
	l.guardItab(ctx, itab, heapCoroutine, fail)

	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, data, int16(coroValue)))
	kind := op.seen.Kind()
	switch kind {
	case types.KindI64:
		if !l.guardI64(ctx, dst, op.ip) {
			return false
		}
		dst = l.sign64(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	case types.KindRef:
		base := l.rcBase(ctx)
		rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
		ctx.assembler.Emit(arm64.CMPI(rc, 1))
		shared := ctx.assembler.Label()
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBGT, shared))
		ctx.values = append(ctx.values[:0], pre...)
		if !l.exit(ctx, op.ip, prof.ExitTerminalOp, int(op.op)) {
			return false
		}
		ctx.assembler.Bind(shared)
		ctx.values = append(ctx.values[:0], pre...)
		l.retainBox(ctx, dst)
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: false})
	case types.KindI32, types.KindF32, types.KindF64:
		l.releaseRef(ctx, addr, pre, op.ip)
		ctx.values = append(pre[:len(pre)-1:len(pre)-1], value{reg: dst, kind: kind, raw: true})
	default:
		return false
	}
	return true
}

// guardI64 deopts when v is a heap-promoted i64.
func (l arm64Lowerer) guardI64(ctx *lowering, v asm.VReg, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardKind, ctx.opcode(ip))
	if !ok {
		return false
	}
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagI64>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	return true
}

// sign64 sign-extends the 49-bit value lane of a boxed i64 to a full raw
// i64 value.
func (l arm64Lowerer) sign64(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SBFX(out, v, 0, boxableWidth))
	return out
}

// exit deopts to the threaded interpreter: flush every live value boxed,
// publish sp, record the live frame chain, and report a fallback at resume.
func (l arm64Lowerer) exit(ctx *lowering, resume int, reason prof.ExitReason, opcode int) bool {
	return l.trap(ctx, trapFallback, resume, reason, opcode)
}

// trap unwinds the inlined native state into the journal and returns to the Go
// wrapper: every live value is flushed boxed, sp is published, the frame chain
// is recorded resuming at resume, and the trap kind is reported. trapFallback
// resumes threaded dispatch; trapYield re-enters native after a safepoint.
func (l arm64Lowerer) trap(ctx *lowering, kind, resume int, reason prof.ExitReason, opcode int) bool {
	if !l.flush(ctx, false) {
		return false
	}
	id := -1
	if kind == trapFallback {
		id = len(ctx.descriptors)
		ctx.descriptors = append(ctx.descriptors, exitDescriptor{reason: reason, opcode: opcode})
	}
	l.trapFlushed(ctx, kind, resume, id)
	return true
}

func (l arm64Lowerer) trapFlushed(ctx *lowering, kind, resume, exitID int) {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp())))
	a.Emit(arm64.STR(sp, vCtrl, int16(journalSP*8)))
	l.unwind(ctx, vCtrl, resume)
	vExit := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vExit, uint64(exitID+1))...)
	a.Emit(arm64.STR(vExit, vCtrl, int16(journalExitID*8)))
	l.report(ctx, vCtrl, kind, resume)
	a.Emit(
		arm64.RET(),
	)
}

// heapRef loads a heap cell for a value already proven to have ref kind by the
// symbolic stack. It reuses ref and off as outputs, so callers use it only after
// a state barrier whose exits reload boxed operands from their stack homes.
func (arm64Lowerer) heapRef(ctx *lowering, ref, off, cell asm.VReg) (asm.VReg, asm.VReg, asm.VReg, asm.VReg) {
	a := ctx.assembler
	addr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ANDI(addr, ref, maskI32))
	if cell.Width() == asm.WidthUndefined {
		cell = a.Reg(asm.RegTypeInt, asm.Width64)
	}
	a.Emit(arm64.LDR(cell, ctx.pin(scratchCtrl), int16(journalHeap*8)))
	if off.Width() == asm.WidthUndefined {
		off = a.Reg(asm.RegTypeInt, asm.Width64)
	}
	a.Emit(arm64.LSLI(off, addr, 4))
	a.Emit(arm64.ADD(cell, cell, off))
	itab := ref
	data := off
	a.Emit(
		arm64.LDR(itab, cell, 0),
		arm64.LDR(data, cell, 8),
	)
	return addr, itab, data, cell
}

// guardHeap loads a heap cell or branches to fail on a non-ref tag. Unlike
// heapRef, it preserves ref because queued side exits may still need the boxed
// operand.
func (arm64Lowerer) guardHeap(ctx *lowering, ref asm.VReg, fail asm.Label) (asm.VReg, asm.VReg, asm.VReg) {
	a := ctx.assembler
	tag := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSRI(tag, ref, uint8(types.VBits)))
	wantRef := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(wantRef, tagRef>>types.VBits)...)
	a.Emit(arm64.CMP(tag, wantRef))
	a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))

	addr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ANDI(addr, ref, maskI32))
	heap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journalHeap*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(off, addr, 4))
	cell := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(cell, heap, off))
	itab := a.Reg(asm.RegTypeInt, asm.Width64)
	data := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(
		arm64.LDR(itab, cell, 0),
		arm64.LDR(data, cell, 8),
	)
	return addr, itab, data
}

func (arm64Lowerer) sliceHeaderTo(ctx *lowering, data, ptr, n asm.VReg, base int16) {
	ctx.assembler.Emit(
		arm64.LDR(ptr, data, base+sliceData),
		arm64.LDR(n, data, base+sliceLen),
	)
}

func (l arm64Lowerer) sliceHeader(ctx *lowering, data asm.VReg, base int16) (asm.VReg, asm.VReg) {
	ptr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	n := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	l.sliceHeaderTo(ctx, data, ptr, n, base)
	return ptr, n
}

// guardIndex uses one unsigned check: sign-extended negative i32 indexes are
// above any VM array or struct length.
func (arm64Lowerer) guardIndex(ctx *lowering, idx, n asm.VReg, fail asm.Label) {
	ctx.assembler.Emit(arm64.CMP(idx, n))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBCS, fail))
}

func (arm64Lowerer) matchItab(ctx *lowering, got asm.VReg, want uintptr, hit asm.Label) {
	v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(v, uint64(want))...)
	ctx.assembler.Emit(arm64.CMP(got, v))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, hit))
}

func (arm64Lowerer) guardItabTo(ctx *lowering, got, scratch asm.VReg, want uintptr, fail asm.Label) {
	ctx.assembler.Emit(arm64.LDI(scratch, uint64(want))...)
	ctx.assembler.Emit(arm64.CMP(got, scratch))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
}

func (l arm64Lowerer) guardItab(ctx *lowering, got asm.VReg, want uintptr, fail asm.Label) {
	v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	l.guardItabTo(ctx, got, v, want, fail)
}

func (arm64Lowerer) matchKind(ctx *lowering, got asm.VReg, want types.Kind, hit asm.Label) {
	ctx.assembler.Emit(arm64.CMPI(got, uint16(want)))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, hit))
}

// unwind appends one journal frame record per live symbolic frame,
// innermost first so deopt rebuilds the chain in interpreter order. The
// innermost frame resumes at resume; outer frames resume past their calls.
func (l arm64Lowerer) unwind(ctx *lowering, vCtrl asm.VReg, resume int) {
	for k := len(ctx.frames) - 1; k >= 0; k-- {
		f := &ctx.frames[k]
		ip := f.resume
		if k == len(ctx.frames)-1 {
			ip = resume
		}
		l.save(ctx, vCtrl, f, ip)
	}
}

func (l arm64Lowerer) save(ctx *lowering, vCtrl asm.VReg, f *activation, ip int) {
	a := ctx.assembler
	depth := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(depth, vCtrl, int16(journalDepth*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(off, depth, 5))
	base := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(base, vCtrl, off))

	vAddr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vAddr, uint64(f.addr))...)
	bp := ctx.pin(scratchBP)
	if f.base != 0 {
		shifted := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(shifted, bp, uint16(f.base)))
		bp = shifted
	}
	a.Emit(arm64.STP(vAddr, bp, base, int16((journalHead+recordAddr)*8)))

	vIP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vIP, uint64(ip))...)
	vReturns := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vReturns, uint64(f.returns))...)
	a.Emit(arm64.STP(vIP, vReturns, base, int16((journalHead+recordIP)*8)))

	a.Emit(arm64.ADDI(depth, depth, 1))
	a.Emit(arm64.STR(depth, vCtrl, int16(journalDepth*8)))
}

func (l arm64Lowerer) report(ctx *lowering, vCtrl asm.VReg, trap, nextIP int) {
	a := ctx.assembler
	vTrap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vTrap, uint64(trap))...)
	a.Emit(arm64.STR(vTrap, vCtrl, int16(journalTrap*8)))
	vIP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vIP, uint64(nextIP))...)
	a.Emit(arm64.STR(vIP, vCtrl, int16(journalNextIP*8)))
}

func (l arm64Lowerer) releaseBoxUnlessEqual(ctx *lowering, old, val asm.VReg, pre []value, ip int) {
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CMP(old, val))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, done))
	l.releaseBox(ctx, old, pre, ip)
	ctx.assembler.Bind(done)
	ctx.values = append(ctx.values[:0], pre...)
}

func (l arm64Lowerer) releaseBox(ctx *lowering, v asm.VReg, pre []value, ip int) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		l.releaseRef(ctx, addr, pre, ip)
	})
}

func (l arm64Lowerer) retainBoxUnlessEqual(ctx *lowering, old, val asm.VReg) {
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CMP(old, val))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, done))
	l.retainBox(ctx, val)
	ctx.assembler.Bind(done)
}

func (l arm64Lowerer) retainBox(ctx *lowering, v asm.VReg) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		l.retainRef(ctx, addr)
	})
}

func (l arm64Lowerer) retainKnownBox(ctx *lowering, v asm.VReg) {
	a := ctx.assembler
	addr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ANDI(addr, v, maskI32))
	base := ctx.pin(scratchSP)
	l.rcBaseTo(ctx, base)
	rc := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(rc, base, addr))
	a.Emit(arm64.ADDI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, base, addr))
}

func (l arm64Lowerer) retainRef(ctx *lowering, addr asm.VReg) {
	base := l.rcBase(ctx)
	rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
	ctx.assembler.Emit(arm64.ADDI(rc, rc, 1))
	ctx.assembler.Emit(arm64.STRR(rc, base, addr))
}

// releaseRef decrements addr after guardRC proves it will stay live.
func (l arm64Lowerer) releaseRef(ctx *lowering, addr asm.VReg, pre []value, ip int) {
	fail, ok := l.sideExit(ctx, pre, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return
	}
	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, fail)
	ctx.assembler.Emit(arm64.SUBI(rc, rc, 1))
	ctx.assembler.Emit(arm64.STRR(rc, rcBase, addr))
}

// sideExit snapshots a guard fallback from the pre-op stack shape. The snapshot
// may include inlined frames; trapFlushed records the frame chain so the Go
// wrapper can rebuild the same threaded resume shape.
func (l arm64Lowerer) sideExit(ctx *lowering, pre []value, resume int, reason prof.ExitReason, opcode int) (asm.Label, bool) {
	ctx.values = append(ctx.values[:0], pre...)
	if !l.flush(ctx, false) {
		return 0, false
	}
	label := ctx.queueExit(nil, resume, 0, reason, opcode)
	ctx.values = append(ctx.values[:0], pre...)
	return label, true
}

// flush writes every dirty local and live operand to its VM stack home
// in boxed form. commit clears dirty marks — only the hot path may do that;
// a guard's cold path flushes a copy of the state and must leave the symbolic
// state of the surviving hot path untouched.
func (l arm64Lowerer) flush(ctx *lowering, commit bool) bool {
	a := ctx.assembler
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for fi := range ctx.frames {
		f := &ctx.frames[fi]
		for idx := range f.kinds {
			if !f.dirty[idx] {
				continue
			}
			boxed, ok := l.boxHome(ctx, f.locals[idx])
			if !ok {
				return false
			}
			a.Emit(arm64.STR(boxed, addr, int16((f.base+idx)*8)))
			if commit {
				f.dirty[idx] = false
			}
		}
	}
	for j, v := range ctx.values {
		if v.kind == types.KindRef && commit {
			return false
		}
		boxed, ok := l.boxHome(ctx, v)
		if !ok {
			return false
		}
		a.Emit(arm64.STR(boxed, addr, int16(ctx.slot(j)*8)))
	}
	return true
}

func (arm64Lowerer) localScratch(ctx *lowering) asm.VReg {
	for fi := range ctx.frames {
		frame := &ctx.frames[fi]
		for _, local := range frame.locals {
			reg := local.reg
			if reg.Width() == asm.WidthUndefined {
				continue
			}
			used := false
			for _, value := range ctx.values {
				if value.reg.Width() != asm.WidthUndefined && value.reg.ID() == reg.ID() {
					used = true
					break
				}
			}
			if !used {
				return reg
			}
		}
	}
	return asm.VReg{}
}

func (l arm64Lowerer) localReg(ctx *lowering, target *activation, idx int) (asm.VReg, bool) {
	reg := target.locals[idx].reg
	if !ctx.reuseLocals || reg.Width() == asm.WidthUndefined {
		return asm.VReg{}, false
	}
	for _, value := range ctx.values {
		if value.reg.Width() != asm.WidthUndefined && value.reg.ID() == reg.ID() {
			return asm.VReg{}, false
		}
	}
	for fi := range ctx.frames {
		frame := &ctx.frames[fi]
		for li, value := range frame.locals {
			if frame == target && li == idx || !frame.loaded[li] {
				continue
			}
			if value.reg.Width() != asm.WidthUndefined && value.reg.ID() == reg.ID() {
				return asm.VReg{}, false
			}
		}
	}
	return reg, true
}

func (l arm64Lowerer) baseTo(ctx *lowering, vStack, addr asm.VReg) {
	vBP := ctx.pin(scratchBP)
	ctx.assembler.Emit(arm64.LSLI(addr, vBP, 3))
	ctx.assembler.Emit(arm64.ADD(addr, vStack, addr))
}

func (l arm64Lowerer) base(ctx *lowering, vStack asm.VReg) asm.VReg {
	if ctx.leaf {
		addr := ctx.pin(scratchSP)
		l.baseTo(ctx, vStack, addr)
		return addr
	}
	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	l.baseTo(ctx, vStack, addr)
	return addr
}

func (l arm64Lowerer) boxHome(ctx *lowering, v value) (asm.VReg, bool) {
	if ctx.spare.Width() != asm.WidthUndefined && ctx.spare.ID() != v.reg.ID() && v.raw {
		var tag uint64
		switch v.kind {
		case types.KindI1:
			tag = tagI1
		case types.KindI8:
			tag = tagI8
		case types.KindI32:
			tag = tagI32
		default:
			return l.box(ctx, v)
		}
		ctx.assembler.Emit(arm64.ANDI(ctx.spare, v.reg, maskI32))
		ctx.assembler.Emit(arm64.MOVK(ctx.spare, uint16(tag>>48), 48))
		return ctx.spare, true
	}
	return l.box(ctx, v)
}

// box produces the boxed form of v in a fresh register. A marker materializes
// its function ref and retains it, because the interpreter state being rebuilt
// will release it through the frame it pushes.
func (l arm64Lowerer) box(ctx *lowering, v value) (asm.VReg, bool) {
	a := ctx.assembler
	switch v.kind {
	case types.KindI32:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI32>>48), 48))
		return lo, true
	case types.KindI8:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI8>>48), 48))
		return lo, true
	case types.KindI1:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI1>>48), 48))
		return lo, true
	case types.KindF32:
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		tag := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(tag, tagF32)...)
		a.Emit(arm64.ORR(lo, lo, tag))
		return lo, true
	case types.KindI64:
		// Raw i64 is the full signed value and boxable by invariant; mask the
		// 49-bit lane and tag.
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI64))
		tag := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(tag, tagI64)...)
		a.Emit(arm64.ORR(lo, lo, tag))
		return lo, true
	case types.KindF64:
		return v.reg, true
	case types.KindRef:
		if !v.raw {
			return v.reg, true
		}
		ref := v.ref
		if ref == 0 {
			ref = v.fn
		}
		boxed := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(boxed, uint64(types.BoxRef(ref)))...)
		l.retain(ctx, ref)
		return boxed, true
	}
	return asm.VReg{}, false
}

// retain bumps the refcount of the heap cell at compile-time address fn.
func (l arm64Lowerer) retain(ctx *lowering, fn int) {
	a := ctx.assembler
	base := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalRC*8)))
	slot := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(slot, uint64(fn))...)
	rc := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(rc, base, slot))
	a.Emit(arm64.ADDI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, base, slot))
}

// guardRC keeps releases that could free objects in the interpreter.
func (arm64Lowerer) guardRCTo(ctx *lowering, rc, addr, rcBase asm.VReg, fail asm.Label) {
	a := ctx.assembler
	a.Emit(arm64.LDRR(rc, rcBase, addr))
	a.Emit(arm64.CMPI(rc, 1))
	a.Emit(arm64.BCondLabel(arm64.OpBLE, fail))
}

func (l arm64Lowerer) guardRC(ctx *lowering, addr, rcBase asm.VReg, fail asm.Label) asm.VReg {
	rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	l.guardRCTo(ctx, rc, addr, rcBase, fail)
	return rc
}

func (l arm64Lowerer) refOnly(ctx *lowering, v asm.VReg, body func(asm.VReg)) {
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagRef>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))

	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, done))

	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(addr, v, maskI32))
	body(addr)

	ctx.assembler.Bind(done)
}

func (arm64Lowerer) rcBaseTo(ctx *lowering, base asm.VReg) {
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalRC*8)))
}

func (l arm64Lowerer) rcBase(ctx *lowering) asm.VReg {
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	l.rcBaseTo(ctx, base)
	return base
}

func join(steps, tail []int) []int {
	if len(steps) == 0 {
		return tail
	}
	if len(tail) == 0 {
		return steps
	}
	return append(append([]int(nil), steps...), tail...)
}

func zeroValue(kind types.Kind) (types.Boxed, bool) {
	switch kind {
	case types.KindI1:
		return types.BoxI1(false), true
	case types.KindI8:
		return types.BoxI8(0), true
	case types.KindI32:
		return types.BoxI32(0), true
	case types.KindI64:
		return types.BoxI64(0), true
	case types.KindF32:
		return types.BoxF32(0), true
	case types.KindF64:
		return types.BoxF64(0), true
	case types.KindRef:
		return types.BoxedNull, true
	default:
		return 0, false
	}
}

func isLeaf(blocks []block) bool {
	for _, block := range blocks {
		for _, step := range block.steps {
			switch step.op {
			case instr.CALL, instr.RETURN_CALL:
				return false
			}
		}
	}
	return true
}

// lower emits one plan through the common block pipeline.
func lower(ctx *lowering, plan plan) bool {
	l := arm64Lowerer{}
	ctx.leaf = isLeaf(plan.blocks)
	l.enter(ctx)
	ctx.blocks = plan.blocks
	ctx.kind = plan.kind
	for id, block := range ctx.blocks {
		if !block.tail && block.state != nil {
			ctx.labels[id] = ctx.assembler.Label()
		}
	}
	root := plan.root
	if _, ok := ctx.labels[root]; !ok {
		ctx.labels[root] = ctx.assembler.Label()
	}
	ctx.back = ctx.labels[root]
	ctx.assembler.Bind(ctx.back)
	if !l.emitBlock(ctx, root, nil) {
		return false
	}
	for id, block := range ctx.blocks {
		if id == root || block.tail || block.state == nil {
			continue
		}
		ctx.assembler.Bind(ctx.labels[id])
		if !l.emitBlock(ctx, id, nil) {
			return false
		}
	}
	for n := 0; n < len(ctx.work); n++ {
		work := ctx.work[n]
		ctx.reuseLocals = false
		ctx.spare = asm.VReg{}
		ctx.values = work.values
		ctx.frames = work.frames
		ctx.assembler.Bind(work.label)
		l.reload(ctx)
		if !l.emitBlock(ctx, work.block, work.tail) {
			return false
		}
	}
	l.materializeExits(ctx)
	return true
}
