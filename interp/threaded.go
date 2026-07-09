package interp

import (
	"math"
	"math/bits"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type threader struct {
	types     []types.Type
	constants []types.Boxed
	heap      []types.Value
	locals    []types.Kind
	globals   []types.Kind
	captures  []types.Kind
	code      []byte
	ip        int
	exact     bool
}

var threaded = [256]func(c *threader) func(i *Interpreter){
	instr.NOP: func(c *threader) func(i *Interpreter) {
		skip := 0
		for !c.exact && c.ip+skip < len(c.code) && instr.Opcode(c.code[c.ip+skip]) == instr.NOP {
			skip++
		}
		if c.exact {
			skip = 1
		}
		c.ip++
		return func(i *Interpreter) {
			i.fr.ip += skip
		}
	},
	instr.UNREACHABLE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			i.fr.ip++
			panic(ErrUnreachableExecuted)
		}
	},
	instr.DROP: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1]
			i.releaseBox(val)
			i.sp--
			i.fr.ip++
		}
	},
	instr.DUP: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			val := i.stack[i.sp-1]
			i.retainBox(val)
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip++
		}
	},
	instr.SWAP: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			i.stack[i.sp-1], i.stack[i.sp-2] = i.stack[i.sp-2], i.stack[i.sp-1]
			i.fr.ip++
		}
	},
	instr.BR: func(c *threader) func(i *Interpreter) {
		offset := instr.ParseI16(c.code, c.ip+1)
		c.ip += 3
		return func(i *Interpreter) {
			f := i.fr
			f.ip += offset + 3
		}
	},
	instr.BR_IF: func(c *threader) func(i *Interpreter) {
		offset := instr.ParseI16(c.code, c.ip+1)
		c.ip += 3
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			i.sp--
			cond := i.stack[i.sp].I32()
			if cond != 0 {
				i.fr.ip += offset
			}
			i.fr.ip += 3
		}
	},
	instr.BR_TABLE: func(c *threader) func(i *Interpreter) {
		count := int(c.code[c.ip+1])
		offsets := make([]int, count+1)
		for i := 0; i < len(offsets); i++ {
			at := c.ip + i*2 + 2
			offsets[i] = instr.ParseI16(c.code, at)
		}
		c.ip += count*2 + 4
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			i.sp--
			cond := int(i.stack[i.sp].I32())
			if cond < 0 || cond >= count {
				cond = count
			}
			i.fr.ip += offsets[cond] + count*2 + 4
		}
	},
	instr.SELECT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 3 {
				panic(ErrStackUnderflow)
			}
			cond := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2]
			v1 := i.stack[i.sp-3]
			selected := v1
			discarded := v2
			if cond == 0 {
				selected = v2
				discarded = v1
			}
			i.releaseBox(discarded)
			i.stack[i.sp-3] = selected
			i.sp -= 2
			i.fr.ip++
		}
	},
	instr.CALL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			addr := i.stack[i.sp-1].Ref()
			switch fn := i.heap[addr].(type) {
			case *types.Function:
				if i.fp == len(i.frames) {
					panic(ErrFrameOverflow)
				}
				params := len(fn.Typ.Params)
				returns := len(fn.Typ.Returns)
				locals := len(fn.Locals)
				if i.sp <= params {
					panic(ErrStackUnderflow)
				}
				if i.sp+locals-1 > len(i.stack) {
					panic(ErrStackOverflow)
				}
				if locals > 0 {
					clear(i.stack[i.sp-1 : i.sp+locals-1])
				}
				f := &i.frames[i.fp]
				f.code = i.code[addr]
				f.upvals = nil
				f.addr = addr
				f.ref = addr
				f.ip = 0
				f.bp = i.sp - params - 1
				f.returns = returns
				f.release = true
				f.coro = 0
				if addr < len(i.coros) && i.coros[addr] {
					f.coro = i.alloc(&Coroutine{typ: fn.Typ})
				}
				i.sp = f.bp + params + locals
				i.fr.ip++
				i.fp++
				i.fr = f
			case *types.Closure:
				if i.fp == len(i.frames) {
					panic(ErrFrameOverflow)
				}
				tmpl, ok := i.heap[fn.Fn].(*types.Function)
				if !ok {
					panic(ErrTypeMismatch)
				}
				params := len(fn.Typ.Params)
				returns := len(fn.Typ.Returns)
				locals := len(tmpl.Locals)
				if i.sp <= params {
					panic(ErrStackUnderflow)
				}
				if i.sp+locals-1 > len(i.stack) {
					panic(ErrStackOverflow)
				}
				if locals > 0 {
					clear(i.stack[i.sp-1 : i.sp+locals-1])
				}
				f := &i.frames[i.fp]
				f.code = i.code[fn.Fn]
				f.upvals = fn.Upvals
				f.addr = int(fn.Fn)
				f.ref = addr
				f.ip = 0
				f.bp = i.sp - params - 1
				f.returns = returns
				f.release = true
				f.coro = 0
				if int(fn.Fn) < len(i.coros) && i.coros[fn.Fn] {
					f.coro = i.alloc(&Coroutine{typ: fn.Typ})
				}
				i.sp = f.bp + params + locals
				i.fr.ip++
				i.fp++
				i.fr = f
			case *HostFunction:
				i.callHost(fn)
			default:
				panic(ErrTypeMismatch)
			}
		}
	},
	instr.RETURN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.fp == 1 {
				panic(ErrFrameUnderflow)
			}
			if i.fr.coro != 0 {
				i.finish()
				return
			}
			i.ret()
		}
	},
	instr.YIELD: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			i.fr.ip++
			if i.fp == 1 {
				panic(errYield)
			}
			i.suspend()
		}
	},
	instr.RESUME: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			i.resume()
		}
	},
	instr.CORO_DONE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-1]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			done := int32(0)
			switch co := i.heap[ref.Ref()].(type) {
			case *Coroutine:
				if co.done {
					done = 1
				}
			case types.Iterator:
				if co.Done() {
					done = 1
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.stack[i.sp-1] = types.BoxI1(done != 0)
			i.fr.ip++
		}
	},
	instr.CORO_VALUE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			box := i.stack[i.sp-1]
			if box.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			var val types.Boxed
			switch co := i.heap[box.Ref()].(type) {
			case *Coroutine:
				val = co.value
				i.retainBox(val)
			case types.Iterator:
				val = i.boxIteratorCurrent(co.Current())
			default:
				panic(ErrTypeMismatch)
			}
			i.releaseBox(box)
			i.stack[i.sp-1] = val
			i.fr.ip++
		}
	},
	instr.RETURN_CALL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			addr := i.stack[i.sp-1].Ref()
			switch fn := i.heap[addr].(type) {
			case *types.Function:
				i.tail(addr, addr, nil, len(fn.Typ.Params), len(fn.Typ.Returns), len(fn.Locals))
			case *types.Closure:
				tmpl, ok := i.heap[fn.Fn].(*types.Function)
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.tail(int(fn.Fn), addr, fn.Upvals, len(fn.Typ.Params), len(fn.Typ.Returns), len(tmpl.Locals))
			case *HostFunction:
				i.callHost(fn)
				if i.fp > 1 {
					i.ret()
				}
			default:
				panic(ErrTypeMismatch)
			}
		}
	},
	instr.GLOBAL_GET: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < len(c.globals) {
			switch c.globals[idx].Repr() {
			case types.KindI32:
				if fused := c.fuseI32(func(i *Interpreter) int32 {
					if idx >= len(i.globals) {
						panic(ErrSegmentationFault)
					}
					return i.globals[idx].I32()
				}, c.globals[idx], 3); fused != nil {
					return fused
				}
			case types.KindI64:
				if fused := c.fuseI64(func(i *Interpreter) int64 {
					if idx >= len(i.globals) {
						panic(ErrSegmentationFault)
					}
					// borrowI64, not unboxI64: the global slot keeps its own
					// ownership of a heap-promoted ref; this read only borrows.
					return i.borrowI64(i.globals[idx])
				}, 3); fused != nil {
					return fused
				}
			case types.KindF32:
				if fused := c.fuseF32(func(i *Interpreter) float32 {
					if idx >= len(i.globals) {
						panic(ErrSegmentationFault)
					}
					return i.globals[idx].F32()
				}, 3); fused != nil {
					return fused
				}
			case types.KindF64:
				if fused := c.fuseF64(func(i *Interpreter) float64 {
					if idx >= len(i.globals) {
						panic(ErrSegmentationFault)
					}
					return i.globals[idx].F64()
				}, 3); fused != nil {
					return fused
				}
			}
			// Superinstruction: GLOBAL_GET idx; <CONST|LOCAL_GET|GLOBAL_GET|
			// UPVAL_GET matching kind>; <binop>.
			if fused := c.fuseGlobalPair(idx); fused != nil {
				return fused
			}
			// I32/F32/F64 globals never hold a heap ref, so retain is a no-op;
			// skip it and the Kind branch. I64 may box to a ref, so it keeps
			// retainBox.
			switch c.globals[idx].Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == len(i.stack) {
						panic(ErrStackOverflow)
					}
					if idx >= len(i.globals) {
						panic(ErrSegmentationFault)
					}
					i.stack[i.sp] = i.globals[idx]
					i.sp++
					i.fr.ip += 3
				}
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			if idx >= len(i.globals) {
				panic(ErrSegmentationFault)
			}
			val := i.globals[idx]
			i.retainBox(val)
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 3
		}
	},
	instr.GLOBAL_SET: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		// I32/F32/F64 globals never hold a heap ref, so the old value can never
		// be a ref; skip the release. I64 may box to a ref, so it keeps it.
		if idx < len(c.globals) {
			switch c.globals[idx].Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == 0 {
						panic(ErrStackUnderflow)
					}
					if idx >= len(i.globals) {
						panic(ErrSegmentationFault)
					}
					i.globals[idx] = i.stack[i.sp-1]
					i.sp--
					i.fr.ip += 3
				}
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			if idx >= len(i.globals) {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.globals[idx]
			if old != val {
				i.releaseBox(old)
			}
			i.globals[idx] = val
			i.sp--
			i.fr.ip += 3
		}
	},
	instr.GLOBAL_TEE: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		// I32/F32/F64 globals never hold a heap ref, so the old value can never
		// be a ref; skip the release. I64 may box to a ref, so it keeps it.
		if idx < len(c.globals) {
			switch c.globals[idx].Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == 0 {
						panic(ErrStackUnderflow)
					}
					if idx >= len(i.globals) {
						panic(ErrSegmentationFault)
					}
					i.globals[idx] = i.stack[i.sp-1]
					i.fr.ip += 3
				}
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			if idx >= len(i.globals) {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.globals[idx]
			if old != val {
				i.retainBox(val)
				i.releaseBox(old)
			}
			i.globals[idx] = val
			i.fr.ip += 3
		}
	},
	instr.LOCAL_GET: func(c *threader) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		switch c.locals[idx].Repr() {
		case types.KindI32:
			if fused := c.fuseI32(func(i *Interpreter) int32 {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				return i.stack[addr].I32()
			}, c.locals[idx], 2); fused != nil {
				return fused
			}
		case types.KindI64:
			if fused := c.fuseI64(func(i *Interpreter) int64 {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				// borrowI64, not unboxI64: the local slot keeps its own
				// ownership of a heap-promoted ref; this read only borrows.
				return i.borrowI64(i.stack[addr])
			}, 2); fused != nil {
				return fused
			}
		case types.KindF32:
			if fused := c.fuseF32(func(i *Interpreter) float32 {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				return i.stack[addr].F32()
			}, 2); fused != nil {
				return fused
			}
		case types.KindF64:
			if fused := c.fuseF64(func(i *Interpreter) float64 {
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				return i.stack[addr].F64()
			}, 2); fused != nil {
				return fused
			}
		}
		// Superinstruction: LOCAL_GET idx; <kind>.CONST c; <kind binop>.
		if fused := c.fuseLocalConst(idx); fused != nil {
			return fused
		}
		// Superinstruction: LOCAL_GET idxA; LOCAL_GET idxB; <kind binop>.
		if fused := c.fuseLocalLocal(idx); fused != nil {
			return fused
		}
		// I32/F32/F64 locals never hold a heap ref, so retain is a no-op; skip
		// it and the Kind branch. I64 may box to a ref, so it keeps retainBox.
		switch c.locals[idx].Repr() {
		case types.KindI32, types.KindF32, types.KindF64:
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				addr := i.fr.bp + idx
				if addr >= i.sp {
					panic(ErrSegmentationFault)
				}
				i.stack[i.sp] = i.stack[addr]
				i.sp++
				i.fr.ip += 2
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
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 2
		}
	},
	instr.LOCAL_SET: func(c *threader) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		// I32/F32/F64 locals never hold a heap ref, so the old value can never
		// be a ref; skip the release. I64 may box to a ref, so it keeps it.
		if idx < len(c.locals) {
			switch c.locals[idx].Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == 0 {
						panic(ErrStackUnderflow)
					}
					addr := i.fr.bp + idx
					if addr >= i.sp {
						panic(ErrSegmentationFault)
					}
					i.stack[addr] = i.stack[i.sp-1]
					i.sp--
					i.fr.ip += 2
				}
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.stack[addr]
			if old != val {
				i.releaseBox(old)
			}
			i.stack[addr] = val
			i.sp--
			i.fr.ip += 2
		}
	},
	instr.LOCAL_TEE: func(c *threader) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		// I32/F32/F64 locals never hold a heap ref, so the old value can never
		// be a ref; skip the release. I64 may box to a ref, so it keeps it.
		if idx < len(c.locals) {
			switch c.locals[idx].Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == 0 {
						panic(ErrStackUnderflow)
					}
					addr := i.fr.bp + idx
					if addr >= i.sp {
						panic(ErrSegmentationFault)
					}
					i.stack[addr] = i.stack[i.sp-1]
					i.fr.ip += 2
				}
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			addr := i.fr.bp + idx
			if addr >= i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.stack[addr]
			if old != val {
				i.retainBox(val)
				i.releaseBox(old)
			}
			i.stack[addr] = val
			i.fr.ip += 2
		}
	},
	instr.CONST_GET: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.constants) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		val := c.constants[idx]
		switch val.Kind() {
		case types.KindI32:
			if fused := c.fuseI32Imm(val.I32(), 3); fused != nil {
				return fused
			}
		case types.KindI64:
			if fused := c.fuseI64Imm(val.I64(), 3); fused != nil {
				return fused
			}
		case types.KindF32:
			if fused := c.fuseF32Imm(val.F32(), 3); fused != nil {
				return fused
			}
		case types.KindF64:
			if fused := c.fuseF64Imm(val.F64(), 3); fused != nil {
				return fused
			}
		case types.KindRef:
			addr := val.Ref()
			if str, ok := c.heap[addr].(types.String); ok {
				text := string(str)
				return func(i *Interpreter) {
					if i.sp == len(i.stack) {
						panic(ErrStackOverflow)
					}
					i.stack[i.sp] = types.BoxRef(int(i.intern(text)))
					i.sp++
					i.fr.ip += 3
				}
			}
			if fused := c.fuseRefImm(addr, 3); fused != nil {
				return fused
			}
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				i.retain(addr)
				i.stack[i.sp] = val
				i.sp++
				i.fr.ip += 3
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 3
		}
	},
	instr.UPVAL_GET: func(c *threader) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		if idx < len(c.captures) {
			switch c.captures[idx].Repr() {
			case types.KindI32:
				if fused := c.fuseI32(func(i *Interpreter) int32 {
					if idx >= len(i.fr.upvals) {
						panic(ErrSegmentationFault)
					}
					return i.fr.upvals[idx].I32()
				}, c.captures[idx], 2); fused != nil {
					return fused
				}
			case types.KindI64:
				if fused := c.fuseI64(func(i *Interpreter) int64 {
					if idx >= len(i.fr.upvals) {
						panic(ErrSegmentationFault)
					}
					// borrowI64, not unboxI64: the capture slot keeps its own
					// ownership of a heap-promoted ref; this read only borrows.
					return i.borrowI64(i.fr.upvals[idx])
				}, 2); fused != nil {
					return fused
				}
			case types.KindF32:
				if fused := c.fuseF32(func(i *Interpreter) float32 {
					if idx >= len(i.fr.upvals) {
						panic(ErrSegmentationFault)
					}
					return i.fr.upvals[idx].F32()
				}, 2); fused != nil {
					return fused
				}
			case types.KindF64:
				if fused := c.fuseF64(func(i *Interpreter) float64 {
					if idx >= len(i.fr.upvals) {
						panic(ErrSegmentationFault)
					}
					return i.fr.upvals[idx].F64()
				}, 2); fused != nil {
					return fused
				}
			}
			// Superinstruction: UPVAL_GET idx; <CONST|LOCAL_GET|GLOBAL_GET|
			// UPVAL_GET matching kind>; <binop>.
			if fused := c.fuseUpvalPair(idx); fused != nil {
				return fused
			}
		}
		// I32/F32/F64 captures never hold a heap ref, so retain is a no-op; skip
		// it and the Kind branch. I64 may box to a ref, so it keeps retainBox.
		if idx < len(c.captures) {
			switch c.captures[idx].Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == len(i.stack) {
						panic(ErrStackOverflow)
					}
					if idx >= len(i.fr.upvals) {
						panic(ErrSegmentationFault)
					}
					i.stack[i.sp] = i.fr.upvals[idx]
					i.sp++
					i.fr.ip += 2
				}
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			if idx >= len(i.fr.upvals) {
				panic(ErrSegmentationFault)
			}
			val := i.fr.upvals[idx]
			i.retainBox(val)
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 2
		}
	},
	instr.UPVAL_SET: func(c *threader) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		// I32/F32/F64 captures never hold a heap ref, so the old value can never
		// be a ref; skip the release. I64 may box to a ref, so it keeps it.
		if idx < len(c.captures) {
			switch c.captures[idx].Repr() {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == 0 {
						panic(ErrStackUnderflow)
					}
					if idx >= len(i.fr.upvals) {
						panic(ErrSegmentationFault)
					}
					i.fr.upvals[idx] = i.stack[i.sp-1]
					i.sp--
					i.fr.ip += 2
				}
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			if idx >= len(i.fr.upvals) {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.fr.upvals[idx]
			if old != val {
				i.releaseBox(old)
			}
			i.fr.upvals[idx] = val
			i.sp--
			i.fr.ip += 2
		}
	},
	instr.REF_NEW: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1]
			if v.Kind() == types.KindRef {
				panic(ErrTypeMismatch)
			}
			i.stack[i.sp-1] = types.BoxRef(i.alloc(types.Unbox(v)))
			i.fr.ip++
		}
	},
	instr.REF_GET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			val := i.refGet()
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip++
		}
	},
	instr.REF_SET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			value := i.stack[i.sp-1]
			ref := i.stack[i.sp-2]
			if value.Kind() == types.KindRef {
				panic(ErrTypeMismatch)
			}
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			i.heap[addr] = types.Unbox(value)
			i.sp -= 2
			i.release(addr)
			i.fr.ip++
		}
	},
	instr.REF_NULL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.retain(0)
			i.stack[i.sp] = types.BoxedNull
			i.sp++
			i.fr.ip++
		}
	},
	instr.REF_TEST: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ := c.types[idx]
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1]
			var cond types.Boxed
			switch kind := val.Kind(); kind {
			case types.KindRef:
				ref := i.heap[val.Ref()]
				cond = types.BoxI1(typ.Equals(ref.Type()))
			default:
				cond = types.BoxI1(typ.Kind() == kind)
			}
			i.releaseBox(val)
			i.stack[i.sp-1] = cond
			i.fr.ip += 3
		}
	},
	instr.REF_CAST: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ := c.types[idx]
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1]
			switch kind := val.Kind(); kind {
			case types.KindRef:
				ref := i.heap[val.Ref()]
				if !typ.Cast(ref.Type()) {
					panic(ErrTypeMismatch)
				}
			default:
				if !typ.Cast(val.Type()) {
					panic(ErrTypeMismatch)
				}
			}
			i.fr.ip += 3
		}
	},
	instr.REF_IS_NULL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1]
			i.stack[i.sp-1] = types.BoxI1(val.Ref() == 0)
			i.releaseBox(val)
			i.fr.ip++
		}
	},
	instr.REF_EQ: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1]
			v2 := i.stack[i.sp-2]
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 == v1)
			i.releaseBox(v1)
			i.releaseBox(v2)
			i.fr.ip++
		}
	},
	instr.REF_NE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1]
			v2 := i.stack[i.sp-2]
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 != v1)
			i.releaseBox(v1)
			i.releaseBox(v2)
			i.fr.ip++
		}
	},
	instr.I32_CONST: func(c *threader) func(i *Interpreter) {
		raw := *(*int32)(unsafe.Pointer(&c.code[c.ip+1]))
		val := types.BoxI32(raw)
		c.ip += 5
		if fused := c.fuseI32Imm(raw, 5); fused != nil {
			return fused
		}
		// Superinstruction: I32_CONST c; BR_IF fuses a compile-time-known
		// branch condition, skipping the push/pop of the boxed boolean
		// entirely.
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				i.branchIf(raw != 0, offset, 8)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 5
		}
	},
	instr.I32_ADD: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Add(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_SUB: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Sub(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_MUL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Mul(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_DIV_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			result := i.i32DivS(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I32_DIV_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			result := i.i32DivU(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I32_REM_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			result := i.i32RemS(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I32_REM_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			result := i.i32RemU(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I32_SHL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Shl(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_SHR_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32ShrS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_SHR_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32ShrU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_XOR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1]
			lhs := i.stack[i.sp-2]
			i.sp--
			i.stack[i.sp-1] = i.i32Xor(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_AND: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1]
			lhs := i.stack[i.sp-2]
			i.sp--
			i.stack[i.sp-1] = i.i32And(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_OR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1]
			lhs := i.stack[i.sp-2]
			i.sp--
			i.stack[i.sp-1] = i.i32Or(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_CLZ: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Clz(v)
			i.fr.ip++
		}
	},
	instr.I32_CTZ: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Ctz(v)
			i.fr.ip++
		}
	},
	instr.I32_POPCNT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Popcnt(v)
			i.fr.ip++
		}
	},
	instr.I32_ROTL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Rotl(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_ROTR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Rotr(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_EXTEND8_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Extend8S(v)
			i.fr.ip++
		}
	},
	instr.I32_EXTEND16_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Extend16S(v)
			i.fr.ip++
		}
	},
	instr.I32_EQZ: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				v := i.stack[i.sp-1].I32()
				i.sp--
				i.branchIf(v == 0, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32Eqz(v)
			i.fr.ip++
		}
	},
	instr.I32_EQ: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(lhs == rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Eq(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_NE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(lhs != rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32Ne(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_LT_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(lhs < rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32LtS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_LT_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(uint32(lhs) < uint32(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32LtU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_GT_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(lhs > rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32GtS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_GT_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(uint32(lhs) > uint32(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32GtU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_LE_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(lhs <= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32LeS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_LE_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(uint32(lhs) <= uint32(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32LeU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_GE_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(lhs >= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32GeS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_GE_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].I32()
				lhs := i.stack[i.sp-2].I32()
				i.sp -= 2
				i.branchIf(uint32(lhs) >= uint32(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].I32()
			lhs := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = i.i32GeU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I32_TO_I64_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ToI64S(v)
			i.fr.ip++
		}
	},
	instr.I32_TO_I64_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ToI64U(v)
			i.fr.ip++
		}
	},
	instr.I32_TO_F32_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ToF32U(v)
			i.fr.ip++
		}
	},
	instr.I32_TO_F32_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ToF32S(v)
			i.fr.ip++
		}
	},
	instr.I32_TO_F64_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ToF64U(v)
			i.fr.ip++
		}
	},
	instr.I32_TO_F64_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.i32ToF64S(v)
			i.fr.ip++
		}
	},
	instr.I32_REINTERPRET_F32: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.i32ReinterpretF32(v)
			i.fr.ip++
		}
	},
	instr.I64_CONST: func(c *threader) func(i *Interpreter) {
		val := int64(*(*uint64)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 9
		if fused := c.fuseI64(func(*Interpreter) int64 { return val }, 9); fused != nil {
			return fused
		}
		if types.IsBoxable(val) {
			v := types.BoxI64(val)
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				i.stack[i.sp] = v
				i.sp++
				i.fr.ip += 9
			}
		}
		v := types.I64(val)
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = types.BoxRef(i.alloc(v))
			i.sp++
			i.fr.ip += 9
		}
	},
	instr.I64_ADD: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Add(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_SUB: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Sub(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_MUL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Mul(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_DIV_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			result := i.i64DivS(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I64_DIV_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			result := i.i64DivU(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I64_REM_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			result := i.i64RemS(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I64_REM_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			result := i.i64RemU(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.I64_SHL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Shl(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_SHR_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64ShrS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_SHR_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64ShrU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_XOR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Xor(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_AND: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64And(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_OR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Or(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_CLZ: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Clz(v)
			i.fr.ip++
		}
	},
	instr.I64_CTZ: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Ctz(v)
			i.fr.ip++
		}
	},
	instr.I64_POPCNT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Popcnt(v)
			i.fr.ip++
		}
	},
	instr.I64_ROTL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Rotl(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_ROTR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Rotr(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_EXTEND8_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Extend8S(v)
			i.fr.ip++
		}
	},
	instr.I64_EXTEND16_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Extend16S(v)
			i.fr.ip++
		}
	},
	instr.I64_EXTEND32_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Extend32S(v)
			i.fr.ip++
		}
	},
	instr.I64_EQZ: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp == 0 {
					panic(ErrStackUnderflow)
				}
				v := i.unboxI64(i.stack[i.sp-1])
				i.sp--
				i.branchIf(v == 0, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64Eqz(v)
			i.fr.ip++
		}
	},
	instr.I64_EQ: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(lhs == rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Eq(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_NE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(lhs != rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64Ne(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_LT_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(lhs < rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64LtS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_LT_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(uint64(lhs) < uint64(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64LtU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_GT_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(lhs > rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64GtS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_GT_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(uint64(lhs) > uint64(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64GtU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_LE_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(lhs <= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64LeS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_LE_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(uint64(lhs) <= uint64(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64LeU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_GE_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(lhs >= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64GeS(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_GE_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.unboxI64(i.stack[i.sp-1])
				lhs := i.unboxI64(i.stack[i.sp-2])
				i.sp -= 2
				i.branchIf(uint64(lhs) >= uint64(rhs), offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.unboxI64(i.stack[i.sp-1])
			lhs := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.i64GeU(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.I64_TO_I32: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ToI32(v)
			i.fr.ip++
		}
	},
	instr.I64_TO_F32_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ToF32S(v)
			i.fr.ip++
		}
	},
	instr.I64_TO_F32_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ToF32U(v)
			i.fr.ip++
		}
	},
	instr.I64_TO_F64_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ToF64S(v)
			i.fr.ip++
		}
	},
	instr.I64_TO_F64_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.i64ToF64U(v)
			i.fr.ip++
		}
	},
	instr.I64_REINTERPRET_F64: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.i64ReinterpretF64(v)
			i.fr.ip++
		}
	},
	instr.F32_CONST: func(c *threader) func(i *Interpreter) {
		raw := *(*float32)(unsafe.Pointer(&c.code[c.ip+1]))
		val := types.BoxF32(raw)
		c.ip += 5
		if fused := c.fuseF32Imm(raw, 5); fused != nil {
			return fused
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 5
		}
	},
	instr.F32_ADD: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Add(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_SUB: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Sub(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_MUL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Mul(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_DIV: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			result := i.f32Div(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.F32_REM: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			result := i.f32Rem(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.F32_MOD: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			result := i.f32Mod(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.F32_ABS: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Abs(v)
			i.fr.ip++
		}
	},
	instr.F32_NEG: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Neg(v)
			i.fr.ip++
		}
	},
	instr.F32_SQRT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Sqrt(v)
			i.fr.ip++
		}
	},
	instr.F32_CEIL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Ceil(v)
			i.fr.ip++
		}
	},
	instr.F32_FLOOR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Floor(v)
			i.fr.ip++
		}
	},
	instr.F32_TRUNC: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Trunc(v)
			i.fr.ip++
		}
	},
	instr.F32_NEAREST: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32Nearest(v)
			i.fr.ip++
		}
	},
	instr.F32_MIN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Min(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_MAX: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Max(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_COPYSIGN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Copysign(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_EQ: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F32()
				lhs := i.stack[i.sp-2].F32()
				i.sp -= 2
				i.branchIf(lhs == rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Eq(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_NE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F32()
				lhs := i.stack[i.sp-2].F32()
				i.sp -= 2
				i.branchIf(lhs != rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Ne(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_LT: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F32()
				lhs := i.stack[i.sp-2].F32()
				i.sp -= 2
				i.branchIf(lhs < rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Lt(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_GT: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F32()
				lhs := i.stack[i.sp-2].F32()
				i.sp -= 2
				i.branchIf(lhs > rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Gt(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_LE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F32()
				lhs := i.stack[i.sp-2].F32()
				i.sp -= 2
				i.branchIf(lhs <= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Le(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_GE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F32()
				lhs := i.stack[i.sp-2].F32()
				i.sp -= 2
				i.branchIf(lhs >= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F32()
			lhs := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = i.f32Ge(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F32_TO_I32_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32ToI32S(v)
			i.fr.ip++
		}
	},
	instr.F32_TO_I32_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32ToI32U(v)
			i.fr.ip++
		}
	},
	instr.F32_TO_I64_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.boxI64(i.satI64(float64(v)))
			i.fr.ip++
		}
	},
	instr.F32_TO_I64_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.boxI64(int64(i.satU64(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_TO_F64: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = i.f32ToF64(v)
			i.fr.ip++
		}
	},
	instr.F32_REINTERPRET_I32: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.f32ReinterpretI32(v)
			i.fr.ip++
		}
	},
	instr.F64_CONST: func(c *threader) func(i *Interpreter) {
		raw := *(*float64)(unsafe.Pointer(&c.code[c.ip+1]))
		val := types.BoxF64(raw)
		c.ip += 9
		if fused := c.fuseF64Imm(raw, 9); fused != nil {
			return fused
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 9
		}
	},
	instr.F64_ADD: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Add(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_SUB: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Sub(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_MUL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Mul(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_DIV: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			result := i.f64Div(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.F64_REM: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			result := i.f64Rem(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.F64_MOD: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			result := i.f64Mod(lhs, rhs)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.F64_ABS: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Abs(v)
			i.fr.ip++
		}
	},
	instr.F64_NEG: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Neg(v)
			i.fr.ip++
		}
	},
	instr.F64_SQRT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Sqrt(v)
			i.fr.ip++
		}
	},
	instr.F64_CEIL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Ceil(v)
			i.fr.ip++
		}
	},
	instr.F64_FLOOR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Floor(v)
			i.fr.ip++
		}
	},
	instr.F64_TRUNC: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Trunc(v)
			i.fr.ip++
		}
	},
	instr.F64_NEAREST: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64Nearest(v)
			i.fr.ip++
		}
	},
	instr.F64_MIN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Min(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_MAX: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Max(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_COPYSIGN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Copysign(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_EQ: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F64()
				lhs := i.stack[i.sp-2].F64()
				i.sp -= 2
				i.branchIf(lhs == rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Eq(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_NE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F64()
				lhs := i.stack[i.sp-2].F64()
				i.sp -= 2
				i.branchIf(lhs != rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Ne(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_LT: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F64()
				lhs := i.stack[i.sp-2].F64()
				i.sp -= 2
				i.branchIf(lhs < rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Lt(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_GT: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F64()
				lhs := i.stack[i.sp-2].F64()
				i.sp -= 2
				i.branchIf(lhs > rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Gt(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_LE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F64()
				lhs := i.stack[i.sp-2].F64()
				i.sp -= 2
				i.branchIf(lhs <= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Le(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_GE: func(c *threader) func(i *Interpreter) {
		c.ip++
		if offset, ok := c.peekBrIf(c.ip); ok {
			return func(i *Interpreter) {
				if i.sp < 2 {
					panic(ErrStackUnderflow)
				}
				rhs := i.stack[i.sp-1].F64()
				lhs := i.stack[i.sp-2].F64()
				i.sp -= 2
				i.branchIf(lhs >= rhs, offset, 4)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			rhs := i.stack[i.sp-1].F64()
			lhs := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = i.f64Ge(lhs, rhs)
			i.fr.ip++
		}
	},
	instr.F64_TO_I32_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64ToI32S(v)
			i.fr.ip++
		}
	},
	instr.F64_TO_I32_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64ToI32U(v)
			i.fr.ip++
		}
	},
	instr.F64_TO_I64_S: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.boxI64(i.satI64(v))
			i.fr.ip++
		}
	},
	instr.F64_TO_I64_U: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.boxI64(int64(i.satU64(v)))
			i.fr.ip++
		}
	},
	instr.F64_TO_F32: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.f64ToF32(v)
			i.fr.ip++
		}
	},
	instr.F64_REINTERPRET_I64: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.f64ReinterpretI64(v)
			i.fr.ip++
		}
	},
	instr.STRING_NEW_UTF32: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := unboxRef[types.TypedArray[int32]](i, i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxRef(int(i.intern(string(types.String(val)))))
			i.fr.ip++
		}
	},
	instr.STRING_LEN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := unboxRef[types.String](i, i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxI32(int32(len(v)))
			i.fr.ip++
		}
	},
	instr.STRING_CONCAT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxRef(int(i.intern(string(v2 + v1))))
			i.fr.ip++
		}
	},
	instr.STRING_EQ: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1]
			v2 := i.stack[i.sp-2]
			i.releaseBox(v1)
			i.releaseBox(v2)
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 == v1)
			i.fr.ip++
		}
	},
	instr.STRING_NE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1]
			v2 := i.stack[i.sp-2]
			i.releaseBox(v1)
			i.releaseBox(v2)
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 != v1)
			i.fr.ip++
		}
	},
	instr.STRING_LT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 < v1)
			i.fr.ip++
		}
	},
	instr.STRING_GT: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 > v1)
			i.fr.ip++
		}
	},
	instr.STRING_LE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 <= v1)
			i.fr.ip++
		}
	},
	instr.STRING_GE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxI1(v2 >= v1)
			i.fr.ip++
		}
	},
	instr.STRING_ENCODE_UTF32: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := unboxRef[types.String](i, i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxRef(i.alloc(types.TypedArray[int32](val)))
			i.fr.ip++
		}
	},
	instr.STRING_ITER: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-1]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			val, ok := i.heap[addr].(types.String)
			if !ok {
				panic(ErrTypeMismatch)
			}
			iter := types.NewStringIterator(types.Ref(addr), val)
			iter.Next()
			i.stack[i.sp-1] = types.BoxRef(i.keep(iter))
			i.fr.ip++
		}
	},
	instr.ARRAY_NEW: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ, ok := c.types[idx].(*types.ArrayType)
		if !ok {
			return func(i *Interpreter) {
				panic(ErrTypeMismatch)
			}
		}
		switch typ.ElemKind {
		case types.KindI1:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := int(i.stack[i.sp-1].I32())
				if i.sp < size+1 {
					panic(ErrStackUnderflow)
				}
				val := make(types.TypedArray[bool], size)
				for j := 0; j < size; j++ {
					val[j] = i.stack[i.sp-size-1+j].Bool()
				}
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindI8:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := int(i.stack[i.sp-1].I32())
				if i.sp < size+1 {
					panic(ErrStackUnderflow)
				}
				val := make(types.TypedArray[int8], size)
				for j := 0; j < size; j++ {
					val[j] = int8(i.stack[i.sp-size-1+j].I32())
				}
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindI32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := int(i.stack[i.sp-1].I32())
				if i.sp < size+1 {
					panic(ErrStackUnderflow)
				}
				val := make(types.TypedArray[int32], size)
				for j := 0; j < size; j++ {
					val[j] = i.stack[i.sp-size-1+j].I32()
				}
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindI64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := int(i.stack[i.sp-1].I32())
				if i.sp < size+1 {
					panic(ErrStackUnderflow)
				}
				val := make(types.TypedArray[int64], size)
				for j := 0; j < size; j++ {
					val[j] = i.unboxI64(i.stack[i.sp-size-1+j])
				}
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindF32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := int(i.stack[i.sp-1].I32())
				if i.sp < size+1 {
					panic(ErrStackUnderflow)
				}
				val := make(types.TypedArray[float32], size)
				for j := 0; j < size; j++ {
					val[j] = i.stack[i.sp-size-1+j].F32()
				}
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindF64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := int(i.stack[i.sp-1].I32())
				if i.sp < size+1 {
					panic(ErrStackUnderflow)
				}
				val := make(types.TypedArray[float64], size)
				for j := 0; j < size; j++ {
					val[j] = i.stack[i.sp-size-1+j].F64()
				}
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		default:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := int(i.stack[i.sp-1].I32())
				if i.sp < size+1 {
					panic(ErrStackUnderflow)
				}
				val := &types.Array{
					Typ:   typ,
					Elems: make([]types.Boxed, size),
				}
				copy(val.Elems, i.stack[i.sp-size-1:i.sp-1])
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		}
	},
	instr.ARRAY_NEW_DEFAULT: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ, ok := c.types[idx].(*types.ArrayType)
		if !ok {
			return func(i *Interpreter) {
				panic(ErrTypeMismatch)
			}
		}
		switch typ.ElemKind {
		case types.KindI1:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				if size < 0 {
					panic(ErrSegmentationFault)
				}
				val := make(types.TypedArray[bool], size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindI8:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				if size < 0 {
					panic(ErrSegmentationFault)
				}
				val := make(types.TypedArray[int8], size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindI32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				if size < 0 {
					panic(ErrSegmentationFault)
				}
				val := make(types.TypedArray[int32], size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindI64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				if size < 0 {
					panic(ErrSegmentationFault)
				}
				val := make(types.TypedArray[int64], size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindF32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				if size < 0 {
					panic(ErrSegmentationFault)
				}
				val := make(types.TypedArray[float32], size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindF64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				if size < 0 {
					panic(ErrSegmentationFault)
				}
				val := make(types.TypedArray[float64], size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		default:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				if size < 0 {
					panic(ErrSegmentationFault)
				}
				val := &types.Array{
					Typ:   typ,
					Elems: make([]types.Boxed, size),
				}
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		}
	},
	instr.ARRAY_LEN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			var n int32
			switch arr := i.unbox(i.stack[i.sp-1]).(type) {
			case types.TypedArray[bool]:
				n = int32(len(arr))
			case types.TypedArray[int8]:
				n = int32(len(arr))
			case types.TypedArray[int32]:
				n = int32(len(arr))
			case types.TypedArray[int64]:
				n = int32(len(arr))
			case types.TypedArray[float32]:
				n = int32(len(arr))
			case types.TypedArray[float64]:
				n = int32(len(arr))
			case *types.Array:
				n = int32(len(arr.Elems))
			default:
				panic(ErrTypeMismatch)
			}
			i.stack[i.sp-1] = types.BoxI32(n)
			i.fr.ip++
		}
	},
	instr.ARRAY_GET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			val := i.arrayGet()
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip++
		}
	},
	instr.ARRAY_SET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 3 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1]
			idx := int(i.stack[i.sp-2].I32())
			ref := i.stack[i.sp-3]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch arr := i.heap[addr].(type) {
			case types.TypedArray[bool]:
				i.bounds(idx, 1, len(arr))
				arr[idx] = val.Bool()
			case types.TypedArray[int8]:
				i.bounds(idx, 1, len(arr))
				arr[idx] = int8(val.I32())
			case types.TypedArray[int32]:
				i.bounds(idx, 1, len(arr))
				arr[idx] = val.I32()
			case types.TypedArray[int64]:
				i.bounds(idx, 1, len(arr))
				arr[idx] = i.unboxI64(val)
			case types.TypedArray[float32]:
				i.bounds(idx, 1, len(arr))
				arr[idx] = val.F32()
			case types.TypedArray[float64]:
				i.bounds(idx, 1, len(arr))
				arr[idx] = val.F64()
			case *types.Array:
				i.bounds(idx, 1, len(arr.Elems))
				elem := arr.Elems[idx]
				arr.Elems[idx] = val
				i.releaseBox(elem)
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp -= 3
			i.fr.ip++
		}
	},
	instr.ARRAY_FILL: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 4 {
				panic(ErrStackUnderflow)
			}
			size := int(i.stack[i.sp-1].I32())
			val := i.stack[i.sp-2]
			idx := int(i.stack[i.sp-3].I32())
			ref := i.stack[i.sp-4]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch arr := i.heap[addr].(type) {
			case types.TypedArray[bool]:
				i.bounds(idx, size, len(arr))
				v := val.Bool()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[int8]:
				i.bounds(idx, size, len(arr))
				v := int8(val.I32())
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[int32]:
				i.bounds(idx, size, len(arr))
				v := val.I32()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[int64]:
				i.bounds(idx, size, len(arr))
				v := i.unboxI64(val)
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[float32]:
				i.bounds(idx, size, len(arr))
				v := val.F32()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[float64]:
				i.bounds(idx, size, len(arr))
				v := val.F64()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case *types.Array:
				i.bounds(idx, size, len(arr.Elems))
				// The stack transfers one reference to val; each filled slot needs
				// its own. Retain the extras before overwriting so releasing an old
				// element cannot free val when it aliases that element.
				if val.Kind() == types.KindRef && size > 1 {
					i.retains(val.Ref(), size-1)
				}
				for k := idx; k < idx+size; k++ {
					old := arr.Elems[k]
					arr.Elems[k] = val
					i.releaseBox(old)
				}
				if size <= 0 {
					i.releaseBox(val)
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp -= 4
			i.fr.ip++
		}
	},
	instr.ARRAY_COPY: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 5 {
				panic(ErrStackUnderflow)
			}
			size := int(i.stack[i.sp-1].I32())
			srcOffset := int(i.stack[i.sp-2].I32())
			srcRef := i.stack[i.sp-3]
			dstOffset := int(i.stack[i.sp-4].I32())
			dstRef := i.stack[i.sp-5]
			if srcRef.Kind() != types.KindRef || dstRef.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			srcAddr := srcRef.Ref()
			dstAddr := dstRef.Ref()
			switch dst := i.heap[dstAddr].(type) {
			case types.TypedArray[bool]:
				src, ok := i.heap[srcAddr].(types.TypedArray[bool])
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.bounds(srcOffset, size, len(src))
				i.bounds(dstOffset, size, len(dst))
				copy(dst[dstOffset:dstOffset+size], src[srcOffset:srcOffset+size])
			case types.TypedArray[int8]:
				src, ok := i.heap[srcAddr].(types.TypedArray[int8])
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.bounds(srcOffset, size, len(src))
				i.bounds(dstOffset, size, len(dst))
				copy(dst[dstOffset:dstOffset+size], src[srcOffset:srcOffset+size])
			case types.TypedArray[int32]:
				src, ok := i.heap[srcAddr].(types.TypedArray[int32])
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.bounds(srcOffset, size, len(src))
				i.bounds(dstOffset, size, len(dst))
				copy(dst[dstOffset:dstOffset+size], src[srcOffset:srcOffset+size])
			case types.TypedArray[int64]:
				src, ok := i.heap[srcAddr].(types.TypedArray[int64])
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.bounds(srcOffset, size, len(src))
				i.bounds(dstOffset, size, len(dst))
				copy(dst[dstOffset:dstOffset+size], src[srcOffset:srcOffset+size])
			case types.TypedArray[float32]:
				src, ok := i.heap[srcAddr].(types.TypedArray[float32])
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.bounds(srcOffset, size, len(src))
				i.bounds(dstOffset, size, len(dst))
				copy(dst[dstOffset:dstOffset+size], src[srcOffset:srcOffset+size])
			case types.TypedArray[float64]:
				src, ok := i.heap[srcAddr].(types.TypedArray[float64])
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.bounds(srcOffset, size, len(src))
				i.bounds(dstOffset, size, len(dst))
				copy(dst[dstOffset:dstOffset+size], src[srcOffset:srcOffset+size])
			case *types.Array:
				src, ok := i.heap[srcAddr].(*types.Array)
				if !ok {
					panic(ErrTypeMismatch)
				}
				i.bounds(srcOffset, size, len(src.Elems))
				i.bounds(dstOffset, size, len(dst.Elems))
				for _, v := range src.Elems[srcOffset : srcOffset+size] {
					i.retainBox(v)
				}
				for _, v := range dst.Elems[dstOffset : dstOffset+size] {
					i.releaseBox(v)
				}
				copy(dst.Elems[dstOffset:dstOffset+size], src.Elems[srcOffset:srcOffset+size])
			default:
				panic(ErrTypeMismatch)
			}
			i.release(srcAddr)
			i.release(dstAddr)
			i.sp -= 5
			i.fr.ip++
		}
	},
	instr.ARRAY_APPEND: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 1 {
				panic(ErrStackUnderflow)
			}
			n := int(i.stack[i.sp-1].I32())
			if n < 0 || i.sp < n+2 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-n-2]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			base := i.sp - n - 1
			switch arr := i.heap[addr].(type) {
			case types.TypedArray[bool]:
				for k := 0; k < n; k++ {
					arr = append(arr, i.stack[base+k].Bool())
				}
				i.heap[addr] = arr
			case types.TypedArray[int8]:
				for k := 0; k < n; k++ {
					arr = append(arr, int8(i.stack[base+k].I32()))
				}
				i.heap[addr] = arr
			case types.TypedArray[int32]:
				for k := 0; k < n; k++ {
					arr = append(arr, i.stack[base+k].I32())
				}
				i.heap[addr] = arr
			case types.TypedArray[int64]:
				for k := 0; k < n; k++ {
					arr = append(arr, i.unboxI64(i.stack[base+k]))
				}
				i.heap[addr] = arr
			case types.TypedArray[float32]:
				for k := 0; k < n; k++ {
					arr = append(arr, i.stack[base+k].F32())
				}
				i.heap[addr] = arr
			case types.TypedArray[float64]:
				for k := 0; k < n; k++ {
					arr = append(arr, i.stack[base+k].F64())
				}
				i.heap[addr] = arr
			case *types.Array:
				for k := 0; k < n; k++ {
					arr.Elems = append(arr.Elems, i.stack[base+k])
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.sp -= n + 1
			i.fr.ip++
		}
	},
	instr.ARRAY_DELETE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			idx := int(i.stack[i.sp-1].I32())
			ref := i.stack[i.sp-2]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			var val types.Boxed
			switch arr := i.heap[addr].(type) {
			case types.TypedArray[bool]:
				i.bounds(idx, 1, len(arr))
				val = types.BoxI1(arr[idx])
				copy(arr[idx:], arr[idx+1:])
				i.heap[addr] = arr[:len(arr)-1]
			case types.TypedArray[int8]:
				i.bounds(idx, 1, len(arr))
				val = types.BoxI8(arr[idx])
				copy(arr[idx:], arr[idx+1:])
				i.heap[addr] = arr[:len(arr)-1]
			case types.TypedArray[int32]:
				i.bounds(idx, 1, len(arr))
				val = types.BoxI32(int32(arr[idx]))
				copy(arr[idx:], arr[idx+1:])
				i.heap[addr] = arr[:len(arr)-1]
			case types.TypedArray[int64]:
				i.bounds(idx, 1, len(arr))
				val = i.boxI64(int64(arr[idx]))
				copy(arr[idx:], arr[idx+1:])
				i.heap[addr] = arr[:len(arr)-1]
			case types.TypedArray[float32]:
				i.bounds(idx, 1, len(arr))
				val = types.BoxF32(float32(arr[idx]))
				copy(arr[idx:], arr[idx+1:])
				i.heap[addr] = arr[:len(arr)-1]
			case types.TypedArray[float64]:
				i.bounds(idx, 1, len(arr))
				val = types.BoxF64(float64(arr[idx]))
				copy(arr[idx:], arr[idx+1:])
				i.heap[addr] = arr[:len(arr)-1]
			case *types.Array:
				i.bounds(idx, 1, len(arr.Elems))
				val = arr.Elems[idx]
				copy(arr.Elems[idx:], arr.Elems[idx+1:])
				arr.Elems[len(arr.Elems)-1] = types.BoxedNull
				arr.Elems = arr.Elems[:len(arr.Elems)-1]
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp--
			i.stack[i.sp-1] = val
			i.fr.ip++
		}
	},
	instr.ARRAY_SLICE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 3 {
				panic(ErrStackUnderflow)
			}
			end := int(i.stack[i.sp-1].I32())
			start := int(i.stack[i.sp-2].I32())
			ref := i.stack[i.sp-3]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			var out types.Value
			switch arr := i.heap[addr].(type) {
			case types.TypedArray[bool]:
				if start < 0 || end > len(arr) || start > end {
					panic(ErrIndexOutOfRange)
				}
				dst := make(types.TypedArray[bool], end-start)
				copy(dst, arr[start:end])
				out = dst
			case types.TypedArray[int8]:
				if start < 0 || end > len(arr) || start > end {
					panic(ErrIndexOutOfRange)
				}
				dst := make(types.TypedArray[int8], end-start)
				copy(dst, arr[start:end])
				out = dst
			case types.TypedArray[int32]:
				if start < 0 || end > len(arr) || start > end {
					panic(ErrIndexOutOfRange)
				}
				dst := make(types.TypedArray[int32], end-start)
				copy(dst, arr[start:end])
				out = dst
			case types.TypedArray[int64]:
				if start < 0 || end > len(arr) || start > end {
					panic(ErrIndexOutOfRange)
				}
				dst := make(types.TypedArray[int64], end-start)
				copy(dst, arr[start:end])
				out = dst
			case types.TypedArray[float32]:
				if start < 0 || end > len(arr) || start > end {
					panic(ErrIndexOutOfRange)
				}
				dst := make(types.TypedArray[float32], end-start)
				copy(dst, arr[start:end])
				out = dst
			case types.TypedArray[float64]:
				if start < 0 || end > len(arr) || start > end {
					panic(ErrIndexOutOfRange)
				}
				dst := make(types.TypedArray[float64], end-start)
				copy(dst, arr[start:end])
				out = dst
			case *types.Array:
				if start < 0 || end > len(arr.Elems) || start > end {
					panic(ErrIndexOutOfRange)
				}
				elems := make([]types.Boxed, end-start)
				copy(elems, arr.Elems[start:end])
				for _, v := range elems {
					i.retainBox(v)
				}
				out = &types.Array{Typ: arr.Typ, Elems: elems}
			default:
				panic(ErrTypeMismatch)
			}
			newAddr := i.alloc(out)
			i.release(addr)
			i.sp -= 2
			i.stack[i.sp-1] = types.BoxRef(newAddr)
			i.fr.ip++
		}
	},
	instr.STRUCT_NEW: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ, ok := c.types[idx].(*types.StructType)
		if !ok {
			return func(i *Interpreter) {
				panic(ErrTypeMismatch)
			}
		}
		size := len(typ.Fields)
		return func(i *Interpreter) {
			if i.sp < size {
				panic(ErrStackUnderflow)
			}
			s := types.NewStruct(typ)
			for j, f := range typ.Fields {
				val := i.stack[i.sp-size+j]
				switch f.Kind {
				case types.KindI32, types.KindI8, types.KindI1, types.KindF32, types.KindF64, types.KindRef:
					s.SetField(j, val)
				case types.KindI64:
					s.SetRaw(j, uint64(i.unboxI64(val)))
				default:
					panic(ErrTypeMismatch)
				}
			}
			i.sp -= size - 1
			i.stack[i.sp-1] = types.BoxRef(i.alloc(s))
			i.fr.ip += 3
		}
	},
	instr.STRUCT_NEW_DEFAULT: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ, ok := c.types[idx].(*types.StructType)
		if !ok {
			return func(i *Interpreter) {
				panic(ErrTypeMismatch)
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			s := types.NewStruct(typ)
			i.stack[i.sp] = types.BoxRef(i.alloc(s))
			i.sp++
			i.fr.ip += 3
		}
	},
	instr.STRUCT_GET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			val := i.structGet()
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip++
		}
	},
	instr.STRUCT_SET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 3 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1]
			idx := int(i.stack[i.sp-2].I32())
			ref := i.stack[i.sp-3]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch s := i.heap[addr].(type) {
			case *types.Struct:
				typ := s.Typ
				if idx < 0 || idx >= len(typ.Fields) {
					panic(ErrSegmentationFault)
				}
				field := typ.Fields[idx]
				switch field.Kind {
				case types.KindI32:
					s.Data[idx] = uint64(uint32(val.I32()))
				case types.KindI8:
					s.Data[idx] = uint64(uint32(int32(val.I8())))
				case types.KindI1:
					if val.Bool() {
						s.Data[idx] = 1
					} else {
						s.Data[idx] = 0
					}
				case types.KindI64:
					s.Data[idx] = uint64(i.unboxI64(val))
				case types.KindF32:
					s.Data[idx] = uint64(math.Float32bits(val.F32()))
				case types.KindF64:
					s.Data[idx] = math.Float64bits(val.F64())
				case types.KindRef:
					old := types.Boxed(s.Data[idx])
					i.releaseBox(old)
					s.Data[idx] = uint64(val)
				default:
					panic(ErrTypeMismatch)
				}
			case *HostObject:
				typ := s.Typ
				if idx < 0 || idx >= len(typ.Fields) {
					panic(ErrSegmentationFault)
				}
				field := typ.Fields[idx]
				switch field.Kind {
				case types.KindI32, types.KindI8, types.KindI1, types.KindF32, types.KindF64:
					s.SetField(idx, val)
				case types.KindI64:
					s.SetRaw(idx, uint64(i.unboxI64(val)))
				case types.KindRef:
					old := types.Boxed(s.Raw(idx))
					i.releaseBox(old)
					s.SetRaw(idx, uint64(val))
				default:
					panic(ErrTypeMismatch)
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp -= 3
			i.fr.ip++
		}
	},
	instr.MAP_NEW: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ, ok := c.types[idx].(*types.MapType)
		if !ok {
			return func(i *Interpreter) {
				panic(ErrTypeMismatch)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 1 {
				panic(ErrStackUnderflow)
			}
			size := int(i.stack[i.sp-1].I32())
			if size < 0 {
				panic(ErrIndexOutOfRange)
			}
			if i.sp < size*2+1 {
				panic(ErrStackUnderflow)
			}
			m := types.NewMapForType(typ, size)
			base := i.sp - 1 - size*2
			for j := 0; j < size; j++ {
				key := i.stack[base+j*2]
				value := i.stack[base+j*2+1]
				switch m := m.(type) {
				case *types.TypedMap[int8]:
					old, ok := m.Set(key.I8(), value)
					if ok {
						i.releaseBox(old)
					}
				case *types.TypedMap[bool]:
					old, ok := m.Set(key.Bool(), value)
					if ok {
						i.releaseBox(old)
					}
				case *types.TypedMap[int32]:
					old, ok := m.Set(key.I32(), value)
					if ok {
						i.releaseBox(old)
					}
				case *types.TypedMap[int64]:
					old, ok := m.Set(i.unboxI64(key), value)
					if ok {
						i.releaseBox(old)
					}
				case *types.TypedMap[float32]:
					old, ok := m.Set(key.F32(), value)
					if ok {
						i.releaseBox(old)
					}
				case *types.TypedMap[float64]:
					old, ok := m.Set(key.F64(), value)
					if ok {
						i.releaseBox(old)
					}
				case *types.Map:
					var k types.MapKey
					entry := types.MapEntry{Value: value}
					keyRef := 0
					drop := false
					switch key.Kind() {
					case types.KindI32:
						bits := uint64(uint32(key.I32()))
						k = types.MapKey{Kind: types.KindI32, Bits: bits}
						entry.Key = types.BoxI32(int32(bits))
					case types.KindI64:
						k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
					case types.KindF32:
						bits := math.Float32bits(key.F32())
						if bits == 1<<31 {
							bits = 0
						}
						k = types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}
						entry.Key = types.BoxF32(math.Float32frombits(bits))
					case types.KindF64:
						bits := math.Float64bits(key.F64())
						if bits == 1<<63 {
							bits = 0
						}
						k = types.MapKey{Kind: types.KindF64, Bits: bits}
						entry.Key = types.BoxF64(math.Float64frombits(bits))
					case types.KindRef:
						keyRef = key.Ref()
						if _, ok := i.heap[keyRef].(types.I64); ok {
							k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
						} else {
							k = types.MapKey{Kind: types.KindRef, Bits: uint64(keyRef)}
							entry.Key = key
							drop = true
						}
					default:
						panic(ErrTypeMismatch)
					}
					old, ok := m.Set(k, entry)
					if ok {
						if drop {
							i.release(keyRef)
						}
						i.releaseBox(old.Value)
					}
				default:
					panic(ErrTypeMismatch)
				}
			}
			var addr int
			if typ.TraceKeys || typ.TraceValues {
				addr = i.keep(m)
			} else {
				addr = i.alloc(m)
			}
			i.sp = base + 1
			i.stack[base] = types.BoxRef(addr)
			i.fr.ip += 3
		}
	},
	instr.MAP_NEW_DEFAULT: func(c *threader) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx >= len(c.types) {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		typ, ok := c.types[idx].(*types.MapType)
		if !ok {
			return func(i *Interpreter) {
				panic(ErrTypeMismatch)
			}
		}
		return func(i *Interpreter) {
			if i.sp < 1 {
				panic(ErrStackUnderflow)
			}
			capacity := int(i.stack[i.sp-1].I32())
			if capacity < 0 {
				panic(ErrIndexOutOfRange)
			}
			i.stack[i.sp-1] = types.BoxRef(i.alloc(types.NewMapForType(typ, capacity)))
			i.fr.ip += 3
		}
	},
	instr.MAP_LEN: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 1 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-1]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			n := 0
			switch m := i.heap[addr].(type) {
			case *types.TypedMap[int8]:
				n = m.Len()
			case *types.TypedMap[bool]:
				n = m.Len()
			case *types.TypedMap[int32]:
				n = m.Len()
			case *types.TypedMap[int64]:
				n = m.Len()
			case *types.TypedMap[float32]:
				n = m.Len()
			case *types.TypedMap[float64]:
				n = m.Len()
			case *types.Map:
				n = m.Len()
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.stack[i.sp-1] = types.BoxI32(int32(n))
			i.fr.ip++
		}
	},
	instr.MAP_GET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			key := i.stack[i.sp-1]
			ref := i.stack[i.sp-2]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			var result types.Boxed
			switch m := i.heap[addr].(type) {
			case *types.TypedMap[int8]:
				value, ok := m.Get(key.I8())
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.TypedMap[bool]:
				value, ok := m.Get(key.Bool())
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.TypedMap[int32]:
				value, ok := m.Get(key.I32())
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.TypedMap[int64]:
				value, ok := m.Get(i.unboxI64(key))
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.TypedMap[float32]:
				value, ok := m.Get(key.F32())
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.TypedMap[float64]:
				value, ok := m.Get(key.F64())
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.Map:
				var k types.MapKey
				keyRef := 0
				drop := false
				switch key.Kind() {
				case types.KindI32:
					k = types.MapKey{Kind: types.KindI32, Bits: uint64(uint32(key.I32()))}
				case types.KindI64:
					k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
				case types.KindF32:
					bits := math.Float32bits(key.F32())
					if bits == 1<<31 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}
				case types.KindF64:
					bits := math.Float64bits(key.F64())
					if bits == 1<<63 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF64, Bits: bits}
				case types.KindRef:
					keyRef = key.Ref()
					if _, ok := i.heap[keyRef].(types.I64); ok {
						k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
					} else {
						k = types.MapKey{Kind: types.KindRef, Bits: uint64(keyRef)}
						drop = true
					}
				default:
					panic(ErrTypeMismatch)
				}
				entry, ok := m.Get(k)
				if drop {
					i.release(keyRef)
				}
				if ok {
					result = entry.Value
				} else {
					result = m.Zero
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.retainBox(result)
			i.release(addr)
			i.sp--
			i.stack[i.sp-1] = result
			i.fr.ip++
		}
	},
	instr.MAP_LOOKUP: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			key := i.stack[i.sp-1]
			ref := i.stack[i.sp-2]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			var result types.Boxed
			var found bool
			switch m := i.heap[addr].(type) {
			case *types.TypedMap[int8]:
				result, found = m.Get(key.I8())
				if !found {
					result = m.Zero
				}
			case *types.TypedMap[bool]:
				result, found = m.Get(key.Bool())
				if !found {
					result = m.Zero
				}
			case *types.TypedMap[int32]:
				result, found = m.Get(key.I32())
				if !found {
					result = m.Zero
				}
			case *types.TypedMap[int64]:
				result, found = m.Get(i.unboxI64(key))
				if !found {
					result = m.Zero
				}
			case *types.TypedMap[float32]:
				result, found = m.Get(key.F32())
				if !found {
					result = m.Zero
				}
			case *types.TypedMap[float64]:
				result, found = m.Get(key.F64())
				if !found {
					result = m.Zero
				}
			case *types.Map:
				var k types.MapKey
				keyRef := 0
				drop := false
				switch key.Kind() {
				case types.KindI32:
					k = types.MapKey{Kind: types.KindI32, Bits: uint64(uint32(key.I32()))}
				case types.KindI64:
					k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
				case types.KindF32:
					bits := math.Float32bits(key.F32())
					if bits == 1<<31 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}
				case types.KindF64:
					bits := math.Float64bits(key.F64())
					if bits == 1<<63 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF64, Bits: bits}
				case types.KindRef:
					keyRef = key.Ref()
					if _, ok := i.heap[keyRef].(types.I64); ok {
						k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
					} else {
						k = types.MapKey{Kind: types.KindRef, Bits: uint64(keyRef)}
						drop = true
					}
				default:
					panic(ErrTypeMismatch)
				}
				entry, ok := m.Get(k)
				if drop {
					i.release(keyRef)
				}
				found = ok
				if ok {
					result = entry.Value
				} else {
					result = m.Zero
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.retainBox(result)
			i.release(addr)
			i.stack[i.sp-2] = result
			i.stack[i.sp-1] = types.BoxI1(found)
			i.fr.ip++
		}
	},
	instr.MAP_SET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 3 {
				panic(ErrStackUnderflow)
			}
			value := i.stack[i.sp-1]
			key := i.stack[i.sp-2]
			ref := i.stack[i.sp-3]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch m := i.heap[addr].(type) {
			case *types.TypedMap[int8]:
				old, ok := m.Set(key.I8(), value)
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[bool]:
				old, ok := m.Set(key.Bool(), value)
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[int32]:
				old, ok := m.Set(key.I32(), value)
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[int64]:
				old, ok := m.Set(i.unboxI64(key), value)
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[float32]:
				old, ok := m.Set(key.F32(), value)
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[float64]:
				old, ok := m.Set(key.F64(), value)
				if ok {
					i.releaseBox(old)
				}
			case *types.Map:
				var k types.MapKey
				entry := types.MapEntry{Value: value}
				keyRef := 0
				drop := false
				switch key.Kind() {
				case types.KindI32:
					bits := uint64(uint32(key.I32()))
					k = types.MapKey{Kind: types.KindI32, Bits: bits}
					entry.Key = types.BoxI32(int32(bits))
				case types.KindI64:
					k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
				case types.KindF32:
					bits := math.Float32bits(key.F32())
					if bits == 1<<31 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}
					entry.Key = types.BoxF32(math.Float32frombits(bits))
				case types.KindF64:
					bits := math.Float64bits(key.F64())
					if bits == 1<<63 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF64, Bits: bits}
					entry.Key = types.BoxF64(math.Float64frombits(bits))
				case types.KindRef:
					keyRef = key.Ref()
					if _, ok := i.heap[keyRef].(types.I64); ok {
						k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
					} else {
						k = types.MapKey{Kind: types.KindRef, Bits: uint64(keyRef)}
						entry.Key = key
						drop = true
					}
				default:
					panic(ErrTypeMismatch)
				}
				old, ok := m.Set(k, entry)
				if ok {
					if drop {
						i.release(keyRef)
					}
					i.releaseBox(old.Value)
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp -= 3
			i.fr.ip++
		}
	},
	instr.MAP_DELETE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			key := i.stack[i.sp-1]
			ref := i.stack[i.sp-2]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch m := i.heap[addr].(type) {
			case *types.TypedMap[int8]:
				old, ok := m.Delete(key.I8())
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[bool]:
				old, ok := m.Delete(key.Bool())
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[int32]:
				old, ok := m.Delete(key.I32())
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[int64]:
				old, ok := m.Delete(i.unboxI64(key))
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[float32]:
				old, ok := m.Delete(key.F32())
				if ok {
					i.releaseBox(old)
				}
			case *types.TypedMap[float64]:
				old, ok := m.Delete(key.F64())
				if ok {
					i.releaseBox(old)
				}
			case *types.Map:
				var k types.MapKey
				keyRef := 0
				drop := false
				switch key.Kind() {
				case types.KindI32:
					k = types.MapKey{Kind: types.KindI32, Bits: uint64(uint32(key.I32()))}
				case types.KindI64:
					k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
				case types.KindF32:
					bits := math.Float32bits(key.F32())
					if bits == 1<<31 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}
				case types.KindF64:
					bits := math.Float64bits(key.F64())
					if bits == 1<<63 {
						bits = 0
					}
					k = types.MapKey{Kind: types.KindF64, Bits: bits}
				case types.KindRef:
					keyRef = key.Ref()
					if _, ok := i.heap[keyRef].(types.I64); ok {
						k = types.MapKey{Kind: types.KindI64, Bits: uint64(i.unboxI64(key))}
					} else {
						k = types.MapKey{Kind: types.KindRef, Bits: uint64(keyRef)}
						drop = true
					}
				default:
					panic(ErrTypeMismatch)
				}
				old, ok := m.Delete(k)
				if ok {
					i.releaseBox(old.Key)
					i.releaseBox(old.Value)
				}
				if drop {
					i.release(keyRef)
				}
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp -= 2
			i.fr.ip++
		}
	},
	instr.MAP_CLEAR: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 1 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-1]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch m := i.heap[addr].(type) {
			case *types.TypedMap[int8]:
				m.Clear(func(value types.Boxed) {
					i.releaseBox(value)
				})
			case *types.TypedMap[bool]:
				m.Clear(func(value types.Boxed) {
					i.releaseBox(value)
				})
			case *types.TypedMap[int32]:
				m.Clear(func(value types.Boxed) {
					i.releaseBox(value)
				})
			case *types.TypedMap[int64]:
				m.Clear(func(value types.Boxed) {
					i.releaseBox(value)
				})
			case *types.TypedMap[float32]:
				m.Clear(func(value types.Boxed) {
					i.releaseBox(value)
				})
			case *types.TypedMap[float64]:
				m.Clear(func(value types.Boxed) {
					i.releaseBox(value)
				})
			case *types.Map:
				m.Clear(func(entry types.MapEntry) {
					i.releaseBox(entry.Key)
					i.releaseBox(entry.Value)
				})
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp--
			i.fr.ip++
		}
	},
	instr.MAP_KEYS: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 1 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-1]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			var keyType types.Type
			var elems []types.Boxed
			switch m := i.heap[addr].(type) {
			case *types.TypedMap[int8]:
				keyType = m.Typ.Key
				elems = make([]types.Boxed, 0, m.Len())
				m.Range(func(k int8, _ types.Boxed) {
					elems = append(elems, types.BoxI8(k))
				})
			case *types.TypedMap[bool]:
				keyType = m.Typ.Key
				elems = make([]types.Boxed, 0, m.Len())
				m.Range(func(k bool, _ types.Boxed) {
					elems = append(elems, types.BoxI1(k))
				})
			case *types.TypedMap[int32]:
				keyType = m.Typ.Key
				elems = make([]types.Boxed, 0, m.Len())
				m.Range(func(k int32, _ types.Boxed) {
					elems = append(elems, types.BoxI32(k))
				})
			case *types.TypedMap[int64]:
				keyType = m.Typ.Key
				elems = make([]types.Boxed, 0, m.Len())
				m.Range(func(k int64, _ types.Boxed) {
					elems = append(elems, i.boxI64(k))
				})
			case *types.TypedMap[float32]:
				keyType = m.Typ.Key
				elems = make([]types.Boxed, 0, m.Len())
				m.Range(func(k float32, _ types.Boxed) {
					elems = append(elems, types.BoxF32(k))
				})
			case *types.TypedMap[float64]:
				keyType = m.Typ.Key
				elems = make([]types.Boxed, 0, m.Len())
				m.Range(func(k float64, _ types.Boxed) {
					elems = append(elems, types.BoxF64(k))
				})
			case *types.Map:
				keyType = m.Typ.Key
				elems = make([]types.Boxed, 0, m.Len())
				m.Range(func(_ types.MapKey, entry types.MapEntry) {
					i.retainBox(entry.Key)
					elems = append(elems, entry.Key)
				})
			default:
				panic(ErrTypeMismatch)
			}
			arr := &types.Array{Typ: types.NewArrayType(keyType), Elems: elems}
			out := types.BoxRef(i.alloc(arr))
			i.release(addr)
			i.stack[i.sp-1] = out
			i.fr.ip++
		}
	},
	instr.MAP_ITER: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 1 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-1]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch i.heap[addr].(type) {
			case *types.TypedMap[int8],
				*types.TypedMap[bool],
				*types.TypedMap[int32],
				*types.TypedMap[int64],
				*types.TypedMap[float32],
				*types.TypedMap[float64],
				*types.Map:
			default:
				panic(ErrTypeMismatch)
			}
			iter := types.NewMapIterator(types.Ref(addr), i.heap[addr])
			iter.Next()
			i.retainIteratorCurrent(iter)
			i.stack[i.sp-1] = types.BoxRef(i.keep(iter))
			i.fr.ip++
		}
	},
	instr.CLOSURE_NEW: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			ref := i.stack[i.sp-1]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			fn, ok := i.heap[addr].(*types.Function)
			if !ok {
				panic(ErrTypeMismatch)
			}
			n := len(fn.Captures)
			if i.sp < n+1 {
				panic(ErrStackUnderflow)
			}
			upvals := make([]types.Boxed, n)
			copy(upvals, i.stack[i.sp-1-n:i.sp-1])
			cl := types.NewClosure(fn.Typ, types.Ref(addr), upvals)
			caddr := i.keep(cl)
			i.sp -= n
			i.stack[i.sp-1] = types.BoxRef(caddr)
			i.fr.ip++
		}
	},

	instr.THROW: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			i.sp--
			exc := i.stack[i.sp]
			if fp, h, ok := i.handler(); ok {
				i.land(fp, h, exc)
				return
			}
			panic(escape{i.uncaught(exc)})
		}
	},

	instr.ERROR_NEW: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			code := i.stack[i.sp-1]
			if code.Kind() != types.KindI32 {
				panic(ErrTypeMismatch)
			}
			payload := i.stack[i.sp-2]
			// The payload's reference transfers from the stack slot into the new
			// Error's value field, so it is overwritten without a release. The
			// i32 code slot is dropped.
			addr := i.keep(types.NewError(types.ErrorCode(code.I32()), i.message(payload), payload))
			i.sp--
			i.stack[i.sp-1] = types.BoxRef(addr)
			i.fr.ip++
		}
	},

	instr.ERROR_GET: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			box := i.stack[i.sp-1]
			if box.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			e, ok := i.heap[box.Ref()].(*types.Error)
			if !ok {
				panic(ErrTypeMismatch)
			}
			val := e.Value()
			i.retainBox(val)
			i.releaseBox(box)
			i.stack[i.sp-1] = val
			i.fr.ip++
		}
	},
	instr.ERROR_CODE: func(c *threader) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			box := i.stack[i.sp-1]
			if box.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			e, ok := i.heap[box.Ref()].(*types.Error)
			if !ok {
				panic(ErrTypeMismatch)
			}
			code := e.Code()
			i.releaseBox(box)
			i.stack[i.sp-1] = types.BoxI32(int32(code))
			i.fr.ip++
		}
	},
}

var invalid = func(_ *Interpreter) {
	panic(ErrUnknownOpcode)
}

func init() {
	for i, fn := range threaded {
		if fn == nil {
			threaded[i] = func(c *threader) func(*Interpreter) {
				inst := instr.Instruction(c.code[c.ip:])
				c.ip += inst.Width()
				return invalid
			}
		}
	}
}

func (c *threader) Compile(code []byte, locals []types.Kind, captures []types.Kind) []func(*Interpreter) {
	c.code = code
	c.locals = locals
	c.captures = captures
	c.ip = 0

	compiled := make([]func(*Interpreter), len(code))
	for c.ip < len(code) {
		ip := c.ip
		compiled[ip] = threaded[code[ip]](c)
	}
	for ip := 0; ip < len(code); ip++ {
		if compiled[ip] == nil {
			compiled[ip] = invalid
		}
	}
	return compiled
}

// callHost invokes a host function in place, replacing its arguments and the
// funcref on the stack with the call's results and releasing any consumed ref
// arguments. It does not push a VM frame.
func (i *Interpreter) callHost(fn *HostFunction) {
	params := len(fn.Typ.Params)
	returns := len(fn.Typ.Returns)
	if i.sp <= params {
		panic(ErrStackUnderflow)
	}
	if i.sp+returns-params-1 > len(i.stack) {
		panic(ErrStackOverflow)
	}
	args := i.stack[i.sp-params-1 : i.sp-1]
	out, err := fn.Fn(i, args)
	if err != nil {
		panic(err)
	}
	i.releaseArgs(args, out)
	i.releaseArgs(i.stack[i.sp-1:i.sp], out)
	i.sp += returns - params - 1
	copy(i.stack[i.sp-returns:i.sp], out)
	i.fr.ip++
}

// releaseArgs releases each ref-kind value in args that rets does not return
// unchanged, i.e. every arg whose ownership the host call consumed rather than
// handing back to the caller. Non-ref args need no bookkeeping and are
// skipped. callHostDirect skips this scan when a host signature cannot carry
// heap refs.
func (i *Interpreter) releaseArgs(args, rets []types.Boxed) {
	for _, val := range args {
		if val.Kind() != types.KindRef {
			continue
		}
		kept := false
		for _, r := range rets {
			if r == val {
				kept = true
				break
			}
		}
		if !kept {
			i.release(val.Ref())
		}
	}
}

// ret pops the current frame, moving its return values down to the frame base
// and releasing the frame's function ref. The caller checks for frame underflow.
func (i *Interpreter) ret() {
	f := i.fr
	if i.sp < f.returns {
		panic(ErrStackUnderflow)
	}
	switch f.returns {
	case 0:
	case 1:
		i.stack[f.bp] = i.stack[i.sp-1]
	default:
		copy(i.stack[f.bp:f.bp+f.returns], i.stack[i.sp-f.returns:i.sp])
	}
	i.sp = f.bp + f.returns
	if f.release {
		i.release(f.ref)
	}
	f.code = nil
	i.fp--
	i.fr = &i.frames[i.fp-1]
}

// tail performs a tail call to a *Function (or *Closure) body. Above the entry
// frame it reuses the current frame in place, so tail recursion runs in constant
// frame depth; at the entry frame it pushes a new frame instead, because a reused
// entry frame's callee would hit ErrFrameUnderflow on its own RETURN. The funcref
// at the top of the stack and its arguments below it transfer into the new frame.
func (i *Interpreter) tail(code, ref int, upvals []types.Boxed, params, returns, locals int) {
	if i.sp <= params {
		panic(ErrStackUnderflow)
	}
	if i.fp == 1 {
		if i.fp == len(i.frames) {
			panic(ErrFrameOverflow)
		}
		if i.sp+locals-1 > len(i.stack) {
			panic(ErrStackOverflow)
		}
		if locals > 0 {
			clear(i.stack[i.sp-1 : i.sp+locals-1])
		}
		f := &i.frames[i.fp]
		f.code = i.code[code]
		f.upvals = upvals
		f.addr = code
		f.ref = ref
		f.ip = 0
		f.bp = i.sp - params - 1
		f.returns = returns
		f.release = true
		i.sp = f.bp + params + locals
		i.fr.ip++
		i.fp++
		i.fr = f
		return
	}

	f := i.fr
	base := f.bp
	if base+params+locals > len(i.stack) {
		panic(ErrStackOverflow)
	}
	copy(i.stack[base:base+params], i.stack[i.sp-params-1:i.sp-1])
	if f.release {
		i.release(f.ref)
	}
	if locals > 0 {
		clear(i.stack[base+params : base+params+locals])
	}
	f.code = i.code[code]
	f.upvals = upvals
	f.addr = code
	f.ref = ref
	f.ip = 0
	f.bp = base
	f.returns = returns
	f.release = true
	f.coro = 0
	i.sp = base + params + locals
}

// suspend captures the current coroutine frame into its Coroutine heap object on
// a YIELD, unwinds to the caller, and delivers the handle in the slot the call
// produced. The yielded value (top of stack) becomes co.value; the frame's
// locals and operand stack move into co.image with ownership transferred, so the
// collector keeps them live through the Coroutine while suspended. The caller's
// ip was already advanced by the CALL or RESUME that activated the frame.
func (i *Interpreter) suspend() {
	f := i.fr
	coAddr := f.coro
	co, ok := i.heap[coAddr].(*Coroutine)
	if !ok {
		panic(ErrTypeMismatch)
	}

	i.sp--
	co.value = i.stack[i.sp]
	co.addr = f.addr
	co.ref = f.ref
	co.returns = f.returns
	co.release = f.release
	co.ip = f.ip
	co.upvals = f.upvals
	co.image = append(co.image[:0], i.stack[f.bp:i.sp]...)

	bp := f.bp
	clear(i.stack[bp:i.sp])
	f.code = nil
	f.upvals = nil
	f.coro = 0
	i.fp--
	i.fr = &i.frames[i.fp-1]

	i.stack[bp] = types.BoxRef(coAddr)
	i.sp = bp + 1
}

// finish completes a coroutine frame on RETURN: it records the final value and
// the done state, releases the function activation, unwinds to the caller, and
// delivers the handle in place of plain return values so a coroutine-function's
// call always yields a single Coroutine handle.
func (i *Interpreter) finish() {
	f := i.fr
	coAddr := f.coro
	co, ok := i.heap[coAddr].(*Coroutine)
	if !ok {
		panic(ErrTypeMismatch)
	}
	if i.sp < f.returns {
		panic(ErrStackUnderflow)
	}

	if f.returns > 0 {
		co.value = i.stack[i.sp-1]
	} else {
		co.value = types.BoxedNull
	}
	co.done = true
	co.image = co.image[:0]
	co.upvals = nil
	if f.release {
		i.release(f.ref)
	}
	co.ref = 0
	co.release = false

	bp := f.bp
	f.code = nil
	f.upvals = nil
	f.coro = 0
	i.fp--
	i.fr = &i.frames[i.fp-1]

	i.stack[bp] = types.BoxRef(coAddr)
	i.sp = bp + 1
}

// resume continues a suspended coroutine on RESUME: it pops the handle and the
// resume-in value, restores the captured frame above the resumer's stack, and
// delivers the in value as the result of the pending YIELD. The handle's
// reference moves onto frame.coro, which the collector roots while it runs.
func (i *Interpreter) resume() {
	if i.sp < 2 {
		panic(ErrStackUnderflow)
	}
	if i.fp == len(i.frames) {
		panic(ErrFrameOverflow)
	}
	in := i.stack[i.sp-1]
	box := i.stack[i.sp-2]
	if box.Kind() != types.KindRef {
		panic(ErrTypeMismatch)
	}
	coAddr := box.Ref()
	switch co := i.heap[coAddr].(type) {
	case *Coroutine:
		i.resumeCoroutine(coAddr, co, in)
	case types.Iterator:
		i.resumeIterator(co, in)
	default:
		panic(ErrTypeMismatch)
	}
}

func (i *Interpreter) resumeCoroutine(coAddr int, co *Coroutine, in types.Boxed) {
	if co.done {
		panic(ErrCoroutineDone)
	}

	i.sp -= 2
	base := i.sp
	if base+len(co.image)+1 > len(i.stack) {
		panic(ErrStackOverflow)
	}
	copy(i.stack[base:], co.image)
	i.sp = base + len(co.image)
	i.stack[i.sp] = in
	i.sp++

	f := &i.frames[i.fp]
	f.code = i.code[co.addr]
	f.upvals = co.upvals
	f.addr = co.addr
	f.ref = co.ref
	f.returns = co.returns
	f.release = co.release
	f.ip = co.ip
	f.bp = base
	f.coro = coAddr

	co.image = co.image[:0]
	co.upvals = nil
	co.ref = 0
	co.release = false

	i.fr.ip++
	i.fp++
	i.fr = f
}

func (i *Interpreter) resumeIterator(iter types.Iterator, in types.Boxed) {
	if iter.Done() {
		panic(ErrCoroutineDone)
	}
	i.releaseBox(in)
	i.releaseIteratorCurrent(iter)
	iter.Next()
	i.retainIteratorCurrent(iter)
	i.sp--
	i.fr.ip++
}

func (i *Interpreter) retainIteratorCurrent(iter types.Iterator) {
	if iter.Done() {
		return
	}
	if _, ok := iter.(*types.MapIterator); ok {
		i.retainValue(iter.Current())
	}
}

func (i *Interpreter) releaseIteratorCurrent(iter types.Iterator) {
	if iter.Done() {
		return
	}
	if _, ok := iter.(*types.MapIterator); ok {
		i.releaseValue(iter.Current())
	}
}

func (i *Interpreter) boxIteratorCurrent(val types.Value) types.Boxed {
	if val == nil {
		i.retain(0)
		return types.BoxedNull
	}
	box := i.box(val)
	i.retainValue(val)
	return box
}

func (i *Interpreter) retainValue(val types.Value) {
	switch val := val.(type) {
	case types.Boxed:
		i.retainBox(val)
	case types.Ref:
		i.retain(int(val))
	}
}

func (i *Interpreter) releaseValue(val types.Value) {
	switch val := val.(type) {
	case types.Boxed:
		i.releaseBox(val)
	case types.Ref:
		i.release(int(val))
	}
}

func (i *Interpreter) refGet() types.Boxed {
	if i.sp == 0 {
		panic(ErrStackUnderflow)
	}
	ref := i.stack[i.sp-1]
	if ref.Kind() != types.KindRef {
		panic(ErrTypeMismatch)
	}
	addr := ref.Ref()
	switch i.heap[addr].(type) {
	case types.I32, types.I64, types.F32, types.F64:
	default:
		panic(ErrTypeMismatch)
	}
	val := i.box(i.heap[addr])
	i.release(addr)
	i.sp--
	return val
}

func (i *Interpreter) arrayGet() types.Boxed {
	if i.sp < 2 {
		panic(ErrStackUnderflow)
	}
	idx := int(i.stack[i.sp-1].I32())
	i.sp--
	return i.arrayGetAt(idx)
}

// bounds panics with ErrIndexOutOfRange unless the half-open range
// [offset, offset+size) fits within [0, length). ARRAY_SET, ARRAY_FILL,
// ARRAY_DELETE, ARRAY_COPY, and arrayGetAt each repeat this exact shape once
// per typed-array element kind; sharing it here removes the duplication
// without changing the condition checked or the panic it raises. A
// single-index check is the size == 1 case; ARRAY_COPY calls it once per side
// of the copy.
func (i *Interpreter) bounds(offset, size, length int) {
	if offset < 0 || offset+size > length {
		panic(ErrIndexOutOfRange)
	}
}

func (i *Interpreter) arrayGetAt(idx int) types.Boxed {
	if i.sp == 0 {
		panic(ErrStackUnderflow)
	}
	ref := i.stack[i.sp-1]
	if ref.Kind() != types.KindRef {
		panic(ErrTypeMismatch)
	}
	addr := ref.Ref()
	var val types.Boxed
	switch arr := i.heap[addr].(type) {
	case types.TypedArray[bool]:
		i.bounds(idx, 1, len(arr))
		val = types.BoxI1(arr[idx])
	case types.TypedArray[int8]:
		i.bounds(idx, 1, len(arr))
		val = types.BoxI8(arr[idx])
	case types.TypedArray[int32]:
		i.bounds(idx, 1, len(arr))
		val = types.BoxI32(int32(arr[idx]))
	case types.TypedArray[int64]:
		i.bounds(idx, 1, len(arr))
		val = i.boxI64(int64(arr[idx]))
	case types.TypedArray[float32]:
		i.bounds(idx, 1, len(arr))
		val = types.BoxF32(float32(arr[idx]))
	case types.TypedArray[float64]:
		i.bounds(idx, 1, len(arr))
		val = types.BoxF64(float64(arr[idx]))
	case *types.Array:
		i.bounds(idx, 1, len(arr.Elems))
		elem := arr.Elems[idx]
		i.retainBox(elem)
		val = elem
	default:
		panic(ErrTypeMismatch)
	}
	i.release(addr)
	i.sp--
	return val
}

func (i *Interpreter) structGet() types.Boxed {
	if i.sp < 2 {
		panic(ErrStackUnderflow)
	}
	idx := int(i.stack[i.sp-1].I32())
	i.sp--
	return i.structGetAt(idx)
}

func (i *Interpreter) structGetAt(idx int) types.Boxed {
	if i.sp == 0 {
		panic(ErrStackUnderflow)
	}
	ref := i.stack[i.sp-1]
	if ref.Kind() != types.KindRef {
		panic(ErrTypeMismatch)
	}
	addr := ref.Ref()
	var val types.Boxed
	switch s := i.heap[addr].(type) {
	case *types.Struct:
		if idx < 0 || idx >= len(s.Typ.Fields) {
			panic(ErrSegmentationFault)
		}
		field := s.Typ.Fields[idx]
		switch field.Kind {
		case types.KindI32:
			val = types.BoxI32(int32(uint32(s.Data[idx])))
		case types.KindI8:
			val = types.BoxI8(int8(uint32(s.Data[idx])))
		case types.KindI1:
			val = types.BoxI1(s.Data[idx] != 0)
		case types.KindI64:
			val = i.boxI64(int64(s.Data[idx]))
		case types.KindF32:
			val = types.BoxF32(math.Float32frombits(uint32(s.Data[idx])))
		case types.KindF64:
			val = types.BoxF64(math.Float64frombits(s.Data[idx]))
		case types.KindRef:
			val = types.Boxed(s.Data[idx])
			i.retainBox(val)
		default:
			panic(ErrTypeMismatch)
		}
	case *HostObject:
		typ := s.Typ
		if idx < 0 || idx >= len(typ.Fields) {
			panic(ErrSegmentationFault)
		}
		field := typ.Fields[idx]
		switch field.Kind {
		case types.KindI32, types.KindI8, types.KindI1, types.KindF32, types.KindF64:
			val = s.Field(idx)
		case types.KindI64:
			val = i.boxI64(int64(s.Raw(idx)))
		case types.KindRef:
			val = types.Boxed(s.Raw(idx))
			i.retainBox(val)
		default:
			panic(ErrTypeMismatch)
		}
	default:
		panic(ErrTypeMismatch)
	}
	i.release(addr)
	i.sp--
	return val
}

// branchIf applies a fused comparison+BR_IF's branch decision: taken selects
// whether to add BR_IF's parsed jump offset before advancing i.fr.ip by the
// fused instruction's total consumed width. Every comparison+BR_IF fusion
// (and the CONST+BR_IF fusion) calls this instead of each duplicating the
// offset arithmetic, matching how the standalone BR_IF closure itself adds
// the offset before the instruction width.
func (i *Interpreter) branchIf(taken bool, offset, width int) {
	if taken {
		i.fr.ip += offset
	}
	i.fr.ip += width
}

func (i *Interpreter) i32Add(lhs, rhs int32) types.Boxed {
	return types.BoxI32(lhs + rhs)
}

func (i *Interpreter) i32Sub(lhs, rhs int32) types.Boxed {
	return types.BoxI32(lhs - rhs)
}

func (i *Interpreter) i32Mul(lhs, rhs int32) types.Boxed {
	return types.BoxI32(lhs * rhs)
}

func (i *Interpreter) i32DivS(lhs, rhs int32) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxI32(lhs / rhs)
}

func (i *Interpreter) i32DivU(lhs, rhs int32) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxI32(int32(uint32(lhs) / uint32(rhs)))
}

func (i *Interpreter) i32RemS(lhs, rhs int32) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxI32(lhs % rhs)
}

func (i *Interpreter) i32RemU(lhs, rhs int32) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxI32(int32(uint32(lhs) % uint32(rhs)))
}

func (i *Interpreter) i32Shl(lhs, rhs int32) types.Boxed {
	return types.BoxI32(lhs << (rhs & 0x1F))
}

func (i *Interpreter) i32ShrS(lhs, rhs int32) types.Boxed {
	return types.BoxI32(lhs >> (rhs & 0x1F))
}

func (i *Interpreter) i32ShrU(lhs, rhs int32) types.Boxed {
	return types.BoxI32(int32(uint32(lhs) >> (rhs & 0x1F)))
}

// i32Xor, i32And, i32Or are width-closed. The i32/i8/i1 tag layout makes the
// result kind equal to lhs.kind & rhs.kind, so threaded bitwise ops can combine
// boxed payloads without calling Kind or rebuilding the box through types.Box.
func (i *Interpreter) i32Xor(lhs, rhs types.Boxed) types.Boxed {
	return i.i32Bitwise(lhs, rhs, uint64(lhs)^uint64(rhs))
}

func (i *Interpreter) i32And(lhs, rhs types.Boxed) types.Boxed {
	return types.Boxed(uint64(lhs) & uint64(rhs))
}

func (i *Interpreter) i32Or(lhs, rhs types.Boxed) types.Boxed {
	return i.i32Bitwise(lhs, rhs, uint64(lhs)|uint64(rhs))
}

func (i *Interpreter) i32Bitwise(lhs, rhs types.Boxed, payload uint64) types.Boxed {
	tag := uint64(lhs) & uint64(rhs) & ^uint64(types.VMask)
	return types.Boxed(tag | payload&types.VMask)
}

func (i *Interpreter) i32Clz(v int32) types.Boxed {
	return types.BoxI32(int32(bits.LeadingZeros32(uint32(v))))
}

func (i *Interpreter) i32Ctz(v int32) types.Boxed {
	return types.BoxI32(int32(bits.TrailingZeros32(uint32(v))))
}

func (i *Interpreter) i32Popcnt(v int32) types.Boxed {
	return types.BoxI32(int32(bits.OnesCount32(uint32(v))))
}

func (i *Interpreter) i32Rotl(lhs, rhs int32) types.Boxed {
	return types.BoxI32(int32(bits.RotateLeft32(uint32(lhs), int(rhs))))
}

func (i *Interpreter) i32Rotr(lhs, rhs int32) types.Boxed {
	return types.BoxI32(int32(bits.RotateLeft32(uint32(lhs), -int(rhs))))
}

func (i *Interpreter) i32Extend8S(v int32) types.Boxed {
	return types.BoxI32(int32(int8(v)))
}

func (i *Interpreter) i32Extend16S(v int32) types.Boxed {
	return types.BoxI32(int32(int16(v)))
}

func (i *Interpreter) i32Eqz(v int32) types.Boxed {
	return types.BoxI1(v == 0)
}

func (i *Interpreter) i32Eq(lhs, rhs int32) types.Boxed {
	return types.BoxI1(lhs == rhs)
}

func (i *Interpreter) i32Ne(lhs, rhs int32) types.Boxed {
	return types.BoxI1(lhs != rhs)
}

func (i *Interpreter) i32LtS(lhs, rhs int32) types.Boxed {
	return types.BoxI1(lhs < rhs)
}

func (i *Interpreter) i32LtU(lhs, rhs int32) types.Boxed {
	return types.BoxI1(uint32(lhs) < uint32(rhs))
}

func (i *Interpreter) i32GtS(lhs, rhs int32) types.Boxed {
	return types.BoxI1(lhs > rhs)
}

func (i *Interpreter) i32GtU(lhs, rhs int32) types.Boxed {
	return types.BoxI1(uint32(lhs) > uint32(rhs))
}

func (i *Interpreter) i32LeS(lhs, rhs int32) types.Boxed {
	return types.BoxI1(lhs <= rhs)
}

func (i *Interpreter) i32LeU(lhs, rhs int32) types.Boxed {
	return types.BoxI1(uint32(lhs) <= uint32(rhs))
}

func (i *Interpreter) i32GeS(lhs, rhs int32) types.Boxed {
	return types.BoxI1(lhs >= rhs)
}

func (i *Interpreter) i32GeU(lhs, rhs int32) types.Boxed {
	return types.BoxI1(uint32(lhs) >= uint32(rhs))
}

func (i *Interpreter) i32ToI64S(v int32) types.Boxed {
	return i.boxI64(int64(v))
}

func (i *Interpreter) i32ToI64U(v int32) types.Boxed {
	return i.boxI64(int64(uint32(v)))
}

func (i *Interpreter) i32ToF32S(v int32) types.Boxed {
	return types.BoxF32(float32(v))
}

func (i *Interpreter) i32ToF32U(v int32) types.Boxed {
	return types.BoxF32(float32(uint32(v)))
}

func (i *Interpreter) i32ToF64S(v int32) types.Boxed {
	return types.BoxF64(float64(v))
}

func (i *Interpreter) i32ToF64U(v int32) types.Boxed {
	return types.BoxF64(float64(uint32(v)))
}

func (i *Interpreter) i32ReinterpretF32(v float32) types.Boxed {
	return types.BoxI32(int32(math.Float32bits(v)))
}

func (i *Interpreter) i64Add(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs + rhs)
}

func (i *Interpreter) i64Sub(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs - rhs)
}

func (i *Interpreter) i64Mul(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs * rhs)
}

func (i *Interpreter) i64DivS(lhs, rhs int64) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return i.boxI64(lhs / rhs)
}

func (i *Interpreter) i64DivU(lhs, rhs int64) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return i.boxI64(int64(uint64(lhs) / uint64(rhs)))
}

func (i *Interpreter) i64RemS(lhs, rhs int64) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return i.boxI64(lhs % rhs)
}

func (i *Interpreter) i64RemU(lhs, rhs int64) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return i.boxI64(int64(uint64(lhs) % uint64(rhs)))
}

func (i *Interpreter) i64Shl(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs << (rhs & 0x3F))
}

func (i *Interpreter) i64ShrS(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs >> (rhs & 0x3F))
}

func (i *Interpreter) i64ShrU(lhs, rhs int64) types.Boxed {
	return i.boxI64(int64(uint64(lhs) >> (rhs & 0x3F)))
}

func (i *Interpreter) i64Xor(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs ^ rhs)
}

func (i *Interpreter) i64And(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs & rhs)
}

func (i *Interpreter) i64Or(lhs, rhs int64) types.Boxed {
	return i.boxI64(lhs | rhs)
}

func (i *Interpreter) i64Clz(v int64) types.Boxed {
	return i.boxI64(int64(bits.LeadingZeros64(uint64(v))))
}

func (i *Interpreter) i64Ctz(v int64) types.Boxed {
	return i.boxI64(int64(bits.TrailingZeros64(uint64(v))))
}

func (i *Interpreter) i64Popcnt(v int64) types.Boxed {
	return i.boxI64(int64(bits.OnesCount64(uint64(v))))
}

func (i *Interpreter) i64Rotl(lhs, rhs int64) types.Boxed {
	return i.boxI64(int64(bits.RotateLeft64(uint64(lhs), int(rhs))))
}

func (i *Interpreter) i64Rotr(lhs, rhs int64) types.Boxed {
	return i.boxI64(int64(bits.RotateLeft64(uint64(lhs), -int(rhs))))
}

func (i *Interpreter) i64Extend8S(v int64) types.Boxed {
	return i.boxI64(int64(int8(v)))
}

func (i *Interpreter) i64Extend16S(v int64) types.Boxed {
	return i.boxI64(int64(int16(v)))
}

func (i *Interpreter) i64Extend32S(v int64) types.Boxed {
	return i.boxI64(int64(int32(v)))
}

func (i *Interpreter) i64Eqz(v int64) types.Boxed {
	return types.BoxI1(v == 0)
}

func (i *Interpreter) i64Eq(lhs, rhs int64) types.Boxed {
	return types.BoxI1(lhs == rhs)
}

func (i *Interpreter) i64Ne(lhs, rhs int64) types.Boxed {
	return types.BoxI1(lhs != rhs)
}

func (i *Interpreter) i64LtS(lhs, rhs int64) types.Boxed {
	return types.BoxI1(lhs < rhs)
}

func (i *Interpreter) i64LtU(lhs, rhs int64) types.Boxed {
	return types.BoxI1(uint64(lhs) < uint64(rhs))
}

func (i *Interpreter) i64GtS(lhs, rhs int64) types.Boxed {
	return types.BoxI1(lhs > rhs)
}

func (i *Interpreter) i64GtU(lhs, rhs int64) types.Boxed {
	return types.BoxI1(uint64(lhs) > uint64(rhs))
}

func (i *Interpreter) i64LeS(lhs, rhs int64) types.Boxed {
	return types.BoxI1(lhs <= rhs)
}

func (i *Interpreter) i64LeU(lhs, rhs int64) types.Boxed {
	return types.BoxI1(uint64(lhs) <= uint64(rhs))
}

func (i *Interpreter) i64GeS(lhs, rhs int64) types.Boxed {
	return types.BoxI1(lhs >= rhs)
}

func (i *Interpreter) i64GeU(lhs, rhs int64) types.Boxed {
	return types.BoxI1(uint64(lhs) >= uint64(rhs))
}

func (i *Interpreter) i64ToI32(v int64) types.Boxed {
	return types.BoxI32(int32(v))
}

func (i *Interpreter) i64ToF32S(v int64) types.Boxed {
	return types.BoxF32(float32(v))
}

func (i *Interpreter) i64ToF32U(v int64) types.Boxed {
	return types.BoxF32(float32(uint64(v)))
}

func (i *Interpreter) i64ToF64S(v int64) types.Boxed {
	return types.BoxF64(float64(v))
}

func (i *Interpreter) i64ToF64U(v int64) types.Boxed {
	return types.BoxF64(float64(uint64(v)))
}

func (i *Interpreter) i64ReinterpretF64(v float64) types.Boxed {
	return i.boxI64(int64(math.Float64bits(v)))
}

func (i *Interpreter) f32Add(lhs, rhs float32) types.Boxed {
	return types.BoxF32(lhs + rhs)
}

func (i *Interpreter) f32Sub(lhs, rhs float32) types.Boxed {
	return types.BoxF32(lhs - rhs)
}

func (i *Interpreter) f32Mul(lhs, rhs float32) types.Boxed {
	return types.BoxF32(lhs * rhs)
}

func (i *Interpreter) f32Div(lhs, rhs float32) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxF32(lhs / rhs)
}

func (i *Interpreter) f32Rem(lhs, rhs float32) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxF32(float32(math.Mod(float64(lhs), float64(rhs))))
}

func (i *Interpreter) f32Mod(lhs, rhs float32) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	m := math.Mod(float64(lhs), float64(rhs))
	if m != 0 && (m < 0) != (rhs < 0) {
		m += float64(rhs)
	}
	return types.BoxF32(float32(m))
}

func (i *Interpreter) f32Abs(v float32) types.Boxed {
	return types.BoxF32(float32(math.Abs(float64(v))))
}

func (i *Interpreter) f32Neg(v float32) types.Boxed {
	return types.BoxF32(-v)
}

func (i *Interpreter) f32Sqrt(v float32) types.Boxed {
	return types.BoxF32(float32(math.Sqrt(float64(v))))
}

func (i *Interpreter) f32Ceil(v float32) types.Boxed {
	return types.BoxF32(float32(math.Ceil(float64(v))))
}

func (i *Interpreter) f32Floor(v float32) types.Boxed {
	return types.BoxF32(float32(math.Floor(float64(v))))
}

func (i *Interpreter) f32Trunc(v float32) types.Boxed {
	return types.BoxF32(float32(math.Trunc(float64(v))))
}

func (i *Interpreter) f32Nearest(v float32) types.Boxed {
	return types.BoxF32(float32(math.RoundToEven(float64(v))))
}

func (i *Interpreter) f32Min(lhs, rhs float32) types.Boxed {
	return types.BoxF32(min(lhs, rhs))
}

func (i *Interpreter) f32Max(lhs, rhs float32) types.Boxed {
	return types.BoxF32(max(lhs, rhs))
}

func (i *Interpreter) f32Copysign(lhs, rhs float32) types.Boxed {
	return types.BoxF32(float32(math.Copysign(float64(lhs), float64(rhs))))
}

func (i *Interpreter) f32Eq(lhs, rhs float32) types.Boxed {
	return types.BoxI1(lhs == rhs)
}

func (i *Interpreter) f32Ne(lhs, rhs float32) types.Boxed {
	return types.BoxI1(lhs != rhs)
}

func (i *Interpreter) f32Lt(lhs, rhs float32) types.Boxed {
	return types.BoxI1(lhs < rhs)
}

func (i *Interpreter) f32Gt(lhs, rhs float32) types.Boxed {
	return types.BoxI1(lhs > rhs)
}

func (i *Interpreter) f32Le(lhs, rhs float32) types.Boxed {
	return types.BoxI1(lhs <= rhs)
}

func (i *Interpreter) f32Ge(lhs, rhs float32) types.Boxed {
	return types.BoxI1(lhs >= rhs)
}

func (i *Interpreter) f32ToI32S(v float32) types.Boxed {
	return types.BoxI32(i.satI32(float64(v)))
}

func (i *Interpreter) f32ToI32U(v float32) types.Boxed {
	return types.BoxI32(int32(i.satU32(float64(v))))
}

func (i *Interpreter) f32ToF64(v float32) types.Boxed {
	return types.BoxF64(float64(v))
}

func (i *Interpreter) f32ReinterpretI32(v int32) types.Boxed {
	return types.BoxF32(math.Float32frombits(uint32(v)))
}

func (i *Interpreter) f64Add(lhs, rhs float64) types.Boxed {
	return types.BoxF64(lhs + rhs)
}

func (i *Interpreter) f64Sub(lhs, rhs float64) types.Boxed {
	return types.BoxF64(lhs - rhs)
}

func (i *Interpreter) f64Mul(lhs, rhs float64) types.Boxed {
	return types.BoxF64(lhs * rhs)
}

func (i *Interpreter) f64Div(lhs, rhs float64) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxF64(lhs / rhs)
}

func (i *Interpreter) f64Rem(lhs, rhs float64) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	return types.BoxF64(math.Mod(lhs, rhs))
}

func (i *Interpreter) f64Mod(lhs, rhs float64) types.Boxed {
	if rhs == 0 {
		panic(ErrDivideByZero)
	}
	m := math.Mod(lhs, rhs)
	if m != 0 && (m < 0) != (rhs < 0) {
		m += rhs
	}
	return types.BoxF64(m)
}

func (i *Interpreter) f64Abs(v float64) types.Boxed {
	return types.BoxF64(math.Abs(v))
}

func (i *Interpreter) f64Neg(v float64) types.Boxed {
	return types.BoxF64(-v)
}

func (i *Interpreter) f64Sqrt(v float64) types.Boxed {
	return types.BoxF64(math.Sqrt(v))
}

func (i *Interpreter) f64Ceil(v float64) types.Boxed {
	return types.BoxF64(math.Ceil(v))
}

func (i *Interpreter) f64Floor(v float64) types.Boxed {
	return types.BoxF64(math.Floor(v))
}

func (i *Interpreter) f64Trunc(v float64) types.Boxed {
	return types.BoxF64(math.Trunc(v))
}

func (i *Interpreter) f64Nearest(v float64) types.Boxed {
	return types.BoxF64(math.RoundToEven(v))
}

func (i *Interpreter) f64Min(lhs, rhs float64) types.Boxed {
	return types.BoxF64(math.Min(lhs, rhs))
}

func (i *Interpreter) f64Max(lhs, rhs float64) types.Boxed {
	return types.BoxF64(math.Max(lhs, rhs))
}

func (i *Interpreter) f64Copysign(lhs, rhs float64) types.Boxed {
	return types.BoxF64(math.Copysign(lhs, rhs))
}

func (i *Interpreter) f64Eq(lhs, rhs float64) types.Boxed {
	return types.BoxI1(lhs == rhs)
}

func (i *Interpreter) f64Ne(lhs, rhs float64) types.Boxed {
	return types.BoxI1(lhs != rhs)
}

func (i *Interpreter) f64Lt(lhs, rhs float64) types.Boxed {
	return types.BoxI1(lhs < rhs)
}

func (i *Interpreter) f64Gt(lhs, rhs float64) types.Boxed {
	return types.BoxI1(lhs > rhs)
}

func (i *Interpreter) f64Le(lhs, rhs float64) types.Boxed {
	return types.BoxI1(lhs <= rhs)
}

func (i *Interpreter) f64Ge(lhs, rhs float64) types.Boxed {
	return types.BoxI1(lhs >= rhs)
}

func (i *Interpreter) f64ToI32S(v float64) types.Boxed {
	return types.BoxI32(i.satI32(v))
}

func (i *Interpreter) f64ToI32U(v float64) types.Boxed {
	return types.BoxI32(int32(i.satU32(v)))
}

func (i *Interpreter) f64ToF32(v float64) types.Boxed {
	return types.BoxF32(float32(v))
}

func (i *Interpreter) f64ReinterpretI64(v int64) types.Boxed {
	return types.BoxF64(math.Float64frombits(uint64(v)))
}

// satI32 truncates v toward zero into a signed i32, saturating out-of-range
// inputs to the i32 bounds and mapping NaN to 0 (WebAssembly trunc_sat_s).
func (*Interpreter) satI32(v float64) int32 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= 2147483648.0:
		return math.MaxInt32
	case v < -2147483648.0:
		return math.MinInt32
	default:
		return int32(v)
	}
}

// satU32 truncates v toward zero into an unsigned i32, saturating out-of-range
// inputs to the u32 bounds and mapping NaN to 0 (WebAssembly trunc_sat_u).
func (*Interpreter) satU32(v float64) uint32 {
	switch {
	case math.IsNaN(v), v < 0:
		return 0
	case v >= 4294967296.0:
		return math.MaxUint32
	default:
		return uint32(v)
	}
}

// satI64 truncates v toward zero into a signed i64, saturating out-of-range
// inputs to the i64 bounds and mapping NaN to 0 (WebAssembly trunc_sat_s).
func (*Interpreter) satI64(v float64) int64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= 9223372036854775808.0:
		return math.MaxInt64
	case v < -9223372036854775808.0:
		return math.MinInt64
	default:
		return int64(v)
	}
}

// satU64 truncates v toward zero into an unsigned i64, saturating out-of-range
// inputs to the u64 bounds and mapping NaN to 0 (WebAssembly trunc_sat_u).
func (*Interpreter) satU64(v float64) uint64 {
	switch {
	case math.IsNaN(v), v < 0:
		return 0
	case v >= 18446744073709551616.0:
		return math.MaxUint64
	default:
		return uint64(v)
	}
}

// containsYield reports whether code contains a YIELD opcode, marking its
// function as a coroutine-function: one whose CALL produces a Coroutine handle
// and whose traces abort rather than compile across the suspension point.
func containsYield(code []byte) bool {
	for ip := 0; ip < len(code); {
		if instr.Opcode(code[ip]) == instr.YIELD {
			return true
		}
		w := instr.Instruction(code[ip:]).Width()
		if w <= 0 {
			break
		}
		ip += w
	}
	return false
}
