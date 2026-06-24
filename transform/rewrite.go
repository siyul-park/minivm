package transform

import (
	"math"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// rewriter applies length-changing edits to a function body and repairs the
// branch and handler offsets that the shift invalidates. Unlike the offset-
// preserving peephole passes, it lets bytecode grow or shrink, then rewrites
// every BR/BR_IF/BR_TABLE operand and exception-table boundary for the new
// layout. Edits must be instruction-aligned and must not cover a branch.
type rewriter struct {
	code     []byte
	handlers []instr.Handler
	edits    []edit
}

// edit replaces code[start:end) with bytes; an insertion is start == end.
type edit struct {
	start int
	end   int
	bytes []byte
}

func newRewriter(fn *types.Function) *rewriter {
	return &rewriter{code: fn.Code, handlers: fn.Handlers}
}

// replace schedules code[start:end) to be overwritten by instrs.
func (r *rewriter) replace(start, end int, instrs ...instr.Instruction) {
	r.edits = append(r.edits, edit{start: start, end: end, bytes: instr.Marshal(instrs)})
}

// insert schedules instrs to be spliced in at offset at.
func (r *rewriter) insert(at int, instrs ...instr.Instruction) {
	r.edits = append(r.edits, edit{start: at, end: at, bytes: instr.Marshal(instrs)})
}

// run materializes the edited code and repaired handlers. It returns ok=false,
// leaving the function untouched, when a branch can no longer reach its target
// within the signed 16-bit operand range.
func (r *rewriter) run() ([]byte, []instr.Handler, bool) {
	if len(r.edits) == 0 {
		return r.code, r.handlers, true
	}

	code, remap := r.build()
	if !r.relink(code, remap) {
		return nil, nil, false
	}
	return code, r.rehandle(remap), true
}

// build emits the edited code and a map from each old byte offset to its new
// offset. Offsets inside a replaced range collapse onto the replacement's start.
func (r *rewriter) build() ([]byte, []int) {
	slices.SortFunc(r.edits, func(a, b edit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return a.end - b.end
	})

	remap := make([]int, len(r.code)+1)
	code := make([]byte, 0, len(r.code))

	old := 0
	for _, e := range r.edits {
		for ; old < e.start; old++ {
			remap[old] = len(code)
			code = append(code, r.code[old])
		}
		for o := e.start; o < e.end; o++ {
			remap[o] = len(code)
		}
		code = append(code, e.bytes...)
		old = e.end
	}
	for ; old < len(r.code); old++ {
		remap[old] = len(code)
		code = append(code, r.code[old])
	}
	remap[len(r.code)] = len(code)
	return code, remap
}

// relink rewrites every branch operand in the original code to target the same
// instruction at its new offset. It reports whether all offsets stayed in range.
func (r *rewriter) relink(code []byte, remap []int) bool {
	for ip := 0; ip < len(r.code); {
		inst := instr.Instruction(r.code[ip:])
		width := inst.Width()
		switch inst.Opcode() {
		case instr.BR, instr.BR_IF:
			if !r.retarget(code, remap, ip, width, inst, 0) {
				return false
			}
		case instr.BR_TABLE:
			count := int(inst.Operand(0))
			for j := 0; j <= count; j++ {
				if !r.retarget(code, remap, ip, width, inst, j+1) {
					return false
				}
			}
		}
		ip += width
	}
	return true
}

// retarget recomputes one relative branch operand for the new layout, writing it
// into the relocated copy of inst. It reports whether the delta fits int16.
func (r *rewriter) retarget(code []byte, remap []int, ip, width int, inst instr.Instruction, operand int) bool {
	target := ip + width + instr.ReadI16(inst.Operand(operand))
	if target < 0 || target >= len(remap) {
		return false
	}
	delta := remap[target] - remap[ip] - width
	if delta < math.MinInt16 || delta > math.MaxInt16 {
		return false
	}
	instr.Instruction(code[remap[ip]:]).SetOperand(operand, uint64(delta))
	return true
}

func (r *rewriter) rehandle(remap []int) []instr.Handler {
	if len(r.handlers) == 0 {
		return r.handlers
	}
	handlers := make([]instr.Handler, len(r.handlers))
	for i, h := range r.handlers {
		handlers[i] = instr.Handler{
			Start: remap[h.Start],
			End:   remap[h.End],
			Catch: remap[h.Catch],
			Depth: h.Depth,
		}
	}
	return handlers
}
