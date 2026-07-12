package interp

import (
	"errors"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/types"
)

// lowerer emits a backend-neutral plan for one target architecture.
type lowerer interface {
	lower(ctx *lowering, plan plan) bool
}

type compiler struct {
	lowerer     lowerer
	arch        asm.Arch
	buffer      *asm.Buffer
	scratchRegs []asm.PReg
}

// continuationLimit caps deferred learned continuations in one native
// callable; beyond this the guard keeps the old deopt fallback.
const continuationLimit = 256

type module struct {
	entries map[anchor]native
	emits   int
	bytes   int
}

type native struct {
	callable asm.Callable
	entry    entryKind
}

// lowering carries symbolic values, inlined activations, deferred blocks, and
// cold exits while one plan is emitted. It contains no planner source objects.
type lowering struct {
	assembler *asm.Assembler
	root      block
	blocks    map[anchor]block
	labels    map[anchor]asm.Label
	funcs     map[int]*types.Function
	constants []types.Boxed
	globals   []types.Kind
	heap      []types.Value
	scratch   []asm.PReg
	entry     asm.Label
	head      asm.Label
	back      asm.Label

	values    []value
	frames    []activation
	work      []work
	scheduled int
	exits     []sideExit
	queued    map[anchor]asm.Label
	saved     []value

	addr    int
	returns int
	kind    entryKind
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

// work is a deferred block whose branch point produced its symbolic state:
// stack homes are current, so the block re-enters at label with
// every local unloaded and every operand awaiting reload. If the branch
// returned from an inlined callee, tail keeps the caller path that must run
// after the deferred block stitches back into the caller frame.
type work struct {
	label  asm.Label
	block  block
	tail   []block
	values []value
	frames []activation
	hits   int64
}

type sideExit struct {
	label  asm.Label
	values []value
	frames []activation
	resume int
	retain int
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
	journalNatives        // &i.natives[0]; atomic per-function entry slots
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

func (c *compiler) newLowering(input *compileInput, arch asm.Arch) *lowering {
	asmb := asm.New(arch)
	ctx := &lowering{
		assembler: asmb,
		blocks:    map[anchor]block{},
		labels:    map[anchor]asm.Label{},
		funcs:     input.functions,
		queued:    map[anchor]asm.Label{},
		constants: input.constants,
		globals:   input.globals,
		heap:      input.heap,
		scratch:   c.scratchRegs[:scratchCount],
		entry:     asmb.Label(),
		head:      asmb.Label(),
		addr:      input.address,
	}
	if input.function.Typ != nil {
		ctx.returns = len(input.function.Typ.Returns)
	}
	ctx.frames = append(ctx.frames, newActivation(input.address, input.function, 0, 0))
	return ctx
}

func (c *compiler) publish(mod *module, a anchor, ctx *lowering, arch asm.Arch, n native) (bool, error) {
	code, err := ctx.assembler.Build()
	if err != nil {
		if errors.Is(err, asm.ErrNoRegistersAvailable) || errors.Is(err, asm.ErrBranchOutOfRange) {
			return false, nil
		}
		return false, err
	}
	linked, err := asm.Link(c.buffer, arch, []*asm.Code{code}, nil)
	if err != nil {
		if errors.Is(err, asm.ErrBranchOutOfRange) {
			return false, nil
		}
		return false, err
	}
	n.callable = linked[0].Callable
	mod.entries[a] = n
	mod.emits++
	mod.bytes += len(code.Bytes)
	return true, nil
}

// Compile selects and lowers the first planner family that emits native code.
func (c *compiler) Compile(i *Interpreter, addr int) (*module, error) {
	input, ok := newCompileInput(i, addr)
	if !ok {
		return nil, nil
	}
	planners := [...]planner{staticPlanner{}, tracePlanner{}}
	for _, planner := range planners {
		plans, err := planner.plan(input)

		if err != nil {
			return nil, err
		}
		mod := &module{entries: map[anchor]native{}}
		for _, plan := range plans {
			if !plan.valid() {
				continue
			}
			ok, err := c.compile(input, plan, mod)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		if mod.emits > 0 {
			return mod, nil
		}
	}
	return &module{entries: map[anchor]native{}}, nil
}

func (c *compiler) compile(input *compileInput, plan plan, mod *module) (bool, error) {
	if len(c.scratchRegs) < scratchCount {
		return false, nil
	}
	arch := c.arch
	if plan.spill == spillForbidden {
		arch = noSpillArch{c.arch}
	}
	ctx := c.newLowering(input, arch)
	if !c.lowerer.lower(ctx, plan) {
		return false, nil
	}
	return c.publish(mod, plan.entry.anchor, ctx, c.arch, native{entry: plan.entry.kind})
}

// noSpillArch wraps an asm.Arch to force Build to reject spilling instead of
// inserting a spill frame. A nil Frame already disables spilling per asm's
// own contract (see asm.Frame's doc comment), so this policy needs no
// dedicated asm-level API — it is purely an interp-side JIT policy decision
// (see planSpill), not a generic assembler concern.
type noSpillArch struct{ asm.Arch }

func (noSpillArch) Frame() asm.Frame { return nil }

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

// snapshot deep-copies operand and frame state for a deferred branch. Callers
// must flush VM stack homes before snapshot; re-entry reloads locals on demand,
// so stale register/local loaded state must stay dropped.
// queueExit records a cold fallback after the caller has materialized VM stack state.
// values may be nil to snapshot the current symbolic stack; retain is applied only
// on the cold path before returning to threaded execution.
func (ctx *lowering) queueExit(values []value, resume, retain int) asm.Label {
	if values != nil {
		ctx.values = append(ctx.values[:0], values...)
	}
	label := ctx.assembler.Label()
	stack, frames := ctx.snapshot()
	ctx.exits = append(ctx.exits, sideExit{label: label, values: stack, frames: frames, resume: resume, retain: retain})
	return label
}

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
