package interp

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type jitCompiler struct {
	emitter   *asm.Emitter
	types     []types.Type
	constants []types.Boxed
	heap      []types.Value
	code      []byte
	params    int
	returns   int
	ip        int
	rp        int
}

func call(addr uintptr)
func call_ret1(addr uintptr) uint64

var jit = [256]func(c *jitCompiler) error{}

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

func (c *jitCompiler) Compile(fn *types.Function) (func(_ *Interpreter), error) {
	params := make([]types.Kind, len(fn.Signature.Params))
	for i, t := range fn.Signature.Params {
		params[i] = t.Kind()
	}
	returns := make([]types.Kind, len(fn.Signature.Returns))
	for i, t := range fn.Signature.Returns {
		returns[i] = t.Kind()
	}

	for _, kind := range params {
		if kind == types.KindRef {
			return nil, ErrTypeMismatch
		}
	}
	for _, kind := range returns {
		if kind == types.KindRef {
			return nil, ErrTypeMismatch
		}
	}

	c.emitter.Reset()

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
	if err := jit[instr.RETURN](c); err != nil {
		return nil, err
	}

	code, err := asm.NewCode(c.emitter.Bytes())
	if err != nil {
		return nil, err
	}

	switch len(returns) {
	case 0:
		switch len(params) {
		case 0:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				call(uintptr(code.Ptr()))
				i.sp = f.bp
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 1:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				(*(*func(uint64))(code.Ptr()))(p0)
				i.sp = f.bp
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 2:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				p1 := i.unbox64(i.stack[f.bp+1])
				(*(*func(uint64, uint64))(code.Ptr()))(p0, p1)
				i.sp = f.bp
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 3:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				p1 := i.unbox64(i.stack[f.bp+1])
				p2 := i.unbox64(i.stack[f.bp+2])
				(*(*func(uint64, uint64, uint64))(code.Ptr()))(p0, p1, p2)
				i.sp = f.bp
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		default:
			return nil, ErrTypeMismatch
		}
	case 1:
		switch fn.Params {
		case 0:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				r0 := call_ret1(uintptr(code.Ptr()))
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 1:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				r0 := (*(*func(uint64) uint64)(code.Ptr()))(p0)
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 2:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				p1 := i.unbox64(i.stack[f.bp+1])
				r0 := (*(*func(uint64, uint64) uint64)(code.Ptr()))(p0, p1)
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 3:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				p1 := i.unbox64(i.stack[f.bp+1])
				p2 := i.unbox64(i.stack[f.bp+2])
				r0 := (*(*func(uint64, uint64, uint64) uint64)(code.Ptr()))(p0, p1, p2)
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		default:
			return nil, ErrTypeMismatch
		}
	case 2:
		switch fn.Params {
		case 0:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				r0, r1 := (*(*func() (uint64, uint64))(code.Ptr()))()
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.stack[f.bp+1] = i.box64(r1, returns[1])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 1:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				r0, r1 := (*(*func(uint64) (uint64, uint64))(code.Ptr()))(p0)
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.stack[f.bp+1] = i.box64(r1, returns[1])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 2:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				p1 := i.unbox64(i.stack[f.bp+1])
				r0, r1 := (*(*func(uint64, uint64) (uint64, uint64))(code.Ptr()))(p0, p1)
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.stack[f.bp+1] = i.box64(r1, returns[1])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		case 3:
			return func(i *Interpreter) {
				f := &i.frames[i.fp-1]
				p0 := i.unbox64(i.stack[f.bp])
				p1 := i.unbox64(i.stack[f.bp+1])
				p2 := i.unbox64(i.stack[f.bp+2])
				r0, r1 := (*(*func(uint64, uint64, uint64) (uint64, uint64))(code.Ptr()))(p0, p1, p2)
				i.stack[f.bp] = i.box64(r0, returns[0])
				i.stack[f.bp+1] = i.box64(r1, returns[1])
				i.sp = f.bp + 1
				i.release(f.addr)
				f.code = nil
				i.fp--
			}, nil
		default:
			return nil, ErrTypeMismatch
		}
	default:
		return nil, ErrTypeMismatch
	}
}
