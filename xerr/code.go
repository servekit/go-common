// Package xerr provides structured error types with error codes, categories,
// and HTTP status mapping for business-level error handling.
package xerr

// Category represents the classification of an error.
type Category int

// Code defines a business error code.
type Code struct {
	reason   string
	category Category
	httpCode int
	message  string
}

// New creates a new Code with the given reason, category, HTTP status code, and default message.
func New(reason string, category Category, httpCode int, message string) Code {
	return Code{
		reason:   reason,
		category: category,
		httpCode: httpCode,
		message:  message,
	}
}

const (
	// CategorySuccess indicates a successful result.
	CategorySuccess Category = iota
	// CategoryBadRequest indicates a bad request error.
	CategoryBadRequest
	// CategoryUnauthorized indicates an authentication error.
	CategoryUnauthorized
	// CategoryForbidden indicates an authorization error.
	CategoryForbidden
	// CategoryNotFound indicates a resource not found error.
	CategoryNotFound
	// CategoryConflict indicates a conflict error.
	CategoryConflict
	// CategoryTooManyRequests indicates a rate limit error.
	CategoryTooManyRequests
	// CategoryInternal indicates an internal server error.
	CategoryInternal
	// CategoryServiceUnavailable indicates a service unavailable error.
	CategoryServiceUnavailable
)

// Reason returns the error code identifier (e.g., "USER_NOT_FOUND").
func (c Code) Reason() string { return c.reason }

// Category returns the error category.
func (c Code) Category() Category { return c.category }

// HTTPCode returns the associated HTTP status code.
func (c Code) HTTPCode() int { return c.httpCode }

// Message returns the default error message.
func (c Code) Message() string { return c.message }

// String returns "reason: message".
func (c Code) String() string {
	return c.reason + ": " + c.message
}
