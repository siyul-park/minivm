package analysis

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

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
	global    []types.Value
	stack     []types.Value
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
		if s.stack[s.sp].Kind() != types.KindI32 {
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
		fn, ok := s.stack[s.sp].(*types.Function)
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
			if fn.Params[idx] != s.stack[s.sp-params+idx].Kind() {
				return ErrTypeMismatch
			}
		}
		s.sp -= params

		for idx := 0; idx < returns; idx++ {
			s.stack[s.sp+idx] = types.Box(0, fn.Returns[idx])
		}
		s.sp += returns

		s.ip++
		return nil
	},
	instr.RETURN: func(s *state) error {
		fn := s.fn
		returns := len(fn.Returns)
		if s.sp < returns {
			return ErrStackUnderflow
		}
		for idx := 0; idx < returns; idx++ {
			if fn.Returns[idx] != s.stack[s.sp-returns+idx].Kind() {
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
				global := make([]types.Value, idx*2)
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
		params := len(fn.Params)
		locals := len(fn.Locals)
		idx := int(int32(binary.BigEndian.Uint32(code[s.ip+1:])))
		if idx < 0 || idx >= params+locals {
			return ErrSegmentationFault
		}

		var val types.Value
		if idx < params {
			val = types.Box(0, fn.Params[idx])
		} else {
			val = types.Box(0, fn.Locals[idx-params])
		}

		s.stack[s.sp] = val
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
		params := len(fn.Params)
		locals := len(fn.Locals)
		idx := int(int32(binary.BigEndian.Uint32(code[s.ip+1:])))
		if idx < 0 || idx >= params+locals {
			return ErrSegmentationFault
		}

		val := s.stack[s.sp-1]
		if idx < params {
			if fn.Params[idx] != val.Kind() {
				return ErrTypeMismatch
			}
		} else {
			if fn.Locals[idx-params] != val.Kind() {
				return ErrTypeMismatch
			}
		}

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
		s.stack[s.sp] = fn
		s.sp++
		s.ip += 5
		return nil
	},
	instr.I32_CONST:    pushTypedConst(types.KindI32, 4),
	instr.I32_ADD:      pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_SUB:      pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_MUL:      pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_DIV_S:    pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_DIV_U:    pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_REM_S:    pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_REM_U:    pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_SHL:      pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_SHR_S:    pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_SHR_U:    pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_XOR:      pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_AND:      pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_OR:       pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_EQ:       pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_NE:       pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_LT_S:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_LT_U:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_GT_S:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_GT_U:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_LE_S:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_LE_U:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_GE_S:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_GE_U:     pushWithTypeCheck([]types.Kind{types.KindI32, types.KindI32}, []types.Kind{types.KindI32}),
	instr.I32_TO_I64_S: pushWithTypeCheck([]types.Kind{types.KindI32}, []types.Kind{types.KindI64}),
	instr.I32_TO_I64_U: pushWithTypeCheck([]types.Kind{types.KindI32}, []types.Kind{types.KindI64}),
	instr.I32_TO_F32_U: pushWithTypeCheck([]types.Kind{types.KindI32}, []types.Kind{types.KindF32}),
	instr.I32_TO_F32_S: pushWithTypeCheck([]types.Kind{types.KindI32}, []types.Kind{types.KindF32}),
	instr.I32_TO_F64_U: pushWithTypeCheck([]types.Kind{types.KindI32}, []types.Kind{types.KindF64}),
	instr.I32_TO_F64_S: pushWithTypeCheck([]types.Kind{types.KindI32}, []types.Kind{types.KindF64}),
	instr.I64_CONST:    pushTypedConst(types.KindI64, 8),
	instr.I64_ADD:      pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_SUB:      pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_MUL:      pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_DIV_S:    pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_DIV_U:    pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_REM_S:    pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_REM_U:    pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_SHL:      pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_SHR_S:    pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_SHR_U:    pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI64}),
	instr.I64_EQ:       pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_NE:       pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_LT_S:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_LT_U:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_GT_S:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_GT_U:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_LE_S:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_LE_U:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_GE_S:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_GE_U:     pushWithTypeCheck([]types.Kind{types.KindI64, types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_TO_I32:   pushWithTypeCheck([]types.Kind{types.KindI64}, []types.Kind{types.KindI32}),
	instr.I64_TO_F32_S: pushWithTypeCheck([]types.Kind{types.KindI64}, []types.Kind{types.KindF32}),
	instr.I64_TO_F32_U: pushWithTypeCheck([]types.Kind{types.KindI64}, []types.Kind{types.KindF32}),
	instr.I64_TO_F64_S: pushWithTypeCheck([]types.Kind{types.KindI64}, []types.Kind{types.KindF64}),
	instr.I64_TO_F64_U: pushWithTypeCheck([]types.Kind{types.KindI64}, []types.Kind{types.KindF64}),
	instr.F32_CONST:    pushTypedConst(types.KindF32, 4),
	instr.F32_ADD:      pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindF32}),
	instr.F32_SUB:      pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindF32}),
	instr.F32_MUL:      pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindF32}),
	instr.F32_DIV:      pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindF32}),
	instr.F32_EQ:       pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_NE:       pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_LT:       pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_GT:       pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_LE:       pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_GE:       pushWithTypeCheck([]types.Kind{types.KindF32, types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_TO_I32_S: pushWithTypeCheck([]types.Kind{types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_TO_I32_U: pushWithTypeCheck([]types.Kind{types.KindF32}, []types.Kind{types.KindI32}),
	instr.F32_TO_I64_S: pushWithTypeCheck([]types.Kind{types.KindF32}, []types.Kind{types.KindI64}),
	instr.F32_TO_I64_U: pushWithTypeCheck([]types.Kind{types.KindF32}, []types.Kind{types.KindI64}),
	instr.F32_TO_F64:   pushWithTypeCheck([]types.Kind{types.KindF32}, []types.Kind{types.KindF64}),
	instr.F64_CONST:    pushTypedConst(types.KindF64, 8),
	instr.F64_ADD:      pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindF64}),
	instr.F64_SUB:      pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindF64}),
	instr.F64_MUL:      pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindF64}),
	instr.F64_DIV:      pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindF64}),
	instr.F64_EQ:       pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_NE:       pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_LT:       pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_GT:       pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_LE:       pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_GE:       pushWithTypeCheck([]types.Kind{types.KindF64, types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_TO_I32_S: pushWithTypeCheck([]types.Kind{types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_TO_I32_U: pushWithTypeCheck([]types.Kind{types.KindF64}, []types.Kind{types.KindI32}),
	instr.F64_TO_I64_S: pushWithTypeCheck([]types.Kind{types.KindF64}, []types.Kind{types.KindI64}),
	instr.F64_TO_I64_U: pushWithTypeCheck([]types.Kind{types.KindF64}, []types.Kind{types.KindI64}),
	instr.F64_TO_F32:   pushWithTypeCheck([]types.Kind{types.KindF64}, []types.Kind{types.KindF32}),
}

func pushTypedConst(kind types.Kind, size int) func(*state) error {
	return func(s *state) error {
		if s.sp == len(s.stack) {
			return ErrStackOverflow
		}
		s.stack[s.sp] = types.Box(0, kind)
		s.sp++
		s.ip += size + 1
		return nil
	}
}

func pushWithTypeCheck(pop, push []types.Kind) func(*state) error {
	return func(s *state) error {
		if s.sp < len(pop) {
			return ErrStackUnderflow
		}

		bp := s.sp - len(pop)
		for i, kind := range pop {
			if s.stack[bp+i].Kind() != kind {
				return ErrTypeMismatch
			}
		}
		s.sp = bp

		if s.sp+len(push) > len(s.stack) {
			return ErrStackOverflow
		}
		for _, kind := range push {
			s.stack[s.sp] = types.Box(0, kind)
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
	fns := []*types.Function{{Code: v.code}}
	for _, v := range v.constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, fn)
		}
	}

	global := make([]types.Value, 0, v.global)

	for len(fns) > 0 {
		fn := fns[0]
		fns = fns[1:]

		cfg, err := BuildCFG(fn.Code)
		if err != nil || len(cfg.Blocks) == 0 {
			return err
		}

		visits := make([][][]types.Value, len(cfg.Blocks))
		states := []*state{{
			fn:        fn,
			block:     0,
			constants: v.constants,
			global:    global,
			stack:     make([]types.Value, v.stack),
		}}

		for len(states) > 0 {
			s := states[0]
			states = states[1:]

			visited := false
			for _, prev := range visits[s.block] {
				if slices.Equal(prev, s.stack) {
					visited = true
					break
				}
			}
			if visited {
				continue
			}
			visits[s.block] = append(visits[s.block], s.stack)

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
				if global[i] == nil && s.global[i] != nil {
					global[i] = s.global[i]
				} else if global[i] != nil && s.global[i] != nil {
					if global[i].Kind() != s.global[i].Kind() { // TODO: Add func type
						return ErrTypeMismatch
					}
					global[i] = s.global[i]
				}
			}

			for _, succ := range blk.Succs {
				states = append(states, &state{
					fn:        fn,
					block:     succ,
					constants: v.constants,
					global:    global,
					stack:     s.stack,
					sp:        s.sp,
				})
			}
		}
	}
	return nil
}
