package ssa

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/siyul-park/minivm/instr"
)

// Format renders f as text: one line per block header, naming its parameters
// and the predecessors that reach it, then one indented line per instruction
// and a last line for the terminator. It is the readable form every later
// phase is tested against.
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
		for _, in := range block.Insts {
			fmt.Fprintf(&sb, "\t%s\n", inst(f, in))
		}
		fmt.Fprintf(&sb, "\t%s\n", term(f, block.Term))
	}
	return sb.String()
}

// inst renders one instruction as "results = operation operands", dropping
// the halves it has none of.
func inst(f *Function, in Instruction) string {
	var sb strings.Builder
	if len(in.Results) > 0 {
		fmt.Fprintf(&sb, "%s = ", defs(f, in.Results))
	}
	switch in.Op {
	case OpPure, OpRead, OpWrite, OpCall:
		sb.WriteString(instr.TypeOf(in.Code).Mnemonic)
	case OpBridge:
		fmt.Fprintf(&sb, "%s %s", in.Op, instr.TypeOf(in.Code).Mnemonic)
	default:
		sb.WriteString(in.Op.String())
	}

	var ops []string
	switch in.Op {
	case OpConst:
		ops = append(ops, in.Const.String())
	case OpLoad, OpStore:
		ops = append(ops, fmt.Sprintf("%s[%d]", in.Slot.Space, in.Slot.Index))
	case OpState:
		for _, frame := range in.Frames {
			ops = append(ops, fmt.Sprintf("{addr=%d base=%d ip=%d returns=%d stack=[%s]}",
				frame.Addr, frame.Base, frame.IP, frame.Returns, strings.Join(refs(frame.Stack), ", ")))
		}
	}
	ops = append(ops, refs(in.Args)...)
	if len(ops) > 0 {
		fmt.Fprintf(&sb, " %s", strings.Join(ops, ", "))
	}
	sb.WriteString(shape(in.Shape))
	if in.State != NoValue {
		fmt.Fprintf(&sb, " state v%d", in.State)
	}
	return sb.String()
}

// term renders one terminator as "operation operands successors", each
// successor carrying the arguments it passes.
func term(f *Function, t Terminator) string {
	var sb strings.Builder
	sb.WriteString(t.Op.String())
	ops := refs(t.Args)
	for _, edge := range t.Edges {
		ops = append(ops, fmt.Sprintf("blk%d(%s)", edge.Block, strings.Join(refs(edge.Args), ", ")))
	}
	if len(ops) > 0 {
		fmt.Fprintf(&sb, " %s", strings.Join(ops, ", "))
	}
	if t.State != NoValue {
		fmt.Fprintf(&sb, " state v%d", t.State)
	}
	return sb.String()
}

// shape renders the speculated container facts an instruction is compiled
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
