// Package xcodes provides predefined error codes for common HTTP error scenarios.
package xcodes

import "github.com/servekit/go-common/xerr"

// Standard HTTP error codes.
var (
	ErrBadRequest         = xerr.New("BAD_REQUEST", xerr.CategoryBadRequest, 400, "bad request")
	ErrUnauthorized       = xerr.New("UNAUTHORIZED", xerr.CategoryUnauthorized, 401, "unauthorized")
	ErrForbidden          = xerr.New("FORBIDDEN", xerr.CategoryForbidden, 403, "forbidden")
	ErrNotFound           = xerr.New("NOT_FOUND", xerr.CategoryNotFound, 404, "not found")
	ErrConflict           = xerr.New("CONFLICT", xerr.CategoryConflict, 409, "conflict")
	ErrTooManyRequests    = xerr.New("TOO_MANY_REQUESTS", xerr.CategoryTooManyRequests, 429, "too many requests")
	ErrInternal           = xerr.New("INTERNAL_ERROR", xerr.CategoryInternal, 500, "internal server error")
	ErrServiceUnavailable = xerr.New("SERVICE_UNAVAILABLE", xerr.CategoryServiceUnavailable, 503, "service unavailable")
)
