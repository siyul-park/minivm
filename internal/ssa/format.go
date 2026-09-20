package ssa

import (
	"fmt"
	"reflect"
	"strings"
)

// Format renders a readable SSA dump.
func Format(function *Function) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "func %s\n", function.name)
	for id, block := range function.blocks {
		fmt.Fprintf(&sb, "blk%d: (%s)", id, definitions(function, block.Params))
		if preds := function.preds[id]; len(preds) > 0 {
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
		args = append(args, o.Const.String())
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
	sb.WriteString(shape(o.Shape))
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

func slot(s Slot) string {
	if s.Base != 0 {
		return fmt.Sprintf("%s[%d+%d]", s.Space, s.Base, s.Index)
	}
	return fmt.Sprintf("%s[%d]", s.Space, s.Index)
}

func shape(s Shape) string {
	var sb strings.Builder
	if s.Tag != 0 {
		fmt.Fprintf(&sb, " tag 0x%x", s.Tag)
	}
	if s.Type != 0 {
		fmt.Fprintf(&sb, " type 0x%x", s.Type)
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
