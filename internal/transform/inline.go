package transform

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// InlinePass replaces a CALL of a statically resolved pure scalar leaf (see
// leaf) with the callee's body. Every operation of the body takes the CALL's
// own deopt state, so a deopt resumes the interpreter at the CALL, which runs
// the callee again: a leaf writes nothing, so the rerun is unobservable.
// Lowering must therefore give no operation of the body a bridge, which
// serves the opcode at its state's IP (compile.ErrInlined).
type InlinePass struct {
	module Module
	// trapless admits only a body that cannot trap: bytecode emission drops
	// the states, so a trap inside the body would be reported in the caller's
	// frame. The JIT keeps them and leaves it false.
	trapless bool
}

// sizeMax caps the bytecode instructions of an inlined callee.
const sizeMax = 32

var _ pass.Pass[*ssa.Function] = (*InlinePass)(nil)

// NewInlinePass returns the pass, resolving callees through module.
func NewInlinePass(module Module) *InlinePass {
	return &InlinePass{module: module}
}

// Run applies the pass to one SSA function.
func (p *InlinePass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	consts := map[ssa.Value]uint64{}
	for b := range function.Len() {
		for _, operation := range function.Block(b).Operations {
			if operation.Op == ssa.OpConst {
				consts[operation.Results[0]] = operation.Const
			}
		}
	}
	bodies := map[int]*ssa.Function{}
	callee := func(operation ssa.Operation) *ssa.Function {
		if operation.Op != ssa.OpExec || operation.Code != instr.CALL || operation.Shape != (ssa.Shape{}) ||
			operation.State == ssa.NoValue || len(operation.Args) == 0 {
			return nil
		}
		word, ok := consts[operation.Args[len(operation.Args)-1]]
		if !ok || types.Boxed(word).Kind() != types.KindRef {
			return nil
		}
		ref := types.Boxed(word).Ref()
		target := p.module.Objects.function(ref)
		if target == nil || target.Typ == nil || len(target.Typ.Params) != len(operation.Args)-1 ||
			len(target.Typ.Returns) != len(operation.Results) {
			return nil
		}
		body, seen := bodies[ref]
		if !seen {
			body = p.body(ref, target)
			bodies[ref] = body
		}
		if body == nil {
			return nil
		}
		for i, param := range target.Typ.Params {
			if function.Type(operation.Args[i]) != ssa.TypeOf(param.Kind()) {
				return nil
			}
		}
		for i, ret := range target.Typ.Returns {
			if function.Type(operation.Results[i]) != ssa.TypeOf(ret.Kind()) {
				return nil
			}
		}
		return body
	}

	found := false
	for b := range function.Len() {
		for _, operation := range function.Block(b).Operations {
			found = found || callee(operation) != nil
		}
	}
	if !found {
		return true, nil
	}

	r := newRebuilder(function)
	for _, block := range graph.Order(function) {
		id := r.open(block)
		b := function.Block(block)
		for _, operation := range b.Operations {
			if body := callee(operation); body != nil {
				id = r.splice(id, operation, body)
				continue
			}
			r.builder.Add(id, r.define(r.operation(operation)))
		}
		r.builder.Term(id, r.terminator(b.Terminator))
	}
	*function = *r.builder.Build()
	return false, nil
}

// body translates target, the function at ref, with its locals promoted to
// SSA values; nil when target is not a leaf.
func (p *InlinePass) body(ref int, target *types.Function) *ssa.Function {
	if !shaped(target) {
		return nil
	}
	body, _, err := Translate(p.module, nil, ref, target, 0)
	if err != nil || body == nil {
		return nil
	}
	if _, err := NewPromotePass().Run(nil, body); err != nil {
		return nil
	}
	if !leaf(target, body) || p.trapless && traps(body) {
		return nil
	}
	return body
}

// traps reports whether body runs an operation that may trap: a division or
// remainder whose divisor is not a nonzero constant.
func traps(body *ssa.Function) bool {
	consts := map[ssa.Value]uint64{}
	for b := range body.Len() {
		for _, operation := range body.Block(b).Operations {
			if operation.Op == ssa.OpConst {
				consts[operation.Results[0]] = operation.Const
			}
		}
	}
	for b := range body.Len() {
		for _, operation := range body.Block(b).Operations {
			if operation.Op != ssa.OpExec || !operation.Code.Traps() {
				continue
			}
			if len(operation.Args) != 2 {
				return true
			}
			if word, ok := consts[operation.Args[1]]; !ok || uint32(word) == 0 {
				return true
			}
		}
	}
	return false
}

// leaf reports whether a call of target can be replaced by body, its
// translation with locals promoted: a pure scalar leaf. It runs no call, no
// handler, and no write or allocation; its params, locals and results are
// scalars (i64 and refs excluded); it has at most sizeMax instructions and no
// backward branch. body holds only constants, reads of a local's entry value,
// and pure operations on scalars, and ends in jumps, branches, tables and
// returns.
func leaf(target *types.Function, body *ssa.Function) bool {
	if !shaped(target) {
		return false
	}
	slots := target.Declared()
	for b := range body.Len() {
		block := body.Block(b)
		for _, param := range block.Params {
			if !scalar(body.Type(param)) {
				return false
			}
		}
		for _, operation := range block.Operations {
			switch operation.Op {
			case ssa.OpState:
				continue
			case ssa.OpConst:
			case ssa.OpLoad:
				s := operation.Slot
				if s.Space != ssa.SpaceLocal || s.Base != 0 || s.Index >= len(slots) ||
					body.Type(operation.Results[0]) != ssa.TypeOf(slots[s.Index].Kind()) {
					return false
				}
			case ssa.OpExec:
				if !operation.Code.IsPure() {
					return false
				}
			default:
				return false
			}
			for _, result := range operation.Results {
				if !scalar(body.Type(result)) {
					return false
				}
			}
		}
		switch block.Terminator.Op {
		case ssa.OpJump, ssa.OpBranch, ssa.OpTable:
		case ssa.OpReturn:
			if len(block.Terminator.Args) != len(target.Typ.Returns) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// shaped reports whether target's header allows inlining: a plain function
// of at most sizeMax instructions whose params, locals and results are
// scalars, and that never branches backward.
func shaped(target *types.Function) bool {
	if len(target.Code) == 0 || len(target.Captures) > 0 || len(target.Handlers) > 0 {
		return false
	}
	for _, t := range target.Declared() {
		if !scalar(ssa.TypeOf(t.Kind())) {
			return false
		}
	}
	for _, t := range target.Typ.Returns {
		if !scalar(ssa.TypeOf(t.Kind())) {
			return false
		}
	}
	count := 0
	for ip := 0; ip < len(target.Code); count++ {
		for _, to := range instr.Targets(target.Code, ip) {
			if to <= ip {
				return false
			}
		}
		ip += instr.Instruction(target.Code[ip:]).Width()
	}
	return count <= sizeMax
}

func scalar(t ssa.Type) bool {
	switch t {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32, ssa.TypeF32, ssa.TypeF64:
		return true
	default:
		return false
	}
}
