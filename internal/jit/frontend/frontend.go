// Package frontend plans native code, in the two forms one compile-time
// snapshot can be planned from. Static reads bytecode alone, resolving element
// kinds, field kinds, and call targets from constants and declared types, and
// bridging the opcodes no backend lowers to the interpreter instead of giving
// the function up. Trace reads what a recording observed, which resolves the
// same facts from what actually ran and specializes the path it took, inlining
// the callees it entered. Both translate one operation the same way, into the
// SSA a backend lowers, and neither reads anything but the snapshot.
package frontend

import (
	"fmt"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
)

// Static returns the SSA rooted at root: the whole function for a function or
// module entry, and the blocks one loop header reaches for a loop entry. It
// returns (nil, nil) when root cannot be planned from bytecode alone, which is
// not an error - the caller falls back to the trace frontend.
func Static(input *jit.Input, root jit.Anchor) (*ssa.Function, error) {
	if input == nil || input.Function == nil || len(input.Function.Code) == 0 || root.Addr != input.Address {
		return nil, nil
	}
	f := facts{
		constants: input.Constants,
		globals:   input.Globals,
		objects:   input.Objects,
		decl:      input.Decl,
		declared:  !calls(input.Function.Code),
	}
	entry := frame{fn: input.Function, addr: input.Address, slots: input.Function.Declared()}
	// Module entry does not implement the framed native-call ABI, and a
	// call-free function is also the only one a declared array type may answer
	// for (see facts.elem), so one test settles both.
	if input.Address == 0 && !f.declared {
		return nil, nil
	}

	blocks, err := analysis.Blocks(input.Function)
	if err != nil {
		return nil, err
	}
	spans := split(input.Function.Code, blocks)
	at, ok := enter(spans, root, input.Installed)
	if !ok {
		return nil, nil
	}
	states, ok := f.resolve(entry, spans)
	if !ok {
		return nil, nil
	}
	return f.build(entry, spans, states, at), nil
}

// build emits the blocks entry reaches, entry first, and returns the assembled
// function, or nil for the same reason Static returns nothing: a span whose
// operands or successors cannot be represented leaves the function unplanned.
func (f facts) build(entry frame, spans []span, states [][]fact, at int) *ssa.Function {
	order := reach(spans, at)
	ids := make([]int, len(spans))
	for i := range ids {
		ids[i] = -1
	}
	b := ssa.New(fmt.Sprintf("%d:%d", entry.addr, spans[at].start))
	for _, id := range order {
		ids[id] = b.Block()
	}

	for _, id := range order {
		// Every block takes its live operands as parameters, so a merge carries
		// its own dataflow and the entry states what a native entry is handed.
		stack := make([]operand, len(states[id]))
		for i, e := range states[id] {
			t, ok := typ(e.kind)
			if !ok {
				return nil
			}
			stack[i] = operand{value: b.Param(ids[id], t), fact: e}
		}
		w := &walk{facts: f, b: b, block: ids[id], frames: []frame{entry}, stack: stack}
		term, ok := w.run(spans[id])
		if !ok {
			return nil
		}
		if term.Edges, ok = w.edges(spans[id], states, ids); !ok {
			return nil
		}
		b.Term(ids[id], term)
	}
	return b.Build()
}
