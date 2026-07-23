package xerr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCategoryValues(t *testing.T) {
	tests := []struct {
		name     string
		category Category
		want     int
	}{
		{"Success", CategorySuccess, 0},
		{"BadRequest", CategoryBadRequest, 1},
		{"Unauthorized", CategoryUnauthorized, 2},
		{"Forbidden", CategoryForbidden, 3},
		{"NotFound", CategoryNotFound, 4},
		{"Conflict", CategoryConflict, 5},
		{"TooManyRequests", CategoryTooManyRequests, 6},
		{"Internal", CategoryInternal, 7},
		{"ServiceUnavailable", CategoryServiceUnavailable, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, int(tt.category))
		})
	}
}

func TestNewCode(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	require.Equal(t, "USER_NOT_FOUND", code.Reason())
	require.Equal(t, CategoryNotFound, code.Category())
	require.Equal(t, 404, code.HTTPCode())
	require.Equal(t, "user not found", code.Message())
}

func TestCodeString(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	require.Equal(t, "USER_NOT_FOUND: user not found", code.String())
}

func TestCode_ZeroValue(t *testing.T) {
	var code Code
	require.Equal(t, "", code.Reason())
	require.Equal(t, Category(0), code.Category())
	require.Equal(t, 0, code.HTTPCode())
	require.Equal(t, "", code.Message())
	require.Equal(t, ": ", code.String())
}

func TestNewCode_emptyStrings(t *testing.T) {
	code := New("", CategorySuccess, 200, "")
	require.Equal(t, "", code.Reason())
	require.Equal(t, "", code.Message())
	require.Equal(t, ": ", code.String())
}
