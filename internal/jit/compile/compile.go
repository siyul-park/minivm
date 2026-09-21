// Package compile lowers SSA functions to native code through a target Machine.
package compile

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
)

// Machine emits the rows of one target. A Machine lowers one function at a
// time: Prologue begins a function and Epilogue ends it.
type Machine interface {
	Arch() asm.Arch
	Reserve() []asm.PReg
	Prologue(a *asm.Assembler, slots, params int)
	Epilogue(a *asm.Assembler)
	// Lower emits op and reports false when the target cannot lower it.
	Lower(a *asm.Assembler, op ssa.Operation, r Regs) bool
	// Branch transfers control to labels, one per edge of t.
	Branch(a *asm.Assembler, t ssa.Terminator, r Regs, labels []asm.Label)
	// Return ends the function with an OpReturn or OpComplete t.
	Return(a *asm.Assembler, t ssa.Terminator, r Regs)
	// Budget counts one loop iteration down.
	Budget(a *asm.Assembler)
	Move(a *asm.Assembler, dst, src asm.VReg)
}

// Regs maps SSA values to their native representation.
type Regs interface {
	Reg(v ssa.Value) asm.VReg
	Type(v ssa.Value) ssa.Type
}

type values struct {
	function *ssa.Function
}

type move struct {
	dst, src asm.VReg
}

type stub struct {
	label asm.Label
	block int
	moves []move
}

// ErrUnsupported reports SSA the backend does not lower.
var ErrUnsupported = errors.New("unsupported lowering")

// Lower emits native code for f, an entry-0 function with params parameter
// slots followed by locals local slots.
func Lower(f *ssa.Function, m Machine, params, locals int) ([]byte, error) {
	if f.Len() == 0 || params < 0 || locals < 0 {
		return nil, fmt.Errorf("%w: function shape", ErrUnsupported)
	}
	if len(f.Block(0).Params) > 0 {
		return nil, fmt.Errorf("%w: entry parameters", ErrUnsupported)
	}
	r := values{function: f}
	a := asm.New(m.Arch())
	a.Reserve(m.Reserve()...)

	order := graph.Order(f)
	labels := make([]asm.Label, f.Len())
	for _, block := range order {
		labels[block] = a.Label()
	}
	headers := graph.Headers(f, graph.NewDominance(f))

	m.Prologue(a, params+locals, params)
	var stubs []stub
	for _, block := range order {
		b := f.Block(block)
		a.Bind(labels[block])
		counted := !slices.Contains(headers, block)
		for _, op := range b.Operations {
			if !counted && op.State != ssa.NoValue {
				m.Budget(a)
				counted = true
			}
			switch op.Op {
			case ssa.OpState:
			case ssa.OpConst, ssa.OpLoad, ssa.OpStore, ssa.OpExec:
				if !m.Lower(a, op, r) {
					return nil, fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
				}
			default:
				return nil, fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
			}
		}
		if !counted {
			return nil, fmt.Errorf("%w: loop header %d without state", ErrUnsupported, block)
		}
		s, err := terminate(a, m, f, r, b.Terminator, labels)
		if err != nil {
			return nil, err
		}
		stubs = append(stubs, s...)
	}
	for _, s := range stubs {
		a.Bind(s.label)
		shuffle(a, m, f, s.moves)
		m.Branch(a, ssa.Terminator{Op: ssa.OpJump}, r, []asm.Label{labels[s.block]})
	}
	m.Epilogue(a)
	return a.Build()
}

func (r values) Reg(v ssa.Value) asm.VReg {
	switch r.function.Type(v) {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		return asm.NewVReg(int32(v), asm.RegTypeInt, asm.Width32)
	case ssa.TypeI64, ssa.TypeRef:
		return asm.NewVReg(int32(v), asm.RegTypeInt, asm.Width64)
	case ssa.TypeF32:
		return asm.NewVReg(int32(v), asm.RegTypeFloat, asm.Width32)
	case ssa.TypeF64:
		return asm.NewVReg(int32(v), asm.RegTypeFloat, asm.Width64)
	default:
		return asm.VReg{}
	}
}

func (r values) Type(v ssa.Value) ssa.Type {
	return r.function.Type(v)
}

// terminate lowers t. An edge that moves values gets a stub of its own, so
// the branch itself never moves anything.
func terminate(a *asm.Assembler, m Machine, f *ssa.Function, r values, t ssa.Terminator, labels []asm.Label) ([]stub, error) {
	switch t.Op {
	case ssa.OpReturn, ssa.OpComplete:
		m.Return(a, t, r)
		return nil, nil
	case ssa.OpJump, ssa.OpBranch, ssa.OpTable:
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, t.Op)
	}
	stubs := make([]stub, len(t.Edges))
	moved := false
	for i, e := range t.Edges {
		moves, err := pair(r, e.Args, f.Block(e.Block).Params)
		if err != nil {
			return nil, err
		}
		stubs[i] = stub{label: labels[e.Block], block: e.Block, moves: moves}
		moved = moved || len(moves) > 0
	}
	if len(stubs) == 0 {
		return nil, fmt.Errorf("%w: %s without edges", ErrUnsupported, t.Op)
	}
	if len(stubs) == 1 {
		shuffle(a, m, f, stubs[0].moves)
		m.Branch(a, t, r, []asm.Label{stubs[0].label})
		return nil, nil
	}
	targets := make([]asm.Label, len(stubs))
	for i := range stubs {
		if moved {
			stubs[i].label = a.Label()
		}
		targets[i] = stubs[i].label
	}
	m.Branch(a, t, r, targets)
	if !moved {
		return nil, nil
	}
	return stubs, nil
}

func pair(r values, args, params []ssa.Value) ([]move, error) {
	if len(args) != len(params) {
		return nil, fmt.Errorf("%w: %d edge arguments for %d parameters", ErrUnsupported, len(args), len(params))
	}
	var moves []move
	for i, arg := range args {
		if dst, src := r.Reg(params[i]), r.Reg(arg); dst != src {
			moves = append(moves, move{dst: dst, src: src})
		}
	}
	return moves, nil
}

// shuffle emits moves as one parallel move: a destination is written only
// once no pending move reads it, and a cycle is broken through a scratch
// register of the bank and width it passes through.
func shuffle(a *asm.Assembler, m Machine, f *ssa.Function, moves []move) {
	pending := slices.Clone(moves)
	for len(pending) > 0 {
		i := slices.IndexFunc(pending, func(p move) bool {
			return !slices.ContainsFunc(pending, func(q move) bool { return q.src == p.dst })
		})
		if i < 0 {
			src := pending[0].src
			tmp := scratch(f, src)
			m.Move(a, tmp, src)
			for j := range pending {
				if pending[j].src == src {
					pending[j].src = tmp
				}
			}
			continue
		}
		m.Move(a, pending[i].dst, pending[i].src)
		pending = slices.Delete(pending, i, i+1)
	}
}

// scratch is a register no SSA value names, one per bank and width.
func scratch(f *ssa.Function, like asm.VReg) asm.VReg {
	id := int32(f.Values())
	if like.Type() == asm.RegTypeFloat {
		id += 2
	}
	if like.Width() == asm.Width64 {
		id++
	}
	return asm.NewVReg(id, like.Type(), like.Width())
}
