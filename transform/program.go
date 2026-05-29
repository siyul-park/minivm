package transform

import (
	"github.com/siyul-park/minivm/types"

	"github.com/siyul-park/minivm/program"
)

// functions returns a program's executable functions: an implicit root over
// prog.Code followed by every *types.Function constant.
func functions(prog *program.Program) []*types.Function {
	fns := []*types.Function{{Typ: &types.FunctionType{}, Code: prog.Code}}
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, fn)
		}
	}
	return fns
}

// dedup builds a compaction index for items: each referenced entry (used[i])
// is renumbered into a dense range with equal entries collapsed to one slot,
// while unreferenced entries map to -1. Returns the index and compacted size.
func dedup[T any](items []T, used []bool, eq func(a, b T) bool) ([]int, int) {
	index := make([]int, len(items))
	for i := range index {
		index[i] = -1
		if used[i] {
			index[i] = i
		}
	}

	for i := range items {
		if index[i] == -1 {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if eq(items[j], items[i]) {
				index[j] = index[i]
			}
		}
	}

	size := 0
	for i := range index {
		if index[i] == -1 {
			continue
		}
		if index[i] != i {
			index[i] = index[index[i]]
		} else {
			index[i] = size
			size++
		}
	}
	return index, size
}
