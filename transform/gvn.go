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

// GlobalValueNumberingPass removes recomputations of a value already produced on
// every path that reaches them, across basic-block boundaries. It is a strict
// superset of the block-local CSE pass: within-block recomputations are still
// replaced by a load of a live local home or a freshly captured slot, and
// cross-block recomputations (at control-flow merges and inside loops) are
// captured at every definition with LOCAL_TEE and reloaded at each use. The
// top-level body (slot 0) may read its declared Program.Locals but cannot
// allocate fresh ones (the write-back persists only code and handlers), so only
// the load-from-existing-home case applies there.
type GlobalValueNumberingPass struct{}

var _ pass.Pass[*program.Program] = (*GlobalValueNumberingPass)(nil)

func NewGlobalValueNumberingPass() *GlobalValueNumberingPass {
	return &GlobalValueNumberingPass{}
}

func (p *GlobalValueNumberingPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
	for i, fn := range functions(prog) {
		gvn, err := pass.GetResult[*analysis.GlobalValueNumbering](m, fn)
		if err != nil {
			return pass.PreserveNone(), err
		}
		if len(gvn.Redundant) == 0 {
			continue
		}

		code, handlers, ok := p.eliminate(fn, gvn, i > 0)
		if !ok {
			continue
		}
		fn.Code = code
		fn.Handlers = handlers
		if i == 0 {
			prog.Code = code
			prog.Handlers = handlers
		}
	}
	return pass.PreserveNone(), nil
}

// eliminate rewrites fn to drop the redundant expressions. allocate enables
// fresh-slot capture; it is false for the top-level body. A captured value gets
// one fresh local shared by all its uses, with a LOCAL_TEE inserted at every
// definition so the slot holds the value on every path. It reports the new code
// and handlers, or ok=false to leave fn unchanged.
func (p *GlobalValueNumberingPass) eliminate(fn *types.Function, gvn *analysis.GlobalValueNumbering, allocate bool) ([]byte, []instr.Handler, bool) {
	reds := make([]analysis.Redundancy, 0, len(gvn.Redundant))
	for _, r := range gvn.Redundant {
		reds = append(reds, r)
	}
	chosen := p.choose(reds)

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
			defs := gvn.Defs[c.Def]
			t := p.kindType(c.Kind)
			idx := base + len(added)
			if t == nil || idx > math.MaxUint8 || len(defs) == 0 || p.covered(chosen, defs) {
				continue
			}
			added = append(added, t)
			slot = idx
			slots[c.Def] = slot
			for _, d := range defs {
				r.insert(d, instr.New(instr.LOCAL_TEE, uint64(slot)))
			}
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
// first, so the rewriter never receives overlapping replacement edits.
func (p *GlobalValueNumberingPass) choose(reds []analysis.Redundancy) []analysis.Redundancy {
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

// covered reports whether any of a value's definition offsets falls strictly
// inside a chosen replacement range, in which case its capture would land in
// code about to be replaced and the value cannot be captured safely.
func (p *GlobalValueNumberingPass) covered(chosen []analysis.Redundancy, offs []int) bool {
	for _, o := range offs {
		for _, c := range chosen {
			if c.Start < o && o < c.End {
				return true
			}
		}
	}
	return false
}

// kindType maps a value kind to the primitive type used to declare a captured
// local, or nil when the kind has no concrete slot type.
func (p *GlobalValueNumberingPass) kindType(k instr.Kind) types.Type {
	switch k {
	case instr.KindI32:
		return types.TypeI32
	case instr.KindI8:
		return types.TypeI8
	case instr.KindI1:
		return types.TypeI1
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
