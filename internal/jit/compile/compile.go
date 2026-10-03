// Package compile lowers SSA functions to native code through a target Machine.
package compile

import (
	"fmt"
	"maps"
	"slices"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

type lowering struct {
	f       *ssa.Function
	m       Machine
	a       *asm.Assembler
	fn      *types.Function
	address int
	// osr reports whether this unit is rooted at a loop header instead of
	// its function's own entry.
	osr   bool
	count bool
	// headers are f's loop headers and loops marks every block in a loop
	// body; gate rejects a bridge or box exit outside them (see scan); block
	// is the block being lowered.
	headers []int
	loops   map[int]bool
	gate    bool
	block   int
	// upvals reports that f reads or writes its upvals.
	upvals  bool
	objects transform.Objects
	states  map[ssa.Value]ssa.Operation
	consts  map[ssa.Value]uint64
	raw     map[ssa.Value]bool
	homes   map[int]int
	// outlets hold one build state per exit, in exit id order; exit ids
	// index it instead of parallel slices.
	outlets []outlet
	// deopts holds the deopt stubs placed since the last terminator that
	// does not fall through, where deopt emits them.
	deopts []stub
	edges  []edge
	stubs  []stub
	err    error
	// enter is the Go entry stub Enter binds, resolved to a byte offset
	// after Build.
	enter asm.Label
	// remats holds each constant loaded at each use (see remat).
	remats map[ssa.Value]bool
	// tmp is the next one-use register id, past the SSA values and the four
	// ids scratch reserves.
	tmp int32
	// args holds each register-passed parameter's incoming value while it
	// still equals its slot: only during block 0, which runs once per
	// activation (see rotate), and until an OpStore to that slot. Cleared
	// entries and every later block read the slot instead.
	args []asm.VReg
	// param marks an OpLoad result that read a register-passed i64
	// parameter (see args): already the raw unboxed payload, so its
	// guard.kind moves instead of unboxing it.
	param map[ssa.Value]bool

	// uses counts each value's uses: operation, terminator, and edge
	// arguments, and the frames of every state.
	uses map[ssa.Value]int
	// fuse is the condition of the OpBranch ending the block being lowered
	// when the block's last operation defines it and nothing else uses it.
	fuse ssa.Value

	// op is the operation or terminator being lowered.
	op ssa.Operation
}

// place is where exit map value to lives in the rows: register reg.
type place struct {
	to  *jit.Value
	reg asm.VReg
}

// outlet is one exit under construction: its published map plus the rows
// its stub still has to emit. memo is an ExitCall map's one register per
// remat constant; stalls holds remat constants materialized at the stub;
// results are the registers a resumed exit loads from Context.Results.
type outlet struct {
	exit    *jit.Exit
	places  []place
	saves   []save
	stalls  []stall
	memo    map[ssa.Value]asm.VReg
	results []asm.VReg
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

// stall is an exit map entry for remat constant v, resolved at the stub.
type stall struct {
	to *jit.Value
	v  ssa.Value
}

// Lower emits code for f and its exit maps. address is the unit's own
// function address: a call to it is its own recursion. count requests its
// function-entry hotness counter. An OSR unit's block
// 0 may carry parameters, the operand stack the interpreter left at its
// header (transform.Translate roots there); Lower loads them itself (see
// preload) instead of rejecting them.
func Lower(f *ssa.Function, m Machine, fn *types.Function, objects transform.Objects, address int, osr, count bool) ([]byte, []jit.Exit, int, error) {
	l, err := newLowering(f, m, fn, objects, address, osr, count)
	if err != nil {
		return nil, nil, 0, err
	}
	if err := l.function(); err != nil {
		return nil, nil, 0, err
	}
	code, err := l.a.Build()
	if err != nil {
		return nil, nil, 0, err
	}
	entry, ok := l.a.Offset(l.enter)
	if !ok {
		return nil, nil, 0, fmt.Errorf("%w: unresolved entry stub", ErrUnsupported)
	}
	exits := make([]jit.Exit, len(l.outlets))
	for id, o := range l.outlets {
		for _, p := range o.places {
			loc, ok := l.a.Loc(p.reg)
			if !ok {
				return nil, nil, 0, fmt.Errorf("%w: exit %d names v%d with no location", ErrUnsupported, id, p.reg.ID())
			}
			p.to.Loc = loc
		}
		exits[id] = *o.exit
	}
	return code, exits, entry, nil
}

// Reg is v's virtual register, typed by its static representation; for a
// remat constant, a fresh register loaded here.
func (l *lowering) Reg(v ssa.Value) asm.VReg {
	if c, ok := l.remat(v); ok {
		return l.materialize(v, c)
	}
	return vreg(int32(v), l.f.Type(v))
}

// Type is v's static type.
func (l *lowering) Type(v ssa.Value) ssa.Type {
	return l.f.Type(v)
}

// Slot returns the static type of slot.
func (l *lowering) Slot(slot ssa.Slot) ssa.Type {
	kinds := l.fn.Slots()
	if slot.Space == ssa.SpaceLocal && slot.Index >= 0 && slot.Index < len(kinds) {
		return ssa.TypeOf(kinds[slot.Index])
	}
	if slot.Space == ssa.SpaceGlobal {
		return ssa.TypeRef
	}
	if slot.Space == ssa.SpaceUpval && slot.Index >= 0 && slot.Index < len(l.fn.Captures) {
		return ssa.TypeOf(l.fn.Captures[slot.Index].Kind())
	}
	return 0
}

// Const reports the word of v when an OpConst defines it.
func (l *lowering) Const(v ssa.Value) (uint64, bool) {
	word, ok := l.consts[v]
	return word, ok
}

// Fuse reports whether v is the condition of the OpBranch ending the block
// being lowered, defined by the block's last operation and used nowhere else.
func (l *lowering) Fuse(v ssa.Value) bool {
	return v == l.fuse
}

// Deopt places a deopt stub at the current state, out of line after the
// next terminator that does not fall through.
func (l *lowering) Deopt() asm.Label {
	s := stub{label: l.a.Label(), id: l.exit(jit.ExitDeopt)}
	l.deopts = append(l.deopts, s)
	return s.label
}

// Trap places a deopt stub as Deopt does, marked as the operation's own trap.
func (l *lowering) Trap() asm.Label {
	label := l.Deopt()
	l.outlets[l.deopts[len(l.deopts)-1].id].exit.Trap = true
	return label
}

// Release places a release stub for ref.
func (l *lowering) Release(ref asm.VReg) (exit, resume asm.Label) {
	return l.word(jit.ExitRelease, types.KindRef, ref)
}

// Box places a box stub for word, a wide i64.
func (l *lowering) Box(word asm.VReg) (exit, resume asm.Label) {
	return l.word(jit.ExitBox, types.KindI64, word)
}

// word places a stub of kind k whose Word is reg, of kind kind.
func (l *lowering) word(k jit.Kind, kind types.Kind, reg asm.VReg) (exit, resume asm.Label) {
	id := l.exit(k)
	out := &l.outlets[id]
	out.exit.Word.Kind = kind
	out.places = append(out.places, place{to: &out.exit.Word, reg: reg})
	return l.stub(id)
}

// newLowering rejects shapes the backend does not lower and scans f for what
// lowering consults: homes, loops, uses, and remat constants.
func newLowering(f *ssa.Function, m Machine, fn *types.Function, objects transform.Objects, address int, osr, count bool) (*lowering, error) {
	if f.Len() == 0 {
		return nil, fmt.Errorf("%w: function shape", ErrUnsupported)
	}
	if !osr && len(f.Block(0).Params) > 0 {
		return nil, fmt.Errorf("%w: entry parameters", ErrUnsupported)
	}
	for _, p := range f.Block(0).Params {
		// A not-yet-loaded i64 param would need every later block-0 param's
		// deopt map to distinguish "raw slot word" from "guarded" by
		// position, which this backend does not implement; refuse rather
		// than risk misboxing one on a guard failure.
		if f.Type(p) == ssa.TypeI64 {
			return nil, fmt.Errorf("%w: OSR i64 operand", ErrUnsupported)
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
					return nil, fmt.Errorf("%w: outer-frame deopt", ErrUnsupported)
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
		states: map[ssa.Value]ssa.Operation{}, consts: map[ssa.Value]uint64{}, raw: map[ssa.Value]bool{},
		homes: homes, tmp: int32(f.Values()) + 4,
		remats: map[ssa.Value]bool{},
		param:  map[ssa.Value]bool{}, uses: map[ssa.Value]int{},
	}
	l.scan()
	l.a.Reserve(m.Reserve()...)
	l.a.ReserveSlots(len(homes))
	return l, nil
}

// scan fills headers, loops, gate, upvals, uses, and remats. A scalar or ref
// constant is rematerialized when f has a call and no loop block uses it: a
// loop keeps its constants in registers.
func (l *lowering) scan() {
	f := l.f
	dom := graph.NewDominance(f)
	l.headers = graph.Headers(f, dom)
	l.loops = map[int]bool{}
	for _, h := range l.headers {
		maps.Copy(l.loops, graph.Body(f, dom, h))
	}
	// A unit entered at ip 0 of code with loops gains only its prefix's
	// dispatch, which a Go round trip outweighs: it bridges only in loops.
	entry := f.Entry().IP
	l.gate = l.osr && entry == 0 && len(l.headers) > 0
	if l.gate {
		if bytecode, err := analysis.Headers(l.fn); err == nil && slices.Contains(bytecode, entry) {
			l.gate = false
		}
	}
	caller := false
	looped := map[ssa.Value]bool{}
	for id := 0; id < f.Len(); id++ {
		b := f.Block(id)
		mark := func(args []ssa.Value) {
			for _, v := range args {
				l.uses[v]++
				if l.loops[id] {
					looped[v] = true
				}
			}
		}
		for _, op := range b.Operations {
			caller = caller || op.Op == ssa.OpExec && op.Code == instr.CALL
			l.upvals = l.upvals || (op.Op == ssa.OpLoad || op.Op == ssa.OpStore) && op.Slot.Space == ssa.SpaceUpval
			mark(op.Args)
			for _, frame := range op.Frames {
				for _, o := range frame.Stack {
					l.uses[o.Value]++
				}
				for _, local := range frame.Locals {
					l.uses[local.Value]++
				}
			}
		}
		mark(b.Terminator.Args)
		for _, e := range b.Terminator.Edges {
			mark(e.Args)
		}
	}
	if !caller {
		return
	}
	for id := 0; id < f.Len(); id++ {
		for _, op := range f.Block(id).Operations {
			if op.Op != ssa.OpConst || looped[op.Results[0]] {
				continue
			}
			switch f.Type(op.Results[0]) {
			case ssa.TypeF32, ssa.TypeF64:
			default:
				l.remats[op.Results[0]] = true
			}
		}
	}
}

// remat reports v's constant and whether it is loaded at each use instead of
// held in one register a call would force to spill.
func (l *lowering) remat(v ssa.Value) (uint64, bool) {
	c, ok := l.consts[v]
	return c, ok && l.remats[v]
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

func (l *lowering) function() error {
	order := graph.Order(l.f)
	labels := make([]asm.Label, l.f.Len())
	for _, block := range order {
		labels[block] = l.a.Label()
	}

	// Every slot is a live local at an OSR header, not just the params the
	// function was called with: the prologue must start none of them.
	var zeros []types.Boxed
	var args []types.Kind
	var borrows []bool
	if !l.osr {
		zeros = types.Zeros(l.fn.Locals)
		args = arguments(l.fn)
		borrows = transform.Borrows(l.fn)
	}
	layout := Layout{Kinds: l.fn.Slots(), Zeros: zeros, Arguments: args, Registers: registers(l.fn), Borrows: borrows, Upvals: l.upvals}
	l.args = make([]asm.VReg, len(args))
	for i, k := range args {
		l.args[i] = l.fresh(ssa.TypeOf(k))
	}
	l.m.Prologue(l.a, l.address, l.count, layout, l.args)
	if l.osr {
		if err := l.preload(); err != nil {
			return err
		}
	}
	for i, block := range order {
		l.block = block
		b := l.f.Block(block)
		l.a.Bind(labels[block])
		l.fuse = ssa.NoValue
		if n := len(b.Operations); n > 0 && b.Terminator.Op == ssa.OpBranch {
			if results := b.Operations[n-1].Results; len(results) == 1 && results[0] == b.Terminator.Args[0] && l.uses[results[0]] == 1 {
				l.fuse = results[0]
			}
		}
		counted := !slices.Contains(l.headers, block)
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
		next := l.a.Label()
		if i+1 < len(order) {
			next = labels[order[i+1]]
		}
		if err := l.terminator(b.Terminator, labels, next); err != nil {
			return err
		}
		l.args = nil
	}
	for _, e := range l.edges {
		l.a.Bind(e.label)
		l.shuffle(e.moves)
		l.jump(labels[e.block])
	}
	for _, s := range l.stubs {
		l.a.Bind(s.label)
		l.emit(s.id)
		if l.outlets[s.id].exit.Kind.Resumes() {
			l.jump(s.resume)
		}
	}
	l.m.Epilogue(l.a)
	l.enter = l.m.Enter(l.a, layout)
	return l.err
}

// registers reports fn's register-convention results: at most two, of any
// kind (an i64 one stays raw; Go boxes it). A function outside that shape
// returns nil, so Return keeps boxing results to slots and Enter's stub
// boxes nothing beyond the slot layout. The same static fact governs every unit
// and tier of fn, so a caller compiled separately from its callee always
// agrees with it.
func registers(fn *types.Function) []types.Kind {
	if fn == nil || fn.Typ == nil {
		return nil
	}
	return convention(fn.Typ.Returns)
}

// arguments reports fn's register-convention parameters: at most two, of any
// kind (an i64 one stays raw), in the target's register-convention registers
// like registers' results. Every other function passes its arguments through
// slots alone. Caller and callee agree on this static fact of fn at every
// unit and tier.
func arguments(fn *types.Function) []types.Kind {
	if fn == nil || fn.Typ == nil {
		return nil
	}
	return convention(fn.Typ.Params)
}

// convention is the kinds of ts when up to jit.Convention values travel in
// registers.
func convention(ts []types.Type) []types.Kind {
	if len(ts) == 0 || len(ts) > jit.Convention {
		return nil
	}
	return types.Kinds(ts)
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
		if _, ok := l.remat(op.Results[0]); ok {
			// Reg loads it at each use.
			return nil
		}
	case ssa.OpLoad:
		if i, ok := l.argument(op.Slot); ok {
			l.m.Move(l.a, l.Reg(op.Results[0]), l.args[i])
			if l.f.Type(op.Results[0]) == ssa.TypeI64 {
				l.raw[op.Results[0]] = true
				l.param[op.Results[0]] = true
			}
			return nil
		}
		if l.f.Type(op.Results[0]) == ssa.TypeI64 {
			l.raw[op.Results[0]] = true
		}
	case ssa.OpGuardKind:
		// A register-passed i64 parameter's load already holds the raw
		// unboxed payload (see param): its guard moves instead of unboxing.
		if l.param[op.Args[0]] {
			l.m.Move(l.a, l.Reg(op.Results[0]), l.Reg(op.Args[0]))
			return nil
		}
		// Only a slot word reaches a guard (promote aliases the rest away);
		// unboxing a raw int would corrupt it.
		if !l.raw[op.Args[0]] {
			return fmt.Errorf("%w: guard.kind of raw int", ErrUnsupported)
		}
	case ssa.OpExec:
		if op.Code == instr.CALL {
			return l.call(op)
		}
		if l.m.Lower(l.a, op, l) {
			return l.err
		}
		l.emit(l.exit(jit.ExitBridge))
		return l.err
	case ssa.OpStore:
		if i, ok := l.argument(op.Slot); ok {
			l.args[i] = asm.VReg{}
		}
	case ssa.OpRetain, ssa.OpRelease, ssa.OpGuardShape, ssa.OpGuardValue, ssa.OpSlice:
	default:
		return fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
	}
	if !l.m.Lower(l.a, op, l) {
		return fmt.Errorf("%w: %s", ErrUnsupported, op.Op)
	}
	return l.err
}

// argument reports the index of the register-passed parameter slot names
// while its incoming register value is still current (see args).
func (l *lowering) argument(slot ssa.Slot) (int, bool) {
	if slot.Space != ssa.SpaceLocal || slot.Base != 0 || slot.Index < 0 || slot.Index >= len(l.args) {
		return 0, false
	}
	return slot.Index, l.args[slot.Index] != asm.VReg{}
}

// terminator lowers t, which falls through to next. An edge that moves
// values gets a stub of its own, so the branch itself never moves anything.
func (l *lowering) terminator(t ssa.Terminator, labels []asm.Label, next asm.Label) error {
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
		l.deopt()
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
		l.m.Branch(l.a, t, l, []asm.Label{edges[0].label}, next)
		if edges[0].label != next {
			l.deopt()
		}
		return nil
	}
	targets := make([]asm.Label, len(edges))
	for i := range edges {
		if moved {
			edges[i].label = l.a.Label()
		}
		targets[i] = edges[i].label
	}
	l.m.Branch(l.a, t, l, targets, next)
	if moved {
		l.edges = append(l.edges, edges...)
	}
	if !slices.Contains(targets, next) {
		l.deopt()
	}
	return nil
}

// deopt emits the pending deopt stubs where no row falls through into them:
// near the checks that take them, so the values their maps name stay live
// only that far.
func (l *lowering) deopt() {
	for _, s := range l.deopts {
		l.a.Bind(s.label)
		l.emit(s.id)
	}
	l.deopts = nil
}

// jump branches to label from stub code, whose next row is never label.
func (l *lowering) jump(label asm.Label) {
	next := l.a.Label()
	l.m.Branch(l.a, ssa.Terminator{Op: ssa.OpJump}, l, []asm.Label{label}, next)
	l.a.Bind(next)
}

// fresh returns an unshared register of type t.
func (l *lowering) fresh(t ssa.Type) asm.VReg {
	id := l.tmp
	l.tmp++
	return vreg(id, t)
}

// vreg is register id typed by t's static representation: its bank and
// width.
func vreg(id int32, t ssa.Type) asm.VReg {
	switch t {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		return asm.NewVReg(id, asm.RegTypeInt, asm.Width32)
	case ssa.TypeI64, ssa.TypeRef:
		return asm.NewVReg(id, asm.RegTypeInt, asm.Width64)
	case ssa.TypeF32:
		return asm.NewVReg(id, asm.RegTypeFloat, asm.Width32)
	case ssa.TypeF64:
		return asm.NewVReg(id, asm.RegTypeFloat, asm.Width64)
	default:
		return asm.VReg{}
	}
}

// materialize loads word for v into a fresh register.
func (l *lowering) materialize(v ssa.Value, word uint64) asm.VReg {
	reg := l.fresh(l.f.Type(v))
	l.m.Const(l.a, reg, word)
	return reg
}

// stub places the stub of exit id; a resumable one continues at resume,
// which its caller binds.
func (l *lowering) stub(id int) (exit, resume asm.Label) {
	s := stub{label: l.a.Label(), id: id}
	if l.outlets[id].exit.Kind.Resumes() {
		s.resume = l.a.Label()
	}
	l.stubs = append(l.stubs, s)
	return s.label, s.resume
}

// call lowers a CALL of a constant function, of the closure its Shape
// names, or, when its Shape names only a function type, of any callee of that
// type, and records the post-call state. A function with captures runs only
// through its closure: called directly, it has no upvals to read.
func (l *lowering) call(op ssa.Operation) error {
	callee := op.Args[len(op.Args)-1]
	generic := op.Shape.Function == 0 && op.Shape.Type != 0
	ref, closure := op.Shape.Function, op.Shape.Function != 0
	var target *types.Function
	if !generic {
		if !closure {
			c, ok := l.consts[callee]
			if !ok || l.f.Type(callee) != ssa.TypeRef {
				return fmt.Errorf("%w: call of v%d", ErrUnsupported, callee)
			}
			ref = types.Boxed(c).Ref()
		}
		target = l.objects[ref].Function
		if target == nil || target.Typ == nil || len(target.Captures) > 0 && !closure {
			return fmt.Errorf("%w: call of %d", ErrUnsupported, ref)
		}
	}
	regs := registers(target)
	if regs == nil {
		for _, v := range op.Results {
			if l.f.Type(v) == ssa.TypeI64 {
				return fmt.Errorf("%w: i64 call result v%d", ErrUnsupported, v)
			}
		}
	}
	state, ok := l.states[op.State]
	if !ok || len(state.Frames) == 0 {
		return fmt.Errorf("%w: call without a state", ErrUnsupported)
	}
	frame := state.Frames[len(state.Frames)-1]
	below := len(frame.Stack) - len(op.Args)
	if below < 0 {
		return fmt.Errorf("%w: call state without its operands", ErrUnsupported)
	}
	// The state's top entry is the callee operand, owned only when
	// translation retained it.
	owned := frame.Stack[len(frame.Stack)-1].Owned
	id := l.exit(jit.ExitCall)
	out := &l.outlets[id]
	out.exit.Callee = ref
	out.exit.Args = len(op.Args) - 1
	out.exit.Owned = owned
	if closure || generic {
		out.exit.Target = &jit.Value{}
		l.place(id, out.exit.Target, callee)
	}
	if generic {
		for _, v := range op.Results {
			out.exit.Returns = append(out.exit.Returns, l.f.Type(v).Kind())
		}
	}
	for j, b := range transform.Borrows(target) {
		switch {
		case !b:
		case frame.Stack[below+j].Owned:
			out.exit.Kept = append(out.exit.Kept, j)
		default:
			out.exit.Lent = append(out.exit.Lent, j)
		}
	}
	if regs != nil {
		for _, v := range op.Results {
			out.exit.Results = append(out.exit.Results, l.f.Type(v).Kind())
			out.results = append(out.results, l.Reg(v))
		}
	}
	bridge, join := l.stub(id)
	site := Call{
		Callee:    ref,
		Target:    callee,
		Args:      op.Args[:len(op.Args)-1],
		Results:   op.Results,
		Base:      len(l.fn.Slots()) + below,
		Exit:      id,
		Stub:      bridge,
		Join:      join,
		Owned:     owned,
		Registers: regs,
		Arguments: arguments(target),
		Generic:   generic,
	}
	if generic {
		site.Size = max(len(op.Args)-1, len(op.Results))
	} else {
		sid := l.exit(jit.ExitSafepoint)
		site.Safepoint, site.Resume = l.stub(sid)
		site.Size = max(len(target.Slots()), len(target.Typ.Returns))
		site.Self = !l.osr && ref == l.address
		site.Upvals = closure && len(target.Captures) > 0
	}
	site.Live = l.live(id)
	if !l.m.Call(l.a, site, l) {
		return fmt.Errorf("%w: call of %d", ErrUnsupported, ref)
	}
	return l.err
}

// leave rejects owned values the native return path would not release.
func (l *lowering) leave(t ssa.Terminator) error {
	state, ok := l.states[t.State]
	if !ok || len(state.Frames) == 0 {
		return nil
	}
	frame := state.Frames[len(state.Frames)-1]
	for _, o := range frame.Stack[:max(len(frame.Stack)-len(t.Args), 0)] {
		if o.Owned {
			return fmt.Errorf("%w: return over owned v%d", ErrUnsupported, o.Value)
		}
	}
	return nil
}

// emit resolves deferred remat constants, saves deferred deopt values,
// leaves native code through exit id, and loads its results on resume.
func (l *lowering) emit(id int) {
	out := &l.outlets[id]
	for _, s := range out.stalls {
		reg := l.materialize(s.v, l.consts[s.v])
		out.places = append(out.places, place{to: s.to, reg: reg})
	}
	for _, save := range out.saves {
		l.m.Spill(l.a, save.reg, save.slot)
	}
	l.m.Exit(l.a, id, out.exit.Kind, l.live(id))
	if len(out.results) > 0 {
		l.m.Results(l.a, out.results)
	}
}

// exit records the map of an exit of kind k at the current operation's state.
func (l *lowering) exit(k jit.Kind) int {
	if l.gate && !l.loops[l.block] && (k == jit.ExitBridge || k == jit.ExitBox) {
		l.fail(fmt.Errorf("%w: prefix %s exit outside every loop", ErrUnsupported, k))
	}
	id := len(l.outlets)
	e := &jit.Exit{Kind: k}
	l.outlets = append(l.outlets, outlet{exit: e})

	if k == jit.ExitRelease {
		return id
	}
	state, ok := l.states[l.op.State]
	if !ok {
		l.fail(fmt.Errorf("%w: %s exits without a state", ErrUnsupported, l.op.Op))
		return id
	}
	frame := state.Frames[len(state.Frames)-1]
	f := &e.Frame
	*f = jit.Frame{Address: frame.Address, IP: frame.IP, Returns: frame.Returns}
	stack := frame.Stack
	if k == jit.ExitCall {
		stack = stack[:len(stack)-len(l.op.Args)]
		f.IP += instr.Instruction(l.fn.Code[f.IP:]).Width()
	}
	if len(stack) > 0 {
		f.Stack = make([]jit.Operand, len(stack))
	}
	for j, o := range stack {
		f.Stack[j].Owned = o.Owned
		l.place(id, &f.Stack[j].Value, o.Value)
	}
	if len(frame.Locals) > 0 {
		f.Locals = make([]jit.Local, len(frame.Locals))
	}
	for j, local := range frame.Locals {
		to := &f.Locals[j]
		to.Index = local.Index
		// A call and a resumable safepoint keep promoted locals live across
		// the native round trip; deopt-only exits can restore them from homes.
		if k == jit.ExitCall || k == jit.ExitSafepoint || slices.Contains(l.op.Args, local.Value) {
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
		l.outlets[id].saves = append(l.outlets[id].saves, save{reg: l.Reg(local.Value), slot: slot})
	}
	if k == jit.ExitBridge {
		e.Code = l.op.Code
		e.Pops = len(l.op.Args)
		e.Adopts = transform.Adopts(l.op.Code, len(l.op.Args), len(l.op.Results))
		for _, v := range l.op.Results {
			e.Results = append(e.Results, l.f.Type(v).Kind())
			l.outlets[id].results = append(l.outlets[id].results, l.Reg(v))
		}
	}
	return id
}

func (l *lowering) place(id int, to *jit.Value, v ssa.Value) {
	if l.raw[v] {
		l.fail(fmt.Errorf("%w: exit %d names unguarded i64 slot word v%d", ErrUnsupported, id, v))
	}
	to.Kind = l.f.Type(v).Kind()
	out := &l.outlets[id]
	if _, ok := l.remat(v); ok {
		// An ExitCall map is also an outer activation's map, read from its
		// spill slot by a deeper trap: the register must live across the
		// call, one per value.
		if out.exit.Kind == jit.ExitCall {
			if out.memo == nil {
				out.memo = map[ssa.Value]asm.VReg{}
			}
			reg, ok := out.memo[v]
			if !ok {
				reg = l.Reg(v)
				out.memo[v] = reg
			}
			out.places = append(out.places, place{to: to, reg: reg})
			return
		}
		// Any other map is read only at its own stub.
		out.stalls = append(out.stalls, stall{to: to, v: v})
		return
	}
	out.places = append(out.places, place{to: to, reg: l.Reg(v)})
}

func (l *lowering) live(id int) []asm.VReg {
	var regs []asm.VReg
	for _, p := range l.outlets[id].places {
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
