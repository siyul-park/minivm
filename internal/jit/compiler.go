package jit

import (
	"errors"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/prof"
)

// Compiler lowers plans into native code for one target. Target owns the
// architecture and both lowering paths, so the compiler cannot combine a
// machine with a different assembler architecture.
type Compiler struct {
	target Target
	buffer *asm.Buffer
}

// Target owns one architecture's native lowering. The same concrete target
// implements the architecture-neutral plan path and the backend lowering path.
type Target interface {
	Arch() asm.Arch
	Compile(a *asm.Assembler, input *Input, root Anchor) (Entry, bool)
	Lower(a *asm.Assembler, input *Input, p Plan, nativeLoop bool) ([]Exit, bool)
}

// compilerBufferSize is the executable-memory buffer New allocates for a
// Compiler's own compiled code. No caller varies it, so it needs no
// constructor parameter.
const compilerBufferSize = 4096

var ErrInvalidTarget = errors.New("invalid jit target")

// New builds a Compiler for one target.
func New(target Target) (*Compiler, error) {
	if target == nil || target.Arch() == nil {
		return nil, ErrInvalidTarget
	}
	buffer, err := asm.NewBuffer(compilerBufferSize)
	if err != nil {
		return nil, err
	}
	return &Compiler{target: target, buffer: buffer}, nil
}

// Close frees the executable buffer backing this Compiler's compiled code.
func (c *Compiler) Close() error {
	return c.buffer.Free()
}

// Buffer returns the executable buffer backing this Compiler's compiled code.
// The caller MUST take ownership immediately when publishing the compiled code;
// after that transfer the Compiler MUST NOT be closed or reused.
func (c *Compiler) Buffer() *asm.Buffer {
	return c.buffer
}

// Compile selects and lowers the first frontend that emits native code. The
// caller supplies the compile-time snapshot: producing one is the
// interpreter's job, not the Compiler's.
func (c *Compiler) Compile(input *Input, root Anchor) Result {
	if result, ok := c.native(input, root); ok {
		return result
	}
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

// native compiles root through Target.Compile, the seam that plans its own
// root. It is the gate the target advances behind: false means the target
// declined the root, or what it emitted did not build, and the
// assembler is discarded whole either way, so the plan frontends below
// compile the root exactly as they would have. Only a genuine failure is
// reported rather than retried, because the plan path cannot succeed where
// linking executable memory did not.
func (c *Compiler) native(input *Input, root Anchor) (Result, bool) {
	a := asm.New(c.target.Arch())
	entry, ok := c.target.Compile(a, input, root)
	if !ok {
		return Result{}, false
	}
	code := &Code{Entries: map[Anchor]Entry{}}
	reason, err := c.publish(code, root, a, entry)
	if err != nil {
		return Result{Anchor: root, Frontend: entry.Frontend, Outcome: prof.CompileOutcomeError, Reason: prof.CompileReasonError, Err: err}, true
	}
	if reason != prof.CompileReasonNone {
		return Result{}, false
	}
	return Result{Code: code, Anchor: root, Frontend: entry.Frontend, Outcome: prof.CompileOutcomeEmitted}, true
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
	asmb := asm.New(c.target.Arch())
	exits, ok := c.target.Lower(asmb, input, plan, nativeLoop)
	if !ok {
		return prof.CompileReasonLoweringRejected, nil
	}
	var resumable []int
	for _, block := range plan.Blocks {
		if block.Bridge {
			resumable = append(resumable, block.Anchor.IP)
		}
	}
	return c.publish(code, plan.Anchor, asmb, Entry{Kind: plan.Kind, Frontend: frontend, Exits: exits, Resumable: resumable})
}

func (c *Compiler) publish(code *Code, a Anchor, asmb *asm.Assembler, entry Entry) (prof.CompileReason, error) {
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
	callable, err := asm.Link(c.buffer, c.target.Arch().ABI(), built)
	if err != nil {
		return prof.CompileReasonError, err
	}
	entry.Callable = callable
	entry.Bytes = len(built)
	code.Entries[a] = entry
	code.Bytes += len(built)
	return prof.CompileReasonNone, nil
}
