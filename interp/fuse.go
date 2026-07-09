package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// fuseRefImm tries to fuse a constant ref load with the following instruction.
// Returns nil if no fusion pattern applies.
func (c *threader) fuseRefImm(addr int, size int) func(*Interpreter) {
	switch v := c.heap[addr].(type) {
	case types.I64:
		if fused := c.fuseI64Imm(int64(v), size); fused != nil {
			return fused
		}
	case *types.Closure:
		if fused := c.fuseClosure(v, addr); fused != nil {
			return fused
		}
	case *types.Function:
		if fused := c.fuseFunction(v, addr); fused != nil {
			return fused
		}
	case *HostFunction:
		if fused := c.fuseHostFunction(v, size); fused != nil {
			return fused
		}
	}
	return nil
}

// fuseFunction tries to fuse a function ref load with a following CALL.
// Returns nil if no fusion pattern applies.
func (c *threader) fuseFunction(fn *types.Function, addr int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.CALL:
		// A coroutine-function's CALL must allocate a Coroutine and tag the frame,
		// which only the generic CALL handler does. Leave it unfused so the plain
		// CONST_GET; CALL pair runs and the coroutine path is taken.
		if containsYield(fn.Code) {
			return nil
		}
		params := len(fn.Typ.Params)
		returns := len(fn.Typ.Returns)
		locals := len(fn.Locals)
		return func(i *Interpreter) {
			if i.fp == len(i.frames) {
				panic(ErrFrameOverflow)
			}
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.sp+locals > len(i.stack) {
				panic(ErrStackOverflow)
			}
			if locals > 0 {
				clear(i.stack[i.sp : i.sp+locals])
			}
			i.fr.ip += 4
			f := &i.frames[i.fp]
			f.code = i.code[addr]
			f.upvals = nil
			f.addr = addr
			f.ref = addr
			f.ip = 0
			f.bp = i.sp - params
			f.returns = returns
			f.release = false
			f.coro = 0
			i.sp += locals
			i.fp++
			i.fr = f
		}
	case instr.RETURN_CALL:
		if containsYield(fn.Code) {
			return nil
		}
		params := len(fn.Typ.Params)
		returns := len(fn.Typ.Returns)
		locals := len(fn.Locals)
		return func(i *Interpreter) {
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.fp == 1 {
				if i.fp == len(i.frames) {
					panic(ErrFrameOverflow)
				}
				if i.sp+locals > len(i.stack) {
					panic(ErrStackOverflow)
				}
				if locals > 0 {
					clear(i.stack[i.sp : i.sp+locals])
				}
				i.fr.ip += 4
				f := &i.frames[i.fp]
				f.code = i.code[addr]
				f.upvals = nil
				f.addr = addr
				f.ref = addr
				f.ip = 0
				f.bp = i.sp - params
				f.returns = returns
				f.release = false
				f.coro = 0
				i.sp += locals
				i.fp++
				i.fr = f
				return
			}
			f := i.fr
			base := f.bp
			if base+params+locals > len(i.stack) {
				panic(ErrStackOverflow)
			}
			copy(i.stack[base:base+params], i.stack[i.sp-params:i.sp])
			if f.release {
				i.release(f.ref)
			}
			if locals > 0 {
				clear(i.stack[base+params : base+params+locals])
			}
			f.code = i.code[addr]
			f.upvals = nil
			f.addr = addr
			f.ref = addr
			f.ip = 0
			f.bp = base
			f.returns = returns
			f.release = false
			f.coro = 0
			i.sp = base + params + locals
		}
	case instr.CLOSURE_NEW:
		captures := len(fn.Captures)
		return func(i *Interpreter) {
			if i.sp < captures {
				panic(ErrStackUnderflow)
			}
			upvals := make([]types.Boxed, captures)
			copy(upvals, i.stack[i.sp-captures:i.sp])
			cl := types.NewClosure(fn.Typ, types.Ref(addr), upvals)
			i.retain(addr)
			caddr := i.keep(cl)
			i.sp -= captures
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = types.BoxRef(caddr)
			i.sp++
			i.fr.ip += 4
		}
	}
	return nil
}

func (c *threader) fuseClosure(fn *types.Closure, addr int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.CALL:
		tmpl, ok := c.heap[fn.Fn].(*types.Function)
		if !ok {
			return nil
		}
		// Coroutine closures take the generic CALL path so the Coroutine handle is
		// allocated and the frame tagged; see fuseFunction.
		if containsYield(tmpl.Code) {
			return nil
		}
		params := len(fn.Typ.Params)
		returns := len(fn.Typ.Returns)
		locals := len(tmpl.Locals)
		return func(i *Interpreter) {
			if i.fp == len(i.frames) {
				panic(ErrFrameOverflow)
			}
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.sp+locals > len(i.stack) {
				panic(ErrStackOverflow)
			}
			if locals > 0 {
				clear(i.stack[i.sp : i.sp+locals])
			}
			i.fr.ip += 4
			f := &i.frames[i.fp]
			f.code = i.code[fn.Fn]
			f.upvals = fn.Upvals
			f.addr = int(fn.Fn)
			f.ref = addr
			f.ip = 0
			f.bp = i.sp - params
			f.returns = returns
			f.release = false
			f.coro = 0
			i.sp += locals
			i.fp++
			i.fr = f
		}
	case instr.RETURN_CALL:
		tmpl, ok := c.heap[fn.Fn].(*types.Function)
		if !ok {
			return nil
		}
		if containsYield(tmpl.Code) {
			return nil
		}
		params := len(fn.Typ.Params)
		returns := len(fn.Typ.Returns)
		locals := len(tmpl.Locals)
		return func(i *Interpreter) {
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.fp == 1 {
				if i.fp == len(i.frames) {
					panic(ErrFrameOverflow)
				}
				if i.sp+locals > len(i.stack) {
					panic(ErrStackOverflow)
				}
				if locals > 0 {
					clear(i.stack[i.sp : i.sp+locals])
				}
				i.fr.ip += 4
				f := &i.frames[i.fp]
				f.code = i.code[fn.Fn]
				f.upvals = fn.Upvals
				f.addr = int(fn.Fn)
				f.ref = addr
				f.ip = 0
				f.bp = i.sp - params
				f.returns = returns
				f.release = false
				f.coro = 0
				i.sp += locals
				i.fp++
				i.fr = f
				return
			}
			f := i.fr
			base := f.bp
			if base+params+locals > len(i.stack) {
				panic(ErrStackOverflow)
			}
			copy(i.stack[base:base+params], i.stack[i.sp-params:i.sp])
			if f.release {
				i.release(f.ref)
			}
			if locals > 0 {
				clear(i.stack[base+params : base+params+locals])
			}
			f.code = i.code[fn.Fn]
			f.upvals = fn.Upvals
			f.addr = int(fn.Fn)
			f.ref = addr
			f.ip = 0
			f.bp = base
			f.returns = returns
			f.release = false
			f.coro = 0
			i.sp = base + params + locals
		}
	}
	return nil
}

func (c *threader) fuseHostFunction(fn *HostFunction, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	params := len(fn.Typ.Params)
	returns := len(fn.Typ.Returns)
	refs := false
	for _, p := range fn.Typ.Params {
		switch p.Kind().Repr() {
		case types.KindI32, types.KindF32, types.KindF64:
		default:
			refs = true
		}
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.CALL:
		return func(i *Interpreter) {
			delta := returns - params
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.sp+delta > len(i.stack) {
				panic(ErrStackOverflow)
			}
			args := i.stack[i.sp-params : i.sp]
			out, err := fn.Fn(i, args)
			if err != nil {
				panic(err)
			}
			if refs {
				i.releaseArgs(args, out)
			}
			i.sp += delta
			copy(i.stack[i.sp-returns:i.sp], out)
			i.fr.ip += size + 1
		}
	case instr.RETURN_CALL:
		return func(i *Interpreter) {
			delta := returns - params
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.sp+delta > len(i.stack) {
				panic(ErrStackOverflow)
			}
			args := i.stack[i.sp-params : i.sp]
			out, err := fn.Fn(i, args)
			if err != nil {
				panic(err)
			}
			if refs {
				i.releaseArgs(args, out)
			}
			i.sp += delta
			copy(i.stack[i.sp-returns:i.sp], out)
			i.fr.ip += size + 1
			if i.fp > 1 {
				i.ret()
			}
		}
	}
	return nil
}

// fuseLocalConst folds LOCAL_GET idx; <kind>.CONST c; <kind binop> into one
// dispatch: it reads the typed local directly out of the frame slot, computes
// the result against the compile-time constant with the existing
// i32Add/i64Add/etc. helper methods, and pushes the result once. There is no
// intermediate stack write for the local's own value and no extra bounds
// check for a slot that was never really pushed. The local kind selects the
// matching CONST opcode, immediate width, and per-kind case switch so all
// four numeric kinds are handled the same way. Narrow I1/I8 locals fall
// through to the plain LOCAL_GET path, matching fuseLocalLocal. Returns nil
// when no pattern applies.
//
// c.ip is restored after probing the binop so the compile loop still emits
// standalone handlers for the absorbed CONST and binop, keeping branch
// targets valid. I64 locals may hold a heap-promoted KindRef, and unboxI64
// releases a ref operand once read, so the local's boxed value is retained
// first to keep the local slot's own reference balanced; I32/F32/F64 never
// box to a ref and skip the retain, matching the plain LOCAL_GET fast path.
func (c *threader) fuseLocalConst(idx int) func(*Interpreter) {
	if c.exact || idx >= len(c.locals) {
		return nil
	}

	switch c.locals[idx] {
	case types.KindI32:
		if c.ip+5 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.I32_CONST {
			return nil
		}
		cst := *(*int32)(unsafe.Pointer(&c.code[c.ip+1]))
		save := c.ip
		c.ip += 5
		fused := c.fuseLocalI32Const(idx, cst, 5)
		c.ip = save
		return fused
	case types.KindI64:
		if c.ip+9 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.I64_CONST {
			return nil
		}
		cst := int64(*(*uint64)(unsafe.Pointer(&c.code[c.ip+1])))
		save := c.ip
		c.ip += 9
		fused := c.fuseLocalI64Const(idx, cst, 9)
		c.ip = save
		return fused
	case types.KindF32:
		if c.ip+5 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.F32_CONST {
			return nil
		}
		cst := *(*float32)(unsafe.Pointer(&c.code[c.ip+1]))
		save := c.ip
		c.ip += 5
		fused := c.fuseLocalF32Const(idx, cst, 5)
		c.ip = save
		return fused
	case types.KindF64:
		if c.ip+9 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.F64_CONST {
			return nil
		}
		cst := *(*float64)(unsafe.Pointer(&c.code[c.ip+1]))
		save := c.ip
		c.ip += 9
		fused := c.fuseLocalF64Const(idx, cst, 9)
		c.ip = save
		return fused
	}
	return nil
}

// fuseLocalLocal folds LOCAL_GET idxA; LOCAL_GET idxB; <binop> into one
// dispatch when both locals share the same declared kind: it reads both
// frame slots directly and pushes the result once, skipping the double
// push/pop round trip the unfused sequence would otherwise do. Only locals
// declared exactly KindI32/I64/F32/F64 participate (matching fuseLocalConst);
// narrower I1/I8 locals fall through to the plain LOCAL_GET path. Returns nil
// when no pattern applies.
//
// c.ip is restored after probing the second LOCAL_GET and the binop so the
// compile loop still emits standalone handlers for both, keeping branch
// targets valid. I64 locals may hold a heap-promoted KindRef, so both
// operands are retained before unboxI64 releases them, keeping each local
// slot's own reference balanced; I32/F32/F64 never box to a ref and skip the
// retain.
func (c *threader) fuseLocalLocal(idxA int) func(*Interpreter) {
	if c.exact || idxA >= len(c.locals) || c.ip+1 >= len(c.code) {
		return nil
	}
	if instr.Opcode(c.code[c.ip]) != instr.LOCAL_GET {
		return nil
	}
	idxB := int(c.code[c.ip+1])
	// idxA == idxB is valid: the unfused program reads the same slot twice.
	if idxB >= len(c.locals) || c.locals[idxB] != c.locals[idxA] {
		return nil
	}

	save := c.ip
	c.ip += 2
	var fused func(*Interpreter)
	switch c.locals[idxA] {
	case types.KindI32:
		fused = c.fuseLocalLocalI32(idxA, idxB)
	case types.KindI64:
		fused = c.fuseLocalLocalI64(idxA, idxB)
	case types.KindF32:
		fused = c.fuseLocalLocalF32(idxA, idxB)
	case types.KindF64:
		fused = c.fuseLocalLocalF64(idxA, idxB)
	}
	c.ip = save
	return fused
}

// peekBrIf reports whether the byte at pos starts a BR_IF instruction,
// returning its parsed jump offset for a comparison+BR_IF (or CONST+BR_IF)
// fusion to apply via Interpreter.branchIf. It only reads c.code — it never
// advances c.ip — so every caller, whether already sitting right after its
// own opcode (pos == c.ip) or still probing one opcode further ahead
// (pos == c.ip+1, from inside an already-probing fuseLocalXConst case), needs
// no restore: the compile loop still visits and standalone-compiles BR_IF's
// own start byte, keeping that offset a valid branch target. BR_IF's fixed
// 3-byte width (opcode + i16 offset) is a constant every caller already
// knows and folds into its own total fused width.
func (c *threader) peekBrIf(pos int) (offset int, ok bool) {
	if c.exact || pos+2 >= len(c.code) || instr.Opcode(c.code[pos]) != instr.BR_IF {
		return 0, false
	}
	return instr.ParseI16(c.code, pos+1), true
}

// fuseLocalI32Const builds the fused closure for LOCAL_GET idx (I32);
// I32_CONST cst; <binop>, mirroring fuseI32Imm's opcode dispatch but reading
// the local directly instead of through an already-pushed stack slot and
// pushing the result once. Does not touch c.ip; fuseLocalConst restores it
// after probing. ARRAY_GET/STRUCT_GET are not handled here: an I32 local can
// never hold the array/struct ref those opcodes need, so the combination
// cannot occur in valid bytecode.
func (c *threader) fuseLocalI32Const(idx int, cst int32, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I32_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Add(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Sub(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Mul(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_DIV_S:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32DivS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_DIV_U:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32DivU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_REM_S:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32RemS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_REM_U:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32RemU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_SHL:
		cst &= 0x1F
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Shl(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_SHR_S:
		cst &= 0x1F
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32ShrS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_SHR_U:
		cst &= 0x1F
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32ShrU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_XOR:
		rhs := types.BoxI32(cst)
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr]
			i.stack[i.sp] = i.i32Xor(lhs, rhs)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_AND:
		rhs := types.BoxI32(cst)
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr]
			i.stack[i.sp] = i.i32And(lhs, rhs)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_OR:
		rhs := types.BoxI32(cst)
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr]
			i.stack[i.sp] = i.i32Or(lhs, rhs)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_ROTL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Rotl(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_ROTR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Rotr(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_EQ:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_EQ; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(lhs == cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Eq(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_NE:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_NE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(lhs != cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32Ne(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_LT_S:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_LT_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(lhs < cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32LtS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_LT_U:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_LT_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(uint32(lhs) < uint32(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32LtU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_GT_S:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_GT_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(lhs > cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32GtS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_GT_U:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_GT_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(uint32(lhs) > uint32(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32GtU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_LE_S:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_LE_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(lhs <= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32LeS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_LE_U:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_LE_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(uint32(lhs) <= uint32(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32LeU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_GE_S:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_GE_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(lhs >= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32GeS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I32_GE_U:
		// Superinstruction: LOCAL_GET idx; I32_CONST cst; I32_GE_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].I32()
				i.branchIf(uint32(lhs) >= uint32(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].I32()
			i.stack[i.sp] = i.i32GeU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	}
	return nil
}

// fuseLocalI64Const builds the fused closure for LOCAL_GET idx (I64);
// I64_CONST cst; <binop>, mirroring fuseI64Imm's opcode dispatch but reading
// the local directly instead of through an already-pushed stack slot and
// pushing the result once. Does not touch c.ip; fuseLocalConst restores it
// after probing. The local's boxed value is retained before unboxI64 reads
// it: unboxI64 releases a heap-promoted ref once read, and without the
// retain that would drop the local slot's own ownership instead of just the
// temporary read.
func (c *threader) fuseLocalI64Const(idx int, cst int64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I64_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Add(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Sub(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Mul(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_DIV_S:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64DivS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_DIV_U:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64DivU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_REM_S:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64RemS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_REM_U:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64RemU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_SHL:
		cst &= 0x3F
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Shl(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_SHR_S:
		cst &= 0x3F
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64ShrS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_SHR_U:
		cst &= 0x3F
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64ShrU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_XOR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Xor(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_AND:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64And(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_OR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Or(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_ROTL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Rotl(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_ROTR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Rotr(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_EQ:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_EQ; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(lhs == cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Eq(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_NE:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_NE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(lhs != cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64Ne(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_LT_S:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_LT_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(lhs < cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64LtS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_LT_U:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_LT_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(uint64(lhs) < uint64(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64LtU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_GT_S:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_GT_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(lhs > cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64GtS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_GT_U:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_GT_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(uint64(lhs) > uint64(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64GtU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_LE_S:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_LE_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(lhs <= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64LeS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_LE_U:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_LE_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(uint64(lhs) <= uint64(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64LeU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_GE_S:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_GE_S; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(lhs >= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64GeS(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.I64_GE_U:
		// Superinstruction: LOCAL_GET idx; I64_CONST cst; I64_GE_U; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		// The retain-before-unbox dance is unchanged: it protects the local
		// slot's own reference exactly as the non-branching path below does.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				val := i.stack[addr]
				i.retainBox(val)
				lhs := i.unboxI64(val)
				i.branchIf(uint64(lhs) >= uint64(cst), offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			lhs := i.unboxI64(val)
			i.stack[i.sp] = i.i64GeU(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	}
	return nil
}

// fuseLocalF32Const builds the fused closure for LOCAL_GET idx (F32);
// F32_CONST cst; <binop>, mirroring fuseF32Imm's opcode dispatch but reading
// the local directly instead of through an already-pushed stack slot and
// pushing the result once. Does not touch c.ip; fuseLocalConst restores it
// after probing. F32 never boxes to a heap ref, so no retain is needed.
func (c *threader) fuseLocalF32Const(idx int, cst float32, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F32_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Add(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Sub(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Mul(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_DIV:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Div(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_REM:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Rem(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_MOD:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Mod(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_MIN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Min(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_MAX:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Max(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Copysign(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_EQ:
		// Superinstruction: LOCAL_GET idx; F32_CONST cst; F32_EQ; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F32()
				i.branchIf(lhs == cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Eq(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_NE:
		// Superinstruction: LOCAL_GET idx; F32_CONST cst; F32_NE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F32()
				i.branchIf(lhs != cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Ne(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_LT:
		// Superinstruction: LOCAL_GET idx; F32_CONST cst; F32_LT; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F32()
				i.branchIf(lhs < cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Lt(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_GT:
		// Superinstruction: LOCAL_GET idx; F32_CONST cst; F32_GT; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F32()
				i.branchIf(lhs > cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Gt(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_LE:
		// Superinstruction: LOCAL_GET idx; F32_CONST cst; F32_LE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F32()
				i.branchIf(lhs <= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Le(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F32_GE:
		// Superinstruction: LOCAL_GET idx; F32_CONST cst; F32_GE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F32()
				i.branchIf(lhs >= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F32()
			i.stack[i.sp] = i.f32Ge(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	}
	return nil
}

// fuseLocalF64Const builds the fused closure for LOCAL_GET idx (F64);
// F64_CONST cst; <binop>, mirroring fuseF64Imm's opcode dispatch but reading
// the local directly instead of through an already-pushed stack slot and
// pushing the result once. Does not touch c.ip; fuseLocalConst restores it
// after probing. F64 never boxes to a heap ref, so no retain is needed.
func (c *threader) fuseLocalF64Const(idx int, cst float64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F64_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Add(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Sub(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Mul(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_DIV:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Div(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_REM:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Rem(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_MOD:
		if cst == 0 {
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Mod(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_MIN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Min(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_MAX:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Max(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Copysign(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_EQ:
		// Superinstruction: LOCAL_GET idx; F64_CONST cst; F64_EQ; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F64()
				i.branchIf(lhs == cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Eq(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_NE:
		// Superinstruction: LOCAL_GET idx; F64_CONST cst; F64_NE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F64()
				i.branchIf(lhs != cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Ne(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_LT:
		// Superinstruction: LOCAL_GET idx; F64_CONST cst; F64_LT; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F64()
				i.branchIf(lhs < cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Lt(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_GT:
		// Superinstruction: LOCAL_GET idx; F64_CONST cst; F64_GT; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F64()
				i.branchIf(lhs > cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Gt(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_LE:
		// Superinstruction: LOCAL_GET idx; F64_CONST cst; F64_LE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F64()
				i.branchIf(lhs <= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Le(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	case instr.F64_GE:
		// Superinstruction: LOCAL_GET idx; F64_CONST cst; F64_GE; BR_IF collapses
		// the 4-instruction loop-header shape into one dispatch, reusing the
		// branch-taking logic instead of also pushing/popping a boxed bool.
		if offset, ok := c.peekBrIf(c.ip + 1); ok {
			return func(i *Interpreter) {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				lhs := i.stack[addr].F64()
				i.branchIf(lhs >= cst, offset, size+6)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addr].F64()
			i.stack[i.sp] = i.f64Ge(lhs, cst)
			i.sp++
			i.fr.ip += size + 3
		}
	}
	return nil
}

// fuseLocalLocalI32 builds the fused closure for LOCAL_GET idxA (I32);
// LOCAL_GET idxB (I32); <binop>: it reads both frame slots directly and
// pushes the result once. Does not touch c.ip; fuseLocalLocal restores it
// after probing. I32 never boxes to a heap ref, so no retain is needed.
func (c *threader) fuseLocalLocalI32(idxA, idxB int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I32_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32Add(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32Sub(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32Mul(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_DIV_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32DivS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_DIV_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32DivU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_REM_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32RemS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_REM_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32RemU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_SHL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32() & 0x1F
			i.stack[i.sp] = i.i32Shl(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_SHR_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32() & 0x1F
			i.stack[i.sp] = i.i32ShrS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_SHR_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32() & 0x1F
			i.stack[i.sp] = i.i32ShrU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_XOR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA]
			rhs := i.stack[addrB]
			i.stack[i.sp] = i.i32Xor(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_AND:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA]
			rhs := i.stack[addrB]
			i.stack[i.sp] = i.i32And(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_OR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA]
			rhs := i.stack[addrB]
			i.stack[i.sp] = i.i32Or(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_ROTL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32Rotl(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_ROTR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32Rotr(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32Eq(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32Ne(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_LT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32LtS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_LT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32LtU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_GT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32GtS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_GT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32GtU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_LE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32LeS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_LE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32LeU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_GE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32GeS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I32_GE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].I32()
			rhs := i.stack[addrB].I32()
			i.stack[i.sp] = i.i32GeU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	}
	return nil
}

// fuseLocalLocalI64 builds the fused closure for LOCAL_GET idxA (I64);
// LOCAL_GET idxB (I64); <binop>: it reads both frame slots directly and
// pushes the result once. Does not touch c.ip; fuseLocalLocal restores it
// after probing. Both operands are retained before unboxI64 reads them:
// unboxI64 releases a heap-promoted ref once read, and without the retain
// that would drop each local slot's own ownership instead of just the
// temporary read.
func (c *threader) fuseLocalLocalI64(idxA, idxB int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I64_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Add(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Sub(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Mul(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_DIV_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64DivS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_DIV_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64DivU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_REM_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64RemS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_REM_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64RemU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_SHL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)&0x3F
			i.stack[i.sp] = i.i64Shl(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_SHR_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)&0x3F
			i.stack[i.sp] = i.i64ShrS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_SHR_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)&0x3F
			i.stack[i.sp] = i.i64ShrU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_XOR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Xor(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_AND:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64And(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_OR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Or(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_ROTL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Rotl(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_ROTR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Rotr(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Eq(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64Ne(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_LT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64LtS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_LT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64LtU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_GT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64GtS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_GT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64GtU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_LE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64LeS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_LE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64LeU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_GE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64GeS(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.I64_GE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			valA, valB := i.stack[addrA], i.stack[addrB]
			i.retainBox(valA)
			i.retainBox(valB)
			lhs, rhs := i.unboxI64(valA), i.unboxI64(valB)
			i.stack[i.sp] = i.i64GeU(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	}
	return nil
}

// fuseLocalLocalF32 builds the fused closure for LOCAL_GET idxA (F32);
// LOCAL_GET idxB (F32); <binop>: it reads both frame slots directly and
// pushes the result once. Does not touch c.ip; fuseLocalLocal restores it
// after probing. F32 never boxes to a ref, so no retain is needed (matching
// fuseLocalLocalI32's plain-scalar handling).
func (c *threader) fuseLocalLocalF32(idxA, idxB int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F32_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Add(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Sub(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Mul(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_DIV:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Div(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_REM:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Rem(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_MOD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Mod(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_MIN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Min(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_MAX:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Max(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Copysign(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Eq(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Ne(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_LT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Lt(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_GT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Gt(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_LE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Le(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F32_GE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F32()
			rhs := i.stack[addrB].F32()
			i.stack[i.sp] = i.f32Ge(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	}
	return nil
}

// fuseLocalLocalF64 builds the fused closure for LOCAL_GET idxA (F64);
// LOCAL_GET idxB (F64); <binop>: it reads both frame slots directly and
// pushes the result once. Does not touch c.ip; fuseLocalLocal restores it
// after probing. F64 never boxes to a ref, so no retain is needed (matching
// fuseLocalLocalI32's plain-scalar handling).
func (c *threader) fuseLocalLocalF64(idxA, idxB int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F64_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Add(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Sub(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Mul(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_DIV:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Div(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_REM:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Rem(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_MOD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Mod(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_MIN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Min(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_MAX:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Max(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Copysign(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Eq(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Ne(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_LT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Lt(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_GT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Gt(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_LE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Le(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	case instr.F64_GE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addrA, addrB := i.fr.bp+idxA, i.fr.bp+idxB
			if addrA > i.sp || addrB > i.sp {
				panic(ErrSegmentationFault)
			}
			lhs := i.stack[addrA].F64()
			rhs := i.stack[addrB].F64()
			i.stack[i.sp] = i.f64Ge(lhs, rhs)
			i.sp++
			i.fr.ip += 5
		}
	}
	return nil
}

func (c *threader) fuseI32(rhs func(*Interpreter) int32, kind types.Kind, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I32_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_DIV_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32DivS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_DIV_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32DivU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_REM_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32RemS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_REM_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32RemU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SHL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			rhs &= 0x1F
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Shl(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SHR_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			rhs &= 0x1F
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ShrS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SHR_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			rhs &= 0x1F
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ShrU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_XOR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := types.Box(uint64(uint32(rhs(i))), kind)
			lhs := i.stack[i.sp-1]
			i.stack[i.sp-1] = i.i32Xor(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_AND:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := types.Box(uint64(uint32(rhs(i))), kind)
			lhs := i.stack[i.sp-1]
			i.stack[i.sp-1] = i.i32And(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_OR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := types.Box(uint64(uint32(rhs(i))), kind)
			lhs := i.stack[i.sp-1]
			i.stack[i.sp-1] = i.i32Or(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_ROTL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = i.i32Rotl(int32(lhs), rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_ROTR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = i.i32Rotr(int32(lhs), rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	}
	return nil
}

func (c *threader) fuseI32Imm(rhs int32, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I32_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_DIV_S:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32DivS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_DIV_U:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32DivU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_REM_S:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32RemS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_REM_U:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32RemU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SHL:
		rhs &= 0x1F
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Shl(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SHR_S:
		rhs &= 0x1F
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ShrS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_SHR_U:
		rhs &= 0x1F
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ShrU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_XOR:
		rhs := types.BoxI32(rhs)
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1]
			i.stack[i.sp-1] = i.i32Xor(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_AND:
		rhs := types.BoxI32(rhs)
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1]
			i.stack[i.sp-1] = i.i32And(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_OR:
		rhs := types.BoxI32(rhs)
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1]
			i.stack[i.sp-1] = i.i32Or(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_ROTL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Rotl(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_ROTR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Rotr(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_LE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32LeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I32_GE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32GeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.ARRAY_GET:
		return func(i *Interpreter) {
			val := i.arrayGetAt(int(rhs))
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.STRUCT_GET:
		return func(i *Interpreter) {
			val := i.structGetAt(int(rhs))
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += size + 1
		}
	}
	return nil
}

func (c *threader) fuseI64(rhs func(*Interpreter) int64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I64_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_DIV_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64DivS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_DIV_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64DivU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_REM_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64RemS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_REM_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64RemU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SHL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i) & 0x3F
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Shl(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SHR_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i) & 0x3F
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ShrS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SHR_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i) & 0x3F
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ShrU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_XOR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Xor(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_AND:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64And(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_OR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Or(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_ROTL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			amount := int(rhs(i))
			lhs := uint64(i.unboxI64(i.stack[i.sp-1]))
			i.stack[i.sp-1] = i.i64Rotl(int64(lhs), int64(amount))
			i.fr.ip += size + 1
		}
	case instr.I64_ROTR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			amount := int(rhs(i))
			lhs := uint64(i.unboxI64(i.stack[i.sp-1]))
			i.stack[i.sp-1] = i.i64Rotr(int64(lhs), int64(amount))
			i.fr.ip += size + 1
		}
	case instr.I64_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	}
	return nil
}

func (c *threader) fuseI64Imm(rhs int64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I64_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_DIV_S:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64DivS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_DIV_U:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64DivU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_REM_S:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64RemS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_REM_U:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64RemU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SHL:
		rhs &= 0x3F
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Shl(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SHR_S:
		rhs &= 0x3F
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ShrS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_SHR_U:
		rhs &= 0x3F
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ShrU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_XOR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Xor(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_AND:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64And(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_OR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Or(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_ROTL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Rotl(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_ROTR:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Rotr(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GT_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GtS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GT_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GtU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_LE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64LeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GE_S:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GeS(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.I64_GE_U:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64GeU(lhs, rhs)
			i.fr.ip += size + 1
		}
	}
	return nil
}

func (c *threader) fuseF32(rhs func(*Interpreter) float32, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F32_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_DIV:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Div(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_REM:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Rem(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MOD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Mod(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MIN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Min(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MAX:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Max(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Copysign(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_LT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Lt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_GT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Gt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_LE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Le(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_GE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Ge(lhs, rhs)
			i.fr.ip += size + 1
		}
	}
	return nil
}

func (c *threader) fuseF32Imm(rhs float32, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F32_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_DIV:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Div(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_REM:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Rem(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MOD:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Mod(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MIN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Min(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_MAX:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Max(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Copysign(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_LT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Lt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_GT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Gt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_LE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Le(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F32_GE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Ge(lhs, rhs)
			i.fr.ip += size + 1
		}
	}
	return nil
}

func (c *threader) fuseF64(rhs func(*Interpreter) float64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F64_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_DIV:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Div(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_REM:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Rem(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MOD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Mod(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MIN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Min(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MAX:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Max(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Copysign(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_LT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Lt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_GT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Gt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_LE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Le(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_GE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			rhs := rhs(i)
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Ge(lhs, rhs)
			i.fr.ip += size + 1
		}
	}
	return nil
}

func (c *threader) fuseF64Imm(rhs float64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F64_ADD:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Add(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_SUB:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Sub(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MUL:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Mul(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_DIV:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Div(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_REM:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Rem(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MOD:
		if rhs == 0 {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				panic(ErrDivideByZero)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Mod(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MIN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Min(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_MAX:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Max(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Copysign(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_EQ:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Eq(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_NE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Ne(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_LT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Lt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_GT:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Gt(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_LE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Le(lhs, rhs)
			i.fr.ip += size + 1
		}
	case instr.F64_GE:
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			lhs := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Ge(lhs, rhs)
			i.fr.ip += size + 1
		}
	}
	return nil
}

// fusePairI32 folds <I32 lhs source>; <I32 rhs source>; <binop> into one
// dispatch when both operands come from loaders instead of the stack: it
// calls lhs and rhs directly and pushes the result once, skipping the
// double push/pop round trip the unfused sequence would otherwise do.
// Mirrors fuseI32's opcode switch, but neither operand is popped from the
// stack (both loaders already resolve their own value), so the check
// guards a push instead of a pop and ip advances by size+1 (size covers
// both source instructions; +1 covers the binop). Returns nil when c.exact
// or no pattern matches.
func (c *threader) fusePairI32(lhs, rhs func(*Interpreter) int32, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I32_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32Add(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32Sub(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32Mul(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_DIV_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32DivS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_DIV_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32DivU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_REM_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32RemS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_REM_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i32RemU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_SHL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)&0x1F
			i.stack[i.sp] = i.i32Shl(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_SHR_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)&0x1F
			i.stack[i.sp] = i.i32ShrS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_SHR_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)&0x1F
			i.stack[i.sp] = i.i32ShrU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_XOR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhsVal := types.BoxI32(lhs(i))
			rhsVal := types.BoxI32(rhs(i))
			i.stack[i.sp] = i.i32Xor(lhsVal, rhsVal)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_AND:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhsVal := types.BoxI32(lhs(i))
			rhsVal := types.BoxI32(rhs(i))
			i.stack[i.sp] = i.i32And(lhsVal, rhsVal)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_OR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhsVal := types.BoxI32(lhs(i))
			rhsVal := types.BoxI32(rhs(i))
			i.stack[i.sp] = i.i32Or(lhsVal, rhsVal)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_ROTL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32Rotl(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_ROTR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32Rotr(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32Eq(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32Ne(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_LT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32LtS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_LT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32LtU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_GT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32GtS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_GT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32GtU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_LE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32LeS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_LE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32LeU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_GE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32GeS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I32_GE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i32GeU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	}
	return nil
}

// fusePairI64 folds <I64 lhs source>; <I64 rhs source>; <binop> into one
// dispatch, mirroring fusePairI32 for 64-bit operands. Both loaders return
// already-unboxed int64 values (matching the no-retain contract the
// existing I64 rhs loaders in threaded.go use for GLOBAL_GET/LOCAL_GET/
// UPVAL_GET/CONST_GET), so the fused closure never touches refcounts
// itself.
func (c *threader) fusePairI64(lhs, rhs func(*Interpreter) int64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I64_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Add(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Sub(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Mul(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_DIV_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64DivS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_DIV_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64DivU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_REM_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64RemS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_REM_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.i64RemU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_SHL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)&0x3F
			i.stack[i.sp] = i.i64Shl(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_SHR_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)&0x3F
			i.stack[i.sp] = i.i64ShrS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_SHR_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)&0x3F
			i.stack[i.sp] = i.i64ShrU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_XOR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Xor(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_AND:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64And(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_OR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Or(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_ROTL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Rotl(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_ROTR:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Rotr(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Eq(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64Ne(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_LT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64LtS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_LT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64LtU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_GT_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64GtS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_GT_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64GtU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_LE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64LeS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_LE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64LeU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_GE_S:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64GeS(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.I64_GE_U:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.i64GeU(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	}
	return nil
}

// fusePairF32 folds <F32 lhs source>; <F32 rhs source>; <binop> into one
// dispatch, mirroring fusePairI32 for 32-bit floats.
func (c *threader) fusePairF32(lhs, rhs func(*Interpreter) float32, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F32_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Add(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Sub(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Mul(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_DIV:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.f32Div(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_REM:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.f32Rem(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_MOD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.f32Mod(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_MIN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Min(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_MAX:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Max(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Copysign(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Eq(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Ne(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_LT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Lt(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_GT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Gt(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_LE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Le(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F32_GE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f32Ge(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	}
	return nil
}

// fusePairF64 folds <F64 lhs source>; <F64 rhs source>; <binop> into one
// dispatch, mirroring fusePairI32 for 64-bit floats.
func (c *threader) fusePairF64(lhs, rhs func(*Interpreter) float64, size int) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F64_ADD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Add(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_SUB:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Sub(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_MUL:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Mul(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_DIV:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.f64Div(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_REM:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.f64Rem(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_MOD:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			if rhs == 0 {
				panic(ErrDivideByZero)
			}
			i.stack[i.sp] = i.f64Mod(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_MIN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Min(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_MAX:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Max(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_COPYSIGN:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Copysign(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_EQ:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Eq(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_NE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Ne(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_LT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Lt(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_GT:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Gt(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_LE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Le(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	case instr.F64_GE:
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			lhs, rhs := lhs(i), rhs(i)
			i.stack[i.sp] = i.f64Ge(lhs, rhs)
			i.sp++
			i.fr.ip += size + 1
		}
	}
	return nil
}

// -- Loader constructors for fusePair operand sources --------------------
//
// Each loader reproduces the exact read semantics its single-operand rhs
// fusion counterpart uses in threaded.go: I32/F32/F64 sources are read as
// plain scalars (never boxed to a heap ref), while I64 sources use
// borrowI64, which never releases, so the slot keeps its own ownership of
// a heap-promoted ref and the loader only borrows the scalar.

func (c *threader) loadGlobalI32(idx int) func(*Interpreter) int32 {
	return func(i *Interpreter) int32 {
		if idx >= len(i.globals) {
			panic(ErrSegmentationFault)
		}
		return i.globals[idx].I32()
	}
}

func (c *threader) loadGlobalI64(idx int) func(*Interpreter) int64 {
	return func(i *Interpreter) int64 {
		if idx >= len(i.globals) {
			panic(ErrSegmentationFault)
		}
		return i.borrowI64(i.globals[idx])
	}
}

func (c *threader) loadGlobalF32(idx int) func(*Interpreter) float32 {
	return func(i *Interpreter) float32 {
		if idx >= len(i.globals) {
			panic(ErrSegmentationFault)
		}
		return i.globals[idx].F32()
	}
}

func (c *threader) loadGlobalF64(idx int) func(*Interpreter) float64 {
	return func(i *Interpreter) float64 {
		if idx >= len(i.globals) {
			panic(ErrSegmentationFault)
		}
		return i.globals[idx].F64()
	}
}

func (c *threader) loadUpvalI32(idx int) func(*Interpreter) int32 {
	return func(i *Interpreter) int32 {
		if idx >= len(i.fr.upvals) {
			panic(ErrSegmentationFault)
		}
		return i.fr.upvals[idx].I32()
	}
}

func (c *threader) loadUpvalI64(idx int) func(*Interpreter) int64 {
	return func(i *Interpreter) int64 {
		if idx >= len(i.fr.upvals) {
			panic(ErrSegmentationFault)
		}
		return i.borrowI64(i.fr.upvals[idx])
	}
}

func (c *threader) loadUpvalF32(idx int) func(*Interpreter) float32 {
	return func(i *Interpreter) float32 {
		if idx >= len(i.fr.upvals) {
			panic(ErrSegmentationFault)
		}
		return i.fr.upvals[idx].F32()
	}
}

func (c *threader) loadUpvalF64(idx int) func(*Interpreter) float64 {
	return func(i *Interpreter) float64 {
		if idx >= len(i.fr.upvals) {
			panic(ErrSegmentationFault)
		}
		return i.fr.upvals[idx].F64()
	}
}

func (c *threader) loadLocalI32(idx int) func(*Interpreter) int32 {
	return func(i *Interpreter) int32 {
		addr := i.fr.bp + idx
		if addr >= i.sp {
			panic(ErrSegmentationFault)
		}
		return i.stack[addr].I32()
	}
}

func (c *threader) loadLocalI64(idx int) func(*Interpreter) int64 {
	return func(i *Interpreter) int64 {
		addr := i.fr.bp + idx
		if addr >= i.sp {
			panic(ErrSegmentationFault)
		}
		return i.borrowI64(i.stack[addr])
	}
}

func (c *threader) loadLocalF32(idx int) func(*Interpreter) float32 {
	return func(i *Interpreter) float32 {
		addr := i.fr.bp + idx
		if addr >= i.sp {
			panic(ErrSegmentationFault)
		}
		return i.stack[addr].F32()
	}
}

func (c *threader) loadLocalF64(idx int) func(*Interpreter) float64 {
	return func(i *Interpreter) float64 {
		addr := i.fr.bp + idx
		if addr >= i.sp {
			panic(ErrSegmentationFault)
		}
		return i.stack[addr].F64()
	}
}

func loadConstI32(cst int32) func(*Interpreter) int32 {
	return func(*Interpreter) int32 { return cst }
}

func loadConstI64(cst int64) func(*Interpreter) int64 {
	return func(*Interpreter) int64 { return cst }
}

func loadConstF32(cst float32) func(*Interpreter) float32 {
	return func(*Interpreter) float32 { return cst }
}

func loadConstF64(cst float64) func(*Interpreter) float64 {
	return func(*Interpreter) float64 { return cst }
}

// fuseSourcePair probes whether the instruction(s) at c.ip form a rhs
// operand source of the given kind — I32_CONST/I64_CONST/F32_CONST/
// F64_CONST, LOCAL_GET, GLOBAL_GET, or UPVAL_GET — matching kind exactly,
// followed by a fusable binop, and if so builds and returns the fused
// closure combining it with lhs (the already-resolved left operand) via
// fusePairI32/I64/F32/F64. lhsSize is the byte width already consumed by
// the left operand's own instruction. c.ip is restored before returning so
// the compile loop still emits standalone handlers for the probed
// instructions, keeping branch targets valid. Returns nil when no pattern
// applies.
func (c *threader) fuseSourcePair(kind types.Kind, lhsSize int, lhsI32 func(*Interpreter) int32, lhsI64 func(*Interpreter) int64, lhsF32 func(*Interpreter) float32, lhsF64 func(*Interpreter) float64) func(*Interpreter) {
	if c.exact || c.ip >= len(c.code) {
		return nil
	}
	save := c.ip
	defer func() { c.ip = save }()

	switch instr.Opcode(c.code[c.ip]) {
	case instr.I32_CONST:
		if kind != types.KindI32 || c.ip+5 > len(c.code) {
			return nil
		}
		cst := *(*int32)(unsafe.Pointer(&c.code[c.ip+1]))
		c.ip += 5
		return c.fusePairI32(lhsI32, loadConstI32(cst), lhsSize+5)
	case instr.I64_CONST:
		if kind != types.KindI64 || c.ip+9 > len(c.code) {
			return nil
		}
		cst := int64(*(*uint64)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 9
		return c.fusePairI64(lhsI64, loadConstI64(cst), lhsSize+9)
	case instr.F32_CONST:
		if kind != types.KindF32 || c.ip+5 > len(c.code) {
			return nil
		}
		cst := *(*float32)(unsafe.Pointer(&c.code[c.ip+1]))
		c.ip += 5
		return c.fusePairF32(lhsF32, loadConstF32(cst), lhsSize+5)
	case instr.F64_CONST:
		if kind != types.KindF64 || c.ip+9 > len(c.code) {
			return nil
		}
		cst := *(*float64)(unsafe.Pointer(&c.code[c.ip+1]))
		c.ip += 9
		return c.fusePairF64(lhsF64, loadConstF64(cst), lhsSize+9)
	case instr.LOCAL_GET:
		if c.ip+2 > len(c.code) {
			return nil
		}
		j := int(c.code[c.ip+1])
		if j >= len(c.locals) || c.locals[j] != kind {
			return nil
		}
		c.ip += 2
		switch kind {
		case types.KindI32:
			return c.fusePairI32(lhsI32, c.loadLocalI32(j), lhsSize+2)
		case types.KindI64:
			return c.fusePairI64(lhsI64, c.loadLocalI64(j), lhsSize+2)
		case types.KindF32:
			return c.fusePairF32(lhsF32, c.loadLocalF32(j), lhsSize+2)
		case types.KindF64:
			return c.fusePairF64(lhsF64, c.loadLocalF64(j), lhsSize+2)
		}
		return nil
	case instr.GLOBAL_GET:
		if c.ip+3 > len(c.code) {
			return nil
		}
		j := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		if j >= len(c.globals) || c.globals[j] != kind {
			return nil
		}
		c.ip += 3
		switch kind {
		case types.KindI32:
			return c.fusePairI32(lhsI32, c.loadGlobalI32(j), lhsSize+3)
		case types.KindI64:
			return c.fusePairI64(lhsI64, c.loadGlobalI64(j), lhsSize+3)
		case types.KindF32:
			return c.fusePairF32(lhsF32, c.loadGlobalF32(j), lhsSize+3)
		case types.KindF64:
			return c.fusePairF64(lhsF64, c.loadGlobalF64(j), lhsSize+3)
		}
		return nil
	case instr.UPVAL_GET:
		if c.ip+2 > len(c.code) {
			return nil
		}
		j := int(c.code[c.ip+1])
		if j >= len(c.captures) || c.captures[j] != kind {
			return nil
		}
		c.ip += 2
		switch kind {
		case types.KindI32:
			return c.fusePairI32(lhsI32, c.loadUpvalI32(j), lhsSize+2)
		case types.KindI64:
			return c.fusePairI64(lhsI64, c.loadUpvalI64(j), lhsSize+2)
		case types.KindF32:
			return c.fusePairF32(lhsF32, c.loadUpvalF32(j), lhsSize+2)
		case types.KindF64:
			return c.fusePairF64(lhsF64, c.loadUpvalF64(j), lhsSize+2)
		}
		return nil
	}
	return nil
}

// fuseGlobalPair tries GLOBAL_GET idx; <CONST|LOCAL_GET|GLOBAL_GET|
// UPVAL_GET matching idx's declared kind>; <binop>, folding all three into
// one dispatch that never pushes the left operand onto the stack. Returns
// nil when idx's kind isn't I32/I64/F32/F64 or no rhs source/binop pattern
// matches.
func (c *threader) fuseGlobalPair(idx int) func(*Interpreter) {
	if c.exact || idx >= len(c.globals) {
		return nil
	}
	kind := c.globals[idx].Repr()
	switch kind {
	case types.KindI32, types.KindI64, types.KindF32, types.KindF64:
	default:
		return nil
	}
	return c.fuseSourcePair(kind, 3, c.loadGlobalI32(idx), c.loadGlobalI64(idx), c.loadGlobalF32(idx), c.loadGlobalF64(idx))
}

// fuseUpvalPair tries UPVAL_GET idx; <CONST|LOCAL_GET|GLOBAL_GET|
// UPVAL_GET matching idx's declared kind>; <binop>, mirroring
// fuseGlobalPair for upvalue captures.
func (c *threader) fuseUpvalPair(idx int) func(*Interpreter) {
	if c.exact || idx >= len(c.captures) {
		return nil
	}
	kind := c.captures[idx].Repr()
	switch kind {
	case types.KindI32, types.KindI64, types.KindF32, types.KindF64:
	default:
		return nil
	}
	return c.fuseSourcePair(kind, 2, c.loadUpvalI32(idx), c.loadUpvalI64(idx), c.loadUpvalF32(idx), c.loadUpvalF64(idx))
}
