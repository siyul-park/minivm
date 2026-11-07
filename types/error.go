package types

import (
	"fmt"
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

// ErrorCode is the numeric code of an Error.
type ErrorCode int32

type errorType struct{}

const (
	// ErrorCodeNone is the code of an error with no code.
	ErrorCodeNone ErrorCode = 0
	// ErrorCodeUserBase is the first code available to guest programs.
	ErrorCodeUserBase ErrorCode = 1 << 20
)

var typeError Type = errorType{}

var (
	_ Traceable = (*Error)(nil)
	_ Type      = errorType{}
	_ error     = (*Error)(nil)
)

// TypeError returns the VM error type.
func TypeError() Type {
	return typeError
}

// WrapError adapts a Go error into an exception value with a numeric code. Its
// message is err.Error() and the original is retained for Unwrap. It returns nil
// for a nil error.
func WrapError(code ErrorCode, err error) *Error {
	if err == nil {
		return nil
	}
	e := NewError(code, err.Error(), BoxedNull)
	e.wrap(err)
	return e
}

// NewError builds an exception with a numeric code, message, and payload. Pass
// BoxedNull as value when there is no payload.
func NewError(code ErrorCode, message string, value Boxed) *Error {
	return &Error{code: code, message: message, value: value}
}

// Value returns the payload, BoxedNull when absent.
func (e *Error) Value() Boxed {
	return e.value
}

// Code returns the numeric code.
func (e *Error) Code() ErrorCode {
	return e.code
}

func (e *Error) Kind() Kind {
	return KindRef
}

func (e *Error) Type() Type {
	return TypeError()
}

func (e *Error) Refs(dst []Ref) []Ref {
	if e.value.Kind() == KindRef {
		dst = append(dst, Ref(e.value.Ref()))
	}
	return dst
}

// Error returns the underlying error.
func (e *Error) Error() string {
	return e.message
}

// Unwrap returns the wrapped error.
func (e *Error) Unwrap() error {
	return e.cause
}

func (e *Error) String() string {
	return fmt.Sprintf("error(%q)", e.message)
}

func (errorType) Kind() Kind {
	return KindRef
}

func (errorType) Cast(other Type) bool {
	return other == TypeError()
}

func (errorType) Equals(other Type) bool {
	return other == TypeError()
}

func (errorType) String() string {
	return "error"
}

func (e *Error) wrap(err error) {
	e.cause = err
}
