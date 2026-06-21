package verify

import "github.com/siyul-park/minivm/types"

// slot is one abstract operand-stack entry: its kind plus, when the entry
// definitely holds a function or closure reference, that reference's signature.
// The signature lets CALL recover a callee's arity statically even though the
// bytecode carries no call type operand.
type slot struct {
	kind types.Kind
	fn   *types.FunctionType
}

// stack is the abstract operand stack threaded through a basic block.
type stack struct {
	slots []slot
}

// anyKind is the verifier's top element over types.Kind: a slot whose concrete
// kind could not be determined statically (a dynamic load, a host result, or a
// control-flow merge of two different kinds). It reuses the types.Kind space
// rather than defining a parallel enum; an out-of-range value prints as
// "unknown". anyKind satisfies any required kind and absorbs any merge, so the
// verifier never rejects on an unknown kind — only on a definite disagreement
// between two concrete kinds.
const anyKind = types.Kind(0xFF)

func (s *stack) len() int {
	return len(s.slots)
}

func (s *stack) top() slot {
	return s.slots[len(s.slots)-1]
}

func (s *stack) push(v slot) {
	s.slots = append(s.slots, v)
}

func (s *stack) pop() slot {
	v := s.slots[len(s.slots)-1]
	s.slots = s.slots[:len(s.slots)-1]
	return v
}

func (s *stack) drop(n int) {
	s.slots = s.slots[:len(s.slots)-n]
}

func (s *stack) swap() {
	n := len(s.slots)
	s.slots[n-1], s.slots[n-2] = s.slots[n-2], s.slots[n-1]
}

func (s *stack) clone() *stack {
	return &stack{slots: append([]slot(nil), s.slots...)}
}

// merge folds other's slots into s at a control-flow join, reporting whether s
// changed (so the successor must be re-evaluated) and whether the two states
// agree on height. A height disagreement is a structural defect; differing
// kinds widen to anyKind rather than failing.
func (s *stack) merge(other *stack) (changed, balanced bool) {
	if len(s.slots) != len(other.slots) {
		return false, false
	}
	for i := range s.slots {
		if k := unify(s.slots[i].kind, other.slots[i].kind); k != s.slots[i].kind {
			s.slots[i].kind = k
			changed = true
		}
		if s.slots[i].fn != nil && s.slots[i].fn != other.slots[i].fn {
			s.slots[i].fn = nil
			changed = true
		}
	}
	return changed, true
}

// signatureOf returns t as a *FunctionType when t is one, so a slot carrying a
// function/closure reference remembers its arity; otherwise nil.
func signatureOf(t types.Type) *types.FunctionType {
	if ft, ok := t.(*types.FunctionType); ok {
		return ft
	}
	return nil
}

// accepts reports whether an actual stack slot of kind got satisfies a required
// operand kind want. anyKind on either side unifies: the verifier stays
// permissive about unknowns and strict only about concrete disagreement.
func accepts(got, want types.Kind) bool {
	return got == anyKind || want == anyKind || got == want
}

// unify folds two slot kinds at a control-flow join: equal kinds survive,
// anything else widens to anyKind.
func unify(a, b types.Kind) types.Kind {
	if a == b {
		return a
	}
	return anyKind
}
