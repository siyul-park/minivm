package interp_test

import (
	"errors"
	"fmt"
	"testing"

	interp "github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestErrorCode(t *testing.T) {
	tests := []struct {
		err  error
		want types.ErrorCode
	}{
		{want: types.ErrorCodeNone},
		{err: interp.ErrYield, want: types.ErrorCodeNone},
		{err: types.NewError(42, "guest", types.BoxedNull), want: 42},
		{err: interp.ErrUnknownOpcode, want: interp.TrapCodeUnknownOpcode},
		{err: interp.ErrUnreachableExecuted, want: interp.TrapCodeUnreachableExecuted},
		{err: interp.ErrSegmentationFault, want: interp.TrapCodeSegmentationFault},
		{err: interp.ErrStackOverflow, want: interp.TrapCodeStackOverflow},
		{err: interp.ErrStackUnderflow, want: interp.TrapCodeStackUnderflow},
		{err: interp.ErrFrameOverflow, want: interp.TrapCodeFrameOverflow},
		{err: interp.ErrFrameUnderflow, want: interp.TrapCodeFrameUnderflow},
		{err: interp.ErrTypeMismatch, want: interp.TrapCodeTypeMismatch},
		{err: interp.ErrDivideByZero, want: interp.TrapCodeDivideByZero},
		{err: interp.ErrIndexOutOfRange, want: interp.TrapCodeIndexOutOfRange},
		{err: interp.ErrFuelExhausted, want: interp.TrapCodeFuelExhausted},
		{err: interp.ErrHeapExhausted, want: interp.TrapCodeHeapExhausted},
		{err: interp.ErrCoroutineDone, want: interp.TrapCodeCoroutineDone},
		{err: interp.ErrUncaughtException, want: interp.TrapCodeUncaughtException},
		{err: errors.New("host"), want: interp.TrapCodeHostError},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.err), func(t *testing.T) {
			require.Equal(t, tt.want, interp.ErrorCode(tt.err))
			if tt.err != nil {
				require.Equal(t, tt.want, interp.ErrorCode(&interp.RuntimeError{Err: fmt.Errorf("wrapped: %w", tt.err)}))
			}
		})
	}
}

func TestRuntimeError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *interp.RuntimeError
		want string
	}{
		{name: "nil receiver", want: "<nil>"},
		{name: "nil cause", err: &interp.RuntimeError{}, want: "<nil>"},
		{name: "cause", err: &interp.RuntimeError{Err: interp.ErrDivideByZero}, want: "divide by zero"},
		{
			name: "frames",
			err: &interp.RuntimeError{
				Err: interp.ErrDivideByZero,
				Frames: []interp.FrameInfo{
					{Func: 2, IP: 7},
					{Func: 1, IP: 3},
				},
			},
			want: "divide by zero: fn=2 ip=7 <- fn=1 ip=3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestRuntimeError_Unwrap(t *testing.T) {
	var nilErr *interp.RuntimeError
	require.NoError(t, nilErr.Unwrap())

	err := &interp.RuntimeError{Err: interp.ErrTypeMismatch}
	require.ErrorIs(t, err, interp.ErrTypeMismatch)
}
