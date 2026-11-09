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

// FrameInfo locates one frame of a RuntimeError by function address and
// instruction pointer.
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

// trapped is the panic of a trap raised ahead of a fused handler's trapping
// consumer: at is the offset from the handler's first instruction to the
// instruction that faulted, the ip a trace names. dispatch's recover applies at
// once and rethrows the cause behind a trapped of at zero, which leaves a
// frame's ip alone when an outer dispatch recovers the same panic.
type trapped struct {
	err error
	at  int
}

// TrapCode values are the types.ErrorCode a guest handler sees for the
// matching Err sentinel; TrapCodeHostError covers every other Go error.
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

// Err sentinels are the semantic failures execution reports. ErrYield is not a
// failure: Run returns it when a root frame yields.
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
	// ErrInterpreterBusy is returned by a func Unmarshal bound to a VM function
	// when Run is active and no host function is running, such as from another
	// goroutine or a hook. Inside a host function the call runs nested instead.
	ErrInterpreterBusy   = errors.New("interpreter busy")
	ErrUncaughtException = errors.New("uncaught exception")

	// errYield is the panic value a root-frame YIELD raises to unwind the Run loop.
	// Run recovers it and returns ErrYield without wrapping, preserving all state so
	// the next Run call resumes exactly after the YIELD.
	errYield = errors.New("yield")
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

// ErrorCode maps err to the types.ErrorCode a guest handler observes: the code
// of a thrown Error, a TrapCode for an execution sentinel, or TrapCodeHostError.
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

func (t trapped) Error() string {
	return t.err.Error()
}

func (t trapped) Unwrap() error {
	return t.err
}
