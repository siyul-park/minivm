package backend

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/frontend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
)

// Code is what one Compile produced that the emitted instructions do not
// already say: the order it laid the blocks out in, the exit descriptors it
// registered, in the order journal.CellExitID counts them, and the bridge
// resume points its callable may be re-entered at. The instructions
// themselves stay in the asm.Assembler the caller supplied, which is what
// allocates and encodes them.
type Code struct {
	Order   []int
	Exits   []jit.Exit
	Bridges []Bridge
}

// Bridge is one external re-entry an OpBridge creates: the IP a fresh call may
// arrive with - the bridged opcode's own IP plus its encoded width, which is
// where the interpreter leaves execution once it has run that opcode - and the
// block laid out at it. A machine's entry dispatch turns the IP the journal
// carries into a branch to that block's label; jit.Entry.Resumable is these
// IPs (see docs/jit-internals.md, Bridge).
type Bridge struct {
	IP    int
	Block int
}

// Move is one register copy a block-parameter edge requires: the parameter's
// register takes the argument's. Compiler.Moves returns them already ordered,
// with a temporary parked in front of any cycle, so a machine emits them in
// sequence and schedules nothing itself.
type Move struct {
	Dst asm.VReg
	Src asm.VReg
}

// Compiler is the architecture-neutral half of one compile, and the surface a
// Lowering asks everything of. It owns the block layout, the register bound to
// each value, the copies an edge needs, and the journal words a deopt writes;
// it owns no instruction.
type Compiler struct {
	asm   *asm.Assembler
	input *jit.Input
	root  jit.Anchor
	fn    *ssa.Function

	order   []int
	next    []int
	labels  []asm.Label
	regs    []asm.VReg
	defs    []site
	slots   map[int]int
	traps   []int
	bridges []Bridge

	exits []jit.Exit
}

// site is where a value is defined: the block holding the definition, and the
// index of the defining operation within it, or -1 for a block parameter.
type site struct {
	block int
	index int
}

// Root compiles the whole native entry anchored at root: it plans in through
// the frontend the anchor implies, lowers what that planned through m, and
// reports the entry facts a jit.Compiler publishes. The frontend order is the
// plan pipeline's own - an entry root is planned from bytecode first, because
// that covers opcodes no recording holds, and a loop root from its recording
// first, because a recording specializes the body to the path actually taken.
//
// Only the first frontend that plans the root gets an attempt, because a
// Lowering that declines leaves its own instructions in a, and a is the
// caller's to discard rather than this function's to rewind. Declining costs
// no coverage: the caller still has the plan pipeline, which tries both.
func Root(m Machine, a *asm.Assembler, in *jit.Input, root jit.Anchor) (jit.Entry, bool) {
	if m == nil || a == nil || in == nil {
		return jit.Entry{}, false
	}
	kinds := [...]prof.Frontend{prof.FrontendStatic, prof.FrontendTrace}
	if root.IP != 0 {
		kinds[0], kinds[1] = kinds[1], kinds[0]
	}
	for _, kind := range kinds {
		f := translate(kind, in, root)
		if f == nil {
			continue
		}
		code, ok := Compile(m, a, in, root, f)
		if !ok {
			return jit.Entry{}, false
		}
		entry := jit.Entry{Kind: root.Kind(), Frontend: kind, Exits: code.Exits}
		for _, bridge := range code.Bridges {
			entry.Resumable = append(entry.Resumable, bridge.IP)
		}
		return entry, true
	}
	return jit.Entry{}, false
}

// Compile lowers f, entered at root, through m into a, reporting the layout,
// exits, and bridge resume points it produced. It refuses a function m
// declined an operation of, and abandons the compile the first time m reports
// failure, exactly as an unlowerable plan leaves threaded execution installed.
// f is expected to have passed ssa.Verify and root to name the entry it was
// built for, which is the caller's boundary and not repeated here.
func Compile(m Machine, a *asm.Assembler, in *jit.Input, root jit.Anchor, f *ssa.Function) (Code, bool) {
	c, ok := newCompiler(m, a, in, root, f)
	if !ok {
		return Code{}, false
	}
	l := m.Open(c)
	if !l.Enter() {
		return Code{}, false
	}
	for _, id := range c.order {
		a.Bind(c.labels[id])
		ops := c.ops(id)
		for i := 0; i < len(ops); {
			n, ok := l.Lower(id, ops[i:])
			if !ok || n <= 0 || i+n > len(ops) {
				return Code{}, false
			}
			i += n
		}
		// A trap has already left for the interpreter, so the block has no
		// terminator to reach and no successor to fall through to.
		if c.traps[id] >= 0 {
			continue
		}
		if !l.Term(id, f.Block(id).Term) {
			return Code{}, false
		}
	}
	if !l.Leave() {
		return Code{}, false
	}
	return Code{Order: c.order, Exits: c.exits, Bridges: c.bridges}, true
}

// Asm returns the assembler this compile emits into. A machine takes
// everything the assembler owns from here - an instruction, a fresh virtual
// register for a temporary, a pin, a label - so the emitted stream has one
// owner and one reader.
func (c *Compiler) Asm() *asm.Assembler {
	return c.asm
}

// Input returns the compile-time snapshot: the constant pool, the declared
// global kinds, the resolved heap objects, and the runtime layout a lowering
// reaches the interpreter's private types through.
func (c *Compiler) Input() *jit.Input {
	return c.input
}

// Root returns the anchor this compile is entered at. Its Kind is what a
// prologue and a teardown differ by: a function entry clears the callee locals
// its callers left and leaves by returning, a module entry completes instead,
// and a loop entry re-enters a frame that is already live and must never
// unwind it. The kind is the anchor's own fact, so nothing carries it
// alongside - which is what keeps it out of ssa.Function, the IR an ahead-of-time
// optimizer with no anchors and no entries shares.
func (c *Compiler) Root() jit.Anchor {
	return c.root
}

// Bridges returns the external re-entries this function's bridges create, in
// layout order. A machine reads them in Enter, where its entry dispatch turns
// the IP a fresh call arrives with into a branch to that block's label.
func (c *Compiler) Bridges() []Bridge {
	return c.bridges
}

// Func returns the function being lowered, for the queries the Lowering hooks
// do not hand over: a value's type, and the parameters of a block an edge
// targets.
func (c *Compiler) Func() *ssa.Function {
	return c.fn
}

// Reg returns the virtual register bound to v for this whole compile, or the
// zero register for a value that holds none - the interpreter state an
// OpState defines, or a value of another function. An i1, i8, and i32 take a
// 32-bit integer register - the W lane a raw payload of that width already
// occupies, with no runtime tag - an i64 and a reference take a 64-bit one,
// an f32 a 32-bit float register, and an f64 a 64-bit one, which is the lane
// each value's representation occupies (see docs/value-representation.md); a
// machine that wants a wider or narrower view of one derives it, since a
// view is a free reinterpretation of the same underlying register (see
// bank).
func (c *Compiler) Reg(v ssa.Value) asm.VReg {
	if v <= ssa.NoValue || int(v) >= len(c.regs) {
		return asm.VReg{}
	}
	return c.regs[v]
}

// Def returns the operation defining v, and false for a block parameter,
// which no operation defines. A machine folds through it: an argument defined
// by an OpConst is an immediate its consumer may encode directly rather than a
// register it must read.
func (c *Compiler) Def(v ssa.Value) (ssa.Operation, bool) {
	if v <= ssa.NoValue || int(v) >= len(c.defs) || c.defs[v].index < 0 {
		return ssa.Operation{}, false
	}
	at := c.defs[v]
	return c.fn.Block(at.block).Ops[at.index], true
}

// Block returns the label bound at the start of block id, or the zero label
// for an id this function has no block for.
func (c *Compiler) Block(id int) asm.Label {
	if id < 0 || id >= len(c.labels) {
		return 0
	}
	return c.labels[id]
}

// Next returns the block laid out immediately after id, and false when id is
// laid out last. A terminator whose successor is that block needs no branch.
func (c *Compiler) Next(id int) (int, bool) {
	if id < 0 || id >= len(c.next) || c.next[id] < 0 {
		return 0, false
	}
	return c.next[id], true
}

// Moves returns the register copies e's arguments must make into its target
// block's parameters, in an order that reads every register before it is
// overwritten. A cycle - the swap two loop-carried values make on a back edge
// - is broken by parking one value in a fresh temporary first. Copies a
// register already satisfies are left out, so an edge that changes nothing
// returns nothing.
func (c *Compiler) Moves(e ssa.Edge) []Move {
	if e.Block < 0 || e.Block >= c.fn.Len() {
		return nil
	}
	params := c.fn.Block(e.Block).Params
	if len(params) != len(e.Args) {
		return nil
	}
	pending := make([]Move, 0, len(params))
	for i, param := range params {
		dst, src := c.Reg(param), c.Reg(e.Args[i])
		if dst != src {
			pending = append(pending, Move{Dst: dst, Src: src})
		}
	}

	var out []Move
	for len(pending) > 0 {
		free := false
		for i := 0; i < len(pending); {
			if reads(pending, pending[i].Dst) {
				i++
				continue
			}
			out = append(out, pending[i])
			pending = append(pending[:i], pending[i+1:]...)
			free = true
		}
		if free {
			continue
		}
		// Every destination left is still read by another pending copy, so
		// the copies form a cycle. Parking one source in a temporary and
		// redirecting its readers there frees that source's own destination
		// without losing the value, which breaks the cycle in one step.
		src := pending[0].Src
		tmp := c.asm.Reg(src.Type(), src.Width())
		out = append(out, Move{Dst: tmp, Src: src})
		for i := range pending {
			if pending[i].Src == src {
				pending[i].Src = tmp
			}
		}
	}
	return out
}

// translate plans in at root through one frontend. It reports nil for a root
// that frontend cannot plan, and for one whose translation failed: neither is
// this seam's to report, because the plan pipeline behind it reaches the same
// bytecode and reports what it finds there itself.
func translate(kind prof.Frontend, in *jit.Input, root jit.Anchor) *ssa.Function {
	if kind == prof.FrontendTrace {
		return frontend.Trace(in, root)
	}
	f, err := frontend.Static(in, root)
	if err != nil {
		return nil
	}
	return f
}

// newCompiler indexes f for one compile: it finds where each block hands
// control back, lays the blocks that are still reached out, binds a register
// to every value and a label to every block, records where each value is
// defined, resolves the slot count of every frame a deopt can rebuild, and
// resolves the re-entry each bridge creates. It refuses a function that is
// malformed for lowering, or that holds an operation m declined.
func newCompiler(m Machine, a *asm.Assembler, in *jit.Input, root jit.Anchor, f *ssa.Function) (*Compiler, bool) {
	if m == nil || a == nil || in == nil || f == nil || f.Len() == 0 {
		return nil, false
	}
	c := &Compiler{
		asm:    a,
		input:  in,
		root:   root,
		fn:     f,
		traps:  traps(m, f),
		next:   make([]int, f.Len()),
		labels: make([]asm.Label, f.Len()),
		slots:  map[int]int{},
	}
	c.order = order(f, c.traps)
	for id := range c.next {
		c.next[id] = -1
	}
	for i, id := range c.order {
		c.labels[id] = a.Label()
		if i+1 < len(c.order) {
			c.next[id] = c.order[i+1]
		}
	}

	for id := 0; id < f.Len(); id++ {
		block := f.Block(id)
		for _, param := range block.Params {
			c.define(param, site{id, -1})
		}
		for i, op := range block.Ops {
			for _, result := range op.Results {
				c.define(result, site{id, i})
			}
		}
	}
	for v := range c.regs {
		typ, width := bank(f.Type(ssa.Value(v)))
		if width != asm.WidthUndefined {
			c.regs[v] = a.Reg(typ, width)
		}
	}

	// Only what is laid out is emitted, so only that is judged: an opcode the
	// machine declined behind a trap is code no native path reaches, and
	// refusing the whole function for it would throw away the prefix the trap
	// exists to keep.
	for _, id := range c.order {
		for _, op := range c.ops(id) {
			if op.Op == ssa.OpExec && !m.Lowers(op.Code) {
				return nil, false
			}
			if op.Op == ssa.OpState && !c.measure(op.Frames) {
				return nil, false
			}
		}
		if bridge, ok := c.bridge(id); ok {
			c.bridges = append(c.bridges, bridge)
		}
	}
	return c, true
}

// bridge resolves the external re-entry the block's trailing OpBridge creates.
// The interpreter runs that one opcode and leaves execution at the instruction
// after it, which is the block the bridge falls into - so a bridge anywhere
// but at the end of a block, or one leaving by anything but a single edge,
// names no re-entry and gets none.
func (c *Compiler) bridge(id int) (Bridge, bool) {
	block, ops := c.fn.Block(id), c.ops(id)
	if len(ops) == 0 || len(block.Term.Edges) != 1 {
		return Bridge{}, false
	}
	last := ops[len(ops)-1]
	if last.Op != ssa.OpBridge {
		return Bridge{}, false
	}
	state, ok := c.Def(last.State)
	if !ok || state.Op != ssa.OpState || len(state.Frames) == 0 {
		return Bridge{}, false
	}
	frame := state.Frames[len(state.Frames)-1]
	fn := c.input.Objects.Function(frame.Addr)
	if fn == nil || frame.IP < 0 || frame.IP >= len(fn.Code) {
		return Bridge{}, false
	}
	return Bridge{IP: frame.IP + instr.Instruction(fn.Code[frame.IP:]).Width(), Block: block.Term.Edges[0].Block}, true
}

// ops returns the operations of block id that native code runs: everything up
// to and including the one that hands control back to the interpreter, and the
// whole block when none does.
func (c *Compiler) ops(id int) []ssa.Operation {
	ops := c.fn.Block(id).Ops
	if at := c.traps[id]; at >= 0 {
		return ops[:at+1]
	}
	return ops
}

// define records where v is defined, growing the per-value tables to reach it.
func (c *Compiler) define(v ssa.Value, at site) {
	for int(v) >= len(c.defs) {
		c.defs = append(c.defs, site{-1, -1})
		c.regs = append(c.regs, asm.VReg{})
	}
	c.defs[v] = at
}

// measure resolves how many stack slots each frame's function occupies, which
// is what turns an operand's position within a frame into the VM stack slot a
// deopt flushes it to. A frame whose address names no function cannot be
// rebuilt, so the whole compile is refused rather than one exit silently
// resuming at the wrong slot.
func (c *Compiler) measure(frames []ssa.Frame) bool {
	for _, frame := range frames {
		if _, ok := c.slots[frame.Addr]; ok {
			continue
		}
		fn := c.input.Objects.Function(frame.Addr)
		if fn == nil {
			return false
		}
		c.slots[frame.Addr] = len(fn.Declared())
	}
	return true
}

// traps records, for every block, the index of the first operation whose
// lowering hands control back to the interpreter, or -1 for a block that
// never does. It is the point the block ends at: what follows it there, and
// every block only it reaches, is code no native path runs.
func traps(m Machine, f *ssa.Function) []int {
	at := make([]int, f.Len())
	for id := range at {
		at[id] = -1
		for i, op := range f.Block(id).Ops {
			if op.Op == ssa.OpExec && m.Traps(op.Code) {
				at[id] = i
				break
			}
		}
	}
	return at
}

// order lays f's blocks out in reverse postorder: a block precedes every
// block it dominates, and a loop body stays contiguous between its header and
// its back edge. The depth-first walk takes each block's successors back to
// front, which puts the first edge first in the result, so the path a
// frontend laid down first - the recorded one, for a trace - is the one that
// falls through. A block that traps reaches none of its successors, so the
// walk stops there and lays out nothing only that block led to. It is the
// layout the backend chooses, not a graph fact, which is why it lives here
// rather than with internal/graph's dominance.
func order(f *ssa.Function, traps []int) []int {
	visited := make([]bool, f.Len())
	post := make([]int, 0, f.Len())
	type frame struct{ block, next int }
	stack := []frame{{0, 0}}
	visited[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		var succ []int
		if traps[top.block] < 0 {
			succ = f.Succ(top.block)
		}
		if top.next < len(succ) {
			s := succ[len(succ)-1-top.next]
			top.next++
			if !visited[s] {
				visited[s] = true
				stack = append(stack, frame{s, 0})
			}
			continue
		}
		post = append(post, top.block)
		stack = stack[:len(stack)-1]
	}

	out := make([]int, len(post))
	for i, id := range post {
		out[len(post)-1-i] = id
	}
	return out
}

// bank is the register a value of type t lives in, or the undefined width for
// a type that holds no register.
//
// An i1, i8, and i32 live in the W lane: their whole representation is a raw
// payload of that width, carrying no runtime tag, so the register the
// compiler hands out for one is already the view every consumer wants (see
// docs/value-representation.md). An i64 takes the X lane at its own full
// width regardless of what it holds: a machine that has proven a value
// cannot leave the boxed 49-bit lane may keep it raw there, and one it has
// not proven that of keeps it boxed, but an i64's raw and boxed forms share
// one register width either way, unlike i32's narrower one - so bank names
// only the width, and a machine's own guard is what earns a value the raw
// form within it. A reference is held boxed throughout, which this port has
// not narrowed, and so does an f64, paired with an f32's own 32-bit float
// register.
func bank(t ssa.Type) (asm.RegType, asm.RegWidth) {
	switch t {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		return asm.RegTypeInt, asm.Width32
	case ssa.TypeI64, ssa.TypeRef:
		return asm.RegTypeInt, asm.Width64
	case ssa.TypeF32:
		return asm.RegTypeFloat, asm.Width32
	case ssa.TypeF64:
		return asm.RegTypeFloat, asm.Width64
	default:
		return asm.RegTypeInt, asm.WidthUndefined
	}
}

// reads reports whether any copy still to be made reads reg.
func reads(pending []Move, reg asm.VReg) bool {
	for _, move := range pending {
		if move.Src == reg {
			return true
		}
	}
	return false
}
