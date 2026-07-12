package interp

import (
	"errors"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// lowerer is the architecture-specific JIT lowerer. compiler owns the
// recorded-trace orchestration and delegates native-code emission to the target
// backend. lowerCFG is compileCFG's counterpart to lower: it walks one
// statically decoded basic block instead of a recorded trace tree.
type lowerer interface {
	lower(ctx *lowering) bool
	lowerCFG(ctx *lowering, blocks []*analysis.BasicBlock, kinds [][]types.Kind, labels []asm.Label) bool
}

type compiler struct {
	lowerer     lowerer
	arch        asm.Arch
	buffer      *asm.Buffer
	scratchRegs []asm.PReg
}

// pendingLimit caps learned branch continuations emitted into one native
// callable; beyond this the guard keeps the old deopt fallback.
const pendingLimit = 256

type module struct {
	entries map[anchor]native
	emits   int
	bytes   int
}

type native struct {
	callable asm.Callable
	loop     bool
}

// lowering carries the symbolic interpreter state for one trace
// compilation: typed operand values, the inline frame chain, and the recorded
// branch continuations still waiting for emission. The arch lowerer mutates it
// while emitting; compiler builds it and links the result.
type lowering struct {
	assembler *asm.Assembler
	tree      *tree
	branches  map[branch]leg
	funcs     map[int]*types.Function
	constants []types.Boxed
	globals   []types.Kind
	heap      []types.Value
	scratch   []asm.PReg
	entry     asm.Label
	head      asm.Label
	back      asm.Label

	values          []value
	frames          []activation
	pending         []pending
	pendingBranches int
	exits           []sideExit
	queued          map[branch]asm.Label
	tails           map[*step]asm.Label
	saved           []value

	addr    int
	returns int
	loop    bool
}

// value is one typed operand: a register plus the runtime kind the trace
// observed for it. raw scalars skip NaN-boxing between opcodes — an i32 keeps
// its value in the low 32 bits, an f64 keeps its IEEE bits (identical to its
// boxed form). A raw ref is a compile-time function or closure constant that
// was never materialized; fn holds the target function and ref holds the
// callable heap ref.
type value struct {
	reg   asm.VReg
	kind  types.Kind
	raw   bool
	known bool
	imm   int64
	fn    int
	ref   int
}

// activation mirrors one interpreter frame the trace inlined. Locals live in
// registers; loaded marks which have been pulled from the VM stack and dirty
// marks which must be written back before native code gives up control.
type activation struct {
	fn     *types.Function
	code   []byte
	kinds  []types.Kind
	upvals []types.Kind
	locals []value
	loaded []bool
	dirty  []bool

	addr     int
	base     int
	opBase   int
	upvalRef int
	resume   int
	returns  int
}

// pending is the cold continuation of a guarded branch: state was
// flushed to the VM stack at the guard, so the body re-enters at label with
// every local unloaded and every operand awaiting reload. If the branch
// returned from an inlined callee, tail keeps the caller path that must run
// after the pending body stitches back into the caller frame.
type pending struct {
	label  asm.Label
	ops    []step
	tail   []step
	kind   outcome
	values []value
	frames []activation
	hits   int64
}

type sideExit struct {
	label  asm.Label
	values []value
	frames []activation
	resume int
}

const (
	scratchStack = iota
	scratchGlobals
	scratchBP
	scratchSP
	scratchCtrl
	scratchCount
)

// Frame-journal layout. X0 carries &journal[0] to native code, which mirrors the
// first cells into pinned scratch registers (X10-X14) on external entry. Header
// cells precede a stack of fixed-stride frame records; each record mirrors the
// int fields the threaded interpreter needs to resume a frame.
const (
	journalStack   = iota // &i.stack[0]; external entry in
	journalGlobals        // &i.globals[0]; external entry in
	journalBP             // current frame bp; external entry in
	journalSP             // interpreter sp; external entry in/out
	journalDepth          // trap-time frame records written; native read/write
	journalCap            // frame budget capped by nativeFrameLimit; read-only
	journalTrap           // exit kind out: trapNone | trapFallback | trapOverflow | trapYield
	journalNextIP         // resume/fallback IP out for the single-frame path
	journalBudget         // back-edges remaining before the next safepoint; native read/write
	journalActive         // active native call depth for frame-budget checks
	journalRC             // &i.rc[0]; read/write for guarded native refcount fast paths
	journalUpvals         // &i.fr.upvals[0] or 0; read/write for closure body fast paths
	journalHeap           // &i.heap[0]; read-only for heap object fast paths
	journalHead           // first frame record cell
)

const journalStride = 4

const (
	recordAddr = iota
	recordBP
	recordIP
	recordReturns
)

const (
	trapNone = iota
	trapFallback
	trapOverflow
	trapYield
)

// nativeFrameLimit caps generated call depth to the stack space reserved by
// the ARM64 invoke trampoline. Deeper calls trap before moving SP.
const nativeFrameLimit = 128

// loopBudget is how many native loop back-edges run between safepoints. It is
// independent of tick so a hot loop amortizes the deopt/re-enter cost of a
// yield over many iterations while still polling for cancellation and fuel.
const loopBudget = 1 << 13

func newActivation(addr int, fn *types.Function, base, opBase int) activation {
	kinds := fn.LocalKinds()
	upvals := types.Kinds(fn.Captures)
	returns := 0
	if fn.Typ != nil {
		returns = len(fn.Typ.Returns)
	}
	return activation{
		fn:      fn,
		code:    fn.Code,
		kinds:   kinds,
		upvals:  upvals,
		locals:  make([]value, len(kinds)),
		loaded:  make([]bool, len(kinds)),
		dirty:   make([]bool, len(kinds)),
		addr:    addr,
		base:    base,
		opBase:  opBase,
		returns: returns,
	}
}

func (c *compiler) Close() error {
	return c.buffer.Free()
}

// Compile attempts to lower the recorded entry trace for fn into one native
// callable. Without a usable trace, unsupported op, or rejected observed shape,
// it emits nothing and leaves threaded dispatch in place.
func (c *compiler) Compile(i *Interpreter, addr int, fn *types.Function) (*module, error) {
	mod := &module{entries: map[anchor]native{}}
	if fn == nil || len(fn.Code) == 0 {
		return mod, nil
	}
	_, err := c.emit(i, addr, fn, mod)
	return mod, err
}

// emit lowers every published trace root recorded for addr — the function
// entry plus any hot loop headers — into framed native callables, one per
// anchor. It returns false (emitting nothing) when no usable trace exists or
// the typed lowerer rejects them all, so the caller can leave threaded dispatch
// installed.
func (c *compiler) emit(i *Interpreter, addr int, fn *types.Function, mod *module) (bool, error) {
	if i.tracer == nil {
		return false, nil
	}
	anchors := i.tracer.anchors(addr)
	if len(anchors) == 0 {
		return false, nil
	}
	funcs := map[int]*types.Function{}
	for addr := range i.instrs {
		if fn, ok := i.function(addr); ok {
			funcs[addr] = fn
		}
	}
	any := false
	for _, ip := range anchors {
		ok, err := c.emitRoot(i, addr, fn, mod, anchor{addr: addr, ip: ip}, funcs)
		if err != nil {
			return false, err
		}
		any = any || ok
	}
	return any, nil
}

// emitRoot lowers the single trace root anchored at a into one framed native
// callable keyed by a. An entry root (a.ip == 0) compiles the whole function
// from a clean frame; a loop root compiles one iteration with a native
// back-edge re-entered mid-function at a.ip.
func (c *compiler) emitRoot(i *Interpreter, addr int, fn *types.Function, mod *module, a anchor, funcs map[int]*types.Function) (bool, error) {
	tree := i.tracer.rootAt(a)
	if tree == nil {
		return false, nil
	}
	// Only the function entry and genuine loop headers compile to standalone
	// native callables. Other non-zero anchors are side-exit branches the
	// recorder stored as top-level trees; they are inlined into the entry trace
	// through branchIPs, never installed on their own. A loop whose header is the
	// function entry (ip 0) has no distinct entry trace to anchor the re-entry
	// state and is left to threaded dispatch for now.
	if (a.ip != 0) != (tree.root.kind == loop) {
		return false, nil
	}
	if len(c.scratchRegs) < scratchCount {
		return false, nil
	}

	// A trace whose terminal op is ARRAY_SET or STRUCT_SET lowers to a native
	// fast path that performs the heap store and falls through to the next
	// threaded instruction on success; a spilled value still live across that
	// terminal store would leave a stale copy on the VM stack if the native
	// path took the store and then trapped before writing the spill back.
	// noSpillArch reports no Frame, which is asm's own contract for
	// disabling spilling (see asm.Frame's doc comment), so this policy needs
	// no dedicated asm-level API. Loop back-edges need no interp-side check
	// here: the assembler's own generic backward-label scan (asm/rewriter.go)
	// already disables spilling for any Code containing an intra-Code
	// backward branch — see docs/jit-internals.md for why both layers exist.
	arch := c.arch
	if !spillSafe(tree) {
		arch = noSpillArch{c.arch}
	}
	asmb := asm.New(arch)
	entry := asmb.Label()

	// The declared Program.Globals are out of scope here; New pre-seeds every
	// slot to the zero Boxed of its declared kind, so the runtime values carry
	// the declared kinds at all times.
	globals := make([]types.Kind, len(i.globals))
	for j, g := range i.globals {
		globals[j] = g.Kind()
	}

	ctx := &lowering{
		assembler: asmb,
		tree:      tree,
		branches:  tree.branchIPs(),
		funcs:     funcs,
		queued:    map[branch]asm.Label{},
		tails:     map[*step]asm.Label{},
		constants: i.constants,
		globals:   globals,
		heap:      i.heap,
		scratch:   c.scratchRegs[:scratchCount],
		entry:     entry,
		head:      asmb.Label(),
		addr:      addr,
		loop:      tree.root.kind == loop,
	}
	if fn.Typ != nil {
		ctx.returns = len(fn.Typ.Returns)
	}
	ctx.frames = append(ctx.frames, newActivation(addr, fn, 0, 0))
	if !c.lowerer.lower(ctx) {
		return false, nil
	}
	code, err := asmb.Build()
	if err != nil {
		if errors.Is(err, asm.ErrNoRegistersAvailable) || errors.Is(err, asm.ErrBranchOutOfRange) {
			return false, nil
		}
		return false, err
	}
	linked, err := asm.Link(c.buffer, c.arch, []*asm.Code{code}, nil)
	if err != nil {
		// External relocation re-encoding (asm/link.go's patchExternalRelocs)
		// can also return ErrBranchOutOfRange, not just Build; treat it the
		// same as the Build-path case above and fall back to threaded
		// dispatch instead of propagating a hard error.
		if errors.Is(err, asm.ErrBranchOutOfRange) {
			return false, nil
		}
		return false, err
	}
	mod.entries[a] = native{callable: linked[0].Callable, loop: ctx.loop}
	mod.emits++
	mod.bytes += len(code.Bytes)
	return true, nil
}

// noSpillArch wraps an asm.Arch to force Build to reject spilling instead of
// inserting a spill frame. A nil Frame already disables spilling per asm's
// own contract (see asm.Frame's doc comment), so this policy needs no
// dedicated asm-level API — it is purely an interp-side JIT policy decision
// (see spillSafe), not a generic assembler concern.
type noSpillArch struct{ asm.Arch }

func (noSpillArch) Frame() asm.Frame { return nil }

// spillSafe reports whether the compiled trace tree may use ordinary
// register spilling. A trace whose terminal op is ARRAY_SET or STRUCT_SET
// lowers to a native fast path that performs the heap store and falls
// through to the next threaded instruction on success; a spilled value still
// live across that terminal store would leave a stale copy on the VM stack if
// the native path took the store and then trapped before writing the spill
// back. The scan covers the root trace and every branch the tree may inline
// as a continuation, not just the root's own last op, because a learned
// continuation that itself ends in a mutation would otherwise escape the
// check.
func spillSafe(tree *tree) bool {
	if endsInMutation(tree.root) {
		return false
	}
	for _, tr := range tree.branches {
		if endsInMutation(tr) {
			return false
		}
	}
	return true
}

// endsInMutation reports whether t's recorded final op is a heap mutation
// that lowers as a terminal native fast path (see spillSafe).
func endsInMutation(t *trace) bool {
	if t == nil || len(t.ops) == 0 {
		return false
	}
	op := t.ops[len(t.ops)-1].op
	return op == instr.ARRAY_SET || op == instr.STRUCT_SET
}

// frame returns the innermost (currently executing) frame.
func (ctx *lowering) frame() *activation {
	return &ctx.frames[len(ctx.frames)-1]
}

// push appends one operand to the symbolic stack.
func (ctx *lowering) push(v value) {
	ctx.values = append(ctx.values, v)
}

// pop removes and returns the top operand.
func (ctx *lowering) pop() value {
	v := ctx.values[len(ctx.values)-1]
	ctx.values = ctx.values[:len(ctx.values)-1]
	return v
}

// count reports how many operands the innermost frame owns.
func (ctx *lowering) count() int {
	return len(ctx.values) - ctx.frame().opBase
}

// slot returns the VM stack slot of values[idx] as a delta from the entry
// frame's bp: the owning frame's locals floor plus the operand's position.
func (ctx *lowering) slot(idx int) int {
	for k := len(ctx.frames) - 1; k >= 0; k-- {
		f := &ctx.frames[k]
		if f.opBase <= idx {
			return f.base + len(f.kinds) + (idx - f.opBase)
		}
	}
	return idx
}

// sp returns the interpreter stack pointer as a delta from the entry bp.
func (ctx *lowering) sp() int {
	f := ctx.frame()
	return f.base + len(f.kinds) + (len(ctx.values) - f.opBase)
}

// snapshot deep-copies operand and frame state for a pending branch. Callers
// must flush VM stack homes before snapshot; re-entry reloads locals on demand,
// so stale register/local loaded state must stay dropped.
func (ctx *lowering) snapshot() ([]value, []activation) {
	values := make([]value, len(ctx.values))
	for i, v := range ctx.values {
		values[i] = value{kind: v.kind, raw: v.raw, known: v.known, imm: v.imm, fn: v.fn, ref: v.ref}
	}
	frames := make([]activation, len(ctx.frames))
	for i, f := range ctx.frames {
		frames[i] = f
		frames[i].locals = make([]value, len(f.locals))
		frames[i].loaded = make([]bool, len(f.loaded))
		frames[i].dirty = make([]bool, len(f.dirty))
	}
	return values, frames
}

// pre copies the operand stack for one guard fallback. saved may share backing
// storage with values; mutating ops must remain terminal or avoid changing
// symbolic values after aliasing.
func (ctx *lowering) pre() []value {
	ctx.saved = append(ctx.saved[:0], ctx.values...)
	return ctx.saved
}

// pin returns a fresh Width64 int vreg bound to the scratch register at idx.
func (ctx *lowering) pin(idx int) asm.VReg {
	v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(v, ctx.scratch[idx])
	return v
}

// pinTo returns a fresh Width64 int vreg bound to the physical register pr.
func (ctx *lowering) pinTo(pr asm.PReg) asm.VReg {
	v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(v, pr)
	return v
}
