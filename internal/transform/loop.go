package transform

import (
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
)

// nest is one function's natural loops with the definition index its loop
// passes share.
type nest struct {
	function  *ssa.Function
	dominance *graph.Dominance
	loops     []graph.Loop
	sites     map[ssa.Value]site
	params    map[ssa.Value]int
}

// site is the operation at index of a block.
type site struct {
	block int
	index int
}

func newNest(function *ssa.Function) *nest {
	n := &nest{function: function, sites: map[ssa.Value]site{}, params: map[ssa.Value]int{}}
	n.dominance = graph.NewDominance(function)
	n.loops = graph.Loops(function, n.dominance)
	for b := 0; b < function.Len(); b++ {
		for _, v := range function.Block(b).Params {
			n.params[v] = b
		}
		for i, operation := range function.Block(b).Operations {
			for _, r := range operation.Results {
				n.sites[r] = site{b, i}
			}
		}
	}
	return n
}

// op is the operation that defines v.
func (n *nest) op(v ssa.Value) (ssa.Operation, bool) {
	at, ok := n.sites[v]
	if !ok {
		return ssa.Operation{}, false
	}
	return n.function.Block(at.block).Operations[at.index], true
}

// inlet is the deopt state at loop header's entry, as seen from preheader:
// the state of header's first operation that has one, with header's params
// replaced by preheader's edge arguments. It fails when an effect precedes
// that operation in header or the state names a value invariant rejects.
func inlet(function *ssa.Function, sites map[ssa.Value]site, header, preheader int, invariant func(ssa.Value) bool) ([]ssa.Frame, bool) {
	block := function.Block(header)
	args := map[ssa.Value]ssa.Value{}
	for i, param := range block.Params {
		args[param] = function.Block(preheader).Terminator.Edges[0].Args[i]
	}
	at := ssa.NoValue
	for _, operation := range block.Operations {
		if operation.State != ssa.NoValue {
			at = operation.State
			break
		}
		if operation.Op != ssa.OpConst && operation.Op != ssa.OpLoad && operation.Op != ssa.OpState {
			return nil, false
		}
	}
	s, ok := sites[at]
	if !ok {
		return nil, false
	}
	state := function.Block(s.block).Operations[s.index]
	substitute := func(v ssa.Value) (ssa.Value, bool) {
		if arg, ok := args[v]; ok {
			return arg, true
		}
		return v, invariant(v)
	}
	frames := make([]ssa.Frame, len(state.Frames))
	for i, frame := range state.Frames {
		stack := make([]ssa.Operand, len(frame.Stack))
		for j, operand := range frame.Stack {
			if stack[j].Value, ok = substitute(operand.Value); !ok {
				return nil, false
			}
			stack[j].Owned = operand.Owned
		}
		locals := make([]ssa.Local, len(frame.Locals))
		for j, local := range frame.Locals {
			if locals[j].Value, ok = substitute(local.Value); !ok {
				return nil, false
			}
			locals[j].Index = local.Index
		}
		frame.Stack, frame.Locals = stack, locals
		frames[i] = frame
	}
	return frames, true
}
