package violations

import "testing"

type eventualer struct{}

type resource struct{}

var require eventualer

func (eventualer) Eventually(*testing.T, func() bool, ...int) {}

func (resource) Ready() bool { return true }

func (resource) Close() {}

func helperThing() { NewThing() } // want "TP007"

func TestPublicBehavior(t *testing.T) {}

func TestHelperThing(t *testing.T) { helperThing() }

func TestPolling(t *testing.T) {
	r := resource{}
	require.Eventually(t, func() bool { return r.Ready() }, 1) // want "TP003"
	r.Close()
}

func TestPollingWithoutClose(t *testing.T) {
	r := resource{}
	require.Eventually(t, func() bool { return r.Ready() }, 1)
	_ = r
}
