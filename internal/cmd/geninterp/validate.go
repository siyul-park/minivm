package main

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

func (p pattern) key() string {
	var key strings.Builder
	for index, step := range p {
		if index > 0 {
			key.WriteByte('/')
		}
		fmt.Fprintf(&key, "%d", step.op)
		if step.typ != nil {
			fmt.Fprintf(&key, ":%t:%s", step.not, step.typ)
		}
	}
	return key.String()
}

func (p pattern) overlaps(other pattern) bool {
	if len(p) != len(other) {
		return false
	}
	for index := range p {
		if p[index].op != other[index].op || !p[index].overlaps(other[index]) {
			return false
		}
	}
	return true
}

func (s step) overlaps(other step) bool {
	if s.typ == nil || other.typ == nil {
		return true
	}
	if !s.not && !other.not {
		return s.typ == other.typ
	}
	if s.not && other.not {
		return true
	}
	if s.not {
		s, other = other, s
	}
	return s.typ != other.typ
}

func validate(patterns []pattern) error {
	seen := make(map[string]pattern, len(patterns))
	for _, pattern := range patterns {
		if len(pattern) == 0 {
			return fmt.Errorf("empty fusion pattern")
		}
		for _, step := range pattern {
			if !instr.Valid(step.op) {
				return fmt.Errorf("unsupported opcode %d", step.op)
			}
			typ := instr.TypeOf(step.op)
			for _, width := range typ.Widths {
				if width < 0 {
					return fmt.Errorf("%s has variable-width operands", typ.Mnemonic)
				}
			}
			if step.typ != nil || step.not {
				if step.typ == nil {
					return fmt.Errorf("%s has invalid guard", typ.Mnemonic)
				}
				if step.op != instr.CONST_GET {
					return fmt.Errorf("%s cannot resolve a type guard", typ.Mnemonic)
				}
			}
		}
		if _, _, _, err := effect(pattern); err != nil {
			return err
		}
		key := pattern.key()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate fusion pattern %s", key)
		}
		for otherKey, other := range seen {
			if pattern.overlaps(other) {
				return fmt.Errorf("ambiguous fusion patterns %s and %s", otherKey, key)
			}
		}
		if _, err := compose(pattern, pattern.width(), ""); err != nil {
			return fmt.Errorf("unsupported fusion %s: %w", key, err)
		}
		seen[key] = pattern
	}
	return nil
}

func effect(pattern pattern) (int, int, bool, error) {
	var stack []instr.Kind
	pops := 0
	for _, step := range pattern {
		typ := instr.TypeOf(step.op)
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
