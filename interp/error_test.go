package interp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestErrorCode(t *testing.T) {
	tests := []struct {
		err  error
		want types.ErrorCode
	}{
		{want: types.ErrorCodeNone},
		{err: ErrYield, want: types.ErrorCodeNone},
		{err: types.NewError(42, "guest", types.BoxedNull), want: 42},
		{err: ErrUnknownOpcode, want: TrapCodeUnknownOpcode},
		{err: ErrUnreachableExecuted, want: TrapCodeUnreachableExecuted},
		{err: ErrSegmentationFault, want: TrapCodeSegmentationFault},
		{err: ErrStackOverflow, want: TrapCodeStackOverflow},
		{err: ErrStackUnderflow, want: TrapCodeStackUnderflow},
		{err: ErrFrameOverflow, want: TrapCodeFrameOverflow},
		{err: ErrFrameUnderflow, want: TrapCodeFrameUnderflow},
		{err: ErrTypeMismatch, want: TrapCodeTypeMismatch},
		{err: ErrDivideByZero, want: TrapCodeDivideByZero},
		{err: ErrIndexOutOfRange, want: TrapCodeIndexOutOfRange},
		{err: ErrFuelExhausted, want: TrapCodeFuelExhausted},
		{err: ErrHeapExhausted, want: TrapCodeHeapExhausted},
		{err: ErrCoroutineDone, want: TrapCodeCoroutineDone},
		{err: ErrUncaughtException, want: TrapCodeUncaughtException},
		{err: errors.New("host"), want: TrapCodeHostError},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.err), func(t *testing.T) {
			require.Equal(t, tt.want, ErrorCode(tt.err))
			if tt.err != nil {
				require.Equal(t, tt.want, ErrorCode(&RuntimeError{Err: fmt.Errorf("wrapped: %w", tt.err)}))
			}
		})
	}
}

func TestRuntimeError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *RuntimeError
		want string
	}{
		{name: "nil receiver", want: "<nil>"},
		{name: "nil cause", err: &RuntimeError{}, want: "<nil>"},
		{name: "cause", err: &RuntimeError{Err: ErrDivideByZero}, want: "divide by zero"},
		{
			name: "frames",
			err: &RuntimeError{
				Err: ErrDivideByZero,
				Frames: []FrameInfo{
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
	var nilErr *RuntimeError
	require.NoError(t, nilErr.Unwrap())

	err := &RuntimeError{Err: ErrTypeMismatch}
	require.ErrorIs(t, err, ErrTypeMismatch)
}
