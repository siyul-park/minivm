package transform

import (
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// functions returns a program's executable functions: an implicit root over
// prog.Code (carrying the program's top-level exception table) followed by every
// *types.Function constant. The root lets length-changing passes repair the
// top-level handlers' offsets through the rewriter; the caller writes the
// repaired code and handlers back to prog for the i == 0 entry.
func functions(prog *program.Program) []*types.Function {
	fns := []*types.Function{{Typ: &types.FunctionType{}, Code: prog.Code, Handlers: prog.Handlers}}
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, fn)
		}
	}
	return fns
}
