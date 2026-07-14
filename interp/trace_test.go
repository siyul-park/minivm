package interp

import (
	"sync"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
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

		result, err := tracer.capture(i, anchor{addr: i.fr.addr, ip: 0})
		require.NoError(t, err)
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

		result, err := tracer.capture(i, anchor{addr: i.fr.addr, ip: 0})
		require.NoError(t, err)
		require.NotNil(t, result.trace)
		tr := result.trace
		require.Equal(t, returned, tr.kind)
		require.NotEmpty(t, tr.ops)
		require.Equal(t, instr.YIELD, tr.ops[len(tr.ops)-1].op)
	})

	t.Run("cuts an oversized trace at a resumable boundary", func(t *testing.T) {
		code := make([]instr.Instruction, opLimit+1)
		for j := range code {
			code[j] = instr.New(instr.NOP)
		}
		tracer := NewTracer()
		i := New(program.New(code), WithTracer(tracer), WithThreshold(-1))
		defer i.Close()

		result, err := tracer.capture(i, anchor{addr: 0, ip: 0})
		require.NoError(t, err)
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

		result, err := tracer.capture(i, anchor{addr: 0, ip: 0})
		require.NoError(t, err)
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
		errs := make(chan error, workers)
		interpreters[0] = New(prog, WithTracer(tracer), WithThreshold(-1))
		go func() {
			_, err := tracer.capture(interpreters[0], anchor{})
			errs <- err
		}()
		<-iter.entered

		started := make(chan struct{})
		for idx := 1; idx < workers; idx++ {
			i := New(prog, WithTracer(tracer), WithThreshold(-1))
			interpreters[idx] = i
			go func() {
				started <- struct{}{}
				_, err := tracer.capture(i, anchor{})
				errs <- err
			}()
		}
		for range workers - 1 {
			<-started
		}
		close(release)

		for range workers {
			require.NoError(t, <-errs)
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

		done := make(chan error, 1)
		go func() {
			_, err := tracer.exit(i, root, anchor{addr: 0, ip: 1})
			done <- err
		}()
		<-iter.entered
		tracer.remove(0)
		close(release)
		require.NoError(t, <-done)

		require.Empty(t, tree.branches)
		require.Nil(t, tracer.rootAt(root))
	})

	t.Run("does not deadlock when recording reclaims a function", func(t *testing.T) {
		tracer := NewTracer()
		i := New(
			program.New([]instr.Instruction{instr.New(instr.DROP)}),
			WithTracer(tracer),
			WithThreshold(-1),
		)
		defer i.Close()

		addr := i.keep(&types.Function{Code: []byte{byte(instr.NOP)}})
		i.stack[0] = types.BoxRef(addr)
		i.sp = 1

		done := make(chan error, 1)
		go func() {
			_, err := tracer.capture(i, anchor{})
			done <- err
		}()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("capture deadlocked while reclaiming a function")
		}
		require.NotNil(t, tracer.rootAt(anchor{}))
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
			tr, err := tracer.capture(i, anchor{})
			require.NoError(t, err)
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
	before := tracer.codes(left)

	right := New(second, WithTracer(tracer), WithThreshold(-1))
	defer right.Close()
	after := right.tracer.codes(right)

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

	exact := tracer.codes(i)
	require.NotNil(t, exact[1])
	tracer.remove(1)
	require.Nil(t, tracer.exact)

	second, err := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.I32_CONST, 3), instr.New(instr.RETURN)).
		Build()
	require.NoError(t, err)
	i.bind(1, second, true)
	rebuilt := tracer.codes(i)
	require.NotSame(t, &exact[1][0], &rebuilt[1][0])
}
