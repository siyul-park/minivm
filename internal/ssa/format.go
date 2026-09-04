package ssa

import (
	"fmt"
	"reflect"
	"strings"
)

// Format renders f as text: one line per block header, naming its parameters
// and the predecessors that reach it, then one indented line per operation and
// a last line for the terminator. It is the readable form every later phase is
// tested against.
func Format(f *Function) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "func %s\n", f.name)
	for id, block := range f.blocks {
		fmt.Fprintf(&sb, "blk%d: (%s)", id, defs(f, block.Params))
		if preds := f.preds[id]; len(preds) > 0 {
			names := make([]string, len(preds))
			for i, pred := range preds {
				names[i] = fmt.Sprintf("blk%d", pred)
			}
			fmt.Fprintf(&sb, " <-- (%s)", strings.Join(names, ", "))
		}
		sb.WriteString("\n")
		for _, o := range block.Ops {
			fmt.Fprintf(&sb, "\t%s\n", op(f, o))
		}
		fmt.Fprintf(&sb, "\t%s\n", term(f, block.Term))
	}
	return sb.String()
}

// op renders one operation as "results = name operands", dropping the halves
// it has none of.
func op(f *Function, o Operation) string {
	var sb strings.Builder
	if len(o.Results) > 0 {
		fmt.Fprintf(&sb, "%s = ", defs(f, o.Results))
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
			args = append(args, fmt.Sprintf("{addr=%d base=%d ip=%d returns=%d stack=[%s]}",
				frame.Addr, frame.Base, frame.IP, frame.Returns, strings.Join(refs(frame.Stack), ", ")))
		}
	}
	args = append(args, refs(o.Args)...)
	if len(args) > 0 {
		fmt.Fprintf(&sb, " %s", strings.Join(args, ", "))
	}
	sb.WriteString(shape(o.Shape))
	if o.State != NoValue {
		fmt.Fprintf(&sb, " state v%d", o.State)
	}
	return sb.String()
}

// term renders one terminator as "operation operands successors", each
// successor carrying the arguments it passes.
func term(f *Function, t Terminator) string {
	var sb strings.Builder
	sb.WriteString(t.Op.String())
	args := refs(t.Args)
	for _, edge := range t.Edges {
		args = append(args, fmt.Sprintf("blk%d(%s)", edge.Block, strings.Join(refs(edge.Args), ", ")))
	}
	if len(args) > 0 {
		fmt.Fprintf(&sb, " %s", strings.Join(args, ", "))
	}
	if t.State != NoValue {
		fmt.Fprintf(&sb, " state v%d", t.State)
	}
	return sb.String()
}

// slot renders the storage an operation reads or writes, naming the frame
// floor a local counts from only when it is not the entry frame's.
func slot(s Slot) string {
	if s.Base != 0 {
		return fmt.Sprintf("%s[%d+%d]", s.Space, s.Base, s.Index)
	}
	return fmt.Sprintf("%s[%d]", s.Space, s.Index)
}

// shape renders the speculated container facts an operation is compiled
// against, omitting every fact it does not carry.
func shape(s Shape) string {
	var sb strings.Builder
	if s.Itab != 0 {
		fmt.Fprintf(&sb, " itab 0x%x", s.Itab)
	}
	if s.Typ != 0 {
		fmt.Fprintf(&sb, " type 0x%x", s.Typ)
	}
	if s.Host != reflect.Invalid {
		fmt.Fprintf(&sb, " host %s", s.Host)
	}
	return sb.String()
}

// defs renders the values being defined, each with its type.
func defs(f *Function, vs []Value) string {
	names := make([]string, len(vs))
	for i, v := range vs {
		names[i] = fmt.Sprintf("v%d:%s", v, f.Type(v))
	}
	return strings.Join(names, ", ")
}

// refs names the values being read.
func refs(vs []Value) []string {
	names := make([]string, len(vs))
	for i, v := range vs {
		names[i] = fmt.Sprintf("v%d", v)
	}
	return names
}
