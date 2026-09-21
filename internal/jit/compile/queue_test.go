package compile_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/types"
)

// newStub is a Machine factory for a Queue whose units never reach the
// backend: every unit here fails translation before Lower runs.
func newStub() compile.Machine { return stub{} }

// unlowerable is a Unit that fails Compile at translation, fast and without
// touching its Machine.
func unlowerable(address int) compile.Unit {
	return compile.Unit{Address: address, Function: &types.Function{}}
}

func TestNewQueue(t *testing.T) {
	t.Run("panics on a nil machine factory", func(t *testing.T) {
		require.Panics(t, func() { compile.NewQueue(nil, 1) })
	})

	t.Run("panics on fewer than one worker", func(t *testing.T) {
		require.Panics(t, func() { compile.NewQueue(newStub, 0) })
	})
}

func TestQueue_Submit(t *testing.T) {
	t.Run("refuses a second submit of the same address until drained", func(t *testing.T) {
		q := compile.NewQueue(newStub, 1)
		defer q.Close()

		require.True(t, q.Submit(unlowerable(1)))
		require.False(t, q.Submit(unlowerable(1)))

		require.Eventually(t, func() bool { return len(q.Drain()) == 1 }, time.Second, time.Millisecond)
		require.True(t, q.Submit(unlowerable(1)))
	})

	t.Run("refuses once the queue is closed", func(t *testing.T) {
		q := compile.NewQueue(newStub, 1)
		q.Close()

		require.False(t, q.Submit(unlowerable(1)))
	})
}

func TestQueue_Drain(t *testing.T) {
	q := compile.NewQueue(newStub, 2)
	defer q.Close()

	require.True(t, q.Submit(unlowerable(1)))
	require.True(t, q.Submit(unlowerable(2)))

	var jobs []compile.Job
	require.Eventually(t, func() bool {
		jobs = append(jobs, q.Drain()...)
		return len(jobs) == 2
	}, time.Second, time.Millisecond)

	addresses := map[int]bool{}
	for _, j := range jobs {
		require.ErrorIs(t, j.Err, compile.ErrUnsupported)
		require.Nil(t, j.Code)
		addresses[j.Unit.Address] = true
	}
	require.Equal(t, map[int]bool{1: true, 2: true}, addresses)

	require.True(t, q.Submit(unlowerable(1)))
}

func TestQueue_Close(t *testing.T) {
	q := compile.NewQueue(newStub, 1)
	require.True(t, q.Submit(unlowerable(1)))

	jobs := q.Close()
	require.Len(t, jobs, 1)
	require.Equal(t, 1, jobs[0].Unit.Address)
	require.ErrorIs(t, jobs[0].Err, compile.ErrUnsupported)

	require.False(t, q.Submit(unlowerable(2)))
	require.Empty(t, q.Close())
}
