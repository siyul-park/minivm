package transform

import (
	"math"
	"slices"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// CommonSubexpressionEliminationPass removes recomputations of a value already
// produced in the same basic block. Each redundant expression is replaced by a
// load: of a local that still holds the value, or — for real functions, whose
// locals persist — of a fresh slot captured at the first computation with
// LOCAL_TEE. The top-level body (slot 0) compiles with no locals, so only the
// load-from-existing-home case applies there.
type CommonSubexpressionEliminationPass struct{}

var _ pass.Pass[*program.Program] = (*CommonSubexpressionEliminationPass)(nil)

func NewCommonSubexpressionEliminationPass() *CommonSubexpressionEliminationPass {
	return &CommonSubexpressionEliminationPass{}
}

func (p *CommonSubexpressionEliminationPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
	for i, fn := range functions(prog) {
		vn, err := pass.GetResult[*analysis.ValueNumbering](m, fn)
		if err != nil {
			return pass.PreserveNone(), err
		}
		if len(vn.Redundant) == 0 {
			continue
		}

		code, handlers, ok := p.eliminate(fn, vn, i > 0)
		if !ok {
			continue
		}
		fn.Code = code
		fn.Handlers = handlers
		if i == 0 {
			prog.Code = code
		}
	}
	return pass.PreserveNone(), nil
}

// eliminate rewrites fn to drop the redundant expressions. allocate enables
// fresh-slot capture; it is false for the top-level body. It reports the new
// code and handlers, or ok=false to leave fn unchanged.
func (p *CommonSubexpressionEliminationPass) eliminate(fn *types.Function, vn *analysis.ValueNumbering, allocate bool) ([]byte, []instr.Handler, bool) {
	chosen := p.choose(vn)

	base := len(fn.Locals)
	if fn.Typ != nil {
		base += len(fn.Typ.Params)
	}

	r := newRewriter(fn)
	slots := map[int]int{}
	var added []types.Type
	applied := false
	for _, c := range chosen {
		if c.Home >= 0 {
			r.replace(c.Start, c.End, instr.New(instr.LOCAL_GET, uint64(c.Home)))
			applied = true
			continue
		}
		if !allocate {
			continue
		}

		slot, ok := slots[c.Def]
		if !ok {
			t := kindType(c.Kind)
			idx := base + len(added)
			if t == nil || idx > math.MaxUint8 || p.covers(chosen, c.Def) {
				continue
			}
			added = append(added, t)
			slot = idx
			slots[c.Def] = slot
			r.insert(c.Def, instr.New(instr.LOCAL_TEE, uint64(slot)))
		}
		r.replace(c.Start, c.End, instr.New(instr.LOCAL_GET, uint64(slot)))
		applied = true
	}
	if !applied {
		return nil, nil, false
	}

	code, handlers, ok := r.run()
	if !ok {
		return nil, nil, false
	}
	for k := range handlers {
		handlers[k].Depth += len(added)
	}
	fn.Locals = append(fn.Locals, added...)
	return code, handlers, true
}

// choose selects a non-overlapping subset of the redundant ranges, lowest offset
// first, so the rewriter never receives overlapping edits.
func (p *CommonSubexpressionEliminationPass) choose(vn *analysis.ValueNumbering) []analysis.Redundancy {
	reds := make([]analysis.Redundancy, 0, len(vn.Redundant))
	for _, r := range vn.Redundant {
		reds = append(reds, r)
	}
	slices.SortFunc(reds, func(a, b analysis.Redundancy) int { return a.Start - b.Start })

	chosen := reds[:0]
	end := -1
	for _, r := range reds {
		if r.Start < end {
			continue
		}
		chosen = append(chosen, r)
		end = r.End
	}
	return chosen
}

// covers reports whether off falls strictly inside one of the chosen ranges, in
// which case a capture inserted there would land in code about to be replaced.
func (p *CommonSubexpressionEliminationPass) covers(chosen []analysis.Redundancy, off int) bool {
	for _, c := range chosen {
		if c.Start < off && off < c.End {
			return true
		}
	}
	return false
}

// kindType maps a value kind to the primitive type used to declare a captured
// local, or nil when the kind has no concrete slot type.
func kindType(k instr.Kind) types.Type {
	switch k {
	case instr.KindI32:
		return types.TypeI32
	case instr.KindI64:
		return types.TypeI64
	case instr.KindF32:
		return types.TypeF32
	case instr.KindF64:
		return types.TypeF64
	case instr.KindRef:
		return types.TypeRef
	default:
		return nil
	}
}
