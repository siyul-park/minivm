package backend

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
)

// Code is what one Compile produced that the emitted instructions do not
// already say: the order it laid the blocks out in, and the exit descriptors
// it registered, in the order journal.CellExitID counts them. The
// instructions themselves stay in the asm.Assembler the caller supplied,
// which is what allocates and encodes them.
type Code struct {
	Order []int
	Exits []jit.Exit
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
	fn    *ssa.Function

	order  []int
	next   []int
	labels []asm.Label
	regs   []asm.VReg
	defs   []site
	slots  map[int]int

	exits []jit.Exit
}

// site is where a value is defined: the block holding the definition, and the
// index of the defining operation within it, or -1 for a block parameter.
type site struct {
	block int
	index int
}

// Compile lowers f through m into a, reporting the layout and exits it
// produced. It refuses a function m declined an operation of, and abandons the
// compile the first time m reports failure, exactly as an unlowerable plan
// leaves threaded execution installed. f is expected to have passed
// ssa.Verify, which is the caller's boundary and not repeated here.
func Compile(m Machine, a *asm.Assembler, in *jit.Input, f *ssa.Function) (Code, bool) {
	c, ok := newCompiler(m, a, in, f)
	if !ok {
		return Code{}, false
	}
	l := m.Open(c)
	if !l.Enter() {
		return Code{}, false
	}
	for _, id := range c.order {
		a.Bind(c.labels[id])
		block := f.Block(id)
		for i := 0; i < len(block.Ops); {
			n, ok := l.Lower(id, block.Ops[i:])
			if !ok || n <= 0 || i+n > len(block.Ops) {
				return Code{}, false
			}
			i += n
		}
		if !l.Term(id, block.Term) {
			return Code{}, false
		}
	}
	if !l.Leave() {
		return Code{}, false
	}
	return Code{Order: c.order, Exits: c.exits}, true
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

// Func returns the function being lowered, for the queries the Lowering hooks
// do not hand over: a value's type, and the parameters of a block an edge
// targets.
func (c *Compiler) Func() *ssa.Function {
	return c.fn
}

// Reg returns the virtual register bound to v for this whole compile, or the
// zero register for a value that holds none - the interpreter state an
// OpState defines, or a value of another function. Integers and references
// take a 64-bit integer register, an f32 a 32-bit float register and an f64 a
// 64-bit one, which is the lane each value's representation occupies (see
// docs/value-representation.md); a machine that wants a narrower view of one
// derives it.
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

// newCompiler indexes f for one compile: it lays the blocks out, binds a
// register to every value and a label to every block, records where each
// value is defined, and resolves the slot count of every frame a deopt can
// rebuild. It refuses a function that is malformed for lowering, or that
// holds an operation m declined.
func newCompiler(m Machine, a *asm.Assembler, in *jit.Input, f *ssa.Function) (*Compiler, bool) {
	if m == nil || a == nil || in == nil || f == nil || f.Len() == 0 {
		return nil, false
	}
	c := &Compiler{
		asm:    a,
		input:  in,
		fn:     f,
		order:  order(f),
		next:   make([]int, f.Len()),
		labels: make([]asm.Label, f.Len()),
		slots:  map[int]int{},
	}
	if len(c.order) != f.Len() {
		return nil, false
	}
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
			if op.Op == ssa.OpExec && !m.Lowers(op.Code) {
				return nil, false
			}
			if op.Op == ssa.OpState && !c.measure(op.Frames) {
				return nil, false
			}
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
	return c, true
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

// order lays f's blocks out in reverse postorder: a block precedes every
// block it dominates, and a loop body stays contiguous between its header and
// its back edge. The depth-first walk takes each block's successors back to
// front, which puts the first edge first in the result, so the path a
// frontend laid down first - the recorded one, for a trace - is the one that
// falls through. It is the layout the backend chooses, not a graph fact,
// which is why it lives here rather than with internal/graph's dominance.
func order(f *ssa.Function) []int {
	visited := make([]bool, f.Len())
	post := make([]int, 0, f.Len())
	type frame struct{ block, next int }
	stack := []frame{{0, 0}}
	visited[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succ := f.Succ(top.block)
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
func bank(t ssa.Type) (asm.RegType, asm.RegWidth) {
	switch t {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32, ssa.TypeI64, ssa.TypeRef:
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
