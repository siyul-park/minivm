// Package compile lowers SSA functions to native code through a target Machine.
package compile

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/transform"
)

// Machine emits the rows of one target. A Machine lowers one function at a
// time: Prologue begins a function and Epilogue ends it.
type Machine interface {
	Arch() asm.Arch
	Reserve() []asm.PReg
	Prologue(a *asm.Assembler, slots, params int)
	Epilogue(a *asm.Assembler)
	// Lower emits op and reports false when the target cannot lower it.
	Lower(a *asm.Assembler, op ssa.Operation, s Site) bool
	// Branch transfers control to labels, one per edge of t.
	Branch(a *asm.Assembler, t ssa.Terminator, s Site, labels []asm.Label)
	// Return ends the function with an OpReturn or OpComplete t.
	Return(a *asm.Assembler, t ssa.Terminator, s Site)
	// Budget counts one loop iteration down and branches to safepoint when
	// the budget is spent.
	Budget(a *asm.Assembler, safepoint asm.Label)
	// Exit leaves native code through exit id of kind k; uses must stay live
	// up to it. Control continues after Exit when the interpreter resumes.
	Exit(a *asm.Assembler, id int, k jit.Kind, uses []asm.VReg)
	// Results loads a bridge's results from the Context into regs.
	Results(a *asm.Assembler, regs []asm.VReg)
	Move(a *asm.Assembler, dst, src asm.VReg)
}

// Site is what Machine sees of the operation or terminator it lowers.
type Site interface {
	Reg(v ssa.Value) asm.VReg
	Type(v ssa.Value) ssa.Type
	// Exit returns the label of an exit of kind k at the interpreter state
	// of the operation. A resumable exit continues after the operation.
	Exit(k jit.Kind) asm.Label
}

type lowering struct {
	f      *ssa.Function
	m      Machine
	a      *asm.Assembler
	states map[ssa.Value]ssa.Operation
	raw    map[ssa.Value]bool
	exits  []*jit.Exit
	places [][]place
	edges  []edge
	stubs  []stub
	err    error

	// The operation or terminator being lowered.
	op      ssa.Operation
	resume  asm.Label
	resumed bool
}

// place is where exit map value to lives in the rows: register reg.
type place struct {
	to  *jit.Value
	reg asm.VReg
}

type move struct {
	dst, src asm.VReg
}

type edge struct {
	label asm.Label
	block int
	moves []move
}

type stub struct {
	label  asm.Label
	id     int
	resume asm.Label
}

// ErrUnsupported reports SSA the backend does not lower.
var ErrUnsupported = errors.New("unsupported lowering")

// Lower emits native code for f, an entry-0 function with params parameter
// slots followed by locals local slots, and the map of every exit it takes,
// indexed by exit id.
func Lower(f *ssa.Function, m Machine, params, locals int) ([]byte, []jit.Exit, error) {
	if f.Len() == 0 || params < 0 || locals < 0 {
		return nil, nil, fmt.Errorf("%w: function shape", ErrUnsupported)
	}
	if len(f.Block(0).Params) > 0 {
		return nil, nil, fmt.Errorf("%w: entry parameters", ErrUnsupported)
	}
	l := &lowering{f: f, m: m, a: asm.New(m.Arch()), states: map[ssa.Value]ssa.Operation{}, raw: map[ssa.Value]bool{}}
	l.a.Reserve(m.Reserve()...)
	if err := l.function(params, locals); err != nil {
		return nil, nil, err
	}
	code, err := l.a.Build()
	if err != nil {
		return nil, nil, err
	}
	exits := make([]jit.Exit, len(l.exits))
	for id, e := range l.exits {
		for _, p := range l.places[id] {
			loc, ok := l.a.Loc(p.reg)
			if !ok {
				return nil, nil, fmt.Errorf("%w: exit %d names v%d with no location", ErrUnsupported, id, p.reg.ID())
			}
			p.to.Loc = loc
		}
		exits[id] = *e
	}
	return code, exits, nil
}

func (l *lowering) function(params, locals int) error {
	order := graph.Order(l.f)
	labels := make([]asm.Label, l.f.Len())
	for _, block := range order {
		labels[block] = l.a.Label()
	}
	headers := graph.Headers(l.f, graph.NewDominance(l.f))

	l.m.Prologue(l.a, params+locals, params)
	for _, block := range order {
		b := l.f.Block(block)
		l.a.Bind(labels[block])
		counted := !slices.Contains(headers, block)
		for _, op := range b.Operations {
			if !counted && op.State != ssa.NoValue {
				l.begin(op)
				l.m.Budget(l.a, l.Exit(jit.ExitSafepoint))
				l.end()
				counted = true
			}
			if err := l.operation(op); err != nil {
				return err
			}
		}
		if !counted {
			return fmt.Errorf("%w: loop header %d without state", ErrUnsupported, block)
		}
		if err := l.terminator(b.Terminator, labels); err != nil {
			return err
		}
	}
	for _, e := range l.edges {
		l.a.Bind(e.label)
		l.shuffle(e.moves)
		l.m.Branch(l.a, ssa.Terminator{Op: ssa.OpJump}, l, []asm.Label{labels[e.block]})
	}
	for _, s := range l.stubs {
		l.a.Bind(s.label)
		l.m.Exit(l.a, s.id, l.exits[s.id].Kind, l.live(s.id))
		if l.exits[s.id].Kind != jit.ExitDeopt {
			l.m.Branch(l.a, ssa.Terminator{Op: ssa.OpJump}, l, []asm.Label{s.resume})
		}
	}
	l.m.Epilogue(l.a)
	return l.err
}

func (l *lowering) operation(op ssa.Operation) error {
	if op.Op != ssa.OpGuardKind {
		if err := l.validate(op.Args); err != nil {
			return err
		}
	}
	l.begin(op)
	defer l.end()
	switch op.Op {
	case ssa.OpState:
		l.states[op.Results[0]] = op
		return nil
	case ssa.OpLoad:
		if l.f.Type(op.Results[0]) == ssa.TypeI64 {
			l.raw[op.Results[0]] = true
		}
	case ssa.OpGuardKind:
		if !l.raw[op.Args[0]] {
			l.m.Move(l.a, l.Reg(op.Results[0]), l.Reg(op.Args[0]))
			return nil
		}
	case ssa.OpExec:
		if l.m.Lower(l.a, op, l) {
			return l.err
		}
		results := make([]asm.VReg, len(op.Results))
		for i, v := range op.Results {
			results[i] = l.Reg(v)
		}
		id := l.exit(jit.ExitBridge)
		l.m.Exit(l.a, id, jit.ExitBridge, l.live(id))
		l.m.Results(l.a, results)
		return l.err
	case ssa.OpConst, ssa.OpStore, ssa.OpRetain, ssa.OpRelease:
	default:
		return fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
	}
	if !l.m.Lower(l.a, op, l) {
		return fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
	}
	return l.err
}

// terminator lowers t. An edge that moves values gets a stub of its own, so
// the branch itself never moves anything.
func (l *lowering) terminator(t ssa.Terminator, labels []asm.Label) error {
	var args []ssa.Value
	for _, e := range t.Edges {
		args = append(args, e.Args...)
	}
	if err := l.validate(append(args, t.Args...)); err != nil {
		return err
	}
	l.begin(ssa.Operation{State: t.State})
	defer l.end()
	switch t.Op {
	case ssa.OpReturn, ssa.OpComplete:
		l.m.Return(l.a, t, l)
		return l.err
	case ssa.OpExit:
		id := l.exit(jit.ExitDeopt)
		l.m.Exit(l.a, id, jit.ExitDeopt, l.live(id))
		return l.err
	case ssa.OpJump, ssa.OpBranch, ssa.OpTable:
	default:
		return fmt.Errorf("%w: %s", ErrUnsupported, t.Op)
	}
	edges := make([]edge, len(t.Edges))
	moved := false
	for i, e := range t.Edges {
		moves, err := l.pair(e.Args, l.f.Block(e.Block).Params)
		if err != nil {
			return err
		}
		edges[i] = edge{label: labels[e.Block], block: e.Block, moves: moves}
		moved = moved || len(moves) > 0
	}
	if len(edges) == 0 {
		return fmt.Errorf("%w: %s without edges", ErrUnsupported, t.Op)
	}
	if len(edges) == 1 {
		l.shuffle(edges[0].moves)
		l.m.Branch(l.a, t, l, []asm.Label{edges[0].label})
		return nil
	}
	targets := make([]asm.Label, len(edges))
	for i := range edges {
		if moved {
			edges[i].label = l.a.Label()
		}
		targets[i] = edges[i].label
	}
	l.m.Branch(l.a, t, l, targets)
	if moved {
		l.edges = append(l.edges, edges...)
	}
	return nil
}

// validate rejects a raw i64 slot word outside its kind guard.
func (l *lowering) validate(args []ssa.Value) error {
	for _, v := range args {
		if l.raw[v] {
			return fmt.Errorf("%w: unguarded i64 slot word v%d", ErrUnsupported, v)
		}
	}
	return nil
}

// Reg is v's virtual register, typed by its static representation.
func (l *lowering) Reg(v ssa.Value) asm.VReg {
	switch l.f.Type(v) {
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

// Type is v's static type.
func (l *lowering) Type(v ssa.Value) ssa.Type {
	return l.f.Type(v)
}

// Exit places a stub for an exit of kind k at the current state.
func (l *lowering) Exit(k jit.Kind) asm.Label {
	id := l.exit(k)
	s := stub{label: l.a.Label(), id: id}
	if k != jit.ExitDeopt {
		if !l.resumed {
			l.resume, l.resumed = l.a.Label(), true
		}
		s.resume = l.resume
	}
	l.stubs = append(l.stubs, s)
	return s.label
}

func (l *lowering) begin(op ssa.Operation) {
	l.op, l.resumed = op, false
}

// end binds where the exits of the current operation resume.
func (l *lowering) end() {
	if l.resumed {
		l.a.Bind(l.resume)
		l.resumed = false
	}
}

// exit records the map of an exit of kind k at the current operation's state.
func (l *lowering) exit(k jit.Kind) int {
	id := len(l.exits)
	e := &jit.Exit{Kind: k}
	l.exits = append(l.exits, e)
	l.places = append(l.places, nil)

	state, ok := l.states[l.op.State]
	if !ok {
		l.fail(fmt.Errorf("%w: %s exits without a state", ErrUnsupported, l.op.Op))
		return id
	}
	e.Frames = make([]jit.Frame, len(state.Frames))
	for i, frame := range state.Frames {
		f := &e.Frames[i]
		*f = jit.Frame{Address: frame.Address, Base: frame.Base, IP: frame.IP, Returns: frame.Returns}
		if len(frame.Stack) > 0 {
			f.Stack = make([]jit.Operand, len(frame.Stack))
		}
		for j, o := range frame.Stack {
			f.Stack[j].Owned = o.Owned
			l.place(id, &f.Stack[j].Value, o.Value)
		}
		if len(frame.Locals) > 0 {
			f.Locals = make([]jit.Local, len(frame.Locals))
		}
		for j, local := range frame.Locals {
			f.Locals[j].Index = local.Index
			l.place(id, &f.Locals[j].Value, local.Value)
		}
	}
	switch k {
	case jit.ExitBridge:
		e.Code = l.op.Code
		e.Adopts = transform.Adopts(l.op.Code, len(l.op.Args))
		for _, v := range l.op.Results {
			e.Results = append(e.Results, l.f.Type(v).Kind())
		}
	case jit.ExitRelease:
		l.place(id, &e.Release, l.op.Args[0])
	}
	return id
}

func (l *lowering) place(id int, to *jit.Value, v ssa.Value) {
	if l.raw[v] {
		l.fail(fmt.Errorf("%w: exit %d names unguarded i64 slot word v%d", ErrUnsupported, id, v))
	}
	to.Kind = l.f.Type(v).Kind()
	l.places[id] = append(l.places[id], place{to: to, reg: l.Reg(v)})
}

func (l *lowering) live(id int) []asm.VReg {
	var regs []asm.VReg
	for _, p := range l.places[id] {
		if !slices.Contains(regs, p.reg) {
			regs = append(regs, p.reg)
		}
	}
	return regs
}

func (l *lowering) fail(err error) {
	if l.err == nil {
		l.err = err
	}
}

func (l *lowering) pair(args, params []ssa.Value) ([]move, error) {
	if len(args) != len(params) {
		return nil, fmt.Errorf("%w: %d edge arguments for %d parameters", ErrUnsupported, len(args), len(params))
	}
	var moves []move
	for i, arg := range args {
		if dst, src := l.Reg(params[i]), l.Reg(arg); dst != src {
			moves = append(moves, move{dst: dst, src: src})
		}
	}
	return moves, nil
}

// shuffle emits moves as one parallel move: a destination is written only
// once no pending move reads it, and a cycle is broken through a scratch
// register of the bank and width it passes through.
func (l *lowering) shuffle(moves []move) {
	pending := slices.Clone(moves)
	for len(pending) > 0 {
		i := slices.IndexFunc(pending, func(p move) bool {
			return !slices.ContainsFunc(pending, func(q move) bool { return q.src == p.dst })
		})
		if i < 0 {
			src := pending[0].src
			tmp := l.scratch(src)
			l.m.Move(l.a, tmp, src)
			for j := range pending {
				if pending[j].src == src {
					pending[j].src = tmp
				}
			}
			continue
		}
		l.m.Move(l.a, pending[i].dst, pending[i].src)
		pending = slices.Delete(pending, i, i+1)
	}
}

// scratch is a register no SSA value names, one per bank and width.
func (l *lowering) scratch(like asm.VReg) asm.VReg {
	id := int32(l.f.Values())
	if like.Type() == asm.RegTypeFloat {
		id += 2
	}
	if like.Width() == asm.Width64 {
		id++
	}
	return asm.NewVReg(id, like.Type(), like.Width())
}
