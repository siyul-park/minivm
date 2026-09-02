package codegen

import (
	"fmt"

	"github.com/siyul-park/minivm/instr"
)

func validate(patterns []pattern) error {
	seen := make(map[string]pattern, len(patterns))
	for _, pattern := range patterns {
		if len(pattern) == 0 {
			return fmt.Errorf("empty fusion pattern")
		}
		for _, current := range pattern {
			if !instr.Valid(current.op) {
				return fmt.Errorf("unsupported opcode %d", current.op)
			}
			typ := instr.TypeOf(current.op)
			for _, size := range typ.Widths {
				if size < 0 {
					return fmt.Errorf("%s has variable-width operands", typ.Mnemonic)
				}
			}
			if current.typ != nil || current.exclude {
				if current.typ == nil {
					return fmt.Errorf("%s has invalid guard", typ.Mnemonic)
				}
				if current.op != instr.CONST_GET && current.op != instr.LOCAL_GET && current.op != instr.GLOBAL_GET && current.op != instr.UPVAL_GET {
					return fmt.Errorf("%s cannot resolve a type guard", typ.Mnemonic)
				}
			}
		}
		if err := validateStack(pattern); err != nil {
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

func validateStack(pattern pattern) error {
	var stack []instr.Kind
	for _, current := range pattern {
		typ := instr.TypeOf(current.op)
		if typ.Pop == nil && typ.Push == nil {
			continue
		}
		for _, want := range typ.Pop {
			if len(stack) == 0 {
				continue
			}
			last := len(stack) - 1
			got := stack[last]
			stack = stack[:last]
			if got != instr.KindAny && want != instr.KindAny && got.Repr() != want.Repr() {
				return fmt.Errorf("%s has stack kind %s, want %s", typ.Mnemonic, got, want)
			}
		}
		stack = append(stack, typ.Push...)
	}
	return nil
}
