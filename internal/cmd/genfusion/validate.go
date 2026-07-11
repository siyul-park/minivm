package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

func expand(declarations ...declaration) ([]rule, error) {
	var result []rule
	for _, declaration := range declarations {
		rules, err := declaration.expand()
		if err != nil {
			return nil, err
		}
		result = append(result, rules...)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].pattern) != len(result[j].pattern) {
			return len(result[i].pattern) > len(result[j].pattern)
		}
		return result[i].pattern.key() < result[j].pattern.key()
	})
	return result, nil
}

func validate(rules []rule) error {
	seen := make(map[string]rule, len(rules))
	for _, rule := range rules {
		if len(rule.pattern) == 0 {
			return fmt.Errorf("empty fusion pattern")
		}
		for _, op := range rule.pattern {
			if !instr.Valid(op.op) {
				return fmt.Errorf("unsupported opcode %d", op.op)
			}
			typ := instr.TypeOf(op.op)
			for _, width := range typ.Widths {
				if width < 0 {
					return fmt.Errorf("%s has variable-width operands", typ.Mnemonic)
				}
			}
			if op.guard != nil {
				if op.guard.typeOf == nil {
					return fmt.Errorf("%s has invalid guards", typ.Mnemonic)
				}
				if op.guard.negations > 1 {
					return fmt.Errorf("%s has nested negation", typ.Mnemonic)
				}
				if op.op != instr.CONST_GET {
					return fmt.Errorf("%s cannot resolve a type guard", typ.Mnemonic)
				}
			}
		}
		if len(rule.pattern) > 2 {
			consumer := rule.pattern[1].op
			if consumer == instr.DROP || (consumer == instr.REF_IS_NULL && (len(rule.pattern) != 3 || rule.pattern[2].op != instr.BR_IF)) {
				return fmt.Errorf("ref rule has unsupported trailing operations")
			}
		}
		if want, ok := rule.delta(); ok {
			pops, pushes, fixed, err := effect(rule.pattern)
			if err != nil {
				return err
			}
			if fixed {
				delta := pushes - pops
				if delta != want {
					return fmt.Errorf("stack delta %d (pop %d, push %d), want %d", delta, pops, pushes, want)
				}
			}
		}
		key := rule.pattern.key()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate fusion pattern %s", key)
		}
		for otherKey, other := range seen {
			if rule.pattern.overlaps(other.pattern) {
				return fmt.Errorf("ambiguous fusion patterns %s and %s", otherKey, key)
			}
		}
		if _, err := renderFusionRule(rule, rule.pattern.width(), ""); err != nil {
			return fmt.Errorf("unsupported threaded fusion %s: %w", key, err)
		}
		seen[key] = rule
	}
	return nil
}

func effect(pattern pattern) (int, int, bool, error) {
	var stack []instr.Kind
	pops := 0
	for _, operation := range pattern {
		typ := instr.TypeOf(operation.op)
		if typ.Pop == nil && typ.Push == nil {
			return 0, 0, false, nil
		}
		for _, want := range typ.Pop {
			if len(stack) == 0 {
				pops++
				continue
			}
			last := len(stack) - 1
			got := stack[last]
			stack = stack[:last]
			if got != instr.KindAny && want != instr.KindAny && got.Repr() != want.Repr() {
				return 0, 0, false, fmt.Errorf("%s has stack kind %s, want %s", typ.Mnemonic, got, want)
			}
		}
		stack = append(stack, typ.Push...)
	}
	return pops, len(stack), true, nil
}

// delta reports the stack delta the rule's generated handler produces, and
// whether the rule's renderer has a fixed, known delta at all.
func (rule rule) delta() (int, bool) {
	pattern := rule.pattern
	last := len(pattern) - 1
	branch := pattern[last].op == instr.BR_IF
	consumerAt := last
	if branch {
		consumerAt--
	}
	if consumerAt < 0 {
		return 0, false
	}
	consumer := pattern[consumerAt].op
	if len(pattern) == 2 && pattern[0].op == instr.I32_CONST && branch {
		return 0, true
	}
	switch consumer {
	case instr.DROP:
		if consumerAt != 1 || branch {
			return 0, false
		}
		return 0, true
	case instr.REF_IS_NULL:
		if consumerAt != 1 {
			return 0, false
		}
		if branch {
			return 0, true
		}
		return 1, true
	case instr.ARRAY_GET, instr.STRUCT_GET:
		if consumerAt != 1 || branch {
			return 0, false
		}
		return 0, true
	}
	if arity, ok := numericConsumer(consumer); ok {
		if consumerAt > 2 {
			return 0, false
		}
		delta := consumerAt - arity
		if !branch {
			delta++
		}
		return delta, true
	}
	return 0, false
}

func (p pattern) key() string {
	var key strings.Builder
	for idx, op := range p {
		if idx > 0 {
			key.WriteByte('/')
		}
		fmt.Fprintf(&key, "%d", op.op)
		if op.guard != nil {
			fmt.Fprintf(&key, ":%t:%s", op.guard.negations == 1, op.guard.typeOf)
		}
	}
	return key.String()
}

func (p pattern) overlaps(other pattern) bool {
	if len(p) != len(other) {
		return false
	}
	for idx := range p {
		if p[idx].op != other[idx].op || !p[idx].guard.overlaps(other[idx].guard) {
			return false
		}
	}
	return true
}

func (g *guard) overlaps(other *guard) bool {
	if g == nil || other == nil {
		return true
	}
	if g.negations == 0 && other.negations == 0 {
		return g.typeOf == other.typeOf
	}
	if g.negations == 1 && other.negations == 1 {
		return true
	}
	if g.negations == 1 {
		g, other = other, g
	}
	return g.typeOf != other.typeOf
}
