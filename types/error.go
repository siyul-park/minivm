package types

import "fmt"

// Error is a guest exception value. It carries a human-readable message, an
// optional payload (any boxed value, traced when it is a ref), and an optional
// wrapped Go error so an uncaught throw or a converted host/runtime failure
// stays compatible with errors.Is / errors.As through Unwrap.
type Error struct {
	cause   error
	value   Boxed
	message string
}

type errorType struct{}

var TypeError = errorType{}

var (
	_ Traceable = (*Error)(nil)
	_ Type      = errorType{}
	_ error     = (*Error)(nil)
)

// NewError builds an exception with the given message and payload. Pass
// BoxedNull as value when there is no payload.
func NewError(message string, value Boxed) *Error {
	return &Error{message: message, value: value}
}

// WrapError adapts a Go error into an exception value: its message is
// err.Error() and the original is retained for Unwrap. It returns nil for a nil
// error.
func WrapError(err error) *Error {
	if err == nil {
		return nil
	}
	return &Error{cause: err, message: err.Error()}
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

func (e *Error) Kind() Kind {
	return KindRef
}

func (e *Error) Type() Type {
	return TypeError
}

func (e *Error) String() string {
	return fmt.Sprintf("error(%q)", e.message)
}

func (e *Error) Refs() []Ref {
	if e.value.Kind() == KindRef {
		return []Ref{Ref(e.value.Ref())}
	}
	return nil
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
