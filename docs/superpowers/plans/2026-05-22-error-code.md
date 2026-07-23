# Error Code Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `xerr`, a lightweight Go error code library with category classification, HTTP status mapping, and error chain support.

**Architecture:** Three files — `code.go` defines the `Code` struct and `Category` enum; `error.go` defines the `Error` struct implementing `error` with Unwrap/Is/As; `xcodes/codes.go` provides predefined sentinel error codes. No external dependencies.

**Tech Stack:** Go 1.21+, standard library only.

**Spec:** `docs/superpowers/specs/2026-05-22-error-code-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `go.mod` | Module definition |
| `xerr/code.go` | `Category` enum, `Code` struct, `New()` constructor, getters, `String()` |
| `xerr/code_test.go` | Tests for `Code` creation, getters, `String()` |
| `xerr/error.go` | `Error` struct, `Error()`, `Unwrap()`, `Is()`, getters, `New()`/`Wrap()`/`Wrapf()` methods on Code |
| `xerr/error_test.go` | Tests for error creation, error chain, `errors.Is`, `errors.As`, Wrap/Wrapf |
| `xerr/xcodes/codes.go` | Predefined sentinel error codes |
| `xerr/xcodes/codes_test.go` | Tests for predefined codes correctness |

---

### Task 1: Initialize Go module

**Files:**
- Create: `go.mod`

- [ ] **Step 1: Initialize module**

Run: `cd /Users/moss/code/go-common && go mod init github.com/moss/go-common`
Expected: `go.mod` created

---

### Task 2: Implement Category and Code (TDD)

**Files:**
- Create: `xerr/code.go`
- Create: `xerr/code_test.go`

- [ ] **Step 1: Write failing tests for Category and Code**

Create `xerr/code_test.go`:

```go
package xerr

import (
	"testing"
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
			if int(tt.category) != tt.want {
				t.Errorf("Category %s = %d, want %d", tt.name, tt.category, tt.want)
			}
		})
	}
}

func TestNewCode(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")

	if code.Reason() != "USER_NOT_FOUND" {
		t.Errorf("Reason() = %q, want %q", code.Reason(), "USER_NOT_FOUND")
	}
	if code.Category() != CategoryNotFound {
		t.Errorf("Category() = %d, want %d", code.Category(), CategoryNotFound)
	}
	if code.HTTPCode() != 404 {
		t.Errorf("HTTPCode() = %d, want %d", code.HTTPCode(), 404)
	}
	if code.Message() != "user not found" {
		t.Errorf("Message() = %q, want %q", code.Message(), "user not found")
	}
}

func TestCodeString(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	want := "USER_NOT_FOUND: user not found"
	if code.String() != want {
		t.Errorf("String() = %q, want %q", code.String(), want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moss/code/go-common && go test ./xerr/ -v -run "TestCategory|TestNewCode|TestCodeString"`
Expected: FAIL — `undefined: Category`, `undefined: New`

- [ ] **Step 3: Write Code implementation**

Create `xerr/code.go`:

```go
package xerr

// Category represents the classification of an error.
type Category int

const (
	CategorySuccess          Category = iota // 成功
	CategoryBadRequest                       // 参数错误
	CategoryUnauthorized                     // 未认证
	CategoryForbidden                        // 无权限
	CategoryNotFound                         // 资源不存在
	CategoryConflict                         // 冲突
	CategoryTooManyRequests                  // 限流
	CategoryInternal                         // 内部错误
	CategoryServiceUnavailable               // 服务不可用
)

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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moss/code/go-common && go test ./xerr/ -v -run "TestCategory|TestNewCode|TestCodeString"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add xerr/code.go xerr/code_test.go go.mod
git commit -m "feat(xerr): add Category enum and Code struct"
```

---

### Task 3: Implement Error with error chain (TDD)

**Files:**
- Create: `xerr/error.go`
- Create: `xerr/error_test.go`

- [ ] **Step 1: Write failing tests for Error**

Create `xerr/error_test.go`:

```go
package xerr

import (
	"errors"
	"fmt"
	"testing"
)

func TestCode_NewError(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default message")

	// Without custom message
	err := code.New()
	if err.Error() != "TEST_ERROR: default message" {
		t.Errorf("Error() = %q, want %q", err.Error(), "TEST_ERROR: default message")
	}

	// With custom message
	err2 := code.New("custom message")
	if err2.Error() != "TEST_ERROR: custom message" {
		t.Errorf("Error() = %q, want %q", err2.Error(), "TEST_ERROR: custom message")
	}
}

func TestCode_Wrap(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")

	err := code.Wrap(cause)

	// Error message includes cause
	want := "USER_NOT_FOUND: user not found: sql: no rows"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	// Unwrap returns cause
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}
}

func TestCode_Wrapf(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")

	err := code.Wrapf(cause, "user %s not found", "alice")

	want := "USER_NOT_FOUND: user alice not found: sql: no rows"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestError_Accessors(t *testing.T) {
	code := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	err := code.New()

	if err.Code().Reason() != "USER_NOT_FOUND" {
		t.Errorf("Code().Reason() = %q, want %q", err.Code().Reason(), "USER_NOT_FOUND")
	}
	if err.Category() != CategoryNotFound {
		t.Errorf("Category() = %d, want %d", err.Category(), CategoryNotFound)
	}
	if err.HTTPCode() != 404 {
		t.Errorf("HTTPCode() = %d, want %d", err.HTTPCode(), 404)
	}
	if err.Cause() != nil {
		t.Errorf("Cause() = %v, want nil", err.Cause())
	}
}

func TestError_Is(t *testing.T) {
	sentinel := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")

	err := sentinel.Wrap(cause)

	// Is matches by reason, regardless of instance
	if !errors.Is(err, sentinel.New()) {
		t.Error("errors.Is(err, sentinel.New()) = false, want true")
	}

	// Is also matches unwrapped cause
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}

	// Does not match different reason
	other := New("ORDER_NOT_FOUND", CategoryNotFound, 404, "order not found")
	if errors.Is(err, other.New()) {
		t.Error("errors.Is(err, other.New()) = true, want false")
	}
}

func TestError_As(t *testing.T) {
	sentinel := New("USER_NOT_FOUND", CategoryNotFound, 404, "user not found")
	cause := errors.New("sql: no rows")

	err := sentinel.Wrap(cause)

	var xerrErr *Error
	if !errors.As(err, &xerrErr) {
		t.Fatal("errors.As(err, &xerrErr) = false, want true")
	}
	if xerrErr.Code().Reason() != "USER_NOT_FOUND" {
		t.Errorf("Reason = %q, want %q", xerrErr.Code().Reason(), "USER_NOT_FOUND")
	}
}

func TestError_NilCause(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default message")
	err := code.New()

	if err.Cause() != nil {
		t.Error("Cause() should be nil for New()")
	}
}

func TestError_NewWithoutMessage(t *testing.T) {
	code := New("TEST_ERROR", CategoryBadRequest, 400, "default message")
	err := code.New()

	// No cause, so format is "reason: message"
	want := "TEST_ERROR: default message"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moss/code/go-common && go test ./xerr/ -v -run "TestCode_New|TestCode_Wrap|TestError_"`
Expected: FAIL — `Code.New`, `Code.Wrap`, `Code.Wrapf` undefined, `Error` type undefined

- [ ] **Step 3: Write Error implementation**

Create `xerr/error.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moss/code/go-common && go test ./xerr/ -v -run "TestCode_New|TestCode_Wrap|TestError_"`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `cd /Users/moss/code/go-common && go test ./xerr/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add xerr/error.go xerr/error_test.go
git commit -m "feat(xerr): add Error struct with error chain support"
```

---

### Task 4: Implement predefined error codes (TDD)

**Files:**
- Create: `xerr/xcodes/codes.go`
- Create: `xerr/xcodes/codes_test.go`

- [ ] **Step 1: Write failing tests for predefined codes**

Create `xerr/xcodes/codes_test.go`:

```go
package xcodes

import (
	"testing"

	"github.com/moss/go-common/xerr"
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
			if tt.code.Reason() != tt.reason {
				t.Errorf("Reason() = %q, want %q", tt.code.Reason(), tt.reason)
			}
			if tt.code.Category() != tt.category {
				t.Errorf("Category() = %d, want %d", tt.code.Category(), tt.category)
			}
			if tt.code.HTTPCode() != tt.httpCode {
				t.Errorf("HTTPCode() = %d, want %d", tt.code.HTTPCode(), tt.httpCode)
			}
			if tt.code.Message() != tt.message {
				t.Errorf("Message() = %q, want %q", tt.code.Message(), tt.message)
			}
		})
	}
}

func TestPredefinedCodesCanCreateErrors(t *testing.T) {
	err := ErrNotFound.New("resource missing")
	if err.Code().Reason() != "NOT_FOUND" {
		t.Errorf("Reason = %q, want %q", err.Code().Reason(), "NOT_FOUND")
	}
	if err.HTTPCode() != 404 {
		t.Errorf("HTTPCode = %d, want %d", err.HTTPCode(), 404)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moss/code/go-common && go test ./xerr/xcodes/ -v`
Expected: FAIL — `undefined: ErrBadRequest`, etc.

- [ ] **Step 3: Write predefined codes**

Create `xerr/xcodes/codes.go`:

```go
package xcodes

import "github.com/moss/go-common/xerr"

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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moss/code/go-common && go test ./xerr/xcodes/ -v`
Expected: PASS

- [ ] **Step 5: Run all tests across all packages**

Run: `cd /Users/moss/code/go-common && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add xerr/xcodes/codes.go xerr/xcodes/codes_test.go
git commit -m "feat(xerr/xcodes): add predefined common error codes"
```
