package interp

import (
	"math"
	"math/bits"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type threadedCompiler struct {
	types     []types.Type
	constants []types.Boxed
	heap      []types.Value
	locals    []types.Kind
	code      []byte
	ip        int
	precise   bool
}

var threaded = [256]func(c *threadedCompiler) func(i *Interpreter){
	instr.NOP: func(c *threadedCompiler) func(i *Interpreter) {
		skip := 0
		for !c.precise && c.ip+skip < len(c.code) && instr.Opcode(c.code[c.ip+skip]) == instr.NOP {
			skip++
		}
		if c.precise {
			skip = 1
		}
		c.ip++
		return func(i *Interpreter) {
			i.fr.ip += skip
		}
	},
	instr.UNREACHABLE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			i.fr.ip++
			panic(ErrUnreachableExecuted)
		}
	},
	instr.DROP: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.DUP: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.SWAP: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			i.stack[i.sp-1], i.stack[i.sp-2] = i.stack[i.sp-2], i.stack[i.sp-1]
			i.fr.ip++
		}
	},
	instr.BR: func(c *threadedCompiler) func(i *Interpreter) {
		offset := instr.ParseI16(c.code, c.ip+1)
		c.ip += 3
		return func(i *Interpreter) {
			f := i.fr
			f.ip += offset + 3
		}
	},
	instr.BR_IF: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.BR_TABLE: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.SELECT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 3 {
				panic(ErrStackUnderflow)
			}
			cond := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2]
			v1 := i.stack[i.sp-3]
			var result types.Boxed
			if cond == 0 {
				result = v2
			} else {
				result = v1
			}
			i.releaseBox(result)
			i.stack[i.sp-3] = result
			i.sp -= 2
			i.fr.ip++
		}
	},
	instr.CALL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			if i.fp == len(i.frames) {
				panic(ErrFrameOverflow)
			}
			addr := i.stack[i.sp-1].Ref()
			switch fn := i.heap[addr].(type) {
			case *types.Function:
				params := len(fn.Typ.Params)
				returns := len(fn.Typ.Returns)
				locals := len(fn.Locals)
				if i.sp <= params {
					panic(ErrStackUnderflow)
				}
				if i.sp+locals-1 >= len(i.stack) {
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
				i.sp = f.bp + params + locals
				i.fr.ip++
				i.fp++
				i.fr = f
			case *types.Closure:
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
				if i.sp+locals-1 >= len(i.stack) {
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
	instr.RETURN: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.fp == 1 {
				panic(ErrFrameUnderflow)
			}
			i.ret()
		}
	},
	instr.RETURN_CALL: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.GLOBAL_GET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			if idx < 0 || idx >= len(i.globals) {
				panic(ErrSegmentationFault)
			}
			val := i.globals[idx]
			i.retainBox(val)
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 3
		}
	},
	instr.GLOBAL_SET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			if idx < 0 {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			if idx >= len(i.globals) {
				if cap(i.globals) > idx {
					i.globals = i.globals[:idx+1]
				} else {
					global := make([]types.Boxed, idx*2)
					copy(global, i.globals)
					i.globals = global[:idx+1]
				}
			}
			old := i.globals[idx]
			if old != val {
				i.releaseBox(old)
			}
			i.globals[idx] = val
			i.sp--
			i.fr.ip += 3
		}
	},
	instr.GLOBAL_TEE: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			if idx < 0 {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			if idx >= len(i.globals) {
				if cap(i.globals) > idx {
					i.globals = i.globals[:idx+1]
				} else {
					global := make([]types.Boxed, idx*2)
					copy(global, i.globals)
					i.globals = global[:idx+1]
				}
			}
			old := i.globals[idx]
			if old != val {
				i.releaseBox(old)
			}
			i.globals[idx] = val
			i.fr.ip += 3
		}
	},
	instr.LOCAL_GET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		if idx < 0 {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		switch c.locals[idx] {
		case types.KindI32:
			if fused := c.fuseI32(func(i *Interpreter) int32 {
				addr := i.fr.bp + idx
				if addr > i.sp {
					panic(ErrSegmentationFault)
				}
				return i.stack[addr].I32()
			}, 2); fused != nil {
				return fused
			}
		case types.KindI64:
			if fused := c.fuseI64(func(i *Interpreter) int64 {
				addr := i.fr.bp + idx
				if addr > i.sp {
					panic(ErrSegmentationFault)
				}
				return i.unboxI64(i.stack[addr])
			}, 2); fused != nil {
				return fused
			}
		case types.KindF32:
			if fused := c.fuseF32(func(i *Interpreter) float32 {
				addr := i.fr.bp + idx
				if addr > i.sp {
					panic(ErrSegmentationFault)
				}
				return i.stack[addr].F32()
			}, 2); fused != nil {
				return fused
			}
		case types.KindF64:
			if fused := c.fuseF64(func(i *Interpreter) float64 {
				addr := i.fr.bp + idx
				if addr > i.sp {
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
		// I32/F32/F64 locals never hold a heap ref, so retain is a no-op; skip
		// it and the Kind branch. I64 may box to a ref, so it keeps retainBox.
		switch c.locals[idx] {
		case types.KindI32, types.KindF32, types.KindF64:
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
			val := i.stack[addr]
			i.retainBox(val)
			i.stack[i.sp] = val
			i.sp++
			i.fr.ip += 2
		}
	},
	instr.LOCAL_SET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		if idx < 0 {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		// I32/F32/F64 locals never hold a heap ref, so the old value can never
		// be a ref; skip the release. I64 may box to a ref, so it keeps it.
		if idx < len(c.locals) {
			switch c.locals[idx] {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == 0 {
						panic(ErrStackUnderflow)
					}
					addr := i.fr.bp + idx
					if addr > i.sp {
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
			if addr > i.sp {
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
	instr.LOCAL_TEE: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		if idx < 0 {
			return func(i *Interpreter) {
				panic(ErrSegmentationFault)
			}
		}
		// I32/F32/F64 locals never hold a heap ref, so the old value can never
		// be a ref; skip the release. I64 may box to a ref, so it keeps it.
		if idx < len(c.locals) {
			switch c.locals[idx] {
			case types.KindI32, types.KindF32, types.KindF64:
				return func(i *Interpreter) {
					if i.sp == 0 {
						panic(ErrStackUnderflow)
					}
					addr := i.fr.bp + idx
					if addr > i.sp {
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
			if addr > i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.stack[addr]
			if old != val {
				i.releaseBox(old)
			}
			i.stack[addr] = val
			i.fr.ip += 2
		}
	},
	instr.CONST_GET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.constants) {
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
	instr.UPVAL_GET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
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
	instr.UPVAL_SET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
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
	instr.REF_NEW: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.REF_GET: func(c *threadedCompiler) func(i *Interpreter) {
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
			switch i.heap[addr].(type) {
			case types.I32, types.I64, types.F32, types.F64:
			default:
				panic(ErrTypeMismatch)
			}
			i.stack[i.sp-1] = i.box(i.heap[addr])
			i.release(addr)
			i.fr.ip++
		}
	},
	instr.REF_SET: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.REF_NULL: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.REF_TEST: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
				cond = types.BoxBool(typ.Equals(ref.Type()))
			default:
				cond = types.BoxBool(typ.Kind() == kind)
			}
			i.stack[i.sp-1] = cond
			i.fr.ip += 3
		}
	},
	instr.REF_CAST: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
	instr.REF_IS_NULL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1]
			i.stack[i.sp-1] = types.BoxBool(val.Ref() == 0)
			i.fr.ip++
		}
	},
	instr.REF_EQ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1]
			v2 := i.stack[i.sp-2]
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 == v1)
			i.fr.ip++
		}
	},
	instr.REF_NE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1]
			v2 := i.stack[i.sp-2]
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 != v1)
			i.fr.ip++
		}
	},
	instr.I32_CONST: func(c *threadedCompiler) func(i *Interpreter) {
		raw := *(*int32)(unsafe.Pointer(&c.code[c.ip+1]))
		val := types.BoxI32(raw)
		c.ip += 5
		if fused := c.fuseI32Imm(raw, 5); fused != nil {
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
	instr.I32_ADD: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v2 + v1)
			i.fr.ip++
		}
	},
	instr.I32_SUB: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v2 - v1)
			i.fr.ip++
		}
	},
	instr.I32_MUL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v2 * v1)
			i.fr.ip++
		}
	},
	instr.I32_DIV_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v2 / v1)
			i.fr.ip++
		}
	},
	instr.I32_DIV_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(int32(uint32(v2) / uint32(v1)))
			i.fr.ip++
		}
	},
	instr.I32_REM_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v2 % v1)
			i.fr.ip++
		}
	},
	instr.I32_REM_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(int32(uint32(v2) % uint32(v1)))
			i.fr.ip++
		}
	},
	instr.I32_SHL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32() & 0x1F
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v2 << v1)
			i.fr.ip++
		}
	},
	instr.I32_SHR_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32() & 0x1F
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v2 >> v1)
			i.fr.ip++
		}
	},
	instr.I32_SHR_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32() & 0x1F
			v2 := uint32(i.stack[i.sp-2].I32())
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(int32(v2 >> v1))
			i.fr.ip++
		}
	},
	instr.I32_XOR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v1 ^ v2)
			i.fr.ip++
		}
	},
	instr.I32_AND: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v1 & v2)
			i.fr.ip++
		}
	},
	instr.I32_OR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(v1 | v2)
			i.fr.ip++
		}
	},
	instr.I32_CLZ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = types.BoxI32(int32(bits.LeadingZeros32(v)))
			i.fr.ip++
		}
	},
	instr.I32_CTZ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = types.BoxI32(int32(bits.TrailingZeros32(v)))
			i.fr.ip++
		}
	},
	instr.I32_POPCNT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = types.BoxI32(int32(bits.OnesCount32(v)))
			i.fr.ip++
		}
	},
	instr.I32_ROTL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			amount := int(i.stack[i.sp-1].I32())
			v := uint32(i.stack[i.sp-2].I32())
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(int32(bits.RotateLeft32(v, amount)))
			i.fr.ip++
		}
	},
	instr.I32_ROTR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			amount := int(i.stack[i.sp-1].I32())
			v := uint32(i.stack[i.sp-2].I32())
			i.sp--
			i.stack[i.sp-1] = types.BoxI32(int32(bits.RotateLeft32(v, -amount)))
			i.fr.ip++
		}
	},
	instr.I32_EXTEND8_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = types.BoxI32(int32(int8(v)))
			i.fr.ip++
		}
	},
	instr.I32_EXTEND16_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = types.BoxI32(int32(int16(v)))
			i.fr.ip++
		}
	},
	instr.I32_EQZ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = types.BoxBool(val == 0)
			i.fr.ip++
		}
	},
	instr.I32_EQ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 == v1)
			i.fr.ip++
		}
	},
	instr.I32_NE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 != v1)
			i.fr.ip++
		}
	},
	instr.I32_LT_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 < v1)
			i.fr.ip++
		}
	},
	instr.I32_LT_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint32(v2) < uint32(v1))
			i.fr.ip++
		}
	},
	instr.I32_GT_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 > v1)
			i.fr.ip++
		}
	},
	instr.I32_GT_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint32(v2) > uint32(v1))
			i.fr.ip++
		}
	},
	instr.I32_LE_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
			i.fr.ip++
		}
	},
	instr.I32_LE_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint32(v2) <= uint32(v1))
			i.fr.ip++
		}
	},
	instr.I32_GE_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
			i.fr.ip++
		}
	},
	instr.I32_GE_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].I32()
			v2 := i.stack[i.sp-2].I32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint32(v2) >= uint32(v1))
			i.fr.ip++
		}
	},
	instr.I32_TO_I64_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = i.boxI64(int64(v))
			i.fr.ip++
		}
	},
	instr.I32_TO_I64_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = i.boxI64(int64(v))
			i.fr.ip++
		}
	},
	instr.I32_TO_F32_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = types.BoxF32(float32(v))
			i.fr.ip++
		}
	},
	instr.I32_TO_F32_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = types.BoxF32(float32(v))
			i.fr.ip++
		}
	},
	instr.I32_TO_F64_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint32(i.stack[i.sp-1].I32())
			i.stack[i.sp-1] = types.BoxF64(float64(v))
			i.fr.ip++
		}
	},
	instr.I32_TO_F64_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = types.BoxF64(float64(v))
			i.fr.ip++
		}
	},
	instr.I32_REINTERPRET_F32: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxI32(int32(math.Float32bits(v)))
			i.fr.ip++
		}
	},
	instr.I64_CONST: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.I64_ADD: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v2 + v1)
			i.fr.ip++
		}
	},
	instr.I64_SUB: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v2 - v1)
			i.fr.ip++
		}
	},
	instr.I64_MUL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v2 * v1)
			i.fr.ip++
		}
	},
	instr.I64_DIV_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v2 / v1)
			i.fr.ip++
		}
	},
	instr.I64_DIV_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) / uint64(v1)))
			i.fr.ip++
		}
	},
	instr.I64_REM_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v2 % v1)
			i.fr.ip++
		}
	},
	instr.I64_REM_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) % uint64(v1)))
			i.fr.ip++
		}
	},
	instr.I64_SHL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(int64(v2 << (v1 & 0x3F)))
			i.fr.ip++
		}
	},
	instr.I64_SHR_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v2 >> (v1 & 0x3F))
			i.fr.ip++
		}
	},
	instr.I64_SHR_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(int64(uint64(v2) >> (v1 & 0x3F)))
			i.fr.ip++
		}
	},
	instr.I64_XOR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v1 ^ v2)
			i.fr.ip++
		}
	},
	instr.I64_AND: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v1 & v2)
			i.fr.ip++
		}
	},
	instr.I64_OR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = i.boxI64(v1 | v2)
			i.fr.ip++
		}
	},
	instr.I64_CLZ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint64(i.unboxI64(i.stack[i.sp-1]))
			i.stack[i.sp-1] = i.boxI64(int64(bits.LeadingZeros64(v)))
			i.fr.ip++
		}
	},
	instr.I64_CTZ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint64(i.unboxI64(i.stack[i.sp-1]))
			i.stack[i.sp-1] = i.boxI64(int64(bits.TrailingZeros64(v)))
			i.fr.ip++
		}
	},
	instr.I64_POPCNT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := uint64(i.unboxI64(i.stack[i.sp-1]))
			i.stack[i.sp-1] = i.boxI64(int64(bits.OnesCount64(v)))
			i.fr.ip++
		}
	},
	instr.I64_ROTL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			amount := int(i.unboxI64(i.stack[i.sp-1]))
			v := uint64(i.unboxI64(i.stack[i.sp-2]))
			i.sp--
			i.stack[i.sp-1] = i.boxI64(int64(bits.RotateLeft64(v, amount)))
			i.fr.ip++
		}
	},
	instr.I64_ROTR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			amount := int(i.unboxI64(i.stack[i.sp-1]))
			v := uint64(i.unboxI64(i.stack[i.sp-2]))
			i.sp--
			i.stack[i.sp-1] = i.boxI64(int64(bits.RotateLeft64(v, -amount)))
			i.fr.ip++
		}
	},
	instr.I64_EXTEND8_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.boxI64(int64(int8(v)))
			i.fr.ip++
		}
	},
	instr.I64_EXTEND16_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.boxI64(int64(int16(v)))
			i.fr.ip++
		}
	},
	instr.I64_EXTEND32_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = i.boxI64(int64(int32(v)))
			i.fr.ip++
		}
	},
	instr.I64_EQZ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxBool(val == 0)
			i.fr.ip++
		}
	},
	instr.I64_EQ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 == v1)
			i.fr.ip++
		}
	},
	instr.I64_NE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 != v1)
			i.fr.ip++
		}
	},
	instr.I64_LT_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 < v1)
			i.fr.ip++
		}
	},
	instr.I64_LT_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint64(v2) < uint64(v1))
			i.fr.ip++
		}
	},
	instr.I64_GT_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 > v1)
			i.fr.ip++
		}
	},
	instr.I64_GT_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint64(v2) > uint64(v1))
			i.fr.ip++
		}
	},
	instr.I64_LE_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
			i.fr.ip++
		}
	},
	instr.I64_LE_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint64(v2) <= uint64(v1))
			i.fr.ip++
		}
	},
	instr.I64_GE_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
			i.fr.ip++
		}
	},
	instr.I64_GE_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.unboxI64(i.stack[i.sp-1])
			v2 := i.unboxI64(i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(uint64(v2) >= uint64(v1))
			i.fr.ip++
		}
	},
	instr.I64_TO_I32: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxI32(int32(v))
			i.fr.ip++
		}
	},
	instr.I64_TO_F32_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxF32(float32(v))
			i.fr.ip++
		}
	},
	instr.I64_TO_F32_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxF32(float32(uint64(v)))
			i.fr.ip++
		}
	},
	instr.I64_TO_F64_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxF64(float64(v))
			i.fr.ip++
		}
	},
	instr.I64_TO_F64_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxF64(float64(uint64(v)))
			i.fr.ip++
		}
	},
	instr.I64_REINTERPRET_F64: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = i.boxI64(int64(math.Float64bits(v)))
			i.fr.ip++
		}
	},
	instr.F32_CONST: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.F32_ADD: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxF32(v2 + v1)
			i.fr.ip++
		}
	},
	instr.F32_SUB: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxF32(v2 - v1)
			i.fr.ip++
		}
	},
	instr.F32_MUL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxF32(v1 * v2)
			i.fr.ip++
		}
	},
	instr.F32_DIV: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = types.BoxF32(v2 / v1)
			i.fr.ip++
		}
	},
	instr.F32_ABS: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF32(float32(math.Abs(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_NEG: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF32(-v)
			i.fr.ip++
		}
	},
	instr.F32_SQRT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF32(float32(math.Sqrt(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_CEIL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF32(float32(math.Ceil(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_FLOOR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF32(float32(math.Floor(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_TRUNC: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF32(float32(math.Trunc(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_NEAREST: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF32(float32(math.RoundToEven(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_MIN: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxF32(float32(math.Min(float64(v2), float64(v1))))
			i.fr.ip++
		}
	},
	instr.F32_MAX: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxF32(float32(math.Max(float64(v2), float64(v1))))
			i.fr.ip++
		}
	},
	instr.F32_COPYSIGN: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxF32(float32(math.Copysign(float64(v2), float64(v1))))
			i.fr.ip++
		}
	},
	instr.F32_EQ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 == v1)
			i.fr.ip++
		}
	},
	instr.F32_NE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 != v1)
			i.fr.ip++
		}
	},
	instr.F32_LT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 < v1)
			i.fr.ip++
		}
	},
	instr.F32_GT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 > v1)
			i.fr.ip++
		}
	},
	instr.F32_LE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
			i.fr.ip++
		}
	},
	instr.F32_GE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F32()
			v2 := i.stack[i.sp-2].F32()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
			i.fr.ip++
		}
	},
	instr.F32_TO_I32_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxI32(i.satI32(float64(v)))
			i.fr.ip++
		}
	},
	instr.F32_TO_I32_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxI32(int32(i.satU32(float64(v))))
			i.fr.ip++
		}
	},
	instr.F32_TO_I64_S: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.F32_TO_I64_U: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.F32_TO_F64: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F32()
			i.stack[i.sp-1] = types.BoxF64(float64(v))
			i.fr.ip++
		}
	},
	instr.F32_REINTERPRET_I32: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].I32()
			i.stack[i.sp-1] = types.BoxF32(math.Float32frombits(uint32(v)))
			i.fr.ip++
		}
	},
	instr.F64_CONST: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.F64_ADD: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxF64(v2 + v1)
			i.fr.ip++
		}
	},
	instr.F64_SUB: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxF64(v2 - v1)
			i.fr.ip++
		}
	},
	instr.F64_MUL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxF64(v1 * v2)
			i.fr.ip++
		}
	},
	instr.F64_DIV: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			if v1 == 0 {
				panic(ErrDivideByZero)
			}
			i.sp--
			i.stack[i.sp-1] = types.BoxF64(v2 / v1)
			i.fr.ip++
		}
	},
	instr.F64_ABS: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF64(math.Abs(v))
			i.fr.ip++
		}
	},
	instr.F64_NEG: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF64(-v)
			i.fr.ip++
		}
	},
	instr.F64_SQRT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF64(math.Sqrt(v))
			i.fr.ip++
		}
	},
	instr.F64_CEIL: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF64(math.Ceil(v))
			i.fr.ip++
		}
	},
	instr.F64_FLOOR: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF64(math.Floor(v))
			i.fr.ip++
		}
	},
	instr.F64_TRUNC: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF64(math.Trunc(v))
			i.fr.ip++
		}
	},
	instr.F64_NEAREST: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF64(math.RoundToEven(v))
			i.fr.ip++
		}
	},
	instr.F64_MIN: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxF64(math.Min(v2, v1))
			i.fr.ip++
		}
	},
	instr.F64_MAX: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxF64(math.Max(v2, v1))
			i.fr.ip++
		}
	},
	instr.F64_COPYSIGN: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxF64(math.Copysign(v2, v1))
			i.fr.ip++
		}
	},
	instr.F64_EQ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 == v1)
			i.fr.ip++
		}
	},
	instr.F64_NE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 != v1)
			i.fr.ip++
		}
	},
	instr.F64_LT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 < v1)
			i.fr.ip++
		}
	},
	instr.F64_GT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 > v1)
			i.fr.ip++
		}
	},
	instr.F64_LE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
			i.fr.ip++
		}
	},
	instr.F64_GE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := i.stack[i.sp-1].F64()
			v2 := i.stack[i.sp-2].F64()
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
			i.fr.ip++
		}
	},
	instr.F64_TO_I32_S: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxI32(i.satI32(v))
			i.fr.ip++
		}
	},
	instr.F64_TO_I32_U: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxI32(int32(i.satU32(v)))
			i.fr.ip++
		}
	},
	instr.F64_TO_I64_S: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.F64_TO_I64_U: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.F64_TO_F32: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.stack[i.sp-1].F64()
			i.stack[i.sp-1] = types.BoxF32(float32(v))
			i.fr.ip++
		}
	},
	instr.F64_REINTERPRET_I64: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v := i.unboxI64(i.stack[i.sp-1])
			i.stack[i.sp-1] = types.BoxF64(math.Float64frombits(uint64(v)))
			i.fr.ip++
		}
	},
	instr.STRING_NEW_UTF32: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.STRING_LEN: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.STRING_CONCAT: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.STRING_EQ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 == v1)
			i.fr.ip++
		}
	},
	instr.STRING_NE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 != v1)
			i.fr.ip++
		}
	},
	instr.STRING_LT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 < v1)
			i.fr.ip++
		}
	},
	instr.STRING_GT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 > v1)
			i.fr.ip++
		}
	},
	instr.STRING_LE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
			i.fr.ip++
		}
	},
	instr.STRING_GE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1 := unboxRef[types.String](i, i.stack[i.sp-1])
			v2 := unboxRef[types.String](i, i.stack[i.sp-2])
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
			i.fr.ip++
		}
	},
	instr.STRING_ENCODE_UTF32: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.ARRAY_NEW: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
		if typ.Elem == types.TypeI8 {
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
					val[j] = int8(i.stack[i.sp-size-j-1].I32())
				}
				i.sp -= size
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		}
		switch typ.ElemKind {
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
					val[j] = i.stack[i.sp-size-j-1].I32()
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
					val[j] = i.unboxI64(i.stack[i.sp-size-j-1])
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
					val[j] = i.stack[i.sp-size-j-1].F32()
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
					val[j] = i.stack[i.sp-size-j-1].F64()
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
	instr.ARRAY_NEW_DEFAULT: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
		if typ.Elem == types.TypeI8 {
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.TypedArray[int8], size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		}
		switch typ.ElemKind {
		case types.KindI32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
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
				val := &types.Array{
					Typ:   typ,
					Elems: make([]types.Boxed, size),
				}
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		}
	},
	instr.ARRAY_LEN: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			var n int32
			switch arr := i.unbox(i.stack[i.sp-1]).(type) {
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
	instr.ARRAY_GET: func(c *threadedCompiler) func(i *Interpreter) {
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
			case types.TypedArray[int8]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = types.BoxI32(int32(uint8(arr[idx])))
			case types.TypedArray[int32]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = types.BoxI32(int32(arr[idx]))
			case types.TypedArray[int64]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = i.boxI64(int64(arr[idx]))
			case types.TypedArray[float32]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = types.BoxF32(float32(arr[idx]))
			case types.TypedArray[float64]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = types.BoxF64(float64(arr[idx]))
			case *types.Array:
				if idx < 0 || idx >= len(arr.Elems) {
					panic(ErrIndexOutOfRange)
				}
				elem := arr.Elems[idx]
				i.retainBox(elem)
				val = elem
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp--
			i.stack[i.sp-1] = val
			i.fr.ip++
		}
	},
	instr.ARRAY_SET: func(c *threadedCompiler) func(i *Interpreter) {
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
			case types.TypedArray[int8]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = int8(val.I32())
			case types.TypedArray[int32]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = val.I32()
			case types.TypedArray[int64]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = i.unboxI64(val)
			case types.TypedArray[float32]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = val.F32()
			case types.TypedArray[float64]:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = val.F64()
			case *types.Array:
				if idx < 0 || idx >= len(arr.Elems) {
					panic(ErrIndexOutOfRange)
				}
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
	instr.ARRAY_FILL: func(c *threadedCompiler) func(i *Interpreter) {
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
			case types.TypedArray[int8]:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := int8(val.I32())
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[int32]:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := val.I32()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[int64]:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := i.unboxI64(val)
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[float32]:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := val.F32()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.TypedArray[float64]:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := val.F64()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case *types.Array:
				if idx < 0 || idx+size > len(arr.Elems) {
					panic(ErrIndexOutOfRange)
				}
				elem := arr.Elems[idx]
				for k := idx; k < idx+size; k++ {
					arr.Elems[k] = val
				}
				if val.Kind() == types.KindRef {
					i.retains(val.Ref(), size-1)
				}
				i.releaseBox(elem)
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp -= 4
			i.fr.ip++
		}
	},
	instr.ARRAY_COPY: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 4 {
				panic(ErrStackUnderflow)
			}
			size := int(i.stack[i.sp-1].I32())
			src := int(i.stack[i.sp-2].I32())
			dst := int(i.stack[i.sp-3].I32())
			ref := i.stack[i.sp-4]
			if ref.Kind() != types.KindRef {
				panic(ErrTypeMismatch)
			}
			addr := ref.Ref()
			switch arr := i.heap[addr].(type) {
			case types.TypedArray[int8]:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case types.TypedArray[int32]:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case types.TypedArray[int64]:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case types.TypedArray[float32]:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case types.TypedArray[float64]:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case *types.Array:
				if src < 0 || dst < 0 || src+size > len(arr.Elems) || dst+size > len(arr.Elems) {
					panic(ErrIndexOutOfRange)
				}
				elems := arr.Elems
				for _, v := range elems[src : src+size] {
					i.retainBox(v)
				}
				for _, v := range elems[dst : dst+size] {
					i.releaseBox(v)
				}
				copy(elems[dst:dst+size], elems[src:src+size])
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp -= 4
			i.fr.ip++
		}
	},
	instr.STRUCT_NEW: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
				val := i.stack[i.sp-size-j]
				switch f.Kind {
				case types.KindI32, types.KindF32, types.KindF64, types.KindRef:
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
	instr.STRUCT_NEW_DEFAULT: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
			s := types.NewStruct(typ)
			i.sp++
			i.stack[i.sp-1] = types.BoxRef(i.alloc(s))
			i.fr.ip += 3
		}
	},
	instr.STRUCT_GET: func(c *threadedCompiler) func(i *Interpreter) {
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
			switch s := i.heap[addr].(type) {
			case *types.Struct:
				if idx < 0 || idx >= len(s.Typ.Fields) {
					panic(ErrSegmentationFault)
				}
				field := s.Typ.Fields[idx]
				switch field.Kind {
				case types.KindI32:
					val = types.BoxI32(int32(uint32(s.Data[idx])))
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
				case types.KindI32, types.KindF32, types.KindF64:
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
			i.stack[i.sp-1] = val
			i.fr.ip++
		}
	},
	instr.STRUCT_SET: func(c *threadedCompiler) func(i *Interpreter) {
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
				case types.KindI32, types.KindF32, types.KindF64:
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
	instr.MAP_NEW: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
	instr.MAP_NEW_DEFAULT: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		if idx < 0 || idx >= len(c.types) {
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
	instr.MAP_LEN: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.MAP_GET: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.MAP_LOOKUP: func(c *threadedCompiler) func(i *Interpreter) {
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
			i.stack[i.sp-1] = types.BoxBool(found)
			i.fr.ip++
		}
	},
	instr.MAP_SET: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.MAP_DELETE: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.MAP_CLEAR: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.MAP_KEYS: func(c *threadedCompiler) func(i *Interpreter) {
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
	instr.CLOSURE_NEW: func(c *threadedCompiler) func(i *Interpreter) {
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
}

var unknown = func(_ *Interpreter) {
	panic(ErrUnknownOpcode)
}

func init() {
	for i, fn := range threaded {
		if fn == nil {
			threaded[i] = func(c *threadedCompiler) func(*Interpreter) {
				inst := instr.Instruction(c.code[c.ip:])
				c.ip += inst.Width()
				return unknown
			}
		}
	}
}

func (c *threadedCompiler) Compile(code []byte, locals []types.Kind) []func(*Interpreter) {
	c.code = code
	c.locals = locals
	c.ip = 0

	compiled := make([]func(*Interpreter), len(code))
	for c.ip < len(code) {
		ip := c.ip
		compiled[ip] = threaded[code[ip]](c)
	}
	for ip := 0; ip < len(code); ip++ {
		if compiled[ip] == nil {
			compiled[ip] = unknown
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
	if i.sp+returns-params-1 >= len(i.stack) {
		panic(ErrStackOverflow)
	}
	args := i.stack[i.sp-params-1 : i.sp-1]
	out, err := fn.Fn(i, args)
	if err != nil {
		panic(err)
	}
	for _, val := range args {
		if val.Kind() != types.KindRef {
			continue
		}
		kept := false
		for _, r := range out {
			if r == val {
				kept = true
				break
			}
		}
		if !kept {
			i.release(val.Ref())
		}
	}
	i.sp += returns - params - 1
	copy(i.stack[i.sp-returns:i.sp], out)
	i.fr.ip++
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
		if i.sp+locals-1 >= len(i.stack) {
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
	i.sp = base + params + locals
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
