package interp

import (
	"math"
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
			if val.Kind() == types.KindRef {
				i.release(val.Ref())
			}
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
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
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
			if result.Kind() == types.KindRef {
				i.release(result.Ref())
			}
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
				f.upvalues = nil
				f.addr = addr
				f.callee = addr
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
				f.upvalues = fn.Upvalues
				f.addr = fn.Fn
				f.callee = addr
				f.ip = 0
				f.bp = i.sp - params - 1
				f.returns = returns
				f.release = true
				i.sp = f.bp + params + locals
				i.fr.ip++
				i.fp++
				i.fr = f
			case *HostFunction:
				if i.sp <= len(fn.Typ.Params) {
					panic(ErrStackUnderflow)
				}
				if i.sp+len(fn.Typ.Returns)-len(fn.Typ.Params)-1 >= len(i.stack) {
					panic(ErrStackOverflow)
				}
				params := i.stack[i.sp-len(fn.Typ.Params)-1 : i.sp-1]
				returns, err := fn.Fn(i, params)
				if err != nil {
					panic(err)
				}
				for _, val := range params {
					if val.Kind() != types.KindRef {
						continue
					}
					ok := false
					for _, r := range returns {
						if r == val {
							ok = true
							break
						}
					}
					if !ok {
						i.release(val.Ref())
					}
				}
				i.sp += len(fn.Typ.Returns) - len(fn.Typ.Params) - 1
				copy(i.stack[i.sp-len(fn.Typ.Returns):i.sp], returns)
				i.fr.ip++
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
				i.release(f.callee)
			}
			f.code = nil
			i.fp--
			i.fr = &i.frames[i.fp-1]
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
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
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
			if old != val && old.Kind() == types.KindRef {
				i.release(old.Ref())
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
			if old != val && old.Kind() == types.KindRef {
				i.release(old.Ref())
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
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			addr := i.fr.bp + idx
			if addr > i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
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
			if old != val && old.Kind() == types.KindRef {
				i.release(old.Ref())
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
			if old != val && old.Kind() == types.KindRef {
				i.release(old.Ref())
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
			i.stack[i.sp-1] = types.BoxI32(int32(v))
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
			i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
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
			i.stack[i.sp-1] = i.boxI64(int64(v))
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
			i.stack[i.sp-1] = i.boxI64(int64(uint32(v)))
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
			i.stack[i.sp-1] = types.BoxI32(int32(v))
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
			i.stack[i.sp-1] = types.BoxI32(int32(uint32(v)))
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
			i.stack[i.sp-1] = i.boxI64(int64(v))
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
			i.stack[i.sp-1] = i.boxI64(int64(uint64(v)))
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
	instr.STRING_NEW_UTF32: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val := unboxRef[types.I32Array](i, i.stack[i.sp-1])
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
			i.stack[i.sp-1] = types.BoxRef(i.alloc(types.I32Array(val)))
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
				val := make(types.I32Array, size)
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
				val := make(types.I64Array, size)
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
				val := make(types.F32Array, size)
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
				val := make(types.F64Array, size)
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
		switch typ.ElemKind {
		case types.KindI32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.I32Array, size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindI64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.I64Array, size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindF32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.F32Array, size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.fr.ip += 3
			}
		case types.KindF64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.F64Array, size)
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
			case types.I32Array:
				n = int32(len(arr))
			case types.I64Array:
				n = int32(len(arr))
			case types.F32Array:
				n = int32(len(arr))
			case types.F64Array:
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
			case types.I32Array:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = types.BoxI32(int32(arr[idx]))
			case types.I64Array:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = i.boxI64(int64(arr[idx]))
			case types.F32Array:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = types.BoxF32(float32(arr[idx]))
			case types.F64Array:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				val = types.BoxF64(float64(arr[idx]))
			case *types.Array:
				if idx < 0 || idx >= len(arr.Elems) {
					panic(ErrIndexOutOfRange)
				}
				elem := arr.Elems[idx]
				if elem.Kind() == types.KindRef {
					i.retain(elem.Ref())
				}
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
			case types.I32Array:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = val.I32()
			case types.I64Array:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = i.unboxI64(val)
			case types.F32Array:
				if idx < 0 || idx >= len(arr) {
					panic(ErrIndexOutOfRange)
				}
				arr[idx] = val.F32()
			case types.F64Array:
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
				if elem.Kind() == types.KindRef {
					i.release(elem.Ref())
				}
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
			case types.I32Array:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := val.I32()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.I64Array:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := i.unboxI64(val)
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.F32Array:
				if idx < 0 || idx+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				v := val.F32()
				for k := idx; k < idx+size; k++ {
					arr[k] = v
				}
			case types.F64Array:
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
				if elem.Kind() == types.KindRef {
					i.release(elem.Ref())
				}
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
			case types.I32Array:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case types.I64Array:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case types.F32Array:
				if src < 0 || dst < 0 || src+size > len(arr) || dst+size > len(arr) {
					panic(ErrIndexOutOfRange)
				}
				copy(arr[dst:dst+size], arr[src:src+size])
			case types.F64Array:
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
					if v.Kind() == types.KindRef {
						i.retain(v.Ref())
					}
				}
				for _, v := range elems[dst : dst+size] {
					if v.Kind() == types.KindRef {
						i.release(v.Ref())
					}
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
					if val.Kind() == types.KindRef {
						i.retain(val.Ref())
					}
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
					if val.Kind() == types.KindRef {
						i.retain(val.Ref())
					}
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
					if old.Kind() == types.KindRef {
						i.release(old.Ref())
					}
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
					if old.Kind() == types.KindRef {
						i.release(old.Ref())
					}
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
				case *types.MapI32:
					old, ok := m.Set(key.I32(), value)
					if ok && old.Kind() == types.KindRef {
						i.release(old.Ref())
					}
				case *types.MapI64:
					old, ok := m.Set(i.unboxI64(key), value)
					if ok && old.Kind() == types.KindRef {
						i.release(old.Ref())
					}
				case *types.MapF32:
					old, ok := m.Set(key.F32(), value)
					if ok && old.Kind() == types.KindRef {
						i.release(old.Ref())
					}
				case *types.MapF64:
					old, ok := m.Set(key.F64(), value)
					if ok && old.Kind() == types.KindRef {
						i.release(old.Ref())
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
						if old.Value.Kind() == types.KindRef {
							i.release(old.Value.Ref())
						}
					}
				default:
					panic(ErrTypeMismatch)
				}
			}
			var addr int
			if typ.TraceKeys || typ.TraceValues {
				addr = i.allocRoot(m)
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
			case *types.MapI32:
				n = m.Len()
			case *types.MapI64:
				n = m.Len()
			case *types.MapF32:
				n = m.Len()
			case *types.MapF64:
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
			case *types.MapI32:
				value, ok := m.Get(key.I32())
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.MapI64:
				value, ok := m.Get(i.unboxI64(key))
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.MapF32:
				value, ok := m.Get(key.F32())
				if ok {
					result = value
				} else {
					result = m.Zero
				}
			case *types.MapF64:
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
			if result.Kind() == types.KindRef {
				i.retain(result.Ref())
			}
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
			case *types.MapI32:
				result, found = m.Get(key.I32())
				if !found {
					result = m.Zero
				}
			case *types.MapI64:
				result, found = m.Get(i.unboxI64(key))
				if !found {
					result = m.Zero
				}
			case *types.MapF32:
				result, found = m.Get(key.F32())
				if !found {
					result = m.Zero
				}
			case *types.MapF64:
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
			if result.Kind() == types.KindRef {
				i.retain(result.Ref())
			}
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
			case *types.MapI32:
				old, ok := m.Set(key.I32(), value)
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
				}
			case *types.MapI64:
				old, ok := m.Set(i.unboxI64(key), value)
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
				}
			case *types.MapF32:
				old, ok := m.Set(key.F32(), value)
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
				}
			case *types.MapF64:
				old, ok := m.Set(key.F64(), value)
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
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
					if old.Value.Kind() == types.KindRef {
						i.release(old.Value.Ref())
					}
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
			case *types.MapI32:
				old, ok := m.Delete(key.I32())
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
				}
			case *types.MapI64:
				old, ok := m.Delete(i.unboxI64(key))
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
				}
			case *types.MapF32:
				old, ok := m.Delete(key.F32())
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
				}
			case *types.MapF64:
				old, ok := m.Delete(key.F64())
				if ok && old.Kind() == types.KindRef {
					i.release(old.Ref())
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
					if old.Key.Kind() == types.KindRef {
						i.release(old.Key.Ref())
					}
					if old.Value.Kind() == types.KindRef {
						i.release(old.Value.Ref())
					}
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
			case *types.MapI32:
				m.Clear(func(value types.Boxed) {
					if value.Kind() == types.KindRef {
						i.release(value.Ref())
					}
				})
			case *types.MapI64:
				m.Clear(func(value types.Boxed) {
					if value.Kind() == types.KindRef {
						i.release(value.Ref())
					}
				})
			case *types.MapF32:
				m.Clear(func(value types.Boxed) {
					if value.Kind() == types.KindRef {
						i.release(value.Ref())
					}
				})
			case *types.MapF64:
				m.Clear(func(value types.Boxed) {
					if value.Kind() == types.KindRef {
						i.release(value.Ref())
					}
				})
			case *types.Map:
				m.Clear(func(entry types.MapEntry) {
					if entry.Key.Kind() == types.KindRef {
						i.release(entry.Key.Ref())
					}
					if entry.Value.Kind() == types.KindRef {
						i.release(entry.Value.Ref())
					}
				})
			default:
				panic(ErrTypeMismatch)
			}
			i.release(addr)
			i.sp--
			i.fr.ip++
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
			upvalues := make([]types.Boxed, n)
			copy(upvalues, i.stack[i.sp-1-n:i.sp-1])
			cl := types.NewClosure(fn.Typ, addr, upvalues)
			caddr := i.allocRoot(cl)
			i.sp -= n
			i.stack[i.sp-1] = types.BoxRef(caddr)
			i.fr.ip++
		}
	},
	instr.UPVAL_GET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(c.code[c.ip+1])
		c.ip += 2
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			if idx >= len(i.fr.upvalues) {
				panic(ErrSegmentationFault)
			}
			val := i.fr.upvalues[idx]
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
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
			if idx >= len(i.fr.upvalues) {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.fr.upvalues[idx]
			if old != val && old.Kind() == types.KindRef {
				i.release(old.Ref())
			}
			i.fr.upvalues[idx] = val
			i.sp--
			i.fr.ip += 2
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
