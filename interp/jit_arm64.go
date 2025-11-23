//go:build arm64

package interp

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

var jit = [256]func(c *jitCompiler) ([]byte, error){
	instr.I32_CONST: func(c *jitCompiler) ([]byte, error) {
		val := types.BoxI32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 5

		reg := c.sp
		c.sp++

		imm0 := uint16(val & 0xFFFF)
		imm1 := uint16((val >> 16) & 0xFFFF)

		code := make([]byte, 0, 8)
		code = append(code, arm64.MOVZ(reg, imm0, 0)...)
		if imm1 != 0 {
			code = append(code, arm64.MOVK(reg, imm1, 16)...)
		}
		return code, nil
	},
	instr.RETURN: func(c *jitCompiler) ([]byte, error) {
		c.ip++

		code := make([]byte, 0, 16)
		for i := 0; i < c.returns && i <= c.sp; i++ {
			src := c.sp - c.returns + i
			if src != i {
				code = append(code, arm64.ORRW(i, src)...)
			}
		}
		code = append(code, arm64.RET()...)
		return code, nil
	},
}

func init() {
	unknown := func(_ *jitCompiler) ([]byte, error) {
		return nil, ErrUnknownOpcode
	}
	for i, fn := range jit {
		if fn == nil {
			jit[i] = unknown
		}
	}
}

func (f *CompiledFunction) Run(_ *Interpreter, _ []types.Boxed) ([]types.Boxed, error) {
	switch len(f.Returns) {
	case 0:
		return nil, nil
	case 1:
		fn := *(*func() uint64)(f.Code.Ptr())
		ret := fn()
		switch f.Returns[0] {
		case types.KindI32:
			return []types.Boxed{types.BoxI32(int32(ret))}, nil
		case types.KindI64:
			return []types.Boxed{types.BoxI64(int64(ret))}, nil
		default:
			return nil, ErrTypeMismatch
		}
	case 2:
		fn := *(*func() (uint64, uint64))(f.Code.Ptr())
		r1, r2 := fn()
		results := make([]types.Boxed, 2)
		switch f.Returns[0] {
		case types.KindI32:
			results[0] = types.BoxI32(int32(r1))
		case types.KindI64:
			results[0] = types.BoxI64(int64(r1))
		default:
			return nil, ErrTypeMismatch
		}
		switch f.Returns[1] {
		case types.KindI32:
			results[1] = types.BoxI32(int32(r2))
		case types.KindI64:
			results[1] = types.BoxI64(int64(r2))
		default:
			return nil, ErrTypeMismatch
		}
		return results, nil
	default:
		return nil, ErrTypeMismatch
	}
}

func (c *jitCompiler) Compile(fn *types.Function) (*CompiledFunction, error) {
	c.code = fn.Code
	c.params = fn.Params
	c.returns = fn.Returns
	c.ip = 0
	c.sp = 0

	emitter := asm.NewEmitter()
	for c.ip < len(c.code) {
		code, err := jit[c.code[c.ip]](c)
		if err != nil {
			return nil, err
		}
		emitter.Emit(code)
	}

	code, err := asm.NewCode(emitter.Bytes())
	if err != nil {
		return nil, err
	}
	return NewCompiledFunction(fn.Signature, code), nil
}
