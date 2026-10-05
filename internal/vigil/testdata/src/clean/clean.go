package clean

import (
	"context"
	"errors"
)

// State owns its state.
type State struct {
	value int
}

var errSentinel = errors.New("sentinel")

// NewState constructs State.
func NewState(value int) State {
	return State{value: value}
}

// Run executes the operation.
func (state State) Run(ctx context.Context) error {
	if ctx == nil {
		return errSentinel
	}
	return nil
}

// Read returns the value.
func (state State) Read() int {
	return state.value
}

func readValue() int {
	return makeValue()
}

func makeValue() int {
	return 1
}
