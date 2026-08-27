package interp

import (
	"sync"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type blockingIterator struct {
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (i *blockingIterator) Kind() types.Kind { return types.KindRef }
func (i *blockingIterator) Type() types.Type { return types.NewIteratorType(types.TypeI32) }
func (i *blockingIterator) String() string   { return "blocking" }
func (i *blockingIterator) Next() bool       { return false }
func (i *blockingIterator) Done() bool       { return false }

func (i *blockingIterator) Current() types.Value {
	i.once.Do(func() { close(i.entered) })
	<-i.release
	return types.I32(0)
}

// trackedValue mirrors the copy in interp_test.go (package interp_test); this
// package stays package interp and cannot import that one.
type trackedValue struct {
	refs   []types.Ref
	closed int
}

func (v *trackedValue) Kind() types.Kind { return types.KindRef }
func (v *trackedValue) Type() types.Type { return types.TypeAny }
func (v *trackedValue) String() string   { return "tracked" }

func (v *trackedValue) Refs(dst []types.Ref) []types.Ref {
	return append(dst, v.refs...)
}

func (v *trackedValue) Close() error {
	v.closed++
	return nil
}

func TestTracer_Capture(t *testing.T) {
	t.Run("records top-level fallthrough as completed", func(t *testing.T) {
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		})
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{Addr: i.fr.addr, IP: 0})
		require.NotNil(t, result.trace)
		require.Equal(t, prof.CaptureOutcomePublished, result.outcome)
		require.Equal(t, prof.CaptureReasonNone, result.reason)
		published := tracer.RootAt(jit.Anchor{Addr: i.fr.addr, IP: 0})
		require.NotNil(t, published)
		require.Same(t, result.trace, published.Root)
		tr := result.trace
		require.Equal(t, jit.StatusCompleted, tr.Status)
		require.NotEmpty(t, tr.Ops)
		require.Equal(t, instr.I32_CONST, tr.Ops[len(tr.Ops)-1].Op)
	})

	t.Run("records yield as a terminal deopt boundary", func(t *testing.T) {
		// YIELD is a suspension point: capture records it as the trace's terminal
		// (status=jit.StatusReturned) instead of aborting, so the JIT can lower it to a deopt.
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.YIELD),
		})
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{Addr: i.fr.addr, IP: 0})
		require.NotNil(t, result.trace)
		require.Equal(t, prof.CaptureOutcomePublished, result.outcome)
		require.Equal(t, prof.CaptureReasonNone, result.reason)
		published := tracer.RootAt(jit.Anchor{Addr: i.fr.addr, IP: 0})
		require.NotNil(t, published)
		require.Same(t, result.trace, published.Root)
		tr := result.trace
		require.Equal(t, jit.StatusReturned, tr.Status)
		require.NotEmpty(t, tr.Ops)
		require.Equal(t, instr.YIELD, tr.Ops[len(tr.Ops)-1].Op)
	})

	t.Run("continues after primitive array set", func(t *testing.T) {
		array := types.TypedArray[int32]{1}
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(array))
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, jit.StatusCompleted, result.trace.Status)
		require.Equal(t, instr.I32_CONST, result.trace.Ops[len(result.trace.Ops)-1].Op)
	})

	t.Run("continues after scalar struct set", func(t *testing.T) {
		structure := types.NewStruct(
			types.NewStructType(types.NewStructField(types.TypeI32)),
			types.BoxI32(1),
		)
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(structure))
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, jit.StatusCompleted, result.trace.Status)
		require.Equal(t, instr.I32_CONST, result.trace.Ops[len(result.trace.Ops)-1].Op)
	})

	t.Run("ends at a ref-field struct set", func(t *testing.T) {
		structure := types.NewStruct(
			types.NewStructType(types.NewStructField(types.TypeI32Array)),
			types.BoxedNull,
		)
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.REF_NULL),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(structure))
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, jit.StatusReturned, result.trace.Status)
		last := result.trace.Ops[len(result.trace.Ops)-1]
		require.Equal(t, instr.STRUCT_SET, last.Op)
		require.True(t, last.Terminal)
	})

	t.Run("records bulk mutation as a terminal deopt boundary", func(t *testing.T) {
		array := types.TypedArray[int32]{1, 2}
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.ARRAY_FILL),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(array))
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, jit.StatusReturned, result.trace.Status)
		require.Equal(t, instr.ARRAY_FILL, result.trace.Ops[len(result.trace.Ops)-1].Op)
	})

	t.Run("still aborts at allocation", func(t *testing.T) {
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.ARRAY_NEW_DEFAULT, 0),
		}, program.WithTypes(types.TypeI32Array))
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{})
		require.Nil(t, result.trace)
		require.Equal(t, prof.CaptureOutcomeRejected, result.outcome)
		require.Equal(t, prof.CaptureReasonUnsupportedOp, result.reason)
		require.Nil(t, tracer.RootAt(jit.Anchor{}))
	})

	t.Run("publishes fallback and loop statuses", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			status jit.Status
		}{
			{name: "fallback", status: jit.StatusFallback},
			{name: "loop", status: jit.StatusLoop},
		} {
			tracer := newTracer()
			root := jit.Anchor{Addr: 1, IP: 4}
			tree := tracer.tree(root)
			tr := &jit.Trace{Anchor: root}

			result := tracer.publish(root, tree, tr, tt.status, prof.CaptureReasonNone)

			require.Same(t, tr, result.trace, tt.name)
			require.Equal(t, tt.status, tr.Status, tt.name)
			require.Equal(t, prof.CaptureOutcomePublished, result.outcome, tt.name)
			require.Equal(t, prof.CaptureReasonNone, result.reason, tt.name)
			published := tracer.RootAt(root)
			require.NotNil(t, published, tt.name)
			require.Same(t, tr, published.Root, tt.name)
		}
	})

	primitive := types.TypedArray[int32]{1}
	array := types.NewArray(types.TypeI32Array, types.BoxI32(1))
	structure := types.NewStruct(
		types.NewStructType(types.NewStructField(types.TypeI32)),
		types.BoxI32(1),
	)
	mutationTests := []struct {
		name  string
		value types.Value
		op    instr.Opcode
		read  func() types.Boxed
	}{
		{name: "primitive array", value: primitive, op: instr.ARRAY_SET, read: func() types.Boxed { return types.BoxI32(primitive[0]) }},
		{name: "boxed array", value: array, op: instr.ARRAY_SET, read: func() types.Boxed { return array.Elems[0] }},
		{name: "struct", value: structure, op: instr.STRUCT_SET, read: func() types.Boxed { return structure.Field(0) }},
	}
	for _, tt := range mutationTests {
		t.Run("isolates captured mutation for "+tt.name, func(t *testing.T) {
			tracer := newTracer()
			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(tt.op),
			}, program.WithConstants(tt.value))
			i := New(prog, withTracer(tracer), WithThreshold(-1))
			defer i.Close()

			tracer.capture(i, jit.Anchor{})
			require.Equal(t, types.BoxI32(1), tt.read())
		})
	}

	shared := types.TypedArray[int32]{0}
	backing := types.TypedArray[int32]{0, 0, 0}
	aliasTests := []struct {
		name      string
		first     types.TypedArray[int32]
		second    types.TypedArray[int32]
		setIndex  uint64
		readIndex uint64
		original  func() int32
	}{
		{name: "same slice", first: shared, second: shared, original: func() int32 { return shared[0] }},
		{name: "overlapping subslices", first: backing[:2:2], second: backing[1:3], setIndex: 1, original: func() int32 { return backing[1] }},
	}
	for _, tt := range aliasTests {
		t.Run("preserves typed array aliases for "+tt.name, func(t *testing.T) {
			tracer := newTracer()
			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, tt.setIndex),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_SET),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.I32_CONST, tt.readIndex),
				instr.New(instr.ARRAY_GET),
			}, program.WithConstants(tt.first, tt.second))
			i := New(prog, withTracer(tracer), WithThreshold(-1))
			defer i.Close()

			result := tracer.capture(i, jit.Anchor{})
			require.NotNil(t, result.trace)
			require.Equal(t, types.BoxI32(1), result.trace.Ops[len(result.trace.Ops)-1].Seen)
			require.Zero(t, tt.original())
		})
	}

	t.Run("cuts an oversized trace at a resumable boundary", func(t *testing.T) {
		code := make([]instr.Instruction, opLimit+1)
		for j := range code {
			code[j] = instr.New(instr.NOP)
		}
		tracer := newTracer()
		i := New(program.New(code), withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{Addr: 0, IP: 0})
		require.NotNil(t, result.trace)
		require.Equal(t, prof.CaptureOutcomePartial, result.outcome)
		require.Equal(t, prof.CaptureReasonOpLimit, result.reason)
		published := tracer.RootAt(jit.Anchor{})
		require.NotNil(t, published)
		require.Same(t, result.trace, published.Root)
		tr := result.trace
		require.Equal(t, jit.StatusPartial, tr.Status)
		require.Len(t, tr.Ops, opLimit+1)
		require.True(t, tr.Ops[len(tr.Ops)-1].Cut)
		require.Equal(t, opLimit, tr.Ops[len(tr.Ops)-1].Target)
	})

	t.Run("cuts a non-anchor back edge at its loop header", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		b.Emit(instr.NOP).
			Bind(loop).
			Emit(instr.I32_CONST, 1).
			Emit(instr.DROP).
			Br(loop)
		prog, err := b.Build()
		require.NoError(t, err)
		tracer := newTracer()
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, jit.Anchor{Addr: 0, IP: 0})
		require.NotNil(t, result.trace)
		tr := result.trace
		require.Equal(t, jit.StatusPartial, tr.Status)
		require.Len(t, tr.Ops, 5)
		require.Equal(t, instr.BR, tr.Ops[len(tr.Ops)-2].Op)
		require.True(t, tr.Ops[len(tr.Ops)-1].Cut)
		require.Equal(t, 1, tr.Ops[len(tr.Ops)-1].Target)
	})

	t.Run("records one entry concurrently", func(t *testing.T) {
		tracer := newTracer()
		release := make(chan struct{})
		iter := &blockingIterator{entered: make(chan struct{}), release: release}
		prog := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CORO_VALUE)},
			program.WithConstants(iter),
		)

		const workers = attemptLimit + 1
		interpreters := make([]*Interpreter, workers)
		done := make(chan struct{}, workers)
		interpreters[0] = New(prog, withTracer(tracer), WithThreshold(-1))
		go func() {
			tracer.capture(interpreters[0], jit.Anchor{})
			done <- struct{}{}
		}()
		<-iter.entered

		started := make(chan struct{})
		for idx := 1; idx < workers; idx++ {
			i := New(prog, withTracer(tracer), WithThreshold(-1))
			interpreters[idx] = i
			go func() {
				started <- struct{}{}
				tracer.capture(i, jit.Anchor{})
				done <- struct{}{}
			}()
		}
		for range workers - 1 {
			<-started
		}
		close(release)

		for range workers {
			<-done
		}
		for _, i := range interpreters {
			require.NoError(t, i.Close())
		}
		tracer.mu.Lock()
		attempts := tracer.trees[jit.Anchor{}].Attempts
		tracer.mu.Unlock()
		require.Equal(t, 1, attempts)
	})

	t.Run("does not publish a side exit to a removed tree", func(t *testing.T) {
		tracer := newTracer()
		release := make(chan struct{})
		iter := &blockingIterator{entered: make(chan struct{}), release: release}
		prog := program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.CORO_VALUE),
		})
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		addr := i.alloc(iter)
		i.stack[0] = types.BoxRef(addr)
		i.sp = 1
		root := jit.Anchor{}
		tree := tracer.tree(root)
		tree.Root = &jit.Trace{Anchor: root, Status: jit.StatusCompleted}

		done := make(chan struct{}, 1)
		go func() {
			tracer.branch(i, root, jit.Anchor{Addr: 0, IP: 1})
			done <- struct{}{}
		}()
		<-iter.entered
		tracer.remove(0)
		close(release)
		<-done

		require.Empty(t, tree.Branches)
		require.Nil(t, tracer.RootAt(root))
	})

	t.Run("isolates function reclamation", func(t *testing.T) {
		tracer := newTracer()
		i := New(
			program.New([]instr.Instruction{instr.New(instr.DROP)}),
			withTracer(tracer),
			WithThreshold(-1),
		)
		defer i.Close()

		fn := &types.Function{Code: []byte{byte(instr.NOP)}}
		addr := i.alloc(fn)
		i.bind(addr, fn, true)
		root := jit.Anchor{Addr: addr}
		tracer.trees[root] = &jit.Tree{Root: &jit.Trace{Anchor: root, Status: jit.StatusCompleted}}
		require.NotEmpty(t, tracer.exactCodes(i)[addr])
		i.stack[0] = types.BoxRef(addr)
		i.sp = 1

		done := make(chan struct{}, 1)
		go func() {
			tracer.capture(i, jit.Anchor{})
			done <- struct{}{}
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			require.Fail(t, "capture deadlocked while reclaiming a function")
		}
		require.NotNil(t, tracer.RootAt(jit.Anchor{}))
		require.NotNil(t, tracer.RootAt(root))
		require.NotEmpty(t, i.instrs[addr])
		require.NotEmpty(t, tracer.exactCodes(i)[addr])
		require.True(t, i.dynamic[addr])
	})

	t.Run("does not finalize live values while recording", func(t *testing.T) {
		tracer := newTracer()
		i := New(
			program.New([]instr.Instruction{instr.New(instr.DROP)}),
			withTracer(tracer),
			WithThreshold(-1),
		)
		defer i.Close()

		value := &trackedValue{}
		addr := i.alloc(value)
		i.stack[0] = types.BoxRef(addr)
		i.sp = 1

		tracer.capture(i, jit.Anchor{})
		require.Zero(t, value.closed)
	})

	t.Run("does not publish aborted traces", func(t *testing.T) {
		tracer := newTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.REF_NEW),
		})
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		for range attemptLimit + 1 {
			tr := tracer.capture(i, jit.Anchor{})
			require.Nil(t, tr.trace)
		}

		tracer.mu.Lock()
		attempts := tracer.trees[jit.Anchor{}].Attempts
		tracer.mu.Unlock()
		require.Equal(t, attemptLimit, attempts)
		require.Nil(t, tracer.RootAt(jit.Anchor{}))
	})

}

func TestTracer_OrdersAnchors(t *testing.T) {
	t.Run("returns anchors in instruction order", func(t *testing.T) {
		tracer := newTracer()
		const count = 64
		for ip := count - 1; ip >= 0; ip-- {
			tracer.trees[jit.Anchor{Addr: 1, IP: ip}] = &jit.Tree{Root: &jit.Trace{Anchor: jit.Anchor{Addr: 1, IP: ip}, Status: jit.StatusCompleted}}
		}

		want := make([]int, count)
		for ip := range count {
			want[ip] = ip
		}
		require.Equal(t, want, tracer.Anchors(1))
	})
}

func TestTracer_Headers(t *testing.T) {
	t.Run("concurrent calls return identical memoized headers", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		b.Locals(types.TypeI32).
			Emit(instr.I32_CONST, 0).
			Emit(instr.LOCAL_SET, 0).
			Bind(loop).
			Emit(instr.LOCAL_GET, 0).
			Emit(instr.I32_CONST, 1).
			Emit(instr.I32_ADD).
			Emit(instr.LOCAL_TEE, 0).
			Emit(instr.I32_CONST, 4).
			Emit(instr.I32_LT_S).
			BrIf(loop).
			Emit(instr.LOCAL_GET, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		tracer := newTracer()
		i := New(prog, withTracer(tracer), WithThreshold(-1))
		defer i.Close()

		const workers = 16
		results := make([][]int, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for w := range workers {
			go func() {
				defer wg.Done()
				results[w] = tracer.headers(i, 0)
			}()
		}
		wg.Wait()

		want := results[0]
		require.NotEmpty(t, want)
		for _, got := range results {
			require.Equal(t, want, got)
		}
	})
}

func TestTracer_IsolatesPrograms(t *testing.T) {
	tracer := newTracer()
	first := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)})
	second := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 2)})

	left := New(first, withTracer(tracer), WithThreshold(-1))
	defer left.Close()
	before := tracer.exactCodes(left)

	right := New(second, withTracer(tracer), WithThreshold(-1))
	defer right.Close()
	after := right.tracer.exactCodes(right)

	require.Same(t, tracer, left.tracer)
	require.NotSame(t, tracer, right.tracer)
	require.NotSame(t, &before[0][0], &after[0][0])
}

func TestTracer_Remove(t *testing.T) {
	tracer := newTracer()
	first := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
	}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 2), instr.New(instr.RETURN)).MustBuild()))
	i := New(first, withTracer(tracer), WithThreshold(-1))
	defer i.Close()

	exact := tracer.exactCodes(i)
	require.NotNil(t, exact[1])
	tracer.remove(1)
	require.Nil(t, tracer.exact)

	second, err := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 3), instr.New(instr.RETURN)).
		Build()
	require.NoError(t, err)
	i.bind(1, second, true)
	rebuilt := tracer.exactCodes(i)
	require.NotSame(t, &exact[1][0], &rebuilt[1][0])
}
