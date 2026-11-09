package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

func TestNewInlinePass(t *testing.T) {
	var _ pass.Pass[*ssa.Function] = transform.NewInlinePass(transform.Module{})

	i32 := func(params ...types.Type) *types.FunctionType {
		return &types.FunctionType{Params: params, Returns: []types.Type{types.TypeI32()}}
	}
	divide := func(b *instr.Builder) {
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
	}
	two := i32(types.TypeI32(), types.TypeI32())
	large := func(b *instr.Builder) {
		for range 20 {
			b.Emit(instr.I32_CONST, 1).Emit(instr.DROP)
		}
		divide(b)
	}

	tests := []struct {
		name    string
		callee  *types.Function
		globals []types.Kind
		inlined bool
	}{
		{
			name:    "a straight-line leaf",
			callee:  &types.Function{Typ: two, Code: assemble(t, divide)},
			inlined: true,
		},
		{
			name: "a leaf with a local and a forward branch",
			callee: &types.Function{Typ: two, Locals: []types.Type{types.TypeI32()}, Code: assemble(t, func(b *instr.Builder) {
				other, done := b.Label(), b.Label()
				b.Emit(instr.LOCAL_GET, 0).BrIf(other)
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_SET, 2).Br(done)
				b.Bind(other).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
				b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.RETURN)
			})},
			inlined: true,
		},
		{
			name:   "a reference parameter",
			callee: &types.Function{Typ: i32(types.TypeI32(), types.TypeI32Array()), Code: assemble(t, divide)},
		},
		{
			name:    "a global write",
			globals: []types.Kind{types.KindI32},
			callee: &types.Function{Typ: two, Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.GLOBAL_SET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
			})},
		},
		{
			name: "a nested call",
			callee: &types.Function{Typ: two, Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
			})},
		},
		{
			name: "a loop",
			callee: &types.Function{Typ: two, Code: assemble(t, func(b *instr.Builder) {
				header := b.Label()
				b.Bind(header).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_LT_S).BrIf(header)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
			})},
		},
		{
			name:   "more instructions than the limit",
			callee: &types.Function{Typ: two, Code: assemble(t, large)},
		},
		{
			name:   "handlers",
			callee: &types.Function{Typ: two, Code: assemble(t, divide), Handlers: []instr.Handler{{}}},
		},
		{
			name:   "captures",
			callee: &types.Function{Typ: two, Code: assemble(t, divide), Captures: []types.Type{types.TypeI32()}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := &types.Function{
				Typ: i32(types.TypeI32()),
				Code: assemble(t, func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 3).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
				}),
			}
			m := transform.Module{
				Constants: []types.Boxed{types.BoxRef(2)},
				Globals:   tt.globals,
				Objects:   transform.Objects{2: {Function: tt.callee}},
			}
			f, _, err := transform.Translate(m, nil, 1, caller, 0)
			require.NoError(t, err)
			require.NoError(t, ssa.Verify(f))

			_, err = transform.NewInlinePass(m).Run(pass.NewManager(), f)
			require.NoError(t, err)
			require.NoError(t, ssa.Verify(f))

			at := calls(caller.Code)[0]
			remains, states := false, map[ssa.Value]int{}
			for b := range f.Len() {
				for _, op := range f.Block(b).Operations {
					if op.Op == ssa.OpState {
						states[op.Results[0]] = op.Frames[0].IP
					}
					if op.Op == ssa.OpExec && op.Code == instr.CALL {
						remains = true
					}
				}
			}
			require.Equal(t, tt.inlined, !remains)
			if !tt.inlined {
				return
			}
			for b := range f.Len() {
				for _, op := range f.Block(b).Operations {
					if op.Op == ssa.OpExec && op.Code != instr.CALL {
						require.Equal(t, at, states[op.State], "%s keeps the call's state", ssa.Format(f))
					}
				}
			}
		})
	}
}
