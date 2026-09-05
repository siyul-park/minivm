// Package frontend plans native code, in the two forms one compile-time
// snapshot can be planned from. Static reads bytecode alone, resolving element
// kinds, field kinds, and call targets from constants and declared types, and
// bridging the opcodes no backend lowers to the interpreter instead of giving
// the function up. Trace reads what a recording observed, which resolves the
// same facts from what actually ran and specializes the path it took, inlining
// the callees it entered. Both translate one operation the same way, into the
// SSA a backend lowers, and neither reads anything but the snapshot.
//
// Body is the same bytecode translation Static performs, over a whole function
// and against nothing but a Module, for a caller that optimizes bytecode ahead
// of time rather than compiling it: it holds no address to anchor a native
// entry at, no installed code to avoid rebuilding, and no native calling
// convention to honour.
package frontend

import (
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// Module is the read-only, module-wide evidence a bytecode translation
// resolves value kinds, container shapes, and call targets against. It is
// everything a translation reads outside the function being translated, and
// nothing more: a jit.Input carries it alongside the recorded traces, the
// address, the layout, and the installed flag, none of which a translation
// consults.
//
// Objects resolves the reference a constant carries into the facts about the
// cell it names. The identity a reference carries is the caller's to choose -
// the interpreter's own heap address for a JIT compile, the constant's pool
// slot for a compile with no heap - because a translation only ever hands it
// straight back to Objects.
type Module struct {
	// Constants is the module's constant pool, as the values CONST_GET
	// pushes.
	Constants []types.Boxed
	// Globals is the declared kind of each global slot.
	Globals []types.Kind
	Objects jit.Objects
	// Decl is the program's declared-type table, indexed by the type operand
	// of STRUCT_NEW and REF_CAST.
	Decl []types.Type
}

// Static returns the SSA rooted at root: the whole function for a function or
// module entry, and the blocks one loop header reaches for a loop entry. It
// returns (nil, nil) when root cannot be planned from bytecode alone, which is
// not an error - the caller falls back to the trace frontend.
func Static(input *jit.Input, root jit.Anchor) (*ssa.Function, error) {
	if input == nil || input.Function == nil || root.Addr != input.Address {
		return nil, nil
	}
	m := Module{Constants: input.Constants, Globals: input.Globals, Objects: input.Objects, Decl: input.Decl}
	// Module entry does not implement the framed native-call ABI, and a
	// call-free function is also the only one a declared array type may answer
	// for (see facts.elem), so one test settles both.
	if input.Address == 0 && calls(input.Function.Code) {
		return nil, nil
	}
	f, whole, err := translate(m, input.Address, input.Function, root.IP, input.Installed)
	if err != nil || !whole {
		// A native compile is checked against jit.StaticPlan, which refuses a
		// function holding a span nothing reaches, so this one refuses it too
		// rather than compile a block graph the plan does not have.
		return nil, err
	}
	return f, nil
}

// Body returns the SSA for the whole of fn, published at addr. Address zero is
// module code, which ends by advancing past its last instruction rather than
// by returning. It returns (nil, nil) when fn holds an operation no
// translation from bytecode alone can resolve.
func Body(m Module, addr int, fn *types.Function) (*ssa.Function, error) {
	if fn == nil {
		return nil, nil
	}
	f, _, err := translate(m, addr, fn, 0, false)
	return f, err
}

// translate lays out fn's spans, resolves the operand facts every one of them
// is entered with, and emits the blocks the span at ip reaches. It also reports
// whether execution reaches every span there is: one it does not is dead code
// and is simply left out, which is what an optimizer wants of a whole function
// and what a native compile must refuse.
func translate(m Module, addr int, fn *types.Function, ip int, installed bool) (*ssa.Function, bool, error) {
	if len(fn.Code) == 0 {
		return nil, false, nil
	}
	f := facts{
		constants: m.Constants,
		globals:   m.Globals,
		objects:   m.Objects,
		decl:      m.Decl,
		declared:  !calls(fn.Code),
	}
	blocks, err := analysis.Blocks(fn)
	if err != nil {
		return nil, false, err
	}
	spans := split(fn.Code, blocks)
	at, ok := enter(spans, ip, installed)
	if !ok {
		return nil, false, nil
	}
	entry := frame{fn: fn, addr: addr, slots: fn.Declared()}
	states, seen, ok := f.resolve(entry, spans)
	if !ok {
		return nil, false, nil
	}
	return f.build(entry, spans, states, at), !slices.Contains(seen, false), nil
}

// build emits the blocks entry reaches, entry first, and returns the assembled
// function, or nil for the same reason translate returns nothing: a span whose
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

// calls reports whether code enters another function.
func calls(code []byte) bool {
	for ip := 0; ip < len(code); {
		inst := instr.Instruction(code[ip:])
		if inst.Opcode().Writes(instr.Frame) {
			return true
		}
		ip += inst.Width()
	}
	return false
}
