package arm64

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// steps emits the ordinary operations of one normalized block. Control flow
// is owned by the block terminator and never appears here.
func (l lowerer) steps(ctx *lowering, ops []jit.Step) (bool, bool) {
	for idx := 0; idx < len(ops); idx++ {
		op := ops[idx]
		f := ctx.frame()
		if op.Fn != f.addr {
			return false, false
		}
		consumed := l.fuse(ctx, ops, idx)
		if consumed > 0 {
			idx += consumed - 1
			continue
		}
		ok := false
		switch op.Op {
		case instr.NOP:
			ok = true
		case instr.I32_CONST, instr.I64_CONST, instr.F32_CONST, instr.F64_CONST:
			ok = l.constant(ctx, op)
		case instr.CONST_GET:
			if op.Known {
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
			if ctx.count() >= 2 && ctx.values[len(ctx.values)-2].backing == jit.BackingConst && ctx.values[len(ctx.values)-2].ref > 0 {
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
			if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.F32_REM, instr.F32_MOD:
			if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
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
			// Ref equality is a boxed-word compare. With at most one owned
			// operand the compare stays native; two owned operands fall back
			// terminally because releasing both natively risks a double release
			// when the second release deopts after the first already
			// decremented a refcount inline.
			terminal, okEq := l.refEq(ctx, op, op.Op == instr.REF_NE)
			if !okEq {
				return false, false
			}
			if terminal {
				return true, idx == len(ops)-1
			}
			ok = true
		case instr.REF_GET:
			ok = l.refGet(ctx, op)
		case instr.ARRAY_LEN:
			ok = l.arrayLen(ctx, op)
		case instr.ARRAY_SET:
			var terminal bool
			ok, terminal = l.arraySet(ctx, op)
			if !ok {
				return false, false
			}
			if terminal {
				return true, idx == len(ops)-1
			}
		case instr.STRUCT_GET:
			if !l.structGet(ctx, op) {
				return false, false
			}
			ok = true
		case instr.STRUCT_SET:
			var terminal bool
			ok, terminal = l.structSet(ctx, op)
			if !ok {
				return false, false
			}
			if terminal {
				return true, idx == len(ops)-1
			}
		case instr.ERROR_GET:
			ok = l.errorGet(ctx, op)
		case instr.CORO_DONE:
			ok = l.coroDone(ctx, op)
		case instr.CORO_VALUE:
			ok = l.coroValue(ctx, op)
		case instr.STRING_LEN:
			ok = l.stringLen(ctx, op)
		// REF_SET stays threaded because it needs a fresh
		// interface box (an allocation); storing in place is unsound against
		// shared static boxes. REF_TEST/REF_CAST stay threaded because they
		// need structural type equality that an itab guard cannot express.
		// MAP_* stay threaded because they reach into Go map internals the
		// lowerer has no native access to. All of these are bridgeable (see
		// bridgeable in interp/jit_plan.go): the static planner ends its
		// block on the opcode instead of including it here, so this case is
		// reached only when a trace records one as an ordinary mid-block
		// step (see docs/jit-internals.md, Trace Recording) rather than a
		// block terminator; the unconditional exit below still deopts
		// cleanly for that shape.
		case instr.STRING_ENCODE_UTF32,
			instr.STRING_ITER,
			instr.MAP_LEN,
			instr.MAP_GET,
			instr.MAP_LOOKUP,
			instr.MAP_KEYS,
			instr.MAP_ITER,
			instr.REF_TEST,
			instr.REF_CAST:
			if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.ARRAY_FILL, instr.ARRAY_COPY, instr.ARRAY_APPEND, instr.MAP_SET:
			// Bulk mutations stay interpreter-owned: the trace records them as
			// terminal boundaries (also bridgeable for the static planner;
			// see the comment above), so the compiled prefix runs native and
			// this unconditional deopt hands the op to the threaded handler,
			// which performs its own IP advance.
			if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.ERROR_NEW, instr.ERROR_CODE, instr.THROW:
			// Allocation and handler landing stay interpreter-owned (also
			// bridgeable for the static planner; see the comment above).
			// Resume at op.IP because each threaded handler performs its own
			// IP update or handler transfer.
			if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.YIELD, instr.RESUME:
			// True suspension points: deopt to the threaded handler, which runs
			// the real suspend/resume. Resume at op.IP (not op.IP+1) because the
			// YIELD and RESUME handlers perform their own ip advance.
			if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
				return false, false
			}
			return true, idx == len(ops)-1
		case instr.CALL:
			if op.Known {
				ok = l.directCall(ctx, op)
			} else {
				ok = l.call(ctx, op)
			}
		case instr.RETURN_CALL:
			// A tail call back to the trace anchor closes the loop with a native
			// back-edge (terminal); a tail call to another function morphs the
			// current frame into the callee in place and keeps walking.
			if op.Callee == ctx.addr {
				if !l.tailLoop(ctx, op) {
					return false, false
				}
				return true, idx == len(ops)-1
			}
			ok = l.tailMorph(ctx, op)
		case instr.RETURN:
			if len(ctx.frames) > 1 {
				ok = l.stitch(ctx, op.IP)
				break
			}
			if !l.ret(ctx, op.IP) {
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

// fuse lowers an adjacent constant function load and call as one marker.
// It returns the number of source steps consumed; a miss leaves standalone
// lowering untouched.
func (l lowerer) fuse(ctx *lowering, ops []jit.Step, idx int) int {
	if idx+1 >= len(ops) {
		return 0
	}
	source := ops[idx]
	consumer := ops[idx+1]
	if source.Fn != consumer.Fn || source.Depth != consumer.Depth {
		return 0
	}
	width := 1
	for _, operand := range instr.TypeOf(source.Op).Widths {
		if operand < 0 {
			return 0
		}
		width += operand
	}
	if consumer.IP != source.IP+width || source.Op != instr.CONST_GET ||
		!instr.IsCall(consumer.Op) {
		return 0
	}
	constant := int(source.Args[0])
	if constant >= len(ctx.constants) || ctx.constants[constant].Kind() != types.KindRef {
		return 0
	}
	ref := ctx.constants[constant].Ref()
	if ref < 0 || ref >= len(ctx.heap) {
		return 0
	}
	callee := ref
	switch fn := ctx.heap[ref].(type) {
	case *types.Closure:
		callee = int(fn.Fn)
	case *types.Function:
	default:
		return 0
	}
	if callee != consumer.Callee || jit.Resolve(ctx.module, ctx.heap, callee) == nil {
		return 0
	}
	ctx.push(value{fn: callee, kind: types.KindRef, backing: jit.BackingConst, ref: ref})
	return 1
}

// constant pushes an immediate operand. Integer constants keep their known
// compile-time value for downstream folding; floats stay raw bits only.
func (l lowerer) constant(ctx *lowering, op jit.Step) bool {
	out := value{kind: types.KindI32, raw: true}
	bits := op.Args[0]
	switch op.Op {
	case instr.I32_CONST:
		bits = uint64(uint32(bits))
		out.known, out.imm = true, int64(int32(bits))
	case instr.I64_CONST:
		if !types.IsBoxable(int64(bits)) {
			return false
		}
		out.kind = types.KindI64
		out.known, out.imm = true, int64(bits)
	case instr.F32_CONST:
		out.kind = types.KindF32
		bits = uint64(uint32(bits))
	case instr.F64_CONST:
		out.kind = types.KindF64
	}
	out.reg = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(out.reg, bits)...)
	ctx.push(out)
	return true
}

func (l lowerer) unreachable(ctx *lowering, op jit.Step) bool {
	return l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op))
}

// localGet loads local idx and, for a ref, pushes it deferred: jit.BackingLocal
// with slot f.base+idx records that the slot itself still carries the retain,
// so no retain is taken here. A container consumer that later reads this
// operand elides its matching release to balance the missing retain; a
// LOCAL_SET/tail/RETURN that would invalidate the slot detaches this deferral
// into a real retain first.
func (l lowerer) localGet(ctx *lowering, op jit.Step) bool {
	f := ctx.frame()
	idx := int(op.Args[0])
	if idx >= len(f.kinds) {
		return false
	}
	if f.kinds[idx] == types.KindRef {
		f.state[idx] &^= localLoaded
	}
	if !l.loadLocal(ctx, f, idx, op.IP) {
		return false
	}
	v := f.locals[idx]
	if v.kind == types.KindRef {
		v.backing = jit.BackingLocal
		v.slot = f.base + idx
	}
	ctx.push(v)
	return true
}

func (l lowerer) localSet(ctx *lowering, op jit.Step, pop bool) bool {
	f := ctx.frame()
	idx := int(op.Args[0])
	if idx >= len(f.kinds) || ctx.count() < 1 {
		return false
	}
	vp := &ctx.values[len(ctx.values)-1]
	if vp.kind.Repr() != f.kinds[idx].Repr() {
		return false
	}
	if vp.kind == types.KindRef {
		deferred := vp.backing != jit.BackingStack
		boxed, ok := l.box(ctx, *vp)
		if !ok {
			return false
		}
		pre := ctx.pre()
		vStack := ctx.pin(scratchStack)
		addr := l.base(ctx, vStack)
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, addr, int16((f.base+idx)*8)))
		l.releaseOverwritten(ctx, old, boxed, pop && deferred, pre, op.IP)
		if _, ok := l.own(ctx, vp); !ok {
			return false
		}
		if !l.detach(ctx, jit.BackingLocal, f.base+idx) {
			return false
		}
		if !pop {
			l.retainBoxExcept(ctx, old, boxed)
		}
		ctx.assembler.Emit(arm64.STR(boxed, addr, int16((f.base+idx)*8)))
		f.locals[idx] = value{reg: boxed, kind: types.KindRef}
		f.state[idx] |= localLoaded
		if pop {
			ctx.pop()
		}
		return true
	}
	if !vp.raw {
		return false
	}
	if carried := l.carried(ctx, f.base+idx); carried != nil {
		if carried.value.reg.ID() != vp.reg.ID() {
			ctx.assembler.Emit(arm64.MOV(carried.value.reg, vp.reg))
		}
		f.locals[idx] = carried.value
	} else {
		f.locals[idx] = *vp
	}
	f.state[idx] = f.state[idx]&^localStored | localLoaded | localDirty
	if pop {
		ctx.pop()
	}
	return true
}

// globalGet loads a global directly from the globals base. Scalars push
// raw; a ref pushes deferred (jit.BackingGlobal, slot idx): the slot itself
// still carries the retain, so no retain is taken here (see localGet).
func (l lowerer) globalGet(ctx *lowering, op jit.Step) bool {
	idx, kind, ok := l.global(ctx, op)
	if !ok {
		return false
	}
	base := ctx.pin(scratchGlobals)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, base, int16(idx*8)))
	if kind == types.KindI64 {
		if !l.guardI64(ctx, dst, op.IP) {
			return false
		}
		dst = l.sign64(ctx, dst)
	}
	if kind == types.KindRef {
		ctx.push(value{reg: dst, kind: kind, backing: jit.BackingGlobal, slot: idx})
		return true
	}
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

// globalSet boxes the top value and stores it to the global. Ref-capable
// slots release the overwritten runtime ref before the store.
func (l lowerer) globalSet(ctx *lowering, op jit.Step, pop bool) bool {
	idx, kind, ok := l.global(ctx, op)
	if !ok {
		return false
	}
	if ctx.count() < 1 {
		return false
	}
	vp := &ctx.values[len(ctx.values)-1]
	if kind == types.KindRef {
		if vp.kind != types.KindRef {
			return false
		}
	} else if vp.kind != kind || !vp.raw {
		return false
	}
	var boxed asm.VReg
	deferred := kind == types.KindRef && vp.backing != jit.BackingStack
	boxed, ok = l.box(ctx, *vp)
	if !ok {
		return false
	}
	base := ctx.pin(scratchGlobals)
	if kind == types.KindRef {
		pre := ctx.pre()
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, base, int16(idx*8)))
		l.releaseOverwritten(ctx, old, boxed, pop && deferred, pre, op.IP)
		if _, ok := l.own(ctx, vp); !ok {
			return false
		}
		if !l.detach(ctx, jit.BackingGlobal, idx) {
			return false
		}
		if !pop {
			l.retainBoxExcept(ctx, old, boxed)
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
// Slots), so GLOBAL_GET/SET see a stable kind at lower time: a per-run
// input is seeded via SetGlobal before Run, so the entry trace already observes
// it. Out-of-range indices and offsets past the 12-bit LDR/STR limit reject.
func (l lowerer) global(ctx *lowering, op jit.Step) (int, types.Kind, bool) {
	idx := int(op.Args[0])
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

func (l lowerer) drop(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 1 {
		return false
	}
	pre := ctx.pre()
	v := ctx.values[len(ctx.values)-1]
	if v.kind == types.KindRef && v.backing == jit.BackingStack {
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		l.releaseBox(ctx, boxed, pre, op.IP)
	}
	ctx.pop()
	return true
}

// dup duplicates the top operand. A deferred ref (backing != jit.BackingStack) is
// still backed by its slot, so the duplicate stays deferred with the same
// backing/slot and no retain; an owned ref takes a fresh retain for the copy.
func (l lowerer) dup(ctx *lowering) bool {
	if ctx.count() < 1 {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind == types.KindRef && v.backing == jit.BackingStack {
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		l.retainBox(ctx, boxed)
		v = value{reg: boxed, kind: types.KindRef}
	}
	ctx.push(v)
	return true
}

func (l lowerer) swap(ctx *lowering) bool {
	if ctx.count() < 2 {
		return false
	}
	last := len(ctx.values) - 1
	ctx.values[last], ctx.values[last-1] = ctx.values[last-1], ctx.values[last]
	return true
}

func (l lowerer) selectOp(ctx *lowering) bool {
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

func (l lowerer) constGetKnown(ctx *lowering, op jit.Step) bool {
	idx := int(op.Args[0])
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
		ctx.push(value{kind: types.KindRef, backing: jit.BackingConst, ref: ref})
		return true
	default:
		return l.constGet(ctx, op)
	}
}

// constGet pushes a scalar constant as an unboxed immediate. Refs retain
// ordinary standalone ownership; call-target fusion owns direct markers.
func (l lowerer) constGet(ctx *lowering, op jit.Step) bool {
	idx := int(op.Args[0])
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
		ctx.push(value{reg: boxed, kind: types.KindRef, ref: ref})
		return true
	}
	return false
}
