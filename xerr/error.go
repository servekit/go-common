package xerr

import "fmt"

// Error implements the error interface with error chain support.
type Error struct {
	code    Code
	message string
	cause   error
}

// New creates a new Error from this Code.
// If msg is provided, it overrides the Code's default message.
func (c Code) New(msg ...string) *Error {
	m := c.message
	if len(msg) > 0 {
		m = msg[0]
	}
	return &Error{
		code:    c,
		message: m,
	}
}

// Wrap wraps an existing error with this Code.
func (c Code) Wrap(err error) *Error {
	return &Error{
		code:    c,
		message: c.message,
		cause:   err,
	}
}

// Wrapf wraps an existing error with this Code and a formatted message.
func (c Code) Wrapf(err error, format string, args ...any) *Error {
	return &Error{
		code:    c,
		message: fmt.Sprintf(format, args...),
		cause:   err,
	}
}

// Error returns the error message string.
// Format: "reason: message" or "reason: message: cause" when wrapped.
func (e *Error) Error() string {
	if e.cause != nil {
		return e.code.reason + ": " + e.message + ": " + e.cause.Error()
	}
	return e.code.reason + ": " + e.message
}

// Unwrap returns the underlying cause error.
func (e *Error) Unwrap() error {
	return e.cause
}

// Is reports whether this error matches the target error.
// Matches by Code.Reason for *Error targets.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.code.reason == t.code.reason
}

// Code returns the error code.
func (e *Error) Code() Code { return e.code }

// Category returns the error category.
func (e *Error) Category() Category { return e.code.category }

// HTTPCode returns the associated HTTP status code.
func (e *Error) HTTPCode() int { return e.code.httpCode }

// Cause returns the underlying cause error, or nil.
func (e *Error) Cause() error { return e.cause }
