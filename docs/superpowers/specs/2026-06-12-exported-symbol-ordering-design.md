# Exported symbol ordering in Go files

Date: 2026-06-12

## Background

`go-common` is a foundational library consumed by other services. When a user
opens any source file to understand its public API, exported symbols (types,
functions, methods, constants, variables) should be visible at first glance
without scrolling past internal helpers.

A scan of the repo found several files where exported and unexported symbols
are interleaved:

- `ratelimit/ratelimit.go`: exported methods `Stats`, `Reset`, `ResetPurpose`
  sit at the bottom of the file, mixed with unexported helpers `key`,
  `allScopedRules`, `scopedRulesFor`.
- `captcha/captcha.go`: the unexported constant `defaultTTL` is wedged between
  exported `WithRedisClient` and exported type `Captcha`; the unexported method
  `ttlForPurpose` precedes the exported methods `Generate` / `Verify`.
- `captcha/store.go`: unexported method `key` sits before the exported methods
  `Set` and `Verify`.
- `captcha/generator.go`: unexported `digits` / `alphaLower` / etc. sit between
  the exported `Format*` vars and the exported type `CodeGenerator`.
- `lifecycle/manager.go`: unexported type `entry` sits between exported type
  `Manager` and exported type `Option`.

There is no rule in `CLAUDE.md` today that constrains intra-file ordering, so
drift has accumulated.

## Goal

1. Add a project rule to `CLAUDE.md` that constrains intra-file symbol
   ordering on the "exported first" principle.
2. Reorder existing `.go` files (non-test) to satisfy the rule. Pure
   whitespace / declaration-move changes — no logic changes.

## Non-Goals

- No logic, behavior, or signature changes. This is a layout-only refactor.
- No new abstractions, no file splits, no renames.
- The rule does not apply to `*_test.go` files. Tests are read by maintainers
  in a different mental mode (setup → exercise → assert) and forcing the same
  ordering there hurts readability.
- No reordering within the exported group or within the unexported group. The
  author's original semantic grouping (e.g. an `Option` type immediately
  followed by its `With*` helpers) is preserved.
- No automated formatter is introduced. `gofmt` / `gofumpt` do not understand
  "exported first"; the rule is enforced by code review.

## Design

### The rule

Inside each non-test `.go` file, top-level declarations are organized into two
contiguous blocks:

1. **Exported block** (top of file), preserving the author's relative order:
   - exported types (`struct`, `interface`, named function types)
   - exported constants and variables
   - exported constructors (`New*`)
   - exported functions (including `With*` Option helpers)
   - exported methods (each method declaration still must immediately follow
     its receiver type definition — this is enforced by Go readability
     convention, not the compiler)
2. **Unexported block** (bottom of file), preserving the author's relative
   order:
   - unexported types / constants / variables
   - unexported functions
   - unexported methods

### Constraints

- **Method placement**: methods must stay adjacent to their receiver type. This
  means a single file containing one exported type and its methods naturally
  produces a layout where:
  - the exported type is in the exported block
  - the exported methods of that type immediately follow the type
  - the unexported methods of that type sit at the bottom of the file
  - any other unexported helpers (functions, types) also sit at the bottom
- **Method visibility follows the receiver type.** A method on an unexported
  type is treated as unexported for ordering purposes, even if its name is
  capitalized (e.g. `func (starterOnly) Stop() error`). Such methods are not
  reachable from outside the package because the receiver type itself is not
  exported, so they belong with the unexported block alongside their receiver
  type. Conversely, methods on an exported type follow the method name's own
  capitalization.
- **Intra-block order is not rewritten.** If the author wrote
  `Config → Option → New → WithFoo → WithBar`, that order is kept. The rule
  only separates exported from unexported.
- **Test files** (`*_test.go`) are exempt.
- **Generated files** (`*_generated.go`, `*.pb.go`, anything under
  `internal/generated/`) are exempt.

### Motivation

When a new consumer of the library opens `ratelimit.go` to learn the public
API, the file's first screenful should show `Rule`, `Stat`, `Config`,
`Limiter`, `RedisLimiter`, `NewRedisLimiter`, `Allow`, `Stats`, `Reset`,
`ResetPurpose`. The Lua script plumbing, `key()` formatter, and
`scopedRulesFor()` lookup are internal mechanics that distract from the API
surface and belong below.

## Affected files

Scan run on 2026-06-12 found 16 non-test files violating the rule. The
scanner groups a method with its receiver type when deciding visibility — so
a method named `Stop` on an unexported type like `starterOnly` is treated as
unexported and does not surface as a violation.

| File | Violation summary |
| --- | --- |
| `captcha/captcha.go` | `generateConfig` (unexported type) and `defaultTTL` (unexported const) sit between exported declarations; `ttlForPurpose` (unexported method) precedes exported `Generate`/`Verify` |
| `captcha/generator.go` | Unexported vars (`digits`, `alphaLower`, etc.) sit between exported `Format*` vars and exported type `CodeGenerator` |
| `captcha/store.go` | Unexported method `key` precedes exported methods `Set`/`Verify` |
| `cronx/cronx.go` | Unexported type `cronOptions` sits between exported `Option` and exported `WithCronOption` |
| `dbx/dbx.go` | Unexported consts `defaultLogLevel`/`defaultSlowThreshold` precede exported `Config`/`New`/`AutoMigrate` |
| `grpcx/auth.go` | Unexported type `userIDKeyType` precedes exported `UserIDKey`/`GetUserIDFromCtx`/`BearerTokenFromCtx` |
| `grpcx/interceptor.go` | Unexported `categoryToGRPC` var precedes exported `ErrorInterceptor` |
| `lifecycle/manager.go` | Unexported type `entry` sits between exported `Manager` and exported `Option` |
| `logging/logger.go` | Unexported function `newWriter` precedes exported `Setup`; unexported type `prefixWriter` precedes exported method `Write` |
| `message/email/mailgun/mailgun.go` | Unexported `var _` interface assertion precedes exported methods `Name`/`Send` |
| `message/email/smtp/smtp.go` | Unexported `var _` interface assertion precedes exported methods `Name`/`Send` |
| `message/sms/aliyun/aliyun.go` | Unexported `smsSender` type, `newProviderWithClient` function, and `var _` all sit between exported symbols |
| `ratelimit/ratelimit.go` | Exported methods `Stats`/`Reset`/`ResetPurpose` sit after unexported helpers `key`, `scopedRule`, `allScopedRules`, `scopedRulesFor` |
| `redisx/lock.go` | Unexported consts `unlockScript`/`renewScript` precede exported types/funcs/methods |
| `tencent/mini/client.go` | 8 unexported consts (`defaultBaseURL`, paths, grant types, etc.) precede exported `Client` and its API |
| `tencent/mini/manager.go` | Unexported consts (`renewBeforeSeconds`/`syncRefreshPoint`) and type `cachedToken` precede exported `Manager` and its API |

Files scanned and confirmed to already comply (no changes needed):
`gorx/*`, `configx/*`, `xerr/*`, `grpcx/server.go`, `ptr/ptr.go`,
`redisx/redis.go`, `redisx/testhelpers.go`, `jsonx/jsonx.go`,
`signalx/signalx.go`, `dbx/testhelpers.go`, `dbx/pagination.go`,
`dbx/slog_logger.go`, `xerr/xcodes/codes.go`, `tencent/mini/types.go`,
`message/sms/sender.go`, `message/sms/router.go`, `message/email/sender.go`,
`cronx/schedules.go`, `cronx/slog_logger.go`, `lifecycle/lifecycle.go`.

**Note on `redisx/testhelpers.go` and `dbx/testhelpers.go`**: these are not
`*_test.go` files (they export helpers for downstream test code), so the rule
applies. The scan confirmed they already comply.

**Note on `var _` import-time side-effect declarations** (e.g. `mailgun.go`
and `smtp.go` use `var _ message.EmailProvider = (*Provider)(nil)`): these
will move to the unexported section at the bottom. Although the symbol
name is `_`, the declaration carries an interface-assertion contract that is
not part of the public API surface.

**Note on `cronx/slog_logger.go`, `dbx/slog_logger.go`, `lifecycle/lifecycle.go`**:
these files contain unexported types with methods named in exported style
(e.g. `(slogLogger).Info`, `(starterOnly).Stop`). Since the receiver type is
unexported, these methods are unreachable from outside the package, so they
are treated as unexported and grouped with their receiver type. No
reordering required.

## Verification

After each file is reordered:

```bash
go build ./...
go test ./...
golangci-lint run ./...
```

Because the change is purely layout, all three must pass without modification
to non-`*_test.go` logic.

## Rollout

1. Add the rule to `CLAUDE.md` under a new `## 文件内符号顺序` section, placed
   immediately before the `## 代码质量` section.
2. Reorder files one at a time, running `go build ./...` after each file.
3. Final full `go test ./...` and `golangci-lint run ./...` before commit.
4. Single commit, message:
   `style: order exported symbols before unexported in non-test files`.

## 关联

Implementation plan: `docs/superpowers/plans/2026-06-12-exported-symbol-ordering.md`.
