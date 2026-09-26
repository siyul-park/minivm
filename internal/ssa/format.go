package ssa

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/siyul-park/minivm/types"
)

// Format renders a readable SSA dump.
func Format(function *Function) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "func %s\n", function.Name())
	for id := range function.Len() {
		block := function.Block(id)
		fmt.Fprintf(&sb, "blk%d: (%s)", id, definitions(function, block.Params))
		if preds := function.Pred(id); len(preds) > 0 {
			names := make([]string, len(preds))
			for i, pred := range preds {
				names[i] = fmt.Sprintf("blk%d", pred)
			}
			fmt.Fprintf(&sb, " <-- (%s)", strings.Join(names, ", "))
		}
		sb.WriteString("\n")
		for _, o := range block.Operations {
			fmt.Fprintf(&sb, "\t%s\n", op(function, o))
		}
		fmt.Fprintf(&sb, "\t%s\n", term(function, block.Terminator))
	}
	return sb.String()
}

func op(function *Function, o Operation) string {
	var sb strings.Builder
	if len(o.Results) > 0 {
		fmt.Fprintf(&sb, "%s = ", definitions(function, o.Results))
	}
	sb.WriteString(o.name())

	var args []string
	switch o.Op {
	case OpConst:
		args = append(args, literal(function, o))
	case OpLoad, OpStore:
		args = append(args, slot(o.Slot))
	case OpState:
		for _, frame := range o.Frames {
			at := fmt.Sprintf("{addr=%d base=%d ip=%d returns=%d stack=[%s]",
				frame.Address, frame.Base, frame.IP, frame.Returns, strings.Join(stack(frame.Stack), ", "))
			if len(frame.Locals) > 0 {
				at += fmt.Sprintf(" locals=[%s]", strings.Join(locals(frame.Locals), ", "))
			}
			args = append(args, at+"}")
		}
	}
	args = append(args, references(o.Args)...)
	if len(args) > 0 {
		fmt.Fprintf(&sb, " %s", strings.Join(args, ", "))
	}
	sb.WriteString(shape(o))
	if o.State != NoValue {
		fmt.Fprintf(&sb, " state v%d", o.State)
	}
	return sb.String()
}

func term(function *Function, t Terminator) string {
	var sb strings.Builder
	sb.WriteString(t.Op.String())
	args := references(t.Args)
	for _, edge := range t.Edges {
		args = append(args, fmt.Sprintf("blk%d(%s)", edge.Block, strings.Join(references(edge.Args), ", ")))
	}
	if len(args) > 0 {
		fmt.Fprintf(&sb, " %s", strings.Join(args, ", "))
	}
	if t.State != NoValue {
		fmt.Fprintf(&sb, " state v%d", t.State)
	}
	return sb.String()
}

// literal renders an OpConst word by its result type.
func literal(function *Function, o Operation) string {
	switch t := function.Type(o.Results[0]); t {
	case TypeI1:
		if o.Const != 0 {
			return "true"
		}
		return "false"
	case TypeI8, TypeI32:
		return fmt.Sprintf("%d", int32(uint32(o.Const)))
	case TypeI64:
		return fmt.Sprintf("%d", int64(o.Const))
	case TypeF32:
		return fmt.Sprintf("%g", math.Float32frombits(uint32(o.Const)))
	case TypeF64:
		return fmt.Sprintf("%g", math.Float64frombits(o.Const))
	case TypeRef:
		return fmt.Sprintf("%d", types.Boxed(o.Const).Ref())
	default:
		return "<invalid>"
	}
}

func slot(s Slot) string {
	if s.Base != 0 {
		return fmt.Sprintf("%s[%d+%d]", s.Space, s.Base, s.Index)
	}
	return fmt.Sprintf("%s[%d]", s.Space, s.Index)
}

// shape renders an OpGuardShape's Shape; no other operation carries one.
func shape(o Operation) string {
	if o.Op != OpGuardShape {
		return ""
	}
	s := o.Shape
	var sb strings.Builder
	if s.Struct {
		fmt.Fprintf(&sb, " struct type 0x%x", s.Type)
	} else {
		fmt.Fprintf(&sb, " kind %s", s.Kind)
	}
	if s.Host != reflect.Invalid {
		fmt.Fprintf(&sb, " host %s", s.Host)
	}
	return sb.String()
}

func definitions(function *Function, values []Value) string {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = fmt.Sprintf("v%d:%s", v, function.Type(v))
	}
	return strings.Join(names, ", ")
}

func stack(operands []Operand) []string {
	names := make([]string, len(operands))
	for i, o := range operands {
		names[i] = fmt.Sprintf("v%d", o.Value)
		if o.Owned {
			names[i] += " owned"
		}
	}
	return names
}

func locals(locals []Local) []string {
	names := make([]string, len(locals))
	for i, l := range locals {
		names[i] = fmt.Sprintf("%d=v%d", l.Index, l.Value)
	}
	return names
}

func references(values []Value) []string {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = fmt.Sprintf("v%d", v)
	}
	return names
}
