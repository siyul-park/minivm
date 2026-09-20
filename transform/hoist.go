package transform

import (
	"sort"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
)

// HoistPass moves safe loop-invariant operations to preheaders.
type HoistPass struct{}

var _ pass.Pass[*ssa.Function] = (*HoistPass)(nil)

// NewHoistPass returns the pass.
func NewHoistPass() *HoistPass {
	return &HoistPass{}
}

// Run applies the pass to one SSA function.
func (p *HoistPass) Run(_ *pass.Manager, function *ssa.Function) (bool, error) {
	dominance := graph.NewDominance(function)
	headers := graph.LoopHeaders(function, dominance)
	if len(headers) == 0 {
		return true, nil
	}

	bodies := make(map[int]map[int]bool, len(headers))
	preheaders := make(map[int]int, len(headers))
	for _, h := range headers {
		b := graph.LoopBody(function, dominance, h)
		bodies[h] = b
		if p, ok := graph.Preheader(function, b, h); ok {
			preheaders[h] = p
		}
	}
	sort.Slice(headers, func(i, j int) bool {
		return len(bodies[headers[i]]) < len(bodies[headers[j]])
	})

	blocks := graph.ReversePostorder(function)
	defSite := map[ssa.Value]operationSite{}
	paramOf := map[ssa.Value]int{}
	for _, b := range blocks {
		currentBlock := function.Block(b)
		for _, v := range currentBlock.Params {
			paramOf[v] = b
		}
		for i, operation := range currentBlock.Operations {
			for _, r := range operation.Results {
				defSite[r] = operationSite{b, i}
			}
		}
	}

	dest := map[operationSite]int{}
	current := func(s operationSite) int {
		if b, ok := dest[s]; ok {
			return b
		}
		return s.block
	}
	location := func(v ssa.Value) (int, bool) {
		if b, ok := paramOf[v]; ok {
			return b, true
		}
		if s, ok := defSite[v]; ok {
			return current(s), true
		}
		return 0, false
	}

	changed := false
	for _, h := range headers {
		p, ok := preheaders[h]
		if !ok {
			continue
		}
		body := bodies[h]
		for _, b := range blocks {
			if !body[b] {
				continue
			}
			ops := function.Block(b).Operations
			for i, operation := range ops {
				s := operationSite{b, i}
				if !body[current(s)] {
					continue
				}
				if !isHoistable(operation) {
					continue
				}
				invariant := true
				for _, a := range operation.Args {
					loc, ok := location(a)
					if !ok || body[loc] {
						invariant = false
						break
					}
				}
				if invariant {
					dest[s] = p
					changed = true
				}
			}
		}
	}

	if !changed {
		return true, nil
	}

	rebuilder := newRebuilder(function)
	for _, b := range blocks {
		id := rebuilder.block(b)
		currentBlock := function.Block(b)
		for _, v := range currentBlock.Params {
			rebuilder.alias(v, rebuilder.builder.Param(id, function.Type(v)))
		}
		for i, operation := range currentBlock.Operations {
			target := rebuilder.block(current(operationSite{b, i}))
			rebuilder.builder.Add(target, rebuilder.define(function, rebuilder.operation(operation)))
		}
		rebuilder.builder.Term(id, rebuilder.terminator(currentBlock.Terminator))
	}
	next := rebuilder.builder.Build()
	*function = *next
	return false, nil
}

func isHoistable(operation ssa.Operation) bool {
	switch operation.Op {
	case ssa.OpConst:
		return true
	case ssa.OpExec:
		return operation.Code.IsPure() && isSpeculatable(operation.Code) && !ssa.CanOverflowI64(operation.Code)
	default:
		return false
	}
}

func isSpeculatable(code instr.Opcode) bool {
	switch code {
	case instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U,
		instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U:
		return false
	default:
		return true
	}
}
