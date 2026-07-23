package xcodes

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/servekit/go-common/xerr"
)

func TestPredefinedCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     xerr.Code
		reason   string
		category xerr.Category
		httpCode int
		message  string
	}{
		{
			name: "BadRequest", code: ErrBadRequest,
			reason: "BAD_REQUEST", category: xerr.CategoryBadRequest, httpCode: 400, message: "bad request",
		},
		{
			name: "Unauthorized", code: ErrUnauthorized,
			reason: "UNAUTHORIZED", category: xerr.CategoryUnauthorized, httpCode: 401, message: "unauthorized",
		},
		{
			name: "Forbidden", code: ErrForbidden,
			reason: "FORBIDDEN", category: xerr.CategoryForbidden, httpCode: 403, message: "forbidden",
		},
		{
			name: "NotFound", code: ErrNotFound,
			reason: "NOT_FOUND", category: xerr.CategoryNotFound, httpCode: 404, message: "not found",
		},
		{
			name: "Conflict", code: ErrConflict,
			reason: "CONFLICT", category: xerr.CategoryConflict, httpCode: 409, message: "conflict",
		},
		{
			name: "TooManyRequests", code: ErrTooManyRequests,
			reason: "TOO_MANY_REQUESTS", category: xerr.CategoryTooManyRequests, httpCode: 429, message: "too many requests",
		},
		{
			name: "Internal", code: ErrInternal,
			reason: "INTERNAL_ERROR", category: xerr.CategoryInternal, httpCode: 500, message: "internal server error",
		},
		{
			name: "ServiceUnavailable", code: ErrServiceUnavailable,
			reason: "SERVICE_UNAVAILABLE", category: xerr.CategoryServiceUnavailable, httpCode: 503, message: "service unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.reason, tt.code.Reason())
			require.Equal(t, tt.category, tt.code.Category())
			require.Equal(t, tt.httpCode, tt.code.HTTPCode())
			require.Equal(t, tt.message, tt.code.Message())
		})
	}
}

func TestPredefinedCodesCanCreateErrors(t *testing.T) {
	err := ErrNotFound.New("resource missing")
	require.Equal(t, "NOT_FOUND", err.Code().Reason())
	require.Equal(t, 404, err.HTTPCode())
}

func TestPredefinedCodes_Wrap(t *testing.T) {
	cause := errors.New("db error")
	err := ErrInternal.Wrap(cause)
	require.Equal(t, "INTERNAL_ERROR", err.Code().Reason())
	require.Equal(t, 500, err.HTTPCode())
	require.True(t, errors.Is(err, cause))
}

func TestPredefinedCodes_Wrapf(t *testing.T) {
	cause := errors.New("db error")
	err := ErrBadRequest.Wrapf(cause, "invalid field %s", "email")
	require.Contains(t, err.Error(), "invalid field email")
	require.Contains(t, err.Error(), "db error")
}

func TestPredefinedCodes_errorsIs(t *testing.T) {
	err := ErrNotFound.Wrap(errors.New("sql: no rows"))
	require.True(t, errors.Is(err, ErrNotFound.New()))
	require.False(t, errors.Is(err, ErrBadRequest.New()))
}
