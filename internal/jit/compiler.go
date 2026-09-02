package jit

import (
	"errors"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/prof"
)

// Compiler lowers plans into native code for one architecture. New's caller
// picks that architecture and the Machine that lowers against it; every
// Compile call links its output into this Compiler's own executable buffer.
type Compiler struct {
	arch    asm.Arch
	buffer  *asm.Buffer
	machine Machine
}

// Machine lowers a plan into an assembler for one architecture. All lowering
// state — the symbolic value stack, inlined activations, deferred work, and
// queued exits — lives on the machine's side of this seam; the Compiler picks
// the arch, builds the assembler, and hands both to the machine, which emits
// instructions and reports the exits it queued.
type Machine interface {
	Lower(a *asm.Assembler, input *Input, p Plan, nativeLoop bool) ([]Exit, bool)
}

// compilerBufferSize is the executable-memory buffer New allocates for a
// Compiler's own compiled code. No caller varies it, so it needs no
// constructor parameter.
const compilerBufferSize = 4096

// New builds a Compiler for one architecture. arch and machine are the
// caller's choice: an arch selector one level above this package is the one
// that knows which ISA is actually available, so New accepts both rather than
// choosing internally.
func New(arch asm.Arch, machine Machine) (*Compiler, error) {
	buffer, err := asm.NewBuffer(compilerBufferSize)
	if err != nil {
		return nil, err
	}
	return &Compiler{arch: arch, buffer: buffer, machine: machine}, nil
}

// Close frees the executable buffer backing this Compiler's compiled code.
func (c *Compiler) Close() error {
	return c.buffer.Free()
}

// Buffer returns the executable buffer backing this Compiler's compiled code,
// for a caller that takes over its lifetime — publishing it into a
// longer-lived cache, say — instead of calling Close.
func (c *Compiler) Buffer() *asm.Buffer {
	return c.buffer
}

// Compile selects and lowers the first frontend that emits native code. The
// caller supplies the compile-time snapshot: producing one is the
// interpreter's job, not the Compiler's.
func (c *Compiler) Compile(input *Input, root Anchor) Result {
	// Entry roots go to the static frontend first: it plans the whole function
	// deterministically and covers opcodes no trace can record. Loop roots go
	// to the trace frontend first, because a recorded loop specializes its
	// body to the path actually taken - folded legs, a hoisted container - and
	// the static loop plan is the fallback for a loop no trace could record.
	frontends := [...]struct {
		kind prof.Frontend
		plan func(*Input) ([]Plan, error)
	}{{prof.FrontendStatic, StaticPlan}, {prof.FrontendTrace, TracePlan}}
	if root.IP != 0 {
		frontends[0], frontends[1] = frontends[1], frontends[0]
	}
	result := Result{Anchor: root, Outcome: prof.CompileOutcomeEmpty, Reason: prof.CompileReasonNoPlan}
	for _, frontend := range frontends {
		plans, err := frontend.plan(input)
		if err != nil {
			return Result{Anchor: root, Frontend: frontend.kind, Outcome: prof.CompileOutcomeError, Reason: prof.CompileReasonError, Err: err}
		}
		result = result.prefer(Result{Anchor: root, Frontend: frontend.kind, Outcome: prof.CompileOutcomeEmpty, Reason: prof.CompileReasonNoPlan})
		code := &Code{Entries: map[Anchor]Entry{}}
		for _, plan := range plans {
			if plan.Anchor != root {
				continue
			}
			if !plan.Valid() {
				result = result.prefer(Result{Anchor: root, Frontend: frontend.kind, Outcome: prof.CompileOutcomeRejected, Reason: prof.CompileReasonInvalidPlan})
				continue
			}
			reason, err := c.compile(input, plan, code, frontend.kind)
			if err != nil {
				return Result{Anchor: root, Frontend: frontend.kind, Outcome: prof.CompileOutcomeError, Reason: prof.CompileReasonError, Err: err}
			}
			if reason != prof.CompileReasonNone {
				result = result.prefer(Result{Anchor: root, Frontend: frontend.kind, Outcome: prof.CompileOutcomeRejected, Reason: reason})
				continue
			}
		}
		if len(code.Entries) > 0 {
			return Result{Code: code, Anchor: root, Frontend: frontend.kind, Outcome: prof.CompileOutcomeEmitted}
		}
	}
	return result
}

func (c *Compiler) compile(input *Input, plan Plan, code *Code, frontend prof.Frontend) (prof.CompileReason, error) {
	nativeLoop := plan.Kind == EntryLoop
	if !nativeLoop && frontend == prof.FrontendStatic {
		// Static whole-function plans may own a hot loop before its trace root exists.
		// Keep the same native safepoint machinery so the loop can hand off later.
		for _, block := range plan.Blocks {
			for _, edge := range block.Term.Edges {
				if edge.Anchor.Addr == block.Anchor.Addr && edge.Anchor.IP <= block.Anchor.IP {
					nativeLoop = true
					break
				}
			}
			if nativeLoop {
				break
			}
		}
	}
	reason, err := c.emit(input, plan, code, frontend, nativeLoop)
	if reason != prof.CompileReasonRegisterPressure {
		return reason, err
	}
	if len(plan.Carried) > 0 {
		plan.Carried = nil
		reason, err = c.emit(input, plan, code, frontend, nativeLoop)
		if reason != prof.CompileReasonRegisterPressure {
			return reason, err
		}
	}
	if !nativeLoop {
		return reason, err
	}
	return c.emit(input, plan, code, frontend, false)
}

func (c *Compiler) emit(input *Input, plan Plan, code *Code, frontend prof.Frontend, nativeLoop bool) (prof.CompileReason, error) {
	asmb := asm.New(c.arch)
	exits, ok := c.machine.Lower(asmb, input, plan, nativeLoop)
	if !ok {
		return prof.CompileReasonLoweringRejected, nil
	}
	var resumable []int
	for _, block := range plan.Blocks {
		if block.Bridge {
			resumable = append(resumable, block.Anchor.IP)
		}
	}
	return c.publish(code, plan.Anchor, asmb, c.arch, Entry{Kind: plan.Kind, Frontend: frontend, Exits: exits, Resumable: resumable})
}

func (c *Compiler) publish(code *Code, a Anchor, asmb *asm.Assembler, arch asm.Arch, entry Entry) (prof.CompileReason, error) {
	built, err := asmb.Build()
	if err != nil {
		if errors.Is(err, asm.ErrNoRegistersAvailable) {
			return prof.CompileReasonRegisterPressure, nil
		}
		if errors.Is(err, asm.ErrBranchOutOfRange) {
			return prof.CompileReasonBranchRange, nil
		}
		return prof.CompileReasonError, err
	}
	callable, err := asm.Link(c.buffer, arch.ABI(), built)
	if err != nil {
		return prof.CompileReasonError, err
	}
	entry.Callable = callable
	entry.Bytes = len(built)
	code.Entries[a] = entry
	code.Bytes += len(built)
	return prof.CompileReasonNone, nil
}
