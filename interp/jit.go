package interp

import (
	"errors"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

type compiler struct {
	arch        asm.Arch
	buffer      *asm.Buffer
	scratchRegs []asm.PReg
}

type module struct {
	entries map[anchor]native
	bytes   int
}

type native struct {
	callable asm.Callable
	kind     entryKind
	frontend prof.Frontend
	bytes    int
	exits    []exitDescriptor
}

type exitDescriptor struct {
	reason prof.ExitReason
	opcode int
}

type counters struct {
	entry  *prof.Counter
	yields *prof.Counter
	exits  []*prof.Counter
}

type compileResult struct {
	module   *module
	anchor   anchor
	frontend prof.Frontend
	outcome  prof.CompileOutcome
	reason   prof.CompileReason
	err      error
}

// lowering carries symbolic values, inlined activations, deferred blocks, and
// cold exits while one plan is emitted. It contains no planner source objects.
type lowering struct {
	assembler *asm.Assembler
	blocks    []block
	labels    map[int]asm.Label
	module    *types.Function
	constants []types.Boxed
	globals   []types.Kind
	heap      []types.Value
	scratch   []asm.PReg
	entry     asm.Label
	head      asm.Label
	back      asm.Label
	budget    asm.VReg

	values      []value
	frames      []activation
	work        []work
	scheduled   int
	exits       []sideExit
	descriptors []exitDescriptor
	saved       []value

	addr     int
	root     int
	returns  int
	kind     entryKind
	leaf     bool
	backEdge bool

	reuseLocals bool
	spare       asm.VReg

	// hoist caches one loop-invariant container's slice header, derived by a
	// per-entry prologue (see arm64Lowerer.hoist). The registers are pure
	// derived state: flush, snapshots, and reload never see them, and an
	// access uses them only when its operand matches slot and want.
	hoist struct {
		slot    int
		want    uintptr
		dataPtr asm.VReg
		n       asm.VReg
		live    bool
	}
}

// value is one typed operand: a register plus the runtime kind the trace
// observed for it. raw scalars skip NaN-boxing between opcodes — an i32 keeps
// its value in the low 32 bits, an f64 keeps its IEEE bits (identical to its
// boxed form). For refs, backing records where the reference count lives: an
// backingStack ref carries its own retain on the operand stack, while every
// other backing defers the retain to its backing storage until the value
// transfers to interpreter state. Field validity depends on backing:
// backingStack uses reg; backingConst uses ref and may also use fn for a direct
// call target; backingLocal, backingGlobal, and backingUpval use reg plus slot.
// slot identifies the VM stack local, global, or upval that carries the retain.
type value struct {
	reg     asm.VReg
	kind    types.Kind
	raw     bool
	backing backing
	slot    int
	known   bool
	imm     int64
	fn      int
	ref     int
	flushed bool
}

// backing identifies where a ref value derives its reference count.
type backing uint8

const (
	backingStack  backing = iota // retain lives on the operand stack copy
	backingConst                 // compile-time constant, never retained
	backingLocal                 // deferred to a VM stack local slot
	backingGlobal                // deferred to a global slot
	backingUpval                 // deferred to a closure upval slot
)

// activation mirrors one interpreter frame the trace inlined. Locals live in
// registers; loaded marks which have been pulled from the VM stack and dirty
// marks which must be written back before native code gives up control.
type activation struct {
	end    int
	kinds  []types.Kind
	upvals []types.Kind
	locals []value
	state  []localState

	addr     int
	base     int
	opBase   int
	upvalRef int
	resume   int
	returns  int
}

// work is a deferred block whose branch point produced its symbolic state:
// VM stack slots are current, so the block re-enters at label with
// every local unloaded and every operand awaiting reload. If the branch
// returned from an inlined callee, tail keeps the caller path that must run
// after the deferred block stitches back into the caller frame.
type localState uint8

const (
	localLoaded localState = 1 << iota
	localDirty
	localFlushed
)

type work struct {
	label  asm.Label
	block  int
	tail   []int
	values []value
	frames []activation
}

type sideExit struct {
	label  asm.Label
	values []value
	frames []activation
	resume int
	id     int
}

// noSpillArch wraps an asm.Arch to force Build to reject spilling instead of
// inserting a spill frame. A nil Frame already disables spilling per asm's
// own contract (see asm.Frame's doc comment), so this policy needs no
// dedicated asm-level API — it is purely an interp-side JIT policy decision
// (see noSpill), not a generic assembler concern.
type noSpillArch struct{ asm.Arch }

// continuationLimit caps deferred learned continuations in one native
// callable; beyond this the guard keeps the old deopt fallback.
const continuationLimit = 256

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
	journalExitID         // fallback descriptor ID + 1; zero means none
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
	kinds := fn.Slots()
	upvals := types.Kinds(fn.Captures)
	returns := 0
	if fn.Typ != nil {
		returns = len(fn.Typ.Returns)
	}
	return activation{
		end:     len(fn.Code),
		kinds:   kinds,
		upvals:  upvals,
		locals:  make([]value, len(kinds)),
		state:   make([]localState, len(kinds)),
		addr:    addr,
		base:    base,
		opBase:  opBase,
		returns: returns,
	}
}

func (c *compiler) Close() error {
	return c.buffer.Free()
}

// Compile selects and lowers the first frontend that emits native code.
func (c *compiler) Compile(i *Interpreter, root anchor) compileResult {
	input, ok := input(i, root.addr)
	if !ok {
		return compileResult{anchor: root, outcome: prof.CompileOutcomeEmpty, reason: prof.CompileReasonNoInput}
	}
	frontends := [...]struct {
		kind prof.Frontend
		plan func(*compileInput) ([]plan, error)
	}{{prof.FrontendStatic, staticPlan}, {prof.FrontendTrace, tracePlan}}
	result := compileResult{anchor: root, outcome: prof.CompileOutcomeEmpty, reason: prof.CompileReasonNoPlan}
	for _, frontend := range frontends {
		plans, err := frontend.plan(input)
		if err != nil {
			return compileResult{anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeError, reason: prof.CompileReasonError, err: err}
		}
		result = result.prefer(compileResult{anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeEmpty, reason: prof.CompileReasonNoPlan})
		mod := &module{entries: map[anchor]native{}}
		for _, plan := range plans {
			if plan.anchor != root {
				continue
			}
			if !plan.valid() {
				result = result.prefer(compileResult{anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeRejected, reason: prof.CompileReasonInvalidPlan})
				continue
			}
			reason, err := c.compile(input, plan, mod, frontend.kind)
			if err != nil {
				return compileResult{anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeError, reason: prof.CompileReasonError, err: err}
			}
			if reason != prof.CompileReasonNone {
				result = result.prefer(compileResult{anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeRejected, reason: reason})
				continue
			}
		}
		if len(mod.entries) > 0 {
			return compileResult{module: mod, anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeEmitted}
		}
	}
	return result
}

func (noSpillArch) Frame() asm.Frame { return nil }

func (current compileResult) prefer(candidate compileResult) compileResult {
	if reasonPriority(candidate.reason) > reasonPriority(current.reason) ||
		reasonPriority(candidate.reason) == reasonPriority(current.reason) && candidate.frontend > current.frontend {
		return candidate
	}
	return current
}

func (c *compiler) compile(input *compileInput, plan plan, mod *module, frontend prof.Frontend) (prof.CompileReason, error) {
	if len(c.scratchRegs) < scratchCount {
		return prof.CompileReasonLoweringRejected, nil
	}
	arch := c.arch
	if plan.noSpill {
		arch = noSpillArch{c.arch}
	}
	attempts := []bool{false}
	if plan.kind == entryLoop {
		attempts = []bool{true, false}
	}
	for _, backEdge := range attempts {
		ctx := c.newLowering(input, arch)
		ctx.backEdge = backEdge
		if !lower(ctx, plan) {
			return prof.CompileReasonLoweringRejected, nil
		}
		exits := append([]exitDescriptor(nil), ctx.descriptors...)
		reason, err := c.publish(mod, plan.anchor, ctx, c.arch, native{kind: plan.kind, frontend: frontend, exits: exits})
		if reason == prof.CompileReasonRegisterPressure && backEdge {
			continue
		}
		return reason, err
	}
	return prof.CompileReasonRegisterPressure, nil
}

func (c *compiler) newLowering(input *compileInput, arch asm.Arch) *lowering {
	asmb := asm.New(arch)
	ctx := &lowering{
		assembler: asmb,
		labels:    map[int]asm.Label{},
		module:    input.module,
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

func (c *compiler) publish(mod *module, a anchor, ctx *lowering, arch asm.Arch, n native) (prof.CompileReason, error) {
	code, err := ctx.assembler.Build()
	if err != nil {
		if errors.Is(err, asm.ErrNoRegistersAvailable) {
			return prof.CompileReasonRegisterPressure, nil
		}
		if errors.Is(err, asm.ErrBranchOutOfRange) {
			return prof.CompileReasonBranchRange, nil
		}
		return prof.CompileReasonError, err
	}
	linked, err := asm.Link(c.buffer, arch, []*asm.Code{code}, nil)
	if err != nil {
		if errors.Is(err, asm.ErrBranchOutOfRange) {
			return prof.CompileReasonBranchRange, nil
		}
		return prof.CompileReasonError, err
	}
	n.callable = linked[0].Callable
	n.bytes = len(code.Bytes)
	mod.entries[a] = n
	mod.bytes += len(code.Bytes)
	return prof.CompileReasonNone, nil
}

func (m counters) exit(encoded uint64) {
	if encoded == 0 {
		return
	}
	id := int(encoded - 1)
	if id >= 0 && id < len(m.exits) {
		m.exits[id].Inc()
	}
}

func (m counters) enter() {
	if m.entry != nil {
		m.entry.Inc()
	}
}

func (m counters) yield() {
	if m.yields != nil {
		m.yields.Inc()
	}
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

func (ctx *lowering) opcode(ip int) int {
	fn := resolve(ctx.module, ctx.heap, ctx.frame().addr)
	if fn == nil || ip < 0 || ip >= len(fn.Code) {
		return prof.OpcodeNone
	}
	return int(fn.Code[ip])
}

// frame returns the innermost (currently executing) frame.
func (ctx *lowering) frame() *activation {
	return &ctx.frames[len(ctx.frames)-1]
}

// queueExit records a cold fallback after the caller has materialized VM stack
// state. values may be nil to snapshot the current symbolic stack; retains for
// deferred refs in the snapshot are applied only on the cold path before
// returning to threaded execution.
func (ctx *lowering) queueExit(values []value, resume int, reason prof.ExitReason, opcode int) asm.Label {
	if values != nil {
		ctx.values = append(ctx.values[:0], values...)
	}
	label := ctx.assembler.Label()
	stack, frames := ctx.snapshot()
	id := len(ctx.descriptors)
	ctx.descriptors = append(ctx.descriptors, exitDescriptor{reason: reason, opcode: opcode})
	ctx.exits = append(ctx.exits, sideExit{
		label: label, values: stack, frames: frames, resume: resume,
		id: id,
	})
	return label
}

// snapshot deep-copies operand and frame state for a deferred branch. Callers
// must flush VM stack slots first; re-entry reloads locals on demand, so stale
// register and local-loaded state must stay dropped.
func (ctx *lowering) snapshot() ([]value, []activation) {
	values := make([]value, len(ctx.values))
	for i, v := range ctx.values {
		values[i] = value{kind: v.kind, raw: v.raw, backing: v.backing, slot: v.slot, known: v.known, imm: v.imm, fn: v.fn, ref: v.ref, flushed: v.flushed}
	}
	frames := make([]activation, len(ctx.frames))
	for i, f := range ctx.frames {
		frames[i] = f
		frames[i].locals = make([]value, len(f.locals))
		frames[i].state = make([]localState, len(f.state))
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

func reasonPriority(reason prof.CompileReason) int {
	switch reason {
	case prof.CompileReasonInvalidPlan:
		return 1
	case prof.CompileReasonLoweringRejected, prof.CompileReasonBackendUnavailable:
		return 2
	case prof.CompileReasonRegisterPressure:
		return 3
	case prof.CompileReasonBranchRange:
		return 4
	case prof.CompileReasonError:
		return 5
	default:
		return 0
	}
}
