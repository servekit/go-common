# Error Code Library Design

## Overview

A lightweight, general-purpose Go error code library (`xerr`) for business error handling. Provides structured error codes with category classification, HTTP status mapping, and Go error chain support. i18n is intentionally left to the application layer.

## Requirements

- String constant error codes (e.g., `USER_NOT_FOUND`)
- Fixed default message per error code
- Error category classification (bad request, unauthorized, not found, etc.)
- HTTP status code mapping
- Go error chain support (Unwrap, Is, As)
- Predefined common error codes + extensible for custom codes
- No i18n — error code `reason` serves as i18n key for application layer

## Core Types

### Category

```go
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
```

### Code

`Code` is a struct defining a business error code. Immutable after creation.

```go
type Code struct {
    reason   string    // 错误码标识，如 "USER_NOT_FOUND"
    category Category  // 错误分类
    httpCode int       // 对应的 HTTP status code
    message  string    // 默认消息（英文）
}
```

Constructors:

```go
func New(reason string, category Category, httpCode int, message string) Code
func (c Code) Reason() string
func (c Code) Category() Category
func (c Code) HTTPCode() int
func (c Code) Message() string
func (c Code) String() string
```

### Error

`Error` implements the `error` interface with error chain support.

```go
type Error struct {
    code    Code
    message string  // 实际消息（可覆盖默认值）
    cause   error   // 底层错误
}
```

Methods:

```go
func (e *Error) Error() string        // "reason: message" or "reason: message: cause.Error()"
func (e *Error) Unwrap() error        // returns cause, enables errors.Is/As chain walking
func (e *Error) Is(target error) bool // matches by Code.Reason
func (e *Error) Code() Code
func (e *Error) Category() Category
func (e *Error) HTTPCode() int
func (e *Error) Cause() error
```

Constructors:

```go
// From a Code, create a new Error
func (c Code) New(msg ...string) *Error

// Wrap wraps an existing error with the Code
func (c Code) Wrap(err error) *Error

// Wrapf wraps with formatted message
func (c Code) Wrapf(err error, format string, args ...any) *Error
```

### Sentinel errors package (`xerr/xcodes`)

Predefined common error codes live in a sub-package to keep the root clean.

```go
package xcodes

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

## Usage Examples

### Basic usage

```go
import "github.com/example/go-common/xerr"
import "github.com/example/go-common/xerr/xcodes"

// Return a predefined error
return xcodes.ErrNotFound.New()

// Return with custom message override
return xcodes.ErrNotFound.New("user not found")
```

### Custom error codes (business side)

```go
var (
    ErrUserNotFound  = xerr.New("USER_NOT_FOUND", xerr.CategoryNotFound, 404, "user not found")
    ErrOrderExpired  = xerr.New("ORDER_EXPIRED", xerr.CategoryConflict, 409, "order has expired")
    ErrInsufficientBalance = xerr.New("INSUFFICIENT_BALANCE", xerr.CategoryBadRequest, 400, "insufficient balance")
)

// Usage
return ErrUserNotFound.Wrap(dbErr)
```

### Error chain

```go
// errors.Is — matches by reason
if errors.Is(err, ErrUserNotFound) {
    // handle
}

// errors.As — extract details
var xerrErr *xerr.Error
if errors.As(err, &xerrErr) {
    fmt.Println(xerrErr.Code().Reason())   // "USER_NOT_FOUND"
    fmt.Println(xerrErr.HTTPCode())        // 404
}

// errors.Is with wrapped errors
dbErr := sql.ErrNoRows
err := ErrUserNotFound.Wrap(dbErr)
errors.Is(err, ErrUserNotFound)  // true
errors.Is(err, sql.ErrNoRows)    // true
```

### Application-layer i18n integration

```go
// Application middleware — not part of the library
func ErrorHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()
        // ... error handling logic
        var xerrErr *xerr.Error
        if errors.As(lastErr, &xerrErr) {
            reason := xerrErr.Code().Reason()
            msg := i18n.T(c.GetHeader("Accept-Language"), reason)  // app's own i18n
            c.JSON(xerrErr.HTTPCode(), gin.H{"reason": reason, "message": msg})
        }
    }
}
```

## Package Structure

```
xerr/
├── code.go         // Code struct, Category enum, New()
├── error.go        // Error struct, Wrap/Is/As/Unwrap
├── xcodes/         // Predefined common error codes
│   └── codes.go
├── code_test.go
├── error_test.go
└── xcodes/codes_test.go
```

## Design Decisions

1. **Code is a struct, not an interface** — keeps it simple, no need for polymorphism at the code level.
2. **Reason-based Is() matching** — `errors.Is(err, ErrUserNotFound)` matches by reason string, so sentinel comparison works regardless of instance.
3. **No i18n in library** — reason string doubles as i18n key; translation is the application's responsibility.
4. **Predefined codes in sub-package** — avoids polluting the root package namespace; business projects define their own codes directly.
5. **No protobuf dependency** — this is a pure Go library, not tied to gRPC or any RPC framework.
