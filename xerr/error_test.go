package xerr

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCode_NewError(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default message")

	err := code.New()
	require.Equal(t, "TEST_ERROR: default message", err.Error())

	err2 := code.New("custom message")
	require.Equal(t, "TEST_ERROR: custom message", err2.Error())
}

func TestCode_Wrap(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")

	err := code.Wrap(cause)
	require.Equal(t, "USER_NOT_FOUND: user not found: sql: no rows", err.Error())
	require.True(t, errors.Is(err, cause))
}

func TestCode_Wrapf(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")

	err := code.Wrapf(cause, "user %s not found", "alice")
	require.Equal(t, "USER_NOT_FOUND: user alice not found: sql: no rows", err.Error())
}

func TestError_Accessors(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	err := code.New()
	require.Equal(t, "USER_NOT_FOUND", err.Code().Reason())
	require.Equal(t, CategoryNotFound, err.Category())
	require.Equal(t, 404, err.HTTPCode())
	require.Nil(t, err.Cause())
}

func TestError_Is(t *testing.T) {
	sentinel := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")
	err := sentinel.Wrap(cause)

	require.True(t, errors.Is(err, sentinel.New()))
	require.True(t, errors.Is(err, cause))

	other := New("ORDER_NOT_FOUND", CategoryNotFound, 404, "order not found")
	require.False(t, errors.Is(err, other.New()))
}

func TestError_As(t *testing.T) {
	sentinel := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")
	err := sentinel.Wrap(cause)

	var xerrErr *Error
	require.True(t, errors.As(err, &xerrErr))
	require.Equal(t, "USER_NOT_FOUND", xerrErr.Code().Reason())
}

func TestError_NilCause(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default message")
	err := code.New()
	require.Nil(t, err.Cause())
}

func TestError_NewWithoutMessage(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default message")
	err := code.New()
	require.Equal(t, "TEST_ERROR: default message", err.Error())
}

func TestCode_New_multipleMessages(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default")
	// Only the first message is used; extras are ignored.
	err := code.New("first", "second", "third")
	require.Equal(t, "TEST_ERROR: first", err.Error())
}

func TestCode_Wrap_nil(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default")
	err := code.Wrap(nil)
	require.Equal(t, "TEST_ERROR: default", err.Error())
	require.Nil(t, err.Cause())
}

func TestError_Unwrap(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default")
	cause := errors.New("root cause")

	err := code.Wrap(cause)
	require.Equal(t, cause, err.Unwrap())

	errNoCause := code.New()
	require.Nil(t, errNoCause.Unwrap())
}

func TestError_Is_nonErrorTarget(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default")
	err := code.Wrap(errors.New("cause"))

	// errors.Is with a non-*Error target should return false.
	require.False(t, errors.Is(err, errors.New("TEST_ERROR")))
}
