package analysis

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type Option struct {
	Global int
	Stack  int
}

type Verifier struct {
	code      []byte
	constants []types.Value
	global    int
	stack     int
	heap      int
}

type state struct {
	fn        *types.Function
	block     int
	constants []types.Value
	global    []types.Type
	stack     []types.Type
	sp        int
	ip        int
}

var (
	ErrUnknownOpcode       = errors.New("unknown opcode")
	ErrUnreachableExecuted = errors.New("unreachable executed")
	ErrSegmentationFault   = errors.New("segmentation fault")
	ErrStackOverflow       = errors.New("stack overflow")
	ErrStackUnderflow      = errors.New("stack underflow")
	ErrTypeMismatch        = errors.New("type mismatch")
)

var dispatch = [256]func(s *state) error{
	instr.NOP: func(s *state) error {
		s.ip++
		return nil
	},
	instr.UNREACHABLE: func(s *state) error {
		s.ip++
		return ErrUnreachableExecuted
	},
	instr.DROP: func(s *state) error {
		if s.sp == 0 {
			return ErrStackUnderflow
		}
		s.sp--
		s.ip++
		return nil
	},
	instr.DUP: func(s *state) error {
		if s.sp == 0 {
			return ErrStackUnderflow
		}
		if s.sp == len(s.stack) {
			return ErrStackOverflow
		}
		s.stack[s.sp] = s.stack[s.sp-1]
		s.sp++
		s.ip++
		return nil
	},
	instr.SWAP: func(s *state) error {
		if s.sp < 2 {
			return ErrStackUnderflow
		}
		s.stack[s.sp-1], s.stack[s.sp-2] = s.stack[s.sp-2], s.stack[s.sp-1]
		s.ip++
		return nil
	},
	instr.BR: func(s *state) error {
		s.ip += 5
		return nil
	},
	instr.BR_IF: func(s *state) error {
		if s.sp == 0 {
			return ErrStackUnderflow
		}
		s.sp--
		if s.stack[s.sp] != types.TypeI32 {
			return ErrTypeMismatch
		}
		s.ip += 5
		return nil
	},
	instr.CALL: func(s *state) error {
		if s.sp == 0 {
			return ErrStackUnderflow
		}

		s.sp--
		fn, ok := s.stack[s.sp].(*types.FunctionType)
		if !ok {
			return ErrTypeMismatch
		}

		params := len(fn.Params)
		returns := len(fn.Returns)
		if s.sp < params {
			return ErrStackUnderflow
		}
		if s.sp-params+returns >= len(s.stack) {
			return ErrStackOverflow
		}

		for idx := 0; idx < params; idx++ {
			if fn.Params[idx] != s.stack[s.sp-params+idx] {
				return ErrTypeMismatch
			}
		}
		s.sp -= params

		for idx := 0; idx < returns; idx++ {
			s.stack[s.sp+idx] = fn.Returns[idx]
		}
		s.sp += returns

		s.ip++
		return nil
	},
	instr.RETURN: func(s *state) error {
		fn := s.fn
		returns := len(fn.Typ.Returns)
		if s.sp < returns {
			return ErrStackUnderflow
		}
		for idx := 0; idx < returns; idx++ {
			if !fn.Typ.Returns[idx].Equals(s.stack[s.sp-returns+idx]) {
				return ErrTypeMismatch
			}
		}
		s.ip++
		return nil
	},
	instr.GLOBAL_GET: func(s *state) error {
		if s.sp == len(s.stack) {
			return ErrStackOverflow
		}
		code := s.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[s.ip+1:])))
		if idx < 0 || idx >= len(s.global) {
			return ErrSegmentationFault
		}
		s.stack[s.sp] = s.global[idx]
		s.sp++
		s.ip += 5
		return nil
	},
	instr.GLOBAL_SET: func(s *state) error {
		if s.sp == len(s.stack) {
			return ErrStackOverflow
		}
		code := s.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[s.ip+1:])))
		if idx < 0 {
			return ErrSegmentationFault
		}

		val := s.stack[s.sp-1]
		if idx >= len(s.global) {
			if cap(s.global) > idx {
				s.global = s.global[:idx+1]
			} else {
				global := make([]types.Type, idx*2)
				copy(global, s.global)
				s.global = global[:idx+1]
			}
		}

		s.global[idx] = val
		s.sp--
		s.ip += 5
		return nil
	},
	instr.LOCAL_GET: func(s *state) error {
		if s.sp == len(s.stack) {
			return ErrStackOverflow
		}

		fn := s.fn
		code := fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[s.ip+1:])))
		if idx < 0 || idx >= s.sp {
			return ErrSegmentationFault
		}

		s.stack[s.sp] = s.stack[idx]
		s.sp++
		s.ip += 5
		return nil
	},
	instr.LOCAL_SET: func(s *state) error {
		if s.sp == 0 {
			return ErrStackUnderflow
		}

		fn := s.fn
		code := fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[s.ip+1:])))
		if idx < 0 || idx > s.sp {
			return ErrSegmentationFault
		}

		s.stack[idx] = s.stack[s.sp-1]
		s.sp--
		s.ip += 5
		return nil
	},
	instr.FN_CONST: func(s *state) error {
		if s.sp == len(s.stack) {
			return ErrStackOverflow
		}
		code := s.fn.Code
		idx := int(int32(binary.BigEndian.Uint32(code[s.ip+1:])))
		if idx < 0 || idx >= len(s.constants) {
			return ErrSegmentationFault
		}
		fn, ok := s.constants[idx].(*types.Function)
		if !ok {
			return ErrTypeMismatch
		}
		s.stack[s.sp] = fn.Typ
		s.sp++
		s.ip += 5
		return nil
	},
	instr.I32_CONST:    pushType(types.TypeI32, 4),
	instr.I32_ADD:      pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SUB:      pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_MUL:      pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_DIV_S:    pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_DIV_U:    pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_REM_S:    pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_REM_U:    pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SHL:      pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SHR_S:    pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_SHR_U:    pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_XOR:      pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_AND:      pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_OR:       pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_EQ:       pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_NE:       pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LT_S:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LT_U:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GT_S:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GT_U:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LE_S:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_LE_U:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GE_S:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_GE_U:     pushTypeWithCheck([]types.Type{types.TypeI32, types.TypeI32}, []types.Type{types.TypeI32}),
	instr.I32_TO_I64_S: pushTypeWithCheck([]types.Type{types.TypeI32}, []types.Type{types.TypeI64}),
	instr.I32_TO_I64_U: pushTypeWithCheck([]types.Type{types.TypeI32}, []types.Type{types.TypeI64}),
	instr.I32_TO_F32_U: pushTypeWithCheck([]types.Type{types.TypeI32}, []types.Type{types.TypeF32}),
	instr.I32_TO_F32_S: pushTypeWithCheck([]types.Type{types.TypeI32}, []types.Type{types.TypeF32}),
	instr.I32_TO_F64_U: pushTypeWithCheck([]types.Type{types.TypeI32}, []types.Type{types.TypeF64}),
	instr.I32_TO_F64_S: pushTypeWithCheck([]types.Type{types.TypeI32}, []types.Type{types.TypeF64}),
	instr.I64_CONST:    pushType(types.TypeI64, 8),
	instr.I64_ADD:      pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SUB:      pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_MUL:      pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_DIV_S:    pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_DIV_U:    pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_REM_S:    pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_REM_U:    pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SHL:      pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SHR_S:    pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_SHR_U:    pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI64}),
	instr.I64_EQ:       pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_NE:       pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LT_S:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LT_U:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GT_S:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GT_U:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LE_S:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_LE_U:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GE_S:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_GE_U:     pushTypeWithCheck([]types.Type{types.TypeI64, types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_TO_I32:   pushTypeWithCheck([]types.Type{types.TypeI64}, []types.Type{types.TypeI32}),
	instr.I64_TO_F32_S: pushTypeWithCheck([]types.Type{types.TypeI64}, []types.Type{types.TypeF32}),
	instr.I64_TO_F32_U: pushTypeWithCheck([]types.Type{types.TypeI64}, []types.Type{types.TypeF32}),
	instr.I64_TO_F64_S: pushTypeWithCheck([]types.Type{types.TypeI64}, []types.Type{types.TypeF64}),
	instr.I64_TO_F64_U: pushTypeWithCheck([]types.Type{types.TypeI64}, []types.Type{types.TypeF64}),
	instr.F32_CONST:    pushType(types.TypeF32, 4),
	instr.F32_ADD:      pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_SUB:      pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_MUL:      pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_DIV:      pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeF32}),
	instr.F32_EQ:       pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_NE:       pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_LT:       pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_GT:       pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_LE:       pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_GE:       pushTypeWithCheck([]types.Type{types.TypeF32, types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_TO_I32_S: pushTypeWithCheck([]types.Type{types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_TO_I32_U: pushTypeWithCheck([]types.Type{types.TypeF32}, []types.Type{types.TypeI32}),
	instr.F32_TO_I64_S: pushTypeWithCheck([]types.Type{types.TypeF32}, []types.Type{types.TypeI64}),
	instr.F32_TO_I64_U: pushTypeWithCheck([]types.Type{types.TypeF32}, []types.Type{types.TypeI64}),
	instr.F32_TO_F64:   pushTypeWithCheck([]types.Type{types.TypeF32}, []types.Type{types.TypeF64}),
	instr.F64_CONST:    pushType(types.TypeF64, 8),
	instr.F64_ADD:      pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_SUB:      pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_MUL:      pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_DIV:      pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeF64}),
	instr.F64_EQ:       pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_NE:       pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_LT:       pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_GT:       pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_LE:       pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_GE:       pushTypeWithCheck([]types.Type{types.TypeF64, types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_TO_I32_S: pushTypeWithCheck([]types.Type{types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_TO_I32_U: pushTypeWithCheck([]types.Type{types.TypeF64}, []types.Type{types.TypeI32}),
	instr.F64_TO_I64_S: pushTypeWithCheck([]types.Type{types.TypeF64}, []types.Type{types.TypeI64}),
	instr.F64_TO_I64_U: pushTypeWithCheck([]types.Type{types.TypeF64}, []types.Type{types.TypeI64}),
	instr.F64_TO_F32:   pushTypeWithCheck([]types.Type{types.TypeF64}, []types.Type{types.TypeF32}),
}

func pushType(typ types.Type, size int) func(*state) error {
	return func(s *state) error {
		if s.sp == len(s.stack) {
			return ErrStackOverflow
		}
		s.stack[s.sp] = typ
		s.sp++
		s.ip += size + 1
		return nil
	}
}

func pushTypeWithCheck(pop, push []types.Type) func(*state) error {
	return func(s *state) error {
		if s.sp < len(pop) {
			return ErrStackUnderflow
		}

		bp := s.sp - len(pop)
		for i, t := range pop {
			if !s.stack[bp+i].Equals(t) {
				return ErrTypeMismatch
			}
		}
		s.sp = bp

		if s.sp+len(push) > len(s.stack) {
			return ErrStackOverflow
		}
		for _, t := range push {
			s.stack[s.sp] = t
			s.sp++
		}

		s.ip++
		return nil
	}
}

func NewVerifier(prog *program.Program, opts ...Option) *Verifier {
	g := 128
	s := 1024
	for _, opt := range opts {
		if opt.Global > 0 {
			g = opt.Global
		}
		if opt.Stack > 0 {
			s = opt.Stack
		}
	}

	return &Verifier{
		code:      prog.Code,
		constants: prog.Constants,
		global:    g,
		stack:     s,
	}
}

func (v *Verifier) Verify() error {
	fns := []*types.Function{{
		Typ:  &types.FunctionType{},
		Code: v.code,
	}}
	for _, v := range v.constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, fn)
		}
	}

	global := make([]types.Type, 0, v.global)

	for len(fns) > 0 {
		fn := fns[0]
		fns = fns[1:]

		cfg, err := BuildCFG(fn.Code)
		if err != nil || len(cfg.Blocks) == 0 {
			return err
		}

		s := &state{
			fn:        fn,
			block:     0,
			constants: v.constants,
			global:    global,
			stack:     make([]types.Type, v.stack),
		}
		for i := 0; i < fn.Locals; i++ {
			s.stack[i] = types.TypeRef
		}
		for i, t := range fn.Typ.Params {
			s.stack[i] = t
		}
		s.sp += fn.Locals

		visits := make([][][]types.Type, len(cfg.Blocks))
		states := []*state{s}

		for len(states) > 0 {
			s := states[0]
			states = states[1:]

			visited := false
			for _, prev := range visits[s.block] {
				if len(prev) != s.sp {
					continue
				}
				ok := true
				for i := 0; i < len(prev); i++ {
					if !prev[i].Equals(s.stack[i]) {
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
			visits[s.block] = append(visits[s.block], s.stack[:s.sp])

			blk := cfg.Blocks[s.block]
			s.ip = blk.Start

			for s.ip < blk.End {
				opcode := instr.Opcode(fn.Code[s.ip])
				fn := dispatch[opcode]
				if fn == nil {
					return fmt.Errorf("%w: at=%d", ErrUnknownOpcode, s.ip)
				}
				if err := fn(s); err != nil {
					return fmt.Errorf("%w: at=%d", err, s.ip)
				}
			}

			for i := 0; i < len(s.global); i++ {
				if i >= len(global) {
					global = append(global, s.global[i])
					continue
				}
				if s.global[i] != nil {
					if global[i] == nil {
						global[i] = s.global[i]
					} else if !s.global[i].Equals(global[i]) {
						return ErrTypeMismatch
					}
				}
			}

			for _, succ := range blk.Succs {
				states = append(states, &state{
					fn:        fn,
					block:     succ,
					constants: v.constants,
					global:    append([]types.Type(nil), global...),
					stack:     append([]types.Type(nil), s.stack...),
					sp:        s.sp,
				})
			}
		}
	}
	return nil
}
