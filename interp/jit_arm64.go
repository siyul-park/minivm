//go:build arm64

package interp

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type CompiledFunction struct {
	Signature *types.FunctionSignature
	Params    []types.Kind
	Returns   []types.Kind
	Code      *asm.Code
}

type jitCompiler struct {
	emitter   *asm.Emitter
	types     []types.Type
	constants []types.Boxed
	code      []byte
	params    int
	returns   int
	ip        int
	rp        int
}

const (
	regBase  = arm64.X8
	regLimit = arm64.X15
	regCount = regLimit - regBase + 1
)

var _ types.Value = (*CompiledFunction)(nil)

var jit = [256]func(c *jitCompiler) error{
	instr.I32_CONST: func(c *jitCompiler) error {
		val := types.BoxI32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
		c.ip += 5

		imm0 := uint16(val & 0xFFFF)
		imm1 := uint16((val >> 16) & 0xFFFF)

		if c.rp <= regLimit {
			c.emitter.Emit32(arm64.MOVZ(c.rp, imm0, 0))
			if imm1 != 0 {
				c.emitter.Emit32(arm64.MOVK(c.rp, imm1, 16))
			}
		} else {
			spill := regBase + ((c.rp - regBase) % regCount)
			c.emitter.Emit32(arm64.STR(spill, arm64.SP, 0))
			c.emitter.Emit32(arm64.ADDI(arm64.SP, arm64.SP, 8))
		}
		c.rp++
		return nil
	},
	instr.RETURN: func(c *jitCompiler) error {
		c.ip++

		reg := regCount
		if c.returns < regCount {
			reg = c.returns
		}

		for i := 0; i < reg; i++ {
			src := c.rp - c.returns + i
			if src >= 0 {
				if src != i {
					c.emitter.Emit32(arm64.ORRW(i, src))
				}
			} else {
				offset := (i - c.returns) * 8
				c.emitter.Emit32(arm64.LDR(i, arm64.SP, offset))
			}
		}
		for i := reg; i < c.returns; i++ {
			offset := (i - reg) * 8
			c.emitter.Emit32(arm64.LDR(i, arm64.SP, offset))
		}
		if c.rp > regLimit {
			spillCount := c.rp - regLimit
			c.emitter.Emit32(arm64.ADDI(arm64.SP, arm64.SP, -8*spillCount))
		}

		c.emitter.Emit32(arm64.RET())
		return nil
	},
}

func init() {
	unknown := func(_ *jitCompiler) error {
		return ErrUnknownOpcode
	}
	for i, fn := range jit {
		if fn == nil {
			jit[i] = unknown
		}
	}
}

func NewCompiledFunction(signature *types.FunctionSignature, code *asm.Code) *CompiledFunction {
	fn := &CompiledFunction{
		Signature: signature,
		Code:      code,
	}
	fn.Params = make([]types.Kind, len(signature.Params))
	for i, t := range signature.Params {
		fn.Params[i] = t.Kind()
	}
	fn.Returns = make([]types.Kind, len(signature.Returns))
	for i, t := range signature.Returns {
		fn.Returns[i] = t.Kind()
	}
	return fn
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

func (f *CompiledFunction) Kind() types.Kind {
	return types.KindRef
}

func (f *CompiledFunction) Type() types.Type {
	return f.Signature.Type()
}

func (f *CompiledFunction) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n", f.Signature.String()))
	sb.WriteString("<compiled>")
	return sb.String()
}

func (f *CompiledFunction) Close() error {
	return f.Code.Close()
}

func (c *jitCompiler) Compile(fn *types.Function) (*CompiledFunction, error) {
	c.emitter = asm.NewEmitter()
	c.code = fn.Code
	c.params = fn.Params
	c.returns = fn.Returns
	c.ip = 0
	c.rp = regBase

	for c.ip < len(c.code) {
		if err := jit[c.code[c.ip]](c); err != nil {
			return nil, err
		}
	}

	code, err := asm.NewCode(c.emitter.Bytes())
	if err != nil {
		return nil, err
	}
	return NewCompiledFunction(fn.Signature, code), nil
}
