package interp

import (
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
		}
	},
	instr.SWAP: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			i.stack[i.sp-1], i.stack[i.sp-2] = i.stack[i.sp-2], i.stack[i.sp-1]
			i.frames[i.fp-1].ip++
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
			f := i.fr
			i.sp--
			cond := i.stack[i.sp].I32()
			if cond != 0 {
				f.ip += offset
			}
			f.ip += 3
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
			f := &i.frames[i.fp-1]
			i.sp--
			cond := int(i.stack[i.sp].I32())
			if cond < 0 || cond >= count {
				cond = count
			}
			f.ip += offsets[cond] + count*2 + 4
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
			i.frames[i.fp-1].ip++
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
				f.addr = addr
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
			f := &i.frames[i.fp-1]
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
				i.release(f.addr)
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
			f := i.fr
			if idx < 0 || idx >= len(i.globals) {
				panic(ErrSegmentationFault)
			}
			val := i.globals[idx]
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
			i.stack[i.sp] = val
			i.sp++
			f.ip += 3
		}
	},
	instr.GLOBAL_SET: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			f := &i.frames[i.fp-1]
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
			f.ip += 3
		}
	},
	instr.GLOBAL_TEE: func(c *threadedCompiler) func(i *Interpreter) {
		idx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 3
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			f := &i.frames[i.fp-1]
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
			f.ip += 3
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
		if idx < len(c.locals) && c.locals[idx] != types.KindRef {
			var fused func(*Interpreter)
			switch c.locals[idx] {
			case types.KindI32:
				fused = c.fuseI32(func(i *Interpreter) int32 {
					addr := i.fr.bp + idx
					if addr > i.sp {
						panic(ErrSegmentationFault)
					}
					return i.stack[addr].I32()
				}, 2)
			case types.KindI64:
				fused = c.fuseI64(func(i *Interpreter) int64 {
					addr := i.fr.bp + idx
					if addr > i.sp {
						panic(ErrSegmentationFault)
					}
					return i.stack[addr].I64()
				}, 2)
			case types.KindF32:
				fused = c.fuseF32(func(i *Interpreter) float32 {
					addr := i.fr.bp + idx
					if addr > i.sp {
						panic(ErrSegmentationFault)
					}
					return i.stack[addr].F32()
				}, 2)
			case types.KindF64:
				fused = c.fuseF64(func(i *Interpreter) float64 {
					addr := i.fr.bp + idx
					if addr > i.sp {
						panic(ErrSegmentationFault)
					}
					return i.stack[addr].F64()
				}, 2)
			}
			if fused != nil {
				return fused
			}
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				f := i.fr
				addr := f.bp + idx
				if addr > i.sp {
					panic(ErrSegmentationFault)
				}
				i.stack[i.sp] = i.stack[addr]
				i.sp++
				f.ip += 2
			}
		}
		if idx < len(c.locals) && c.locals[idx] == types.KindRef {
			if fused := c.fuseRef(func(i *Interpreter) types.Boxed {
				return i.stack[i.fr.bp+idx]
			}, 2); fused != nil {
				return fused
			}
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			f := &i.frames[i.fp-1]
			addr := f.bp + idx
			if addr > i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[addr]
			if val.Kind() == types.KindRef {
				i.retain(val.Ref())
			}
			i.stack[i.sp] = val
			i.sp++
			f.ip += 2
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
			f := &i.frames[i.fp-1]
			addr := f.bp + idx
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
			f.ip += 2
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
			f := &i.frames[i.fp-1]
			addr := f.bp + idx
			if addr > i.sp {
				panic(ErrSegmentationFault)
			}
			val := i.stack[i.sp-1]
			old := i.stack[addr]
			if old != val && old.Kind() == types.KindRef {
				i.release(old.Ref())
			}
			i.stack[addr] = val
			f.ip += 2
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
		if val.Kind() == types.KindRef {
			addr := val.Ref()
			if !c.precise && c.ip < len(c.code) {
				switch instr.Opcode(c.code[c.ip]) {
				case instr.CALL:
					switch fn := c.heap[addr].(type) {
					case *types.Function:
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
							if i.sp+locals >= len(i.stack) {
								panic(ErrStackOverflow)
							}
							if locals > 0 {
								clear(i.stack[i.sp : i.sp+locals])
							}
							i.fr.ip += 4
							f := &i.frames[i.fp]
							f.code = i.code[addr]
							f.addr = addr
							f.ip = 0
							f.bp = i.sp - params
							f.returns = returns
							f.release = false
							i.sp += locals
							i.fp++
							i.fr = f
						}
					case *HostFunction:
						return func(i *Interpreter) {
							if i.fp == len(i.frames) {
								panic(ErrFrameOverflow)
							}
							if i.sp < len(fn.Typ.Params) {
								panic(ErrStackUnderflow)
							}
							if i.sp+len(fn.Typ.Returns)-len(fn.Typ.Params) >= len(i.stack) {
								panic(ErrStackOverflow)
							}
							params := i.stack[i.sp-len(fn.Typ.Params) : i.sp]
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
							i.sp += len(fn.Typ.Returns) - len(fn.Typ.Params)
							copy(i.stack[i.sp-len(fn.Typ.Returns):i.sp], returns)
							i.fr.ip += 4
						}
					default:
						return func(i *Interpreter) {
							panic(ErrTypeMismatch)
						}
					}
				default:
				}
			}
			if fused := c.fuseRef(func(_ *Interpreter) types.Boxed { return val }, 3); fused != nil {
				return fused
			}
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				i.retain(addr)
				i.stack[i.sp] = val
				i.sp++
				i.frames[i.fp-1].ip += 3
			}
		}
		var fused func(*Interpreter)
		switch val.Kind() {
		case types.KindI32:
			v := val.I32()
			fused = c.fuseI32(func(*Interpreter) int32 { return v }, 3)
		case types.KindI64:
			v := val.I64()
			fused = c.fuseI64(func(*Interpreter) int64 { return v }, 3)
		case types.KindF32:
			v := val.F32()
			fused = c.fuseF32(func(*Interpreter) float32 { return v }, 3)
		case types.KindF64:
			v := val.F64()
			fused = c.fuseF64(func(*Interpreter) float64 { return v }, 3)
		}
		if fused != nil {
			return fused
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = val
			i.sp++
			i.frames[i.fp-1].ip += 3
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip += 3
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
			i.frames[i.fp-1].ip += 3
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
		}
	},
	instr.I32_CONST: func(c *threadedCompiler) func(i *Interpreter) {
		raw := *(*int32)(unsafe.Pointer(&c.code[c.ip+1]))
		val := types.BoxI32(raw)
		c.ip += 5
		if fused := c.fuseI32(func(*Interpreter) int32 { return raw }, 5); fused != nil {
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
				i.frames[i.fp-1].ip += 9
			}
		}
		v := types.I64(val)
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = types.BoxRef(i.alloc(v))
			i.sp++
			i.frames[i.fp-1].ip += 9
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
		}
	},
	instr.F32_CONST: func(c *threadedCompiler) func(i *Interpreter) {
		raw := *(*float32)(unsafe.Pointer(&c.code[c.ip+1]))
		val := types.BoxF32(raw)
		c.ip += 5
		if fused := c.fuseF32(func(*Interpreter) float32 { return raw }, 5); fused != nil {
			return fused
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = val
			i.sp++
			i.frames[i.fp-1].ip += 5
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
		}
	},
	instr.F64_CONST: func(c *threadedCompiler) func(i *Interpreter) {
		raw := *(*float64)(unsafe.Pointer(&c.code[c.ip+1]))
		val := types.BoxF64(raw)
		c.ip += 9
		if fused := c.fuseF64(func(*Interpreter) float64 { return raw }, 9); fused != nil {
			return fused
		}
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = val
			i.sp++
			i.frames[i.fp-1].ip += 9
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_NEW_UTF32: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val, _ := i.unbox(i.stack[i.sp-1]).(types.I32Array)
			i.stack[i.sp-1] = types.BoxRef(i.alloc(types.String(val)))
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_LEN: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			v, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			i.stack[i.sp-1] = types.BoxI32(int32(len(v)))
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_CONCAT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
			i.sp--
			i.stack[i.sp-1] = types.BoxRef(i.alloc(v2 + v1))
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_EQ: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 == v1)
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_NE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 != v1)
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_LT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 < v1)
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_GT: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 > v1)
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_LE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 <= v1)
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_GE: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp < 2 {
				panic(ErrStackUnderflow)
			}
			v1, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			v2, _ := i.unbox(i.stack[i.sp-2]).(types.String)
			i.sp--
			i.stack[i.sp-1] = types.BoxBool(v2 >= v1)
			i.frames[i.fp-1].ip++
		}
	},
	instr.STRING_ENCODE_UTF32: func(c *threadedCompiler) func(i *Interpreter) {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == 0 {
				panic(ErrStackUnderflow)
			}
			val, _ := i.unbox(i.stack[i.sp-1]).(types.String)
			i.stack[i.sp-1] = types.BoxRef(i.alloc(types.I32Array(val)))
			i.frames[i.fp-1].ip++
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
				i.frames[i.fp-1].ip += 3
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
				i.frames[i.fp-1].ip += 3
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
				i.frames[i.fp-1].ip += 3
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
				i.frames[i.fp-1].ip += 3
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
				i.frames[i.fp-1].ip += 3
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
				i.frames[i.fp-1].ip += 3
			}
		case types.KindI64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.I64Array, size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.frames[i.fp-1].ip += 3
			}
		case types.KindF32:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.F32Array, size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.frames[i.fp-1].ip += 3
			}
		case types.KindF64:
			return func(i *Interpreter) {
				if i.sp < 1 {
					panic(ErrStackUnderflow)
				}
				size := i.stack[i.sp-1].I32()
				val := make(types.F64Array, size)
				i.stack[i.sp-1] = types.BoxRef(i.alloc(val))
				i.frames[i.fp-1].ip += 3
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
				i.frames[i.fp-1].ip += 3
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip++
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
			i.frames[i.fp-1].ip += 3
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
			i.frames[i.fp-1].ip += 3
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
			s, ok := i.heap[addr].(*types.Struct)
			if !ok {
				panic(ErrTypeMismatch)
			}
			typ := s.Typ
			if idx < 0 || idx >= len(typ.Fields) {
				panic(ErrSegmentationFault)
			}
			field := typ.Fields[idx]
			var val types.Boxed
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
			i.release(addr)
			i.sp--
			i.stack[i.sp-1] = val
			i.frames[i.fp-1].ip++
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
			s, ok := i.heap[addr].(*types.Struct)
			if !ok {
				panic(ErrTypeMismatch)
			}
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
			i.release(addr)
			i.sp -= 3
			i.frames[i.fp-1].ip++
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

// fuseI32 peeks the next opcode and, when it is a fusible I32 binary op,
// consumes it and returns a closure that loads the right-hand operand via
// `load`, pops the left-hand from the stack, and pushes the result.
// `advance` is the byte width of the producer instruction.
func (c *threadedCompiler) fuseI32(load func(*Interpreter) int32, advance int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
		return nil
	}
	var op func(_ *Interpreter, lhs, rhs int32) types.Boxed
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I32_ADD:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a + b) }
	case instr.I32_SUB:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a - b) }
	case instr.I32_MUL:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a * b) }
	case instr.I32_DIV_S:
		op = func(_ *Interpreter, a, b int32) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return types.BoxI32(a / b)
		}
	case instr.I32_DIV_U:
		op = func(_ *Interpreter, a, b int32) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return types.BoxI32(int32(uint32(a) / uint32(b)))
		}
	case instr.I32_REM_S:
		op = func(_ *Interpreter, a, b int32) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return types.BoxI32(a % b)
		}
	case instr.I32_REM_U:
		op = func(_ *Interpreter, a, b int32) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return types.BoxI32(int32(uint32(a) % uint32(b)))
		}
	case instr.I32_SHL:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a << (b & 0x1F)) }
	case instr.I32_SHR_S:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a >> (b & 0x1F)) }
	case instr.I32_SHR_U:
		op = func(_ *Interpreter, a, b int32) types.Boxed {
			return types.BoxI32(int32(uint32(a) >> (b & 0x1F)))
		}
	case instr.I32_XOR:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a ^ b) }
	case instr.I32_AND:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a & b) }
	case instr.I32_OR:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxI32(a | b) }
	case instr.I32_EQ:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(a == b) }
	case instr.I32_NE:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(a != b) }
	case instr.I32_LT_S:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(a < b) }
	case instr.I32_LT_U:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(uint32(a) < uint32(b)) }
	case instr.I32_GT_S:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(a > b) }
	case instr.I32_GT_U:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(uint32(a) > uint32(b)) }
	case instr.I32_LE_S:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(a <= b) }
	case instr.I32_LE_U:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(uint32(a) <= uint32(b)) }
	case instr.I32_GE_S:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(a >= b) }
	case instr.I32_GE_U:
		op = func(_ *Interpreter, a, b int32) types.Boxed { return types.BoxBool(uint32(a) >= uint32(b)) }
	default:
		return nil
	}
	c.ip++
	return func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		rhs := load(i)
		lhs := i.stack[i.sp-1].I32()
		i.stack[i.sp-1] = op(i, lhs, rhs)
		i.fr.ip += advance + 1
	}
}

// fuseI64 mirrors fuseI32 for 64-bit integer ops. The left-hand operand uses
// unboxI64 (releasing any heap-boxed I64 reference) and the result uses
// boxI64 (allocating only when the value is out of the boxable range).
func (c *threadedCompiler) fuseI64(load func(*Interpreter) int64, advance int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
		return nil
	}
	var op func(i *Interpreter, lhs, rhs int64) types.Boxed
	switch instr.Opcode(c.code[c.ip]) {
	case instr.I64_ADD:
		op = func(i *Interpreter, a, b int64) types.Boxed { return i.boxI64(a + b) }
	case instr.I64_SUB:
		op = func(i *Interpreter, a, b int64) types.Boxed { return i.boxI64(a - b) }
	case instr.I64_MUL:
		op = func(i *Interpreter, a, b int64) types.Boxed { return i.boxI64(a * b) }
	case instr.I64_DIV_S:
		op = func(i *Interpreter, a, b int64) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return i.boxI64(a / b)
		}
	case instr.I64_DIV_U:
		op = func(i *Interpreter, a, b int64) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return i.boxI64(int64(uint64(a) / uint64(b)))
		}
	case instr.I64_REM_S:
		op = func(i *Interpreter, a, b int64) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return i.boxI64(a % b)
		}
	case instr.I64_REM_U:
		op = func(i *Interpreter, a, b int64) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return i.boxI64(int64(uint64(a) % uint64(b)))
		}
	case instr.I64_SHL:
		op = func(i *Interpreter, a, b int64) types.Boxed { return i.boxI64(a << (b & 0x3F)) }
	case instr.I64_SHR_S:
		op = func(i *Interpreter, a, b int64) types.Boxed { return i.boxI64(a >> (b & 0x3F)) }
	case instr.I64_SHR_U:
		op = func(i *Interpreter, a, b int64) types.Boxed {
			return i.boxI64(int64(uint64(a) >> (b & 0x3F)))
		}
	case instr.I64_EQ:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(a == b) }
	case instr.I64_NE:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(a != b) }
	case instr.I64_LT_S:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(a < b) }
	case instr.I64_LT_U:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(uint64(a) < uint64(b)) }
	case instr.I64_GT_S:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(a > b) }
	case instr.I64_GT_U:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(uint64(a) > uint64(b)) }
	case instr.I64_LE_S:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(a <= b) }
	case instr.I64_LE_U:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(uint64(a) <= uint64(b)) }
	case instr.I64_GE_S:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(a >= b) }
	case instr.I64_GE_U:
		op = func(_ *Interpreter, a, b int64) types.Boxed { return types.BoxBool(uint64(a) >= uint64(b)) }
	default:
		return nil
	}
	c.ip++
	return func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		rhs := load(i)
		lhs := i.unboxI64(i.stack[i.sp-1])
		i.stack[i.sp-1] = op(i, lhs, rhs)
		i.fr.ip += advance + 1
	}
}

// fuseF32 mirrors fuseI32 for 32-bit floating-point ops.
func (c *threadedCompiler) fuseF32(load func(*Interpreter) float32, advance int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
		return nil
	}
	var op func(_ *Interpreter, lhs, rhs float32) types.Boxed
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F32_ADD:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxF32(a + b) }
	case instr.F32_SUB:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxF32(a - b) }
	case instr.F32_MUL:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxF32(a * b) }
	case instr.F32_DIV:
		op = func(_ *Interpreter, a, b float32) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return types.BoxF32(a / b)
		}
	case instr.F32_EQ:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxBool(a == b) }
	case instr.F32_NE:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxBool(a != b) }
	case instr.F32_LT:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxBool(a < b) }
	case instr.F32_GT:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxBool(a > b) }
	case instr.F32_LE:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxBool(a <= b) }
	case instr.F32_GE:
		op = func(_ *Interpreter, a, b float32) types.Boxed { return types.BoxBool(a >= b) }
	default:
		return nil
	}
	c.ip++
	return func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		rhs := load(i)
		lhs := i.stack[i.sp-1].F32()
		i.stack[i.sp-1] = op(i, lhs, rhs)
		i.fr.ip += advance + 1
	}
}

// fuseF64 mirrors fuseI32 for 64-bit floating-point ops.
func (c *threadedCompiler) fuseF64(load func(*Interpreter) float64, advance int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
		return nil
	}
	var op func(_ *Interpreter, lhs, rhs float64) types.Boxed
	switch instr.Opcode(c.code[c.ip]) {
	case instr.F64_ADD:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxF64(a + b) }
	case instr.F64_SUB:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxF64(a - b) }
	case instr.F64_MUL:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxF64(a * b) }
	case instr.F64_DIV:
		op = func(_ *Interpreter, a, b float64) types.Boxed {
			if b == 0 {
				panic(ErrDivideByZero)
			}
			return types.BoxF64(a / b)
		}
	case instr.F64_EQ:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxBool(a == b) }
	case instr.F64_NE:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxBool(a != b) }
	case instr.F64_LT:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxBool(a < b) }
	case instr.F64_GT:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxBool(a > b) }
	case instr.F64_LE:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxBool(a <= b) }
	case instr.F64_GE:
		op = func(_ *Interpreter, a, b float64) types.Boxed { return types.BoxBool(a >= b) }
	default:
		return nil
	}
	c.ip++
	return func(i *Interpreter) {
		if i.sp == 0 {
			panic(ErrStackUnderflow)
		}
		rhs := load(i)
		lhs := i.stack[i.sp-1].F64()
		i.stack[i.sp-1] = op(i, lhs, rhs)
		i.fr.ip += advance + 1
	}
}

// fuseRef peeks ahead of the current ip and, when the layout matches a
// ref-producer chain ending in a ref op, returns a single fused closure for
// the whole sequence. `loadRef` reads the boxed ref produced by the outer
// instruction (LOCAL_GET ref / CONST_GET ref); `advance` is the byte width of
// that producer. Mirrors the shape of fuseI32 / fuseI64 / fuseF32 / fuseF64.
func (c *threadedCompiler) fuseRef(loadRef func(*Interpreter) types.Boxed, advance int) func(*Interpreter) {
	if c.precise || c.ip >= len(c.code) {
		return nil
	}

	// 1-arg ref op directly after the outer producer.
	var op1 func(*Interpreter, types.Boxed) types.Boxed
	switch instr.Opcode(c.code[c.ip]) {
	case instr.STRING_LEN:
		op1 = func(i *Interpreter, b types.Boxed) types.Boxed {
			s, _ := i.heap[b.Ref()].(types.String)
			return types.BoxI32(int32(len(s)))
		}
	case instr.ARRAY_LEN:
		op1 = func(i *Interpreter, b types.Boxed) types.Boxed {
			switch arr := i.heap[b.Ref()].(type) {
			case types.I32Array:
				return types.BoxI32(int32(len(arr)))
			case types.I64Array:
				return types.BoxI32(int32(len(arr)))
			case types.F32Array:
				return types.BoxI32(int32(len(arr)))
			case types.F64Array:
				return types.BoxI32(int32(len(arr)))
			case *types.Array:
				return types.BoxI32(int32(len(arr.Elems)))
			default:
				panic(ErrTypeMismatch)
			}
		}
	case instr.REF_IS_NULL:
		op1 = func(_ *Interpreter, b types.Boxed) types.Boxed {
			return types.BoxBool(b.Ref() == 0)
		}
	}
	if op1 != nil {
		c.ip++
		return func(i *Interpreter) {
			if i.sp == len(i.stack) {
				panic(ErrStackOverflow)
			}
			i.stack[i.sp] = op1(i, loadRef(i))
			i.sp++
			i.fr.ip += advance + 1
		}
	}

	// Peek a second loader. Either i32 (for ARRAY_GET / STRUCT_GET) or ref
	// (for 2-arg ref ops). Only one of loadIdx / loadRef2 is populated.
	var (
		loadIdx  func(*Interpreter) int32
		loadRef2 func(*Interpreter) types.Boxed
		w2       int
	)
	switch instr.Opcode(c.code[c.ip]) {
	case instr.LOCAL_GET:
		if c.ip+1 >= len(c.code) {
			return nil
		}
		lidx := int(c.code[c.ip+1])
		if lidx < 0 || lidx >= len(c.locals) {
			return nil
		}
		switch c.locals[lidx] {
		case types.KindI32:
			loadIdx = func(i *Interpreter) int32 {
				return i.stack[i.fr.bp+lidx].I32()
			}
			w2 = 2
		case types.KindRef:
			loadRef2 = func(i *Interpreter) types.Boxed {
				return i.stack[i.fr.bp+lidx]
			}
			w2 = 2
		}
	case instr.CONST_GET:
		if c.ip+2 >= len(c.code) {
			return nil
		}
		cidx := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
		if cidx < 0 || cidx >= len(c.constants) {
			return nil
		}
		v := c.constants[cidx]
		switch v.Kind() {
		case types.KindI32:
			iv := v.I32()
			loadIdx = func(_ *Interpreter) int32 { return iv }
			w2 = 3
		case types.KindRef:
			loadRef2 = func(_ *Interpreter) types.Boxed { return v }
			w2 = 3
		}
	case instr.I32_CONST:
		if c.ip+4 >= len(c.code) {
			return nil
		}
		v := *(*int32)(unsafe.Pointer(&c.code[c.ip+1]))
		loadIdx = func(_ *Interpreter) int32 { return v }
		w2 = 5
	}
	if w2 == 0 {
		return nil
	}
	opIp := c.ip + w2
	if opIp >= len(c.code) {
		return nil
	}

	// (ref) ; (i32 loader) ; ARRAY_GET | STRUCT_GET
	if loadIdx != nil {
		var op2 func(i *Interpreter, ref types.Boxed, idx int) types.Boxed
		switch instr.Opcode(c.code[opIp]) {
		case instr.ARRAY_GET:
			op2 = func(i *Interpreter, ref types.Boxed, idx int) types.Boxed {
				addr := ref.Ref()
				switch arr := i.heap[addr].(type) {
				case types.I32Array:
					if idx < 0 || idx >= len(arr) {
						panic(ErrIndexOutOfRange)
					}
					return types.BoxI32(int32(arr[idx]))
				case types.I64Array:
					if idx < 0 || idx >= len(arr) {
						panic(ErrIndexOutOfRange)
					}
					return i.boxI64(int64(arr[idx]))
				case types.F32Array:
					if idx < 0 || idx >= len(arr) {
						panic(ErrIndexOutOfRange)
					}
					return types.BoxF32(float32(arr[idx]))
				case types.F64Array:
					if idx < 0 || idx >= len(arr) {
						panic(ErrIndexOutOfRange)
					}
					return types.BoxF64(float64(arr[idx]))
				case *types.Array:
					if idx < 0 || idx >= len(arr.Elems) {
						panic(ErrIndexOutOfRange)
					}
					elem := arr.Elems[idx]
					if elem.Kind() == types.KindRef {
						i.retain(elem.Ref())
					}
					return elem
				default:
					panic(ErrTypeMismatch)
				}
			}
		case instr.STRUCT_GET:
			op2 = func(i *Interpreter, ref types.Boxed, idx int) types.Boxed {
				s, ok := i.heap[ref.Ref()].(*types.Struct)
				if !ok {
					panic(ErrTypeMismatch)
				}
				if idx < 0 || idx >= len(s.Typ.Fields) {
					panic(ErrSegmentationFault)
				}
				switch s.Typ.Fields[idx].Kind {
				case types.KindI32, types.KindF32, types.KindF64:
					return s.Field(idx)
				case types.KindI64:
					return i.boxI64(int64(s.Raw(idx)))
				case types.KindRef:
					v := types.Boxed(s.Raw(idx))
					if v.Kind() == types.KindRef {
						i.retain(v.Ref())
					}
					return v
				default:
					panic(ErrTypeMismatch)
				}
			}
		}
		if op2 != nil {
			c.ip = opIp + 1
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				i.stack[i.sp] = op2(i, loadRef(i), int(loadIdx(i)))
				i.sp++
				i.fr.ip += advance + w2 + 1
			}
		}
	}

	// (ref) ; (ref loader) ; 2-arg ref op
	if loadRef2 != nil {
		var op2 func(*Interpreter, types.Boxed, types.Boxed) types.Boxed
		switch instr.Opcode(c.code[opIp]) {
		case instr.REF_EQ:
			op2 = func(_ *Interpreter, a, b types.Boxed) types.Boxed {
				return types.BoxBool(a == b)
			}
		case instr.REF_NE:
			op2 = func(_ *Interpreter, a, b types.Boxed) types.Boxed {
				return types.BoxBool(a != b)
			}
		case instr.STRING_EQ:
			op2 = func(i *Interpreter, a, b types.Boxed) types.Boxed {
				sa, _ := i.heap[a.Ref()].(types.String)
				sb, _ := i.heap[b.Ref()].(types.String)
				return types.BoxBool(sa == sb)
			}
		case instr.STRING_NE:
			op2 = func(i *Interpreter, a, b types.Boxed) types.Boxed {
				sa, _ := i.heap[a.Ref()].(types.String)
				sb, _ := i.heap[b.Ref()].(types.String)
				return types.BoxBool(sa != sb)
			}
		case instr.STRING_LT:
			op2 = func(i *Interpreter, a, b types.Boxed) types.Boxed {
				sa, _ := i.heap[a.Ref()].(types.String)
				sb, _ := i.heap[b.Ref()].(types.String)
				return types.BoxBool(sa < sb)
			}
		case instr.STRING_GT:
			op2 = func(i *Interpreter, a, b types.Boxed) types.Boxed {
				sa, _ := i.heap[a.Ref()].(types.String)
				sb, _ := i.heap[b.Ref()].(types.String)
				return types.BoxBool(sa > sb)
			}
		case instr.STRING_LE:
			op2 = func(i *Interpreter, a, b types.Boxed) types.Boxed {
				sa, _ := i.heap[a.Ref()].(types.String)
				sb, _ := i.heap[b.Ref()].(types.String)
				return types.BoxBool(sa <= sb)
			}
		case instr.STRING_GE:
			op2 = func(i *Interpreter, a, b types.Boxed) types.Boxed {
				sa, _ := i.heap[a.Ref()].(types.String)
				sb, _ := i.heap[b.Ref()].(types.String)
				return types.BoxBool(sa >= sb)
			}
		}
		if op2 != nil {
			c.ip = opIp + 1
			return func(i *Interpreter) {
				if i.sp == len(i.stack) {
					panic(ErrStackOverflow)
				}
				i.stack[i.sp] = op2(i, loadRef(i), loadRef2(i))
				i.sp++
				i.fr.ip += advance + w2 + 1
			}
		}
	}

	return nil
}
