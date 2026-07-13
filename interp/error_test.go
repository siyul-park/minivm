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
		name string
		err  error
		want types.ErrorCode
	}{
		{name: "nil", want: types.ErrorCodeNone},
		{name: "yield", err: ErrYield, want: types.ErrorCodeNone},
		{name: "guest error", err: types.NewError(42, "guest", types.BoxedNull), want: 42},
		{name: "unknown opcode", err: ErrUnknownOpcode, want: TrapCodeUnknownOpcode},
		{name: "unreachable", err: ErrUnreachableExecuted, want: TrapCodeUnreachableExecuted},
		{name: "segmentation fault", err: ErrSegmentationFault, want: TrapCodeSegmentationFault},
		{name: "stack overflow", err: ErrStackOverflow, want: TrapCodeStackOverflow},
		{name: "stack underflow", err: ErrStackUnderflow, want: TrapCodeStackUnderflow},
		{name: "frame overflow", err: ErrFrameOverflow, want: TrapCodeFrameOverflow},
		{name: "frame underflow", err: ErrFrameUnderflow, want: TrapCodeFrameUnderflow},
		{name: "type mismatch", err: ErrTypeMismatch, want: TrapCodeTypeMismatch},
		{name: "divide by zero", err: ErrDivideByZero, want: TrapCodeDivideByZero},
		{name: "index out of range", err: ErrIndexOutOfRange, want: TrapCodeIndexOutOfRange},
		{name: "fuel exhausted", err: ErrFuelExhausted, want: TrapCodeFuelExhausted},
		{name: "heap exhausted", err: ErrHeapExhausted, want: TrapCodeHeapExhausted},
		{name: "coroutine done", err: ErrCoroutineDone, want: TrapCodeCoroutineDone},
		{name: "uncaught exception", err: ErrUncaughtException, want: TrapCodeUncaughtException},
		{name: "host error", err: errors.New("host"), want: TrapCodeHostError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
