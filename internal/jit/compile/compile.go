// Package compile lowers SSA functions to native code through a target Machine.
package compile

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

// Machine emits target rows for one function.
type Machine interface {
	Arch() asm.Arch
	Reserve() []asm.PReg
	// Prologue begins a function at address whose slots have kinds, params
	// of them parameters, and count requests its entry hotness counter.
	Prologue(a *asm.Assembler, kinds []types.Kind, params int, count bool, address int)
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
	// Spill stores reg into fixed spill slot slot.
	Spill(a *asm.Assembler, reg asm.VReg, slot int)
	// Results loads a bridge's results from the Context into regs.
	Results(a *asm.Assembler, regs []asm.VReg)
	// Call emits call site c and reports false when the target cannot.
	Call(a *asm.Assembler, c Call, s Site) bool
	Move(a *asm.Assembler, dst, src asm.VReg)
}

// Site is what Machine sees of the operation or terminator it lowers.
type Site interface {
	Reg(v ssa.Value) asm.VReg
	Type(v ssa.Value) ssa.Type
	Slot(slot ssa.Slot) ssa.Type
	// Deopt returns the label of an exit that abandons native code at the
	// interpreter state of the operation.
	Deopt() asm.Label
	// Release returns the label of an exit that hands the interpreter ref,
	// whose last reference native code drops, and the label the machine
	// binds where native code resumes.
	Release(ref asm.VReg) (exit, resume asm.Label)
}

// Call describes a statically resolved CALL.
type Call struct {
	// Address is the callee's function address, its Context.Natives index.
	Address int
	// Callee is the function reference the call adopts when Owned; a
	// borrowed Callee lives in the constant pool and native code neither
	// retains nor releases it.
	Callee        ssa.Value
	Args, Results []ssa.Value
	// Base is the callee's frame base in slots from this activation's, and
	// Size the slots from there the callee's frame must fit below
	// Context.Top.
	Base, Size int
	// Exit is the id of the map of the caller's state after the call; Live
	// are the registers it names, which stay live across the call.
	Exit int
	Live []asm.VReg
	// Bridge is the ExitCall the call takes when the callee is not native,
	// the activation is too deep, or the frame does not fit. It resumes at
	// Resume, which Call binds where both paths load the results.
	Bridge, Resume asm.Label
	// Owned reports whether the call adopted Callee's reference, so it
	// releases it once the callee returns.
	Owned bool
	// Self reports whether Address is the unit being lowered's own address
	// and the unit is not OSR (its entry is a loop header): the call branches
	// to its own entry instead of Context.Natives.
	Self bool
}

type lowering struct {
	f       *ssa.Function
	m       Machine
	a       *asm.Assembler
	fn      *types.Function
	address int
	// osr reports whether this unit is rooted at a loop header instead of
	// its function's own entry.
	osr     bool
	count   bool
	objects transform.Objects
	states  map[ssa.Value]ssa.Operation
	consts  map[ssa.Value]types.Boxed
	raw     map[ssa.Value]bool
	// borrow names a value an OpConst ref retained exactly once and used
	// exactly once, as some call's callee: the retain is redundant, since
	// the constant pool already holds the callee alive.
	borrow map[ssa.Value]bool
	homes  map[int]int
	exits  []*jit.Exit
	places [][]place
	saves  [][]save
	deopts []stub
	edges  []edge
	stubs  []stub
	err    error

	// op is the operation or terminator being lowered.
	op ssa.Operation
	// pending is a borrow candidate's retain deferred until the operation
	// right after it either is, or is not, the call it feeds.
	pending ssa.Value
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

type save struct {
	reg  asm.VReg
	slot int
}

// ErrUnsupported reports SSA the backend does not lower.
var ErrUnsupported = errors.New("unsupported lowering")

// Lower emits code for f and its exit maps. address is the unit's own
// function address: a call to it is its own recursion. count requests its
// function-entry hotness counter. An OSR unit's block
// 0 may carry parameters, the operand stack the interpreter left at its
// header (transform.Translate roots there); Lower loads them itself (see
// params) instead of rejecting them.
func Lower(f *ssa.Function, m Machine, fn *types.Function, objects transform.Objects, address int, osr, count bool) ([]byte, []jit.Exit, error) {
	if f.Len() == 0 {
		return nil, nil, fmt.Errorf("%w: function shape", ErrUnsupported)
	}
	if !osr && len(f.Block(0).Params) > 0 {
		return nil, nil, fmt.Errorf("%w: entry parameters", ErrUnsupported)
	}
	for _, p := range f.Block(0).Params {
		// A not-yet-loaded i64 param would need every later block-0 param's
		// deopt map to distinguish "raw slot word" from "guarded" by
		// position, which this backend does not implement; refuse rather
		// than risk misboxing one on a guard failure.
		if f.Type(p) == ssa.TypeI64 {
			return nil, nil, fmt.Errorf("%w: OSR i64 operand", ErrUnsupported)
		}
	}
	homes := map[int]int{}
	for block := 0; block < f.Len(); block++ {
		for _, op := range f.Block(block).Operations {
			if op.Op != ssa.OpState {
				continue
			}
			for _, frame := range op.Frames {
				if frame.Base != 0 && len(frame.Locals) > 0 {
					return nil, nil, fmt.Errorf("%w: outer-frame deopt", ErrUnsupported)
				}
				for _, local := range frame.Locals {
					if _, ok := homes[local.Index]; !ok {
						homes[local.Index] = len(homes)
					}
				}
			}
		}
	}
	l := &lowering{
		f: f, m: m, a: asm.New(m.Arch()), fn: fn, address: address, osr: osr, count: count, objects: objects,
		states: map[ssa.Value]ssa.Operation{}, consts: map[ssa.Value]types.Boxed{}, raw: map[ssa.Value]bool{},
		borrow: borrowed(f), homes: homes,
	}
	l.a.Reserve(m.Reserve()...)
	l.a.ReserveSlots(len(homes))
	if err := l.function(); err != nil {
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

// borrowed reports, for every OpConst ref value f retains exactly once and
// uses exactly once as some CALL's callee, that the retain is redundant: the
// constant pool already holds the callee alive for the call's own duration.
func borrowed(f *ssa.Function) map[ssa.Value]bool {
	retains, uses, callees := map[ssa.Value]int{}, map[ssa.Value]int{}, map[ssa.Value]bool{}
	use := func(args []ssa.Value) {
		for _, v := range args {
			uses[v]++
		}
	}
	for id := 0; id < f.Len(); id++ {
		b := f.Block(id)
		for _, op := range b.Operations {
			switch op.Op {
			case ssa.OpRetain:
				retains[op.Args[0]]++
			case ssa.OpRelease:
			default:
				use(op.Args)
				if op.Op == ssa.OpExec && op.Code == instr.CALL && len(op.Args) > 0 {
					callees[op.Args[len(op.Args)-1]] = true
				}
			}
		}
		use(b.Terminator.Args)
		for _, e := range b.Terminator.Edges {
			use(e.Args)
		}
	}
	out := map[ssa.Value]bool{}
	for v := range callees {
		if retains[v] == 1 && uses[v] == 1 {
			out[v] = true
		}
	}
	return out
}

func (l *lowering) function() error {
	order := graph.Order(l.f)
	labels := make([]asm.Label, l.f.Len())
	for _, block := range order {
		labels[block] = l.a.Label()
	}
	headers := graph.Headers(l.f, graph.NewDominance(l.f))

	kinds := l.fn.Slots()
	params := 0
	if l.fn.Typ != nil {
		params = len(l.fn.Typ.Params)
	}
	if l.osr {
		// Every slot is a live local at a header, not just the params the
		// function was called with: the prologue must clear none of them.
		params = len(kinds)
	}
	l.m.Prologue(l.a, kinds, params, l.count, l.address)
	if l.osr {
		if err := l.preload(); err != nil {
			return err
		}
	}
	for _, block := range order {
		b := l.f.Block(block)
		l.a.Bind(labels[block])
		counted := !slices.Contains(headers, block)
		for _, op := range b.Operations {
			if !counted && op.State != ssa.NoValue {
				l.op = op
				safepoint, resume := l.stub(l.exit(jit.ExitSafepoint))
				l.m.Budget(l.a, safepoint)
				l.a.Bind(resume)
				counted = true
			}
			if err := l.operation(op); err != nil {
				return err
			}
		}
		if !counted {
			return fmt.Errorf("%w: loop header %d without state", ErrUnsupported, block)
		}
		if l.pending != ssa.NoValue {
			if err := l.flush(); err != nil {
				return err
			}
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
		l.emit(s.id)
		if l.exits[s.id].Kind != jit.ExitDeopt {
			l.m.Branch(l.a, ssa.Terminator{Op: ssa.OpJump}, l, []asm.Label{s.resume})
		}
	}
	l.m.Epilogue(l.a)
	return l.err
}

// preload loads block 0's parameters from the operand-stack slots the
// interpreter left them at (translate.go roots an OSR unit at the header,
// block 0's parameters bottom first at len(fn.Slots())+i), boxed exactly as
// OpLoad unboxes a slot: Lower already refused any i64 one.
func (l *lowering) preload() error {
	slots := len(l.fn.Slots())
	for i, p := range l.f.Block(0).Params {
		op := ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: slots + i}, Results: []ssa.Value{p}}
		if !l.m.Lower(l.a, op, l) {
			return fmt.Errorf("%w: OSR parameter %d", ErrUnsupported, i)
		}
	}
	return nil
}

func (l *lowering) operation(op ssa.Operation) error {
	if l.pending != ssa.NoValue && op.Op != ssa.OpState && !l.calls(op) {
		if err := l.flush(); err != nil {
			return err
		}
	}
	if op.Op != ssa.OpGuardKind {
		if err := l.validate(op.Args); err != nil {
			return err
		}
	}
	l.op = op
	switch op.Op {
	case ssa.OpState:
		l.states[op.Results[0]] = op
		return nil
	case ssa.OpConst:
		l.consts[op.Results[0]] = op.Const
	case ssa.OpLoad:
		if l.f.Type(op.Results[0]) == ssa.TypeI64 {
			l.raw[op.Results[0]] = true
		}
	case ssa.OpGuardKind:
		if !l.raw[op.Args[0]] {
			l.m.Move(l.a, l.Reg(op.Results[0]), l.Reg(op.Args[0]))
			return nil
		}
	case ssa.OpRetain:
		if l.borrow[op.Args[0]] {
			l.pending = op.Args[0]
			return nil
		}
	case ssa.OpExec:
		if op.Code == instr.CALL {
			return l.call(op)
		}
		if l.m.Lower(l.a, op, l) {
			l.deopt()
			return l.err
		}
		results := make([]asm.VReg, len(op.Results))
		for i, v := range op.Results {
			results[i] = l.Reg(v)
		}
		id := l.exit(jit.ExitBridge)
		l.emit(id)
		l.m.Results(l.a, results)
		return l.err
	case ssa.OpStore, ssa.OpRelease, ssa.OpGuardShape:
	default:
		return fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
	}
	if !l.m.Lower(l.a, op, l) {
		return fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
	}
	l.deopt()
	return l.err
}

func (l *lowering) deopt() {
	if len(l.deopts) == 0 {
		return
	}
	resume := l.a.Label()
	l.m.Branch(l.a, ssa.Terminator{Op: ssa.OpJump}, l, []asm.Label{resume})
	for _, stub := range l.deopts {
		l.a.Bind(stub.label)
		l.emit(stub.id)
	}
	l.a.Bind(resume)
	l.deopts = nil
}

// calls reports whether op is the CALL that consumes l.pending as its
// callee: the only shape a borrow-candidate retain feeds. OpState never
// intervenes (operation skips it above): it records a snapshot and emits no
// row, so it never places an exit map between the retain and its call.
func (l *lowering) calls(op ssa.Operation) bool {
	return op.Op == ssa.OpExec && op.Code == instr.CALL && len(op.Args) > 0 && op.Args[len(op.Args)-1] == l.pending
}

// flush lowers a deferred retain whose call did not immediately follow it
// (an operation genuinely came between, so an exit could name the value),
// keeping today's code for that value.
func (l *lowering) flush() error {
	op := ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{l.pending}}
	delete(l.borrow, l.pending)
	l.pending = ssa.NoValue
	l.op = op
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
	l.op = ssa.Operation{State: t.State}
	switch t.Op {
	case ssa.OpReturn:
		if err := l.leave(t); err != nil {
			return err
		}
		l.m.Return(l.a, t, l)
		l.deopt()
		return l.err
	case ssa.OpComplete:
		l.m.Return(l.a, t, l)
		l.deopt()
		return l.err
	case ssa.OpExit:
		id := l.exit(jit.ExitDeopt)
		l.emit(id)
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

// validate rejects an unguarded promoted i64 slot word.
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

func (l *lowering) Slot(slot ssa.Slot) ssa.Type {
	kinds := l.fn.Slots()
	if slot.Space == ssa.SpaceLocal && slot.Index >= 0 && slot.Index < len(kinds) {
		return ssa.TypeOf(kinds[slot.Index])
	}
	if slot.Space == ssa.SpaceGlobal {
		return ssa.TypeRef
	}
	return 0
}

// Deopt places a deopt stub at the current state.
func (l *lowering) Deopt() asm.Label {
	id := l.exit(jit.ExitDeopt)
	label := l.a.Label()
	l.deopts = append(l.deopts, stub{label: label, id: id})
	return label
}

// Release places a release stub for ref.
func (l *lowering) Release(ref asm.VReg) (exit, resume asm.Label) {
	id := l.exit(jit.ExitRelease)
	e := l.exits[id]
	e.Release.Kind = types.KindRef
	l.places[id] = append(l.places[id], place{to: &e.Release, reg: ref})
	return l.stub(id)
}

// stub places the stub of exit id; a resumable one continues at resume,
// which its caller binds.
func (l *lowering) stub(id int) (exit, resume asm.Label) {
	s := stub{label: l.a.Label(), id: id}
	if l.exits[id].Kind != jit.ExitDeopt {
		s.resume = l.a.Label()
	}
	l.stubs = append(l.stubs, s)
	return s.label, s.resume
}

// call lowers a statically resolved CALL and records the post-call state.
func (l *lowering) call(op ssa.Operation) error {
	callee := op.Args[len(op.Args)-1]
	c, ok := l.consts[callee]
	if !ok || c.Kind() != types.KindRef {
		return fmt.Errorf("%w: call of v%d", ErrUnsupported, callee)
	}
	target := l.objects[c.Ref()].Function
	if target == nil || target.Typ == nil {
		return fmt.Errorf("%w: call of %d", ErrUnsupported, c.Ref())
	}
	for _, v := range op.Results {
		if l.f.Type(v) == ssa.TypeI64 {
			return fmt.Errorf("%w: i64 call result v%d", ErrUnsupported, v)
		}
	}
	state, ok := l.states[op.State]
	if !ok || len(state.Frames) == 0 {
		return fmt.Errorf("%w: call without a state", ErrUnsupported)
	}
	below := len(state.Frames[len(state.Frames)-1].Stack) - len(op.Args)
	owned := l.pending != callee
	l.pending = ssa.NoValue
	id := l.exit(jit.ExitCall)
	l.exits[id].Callee = c.Ref()
	l.exits[id].Owned = owned
	bridge, resume := l.stub(id)
	site := Call{
		Address: c.Ref(),
		Callee:  callee,
		Args:    op.Args[:len(op.Args)-1],
		Results: op.Results,
		Base:    len(l.fn.Slots()) + below,
		Size:    max(len(target.Slots()), len(target.Typ.Returns)),
		Exit:    id,
		Live:    l.live(id),
		Bridge:  bridge,
		Resume:  resume,
		Owned:   owned,
		Self:    !l.osr && c.Ref() == l.address,
	}
	if !l.m.Call(l.a, site, l) {
		return fmt.Errorf("%w: call of %d", ErrUnsupported, c.Ref())
	}
	return l.err
}

// leave checks that returning t releases nothing the interpreter's RETURN
// would: every owned operand is a result.
func (l *lowering) leave(t ssa.Terminator) error {
	state, ok := l.states[t.State]
	if !ok {
		return nil
	}
	for _, frame := range state.Frames {
		for _, o := range frame.Stack[:max(len(frame.Stack)-len(t.Args), 0)] {
			if o.Owned {
				return fmt.Errorf("%w: return over owned v%d", ErrUnsupported, o.Value)
			}
		}
	}
	return nil
}

// emit saves deferred deopt values and leaves native code through exit id.
func (l *lowering) emit(id int) {
	for _, save := range l.saves[id] {
		l.m.Spill(l.a, save.reg, save.slot)
	}
	l.m.Exit(l.a, id, l.exits[id].Kind, l.live(id))
}

// exit records the map of an exit of kind k at the current operation's state.
func (l *lowering) exit(k jit.Kind) int {
	id := len(l.exits)
	e := &jit.Exit{Kind: k}
	l.exits = append(l.exits, e)
	l.places = append(l.places, nil)
	l.saves = append(l.saves, nil)

	if k == jit.ExitRelease {
		return id
	}
	state, ok := l.states[l.op.State]
	if !ok {
		l.fail(fmt.Errorf("%w: %s exits without a state", ErrUnsupported, l.op.Op))
		return id
	}
	e.Frames = make([]jit.Frame, len(state.Frames))
	for i, frame := range state.Frames {
		f := &e.Frames[i]
		*f = jit.Frame{Address: frame.Address, Base: frame.Base, IP: frame.IP, Returns: frame.Returns}
		stack := frame.Stack
		if k == jit.ExitCall && i == len(state.Frames)-1 {
			stack = stack[:len(stack)-len(l.op.Args)]
			f.IP += instr.Instruction(l.fn.Code[f.IP:]).Width()
		}
		if len(stack) > 0 {
			f.Stack = make([]jit.Operand, len(stack))
		}
		for j, o := range stack {
			// A borrowed callee's retain was never emitted: the materializer
			// must retain it itself.
			f.Stack[j].Owned = o.Owned && !l.borrow[o.Value]
			l.place(id, &f.Stack[j].Value, o.Value)
		}
		if len(frame.Locals) > 0 {
			f.Locals = make([]jit.Local, len(frame.Locals))
		}
		for j, local := range frame.Locals {
			to := &f.Locals[j]
			to.Index = local.Index
			// A call's map also describes the caller while a callee runs, so
			// its promoted locals must stay live across the call itself.
			if k == jit.ExitCall || slices.Contains(l.op.Args, local.Value) {
				l.place(id, &to.Value, local.Value)
				continue
			}
			slot, ok := l.homes[local.Index]
			if !ok {
				l.fail(fmt.Errorf("%w: deopt local %d has no home", ErrUnsupported, local.Index))
				continue
			}
			if l.raw[local.Value] {
				l.fail(fmt.Errorf("%w: exit %d names unguarded i64 slot word v%d", ErrUnsupported, id, local.Value))
				continue
			}
			to.Value = jit.Value{Kind: l.f.Type(local.Value).Kind(), Loc: asm.Loc{Slot: slot, Spilled: true}}
			l.saves[id] = append(l.saves[id], save{reg: l.Reg(local.Value), slot: slot})
		}
	}
	if k == jit.ExitBridge {
		e.Code = l.op.Code
		e.Adopts = transform.Adopts(l.op.Code, len(l.op.Args))
		e.Pops = len(l.op.Args)
	}
	if k == jit.ExitBridge || k == jit.ExitCall {
		for _, v := range l.op.Results {
			e.Results = append(e.Results, l.f.Type(v).Kind())
		}
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
