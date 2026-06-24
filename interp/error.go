package interp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
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

// errYield is the panic value a root-frame YIELD raises to unwind the Run loop.
// Run recovers it and returns ErrYield without wrapping, preserving all state so
// the next Run call resumes exactly after the YIELD.
var errYield = errors.New("yield")

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

func (i *Interpreter) runtimeError(r any) error {
	return &RuntimeError{
		Err:    i.cause(r),
		Frames: i.framesInfo(),
	}
}

func (i *Interpreter) guard(err *error) {
	if r := recover(); r != nil {
		*err = i.cause(r)
	}
}

func (i *Interpreter) cause(r any) error {
	switch e := r.(type) {
	case escape:
		return e.err
	case error:
		return e
	default:
		return fmt.Errorf("%v", r)
	}
}

// handle attempts to deliver a recovered panic to a guest exception handler. An
// escape is a throw that already failed its handler search, so it stays
// terminal; any other Go error (a runtime trap or a host-function failure) is
// converted to an Error value and delivered if a covering handler exists.
func (i *Interpreter) handle(r any) bool {
	if _, ok := r.(escape); ok {
		return false
	}
	err, ok := r.(error)
	if !ok {
		return false
	}
	fp, h, ok := i.handler()
	if !ok {
		return false
	}
	i.land(fp, h, i.wrap(err))
	return true
}

// handler walks frames from innermost outward for the first protected region
// covering the active instruction: the throwing site in the top frame, the call
// site (ip-1, CALL/RETURN_CALL are one byte) in each suspended caller.
func (i *Interpreter) handler() (int, instr.Handler, bool) {
	for fp := i.fp; fp >= 1; fp-- {
		f := &i.frames[fp-1]
		ip := f.ip
		if fp != i.fp {
			ip--
		}
		if f.addr < 0 || f.addr >= len(i.handlers) {
			continue
		}
		for _, h := range i.handlers[f.addr] {
			if h.Start <= ip && ip < h.End {
				return fp, h, true
			}
		}
	}
	return 0, instr.Handler{}, false
}

// land unwinds to the handler frame, discarding the frames and operand values
// above the protected region's entry depth, then delivers exc as the sole
// operand and resumes at the catch IP. exc keeps the single reference it already
// owned (popped off the stack by THROW, or freshly allocated for a trap).
func (i *Interpreter) land(fp int, h instr.Handler, exc types.Boxed) {
	for i.fp > fp {
		i.discard(&i.frames[i.fp-1])
		i.fp--
	}
	f := &i.frames[fp-1]
	base := f.bp + h.Depth
	for s := i.sp - 1; s >= base; s-- {
		i.releaseBox(i.stack[s])
	}
	i.stack[base] = exc
	i.sp = base + 1
	f.ip = h.Catch
	i.fr = f
}

// discard releases an unwound frame's activation: its function reference and any
// in-flight coroutine handle. Operand slots are released by land in one sweep.
func (i *Interpreter) discard(f *frame) {
	if f.release {
		i.release(f.ref)
	}
	if f.coro != 0 {
		i.release(f.coro)
	}
	f.code = nil
	f.upvals = nil
	f.coro = 0
}

// wrap allocates a heap Error wrapping a Go failure so a recovered trap or
// host error becomes a catchable guest value while staying errors.Is/As aware.
func (i *Interpreter) wrap(err error) types.Boxed {
	return types.BoxRef(i.keep(types.WrapError(err)))
}

// uncaught renders an escaped throw as a Go error. A thrown Error surfaces
// directly (preserving its Unwrap chain); any other value is wrapped with its
// rendered form under ErrUncaughtException.
func (i *Interpreter) uncaught(exc types.Boxed) error {
	if exc.Kind() == types.KindRef {
		v := i.heap[exc.Ref()]
		if e, ok := v.(*types.Error); ok {
			return e
		}
		return fmt.Errorf("%w: %s", ErrUncaughtException, v.String())
	}
	return fmt.Errorf("%w: %s", ErrUncaughtException, types.Unbox(exc).String())
}

// message derives an Error message from a payload: a string's contents, else the
// value's rendered form.
func (i *Interpreter) message(v types.Boxed) string {
	if v.Kind() == types.KindRef {
		if s, ok := i.heap[v.Ref()].(types.String); ok {
			return string(s)
		}
		return i.heap[v.Ref()].String()
	}
	return types.Unbox(v).String()
}

func (i *Interpreter) framesInfo() []FrameInfo {
	if i.fp <= 0 {
		return nil
	}
	frames := make([]FrameInfo, 0, i.fp)
	for idx := i.fp - 1; idx >= 0; idx-- {
		f := i.frames[idx]
		frames = append(frames, FrameInfo{Func: f.addr, IP: f.ip})
	}
	return frames
}
