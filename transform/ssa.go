package transform

import (
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// SSAPass translates functions to SSA, runs a pipeline, and emits bytecode.
type SSAPass struct {
	pipeline *pass.Pipeline[*ssa.Function]
}

type pool struct {
	program *program.Program
	boxed   []types.Boxed
	objects Objects
	at      map[types.Boxed]int
}

var _ pass.Pass[*program.Program] = (*SSAPass)(nil)

// NewSSAPass returns an SSA round-trip pass using pipeline.
func NewSSAPass(pipeline *pass.Pipeline[*ssa.Function]) *SSAPass {
	return &SSAPass{pipeline: pipeline}
}

// Run applies the SSA round trip to a program.
func (p *SSAPass) Run(manager *pass.Manager, program *program.Program) (pass.Preserved, error) {
	constants := newPool(program)

	root := &types.Function{Typ: &types.FunctionType{}, Locals: program.Locals, Code: program.Code, Handlers: program.Handlers}
	changed, err := p.roundtrip(manager, constants, 0, root)
	if err != nil {
		return pass.PreserveNone(), err
	}
	if changed {
		program.Code, program.Locals = root.Code, root.Locals
	}
	for i, v := range program.Constants {
		function, ok := v.(*types.Function)
		if !ok {
			continue
		}
		done, err := p.roundtrip(manager, constants, i+1, function)
		if err != nil {
			return pass.PreserveNone(), err
		}
		changed = changed || done
	}

	if !changed {
		return pass.PreserveAll(), nil
	}
	return pass.PreserveNone(), nil
}

func (p *SSAPass) roundtrip(manager *pass.Manager, constants *pool, address int, function *types.Function) (bool, error) {
	if !isExpressible(function.Code) {
		return false, nil
	}
	f, err := Translate(constants.module(), address, function, 0)
	if err != nil || f == nil {
		return false, err
	}
	if _, err := p.pipeline.Run(manager, f); err != nil {
		return false, err
	}
	code, added, ok := emit(f, constants, address == 0, len(function.Declared()))
	if !ok || (len(added) == 0 && slices.Equal(code, function.Code)) {
		return false, nil
	}
	function.Code = code
	if len(added) > 0 {
		function.Locals = append(slices.Clone(function.Locals), added...)
	}
	return true, nil
}

func newPool(program *program.Program) *pool {
	p := &pool{
		program: program,
		boxed:   make([]types.Boxed, len(program.Constants)),
		objects: Objects{},
		at:      map[types.Boxed]int{},
	}
	for i, v := range program.Constants {
		boxed, ok := box(v)
		if !ok {
			boxed = types.BoxRef(i + 1)
			p.objects[i+1] = resolveObject(v)
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
		Globals:   types.Kinds(p.program.Globals),
		Objects:   p.objects,
		Types:     p.program.Types,
	}
}

func (p *pool) intern(c types.Boxed) (int, bool) {
	if at, ok := p.at[c]; ok {
		return at, true
	}
	if c.Kind() == types.KindRef {
		return 0, false
	}
	at := len(p.program.Constants)
	p.program.Constants = append(p.program.Constants, types.Unbox(c))
	p.boxed = append(p.boxed, c)
	p.at[c] = at
	return at, true
}

func isExpressible(code []byte) bool {
	for ip := 0; ip < len(code); {
		inst := instr.Instruction(code[ip:])
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

func resolveObject(v types.Value) Object {
	switch v := v.(type) {
	case *types.Function:
		return Object{Function: v}
	case *types.Struct:
		return Object{Type: v.Typ}
	default:
		return Object{}
	}
}
