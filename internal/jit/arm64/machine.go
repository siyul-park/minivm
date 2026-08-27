// Package arm64 is the ARM64 JIT backend: it lowers an architecture-neutral
// compiler plan (internal/jit) into native ARM64 code through the
// jit.Machine seam. New is the package's only exported symbol; every
// lowering mechanic below it is package-private.
package arm64

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// lowerer is the AArch64 JIT lowerer. It owns the ARM64 scratch
// registers used to pin the frame journal (see enter): the physical
// registers a lowering context reaches through ctx.scratch.
type lowerer struct {
	scratch []asm.PReg
}

// New returns the ARM64 JIT backend, a jit.Machine that lowers a compiler
// plan into native ARM64 code. It is this package's only exported symbol;
// the arch selector one level above (interp) picks it when the running
// process is arm64 and hands it to jit.New alongside internal/asm/arm64's
// own Arch.
func New() jit.Machine {
	return lowerer{scratch: []asm.PReg{arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14}}
}

// lowering carries symbolic values, inlined activations, deferred blocks, and
// cold exits while one plan is emitted. It contains no planner source objects.
type lowering struct {
	assembler *asm.Assembler
	blocks    []jit.Block
	labels    map[int]asm.Label
	module    *types.Function
	constants []types.Boxed
	globals   []types.Kind
	heap      []types.Value
	scratch   []asm.PReg
	layout    jit.Layout
	head      asm.Label
	back      asm.Label
	budget    asm.VReg

	values      []value
	frames      []activation
	work        []work
	exits       []sideExit
	descriptors []jit.ExitDescriptor
	saved       []value

	addr       int
	loopRoot   int
	params     int
	returns    int
	kind       jit.EntryKind
	leaf       bool
	nativeLoop bool
	carried    []carriedLocal

	// hoist caches one loop-invariant container's slice header, derived by a
	// per-entry prologue (see lowerer.hoist). The registers are pure
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
// jit.BackingStack ref carries its own retain on the operand stack, while every
// other backing defers the retain to its backing storage until the value
// transfers to interpreter state. Field validity depends on backing:
// jit.BackingStack uses reg; jit.BackingConst uses ref and may also use fn for a direct
// call target; jit.BackingLocal, jit.BackingGlobal, and jit.BackingUpval use reg plus slot.
// slot identifies the VM stack local, global, or upval that carries the retain.
type value struct {
	reg     asm.VReg
	kind    types.Kind
	raw     bool
	backing jit.Backing
	slot    int
	known   bool
	imm     int64
	fn      int
	ref     int
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
	fn := jit.Resolve(ctx.module, ctx.heap, ctx.frame().addr)
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
	ctx.descriptors = append(ctx.descriptors, jit.ExitDescriptor{Reason: reason, Opcode: opcode})
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
		values[i] = value{kind: v.kind, raw: v.raw, backing: v.backing, slot: v.slot, known: v.known, imm: v.imm, fn: v.fn, ref: v.ref}
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

// Boxing masks used by scalar lowering.
const (
	maskI32 = uint64(0xFFFFFFFF)
	maskI64 = uint64(0x0001_FFFF_FFFF_FFFF)

	boxableWidth = uint8(49)
)

// scratchStack..scratchCtrl index the physical registers a lowering context
// pins the frame journal header into on external entry (see
// lowerer.enter); scratchCount is their count.
const (
	scratchStack = iota
	scratchGlobals
	scratchBP
	scratchSP
	scratchCtrl
	scratchCount
)

// Boxing tags used by scalar lowering, derived from the Kind
// tag layout so they track any reordering of the Kind enum. i1/i8 share the i32
// representation and box through tagI32.
var (
	tagI1  = types.Tag(types.KindI1)
	tagI8  = types.Tag(types.KindI8)
	tagI32 = types.Tag(types.KindI32)
	tagI64 = types.Tag(types.KindI64)
	tagF32 = types.Tag(types.KindF32)
	tagRef = types.Tag(types.KindRef)
)

// enter opens the framed callable: the entry at offset zero mirrors the
// journal header into the pinned scratch registers, dispatches an external
// bridge re-entry to its resume block (see dispatch), then the internal head —
// the BL target for recursive trace calls — saves the link register. A
// recursive self-call branches straight to head and never runs the dispatch,
// because it always starts its callee at IP zero.
func (l lowerer) enter(ctx *lowering) {
	a := ctx.assembler
	a.Emit(
		arm64.MOV(ctx.scratch[scratchCtrl], arm64.X0),
		arm64.LDP(ctx.scratch[scratchStack], ctx.scratch[scratchGlobals], ctx.scratch[scratchCtrl], int16(journal.CellStack*8)),
		arm64.LDP(ctx.scratch[scratchBP], ctx.scratch[scratchSP], ctx.scratch[scratchCtrl], int16(journal.CellBP*8)),
	)
	vCtrl := ctx.pin(scratchCtrl)
	active := ctx.pinTo(arm64.X15)
	a.Emit(arm64.LDR(active, vCtrl, int16(journal.CellActive*8)))
	l.dispatch(ctx, vCtrl)
	a.Bind(ctx.head)
	l.zeroLocals(ctx)
}

func (l lowerer) base(ctx *lowering, vStack asm.VReg) asm.VReg {
	if ctx.leaf {
		addr := ctx.pin(scratchSP)
		l.baseTo(ctx, vStack, addr)
		return addr
	}
	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	l.baseTo(ctx, vStack, addr)
	return addr
}

func (l lowerer) baseTo(ctx *lowering, vStack, addr asm.VReg) {
	vBP := ctx.pin(scratchBP)
	ctx.assembler.Emit(arm64.LSLI(addr, vBP, 3))
	ctx.assembler.Emit(arm64.ADD(addr, vStack, addr))
}

// Lower lowers plan p into a for one native entry, reporting the exits it
// queued and whether lowering succeeded. It is the jit.Machine interface's
// seam with the architecture-neutral compiler (see internal/jit/compiler.go):
// the compiler picks the arch and builds a, and everything from here down is
// ARM64 lowering state and mechanics.
func (l lowerer) Lower(a *asm.Assembler, input *jit.Input, p jit.Plan, nativeLoop bool) ([]jit.ExitDescriptor, bool) {
	if len(l.scratch) < scratchCount {
		return nil, false
	}
	ctx := l.newLowering(input, a)
	ctx.nativeLoop = nativeLoop
	if !l.lower(ctx, p) {
		return nil, false
	}
	return append([]jit.ExitDescriptor(nil), ctx.descriptors...), true
}

// newLowering builds the lowering context one plan is emitted through.
func (l lowerer) newLowering(input *jit.Input, a *asm.Assembler) *lowering {
	ctx := &lowering{
		assembler: a,
		labels:    map[int]asm.Label{},
		module:    input.Module,
		constants: input.Constants,
		globals:   input.Globals,
		heap:      input.Heap,
		scratch:   l.scratch[:scratchCount],
		layout:    input.Layout,
		head:      a.Label(),
		addr:      input.Address,
	}
	if input.Function.Typ != nil {
		ctx.returns = len(input.Function.Typ.Returns)
		ctx.params = len(input.Function.Typ.Params)
	}
	ctx.frames = append(ctx.frames, newActivation(input.Address, input.Function, 0, 0))
	return ctx
}

// lower emits one plan through the common block pipeline.
func (l lowerer) lower(ctx *lowering, plan jit.Plan) bool {
	ctx.leaf = true
	for _, block := range plan.Blocks {
		for _, step := range block.Steps {
			if instr.IsCall(step.Op) {
				ctx.leaf = false
			}
		}
	}
	// blocks, kind, and labels must exist before enter, because enter's entry
	// dispatch (see lowerer.dispatch) branches to a bridge block's label
	// on external re-entry.
	ctx.blocks = plan.Blocks
	ctx.kind = plan.Kind
	for id, block := range ctx.blocks {
		if !block.Tail && block.State != nil {
			ctx.labels[id] = ctx.assembler.Label()
		}
	}
	l.enter(ctx)
	root := plan.Root
	ctx.loopRoot = root
	if _, ok := ctx.labels[root]; !ok {
		ctx.labels[root] = ctx.assembler.Label()
	}
	ctx.back = ctx.labels[root]
	if ctx.nativeLoop && ctx.leaf {
		ctx.budget = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(ctx.budget, ctx.pin(scratchCtrl), int16(journal.CellBudget*8)))
	}
	if len(plan.Carried) > 0 && !l.carry(ctx, plan.Carried, plan.Anchor.IP) {
		return false
	}
	if plan.Kind == jit.EntryLoop && plan.Hoist != nil && !l.hoist(ctx, *plan.Hoist, plan.Anchor.IP) {
		return false
	}
	ctx.assembler.Bind(ctx.back)
	if !l.emitBlock(ctx, root, nil) {
		return false
	}
	for id, block := range ctx.blocks {
		if id == root || block.Tail || block.State == nil {
			continue
		}
		ctx.assembler.Bind(ctx.labels[id])
		if !l.emitBlock(ctx, id, nil) {
			return false
		}
	}
	for n := 0; n < len(ctx.work); n++ {
		work := ctx.work[n]
		ctx.values = work.values
		ctx.frames = work.frames
		ctx.assembler.Bind(work.label)
		l.clearLocals(ctx)
		l.reload(ctx)
		if !l.emitBlock(ctx, work.block, work.tail) {
			return false
		}
	}
	return l.emitExits(ctx)
}
