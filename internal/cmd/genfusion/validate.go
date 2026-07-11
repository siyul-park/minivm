package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

func expand(declaration declaration) ([]rule, error) {
	if len(declaration.pattern) > 0 {
		return []rule{{pattern: declaration.pattern, arm64: declaration.arm64}}, nil
	}
	if len(declaration.sources) == 0 || len(declaration.consumers) == 0 {
		return nil, fmt.Errorf("empty fusion product")
	}
	result := make([]rule, 0, len(declaration.sources)*len(declaration.consumers))
	for _, source := range declaration.sources {
		for _, consumer := range declaration.consumers {
			pattern := append([]operation(nil), source...)
			pattern = append(pattern, consumer...)
			result = append(result, rule{pattern: pattern, arm64: declaration.arm64})
		}
	}
	return result, nil
}

func expandAll(declarations []declaration) ([]rule, error) {
	var result []rule
	for _, declaration := range declarations {
		rules, err := expand(declaration)
		if err != nil {
			return nil, err
		}
		result = append(result, rules...)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].pattern) != len(result[j].pattern) {
			return len(result[i].pattern) > len(result[j].pattern)
		}
		return patternKey(result[i].pattern) < patternKey(result[j].pattern)
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
		key := patternKey(rule.pattern)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate fusion pattern %s", key)
		}
		for otherKey, other := range seen {
			if patternsOverlap(rule.pattern, other.pattern) {
				return fmt.Errorf("ambiguous fusion patterns %s and %s", otherKey, key)
			}
		}
		if _, err := renderThreadedRule(rule, patternWidth(rule.pattern), ""); err != nil {
			return fmt.Errorf("unsupported threaded fusion %s: %w", key, err)
		}
		if rule.arm64 && !supportsARM64(rule.pattern) {
			return fmt.Errorf("ARM64-marked fusion has no specialization %s", key)
		}
		seen[key] = rule
	}
	return nil
}

func patternKey(pattern []operation) string {
	var key strings.Builder
	for idx, op := range pattern {
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

func patternsOverlap(a, b []operation) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx].op != b[idx].op || !guardsOverlap(a[idx].guard, b[idx].guard) {
			return false
		}
	}
	return true
}

func guardsOverlap(a, b *guard) bool {
	if a == nil || b == nil {
		return true
	}
	if a.negations == 0 && b.negations == 0 {
		return a.typeOf == b.typeOf
	}
	if a.negations == 1 && b.negations == 1 {
		return true
	}
	if a.negations == 1 {
		a, b = b, a
	}
	return a.typeOf != b.typeOf
}
