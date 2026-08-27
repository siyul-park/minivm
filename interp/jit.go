package interp

import (
	"errors"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"
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
	Lower(a *asm.Assembler, input *jit.Input, p jit.Plan, nativeLoop bool) ([]exitDescriptor, bool)
}

type module struct {
	entries map[jit.Anchor]native
	bytes   int
}

type native struct {
	callable  asm.Callable
	kind      jit.EntryKind
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
	anchor   jit.Anchor
	frontend prof.Frontend
	outcome  prof.CompileOutcome
	reason   prof.CompileReason
	err      error
}

// noSpillArch wraps an asm.Arch to force Build to reject spilling instead of
// inserting a spill frame. A nil Frame already disables spilling per asm's
// own contract (see asm.Frame's doc comment), so this policy needs no
// dedicated asm-level API — it is purely an interp-side JIT policy decision
// (see jit.Plan.NoSpill), not a generic assembler concern.
type noSpillArch struct{ asm.Arch }

func (c *compiler) Close() error {
	return c.buffer.Free()
}

// Compile selects and lowers the first frontend that emits native code. The
// caller supplies the compile-time snapshot: producing one is the
// interpreter's job (see Interpreter.compileSnapshot), not the compiler's.
func (c *compiler) Compile(input *jit.Input, root jit.Anchor) compileResult {
	// Entry roots go to the static frontend first: it plans the whole function
	// deterministically and covers opcodes no trace can record. Loop roots go
	// to the trace frontend first, because a recorded loop specializes its
	// body to the path actually taken - folded legs, a hoisted container - and
	// the static loop plan is the fallback for a loop no trace could record.
	frontends := [...]struct {
		kind prof.Frontend
		plan func(*jit.Input) ([]jit.Plan, error)
	}{{prof.FrontendStatic, jit.StaticPlan}, {prof.FrontendTrace, jit.TracePlan}}
	if root.IP != 0 {
		frontends[0], frontends[1] = frontends[1], frontends[0]
	}
	result := compileResult{anchor: root, outcome: prof.CompileOutcomeEmpty, reason: prof.CompileReasonNoPlan}
	for _, frontend := range frontends {
		plans, err := frontend.plan(input)
		if err != nil {
			return compileResult{anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeError, reason: prof.CompileReasonError, err: err}
		}
		result = result.prefer(compileResult{anchor: root, frontend: frontend.kind, outcome: prof.CompileOutcomeEmpty, reason: prof.CompileReasonNoPlan})
		mod := &module{entries: map[jit.Anchor]native{}}
		for _, plan := range plans {
			if plan.Anchor != root {
				continue
			}
			if !plan.Valid() {
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

func (c *compiler) compile(input *jit.Input, plan jit.Plan, mod *module, frontend prof.Frontend) (prof.CompileReason, error) {
	arch := c.arch
	if plan.NoSpill {
		arch = noSpillArch{c.arch}
	}
	nativeLoop := plan.Kind == jit.EntryLoop
	reason, err := c.emit(input, plan, mod, frontend, arch, nativeLoop)
	if reason != prof.CompileReasonRegisterPressure {
		return reason, err
	}
	if len(plan.Carried) > 0 {
		plan.Carried = nil
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

func (c *compiler) emit(input *jit.Input, plan jit.Plan, mod *module, frontend prof.Frontend, arch asm.Arch, nativeLoop bool) (prof.CompileReason, error) {
	asmb := asm.New(arch)
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
	return c.publish(mod, plan.Anchor, asmb, c.arch, native{kind: plan.Kind, frontend: frontend, exits: exits, resumable: resumable})
}

func (c *compiler) publish(mod *module, a jit.Anchor, asmb *asm.Assembler, arch asm.Arch, n native) (prof.CompileReason, error) {
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
