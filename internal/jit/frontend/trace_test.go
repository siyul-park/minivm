package frontend_test

import (
	"sort"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/frontend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestTrace(t *testing.T) {
	for _, tc := range recordings(t) {
		t.Run(tc.name, func(t *testing.T) {
			plans, err := jit.TracePlan(tc.input)
			require.NoError(t, err)
			roots := map[jit.Anchor]jit.Plan{}
			for _, plan := range plans {
				roots[plan.Anchor] = plan
			}
			for _, anchor := range tc.anchors {
				fn := frontend.Trace(tc.input, anchor)
				plan, planned := roots[anchor]
				require.Equal(t, planned, fn != nil)
				if fn == nil {
					continue
				}
				require.NoError(t, ssa.Verify(fn))
				adopts(t, fn)
				targets(t, fn)
				if tc.diverges {
					continue
				}
				_, blocks := breadth(0, fn.Len(), fn.Succ)
				require.Equal(t, reached(plan), blocks)
			}
		})
	}

	t.Run("emits an inlined frame's own slots and deopt frame", func(t *testing.T) {
		callee := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
			}),
		}
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				head := b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
				b.Bind(head)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.LOCAL_SET, 0)
				b.Br(head)
			}),
		}
		at, in := offsets(fn.Code), offsets(callee.Code)
		rec := &tape{}
		rec.at(fn, 1, at[2], 0)
		rec.at(fn, 1, at[3], 0)
		call := rec.at(fn, 1, at[4], 0)
		call.Callee, call.Seen = 2, types.BoxRef(2)
		for _, ip := range in {
			rec.at(callee, 2, ip, 1)
		}
		rec.at(fn, 1, at[5], 0)
		rec.at(fn, 1, at[6], 0).Target = at[2]

		root := jit.Anchor{Addr: 1, IP: at[2]}
		out := frontend.Trace(&jit.Input{
			Traces:    fakeTraces{trees: map[jit.Anchor]*jit.Tree{root: {Root: &jit.Trace{Anchor: root, Ops: rec.ops, Status: jit.StatusLoop}}}},
			Address:   1,
			Function:  fn,
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   jit.Objects{1: {Fn: fn}, 2: {Fn: callee}},
		}, root)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:7
blk0: () <-- (blk0)
	v1:i32 = load local[0]
	v2:ref = const 2
	v3:state = state {addr=1 base=0 ip=12 returns=1 stack=[v1, v2]}
	store local[1+0], v1 state v3
	v4:i32 = const 0
	store local[1+1], v4 state v3
	v5:i32 = load local[1+0]
	v6:i32 = const 1
	v7:i32 = i32.add v5, v6
	v8:state = state {addr=1 base=0 ip=13 returns=1 stack=[]}, {addr=2 base=1 ip=8 returns=1 stack=[v7]}
	store local[1+1], v7 state v8
	v9:i32 = load local[1+1]
	v10:state = state {addr=1 base=0 ip=13 returns=1 stack=[v9]}
	store local[0], v9 state v10
	jump blk0()
`, ssa.Format(out))
	})

	t.Run("a continuation inside an inlined frame is an edge, not a tail", func(t *testing.T) {
		// The plan reaches the caller's continuation only through Edge.Tail,
		// which its block graph cannot name, so the two shapes differ here by
		// exactly that block and the edge into it.
		tc := inlinedBranch(t)
		plans, err := jit.TracePlan(tc.input)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Equal(t, [][]int{{1, 2}, nil, nil}, reached(plans[0]))

		fn := frontend.Trace(tc.input, tc.anchors[0])
		require.NoError(t, ssa.Verify(fn))
		_, blocks := breadth(0, fn.Len(), fn.Succ)
		require.Equal(t, [][]int{{1, 2}, {3}, {3}, nil}, blocks)
	})
}

// recording is one recorded tree together with the anchors a plan could be
// asked for and the name its case runs under.
type recording struct {
	name     string
	input    *jit.Input
	anchors  []jit.Anchor
	diverges bool
}

// recordings is the set of trees the differential runs over: every status a
// recording ends on, both loop foldings, a leg the tree excludes, and the
// inlining a recording is the only frontend that does.
func recordings(t *testing.T) []recording {
	t.Helper()

	var out []recording
	add := func(name string, r recording) {
		r.name = name
		out = append(out, r)
	}

	add("module completes", module(t))
	add("function returns", returns(t))
	add("hot leg folds onto the branch", legged(t, jit.StatusReturned, false))
	add("aborted leg is not folded", legged(t, jit.StatusAborted, false))
	add("loop-kind leg is not folded", legged(t, jit.StatusLoop, false))
	add("carried loop root is refused", legged(t, jit.StatusReturned, true))
	for _, tc := range cuts(t) {
		add(tc.name, tc)
	}
	add("entry falls back", ending(t, jit.StatusFallback, 0))
	add("loop root falls back", ending(t, jit.StatusFallback, 1))
	add("entry is aborted", ending(t, jit.StatusAborted, 0))
	add("entry is partial", ending(t, jit.StatusPartial, 0))
	add("self tail call closes the recording", tailed(t, true))
	add("tail call morphs into another function", tailed(t, false))
	add("call is inlined", inlined(t, 1))
	add("nested call is inlined", inlined(t, 2))
	add("observed callee is guarded and entered", observed(t, true))
	add("observed callee is guarded and called", observed(t, false))
	add("legs fold hottest first", ordered(t))

	branched := inlinedBranch(t)
	branched.diverges = true
	add("branch inside an inlined frame", branched)
	return out
}

// module records a top-level body running off its end.
func module(t *testing.T) recording {
	t.Helper()
	fn := &types.Function{Code: assemble(t, func(b *instr.Builder) {
		b.Emit(instr.I32_CONST, 1).Emit(instr.DROP)
	})}
	at := offsets(fn.Code)
	rec := &tape{}
	rec.at(fn, 0, at[0], 0)
	rec.at(fn, 0, at[1], 0)
	return tree(fn, 0, &jit.Trace{Anchor: jit.Anchor{}, Ops: rec.ops, Status: jit.StatusCompleted}, nil)
}

// returns records a function reaching its own RETURN.
func returns(t *testing.T) recording {
	t.Helper()
	fn := &types.Function{
		Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 7).Emit(instr.RETURN) }),
	}
	at := offsets(fn.Code)
	rec := &tape{}
	rec.at(fn, 1, at[0], 0)
	rec.at(fn, 1, at[1], 0)
	return tree(fn, 1, &jit.Trace{Anchor: jit.Anchor{Addr: 1}, Ops: rec.ops, Status: jit.StatusReturned}, nil)
}

// counter is the loop every leg fixture is recorded from: a header that leaves
// through one conditional branch and closes on a backward one.
func counter(t *testing.T) *types.Function {
	t.Helper()
	return &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32},
		Code: assemble(t, func(b *instr.Builder) {
			head, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Bind(head)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(head)
			b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		}),
	}
}

// legged records one turn of counter's loop, with a leg of the given status
// waiting at the exit its branch did not take.
func legged(t *testing.T, status jit.Status, carried bool) recording {
	t.Helper()
	fn := counter(t)
	at := offsets(fn.Code)
	head, done := at[2], at[11]

	rec := &tape{}
	for _, ip := range at[2:5] {
		rec.at(fn, 1, ip, 0)
	}
	rec.at(fn, 1, at[5], 0).Target = done
	for _, ip := range at[6:10] {
		rec.at(fn, 1, ip, 0)
	}
	rec.at(fn, 1, at[10], 0).Target = head
	root := &jit.Trace{Anchor: jit.Anchor{Addr: 1, IP: head}, Ops: rec.ops, Status: jit.StatusLoop, Carried: carried}

	tail := &tape{}
	tail.at(fn, 1, at[11], 0)
	tail.at(fn, 1, at[12], 0)
	leg := &jit.Trace{Anchor: jit.Anchor{Addr: 1, IP: done}, Ops: tail.ops, Status: status}
	return tree(fn, 1, root, map[int]*jit.Trace{0: leg})
}

// looper is the loop the cut fixtures are recorded from: a header with two
// bodies, so a bounded recording of the second one can be cut at the header it
// was about to rejoin.
func looper(t *testing.T) *types.Function {
	t.Helper()
	return &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32},
		Code: assemble(t, func(b *instr.Builder) {
			head, alt := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Bind(head)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(alt)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(head)
			b.Bind(alt).Emit(instr.LOCAL_GET, 0).Emit(instr.DROP)
			b.Br(head)
		}),
	}
}

// cuts records the three ways a bounded recording's own cut resolves: onto the
// header it rejoins, behind the branch that already named it, and anywhere
// else, which leaves native code.
func cuts(t *testing.T) []recording {
	t.Helper()
	var out []recording
	for _, tc := range []struct {
		name   string
		branch bool
		header bool
	}{
		{name: "cut on the header folds into the back-edge", header: true},
		{name: "cut behind a back-edge branch leaves no block", branch: true, header: true},
		{name: "cut elsewhere leaves native code"},
	} {
		fn := looper(t)
		at := offsets(fn.Code)
		head, alt := at[2], at[11]

		rec := &tape{}
		for _, ip := range at[2:5] {
			rec.at(fn, 1, ip, 0)
		}
		rec.at(fn, 1, at[5], 0).Target = alt
		for _, ip := range at[6:10] {
			rec.at(fn, 1, ip, 0)
		}
		rec.at(fn, 1, at[10], 0).Target = head
		root := &jit.Trace{Anchor: jit.Anchor{Addr: 1, IP: head}, Ops: rec.ops, Status: jit.StatusLoop}

		tail := &tape{}
		tail.at(fn, 1, at[11], 0)
		tail.at(fn, 1, at[12], 0)
		target := at[0]
		if tc.header {
			target = head
		}
		if tc.branch {
			tail.at(fn, 1, at[13], 0).Target = target
		}
		tail.ops = append(tail.ops, jit.Record{Step: jit.Step{Fn: 1}, Target: target, Cut: true})
		leg := &jit.Trace{Anchor: jit.Anchor{Addr: 1, IP: alt}, Ops: tail.ops, Status: jit.StatusPartial}
		r := tree(fn, 1, root, map[int]*jit.Trace{0: leg})
		r.name = tc.name
		out = append(out, r)
	}
	return out
}

// ending records a straight-line body ending on one status, anchored either at
// the function entry or at a loop header.
func ending(t *testing.T, status jit.Status, ip int) recording {
	t.Helper()
	fn := counter(t)
	at := offsets(fn.Code)
	anchor := jit.Anchor{Addr: 1}
	if ip != 0 {
		anchor.IP = at[2]
	}
	rec := &tape{}
	rec.at(fn, 1, at[2], 0)
	rec.at(fn, 1, at[3], 0)
	ops := rec.ops
	if status == jit.StatusPartial {
		ops = append(ops, jit.Record{Step: jit.Step{Fn: 1}, Target: at[4], Cut: true})
	}
	return tree(fn, 1, &jit.Trace{Anchor: anchor, Ops: ops, Status: status}, nil)
}

// inlined records a call the recorder stepped into, depth levels deep.
func inlined(t *testing.T, depth int) recording {
	t.Helper()
	body := func(next int) *types.Function {
		return &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				if next > 0 {
					b.Emit(instr.CONST_GET, uint64(next)).Emit(instr.CALL).Emit(instr.RETURN)
					return
				}
				b.Emit(instr.I32_CONST, 5).Emit(instr.RETURN)
			}),
		}
	}
	objects := jit.Objects{}
	constants := make([]types.Boxed, depth)
	for i := 0; i < depth; i++ {
		objects[2+i] = jit.Object{Fn: body(depth - 1 - i)}
		constants[i] = types.BoxRef(2 + i)
	}
	caller := body(depth)
	objects[1] = jit.Object{Fn: caller}
	constants = append(constants, types.BoxRef(2))
	caller.Code = assemble(t, func(b *instr.Builder) {
		b.Emit(instr.CONST_GET, uint64(depth)).Emit(instr.CALL).Emit(instr.RETURN)
	})

	rec := &tape{}
	frames := []*types.Function{caller}
	addrs := []int{1}
	for level := 0; level < depth; level++ {
		fn, addr := frames[level], addrs[level]
		at := offsets(fn.Code)
		rec.at(fn, addr, at[0], level)
		call := rec.at(fn, addr, at[1], level)
		call.Callee = 2 + level
		call.Seen = types.BoxRef(2 + level)
		frames = append(frames, objects[2+level].Fn)
		addrs = append(addrs, 2+level)
	}
	deepest, addr := frames[depth], addrs[depth]
	at := offsets(deepest.Code)
	rec.at(deepest, addr, at[0], depth)
	rec.at(deepest, addr, at[1], depth)
	for level := depth - 1; level >= 0; level-- {
		fn := frames[level]
		rec.at(fn, addrs[level], offsets(fn.Code)[2], level)
	}

	input := &jit.Input{
		Traces:    fakeTraces{trees: map[jit.Anchor]*jit.Tree{{Addr: 1}: {Root: &jit.Trace{Anchor: jit.Anchor{Addr: 1}, Ops: rec.ops, Status: jit.StatusReturned}}}},
		Address:   1,
		Function:  caller,
		Constants: constants,
		Objects:   objects,
	}
	return recording{input: input, anchors: []jit.Anchor{{Addr: 1}}}
}

// observed records a call whose callee is no constant: the caller loads it from
// a global, so the reference it enters is only what the recording saw there and
// a guard is what admits it. A recording that steps into the callee inlines it;
// one that steps over the call - as it does for the recursion here - leaves the
// call itself, which is where a lowering reads the callee back off the guard.
func observed(t *testing.T, enters bool) recording {
	t.Helper()
	body := func(b *instr.Builder) {
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
	}
	caller := &types.Function{
		Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, body),
	}
	objects := jit.Objects{1: {Fn: caller}}
	target, addr := caller, 1
	if enters {
		target, addr = &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 5).Emit(instr.RETURN) }),
		}, 2
		objects[2] = jit.Object{Fn: target}
	}
	outer := offsets(caller.Code)

	rec := &tape{}
	rec.at(caller, 1, outer[0], 0)
	rec.at(caller, 1, outer[1], 0).Seen = types.BoxRef(addr)
	if enters {
		for _, ip := range offsets(target.Code) {
			rec.at(target, addr, ip, 1)
		}
	}
	rec.at(caller, 1, outer[2], 0)

	root := jit.Anchor{Addr: 1}
	return recording{
		input: &jit.Input{
			Traces: fakeTraces{trees: map[jit.Anchor]*jit.Tree{
				root: {Root: &jit.Trace{Anchor: root, Ops: rec.ops, Status: jit.StatusReturned}},
			}},
			Address:  1,
			Function: caller,
			Globals:  []types.Kind{types.KindRef},
			Objects:  objects,
		},
		anchors: []jit.Anchor{root},
	}
}

// inlinedBranch records a conditional branch inside an inlined callee, with a
// continuation waiting at the target it did not take.
func inlinedBranch(t *testing.T) recording {
	t.Helper()
	callee := &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			other := b.Label()
			b.Emit(instr.I32_CONST, 0).BrIf(other)
			b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
			b.Bind(other).Emit(instr.I32_CONST, 2).Emit(instr.RETURN)
		}),
	}
	caller := &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		}),
	}
	outer, inner := offsets(caller.Code), offsets(callee.Code)

	rec := &tape{}
	rec.at(caller, 1, outer[0], 0)
	call := rec.at(caller, 1, outer[1], 0)
	call.Callee, call.Seen = 2, types.BoxRef(2)
	rec.at(callee, 2, inner[0], 1)
	rec.at(callee, 2, inner[1], 1).Target = inner[4]
	rec.at(callee, 2, inner[2], 1)
	rec.at(callee, 2, inner[3], 1)
	rec.at(caller, 1, outer[2], 0)

	leg := &tape{}
	leg.at(callee, 2, inner[4], 0)
	leg.at(callee, 2, inner[5], 0)

	input := &jit.Input{
		Traces: fakeTraces{trees: map[jit.Anchor]*jit.Tree{{Addr: 1}: {
			Root:     &jit.Trace{Anchor: jit.Anchor{Addr: 1}, Ops: rec.ops, Status: jit.StatusReturned},
			Branches: map[int]*jit.Trace{0: {Anchor: jit.Anchor{Addr: 2, IP: inner[4]}, Ops: leg.ops, Status: jit.StatusReturned}},
			Hits:     []int64{9},
		}}},
		Address:   1,
		Function:  caller,
		Constants: []types.Boxed{types.BoxRef(2)},
		Objects:   jit.Objects{1: {Fn: caller}, 2: {Fn: callee}},
	}
	return recording{input: input, anchors: []jit.Anchor{{Addr: 1}}}
}

// ordered records two legs at the two exits of one branch table, so the order
// they fold in is the order their hit counts rank them.
func ordered(t *testing.T) recording {
	t.Helper()
	fn := &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32},
		Code: assemble(t, func(b *instr.Builder) {
			one, two, done := b.Label(), b.Label(), b.Label()
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 0).BrTable(one, two)
			b.Bind(one).Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
			b.Bind(two).Emit(instr.I32_CONST, 2).Emit(instr.RETURN)
		}),
	}
	at := offsets(fn.Code)
	rec := &tape{}
	rec.at(fn, 1, at[0], 0)
	rec.at(fn, 1, at[1], 0).Target = at[2]
	root := &jit.Trace{Anchor: jit.Anchor{Addr: 1}, Ops: rec.ops, Status: jit.StatusReturned}

	cold := &tape{}
	cold.at(fn, 1, at[4], 0)
	cold.at(fn, 1, at[5], 0)
	hot := &tape{}
	hot.at(fn, 1, at[2], 0)
	hot.at(fn, 1, at[3], 0)
	return tree(fn, 1, root, map[int]*jit.Trace{
		0: {Anchor: jit.Anchor{Addr: 1, IP: at[4]}, Ops: cold.ops, Status: jit.StatusReturned},
		1: {Anchor: jit.Anchor{Addr: 1, IP: at[2]}, Ops: hot.ops, Status: jit.StatusReturned},
	})
}

// tree wraps one root and its branches into the input a plan is built from.
func tree(fn *types.Function, addr int, root *jit.Trace, branches map[int]*jit.Trace) recording {
	hits := make([]int64, len(branches))
	for id := range branches {
		hits[id] = int64(len(branches) - id)
	}
	return recording{
		input: &jit.Input{
			Traces: fakeTraces{trees: map[jit.Anchor]*jit.Tree{
				root.Anchor: {Root: root, Branches: branches, Hits: hits},
			}},
			Address:  addr,
			Function: fn,
			Objects:  jit.Objects{addr: {Fn: fn}},
		},
		anchors: []jit.Anchor{{Addr: addr}, root.Anchor},
	}
}

// tailed records a function reaching a tail call: back to itself, which the
// recorder closes the recording on without stepping into the reused frame, or
// into another function, which it steps into so the records after it run in a
// frame the caller's own block cannot name.
func tailed(t *testing.T, self bool) recording {
	t.Helper()
	fn := &types.Function{
		Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.RETURN_CALL)
		}),
	}
	other := &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN) }),
	}
	at := offsets(fn.Code)

	rec := &tape{}
	rec.at(fn, 1, at[0], 0)
	rec.at(fn, 1, at[1], 0)
	tail := rec.at(fn, 1, at[2], 0)
	tail.Callee, tail.Seen = 1, types.BoxRef(1)
	if !self {
		tail.Callee, tail.Seen = 2, types.BoxRef(2)
		for _, ip := range offsets(other.Code) {
			rec.at(other, 2, ip, 0)
		}
	}

	out := tree(fn, 1, &jit.Trace{Anchor: jit.Anchor{Addr: 1}, Ops: rec.ops, Status: jit.StatusReturned}, nil)
	out.input.Constants = []types.Boxed{types.BoxRef(1)}
	out.input.Objects = jit.Objects{1: {Fn: fn}, 2: {Fn: other}}
	if !self {
		out.input.Constants = []types.Boxed{types.BoxRef(2)}
	}
	return out
}

// tape builds one recording the way interp's tracer writes it: one record per
// instruction, decoded from the function the frame runs.
type tape struct {
	ops []jit.Record
}

func (t *tape) at(fn *types.Function, addr, ip, depth int) *jit.Record {
	inst := instr.Instruction(fn.Code[ip:])
	t.ops = append(t.ops, jit.Record{Step: jit.Step{
		Op: inst.Opcode(), Args: jit.Args(inst), Fn: addr, IP: ip, Depth: depth,
	}})
	return &t.ops[len(t.ops)-1]
}

// fakeTraces is a jit.RecordedTraces client built from a fixed set of trees,
// standing in for interp's recorder.
type fakeTraces struct {
	trees map[jit.Anchor]*jit.Tree
}

func (f fakeTraces) Anchors(addr int) []int {
	var out []int
	for a, tree := range f.trees {
		if a.Addr == addr && tree.Root != nil {
			out = append(out, a.IP)
		}
	}
	sort.Ints(out)
	return out
}

func (f fakeTraces) RootAt(a jit.Anchor) *jit.Tree {
	tree, ok := f.trees[a]
	if !ok || tree.Root == nil {
		return nil
	}
	return tree
}

// reached is a trace plan's blocks as an adjacency list in breadth-first order
// from its root, the one numbering both forms share. An edge naming no block
// leaves the plan for the interpreter and meets a node of its own, which is the
// block the SSA lays out for exactly that exit.
func reached(plan jit.Plan) [][]int {
	ids := map[int]int{plan.Root: 0}
	queue := []int{plan.Root}
	var out [][]int
	for n := 0; n < len(queue); n++ {
		var succs []int
		if queue[n] != jit.NoBlock {
			for _, edge := range plan.Blocks[queue[n]].Term.Edges {
				node, ok := ids[edge.Index]
				if edge.Index == jit.NoBlock || !ok {
					node = len(queue)
					queue = append(queue, edge.Index)
					if edge.Index != jit.NoBlock {
						ids[edge.Index] = node
					}
				}
				succs = append(succs, node)
			}
		}
		out = append(out, succs)
	}
	return out
}

// offsets returns the offset of every instruction in code, so a fixture names
// the instruction it recorded rather than the byte it starts at.
func offsets(code []byte) []int {
	var out []int
	for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
		out = append(out, ip)
	}
	return out
}
