package program

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

const textFormatVersion = 1

type Program struct {
	Code      []byte
	Locals    []types.Type
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
	sb.WriteString(fmt.Sprintf(".version %d\n\n", textFormatVersion))
	writeCodeSection(&sb, p.Code)
	if len(p.Locals) > 0 {
		writeSection(&sb, ".locals")
		writeIndexed(&sb, p.Locals)
	}
	if len(p.Constants) > 0 {
		writeSection(&sb, ".constants")
		writeConstants(&sb, p.Constants)
	}
	if len(p.Types) > 0 {
		writeSection(&sb, ".types")
		writeIndexed(&sb, p.Types)
	}
	if len(p.Handlers) > 0 {
		writeSection(&sb, ".handlers")
		writeHandlers(&sb, p.Handlers)
	}
	return sb.String()
}

func writeCodeSection(sb *strings.Builder, code []byte) {
	sb.WriteString(".code\n")
	sb.WriteString(instr.Format(code))
}

func writeSection(sb *strings.Builder, name string) {
	sb.WriteByte('\n')
	sb.WriteString(name)
	sb.WriteByte('\n')
}

func writeIndexed[T fmt.Stringer](sb *strings.Builder, items []T) {
	for i, item := range items {
		writeIndexedBody(sb, i, item.String())
	}
}

func writeConstants(sb *strings.Builder, consts []types.Value) {
	for i, c := range consts {
		writeIndexedBody(sb, i, formatConstant(c))
	}
}

func writeHandlers(sb *strings.Builder, handlers []instr.Handler) {
	for i, h := range handlers {
		body := fmt.Sprintf("start=%04d end=%04d catch=%04d depth=%d", h.Start, h.End, h.Catch, h.Depth)
		writeIndexedBody(sb, i, body)
	}
}

func writeIndexedBody(sb *strings.Builder, index int, body string) {
	head, tail, _ := strings.Cut(body, "\n")
	sb.WriteString(fmt.Sprintf("%04d:\t%s\n", index, head))
	for tail != "" {
		var line string
		line, tail, _ = strings.Cut(tail, "\n")
		if line == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s\n", line))
	}
}

func formatConstant(v types.Value) string {
	switch v := v.(type) {
	case types.I1:
		return fmt.Sprintf("i1 %t", bool(v))
	case types.I8:
		return fmt.Sprintf("i8 %d", int8(v))
	case types.I32:
		return fmt.Sprintf("i32 %d", int32(v))
	case types.I64:
		return fmt.Sprintf("i64 %d", int64(v))
	case types.F32:
		return "f32 " + strconv.FormatFloat(float64(v), 'g', -1, 32)
	case types.F64:
		return "f64 " + strconv.FormatFloat(float64(v), 'g', -1, 64)
	case types.Ref:
		return fmt.Sprintf("ref %d", int32(v))
	case types.String:
		return "string " + strconv.Quote(string(v))
	case types.Boxed:
		return formatBoxedConstant(v)
	default:
		return v.String()
	}
}

func formatBoxedConstant(v types.Boxed) string {
	switch v.Kind() {
	case types.KindI1:
		return fmt.Sprintf("i1 %t", v.Bool())
	case types.KindI8:
		return fmt.Sprintf("i8 %d", v.I8())
	case types.KindI32:
		return fmt.Sprintf("i32 %d", v.I32())
	case types.KindI64:
		return fmt.Sprintf("i64 %d", v.I64())
	case types.KindF32:
		return "f32 " + strconv.FormatFloat(float64(v.F32()), 'g', -1, 32)
	case types.KindF64:
		return "f64 " + strconv.FormatFloat(v.F64(), 'g', -1, 64)
	case types.KindRef:
		return fmt.Sprintf("ref %d", v.Ref())
	default:
		return v.String()
	}
}
