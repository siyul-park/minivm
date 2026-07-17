package interp

import (
	"sync"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
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

func TestNewTracer(t *testing.T) {
	tracer := NewTracer()
	require.NotNil(t, tracer)
	require.NotNil(t, tracer.trees)
}

func TestTracer_Capture(t *testing.T) {
	t.Run("records top-level fallthrough as completed", func(t *testing.T) {
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
		})
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{addr: i.fr.addr, ip: 0})
		require.NotNil(t, result.trace)
		tr := result.trace
		require.Equal(t, completed, tr.kind)
		require.NotEmpty(t, tr.ops)
		require.Equal(t, instr.I32_CONST, tr.ops[len(tr.ops)-1].op)
	})

	t.Run("records yield as a terminal deopt boundary", func(t *testing.T) {
		// YIELD is a suspension point: capture records it as the trace's terminal
		// (kind=returned) instead of aborting, so the JIT can lower it to a deopt.
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.YIELD),
		})
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{addr: i.fr.addr, ip: 0})
		require.NotNil(t, result.trace)
		tr := result.trace
		require.Equal(t, returned, tr.kind)
		require.NotEmpty(t, tr.ops)
		require.Equal(t, instr.YIELD, tr.ops[len(tr.ops)-1].op)
	})

	t.Run("continues after primitive array set", func(t *testing.T) {
		array := types.TypedArray[int32]{1}
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(array))
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, completed, result.trace.kind)
		require.Equal(t, instr.I32_CONST, result.trace.ops[len(result.trace.ops)-1].op)
	})

	t.Run("continues after scalar struct set", func(t *testing.T) {
		structure := types.NewStruct(
			types.NewStructType(types.NewStructField(types.TypeI32)),
			types.BoxI32(1),
		)
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(structure))
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, completed, result.trace.kind)
		require.Equal(t, instr.I32_CONST, result.trace.ops[len(result.trace.ops)-1].op)
	})

	t.Run("ends at a ref-field struct set", func(t *testing.T) {
		structure := types.NewStruct(
			types.NewStructType(types.NewStructField(types.TypeI32Array)),
			types.BoxedNull,
		)
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.REF_NULL),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(structure))
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, returned, result.trace.kind)
		last := result.trace.ops[len(result.trace.ops)-1]
		require.Equal(t, instr.STRUCT_SET, last.op)
		require.True(t, last.terminal)
	})

	t.Run("records bulk mutation as a terminal deopt boundary", func(t *testing.T) {
		array := types.TypedArray[int32]{1, 2}
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.ARRAY_FILL),
			instr.New(instr.I32_CONST, 7),
		}, program.WithConstants(array))
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{})
		require.NotNil(t, result.trace)
		require.Equal(t, returned, result.trace.kind)
		require.Equal(t, instr.ARRAY_FILL, result.trace.ops[len(result.trace.ops)-1].op)
	})

	t.Run("still aborts at allocation", func(t *testing.T) {
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.ARRAY_NEW_DEFAULT, 0),
		}, program.WithTypes(types.TypeI32Array))
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{})
		require.Equal(t, prof.CaptureReasonUnsupportedOp, result.reason)
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
			tracer := NewTracer()
			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(tt.op),
			}, program.WithConstants(tt.value))
			i := New(prog, WithTracer(tracer), WithThreshold(-1))
			defer i.Close()

			tracer.capture(i, anchor{})
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
			tracer := NewTracer()
			prog := program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.I32_CONST, tt.setIndex),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.ARRAY_SET),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.I32_CONST, tt.readIndex),
				instr.New(instr.ARRAY_GET),
			}, program.WithConstants(tt.first, tt.second))
			i := New(prog, WithTracer(tracer), WithThreshold(-1))
			defer i.Close()

			result := tracer.capture(i, anchor{})
			require.NotNil(t, result.trace)
			require.Equal(t, types.BoxI32(1), result.trace.ops[len(result.trace.ops)-1].seen)
			require.Zero(t, tt.original())
		})
	}

	t.Run("cuts an oversized trace at a resumable boundary", func(t *testing.T) {
		code := make([]instr.Instruction, opLimit+1)
		for j := range code {
			code[j] = instr.New(instr.NOP)
		}
		tracer := NewTracer()
		i := New(program.New(code), WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{addr: 0, ip: 0})
		require.NotNil(t, result.trace)
		tr := result.trace
		require.Equal(t, partial, tr.kind)
		require.Len(t, tr.ops, opLimit+1)
		require.True(t, tr.ops[len(tr.ops)-1].cut)
		require.Equal(t, opLimit, tr.ops[len(tr.ops)-1].target)
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
		tracer := NewTracer()
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result := tracer.capture(i, anchor{addr: 0, ip: 0})
		require.NotNil(t, result.trace)
		tr := result.trace
		require.Equal(t, partial, tr.kind)
		require.Len(t, tr.ops, 5)
		require.Equal(t, instr.BR, tr.ops[len(tr.ops)-2].op)
		require.True(t, tr.ops[len(tr.ops)-1].cut)
		require.Equal(t, 1, tr.ops[len(tr.ops)-1].target)
	})

	t.Run("records one entry concurrently", func(t *testing.T) {
		tracer := NewTracer()
		release := make(chan struct{})
		iter := &blockingIterator{entered: make(chan struct{}), release: release}
		prog := program.New(
			[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CORO_VALUE)},
			program.WithConstants(iter),
		)

		const workers = attemptLimit + 1
		interpreters := make([]*Interpreter, workers)
		done := make(chan struct{}, workers)
		interpreters[0] = New(prog, WithTracer(tracer), WithThreshold(-1))
		go func() {
			tracer.capture(interpreters[0], anchor{})
			done <- struct{}{}
		}()
		<-iter.entered

		started := make(chan struct{})
		for idx := 1; idx < workers; idx++ {
			i := New(prog, WithTracer(tracer), WithThreshold(-1))
			interpreters[idx] = i
			go func() {
				started <- struct{}{}
				tracer.capture(i, anchor{})
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
		attempts := tracer.trees[anchor{}].attempts
		tracer.mu.Unlock()
		require.Equal(t, 1, attempts)
	})

	t.Run("does not publish a side exit to a removed tree", func(t *testing.T) {
		tracer := NewTracer()
		release := make(chan struct{})
		iter := &blockingIterator{entered: make(chan struct{}), release: release}
		prog := program.New([]instr.Instruction{
			instr.New(instr.NOP),
			instr.New(instr.CORO_VALUE),
		})
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		addr := i.keep(iter)
		i.stack[0] = types.BoxRef(addr)
		i.sp = 1
		root := anchor{}
		tree := tracer.tree(root)
		tree.root = &trace{anchor: root, kind: completed}

		done := make(chan struct{}, 1)
		go func() {
			tracer.branch(i, root, anchor{addr: 0, ip: 1})
			done <- struct{}{}
		}()
		<-iter.entered
		tracer.remove(0)
		close(release)
		<-done

		require.Empty(t, tree.branches)
		require.Nil(t, tracer.rootAt(root))
	})

	t.Run("isolates function reclamation", func(t *testing.T) {
		tracer := NewTracer()
		i := New(
			program.New([]instr.Instruction{instr.New(instr.DROP)}),
			WithTracer(tracer),
			WithThreshold(-1),
		)
		defer i.Close()

		fn := &types.Function{Code: []byte{byte(instr.NOP)}}
		addr := i.keep(fn)
		i.bind(addr, fn, true)
		root := anchor{addr: addr}
		tracer.trees[root] = &tree{root: &trace{anchor: root, kind: completed}}
		require.NotEmpty(t, tracer.exactCodes(i)[addr])
		i.stack[0] = types.BoxRef(addr)
		i.sp = 1

		done := make(chan struct{}, 1)
		go func() {
			tracer.capture(i, anchor{})
			done <- struct{}{}
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			require.Fail(t, "capture deadlocked while reclaiming a function")
		}
		require.NotNil(t, tracer.rootAt(anchor{}))
		require.NotNil(t, tracer.rootAt(root))
		require.NotEmpty(t, i.instrs[addr])
		require.NotEmpty(t, tracer.exactCodes(i)[addr])
		require.True(t, i.dynamic[addr])
	})

	t.Run("does not finalize live values while recording", func(t *testing.T) {
		tracer := NewTracer()
		i := New(
			program.New([]instr.Instruction{instr.New(instr.DROP)}),
			WithTracer(tracer),
			WithThreshold(-1),
		)
		defer i.Close()

		value := &trackedValue{}
		addr := i.keep(value)
		i.stack[0] = types.BoxRef(addr)
		i.sp = 1

		tracer.capture(i, anchor{})
		require.Zero(t, value.closed)
	})

	t.Run("does not publish aborted traces", func(t *testing.T) {
		tracer := NewTracer()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.REF_NEW),
		})
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		for range attemptLimit + 1 {
			tr := tracer.capture(i, anchor{})
			require.Nil(t, tr.trace)
		}

		tracer.mu.Lock()
		attempts := tracer.trees[anchor{}].attempts
		tracer.mu.Unlock()
		require.Equal(t, attemptLimit, attempts)
		require.Nil(t, tracer.rootAt(anchor{}))
	})

}

func TestTracer_OrdersAnchors(t *testing.T) {
	t.Run("returns anchors in instruction order", func(t *testing.T) {
		tracer := NewTracer()
		const count = 64
		for ip := count - 1; ip >= 0; ip-- {
			tracer.trees[anchor{addr: 1, ip: ip}] = &tree{root: &trace{anchor: anchor{addr: 1, ip: ip}, kind: completed}}
		}

		want := make([]int, count)
		for ip := range count {
			want[ip] = ip
		}
		require.Equal(t, want, tracer.anchors(1))
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
		tracer := NewTracer()
		i := New(prog, WithTracer(tracer), WithThreshold(-1))
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
	tracer := NewTracer()
	first := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 1)})
	second := program.New([]instr.Instruction{instr.New(instr.I32_CONST, 2)})

	left := New(first, WithTracer(tracer), WithThreshold(-1))
	defer left.Close()
	before := tracer.exactCodes(left)

	right := New(second, WithTracer(tracer), WithThreshold(-1))
	defer right.Close()
	after := right.tracer.exactCodes(right)

	require.Same(t, tracer, left.tracer)
	require.NotSame(t, tracer, right.tracer)
	require.NotSame(t, &before[0][0], &after[0][0])
}

func TestTracer_Remove(t *testing.T) {
	tracer := NewTracer()
	first := program.New([]instr.Instruction{
		instr.New(instr.I32_CONST, 1),
	}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 2), instr.New(instr.RETURN)).MustBuild()))
	i := New(first, WithTracer(tracer), WithThreshold(-1))
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
