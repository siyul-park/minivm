// Package frontend plans native code from bytecode alone. It turns the
// compile-time snapshot of one function into the SSA a backend lowers,
// resolving element kinds, field kinds, and call targets from constants and
// declared types rather than from a recorded trace, and bridging the opcodes
// no backend lowers to the interpreter instead of giving the function up.
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
		fn:        input.Function,
		addr:      input.Address,
		constants: input.Constants,
		globals:   input.Globals,
		objects:   input.Objects,
		decl:      input.Decl,
		slots:     input.Function.Declared(),
		declared:  !calls(input.Function.Code),
	}
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
	entry, ok := enter(spans, root, input.Installed)
	if !ok {
		return nil, nil
	}
	states, ok := f.resolve(spans)
	if !ok {
		return nil, nil
	}
	return f.build(spans, states, entry), nil
}

// build emits the blocks entry reaches, entry first, and returns the assembled
// function, or nil for the same reason Static returns nothing: a span whose
// operands or successors cannot be represented leaves the function unplanned.
func (f facts) build(spans []span, states [][]fact, entry int) *ssa.Function {
	order := reach(spans, entry)
	ids := make([]int, len(spans))
	for i := range ids {
		ids[i] = -1
	}
	b := ssa.New(fmt.Sprintf("%d:%d", f.addr, spans[entry].start))
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
		w := &walk{facts: f, b: b, block: ids[id], stack: stack}
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
