package transform

import (
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/frontend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// SSAPass rewrites every function a program holds through SSA: it translates
// the function with the same frontend a compile uses, runs the SSA pipeline it
// was built with over the result, and writes the outcome back out as bytecode.
// It is the route by which one implementation of folding, dead-code
// elimination, and common-subexpression elimination serves both an
// ahead-of-time optimizer and a compiler, instead of each keeping its own.
//
// A function it cannot take the whole way round comes back untouched. The
// frontend declines what bytecode alone cannot resolve, the emitter declines
// what SSA cannot be written back as, and either answer leaves the function
// exactly as it was - which is the same bargain transform.GVNPass strikes when
// a rewrite would not fit its encoding, and what docs/coding-patterns.md §7.1
// requires of any pass that moves bytecode offsets.
type SSAPass struct {
	pipeline *pass.Pipeline[*ssa.Function]
}

// pool is a program's constant pool as a compile resolves it: the value each
// CONST_GET pushes, the facts about the cell a reference names, and the slot a
// value has to be interned at when a fold produces one the program does not
// already hold.
//
// A reference here names its own constant slot plus one, because zero is the
// null reference. An ahead-of-time compile has no interpreter behind it and so
// no heap address for a reference to carry, and it needs none: the translation
// only ever hands the identity back to the table it came from. A constant the
// interpreter would put on the heap is one of these - a string, a container, a
// function, and an i64 too wide for its boxed payload alike.
type pool struct {
	prog    *program.Program
	boxed   []types.Boxed
	objects jit.Objects
	at      map[types.Boxed]int
}

var _ pass.Pass[*program.Program] = (*SSAPass)(nil)

// NewSSAPass returns a pass running pipeline over the SSA of every function a
// program holds.
func NewSSAPass(pipeline *pass.Pipeline[*ssa.Function]) *SSAPass {
	return &SSAPass{pipeline: pipeline}
}

func (p *SSAPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
	consts := newPool(prog)

	root := &types.Function{Typ: &types.FunctionType{}, Locals: prog.Locals, Code: prog.Code, Handlers: prog.Handlers}
	changed, err := p.rewrite(m, consts, 0, root)
	if err != nil {
		return pass.PreserveNone(), err
	}
	if changed {
		prog.Code, prog.Locals = root.Code, root.Locals
	}
	// Emitting a folded constant the program does not hold appends one, never
	// a function, so the pool as it stands now is exactly the set of units.
	for i, v := range prog.Constants {
		fn, ok := v.(*types.Function)
		if !ok {
			continue
		}
		// A reference names its own constant slot plus one, the identity pool
		// resolves cells at.
		done, err := p.rewrite(m, consts, i+1, fn)
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

// rewrite takes one function round: to SSA, through the pipeline, and back to
// bytecode. It reports whether fn changed, and leaves fn untouched when either
// direction declines it or the round trip lands on the code it started from.
func (p *SSAPass) rewrite(m *pass.Manager, consts *pool, addr int, fn *types.Function) (bool, error) {
	if !expressible(fn.Code) {
		return false, nil
	}
	f, err := frontend.Body(consts.module(), addr, fn)
	if err != nil || f == nil {
		return false, err
	}
	if _, err := p.pipeline.Run(m, f); err != nil {
		return false, err
	}
	code, added, ok := emit(f, consts, addr == 0, len(fn.Declared()))
	if !ok || (len(added) == 0 && slices.Equal(code, fn.Code)) {
		return false, nil
	}
	fn.Code = code
	if len(added) > 0 {
		fn.Locals = append(slices.Clone(fn.Locals), added...)
	}
	return true, nil
}

func newPool(prog *program.Program) *pool {
	p := &pool{
		prog:    prog,
		boxed:   make([]types.Boxed, len(prog.Constants)),
		objects: jit.Objects{},
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

// module returns the evidence a translation resolves kinds, shapes, and call
// targets against.
func (p *pool) module() frontend.Module {
	return frontend.Module{
		Constants: p.boxed,
		Globals:   types.Kinds(p.prog.Globals),
		Objects:   p.objects,
		Decl:      p.prog.Types,
	}
}

// index returns the constant slot CONST_GET reads c from, interning c when the
// program holds no such value. A reference it does not already hold names a
// cell only a running interpreter could allocate, so no slot can be made for
// one.
func (p *pool) index(c types.Boxed) (int, bool) {
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

// expressible reports whether code holds only operations that survive the
// round trip. UNREACHABLE traps where it stands and the IR gives it no
// operation of its own, so a function holding one would come back without its
// trap.
func expressible(code []byte) bool {
	for ip := 0; ip < len(code); {
		inst := instr.Instruction(code[ip:])
		if inst.Opcode() == instr.UNREACHABLE {
			return false
		}
		ip += inst.Width()
	}
	return true
}

// box returns the compile-time value a constant pushes, and false for one the
// interpreter would allocate a heap cell for instead.
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

// resolved reads off a constant the facts a translation asks of the cell it
// would live in, mirroring what a compile resolves from the running heap.
func resolved(v types.Value) jit.Object {
	switch v := v.(type) {
	case *types.Function:
		return jit.Object{Fn: v}
	case *types.Struct:
		return jit.Object{Typ: v.Typ}
	case types.TypedArray[bool], types.TypedArray[int8], types.TypedArray[int32],
		types.TypedArray[int64], types.TypedArray[float32], types.TypedArray[float64]:
		return jit.Object{Array: jit.Itab(v)}
	default:
		return jit.Object{}
	}
}
