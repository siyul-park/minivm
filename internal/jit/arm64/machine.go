// Package arm64 is the ARM64 JIT backend. Its machine implements the stable
// jit.Machine contract and the backend.Machine lowering contract.
package arm64

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// machine is the ARM64 JIT target. It implements the stable jit.Machine
// contract and the internal backend.Machine lowering contract. All target
// state shared across compiles is the immutable scratch-register binding.
type machine struct {
	scratch []asm.PReg
}

// lowering carries symbolic values, inlined activations, deferred blocks, and
// cold exits while one plan is emitted. It contains no planner source objects.
type lowering struct {
	assembler *asm.Assembler
	blocks    []jit.Block
	labels    map[int]asm.Label
	constants []types.Boxed
	globals   []types.Kind
	objects   jit.Objects
	scratch   []asm.PReg
	layout    jit.Layout
	head      asm.Label
	back      asm.Label
	budget    asm.VReg

	values    []value
	frames    []activation
	work      []work
	sideExits []sideExit
	exits     []jit.Exit
	saved     []value

	addr       int
	loopRoot   int
	params     int
	returns    int
	kind       jit.EntryKind
	leaf       bool
	nativeLoop bool
	carried    []carriedLocal

	// hoist caches one loop-invariant container's slice header, derived by a
	// per-entry prologue (see machine.hoist). The registers are pure
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

// Boxing masks used by scalar lowering. The i64 payload width is the boxed
// value field's own width, derived rather than restated: a change to the kind
// tag's width moves it, and a literal here would desync silently.
const (
	maskI32 = uint64(0xFFFFFFFF)
	maskI64 = uint64(types.VMask)

	boxableWidth = uint8(types.VBits)
)

// scratchStack..scratchCtrl index the physical registers a lowering context
// pins the frame journal header into on external entry (see
// machine.enter); scratchCount is their count.
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

// New returns the ARM64 JIT backend, a jit.Machine that lowers a compiler
// plan into native ARM64 code. It is this package's only exported symbol;
// the arch selector one level above (interp) picks it when the running
// process is arm64 and hands it to jit.New alongside internal/asm/arm64's
// concrete architecture.
func New() machine {
	return machine{scratch: []asm.PReg{arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14}}
}

// Lowers reports whether this machine emits native code for code. It is the
// opcode ratchet the port advances: a function holding an opcode missing here
// is refused whole, and its root compiles through the plan pipeline.
//
// It restates the set exec has a rule for, because the seam asks the question
// before anything is planned and no answer can come from emitting. The two
// disagreeing costs a wasted compile rather than a wrong one: an opcode named
// here that exec declines abandons the compile it had already begun, and one
// exec handles but this refuses is simply never reached.
func (m machine) Lowers(code instr.Opcode) bool {
	switch code {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_MUL,
		instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U,
		instr.I32_AND, instr.I32_OR, instr.I32_XOR,
		instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U,
		instr.I32_EQZ, instr.I32_EQ, instr.I32_NE,
		instr.I32_LT_S, instr.I32_LE_S, instr.I32_GT_S, instr.I32_GE_S,
		instr.I32_LT_U, instr.I32_LE_U, instr.I32_GT_U, instr.I32_GE_U,
		instr.I64_ADD, instr.I64_SUB, instr.I64_MUL,
		instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U,
		instr.I64_AND, instr.I64_OR, instr.I64_XOR, instr.I64_EQZ,
		instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_LE_S,
		instr.I64_GT_S, instr.I64_GE_S, instr.I64_LT_U, instr.I64_LE_U,
		instr.I64_GT_U, instr.I64_GE_U, instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U,
		instr.I32_TO_I64_S, instr.I32_TO_I64_U,
		instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV,
		instr.F32_ABS, instr.F32_NEG, instr.F32_SQRT,
		instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_LE, instr.F32_GT, instr.F32_GE,
		instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV,
		instr.F64_ABS, instr.F64_NEG, instr.F64_SQRT,
		instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_LE, instr.F64_GT, instr.F64_GE,
		instr.ARRAY_GET, instr.ARRAY_SET, instr.STRUCT_GET, instr.STRUCT_SET, instr.CALL:
		return true
	default:
		return false
	}
}

// Traps reports whether lowering code ends the block by handing control back.
// Nothing this machine lowers does: an opcode it cannot compute it declines
// outright rather than running as an exit, because an unconditional exit ends
// the block, and every exit this machine emits is the cold path behind a
// guard the hot path falls through.
func (m machine) Traps(instr.Opcode) bool {
	return false
}

// Open begins one compile.
func (m machine) Open(c *backend.Compiler) backend.Lowering {
	return &emitter{c: c, a: c.Asm(), scratch: m.scratch, seen: make([]bool, c.Func().Len())}
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
	fn := ctx.objects.Function(ctx.frame().addr)
	if fn == nil || ip < 0 || ip >= len(fn.Code) {
		return prof.OpcodeNone
	}
	return int(fn.Code[ip])
}

// elemShape resolves the element storage of the primitive typed array the
// snapshot recorded at addr, and reports false where a constant container has
// no native load: a ref array carries no primitive element, and an i64 element
// may be heap-promoted, which is a ref rather than a value (see guardI64).
func (ctx *lowering) elemShape(addr int) (jit.ElemShape, bool) {
	shape, ok := jit.ElemShapeByItab(ctx.objects[addr].Array)
	if !ok || shape.Kind == types.KindI64 {
		return jit.ElemShape{}, false
	}
	return shape, true
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
	id := len(ctx.exits)
	ctx.exits = append(ctx.exits, jit.Exit{Reason: reason, Opcode: opcode})
	ctx.sideExits = append(ctx.sideExits, sideExit{
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

// enter opens the framed callable: the entry at offset zero mirrors the
// journal header into the pinned scratch registers, dispatches an external
// bridge re-entry to its resume block (see dispatch), then the internal head —
// the BL target for recursive trace calls — saves the link register. A
// recursive self-call branches straight to head and never runs the dispatch,
// because it always starts its callee at IP zero.
func (m machine) enter(ctx *lowering) {
	a := ctx.assembler
	a.Emit(
		arm64.MOV(ctx.scratch[scratchCtrl], arm64.X0),
		arm64.LDP(ctx.scratch[scratchStack], ctx.scratch[scratchGlobals], ctx.scratch[scratchCtrl], int16(journal.CellStack*8)),
		arm64.LDP(ctx.scratch[scratchBP], ctx.scratch[scratchSP], ctx.scratch[scratchCtrl], int16(journal.CellBP*8)),
	)
	vCtrl := ctx.pin(scratchCtrl)
	active := ctx.pinTo(arm64.X15)
	a.Emit(arm64.LDR(active, vCtrl, int16(journal.CellActive*8)))
	m.dispatch(ctx, vCtrl)
	a.Bind(ctx.head)
	m.zeroLocals(ctx)
}

func (m machine) base(ctx *lowering, vStack asm.VReg) asm.VReg {
	if ctx.leaf {
		addr := ctx.pin(scratchSP)
		m.baseTo(ctx, vStack, addr)
		return addr
	}
	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	m.baseTo(ctx, vStack, addr)
	return addr
}

func (m machine) baseTo(ctx *lowering, vStack, addr asm.VReg) {
	vBP := ctx.pin(scratchBP)
	ctx.assembler.Emit(arm64.LSLI(addr, vBP, 3))
	ctx.assembler.Emit(arm64.ADD(addr, vStack, addr))
}

// Compile emits the whole native entry anchored at root from SSA, through
// this package's own machine. It is the seam the port advances behind: a root
// holding anything that machine has not learned yet is declined here, and
// jit.Compiler falls back to Lower's plan pipeline for it.
func (m machine) Compile(a *asm.Assembler, input *jit.Input, root jit.Anchor) (jit.Entry, bool) {
	if len(m.scratch) < scratchCount {
		return jit.Entry{}, false
	}
	return backend.Root(m, a, input, root)
}

// Lower lowers plan p into a for one native entry, reporting the exits it
// queued and whether lowering succeeded. It is the jit.Machine interface's
// seam with the architecture-neutral compiler (see internal/jit/compiler.go):
// the compiler picks the arch and builds a, and everything from here down is
// ARM64 lowering state and mechanics.
func (m machine) Lower(a *asm.Assembler, input *jit.Input, p jit.Plan, nativeLoop bool) ([]jit.Exit, bool) {
	if len(m.scratch) < scratchCount {
		return nil, false
	}
	ctx := m.newLowering(input, a)
	ctx.nativeLoop = nativeLoop
	if !m.lower(ctx, p) {
		return nil, false
	}
	return append([]jit.Exit(nil), ctx.exits...), true
}

// newLowering builds the lowering context one plan is emitted through.
func (m machine) newLowering(input *jit.Input, a *asm.Assembler) *lowering {
	ctx := &lowering{
		assembler: a,
		labels:    map[int]asm.Label{},
		constants: input.Constants,
		globals:   input.Globals,
		objects:   input.Objects,
		scratch:   m.scratch[:scratchCount],
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
func (m machine) lower(ctx *lowering, plan jit.Plan) bool {
	ctx.leaf = true
	for _, block := range plan.Blocks {
		for _, step := range block.Steps {
			if step.Op.Writes(instr.Frame) {
				ctx.leaf = false
			}
		}
	}
	// blocks, kind, and labels must exist before enter, because enter's entry
	// dispatch (see machine.dispatch) branches to a bridge block's label
	// on external re-entry.
	ctx.blocks = plan.Blocks
	ctx.kind = plan.Kind
	for id, block := range ctx.blocks {
		if !block.Tail && block.State != nil {
			ctx.labels[id] = ctx.assembler.Label()
		}
	}
	m.enter(ctx)
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
	if len(plan.Carried) > 0 && !m.carry(ctx, plan.Carried, plan.Anchor.IP) {
		return false
	}
	if plan.Kind == jit.EntryLoop && plan.Hoist != nil && !m.hoist(ctx, *plan.Hoist, plan.Anchor.IP) {
		return false
	}
	ctx.assembler.Bind(ctx.back)
	if !m.emitBlock(ctx, root, nil) {
		return false
	}
	for id, block := range ctx.blocks {
		if id == root || block.Tail || block.State == nil {
			continue
		}
		ctx.assembler.Bind(ctx.labels[id])
		if !m.emitBlock(ctx, id, nil) {
			return false
		}
	}
	for n := 0; n < len(ctx.work); n++ {
		work := ctx.work[n]
		ctx.values = work.values
		ctx.frames = work.frames
		ctx.assembler.Bind(work.label)
		m.clearLocals(ctx)
		m.reload(ctx)
		if !m.emitBlock(ctx, work.block, work.tail) {
			return false
		}
	}
	return m.emitExits(ctx)
}
