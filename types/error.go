package types

import (
	"fmt"
	"unsafe"
)

// Error is a guest exception value. It carries a human-readable message, an
// optional numeric code, an optional payload (any boxed value, traced when it is
// a ref), and an optional wrapped Go error so an uncaught throw or a converted
// host/runtime failure stays compatible with errors.Is / errors.As through
// Unwrap.
type Error struct {
	cause   error
	value   Boxed
	code    ErrorCode
	message string
}

type ErrorCode int32

type errorType struct{}

// ErrorValueOffset exposes Error.value's layout to the ARM64 JIT heap-read
// fast path.
const ErrorValueOffset = int(unsafe.Offsetof(Error{}.value))

// ErrorCodeOffset exposes Error.code's layout for future native fast paths.
const ErrorCodeOffset = int(unsafe.Offsetof(Error{}.code))

const (
	ErrorCodeNone     ErrorCode = 0
	ErrorCodeUserBase ErrorCode = 1 << 20
)

var TypeError = errorType{}

var (
	_ Traceable = (*Error)(nil)
	_ Type      = errorType{}
	_ error     = (*Error)(nil)
)

// NewError builds an exception with a numeric code, message, and payload. Pass
// BoxedNull as value when there is no payload.
func NewError(code ErrorCode, message string, value Boxed) *Error {
	return &Error{code: code, message: message, value: value}
}

// WrapError adapts a Go error into an exception value with a numeric code. Its
// message is err.Error() and the original is retained for Unwrap. It returns nil
// for a nil error.
func WrapError(code ErrorCode, err error) *Error {
	if err == nil {
		return nil
	}
	return &Error{cause: err, code: code, message: err.Error(), value: BoxedNull}
}

func (e *Error) Error() string {
	return e.message
}

func (e *Error) Unwrap() error {
	return e.cause
}

func (e *Error) Value() Boxed {
	return e.value
}

func (e *Error) Code() ErrorCode {
	return e.code
}

func (e *Error) Kind() Kind {
	return KindRef
}

func (e *Error) Type() Type {
	return TypeError
}

func (e *Error) String() string {
	return fmt.Sprintf("error(%q)", e.message)
}

func (e *Error) Refs(dst []Ref) []Ref {
	if e.value.Kind() == KindRef {
		dst = append(dst, Ref(e.value.Ref()))
	}
	return dst
}

func (errorType) Kind() Kind {
	return KindRef
}

func (errorType) String() string {
	return "error"
}

func (errorType) Cast(other Type) bool {
	return other == TypeError
}

func (errorType) Equals(other Type) bool {
	return other == TypeError
}
