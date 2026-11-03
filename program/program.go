package program

import (
	"fmt"
	"slices"
	"strings"

	"github.com/siyul-park/minivm/types"

	"github.com/siyul-park/minivm/instr"
)

// Program is a loadable unit: top-level code plus its local, global, constant, and type pools and exception handlers.
type Program struct {
	Code      []byte
	Locals    []types.Type
	Globals   []types.Type
	Constants []types.Value
	Types     []types.Type
	Handlers  []instr.Handler
}

// WithConstants sets the constant pool.
func WithConstants(consts ...types.Value) func(*Program) {
	return func(p *Program) {
		p.Constants = consts
	}
}

// WithTypes sets the type pool.
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

// New returns a program for instrs configured by options.
func New(instrs []instr.Instruction, options ...func(*Program)) *Program {
	p := &Program{Code: instr.Marshal(instrs)}
	for _, opt := range options {
		opt(p)
	}
	return p
}

// Clone returns a snapshot that shares no mutable state with p: Code, Locals,
// Globals, Constants, Types, and Handlers get fresh slices, and the constant
// graph is copied. Every *types.Function, including one nested in an array,
// map, or struct literal, is replaced by a copy owning its Code, Locals,
// Captures, and Handlers; literals are rebuilt around copied children and other
// mutable constants are cloned (types.Clone). An object reachable from several
// positions is copied once, so the snapshot shares it as p does. Type objects,
// including each function's signature, stay shared and must not be mutated. A
// nil receiver returns nil.
func (p *Program) Clone() *Program {
	if p == nil {
		return nil
	}
	c := &Program{
		Code:      slices.Clone(p.Code),
		Locals:    slices.Clone(p.Locals),
		Globals:   slices.Clone(p.Globals),
		Constants: slices.Clone(p.Constants),
		Types:     slices.Clone(p.Types),
		Handlers:  slices.Clone(p.Handlers),
	}
	copies := make(map[any]types.Value)
	for i, v := range c.Constants {
		c.Constants[i] = cloneConstant(v, copies)
	}
	return c
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
				writeEntry(&sb, i, fn.String())
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
		writeEntry(sb, i, item.String())
	}
}

func writeEntry(sb *strings.Builder, index int, s string) {
	head, tail, _ := strings.Cut(s, "\n")
	sb.WriteString(fmt.Sprintf("%04d:\t%s\n", index, head))
	for tail != "" {
		var line string
		line, tail, _ = strings.Cut(tail, "\n")
		sb.WriteString(fmt.Sprintf("\t%s\n", line))
	}
}

func cloneConstant(v types.Value, copies map[any]types.Value) types.Value {
	id, aliased := types.Identity(v)
	if aliased {
		if c, ok := copies[id]; ok {
			return c
		}
	}
	var c types.Value
	if elems, rebuild := children(v); rebuild != nil {
		cloned := make([]types.Value, len(elems))
		for i, elem := range elems {
			cloned[i] = cloneConstant(elem, copies)
		}
		c = rebuild(cloned)
	} else if fn, ok := v.(*types.Function); ok {
		c = fn.Clone()
	} else {
		c = types.Clone(v)
	}
	if aliased {
		copies[id] = c
	}
	return c
}

// children returns the constants held by an array, map, or struct literal
// and a function rebuilding the literal around replacements for them. Both are
// nil for any other value. A map literal's children are its keys followed by its values.
func children(v types.Value) ([]types.Value, func([]types.Value) types.Value) {
	switch v := v.(type) {
	case *types.ArrayLiteral:
		return v.Elems, func(elems []types.Value) types.Value {
			return &types.ArrayLiteral{Typ: v.Typ, Elems: elems}
		}
	case *types.MapLiteral:
		return slices.Concat(v.Keys, v.Values), func(elems []types.Value) types.Value {
			n := len(v.Keys)
			return &types.MapLiteral{Typ: v.Typ, Keys: elems[:n:n], Values: elems[n:]}
		}
	case *types.StructLiteral:
		return v.Fields, func(elems []types.Value) types.Value {
			return &types.StructLiteral{Typ: v.Typ, Fields: elems}
		}
	}
	return nil, nil
}
