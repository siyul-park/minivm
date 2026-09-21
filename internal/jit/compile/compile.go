package compile

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
)

// Machine lowers SSA rows without knowing the target instruction set.
type Machine interface {
	Arch() asm.Arch
	Reserve() []asm.PReg
	Prologue(a *asm.Assembler, slots, params int)
	Epilogue(a *asm.Assembler)
	Lower(a *asm.Assembler, op ssa.Operation, r Regs) bool
	Branch(a *asm.Assembler, t ssa.Terminator, r Regs, labels []asm.Label)
	Return(a *asm.Assembler, args []asm.VReg, types []ssa.Type)
	Complete(a *asm.Assembler, args []asm.VReg, types []ssa.Type)
	Budget(a *asm.Assembler, safepoint asm.Label)
	Move(a *asm.Assembler, dst, src asm.VReg)
}

// Regs maps SSA values to their native representation.
type Regs interface {
	Reg(v ssa.Value) asm.VReg
	Type(v ssa.Value) ssa.Type
}

// ErrUnsupported reports an SSA feature the P4a backend does not lower.
var ErrUnsupported = errors.New("unsupported lowering")

type values struct {
	function *ssa.Function
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

type edge struct {
	label asm.Label
	block int
	args  []ssa.Value
}

// Lower emits machine rows for one SSA function.
func Lower(f *ssa.Function, m Machine, params, locals int) ([]byte, error) {
	if f == nil || m == nil || f.Len() == 0 || params < 0 || locals < 0 {
		return nil, fmt.Errorf("%w: invalid lowering input", ErrUnsupported)
	}
	if len(f.Block(0).Params) != params {
		return nil, fmt.Errorf("%w: parameters", ErrUnsupported)
	}
	r := values{function: f}
	a := asm.New(m.Arch())
	a.Reserve(m.Reserve()...)

	order := graph.Order(f)
	labels := make([]asm.Label, f.Len())
	for _, block := range order {
		labels[block] = a.Label()
	}
	heads := graph.Headers(f, graph.NewDominance(f))
	safepoints := make(map[int]asm.Label, len(heads))
	for _, block := range heads {
		safepoints[block] = a.Label()
	}

	temp := scratch(f)
	m.Prologue(a, params+locals, params)
	var stubs []edge
	for _, block := range order {
		b := f.Block(block)
		a.Bind(labels[block])
		if block == 0 {
			for i, value := range b.Params {
				op := ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: i}, Results: []ssa.Value{value}}
				if !m.Lower(a, op, r) {
					return nil, fmt.Errorf("%w: parameter %d", ErrUnsupported, i)
				}
			}
		}
		bound := false
		for _, op := range b.Operations {
			if !bound && hasLabel(safepoints, block) && op.State != ssa.NoValue {
				a.Bind(safepoints[block])
				bound = true
			}
			if op.Op == ssa.OpState {
				continue
			}
			switch op.Op {
			case ssa.OpConst, ssa.OpLoad, ssa.OpStore, ssa.OpExec:
				if !m.Lower(a, op, r) {
					return nil, fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
				}
			default:
				return nil, fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
			}
		}
		if hasLabel(safepoints, block) && !bound {
			return nil, fmt.Errorf("%w: loop header %d has no state", ErrUnsupported, block)
		}
		edges, err := terminator(a, m, b.Terminator, r, f, labels, safepoints, temp)
		if err != nil {
			return nil, err
		}
		stubs = append(stubs, edges...)
	}
	for _, e := range stubs {
		a.Bind(e.label)
		if err := move(a, m, e.args, f.Block(e.block).Params, r, temp); err != nil {
			return nil, err
		}
		if hasLabel(safepoints, e.block) {
			m.Budget(a, safepoints[e.block])
		}
		m.Branch(a, ssa.Terminator{Op: ssa.OpJump}, r, []asm.Label{labels[e.block]})
	}
	m.Epilogue(a)
	code, err := a.Build()
	if err != nil {
		return nil, err
	}
	return code, nil
}
func terminator(a *asm.Assembler, m Machine, t ssa.Terminator, r values, f *ssa.Function, labels []asm.Label, safepoints map[int]asm.Label, temp func(asm.RegType, asm.RegWidth) asm.VReg) ([]edge, error) {
	switch t.Op {
	case ssa.OpReturn, ssa.OpComplete:
		args := make([]asm.VReg, len(t.Args))
		types := make([]ssa.Type, len(t.Args))
		for i, v := range t.Args {
			args[i] = r.Reg(v)
			types[i] = f.Type(v)
		}
		if t.Op == ssa.OpComplete {
			m.Complete(a, args, types)
		} else {
			m.Return(a, args, types)
		}
		return nil, nil
	case ssa.OpJump, ssa.OpBranch, ssa.OpTable:
		if len(t.Edges) == 0 {
			return nil, fmt.Errorf("%w: terminator without edge", ErrUnsupported)
		}
		stubs := needsStubs(t.Edges, safepoints)
		if len(t.Edges) == 1 {
			e := t.Edges[0]
			if err := move(a, m, e.Args, f.Block(e.Block).Params, r, temp); err != nil {
				return nil, err
			}
			if hasLabel(safepoints, e.Block) {
				m.Budget(a, safepoints[e.Block])
			}
			m.Branch(a, t, r, []asm.Label{labels[e.Block]})
			return nil, nil
		}
		if !stubs {
			if err := move(a, m, t.Edges[0].Args, f.Block(t.Edges[0].Block).Params, r, temp); err != nil {
				return nil, err
			}
			targets := make([]asm.Label, len(t.Edges))
			for i, edge := range t.Edges {
				targets[i] = labels[edge.Block]
			}
			m.Branch(a, t, r, targets)
			return nil, nil
		}
		out := make([]edge, len(t.Edges))
		targets := make([]asm.Label, len(t.Edges))
		for i, e := range t.Edges {
			out[i] = edge{label: a.Label(), block: e.Block, args: e.Args}
			targets[i] = out[i].label
		}
		m.Branch(a, t, r, targets)
		return out, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, t.Op)
	}
}

func needsStubs(edges []ssa.Edge, safepoints map[int]asm.Label) bool {
	if len(edges) < 2 {
		return false
	}
	first := edges[0]
	for _, edge := range edges[1:] {
		if edge.Block != first.Block || len(edge.Args) != len(first.Args) {
			if len(first.Args) > 0 || len(edge.Args) > 0 {
				return true
			}
			continue
		}
		for i, value := range edge.Args {
			if value != first.Args[i] {
				return true
			}
		}
	}
	for _, edge := range edges {
		if hasLabel(safepoints, edge.Block) {
			return true
		}
	}
	return false
}

func hasLabel(labels map[int]asm.Label, block int) bool {
	_, ok := labels[block]
	return ok
}

func move(a *asm.Assembler, m Machine, src, dst []ssa.Value, r values, scratch func(asm.RegType, asm.RegWidth) asm.VReg) error {
	if len(src) != len(dst) {
		return fmt.Errorf("%w: edge arity", ErrUnsupported)
	}
	pending := make([]movePair, 0, len(src))
	for i, value := range src {
		from, to := r.Reg(value), r.Reg(dst[i])
		if from != to {
			pending = append(pending, movePair{dst: to, src: from})
		}
	}
	for len(pending) > 0 {
		progress := false
		for i, pair := range pending {
			if used(pending, i, pair.dst) {
				continue
			}
			m.Move(a, pair.dst, pair.src)
			pending = append(pending[:i], pending[i+1:]...)
			progress = true
			break
		}
		if progress {
			continue
		}
		pair := pending[0]
		tmp := scratch(pair.src.Type(), pair.src.Width())
		m.Move(a, tmp, pair.src)
		for i := range pending {
			if pending[i].src == pair.src {
				pending[i].src = tmp
			}
		}
	}
	return nil
}

type movePair struct {
	dst asm.VReg
	src asm.VReg
}

func used(pending []movePair, skip int, src asm.VReg) bool {
	for i, pair := range pending {
		if i != skip && pair.src == src {
			return true
		}
	}
	return false
}

func scratch(f *ssa.Function) func(asm.RegType, asm.RegWidth) asm.VReg {
	max := 0
	for block := range f.Len() {
		b := f.Block(block)
		for _, value := range b.Params {
			if n := int(value); n > max {
				max = n
			}
		}
		for _, op := range b.Operations {
			for _, value := range op.Results {
				if n := int(value); n > max {
					max = n
				}
			}
			for _, value := range op.Args {
				if n := int(value); n > max {
					max = n
				}
			}
		}
		for _, value := range b.Terminator.Args {
			if n := int(value); n > max {
				max = n
			}
		}
		for _, edge := range b.Terminator.Edges {
			for _, value := range edge.Args {
				if n := int(value); n > max {
					max = n
				}
			}
		}
	}
	intID := int32(max + 1)
	floatID := intID + 1
	return func(typ asm.RegType, width asm.RegWidth) asm.VReg {
		if typ == asm.RegTypeFloat {
			return asm.NewVReg(floatID, typ, width)
		}
		return asm.NewVReg(intID, typ, width)
	}
}
