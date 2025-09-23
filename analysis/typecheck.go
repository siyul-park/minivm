package analysis

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type TypeCheckPassOption struct {
	Global int
	Stack  int
}

type TypeCheckPass struct {
	global int
	stack  int
	heap   int
}

type frame struct {
	fn        *types.FunctionType
	block     int
	constants []types.Value
	global    []types.Type
	stack     []types.Type
	sp        int
}

var (
	ErrUnknownOpcode     = errors.New("unknown opcode")
	ErrSegmentationFault = errors.New("segmentation fault")
	ErrStackOverflow     = errors.New("stack overflow")
	ErrStackUnderflow    = errors.New("stack underflow")
	ErrTypeMismatch      = errors.New("type mismatch")
)

var _ Pass = (*TypeCheckPass)(nil)

var typeCheck = [256]func(f *frame, operands []uint64) error{
	instr.NOP: func(f *frame, operands []uint64) error {
		return nil
	},
	instr.UNREACHABLE: func(f *frame, operands []uint64) error {
		return nil
	},
	instr.DROP: func(f *frame, operands []uint64) error {
		if f.sp == 0 {
			return ErrStackUnderflow
		}
		f.sp--
		return nil
	},
	instr.DUP: func(f *frame, operands []uint64) error {
		if f.sp == 0 {
			return ErrStackUnderflow
		}
		if f.sp == len(f.stack) {
			return ErrStackOverflow
		}
		f.stack[f.sp] = f.stack[f.sp-1]
		f.sp++
		return nil
	},
	instr.SWAP: func(f *frame, operands []uint64) error {
		if f.sp < 2 {
			return ErrStackUnderflow
		}
		f.stack[f.sp-1], f.stack[f.sp-2] = f.stack[f.sp-2], f.stack[f.sp-1]
		return nil
	},
	instr.BR: func(f *frame, operands []uint64) error {
		return nil
	},
	instr.BR_IF: func(f *frame, operands []uint64) error {
		if f.sp == 0 {
			return ErrStackUnderflow
		}
		f.sp--
		if f.stack[f.sp] != types.TypeI32 {
			return ErrTypeMismatch
		}
		return nil
	},
	instr.CALL: func(f *frame, operands []uint64) error {
		if f.sp == 0 {
			return ErrStackUnderflow
		}

		f.sp--
		fn, ok := f.stack[f.sp].(*types.FunctionType)
		if !ok {
			return ErrTypeMismatch
		}

		params := len(fn.Params)
		returns := len(fn.Returns)
		if f.sp < params {
			return ErrStackUnderflow
		}
		if f.sp-params+returns >= len(f.stack) {
			return ErrStackOverflow
		}

		for idx := 0; idx < params; idx++ {
			if fn.Params[idx] != f.stack[f.sp-params+idx] {
				return ErrTypeMismatch
			}
		}
		f.sp -= params

		for idx := 0; idx < returns; idx++ {
			f.stack[f.sp+idx] = fn.Returns[idx]
		}
		f.sp += returns
		return nil
	},
	instr.RETURN: func(f *frame, operands []uint64) error {
		fn := f.fn
		returns := len(fn.Returns)
		if f.sp < returns {
			return ErrStackUnderflow
		}
		for idx := 0; idx < returns; idx++ {
			if !fn.Returns[idx].Equals(f.stack[f.sp-returns+idx]) {
				return ErrTypeMismatch
			}
		}
		return nil
	},
	instr.GLOBAL_GET: func(f *frame, operands []uint64) error {
		if f.sp == len(f.stack) {
			return ErrStackOverflow
		}
		idx := int(int32(operands[0]))
		if idx < 0 || idx >= len(f.global) {
			return ErrSegmentationFault
		}
		f.stack[f.sp] = f.global[idx]
		f.sp++
		return nil
	},
	instr.GLOBAL_SET: func(f *frame, operands []uint64) error {
		if f.sp == len(f.stack) {
			return ErrStackOverflow
		}
		idx := int(int32(operands[0]))
		if idx < 0 {
			return ErrSegmentationFault
		}
		val := f.stack[f.sp-1]
		if idx >= len(f.global) {
			if cap(f.global) > idx {
				f.global = f.global[:idx+1]
			} else {
				global := make([]types.Type, idx*2)
				copy(global, f.global)
				f.global = global[:idx+1]
			}
		}
		f.global[idx] = val
		f.sp--
		return nil
	},
	instr.LOCAL_GET: func(f *frame, operands []uint64) error {
		if f.sp == len(f.stack) {
			return ErrStackOverflow
		}
		idx := int(int32(operands[0]))
		if idx < 0 || idx >= f.sp {
			return ErrSegmentationFault
		}
		f.stack[f.sp] = f.stack[idx]
		f.sp++
		return nil
	},
	instr.LOCAL_SET: func(f *frame, operands []uint64) error {
		if f.sp == 0 {
			return ErrStackUnderflow
		}
		idx := int(int32(operands[0]))
		if idx < 0 || idx > f.sp {
			return ErrSegmentationFault
		}
		f.stack[idx] = f.stack[f.sp-1]
		f.sp--
		return nil
	},
	instr.CONST_GET: func(f *frame, operands []uint64) error {
		if f.sp == len(f.stack) {
			return ErrStackOverflow
		}
		idx := int(int32(operands[0]))
		if idx < 0 || idx >= len(f.constants) {
			return ErrSegmentationFault
		}
		f.stack[f.sp] = f.constants[idx].Type()
		f.sp++
		return nil
	},
	instr.I32_CONST:     pushType(types.TypeI32),
	instr.I32_ADD:       popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SUB:       popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_MUL:       popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_DIV_S:     popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_DIV_U:     popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_REM_S:     popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_REM_U:     popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SHL:       popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SHR_S:     popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SHR_U:     popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_XOR:       popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_AND:       popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_OR:        popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_EQ:        popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_NE:        popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LT_S:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LT_U:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GT_S:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GT_U:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LE_S:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LE_U:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GE_S:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GE_U:      popAndPushType([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_TO_I64_S:  popAndPushType([]types.Type{types.TypeI32}, []types.Type{types.TypeI64}),
	instr.I32_TO_I64_U:  popAndPushType([]types.Type{types.TypeI32}, []types.Type{types.TypeI64}),
	instr.I32_TO_F32_U:  popAndPushType([]types.Type{types.TypeI32}, []types.Type{types.TypeF32}),
	instr.I32_TO_F32_S:  popAndPushType([]types.Type{types.TypeI32}, []types.Type{types.TypeF32}),
	instr.I32_TO_F64_U:  popAndPushType([]types.Type{types.TypeI32}, []types.Type{types.TypeF64}),
	instr.I32_TO_F64_S:  popAndPushType([]types.Type{types.TypeI32}, []types.Type{types.TypeF64}),
	instr.I64_CONST:     pushType(types.TypeI64),
	instr.I64_ADD:       popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SUB:       popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_MUL:       popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_DIV_S:     popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_DIV_U:     popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_REM_S:     popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_REM_U:     popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SHL:       popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SHR_S:     popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SHR_U:     popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_EQ:        popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_NE:        popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LT_S:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LT_U:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GT_S:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GT_U:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LE_S:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LE_U:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GE_S:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GE_U:      popAndPushType([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_TO_I32:    popAndPushType([]types.Type{types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_TO_F32_S:  popAndPushType([]types.Type{types.TypeI64}, []types.Type{types.TypeF32}),
	instr.I64_TO_F32_U:  popAndPushType([]types.Type{types.TypeI64}, []types.Type{types.TypeF32}),
	instr.I64_TO_F64_S:  popAndPushType([]types.Type{types.TypeI64}, []types.Type{types.TypeF64}),
	instr.I64_TO_F64_U:  popAndPushType([]types.Type{types.TypeI64}, []types.Type{types.TypeF64}),
	instr.F32_CONST:     pushType(types.TypeF32),
	instr.F32_ADD:       popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_SUB:       popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_MUL:       popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_DIV:       popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_EQ:        popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_NE:        popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_LT:        popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_GT:        popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_LE:        popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_GE:        popAndPushType([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_TO_I32_S:  popAndPushType([]types.Type{types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_TO_I32_U:  popAndPushType([]types.Type{types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_TO_I64_S:  popAndPushType([]types.Type{types.TypeF32}, []types.Type{types.TypeI64}),
	instr.F32_TO_I64_U:  popAndPushType([]types.Type{types.TypeF32}, []types.Type{types.TypeI64}),
	instr.F32_TO_F64:    popAndPushType([]types.Type{types.TypeF32}, []types.Type{types.TypeF64}),
	instr.F64_CONST:     pushType(types.TypeF64),
	instr.F64_ADD:       popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_SUB:       popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_MUL:       popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_DIV:       popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_EQ:        popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_NE:        popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_LT:        popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_GT:        popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_LE:        popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_GE:        popAndPushType([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_TO_I32_S:  popAndPushType([]types.Type{types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_TO_I32_U:  popAndPushType([]types.Type{types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_TO_I64_S:  popAndPushType([]types.Type{types.TypeF64}, []types.Type{types.TypeI64}),
	instr.F64_TO_I64_U:  popAndPushType([]types.Type{types.TypeF64}, []types.Type{types.TypeI64}),
	instr.F64_TO_F32:    popAndPushType([]types.Type{types.TypeF64}, []types.Type{types.TypeF32}),
	instr.STRING_LEN:    popAndPushType([]types.Type{types.TypeString}, []types.Type{types.TypeI32}),
	instr.STRING_CONCAT: popAndPushType([]types.Type{types.TypeString, types.TypeString}, []types.Type{types.TypeString}),
	instr.STRING_EQ:     popAndPushType([]types.Type{types.TypeString, types.TypeString}, []types.Type{types.TypeI32}),
	instr.STRING_NE:     popAndPushType([]types.Type{types.TypeString, types.TypeString}, []types.Type{types.TypeI32}),
	instr.STRING_LT:     popAndPushType([]types.Type{types.TypeString, types.TypeString}, []types.Type{types.TypeI32}),
	instr.STRING_GT:     popAndPushType([]types.Type{types.TypeString, types.TypeString}, []types.Type{types.TypeI32}),
	instr.STRING_LE:     popAndPushType([]types.Type{types.TypeString, types.TypeString}, []types.Type{types.TypeI32}),
	instr.STRING_GE:     popAndPushType([]types.Type{types.TypeString, types.TypeString}, []types.Type{types.TypeI32}),
}

func pushType(typ types.Type) func(*frame, []uint64) error {
	return func(f *frame, operands []uint64) error {
		if f.sp == len(f.stack) {
			return ErrStackOverflow
		}
		f.stack[f.sp] = typ
		f.sp++
		return nil
	}
}

func popAndPushType(pop, push []types.Type) func(*frame, []uint64) error {
	return func(f *frame, operands []uint64) error {
		if f.sp < len(pop) {
			return ErrStackUnderflow
		}

		bp := f.sp - len(pop)
		for i, t := range pop {
			if !f.stack[bp+i].Equals(t) {
				return ErrTypeMismatch
			}
		}
		f.sp = bp

		if f.sp+len(push) > len(f.stack) {
			return ErrStackOverflow
		}
		for _, t := range push {
			f.stack[f.sp] = t
			f.sp++
		}
		return nil
	}
}

func NewTypeCheckPass(opts ...TypeCheckPassOption) *TypeCheckPass {
	g := 128
	f := 1024
	for _, opt := range opts {
		if opt.Global > 0 {
			g = opt.Global
		}
		if opt.Stack > 0 {
			f = opt.Stack
		}
	}

	return &TypeCheckPass{
		global: g,
		stack:  f,
	}
}

func (p *TypeCheckPass) Run(m *Module) error {
	global := make([]types.Type, 0, p.global)

	for _, fn := range m.Functions {
		if len(fn.CFG.Blocks) == 0 {
			continue
		}

		f := &frame{
			fn:        fn.Typ,
			block:     0,
			constants: m.Constants,
			global:    global,
			stack:     make([]types.Type, p.stack),
		}
		for i := 0; i < fn.Locals; i++ {
			f.stack[i] = types.TypeRef
		}
		for i, t := range fn.Typ.Params {
			f.stack[i] = t
		}
		f.sp += fn.Locals

		visits := make([][][]types.Type, len(fn.CFG.Blocks))
		frames := []*frame{f}

		for len(frames) > 0 {
			f := frames[0]
			frames = frames[1:]

			visited := false
			for _, prev := range visits[f.block] {
				if len(prev) != f.sp {
					continue
				}
				ok := true
				for i := 0; i < len(prev); i++ {
					if !prev[i].Equals(f.stack[i]) {
						ok = false
						break
					}
				}
				if ok {
					visited = true
				}
			}
			if visited {
				continue
			}

			visits[f.block] = append(visits[f.block], f.stack[:f.sp])

			blk := fn.CFG.Blocks[f.block]
			ip := 0
			for ip < len(blk.Code) {
				typ := instr.TypeOf(instr.Opcode(blk.Code[ip]))
				inst := instr.Instruction(blk.Code[ip : ip+typ.Size()])
				fn := typeCheck[inst.Opcode()]
				if fn == nil {
					return fmt.Errorf("%w: at=%d", ErrUnknownOpcode, ip+blk.Offset)
				}
				if err := fn(f, inst.Operands()); err != nil {
					return fmt.Errorf("%w: at=%d", err, ip+blk.Offset)
				}
				ip += len(inst)
			}

			for i := 0; i < len(f.global); i++ {
				if i >= len(global) {
					global = append(global, f.global[i])
					continue
				}
				if f.global[i] != nil {
					if global[i] == nil {
						global[i] = f.global[i]
					} else if !f.global[i].Equals(global[i]) {
						return fmt.Errorf("%w: global=%d", ErrTypeMismatch, i)
					}
				}
			}

			for _, s := range blk.Succs {
				frames = append(frames, &frame{
					fn:        fn.Typ,
					block:     s,
					constants: m.Constants,
					global:    append([]types.Type(nil), global...),
					stack:     append([]types.Type(nil), f.stack...),
					sp:        f.sp,
				})
			}
		}
	}
	return nil
}
