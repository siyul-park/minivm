package interp

import (
	"errors"
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

type compiler struct {
	arch    asm.Arch
	buffer  *asm.Buffer
	machine machine
}

// machine lowers a plan into an assembler for one architecture. All lowering
// state — the symbolic value stack, inlined activations, deferred work, and
// queued exits — lives on the machine's side of this seam; the compiler picks
// the arch, builds the assembler, and hands both to the machine, which emits
// instructions and reports the exits it queued.
type machine interface {
	Lower(a *asm.Assembler, input *compileInput, p plan, nativeLoop bool) ([]exitDescriptor, bool)
}

// layout is the set of runtime struct offsets a lowerer needs to reach into
// interp's private types (HostStruct, field, conversion, coroutine) without
// naming them. input computes it once, in architecture-neutral code where
// those types are visible, and lowering carries the copy from compileInput to
// every lowering site — the same offsets a future package boundary would hand
// across unchanged once the backend that consumes them moves out of interp.
type layout struct {
	hostFields      int
	hostPtr         int
	hostFieldOffset int
	hostFieldConv   int
	hostFieldSize   int
	hostConvKind    int
	coroValue       int
	coroDone        int
}

type module struct {
	entries map[anchor]native
	bytes   int
}

type native struct {
	callable  asm.Callable
	kind      entryKind
	frontend  prof.Frontend
	bytes     int
	exits     []exitDescriptor
	resumable []int
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

// watchdog counts native entries, give-up exits, and bridge cycles for one
// installed anchor. Unlike counters, it is always live regardless of
// i.profiler: a give-up exit is one where the entry abandoned the work it was
// compiled for (see givesUp) rather than completing its job or leaving
// through a healthy loop-exit edge, so a high rate of it — not a high exit rate
// alone — is the signal that the installed entry should be retired (see
// Interpreter.retire). A bridge cycle (see Interpreter.bridge) is productive
// work and must never count as one, but an anchor that spends most of its
// entries bridging pays the same deopt/re-enter cost, so it is tracked and
// retired the same way, just through its own counter.
type watchdog struct {
	gaveUp  []bool // exit descriptor ID -> givesUp(reason)
	entries uint32
	giveUps uint32
	bridges uint32
}

type compileResult struct {
	module   *module
	anchor   anchor
	frontend prof.Frontend
	outcome  prof.CompileOutcome
	reason   prof.CompileReason
	err      error
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

// noSpillArch wraps an asm.Arch to force Build to reject spilling instead of
// inserting a spill frame. A nil Frame already disables spilling per asm's
// own contract (see asm.Frame's doc comment), so this policy needs no
// dedicated asm-level API — it is purely an interp-side JIT policy decision
// (see noSpill), not a generic assembler concern.
type noSpillArch struct{ asm.Arch }

// elemShape is how one array element kind is stored: the concrete container
// itab, the byte offset its data begins at, the shift from index to byte, and
// whether the element is raw rather than boxed.
type elemShape struct {
	itab  uintptr
	base  int16
	scale uint8
	raw   bool
}

// Frame-journal layout. X0 carries &journal[0] to native code, which mirrors the
// first cells into pinned scratch registers (X10-X14) on external entry. Header
// cells precede a stack of fixed-stride frame records; each record mirrors the
// int fields the threaded interpreter needs to resume a frame.
const (
	journalStack   = iota // &i.stack[0]; external entry in
	journalGlobals        // &i.globals[0]; external entry in
	journalBP             // current frame bp; external entry in
	journalSP             // interpreter sp; external entry in/out
	journalEntry          // bridge resume IP in; zero starts at the anchor
	journalDepth          // trap-time frame records written; native read/write
	journalCap            // frame budget capped by nativeFrameLimit; read-only
	journalTrap           // exit kind out: trapNone | trapFallback | trapOverflow | trapYield | trapBridge
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
	// trapBridge reports the IP of one opcode the backend cannot lower.
	// journalNextIP carries that opcode's own IP; the Go wrapper runs its
	// threaded closure exactly once and re-enters the same callable at the
	// closure's new IP (see Interpreter.bridge and arm64Lowerer.dispatch).
	trapBridge
)

// nativeFrameLimit caps generated call depth to the stack space reserved by
// the ARM64 invoke trampoline. Deeper calls trap before moving SP.
const nativeFrameLimit = 128

// loopBudget is how many native loop back-edges run between safepoints. It is
// independent of tick so a hot loop amortizes the deopt/re-enter cost of a
// yield over many iterations while still polling for cancellation and fuel.
const loopBudget = 1 << 13

// retireWindow is the number of native entries a watchdog observes before
// judging whether an installed anchor is paying for itself (see watchdog).
const retireWindow = 1024

// retireGiveUpThreshold is the minimum count of give-up exits (see givesUp)
// within one retireWindow that marks the anchor as a net loss rather than a
// healthy kernel's normal loop-exit traffic.
const retireGiveUpThreshold = retireWindow / 4

// arrayElems is where a ref array's elements begin. It sits here with the
// shape table rather than with the lowering offsets because the portable
// planner reads it through elemShapes.
const arrayElems = int(unsafe.Offsetof(types.Array{}.Elems))

// The heap itabs the backends and the planner compare against. They are
// runtime type identities rather than lowering detail, so they belong with
// the portable JIT core: jit_plan.go resolves element layout on every
// architecture, including ones with no native backend at all.
var (
	heapI32        = itab(types.I32(0))
	heapF32        = itab(types.F32(0))
	heapF64        = itab(types.F64(0))
	heapArrayI1    = itab(types.TypedArray[bool](nil))
	heapArrayI8    = itab(types.TypedArray[int8](nil))
	heapArrayI32   = itab(types.TypedArray[int32](nil))
	heapArrayI64   = itab(types.TypedArray[int64](nil))
	heapArrayF32   = itab(types.TypedArray[float32](nil))
	heapArrayF64   = itab(types.TypedArray[float64](nil))
	heapArrayRef   = itab((*types.Array)(nil))
	heapString     = itab(types.String(""))
	heapStruct     = itab((*types.Struct)(nil))
	heapHostStruct = itab((*HostStruct)(nil))
	heapError      = itab((*types.Error)(nil))
	heapCoroutine  = itab((*coroutine)(nil))
)

// elemShapes is the one place the element storage layout is written down.
// arrayGet, arraySet, arrayLen, and the planner's hoist eligibility all resolve
// through it, so a new element kind is one row rather than an edit to each.
var elemShapes = []struct {
	kind  types.Kind
	shape elemShape
}{
	{types.KindI1, elemShape{itab: heapArrayI1, raw: true}},
	{types.KindI8, elemShape{itab: heapArrayI8, raw: true}},
	{types.KindI32, elemShape{itab: heapArrayI32, scale: 2, raw: true}},
	{types.KindI64, elemShape{itab: heapArrayI64, scale: 3, raw: true}},
	{types.KindF32, elemShape{itab: heapArrayF32, scale: 2, raw: true}},
	{types.KindF64, elemShape{itab: heapArrayF64, scale: 3, raw: true}},
	{types.KindRef, elemShape{itab: heapArrayRef, base: int16(arrayElems)}},
}

// elemShapeOf resolves the storage shape of an element kind.
func elemShapeOf(kind types.Kind) (elemShape, bool) {
	for _, row := range elemShapes {
		if row.kind == kind {
			return row.shape, true
		}
	}
	return elemShape{}, false
}

// elemShapeByItab resolves the storage shape of a container's concrete itab.
func elemShapeByItab(want uintptr) (elemShape, bool) {
	for _, row := range elemShapes {
		if row.shape.itab == want {
			return row.shape, true
		}
	}
	return elemShape{}, false
}

// hostShape is how one Go field kind sits in memory. kind is the VM kind its
// conversion produces, size is the width of the Go field, and signed is the
// extension a field narrower than its slot widens with; a float row is signed
// because the VM holds a float's bit pattern sign-extended, as it holds an i32.
type hostShape struct {
	kind   types.Kind
	size   uintptr
	signed bool
}

// hostShapes is the one place the memory layout of a hosted Go field is written
// down, indexed by the reflect.Kind the codec compiled the field through. It
// mirrors the leaves table the codec picks a conversion from, and holds a row
// exactly where that conversion is a plain load or store: a string, pointer, or
// container field publishes a heap reference instead, so it has no row and its
// access stays with the interpreter.
var hostShapes = [...]hostShape{
	reflect.Bool:    {kind: types.KindI1, size: unsafe.Sizeof(false)},
	reflect.Int8:    {kind: types.KindI8, size: unsafe.Sizeof(int8(0)), signed: true},
	reflect.Int16:   {kind: types.KindI32, size: unsafe.Sizeof(int16(0)), signed: true},
	reflect.Int32:   {kind: types.KindI32, size: unsafe.Sizeof(int32(0)), signed: true},
	reflect.Int:     {kind: types.KindI64, size: unsafe.Sizeof(int(0)), signed: true},
	reflect.Int64:   {kind: types.KindI64, size: unsafe.Sizeof(int64(0)), signed: true},
	reflect.Uint8:   {kind: types.KindI32, size: unsafe.Sizeof(uint8(0))},
	reflect.Uint16:  {kind: types.KindI32, size: unsafe.Sizeof(uint16(0))},
	reflect.Uint32:  {kind: types.KindI32, size: unsafe.Sizeof(uint32(0))},
	reflect.Uint:    {kind: types.KindI64, size: unsafe.Sizeof(uint(0))},
	reflect.Uint64:  {kind: types.KindI64, size: unsafe.Sizeof(uint64(0))},
	reflect.Uintptr: {kind: types.KindI64, size: unsafe.Sizeof(uintptr(0))},
	reflect.Float32: {kind: types.KindF32, size: unsafe.Sizeof(float32(0)), signed: true},
	reflect.Float64: {kind: types.KindF64, size: unsafe.Sizeof(float64(0)), signed: true},
}

// hostShapeOf resolves the layout of a Go field kind, and reports false where
// the kind has no row.
func hostShapeOf(kind reflect.Kind) (hostShape, bool) {
	if int(kind) >= len(hostShapes) {
		return hostShape{}, false
	}
	shape := hostShapes[kind]
	return shape, shape.size != 0
}

// slotShapes is the width a VM slot holds a raw scalar in, and whether it holds
// it sign-extended. A host field as wide as its slot is the slot's exact image,
// so a read reinterprets it and a write stores it whole.
var slotShapes = [...]struct {
	size   uintptr
	signed bool
}{
	types.KindI1:  {size: 1},
	types.KindI8:  {size: 1, signed: true},
	types.KindI32: {size: 4, signed: true},
	types.KindI64: {size: 8, signed: true},
	types.KindF32: {size: 4, signed: true},
	types.KindF64: {size: 8, signed: true},
}

// exact reports whether the Go field of shape s is as wide as a VM slot of
// s.kind, which makes the two the same bytes in either direction. A narrower
// field is not: it decodes through the range check setSigned and setUnsigned
// perform, and a check that can fail belongs with the interpreter that reports
// it, so only an exact field lowers a write. Signedness does not enter, because
// at equal width a conversion only reinterprets the bytes a store already
// writes.
func (s hostShape) exact() bool {
	return s.size == slotShapes[s.kind].size
}

// read is the width and extension a read of the field loads with. An exact
// field is reinterpreted with its slot's own extension, which is how an
// unsigned Go field reaches the guest as the signed VM value its conversion
// casts to; a narrower one widens with its own.
func (s hostShape) read() (uintptr, bool) {
	if s.exact() {
		return s.size, slotShapes[s.kind].signed
	}
	return s.size, s.signed
}

func (c *compiler) Close() error {
	return c.buffer.Free()
}

// Compile selects and lowers the first frontend that emits native code. The
// caller supplies the compile-time snapshot: producing one is the
// interpreter's job (see Interpreter.compileSnapshot), not the compiler's.
func (c *compiler) Compile(input *compileInput, root anchor) compileResult {
	// Entry roots go to the static frontend first: it plans the whole function
	// deterministically and covers opcodes no trace can record. Loop roots go
	// to the trace frontend first, because a recorded loop specializes its
	// body to the path actually taken - folded legs, a hoisted container - and
	// the static loop plan is the fallback for a loop no trace could record.
	frontends := [...]struct {
		kind prof.Frontend
		plan func(*compileInput) ([]plan, error)
	}{{prof.FrontendStatic, staticPlan}, {prof.FrontendTrace, tracePlan}}
	if root.ip != 0 {
		frontends[0], frontends[1] = frontends[1], frontends[0]
	}
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
	arch := c.arch
	if plan.noSpill {
		arch = noSpillArch{c.arch}
	}
	nativeLoop := plan.kind == entryLoop
	reason, err := c.emit(input, plan, mod, frontend, arch, nativeLoop)
	if reason != prof.CompileReasonRegisterPressure {
		return reason, err
	}
	if len(plan.carried) > 0 {
		plan.carried = nil
		reason, err = c.emit(input, plan, mod, frontend, arch, nativeLoop)
		if reason != prof.CompileReasonRegisterPressure {
			return reason, err
		}
	}
	if !nativeLoop {
		return reason, err
	}
	return c.emit(input, plan, mod, frontend, arch, false)
}

func (c *compiler) emit(input *compileInput, plan plan, mod *module, frontend prof.Frontend, arch asm.Arch, nativeLoop bool) (prof.CompileReason, error) {
	asmb := asm.New(arch)
	exits, ok := c.machine.Lower(asmb, input, plan, nativeLoop)
	if !ok {
		return prof.CompileReasonLoweringRejected, nil
	}
	var resumable []int
	for _, block := range plan.blocks {
		if block.bridge {
			resumable = append(resumable, block.anchor.ip)
		}
	}
	return c.publish(mod, plan.anchor, asmb, c.arch, native{kind: plan.kind, frontend: frontend, exits: exits, resumable: resumable})
}

func (c *compiler) publish(mod *module, a anchor, asmb *asm.Assembler, arch asm.Arch, n native) (prof.CompileReason, error) {
	code, err := asmb.Build()
	if err != nil {
		if errors.Is(err, asm.ErrNoRegistersAvailable) {
			return prof.CompileReasonRegisterPressure, nil
		}
		if errors.Is(err, asm.ErrBranchOutOfRange) {
			return prof.CompileReasonBranchRange, nil
		}
		return prof.CompileReasonError, err
	}
	callable, err := asm.Link(c.buffer, arch.ABI(), code)
	if err != nil {
		return prof.CompileReasonError, err
	}
	n.callable = callable
	n.bytes = len(code)
	mod.entries[a] = n
	mod.bytes += len(code)
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

// newWatchdog precomputes, for each of entry's exit descriptors, whether taking
// it means the entry gave up, so the watchdog's hot path only ever indexes a
// []bool keyed by descriptor ID.
func newWatchdog(entry native) *watchdog {
	gaveUp := make([]bool, len(entry.exits))
	for id, exit := range entry.exits {
		gaveUp[id] = givesUp(exit.reason)
	}
	return &watchdog{gaveUp: gaveUp}
}

// givesUp reports whether taking this exit means the native entry abandoned the
// work it was compiled for. A guard failure and a cold branch both say the
// recording predicted the program wrong, and a trace cut says the code knowingly
// stopped mid-function; each pays full bailout and re-entry for nothing. A loop
// exit is how a loop normally ends and a terminal op is a deopt the plan
// intended, so neither counts.
func givesUp(reason prof.ExitReason) bool {
	switch reason {
	case prof.ExitTraceCut, prof.ExitColdBranch,
		prof.ExitGuardKind, prof.ExitGuardShape, prof.ExitGuardBounds, prof.ExitGuardValue:
		return true
	default:
		return false
	}
}

// enter counts one invocation of the installed native entry.
func (w *watchdog) enter() {
	w.entries++
}

// exit counts one give-up fallback exit. encoded is i.journal[journalExitID]
// exactly as counters.exit consumes it: the exit descriptor ID plus one, zero
// meaning no descriptor.
func (w *watchdog) exit(encoded uint64) {
	if encoded == 0 {
		return
	}
	id := int(encoded - 1)
	if id >= 0 && id < len(w.gaveUp) && w.gaveUp[id] {
		w.giveUps++
	}
}

// bridge counts one bridge cycle (see Interpreter.bridge). It is tracked
// separately from exit so a bridge never counts toward the give-up rate.
func (w *watchdog) bridge() {
	w.bridges++
}

// failed reports whether this anchor lost the window it just completed: its
// give-up rate or bridge rate reached retireGiveUpThreshold, so it is retired
// (see Interpreter.retire). A window shorter than retireWindow has decided
// nothing yet; a completed one always resets every counter, so the next window
// starts clean whichever way it went.
func (w *watchdog) failed() bool {
	if w.entries < retireWindow {
		return false
	}
	bad := w.giveUps >= retireGiveUpThreshold || w.bridges >= retireGiveUpThreshold
	w.entries, w.giveUps, w.bridges = 0, 0, 0
	return bad
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
