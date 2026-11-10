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
	inline   bool
}

// SSAOption configures an SSAPass.
type SSAOption func(*SSAPass)

type pool struct {
	prog    *program.Program
	boxed   []types.Boxed
	objects Objects
	at      map[types.Boxed]int
}

// change is a function rewritten by a round trip; a nil function is the
// module body.
type change struct {
	function      *types.Function
	before, after original
}

// original is the code and locals of a function before a round trip.
type original struct {
	code   []byte
	locals []types.Type
}

var _ pass.Pass[*program.Program] = (*SSAPass)(nil)

// WithInline inlines, ahead of the states' removal, each call of a pure
// scalar leaf that cannot trap (see InlinePass).
func WithInline() SSAOption {
	return func(p *SSAPass) { p.inline = true }
}

// NewSSAPass returns an SSA round-trip pass. Each function first has its deopt
// states stripped, which emission never reads, then runs pipeline.
func NewSSAPass(pipeline *pass.Pipeline[*ssa.Function], opts ...SSAOption) *SSAPass {
	p := &SSAPass{pipeline: pipeline}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run applies the SSA round trip. The program is verified once afterwards.
// On a fault every changed function is restored and the program verified
// again: if it still fails the input was invalid and comes back unchanged.
// Otherwise each changed function is reapplied alone and kept only if the
// program still verifies, so a function whose emitted code would fail
// verification is left as it was.
func (p *SSAPass) Run(_ *pass.Manager, prog *program.Program) (bool, error) {
	constants := newPool(prog)
	size := len(prog.Constants)

	manager := pass.NewManager()

	var changes []change
	root := &types.Function{Typ: &types.FunctionType{}, Locals: prog.Locals, Code: prog.Code, Handlers: prog.Handlers}
	before := original{root.Code, root.Locals}
	changed, err := p.roundtrip(manager, constants, 0, root)
	if err != nil {
		return false, err
	}
	if changed {
		prog.Code, prog.Locals = root.Code, root.Locals
		changes = append(changes, change{before: before, after: original{root.Code, root.Locals}})
	}
	seen := map[*types.Function]bool{}
	for i, v := range prog.Constants {
		function, ok := v.(*types.Function)
		if !ok || seen[function] {
			continue
		}
		seen[function] = true
		before := original{function.Code, function.Locals}
		done, err := p.roundtrip(manager, constants, i+1, function)
		if err != nil {
			return false, err
		}
		if done {
			changes = append(changes, change{function: function, before: before, after: original{function.Code, function.Locals}})
		}
	}

	kept := len(changes)
	if kept > 0 && program.Verify(prog) != nil {
		for _, c := range changes {
			c.use(prog, c.before)
		}
		kept = 0
		if program.Verify(prog) == nil {
			for _, c := range changes {
				c.use(prog, c.after)
				if program.Verify(prog) != nil {
					c.use(prog, c.before)
					continue
				}
				kept++
			}
		}
	}
	if kept == 0 {
		prog.Constants = prog.Constants[:size]
	}
	return kept == 0, nil
}

// use sets the code and locals of the changed function.
func (c change) use(prog *program.Program, o original) {
	if c.function == nil {
		prog.Code, prog.Locals = o.code, o.locals
		return
	}
	c.function.Code, c.function.Locals = o.code, o.locals
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

func (p *SSAPass) roundtrip(manager *pass.Manager, constants *pool, address int, function *types.Function) (bool, error) {
	if !expressible(function) {
		return false, nil
	}
	f, _, err := Translate(constants.module(), nil, address, function, 0)
	if err != nil || f == nil {
		return false, err
	}
	if p.inline {
		if _, err := (&InlinePass{module: constants.module(), quiet: true}).Run(manager, f); err != nil {
			return false, err
		}
	}
	if _, err := NewStripPass().Run(manager, f); err != nil {
		return false, err
	}
	if _, err := p.pipeline.Run(manager, f); err != nil {
		return false, err
	}
	code, locals, ok := emit(f, constants, address == 0, function)
	if !ok || (len(locals) == 0 && slices.Equal(code, function.Code)) {
		return false, nil
	}
	function.Code = code
	if len(locals) > 0 {
		function.Locals = append(slices.Clone(function.Locals), locals...)
	}
	return true, nil
}

func (p *pool) module() Module {
	return Module{
		Constants: p.boxed,
		Globals:   types.Kinds(p.prog.Globals),
		Objects:   p.objects,
		Types:     p.prog.Types,
	}
}

// typeOf returns the type of the object the ref cell c names, or nil when the
// pool knows none.
func (p *pool) typeOf(c types.Boxed) types.Type {
	if c == types.BoxedNull || c.Kind() != types.KindRef {
		return nil
	}
	object := p.objects[c.Ref()]
	switch {
	case object.Array != nil:
		return object.Array
	case object.Struct != nil:
		return object.Struct
	case object.Function != nil && object.Function.Typ != nil:
		return object.Function.Typ
	default:
		return nil
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
// holds byte offsets that a rewrite would invalidate, and a RETURN_CALL ends
// in a frame reuse that only native execution lowers.
func expressible(function *types.Function) bool {
	if len(function.Handlers) > 0 {
		return false
	}
	for ip := 0; ip < len(function.Code); {
		inst := instr.Instruction(function.Code[ip:])
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.YIELD, instr.RESUME, instr.RETURN_CALL:
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
