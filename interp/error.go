package interp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/types"
)

// RuntimeError wraps a guest execution failure with the VM call stack at the
// point of failure. Frames are innermost first.
type RuntimeError struct {
	Err    error
	Frames []FrameInfo
}

type FrameInfo struct {
	Func int
	IP   int
}

// escape carries a guest throw that found no handler out of the dispatch loop.
// dispatch's recover turns it into the returned error without re-attempting the
// (already failed) handler search.
type escape struct {
	err error
}

var (
	ErrUnknownOpcode       = errors.New("unknown opcode")
	ErrUnreachableExecuted = errors.New("unreachable executed")
	ErrSegmentationFault   = errors.New("segmentation fault")
	ErrStackOverflow       = errors.New("stack overflow")
	ErrStackUnderflow      = errors.New("stack underflow")
	ErrFrameOverflow       = errors.New("frame overflow")
	ErrFrameUnderflow      = errors.New("frame underflow")
	ErrTypeMismatch        = errors.New("type mismatch")
	ErrDivideByZero        = errors.New("divide by zero")
	ErrIndexOutOfRange     = errors.New("index out of range")
	ErrFuelExhausted       = errors.New("fuel exhausted")
	ErrHeapExhausted       = errors.New("heap exhausted")
	ErrYield               = errors.New("yield")
	ErrCoroutineDone       = errors.New("coroutine done")
	ErrUncaughtException   = errors.New("uncaught exception")
)

const (
	TrapCodeUnknownOpcode       types.ErrorCode = -1
	TrapCodeUnreachableExecuted types.ErrorCode = -2
	TrapCodeSegmentationFault   types.ErrorCode = -3
	TrapCodeStackOverflow       types.ErrorCode = -4
	TrapCodeStackUnderflow      types.ErrorCode = -5
	TrapCodeFrameOverflow       types.ErrorCode = -6
	TrapCodeFrameUnderflow      types.ErrorCode = -7
	TrapCodeTypeMismatch        types.ErrorCode = -8
	TrapCodeDivideByZero        types.ErrorCode = -9
	TrapCodeIndexOutOfRange     types.ErrorCode = -10
	TrapCodeFuelExhausted       types.ErrorCode = -11
	TrapCodeHeapExhausted       types.ErrorCode = -12
	TrapCodeCoroutineDone       types.ErrorCode = -13
	TrapCodeUncaughtException   types.ErrorCode = -14
	TrapCodeHostError           types.ErrorCode = -15
)

var errorCodes = []struct {
	err  error
	code types.ErrorCode
}{
	{ErrUnknownOpcode, TrapCodeUnknownOpcode},
	{ErrUnreachableExecuted, TrapCodeUnreachableExecuted},
	{ErrSegmentationFault, TrapCodeSegmentationFault},
	{ErrStackOverflow, TrapCodeStackOverflow},
	{ErrStackUnderflow, TrapCodeStackUnderflow},
	{ErrFrameOverflow, TrapCodeFrameOverflow},
	{ErrFrameUnderflow, TrapCodeFrameUnderflow},
	{ErrTypeMismatch, TrapCodeTypeMismatch},
	{ErrDivideByZero, TrapCodeDivideByZero},
	{ErrIndexOutOfRange, TrapCodeIndexOutOfRange},
	{ErrFuelExhausted, TrapCodeFuelExhausted},
	{ErrHeapExhausted, TrapCodeHeapExhausted},
	{ErrCoroutineDone, TrapCodeCoroutineDone},
	{ErrUncaughtException, TrapCodeUncaughtException},
}

// errYield is the panic value a root-frame YIELD raises to unwind the Run loop.
// Run recovers it and returns ErrYield without wrapping, preserving all state so
// the next Run call resumes exactly after the YIELD.
var errYield = errors.New("yield")

func ErrorCode(err error) types.ErrorCode {
	if err == nil {
		return types.ErrorCodeNone
	}
	if errors.Is(err, ErrYield) {
		return types.ErrorCodeNone
	}
	var exc *types.Error
	if errors.As(err, &exc) {
		return exc.Code()
	}
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) {
		err = runtimeErr.Err
	}
	for _, ec := range errorCodes {
		if errors.Is(err, ec.err) {
			return ec.code
		}
	}
	return TrapCodeHostError
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := "<nil>"
	if e.Err != nil {
		msg = e.Err.Error()
	}
	if len(e.Frames) == 0 {
		return msg
	}

	var b strings.Builder
	b.WriteString(msg)
	for idx, f := range e.Frames {
		if idx == 0 {
			fmt.Fprintf(&b, ": fn=%d ip=%d", f.Func, f.IP)
			continue
		}
		fmt.Fprintf(&b, " <- fn=%d ip=%d", f.Func, f.IP)
	}
	return b.String()
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
