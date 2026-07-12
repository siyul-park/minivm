package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
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

// lower emits one plan through the common block pipeline.
func lower(ctx *lowering, plan plan) bool {
	l := arm64Lowerer{}
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
		ctx.values = exit.values
		ctx.frames = exit.frames
		ctx.assembler.Bind(exit.label)
		if exit.retain > 0 {
			l.retain(ctx, exit.retain)
		}
		l.trapFlushed(ctx, trapFallback, exit.resume)
	}
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
			if !l.exit(ctx, op.ip) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.F32_REM, instr.F32_MOD:
			if !l.exit(ctx, op.ip) {
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
			if !l.arraySet(ctx, op) {
				return false, false
			}
			return true, idx == len(ops)-1
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
			if !l.exit(ctx, op.ip) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.ERROR_NEW, instr.ERROR_CODE, instr.THROW:
			// Allocation and handler landing stay interpreter-owned. Resume at
			// op.ip because each threaded handler performs its own IP update or
			// handler transfer.
			if !l.exit(ctx, op.ip) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.YIELD, instr.RESUME:
			// True suspension points: deopt to the threaded handler, which runs
			// the real suspend/resume. Resume at op.ip (not op.ip+1) because the
			// YIELD and RESUME handlers perform their own ip advance.
			if !l.exit(ctx, op.ip) {
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
	return l.exit(ctx, op.ip)
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
		l.retainBox(ctx, f.locals[idx].reg)
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

// base returns &stack[bp] for slot-relative loads and stores.
func (l arm64Lowerer) base(ctx *lowering, vStack asm.VReg) asm.VReg {
	a := ctx.assembler
	vBP := ctx.pin(scratchBP)
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(off, vBP, 3))
	addr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(addr, vStack, off))
	return addr
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

// marked reports whether a raw ref marker blocks deferred continuation reload.
func (l arm64Lowerer) marked(ctx *lowering) bool {
	for _, v := range ctx.values {
		if v.kind == types.KindRef && v.raw {
			return true
		}
	}
	return false
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

// setBool pushes a comparison/test result as i1: every caller is an
// eqz/eq/lt/.../is_null/test whose result kind is i1 (matching the interpreter,
// which boxes these through BoxI1). The 0/1 flag still satisfies any later
// i32 operand because kinds compares by representation.
func (l arm64Lowerer) setBool(ctx *lowering, cond uint8) {
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))
	ctx.push(value{reg: flag, kind: types.KindI1, raw: true})
}

// sign32 sign-extends a raw i32's low lane for signed division and
// shifts; zero32 zero-extends it for their unsigned counterparts.
func (l arm64Lowerer) sign32(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SXTW(out, v))
	return out
}

func (l arm64Lowerer) zero32(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(out, v, maskI32))
	return out
}

func (arm64Lowerer) narrow32(v asm.VReg) asm.VReg {
	return asm.NewVReg(v.ID(), v.Type(), asm.Width32)
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
	fail := ctx.queueExit(nil, op.ip, constant)

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

const branchTableLimit = 32

func (l arm64Lowerer) enterBlock(ctx *lowering, state []slot) {
	ctx.values = ctx.values[:0]
	for _, slot := range state {
		ctx.values = append(ctx.values, value{kind: slot.kind, ref: slot.ref, raw: slot.refKnown})
	}
	frame := ctx.frame()
	clear(frame.loaded)
	clear(frame.dirty)
	l.reload(ctx)
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
			return l.next(ctx, block.anchor, target, tail)
		}
		if !l.flush(ctx, false) {
			return false
		}
		return l.path(ctx, block.anchor, target, tail)
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
		return l.exit(ctx, block.term.ip)
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
		label, ok := l.label(ctx, target, join(target.tail, tail))
		if !ok {
			return false
		}
		if block.term.hot == 1 {
			ctx.assembler.Emit(arm64.CBNZLabel(l.narrow32(cond.reg), label))
		} else {
			ctx.assembler.Emit(arm64.CBZLabel(l.narrow32(cond.reg), label))
		}
		return l.next(ctx, block.anchor, block.term.edges[block.term.hot], tail)
	}

	if !l.flush(ctx, false) {
		return false
	}
	taken := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CBNZLabel(l.narrow32(cond.reg), taken))
	if !l.path(ctx, block.anchor, block.term.edges[1], tail) {
		return false
	}
	ctx.assembler.Bind(taken)
	return l.path(ctx, block.anchor, block.term.edges[0], tail)
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
		if !l.path(ctx, block.anchor, block.term.edges[idx], tail) {
			return false
		}
	}
	return true
}

func (l arm64Lowerer) next(ctx *lowering, from anchor, target edge, tail []int) bool {
	tail = join(target.tail, tail)
	target.tail = nil
	if target.anchor.addr == from.addr && target.anchor.ip <= from.ip {
		if !l.flush(ctx, false) {
			return false
		}
		return l.path(ctx, from, target, tail)
	}
	if target.block == noBlock {
		return l.exit(ctx, target.anchor.ip)
	}
	return l.emitBlock(ctx, target.block, tail)
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

func join(steps, tail []int) []int {
	if len(steps) == 0 {
		return tail
	}
	if len(tail) == 0 {
		return steps
	}
	return append(append([]int(nil), steps...), tail...)
}

func (l arm64Lowerer) path(ctx *lowering, from anchor, target edge, tail []int) bool {
	tail = join(target.tail, tail)
	target.tail = nil
	label, ok := l.label(ctx, target, tail)
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
		l.trapFlushed(ctx, trapYield, target.anchor.ip)
		return true
	}
	ctx.assembler.Emit(arm64.BLabel(label))
	return true
}

func (l arm64Lowerer) label(ctx *lowering, target edge, tail []int) (asm.Label, bool) {
	if target.block == noBlock {
		return ctx.queueExit(nil, target.anchor.ip, 0), true
	}
	if target.block < 0 || target.block >= len(ctx.blocks) {
		return 0, false
	}
	block := ctx.blocks[target.block]
	if block.state != nil {
		return ctx.labels[target.block], true
	}
	if l.marked(ctx) || ctx.scheduled >= continuationLimit {
		return ctx.queueExit(nil, target.anchor.ip, 0), true
	}
	label := ctx.assembler.Label()
	work := work{label: label, block: target.block, tail: tail}
	work.values, work.frames = ctx.snapshot()
	ctx.work = append(ctx.work, work)
	ctx.scheduled++
	return label, true
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
	if !l.exit(ctx, op.ip) {
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
