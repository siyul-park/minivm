package transform_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

// boundedLoop is `for i := init; i < limit; i += step { a[index] }` over a
// sliced i32 array a, its header entry state at ip 2.
type boundedLoop struct {
	init, step, limit uint64
	// length makes the limit array.len of a instead of a constant.
	length bool
	// variable makes the step a preheader parameter instead of a constant.
	variable bool
	// offset indexes a at i+1 instead of i.
	offset bool
	// test is the header compare, its operands in order (i, limit) unless
	// swapped.
	test    instr.Opcode
	swapped bool
}

const (
	preheaderBlock = iota
	headerBlock
)

func (l boundedLoop) build() *ssa.Function {
	b := ssa.New("f")
	pre, header, body, exit := b.Block(), b.Block(), b.Block(), b.Block()
	array := b.Param(pre, ssa.TypeRef)
	entry := b.Value(ssa.TypeState)
	b.Add(pre, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 1}}, Results: []ssa.Value{entry}})
	guarded, sliced := b.Value(ssa.TypeRef), b.Value(ssa.TypeRef)
	b.Add(pre, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Kind: types.KindI32}, Args: []ssa.Value{array}, State: entry, Results: []ssa.Value{guarded}})
	b.Add(pre, ssa.Operation{Op: ssa.OpSlice, Args: []ssa.Value{guarded}, Results: []ssa.Value{sliced}})
	init, limit, step := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
	b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: l.init, Results: []ssa.Value{init}})
	if l.length {
		b.Add(pre, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_LEN, Args: []ssa.Value{sliced}, State: entry, Results: []ssa.Value{limit}})
	} else {
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: l.limit, Results: []ssa.Value{limit}})
	}
	if l.variable {
		b.Add(pre, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 1}, Results: []ssa.Value{step}})
	} else {
		b.Add(pre, ssa.Operation{Op: ssa.OpConst, Const: l.step, Results: []ssa.Value{step}})
	}
	b.Term(pre, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{init}}}})

	i := b.Param(header, ssa.TypeI32)
	at := b.Value(ssa.TypeState)
	b.Add(header, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, IP: 2, Stack: []ssa.Operand{{Value: i}, {Value: limit}}}}, Results: []ssa.Value{at}})
	cond := b.Value(ssa.TypeI1)
	args := []ssa.Value{i, limit}
	if l.swapped {
		args = []ssa.Value{limit, i}
	}
	b.Add(header, ssa.Operation{Op: ssa.OpExec, Code: l.test, Args: args, State: at, Results: []ssa.Value{cond}})
	edges := []ssa.Edge{{Block: exit}, {Block: body}}
	if l.test == instr.I32_LT_S || l.test == instr.I32_GT_S {
		edges = []ssa.Edge{{Block: body}, {Block: exit}}
	}
	b.Term(header, ssa.Terminator{Op: ssa.OpBranch, Args: []ssa.Value{cond}, Edges: edges})

	index := i
	if l.offset {
		index = b.Value(ssa.TypeI32)
		b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{i, step}, State: at, Results: []ssa.Value{index}})
	}
	element, next := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
	b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.ARRAY_GET, Args: []ssa.Value{sliced, index}, State: at, Results: []ssa.Value{element}})
	b.Add(body, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{i, step}, State: at, Results: []ssa.Value{next}})
	b.Term(body, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{next}}}})
	b.Term(exit, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{i}})
	return b.Build()
}

func TestNewBoundPass(t *testing.T) {
	var p pass.Pass[*ssa.Function] = transform.NewBoundPass(nil)
	require.NotNil(t, p)
}

func TestBoundPass_Run(t *testing.T) {
	counted := boundedLoop{init: 0, step: 1, limit: 256, test: instr.I32_GE_S}
	minus := uint64(uint32(math.MaxUint32))
	tests := []struct {
		name    string
		loop    boundedLoop
		refuted map[int]bool
		// guards counts the guard.bounds in the preheader, bounds the array
		// accesses indexed by a bound of the header's induction variable.
		guards, bounds int
	}{
		{name: "bounds an index below a constant limit behind a preheader guard", loop: counted, guards: 1, bounds: 1},
		{name: "bounds an index below a stay-on-true compare", loop: boundedLoop{step: 1, limit: 256, test: instr.I32_LT_S}, guards: 1, bounds: 1},
		{name: "bounds an index below a swapped compare", loop: boundedLoop{step: 1, limit: 256, test: instr.I32_LE_S, swapped: true}, guards: 1, bounds: 1},
		{name: "bounds an index stepping by a constant that cannot wrap past the limit", loop: boundedLoop{init: 2, step: 3, limit: 256, test: instr.I32_GE_S}, guards: 1, bounds: 1},
		{name: "guards an index below the array's own length", loop: boundedLoop{step: 1, length: true, test: instr.I32_GE_S}, guards: 1, bounds: 1},
		{name: "leaves an index starting negative", loop: boundedLoop{init: minus, step: 1, limit: 256, test: instr.I32_GE_S}},
		{name: "leaves an index stepping by a variable", loop: boundedLoop{step: 1, limit: 256, variable: true, test: instr.I32_GE_S}},
		{name: "leaves an index whose step could wrap past a constant limit", loop: boundedLoop{step: 2, limit: math.MaxInt32, test: instr.I32_GE_S}},
		{name: "leaves an index stepping by more than one below the array's length", loop: boundedLoop{step: 2, length: true, test: instr.I32_GE_S}},
		{name: "leaves an index that is not the induction variable", loop: boundedLoop{step: 1, limit: 256, offset: true, test: instr.I32_GE_S}},
		{name: "leaves an unsigned compare", loop: boundedLoop{step: 1, limit: 256, test: instr.I32_GE_U}},
		{name: "leaves a loop whose header entry a guard refuted", loop: counted, refuted: map[int]bool{2: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := tt.loop.build()
			require.NoError(t, ssa.Verify(fn))

			_, err := transform.NewBoundPass(tt.refuted).Run(pass.NewManager(), fn)

			require.NoError(t, err)
			require.NoError(t, ssa.Verify(fn))
			out := ssa.Format(fn)
			induction := fn.Block(headerBlock).Params[0]
			require.Equal(t, tt.guards, strings.Count(out, "guard.bounds"), out)
			require.Equal(t, tt.guards, strings.Count(blockChunk(out, preheaderBlock), "guard.bounds"), out)
			require.Equal(t, tt.bounds, strings.Count(out, "bound v"), out)
			require.Equal(t, tt.bounds, strings.Count(out, fmt.Sprintf("bound v%d,", induction)), out)
		})
	}
}
