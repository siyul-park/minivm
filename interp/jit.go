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
