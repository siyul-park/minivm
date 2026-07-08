package program

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/types"

	"github.com/siyul-park/minivm/instr"
)

type Program struct {
	Code      []byte
	Locals    []types.Type
	Globals   []types.Type
	Constants []types.Value
	Types     []types.Type
	Handlers  []instr.Handler
}

func WithConstants(consts ...types.Value) func(*Program) {
	return func(p *Program) {
		p.Constants = consts
	}
}

func WithTypes(types ...types.Type) func(*Program) {
	return func(p *Program) {
		p.Types = types
	}
}

// WithLocals declares the entry frame's local scratch slots, addressable by
// LOCAL_* at the top level just as a function's locals are. It lets a compiler
// hold module-level temporaries in frame locals instead of reserving globals.
func WithLocals(locals ...types.Type) func(*Program) {
	return func(p *Program) {
		p.Locals = locals
	}
}

// WithGlobals declares the module's global slots and their types, forming the
// fixed global table addressed by GLOBAL_GET/SET/TEE; GLOBAL_* past the
// declared count traps. Declaring globals gives each interpreter a pre-sized,
// kind-stable global table so GLOBAL_GET/SET emit native traces at the top
// level. Seed per-run input into a declared slot with Interpreter.SetGlobal
// before Run.
func WithGlobals(globals ...types.Type) func(*Program) {
	return func(p *Program) {
		p.Globals = globals
	}
}

// WithHandlers attaches the exception table for the top-level code (slot 0).
func WithHandlers(handlers ...instr.Handler) func(*Program) {
	return func(p *Program) {
		p.Handlers = handlers
	}
}

func New(instrs []instr.Instruction, options ...func(*Program)) *Program {
	p := &Program{Code: instr.Marshal(instrs)}
	for _, opt := range options {
		opt(p)
	}
	return p
}

func (p *Program) String() string {
	var sb strings.Builder
	sb.WriteString(".code\n")
	sb.WriteString(instr.Format(p.Code))
	if len(p.Locals) > 0 {
		sb.WriteString(".locals\n")
		writeIndexed(&sb, p.Locals)
	}
	if len(p.Globals) > 0 {
		sb.WriteString(".globals\n")
		writeIndexed(&sb, p.Globals)
	}
	if len(p.Constants) > 0 {
		sb.WriteString(".constants\n")
		for i, v := range p.Constants {
			if fn, ok := v.(*types.Function); ok {
				head, tail, _ := strings.Cut(fn.String(), "\n")
				sb.WriteString(fmt.Sprintf("%04d:\t%s\n", i, head))
				for tail != "" {
					var line string
					line, tail, _ = strings.Cut(tail, "\n")
					sb.WriteString(fmt.Sprintf("\t%s\n", line))
				}
			} else {
				sb.WriteString(fmt.Sprintf("%04d:\t%s %s\n", i, v.Type().String(), v.String()))
			}
		}
	}
	if len(p.Types) > 0 {
		sb.WriteString(".types\n")
		writeIndexed(&sb, p.Types)
	}
	if len(p.Handlers) > 0 {
		sb.WriteString(".handlers\n")
		for i, h := range p.Handlers {
			sb.WriteString(fmt.Sprintf("%04d:\tstart=%d end=%d catch=%d depth=%d\n", i, h.Start, h.End, h.Catch, h.Depth))
		}
	}
	return sb.String()
}

func writeIndexed[T fmt.Stringer](sb *strings.Builder, items []T) {
	for i, item := range items {
		head, tail, _ := strings.Cut(item.String(), "\n")
		sb.WriteString(fmt.Sprintf("%04d:\t%s\n", i, head))
		for tail != "" {
			var line string
			line, tail, _ = strings.Cut(tail, "\n")
			sb.WriteString(fmt.Sprintf("\t%s\n", line))
		}
	}
}
