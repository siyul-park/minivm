package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// fuseRefImm tries to fuse a constant ref load with the following instruction.
// Returns nil if no fusion pattern applies.
func (c *threadedCompiler) fuseRefImm(addr int, size int) func(*Interpreter) {
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
	if c.precise || c.ip >= len(c.code) {
		return nil
	}
	if instr.Opcode(c.code[c.ip]) == instr.REF_GET {
		switch c.heap[addr].(type) {
		case types.I32, types.I64, types.F32, types.F64:
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				switch i.heap[addr].(type) {
				case types.I32, types.I64, types.F32, types.F64:
				default:
					panic(ErrTypeMismatch)
				}
				i.stack[i.sp] = i.box(i.heap[addr])
				i.sp++
				i.fr.ip += size + 1
			}
		}
	}
	return nil
}

// fuseFunction tries to fuse a function ref load with a following CALL.
// Returns nil if no fusion pattern applies.
func (c *threadedCompiler) fuseFunction(fn *types.Function, addr int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseClosure(fn *types.Closure, addr int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseHostFunction(fn *HostFunction, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
		return nil
	}
	switch instr.Opcode(c.code[c.ip]) {
	case instr.CALL:
		params := len(fn.Typ.Params)
		returns := len(fn.Typ.Returns)
		delta := returns - params
		return func(i *Interpreter) {
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.sp+delta > len(i.stack) {
				panic(ErrStackOverflow)
			}
			args := i.stack[i.sp-params : i.sp]
			rets, err := fn.Fn(i, args)
			if err != nil {
				panic(err)
			}
			for _, val := range args {
				if val.Kind() != types.KindRef {
					continue
				}
				keep := false
				for _, ret := range rets {
					if ret == val {
						keep = true
						break
					}
				}
				if !keep {
					i.release(val.Ref())
				}
			}
			i.sp += delta
			copy(i.stack[i.sp-returns:i.sp], rets)
			i.fr.ip += size + 1
		}
	case instr.RETURN_CALL:
		params := len(fn.Typ.Params)
		returns := len(fn.Typ.Returns)
		delta := returns - params
		return func(i *Interpreter) {
			if i.sp < params {
				panic(ErrStackUnderflow)
			}
			if i.sp+delta > len(i.stack) {
				panic(ErrStackOverflow)
			}
			args := i.stack[i.sp-params : i.sp]
			rets, err := fn.Fn(i, args)
			if err != nil {
				panic(err)
			}
			for _, val := range args {
				if val.Kind() != types.KindRef {
					continue
				}
				keep := false
				for _, ret := range rets {
					if ret == val {
						keep = true
						break
					}
				}
				if !keep {
					i.release(val.Ref())
				}
			}
			i.sp += delta
			copy(i.stack[i.sp-returns:i.sp], rets)
			i.fr.ip += size + 1
			if i.fp > 1 {
				i.ret()
			}
		}
	}
	return nil
}

// fuseString tries to fuse a constant string load with a following I32_CONST
// code and ERROR_NEW, wrapping the string in an Error in one dispatch. The
// interned string becomes the error's payload and message. Returns nil if no
// pattern applies.
func (c *threadedCompiler) fuseString(text string, size int) func(*Interpreter) {
	if c.precise || c.ip+5 >= len(c.code) {
		return nil
	}
	if instr.Opcode(c.code[c.ip]) != instr.I32_CONST || instr.Opcode(c.code[c.ip+5]) != instr.ERROR_NEW {
		return nil
	}
	code := int32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
	return func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		payload := types.BoxRef(int(i.intern(text)))
		i.stack[i.sp] = types.BoxRef(i.keep(types.NewError(types.ErrorCode(code), text, payload)))
		i.sp++
		i.fr.ip += size + 6
	}
}

// fuseError tries to fuse an ERROR_NEW with a following THROW, building the
// exception and raising it in one dispatch. As a terminator the fused closure
// never advances the IP: it lands on a handler or escapes the dispatch loop.
// Returns nil if no pattern applies.
func (c *threadedCompiler) fuseError() func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
		return nil
	}
	if instr.Opcode(c.code[c.ip]) != instr.THROW {
		return nil
	}
	return func(i *Interpreter) {
		if i.sp < 2 {
			panic(ErrStackUnderflow)
		}
		code := i.stack[i.sp-1]
		if code.Kind() != types.KindI32 {
			panic(ErrTypeMismatch)
		}
		payload := i.stack[i.sp-2]
		// The payload reference transfers into the new Error's value field, so the
		// slot is dropped without a release. The i32 code slot is discarded.
		exc := types.BoxRef(i.keep(types.NewError(types.ErrorCode(code.I32()), i.message(payload), payload)))
		i.sp -= 2
		if fp, h, ok := i.handler(); ok {
			i.land(fp, h, exc)
			return
		}
		panic(escape{i.uncaught(exc)})
	}
}

// fuseLocalConst folds LOCAL_GET idx; <kind>.CONST c; <kind binop> into one
// dispatch: it pushes the typed local as the lhs, then runs the existing
// const+binop fused closure (fuse*Imm) in the same dispatch, saving a
// central-loop round-trip. The local kind selects the matching CONST opcode,
// immediate width, and folder so all four numeric kinds are handled the same
// way. Returns nil when no pattern applies.
//
// c.ip is restored after probing the folder so the compile loop still emits
// standalone handlers for the absorbed CONST and binop, keeping branch targets
// valid. I64 locals may hold a heap-promoted KindRef, and the i64 folder
// unboxes (and releases) the lhs, so the pushed local is retained to stay
// balanced; I32/F32/F64 never box to a ref and skip the retain (and its Kind
// branch), matching the plain LOCAL_GET fast path.
func (c *threadedCompiler) fuseLocalConst(idx int) func(*Interpreter) {
	if c.precise || idx >= len(c.locals) {
		return nil
	}

	var inner func(*Interpreter)
	retain := false
	switch c.locals[idx] {
	case types.KindI32:
		if c.ip+5 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.I32_CONST {
			return nil
		}
		cst := *(*int32)(unsafe.Pointer(&c.code[c.ip+1]))
		save := c.ip
		c.ip += 5
		inner = c.fuseI32Imm(cst, 5)
		c.ip = save
	case types.KindI64:
		if c.ip+9 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.I64_CONST {
			return nil
		}
		cst := int64(*(*uint64)(unsafe.Pointer(&c.code[c.ip+1])))
		save := c.ip
		c.ip += 9
		inner = c.fuseI64Imm(cst, 9)
		c.ip = save
		retain = true
	case types.KindF32:
		if c.ip+5 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.F32_CONST {
			return nil
		}
		cst := *(*float32)(unsafe.Pointer(&c.code[c.ip+1]))
		save := c.ip
		c.ip += 5
		inner = c.fuseF32Imm(cst, 5)
		c.ip = save
	case types.KindF64:
		if c.ip+9 >= len(c.code) || instr.Opcode(c.code[c.ip]) != instr.F64_CONST {
			return nil
		}
		cst := *(*float64)(unsafe.Pointer(&c.code[c.ip+1]))
		save := c.ip
		c.ip += 9
		inner = c.fuseF64Imm(cst, 9)
		c.ip = save
	default:
		return nil
	}
	if inner == nil {
		return nil
	}

	if retain {
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr > i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			i.retainBox(val)
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 2
			inner(i)
		}
	}
	return func(i *Interpreter) {
		if i.sp == len(i.stack) {
			panic(ErrStackOverflow)
		}
		addr := i.fr.bp + idx
		if addr > i.sp {
			panic(ErrSegmentationFault)
		}
		i.stack[i.sp] = i.stack[addr]
		i.sp++
		i.fr.ip += 2
		inner(i)
	}
}

func (c *threadedCompiler) fuseI32(rhs func(*Interpreter) int32, kind types.Kind, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseI32Imm(rhs int32, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseI64(rhs func(*Interpreter) int64, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseI64Imm(rhs int64, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseF32(rhs func(*Interpreter) float32, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseF32Imm(rhs float32, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseF64(rhs func(*Interpreter) float64, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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

func (c *threadedCompiler) fuseF64Imm(rhs float64, size int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
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
