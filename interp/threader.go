package interp

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// local decodes a LOCAL_GET operand at code offset at and confirms the
// local's static kind matches kind, folding threaded.go's generated
// fusion table decode+bounds+kind boilerplate into one call. It runs only
// during Compile, never per instruction execution: the returned index is
// baked into the fully specialized runtime closure the generated code
// builds around the call, so this adds no runtime indirection.
func (c *threader) local(at int, kind types.Kind) (int, bool) {
	idx := int(c.code[at])
	if idx >= len(c.locals) || c.locals[idx].Repr() != kind {
		return 0, false
	}
	return idx, true
}

// global decodes a GLOBAL_GET operand at code offset at and confirms the
// global's static kind matches kind. See local for when this runs.
func (c *threader) global(at int, kind types.Kind) (int, bool) {
	idx := instr.ParseU16(c.code, at)
	if idx >= len(c.globals) || c.globals[idx].Repr() != kind {
		return 0, false
	}
	return idx, true
}

// upval decodes an UPVAL_GET operand at code offset at and confirms the
// capture's static kind matches kind. See local for when this runs.
func (c *threader) upval(at int, kind types.Kind) (int, bool) {
	idx := int(c.code[at])
	if idx >= len(c.captures) || c.captures[idx].Repr() != kind {
		return 0, false
	}
	return idx, true
}

// constant decodes a CONST_GET operand at code offset at and confirms the
// constant's kind matches kind. A types.KindI64 request also accepts a
// Ref constant that spills to a heap-resident types.I64, mirroring the
// interpreter's I64 heap-spill representation for over-wide immediates.
// See local for when this runs.
func (c *threader) constant(at int, kind types.Kind) (types.Boxed, bool) {
	idx := instr.ParseU16(c.code, at)
	if idx >= len(c.constants) {
		return types.Boxed(0), false
	}
	boxed := c.constants[idx]
	if kind == types.KindI64 {
		switch boxed.Kind() {
		case types.KindI64:
		case types.KindRef:
			ref := boxed.Ref()
			if ref < 0 || ref >= len(c.heap) {
				return types.Boxed(0), false
			}
			if _, ok := c.heap[ref].(types.I64); !ok {
				return types.Boxed(0), false
			}
		default:
			return types.Boxed(0), false
		}
	} else if boxed.Kind() != kind {
		return types.Boxed(0), false
	}
	return boxed, true
}
