package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// opStack is the abstract operand stack threaded through blockHeights' dataflow.
// Each entry tracks only what CALL/RETURN_CALL need to resolve their arity
// statically: the callee signature when the slot is known to hold a function
// or closure value, or nil for every other value. Unlike program/verify.go's
// checker, it never tracks operand kinds; blockHeights only proves height.
type opStack struct {
	entries []*types.FunctionType
}

// blockHeights computes each basic block's entry operand-stack height via a
// forward fixpoint over blocks. constants and heap are an Interpreter's own
// fields (see interp.go's New): constants holds each CONST_GET slot's boxed
// value, and heap resolves a ref constant to its *types.Function so CONST_GET
// of a function constant can push that function's signature, the same way
// program/verify.go's checker resolves CONST_GET via program.Program.Constants.
// Returns ok=false when any reachable block's stack effect cannot be
// statically resolved, two predecessors disagree on a block's entry height,
// or a block is unreachable from entry.
//
// Exception handlers are rejected outright: a catch block is entered out of
// band with a stack height the CFG edges alone cannot express (program/verify.go's
// flow pass seeds each catch block as its own root), and replicating that
// seeding is unnecessary complexity for what Phase 1 needs.
func blockHeights(fn *types.Function, blocks []*analysis.BasicBlock, constants []types.Boxed, heap []types.Value) ([]int, bool) {
	if len(fn.Handlers) > 0 {
		return nil, false
	}
	if len(blocks) == 0 {
		return nil, true
	}

	locals := localTypes(fn)
	entries := make([]*opStack, len(blocks))
	entries[0] = &opStack{}
	work := []int{0}

	for len(work) > 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]

		st := entries[i].clone()
		if !stackEffect(fn, locals, constants, heap, blocks[i], st) {
			return nil, false
		}
		for _, s := range blocks[i].Succs {
			if entries[s] == nil {
				entries[s] = st.clone()
				work = append(work, s)
				continue
			}
			changed, agree := entries[s].merge(st)
			if !agree {
				return nil, false
			}
			if changed {
				work = append(work, s)
			}
		}
	}

	heights := make([]int, len(blocks))
	for i, e := range entries {
		if e == nil {
			return nil, false
		}
		heights[i] = e.len()
	}
	return heights, true
}

// stackEffect applies every instruction of b to st in place, reporting false
// as soon as one instruction's height effect cannot be statically resolved.
func stackEffect(fn *types.Function, locals []types.Type, constants []types.Boxed, heap []types.Value, b *analysis.BasicBlock, st *opStack) bool {
	for ip := b.Start; ip < b.End; {
		inst := instr.Instruction(fn.Code[ip:])
		if !applyStackEffect(fn, locals, constants, heap, st, inst, inst.Opcode()) {
			return false
		}
		ip += inst.Width()
	}
	return true
}

// applyStackEffect applies one instruction's height effect to st, mirroring
// program/verify.go's checker.step but tracking only height plus the narrow
// function-signature tag call needs. It is named apart from interp's own
// trace step type. Opcodes with a fixed instr.Type pop/push count fall
// through to the generic case; the rest need program context (declared
// local/capture types, the callee signature) or have a runtime-determined
// arity that blockHeights cannot resolve.
func applyStackEffect(fn *types.Function, locals []types.Type, constants []types.Boxed, heap []types.Value, st *opStack, inst instr.Instruction, op instr.Opcode) bool {
	switch op {
	case instr.NOP, instr.UNREACHABLE, instr.BR:
		return true
	case instr.LOCAL_TEE, instr.GLOBAL_TEE:
		return st.len() > 0
	case instr.LOCAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(locals) {
			return false
		}
		st.push(funcSignature(locals[idx]))
		return true
	case instr.UPVAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(fn.Captures) {
			return false
		}
		st.push(funcSignature(fn.Captures[idx]))
		return true
	case instr.CONST_GET:
		idx := int(inst.Operand(0))
		if idx >= len(constants) {
			return false
		}
		st.push(constFuncSignature(constants[idx], heap))
		return true
	case instr.DUP:
		if st.len() == 0 {
			return false
		}
		st.push(st.top())
		return true
	case instr.SWAP:
		if st.len() < 2 {
			return false
		}
		st.swap()
		return true
	case instr.SELECT:
		if st.len() < 3 {
			return false
		}
		st.drop(3)
		st.push(nil)
		return true
	case instr.CALL:
		return call(st, false)
	case instr.RETURN_CALL:
		return call(st, true)
	case instr.RETURN:
		returns := 0
		if fn.Typ != nil {
			returns = len(fn.Typ.Returns)
		}
		return st.len() >= returns
	case instr.STRUCT_NEW, instr.MAP_NEW, instr.CLOSURE_NEW:
		// STRUCT_NEW's field count needs the declared type from
		// program.Program.Types, which blockHeights has no access to;
		// MAP_NEW and ARRAY_APPEND-style ops read their count off the
		// runtime stack, so no static height exists to resolve.
		return false
	}

	t := inst.Type()
	if t.Pop == nil && t.Push == nil {
		return false
	}
	if st.len() < len(t.Pop) {
		return false
	}
	st.drop(len(t.Pop))
	for range t.Push {
		st.push(nil)
	}
	return true
}

// call resolves CALL/RETURN_CALL's height effect from the callee signature
// recorded on the top slot. Only LOCAL_GET/UPVAL_GET of a statically
// FunctionType-declared local or capture leaves that signature behind; a
// callee loaded any other way (CONST_GET, a merged value, a computed value)
// is indeterminate.
func call(st *opStack, tail bool) bool {
	if st.len() == 0 {
		return false
	}
	sig := st.pop()
	if sig == nil {
		return false
	}
	if st.len() < len(sig.Params) {
		return false
	}
	st.drop(len(sig.Params))
	if tail {
		return true
	}
	for range sig.Returns {
		st.push(nil)
	}
	return true
}

// localTypes returns fn's LOCAL_GET-addressable types in operand-index order:
// its declared params first, then its declared locals.
func localTypes(fn *types.Function) []types.Type {
	var out []types.Type
	if fn.Typ != nil {
		out = append(out, fn.Typ.Params...)
	}
	return append(out, fn.Locals...)
}

// funcSignature returns t as a *types.FunctionType when t is one, so a slot
// known to hold a function/closure value remembers its arity; nil otherwise.
func funcSignature(t types.Type) *types.FunctionType {
	ft, ok := t.(*types.FunctionType)
	if !ok {
		return nil
	}
	return ft
}

// constFuncSignature resolves a CONST_GET slot's signature the way an
// Interpreter's own state does: b is the constant's boxed value (a Ref for
// heap-allocated constants), and heap resolves that ref to the *types.Function
// the constant pool entry built, mirroring interp.go's New. nil for every
// other constant, or a ref that isn't a function.
func constFuncSignature(b types.Boxed, heap []types.Value) *types.FunctionType {
	if b.Kind() != types.KindRef {
		return nil
	}
	ref := b.Ref()
	if ref < 0 || ref >= len(heap) {
		return nil
	}
	fn, ok := heap[ref].(*types.Function)
	if !ok {
		return nil
	}
	return fn.Typ
}

func (s *opStack) len() int {
	return len(s.entries)
}

func (s *opStack) top() *types.FunctionType {
	return s.entries[len(s.entries)-1]
}

func (s *opStack) push(sig *types.FunctionType) {
	s.entries = append(s.entries, sig)
}

func (s *opStack) pop() *types.FunctionType {
	v := s.entries[len(s.entries)-1]
	s.entries = s.entries[:len(s.entries)-1]
	return v
}

func (s *opStack) drop(n int) {
	s.entries = s.entries[:len(s.entries)-n]
}

func (s *opStack) swap() {
	n := len(s.entries)
	s.entries[n-1], s.entries[n-2] = s.entries[n-2], s.entries[n-1]
}

func (s *opStack) clone() *opStack {
	return &opStack{entries: append([]*types.FunctionType(nil), s.entries...)}
}

// merge folds other's entries into s at a control-flow join, reporting
// whether s changed (so the successor must be re-evaluated) and whether the
// two states agree on height. A height disagreement is a structural defect;
// differing signatures widen to nil (unknown) rather than failing.
func (s *opStack) merge(other *opStack) (changed, agree bool) {
	if len(s.entries) != len(other.entries) {
		return false, false
	}
	for i := range s.entries {
		if s.entries[i] != nil && s.entries[i] != other.entries[i] {
			s.entries[i] = nil
			changed = true
		}
	}
	return changed, true
}
