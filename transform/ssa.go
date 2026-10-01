package transform

import (
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// SSAPass translates, optimizes, and re-emits expressible functions.
type SSAPass struct {
	pipeline *pass.Pipeline[*ssa.Function]
}

type pool struct {
	prog    *program.Program
	boxed   []types.Boxed
	objects Objects
	at      map[types.Boxed]int
}

var _ pass.Pass[*program.Program] = (*SSAPass)(nil)

// NewSSAPass returns an SSA round-trip pass.
func NewSSAPass(pipeline *pass.Pipeline[*ssa.Function]) *SSAPass {
	return &SSAPass{pipeline: pipeline}
}

// Run applies the SSA round trip.
func (p *SSAPass) Run(_ *pass.Manager, prog *program.Program) (bool, error) {
	constants := newPool(prog)
	// The inner manager caches per-function analyses; the outer manager's
	// unit is the whole program, a different cache key space.
	manager := pass.NewManager()

	root := &types.Function{Typ: &types.FunctionType{}, Locals: prog.Locals, Code: prog.Code, Handlers: prog.Handlers}
	changed, err := p.roundtrip(manager, constants, 0, root)
	if err != nil {
		return false, err
	}
	if changed {
		prog.Code, prog.Locals = root.Code, root.Locals
	}
	for i, v := range prog.Constants {
		function, ok := v.(*types.Function)
		if !ok {
			continue
		}
		done, err := p.roundtrip(manager, constants, i+1, function)
		if err != nil {
			return false, err
		}
		changed = changed || done
	}

	if !changed {
		return true, nil
	}
	return false, nil
}

func (p *SSAPass) roundtrip(manager *pass.Manager, constants *pool, address int, function *types.Function) (bool, error) {
	if !expressible(function) {
		return false, nil
	}
	f, err := Translate(constants.module(), address, function, 0)
	if err != nil || f == nil {
		return false, err
	}
	if _, err := p.pipeline.Run(manager, f); err != nil {
		return false, err
	}
	code, locals, ok := emit(f, constants, address == 0, len(function.Declared()))
	if !ok || (len(locals) == 0 && slices.Equal(code, function.Code)) {
		return false, nil
	}
	function.Code = code
	if len(locals) > 0 {
		function.Locals = append(slices.Clone(function.Locals), locals...)
	}
	return true, nil
}

func newPool(prog *program.Program) *pool {
	p := &pool{
		prog:    prog,
		boxed:   make([]types.Boxed, len(prog.Constants)),
		objects: Objects{},
		at:      map[types.Boxed]int{},
	}
	for i, v := range prog.Constants {
		boxed, ok := box(v)
		if !ok {
			boxed = types.BoxRef(i + 1)
			p.objects[i+1] = resolved(v)
		}
		p.boxed[i] = boxed
		if _, seen := p.at[boxed]; !seen {
			p.at[boxed] = i
		}
	}
	return p
}

func (p *pool) module() Module {
	return Module{
		Constants: p.boxed,
		Globals:   types.Kinds(p.prog.Globals),
		Objects:   p.objects,
		Types:     p.prog.Types,
	}
}

func (p *pool) intern(c types.Boxed) (int, bool) {
	if at, ok := p.at[c]; ok {
		return at, true
	}
	if c.Kind() == types.KindRef {
		return 0, false
	}
	at := len(p.prog.Constants)
	p.prog.Constants = append(p.prog.Constants, types.Unbox(c))
	p.boxed = append(p.boxed, c)
	p.at[c] = at
	return at, true
}

// expressible reports whether emit can rewrite function: its handler table
// holds byte offsets that a rewrite would invalidate.
func expressible(function *types.Function) bool {
	if len(function.Handlers) > 0 {
		return false
	}
	for ip := 0; ip < len(function.Code); {
		inst := instr.Instruction(function.Code[ip:])
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.YIELD, instr.RESUME:
			return false
		}
		ip += inst.Width()
	}
	return true
}

func box(v types.Value) (types.Boxed, bool) {
	switch v := v.(type) {
	case types.Boxed:
		return v, v.Kind() != types.KindRef
	case types.I1:
		return types.BoxI1(bool(v)), true
	case types.I8:
		return types.BoxI8(int8(v)), true
	case types.I32:
		return types.BoxI32(int32(v)), true
	case types.I64:
		return types.BoxI64(int64(v)), types.IsBoxable(int64(v))
	case types.F32:
		return types.BoxF32(float32(v)), true
	case types.F64:
		return types.BoxF64(float64(v)), true
	default:
		return 0, false
	}
}

func resolved(v types.Value) Object {
	switch v := v.(type) {
	case *types.Function:
		return Object{Function: v}
	case *types.Struct:
		return Object{Struct: v.Typ}
	case types.I64:
		return Object{I64: &v}
	default:
		if at, ok := v.Type().(*types.ArrayType); ok {
			return Object{Array: at}
		}
		return Object{}
	}
}
