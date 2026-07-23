# Exported Symbol Ordering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an "exported symbols first" rule to `CLAUDE.md` and reorder 16 non-test `.go` files so every file's exported declarations appear above its unexported ones. Pure layout refactor — no logic, signature, or behavior changes.

**Architecture:** Each task targets one directory (or one file when a directory has a single offender). Each file is reordered to the explicit "target order" listed in the task, then verified with `go build ./...`. After all files are reordered, a final task runs the full `go test` + `golangci-lint` suite and commits everything in a single `style:` commit. A scanner script (`/tmp/symorder3.py`) is used to confirm zero violations remain.

**Tech Stack:** Go 1.21+ (see `go.mod`), `golangci-lint`. No new dependencies.

**Reference:** Spec at `docs/superpowers/specs/2026-06-12-exported-symbol-ordering-design.md`.

---

## Conventions used in every file task

Each task lists a **Target order** for one file. The target order uses the
same identifiers as the source file; the only change is the top-to-bottom
sequence of declarations. Comments attached to a declaration travel with
it (a doc comment stays directly above its `func`/`type`/`var`/`const`).
Section-divider comments such as `// --- internal helpers ---` either
travel with the block they annotate or get deleted if they no longer make
sense at the destination — engineer's call, no logic depends on them.

For each file, the engineer should:

1. **Read** the file in full to confirm current state matches what the task
   describes.
2. **Reorder** by either:
   - One `Write` call replacing the entire file body with the reordered
     content (cleanest for files with multiple moves); or
   - Multiple `Edit` calls, each relocating one block to its target
     position.
3. **Verify** the file still compiles via `go build ./...`.
4. **Spot-check** by re-reading the file's declaration order to confirm
   exported declarations all precede unexported ones.

The "current order" in each task uses line numbers from the file as it
exists at the start of implementation. If the engineer reorders files in
sequence, those line numbers stay accurate for files not yet touched.

---

### Task 1: Add the rule to CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (insert new section before `## 代码质量`)

- [ ] **Step 1: Read the current CLAUDE.md**

Run: `Read /Users/moss/code/base/go-common/CLAUDE.md`

Locate the `## 代码质量` heading.

- [ ] **Step 2: Insert the new `## 文件内符号顺序` section**

Insert the following block **immediately before** `## 代码质量`. Use `Edit`
with `old_string` set to `## 代码质量` (heading line) and `new_string` set
to the new section followed by the original `## 代码质量` heading:

```markdown
## 文件内符号顺序

每个 `.go` 文件（除 `*_test.go` 和生成文件外）的顶层声明按"导出优先"原则分为两段：

1. **导出段**（文件上半，保留原相对顺序）：导出类型 / 常量 / 变量 / 构造函数 `New*` / 导出函数 / 导出方法
2. **未导出段**（文件下半，保留原相对顺序）：未导出类型 / 常量 / 变量 / 函数 / 方法

**约束：**

- 同一 receiver 类型的方法必须紧接该 type 定义；type 内部按"先导出方法、后未导出方法"分组
- 方法的可见性跟随 receiver 类型：未导出类型上的"导出方法名"（如 `func (starterOnly) Stop()`）按未导出处理
- 同段内部不强制重排，保留原作者的语义分组（例如 `Option` 类型与对应 `With*` helpers 仍可相邻）
- `*_test.go` 与生成文件（`*_generated.go`、`*.pb.go`、`internal/generated/` 下）豁免
- 该规则仅约束排版，不影响逻辑

**目的：** 使用方打开任意源文件，第一眼看到的是该文件的全部公开 API。

```

Leave a blank line between the new section and the `## 代码质量` heading.

- [ ] **Step 3: Verify CLAUDE.md renders correctly**

Run: `Read /Users/moss/code/base/go-common/CLAUDE.md`

Confirm the new `## 文件内符号顺序` section appears before `## 代码质量`, with both headings at the same `##` level and surrounded by blank lines.

- [ ] **Step 4: Commit CLAUDE.md change**

```bash
git add CLAUDE.md
git commit -m "docs(claude): add exported-symbol-first ordering rule"
```

---

### Task 2: Reorder captcha/ (3 files)

**Files:**
- Modify: `captcha/captcha.go`
- Modify: `captcha/generator.go`
- Modify: `captcha/store.go`

#### 2a. `captcha/captcha.go`

Current order (line numbers as of plan writing):

```
L17  type Config                 (EXP)
L26  type PurposeConfig          (EXP)
L33  type SendFunc               (EXP)
L36  type GenerateOption         (EXP)
L38  type generateConfig         (unex)
L44  func WithSend               (EXP)
L49  type Option                 (EXP)
L52  func WithRedisClient        (EXP)
L57  const defaultTTL            (unex)
L60  type Captcha                (EXP)
L73  func New                    (EXP)
L141 func (c *Captcha) ttlForPurpose  (unex method)
L159 func (c *Captcha) Generate       (EXP method)
L223 func (c *Captcha) Verify         (EXP method)
```

Target order:

```
type Config
type PurposeConfig
type SendFunc
type GenerateOption
func WithSend
type Option
func WithRedisClient
type Captcha
func New
func (c *Captcha) Generate
func (c *Captcha) Verify
// --- unexported below ---
type generateConfig
const defaultTTL
func (c *Captcha) ttlForPurpose
```

- [ ] **Step 1: Read the file in full**

Run: `Read /Users/moss/code/base/go-common/captcha/captcha.go`

- [ ] **Step 2: Reorder declarations**

Use `Write` to replace the whole file with the reordered content, or use
multiple `Edit` calls to:
1. Move `type generateConfig` (with its doc comment if any) to the bottom
   (above `ttlForPurpose`).
2. Move `const defaultTTL` to the bottom (group with `generateConfig`).
3. Move `func (c *Captcha) ttlForPurpose` to the bottom (after the two
   above).

The end of the exported section should now read:

```go
// Verify checks a verification code by target, purpose, and channel.
func (c *Captcha) Verify(ctx context.Context, target, code, purpose, channel string) (*VerifyResult, error) {
	return c.store.Verify(ctx, purpose, channel, target, code)
}
```

And the unexported tail should read (in this order):

```go
type generateConfig struct {
	send SendFunc
}

// defaultTTL is the code validity duration when no rate rules are configured.
const defaultTTL = 5 * time.Minute

// ttlForPurpose returns the code validity duration for the given purpose.
// The TTL is derived from the shortest rate-limit window.
// If no rules are configured, defaultTTL is returned.
func (c *Captcha) ttlForPurpose(purpose string) time.Duration {
	pc, ok := c.purposes[purpose]
	if !ok || len(pc.RateRules) == 0 {
		return defaultTTL
	}
	shortest := pc.RateRules[0].Window
	for _, r := range pc.RateRules[1:] {
		if r.Window < shortest {
			shortest = r.Window
		}
	}
	return shortest
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 4: Confirm order**

Run: `Read /Users/moss/code/base/go-common/captcha/captcha.go`

Verify `type generateConfig`, `const defaultTTL`, and `func ttlForPurpose`
all appear after `func Verify`.

#### 2b. `captcha/generator.go`

Current order:

```
L11 type CodeFormat                       (EXP)
L23 var (FormatDigit6, FormatDigit4, ...) (EXP, single var block)
L25 var (digits, alphaLower, ...)         (unex, single var block)
L35 type CodeGenerator                    (EXP)
L40 func NewCodeGenerator                 (EXP)
L45 func (g *CodeGenerator) Generate      (EXP method)
L68 func (g *CodeGenerator) charset       (unex method)
```

Target order:

```
type CodeFormat
var (FormatDigit6, FormatDigit4, ...)
type CodeGenerator
func NewCodeGenerator
func (g *CodeGenerator) Generate
// --- unexported below ---
var (digits, alphaLower, ...)
func (g *CodeGenerator) charset
```

Note: `charset` is already in the correct relative position (it follows
`Generate`). Only the unexported `var (...)` block needs to move from
between `FormatDigit6...` and `CodeGenerator` to the bottom (between
`Generate` and `charset`).

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/captcha/generator.go`

- [ ] **Step 2: Move the unexported `var (...)` block**

Use `Edit` (or `Write`) so that the file reads in this order:
1. `type CodeFormat`
2. `var ( FormatDigit6 ... FormatAlphaMixed6 )` (the exported block)
3. `type CodeGenerator`
4. `func NewCodeGenerator`
5. `func (g *CodeGenerator) Generate`
6. `var ( digits alphaLower alphaUpper alphaMixed alphanumeric alphanumMixed )` (the unexported block)
7. `func (g *CodeGenerator) charset`

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/captcha/generator.go`

Verify the `digits` / `alphaLower` block sits between `Generate` and
`charset`.

#### 2c. `captcha/store.go`

Current order:

```
L14 type Record                    (EXP)
L24 type VerifyResult              (EXP)
L33 type Store                     (EXP, interface)
L42 type RedisStore                (EXP)
L50 func NewRedisStore             (EXP)
L84 func (s *RedisStore) key       (unex method)
L89 func (s *RedisStore) Set       (EXP method)
L98 func (s *RedisStore) Verify    (EXP method)
```

Target order:

```
type Record
type VerifyResult
type Store
type RedisStore
func NewRedisStore
func (s *RedisStore) Set
func (s *RedisStore) Verify
// --- unexported below ---
func (s *RedisStore) key
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/captcha/store.go`

- [ ] **Step 2: Move `key` method to the bottom**

Use `Edit` (or `Write`) so the `key` helper appears *after* `Verify`. The
method body is:

```go
func (s *RedisStore) key(purpose, channel, target string) string {
	return fmt.Sprintf("%s:%s:%s:%s", s.prefix, purpose, channel, target)
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/captcha/store.go`

Verify `key` is the last declaration in the file.

---

### Task 3: Reorder cronx/cronx.go

**Files:**
- Modify: `cronx/cronx.go`

Current order:

```
L17 type Config       (EXP)
L26 type Option       (EXP)
L28 type cronOptions  (unex)
L33 func WithCronOption  (EXP)
L47 func New          (EXP)
```

Target order:

```
type Config
type Option
func WithCronOption
func New
// --- unexported below ---
type cronOptions
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/cronx/cronx.go`

- [ ] **Step 2: Move `cronOptions` to the bottom**

Use `Edit` (or `Write`) so that:

```go
// cronOptions holds options applied via Option functions.
type cronOptions struct {
	extraOpts []cron.Option
}
```

appears as the last declaration in the file, after `func New`.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/cronx/cronx.go`

Verify `type cronOptions` is the last declaration.

---

### Task 4: Reorder dbx/dbx.go

**Files:**
- Modify: `dbx/dbx.go`

Current order:

```
L15 const (defaultLogLevel, defaultSlowThreshold)  (unex)
L21 type Config                                    (EXP)
L41 func New                                       (EXP)
L84 func AutoMigrate                               (EXP)
L93 func parseLogLevel                             (unex)
```

Target order:

```
type Config
func New
func AutoMigrate
// --- unexported below ---
const (defaultLogLevel, defaultSlowThreshold)
func parseLogLevel
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/dbx/dbx.go`

- [ ] **Step 2: Move the defaults const block to the bottom**

Use `Edit` (or `Write`) so the file's declaration order becomes:

1. (package + imports, unchanged)
2. `type Config` (with all fields unchanged)
3. `func New` (body unchanged)
4. `func AutoMigrate` (body unchanged)
5. `// defaults` comment + `const ( defaultLogLevel ... defaultSlowThreshold ... )` block
6. `func parseLogLevel` (body unchanged)

The two const identifiers and their values stay exactly as in the current
file:

```go
// defaults
const (
	defaultLogLevel      = "warn"
	defaultSlowThreshold = 200 * time.Millisecond
)
```

And `parseLogLevel` retains its current switch body (cases: `"silent"` →
`gormLogLevelSilent`, `"error"` → `gormLogLevelError`, `"warn"` →
`gormLogLevelWarn`, `"info"` → `gormLogLevelInfo`, default →
`gormLogLevelWarn`). The engineer does **not** edit function or struct
bodies — only the position of the const block.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/dbx/dbx.go`

Verify the `defaults` const block appears after `AutoMigrate` and before
`parseLogLevel`.

---

### Task 5: Reorder grpcx/ (2 files)

**Files:**
- Modify: `grpcx/auth.go`
- Modify: `grpcx/interceptor.go`

#### 5a. `grpcx/auth.go`

Current order:

```
L13 type userIDKeyType     (unex)
L16 var UserIDKey          (EXP)
L19 func GetUserIDFromCtx  (EXP)
L30 func BearerTokenFromCtx (EXP)
```

Target order:

```
var UserIDKey
func GetUserIDFromCtx
func BearerTokenFromCtx
// --- unexported below ---
type userIDKeyType
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/grpcx/auth.go`

- [ ] **Step 2: Move `userIDKeyType` to the bottom**

The type declaration travels with its doc comment:

```go
// userIDKeyType is an unexported type for context keys, preventing collisions.
type userIDKeyType struct{}
```

Use `Edit` (or `Write`) so that `type userIDKeyType` is the last declaration
in the file. The line `var UserIDKey = userIDKeyType{}` stays at the top
of the exported block — the type can be referenced before its declaration
in Go, so no compilation issue.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/grpcx/auth.go`

Verify `type userIDKeyType` is last.

#### 5b. `grpcx/interceptor.go`

Current order:

```
L15 var categoryToGRPC       (unex)
L28 func ErrorInterceptor    (EXP)
```

Target order:

```
func ErrorInterceptor
// --- unexported below ---
var categoryToGRPC
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/grpcx/interceptor.go`

- [ ] **Step 2: Move `categoryToGRPC` map to the bottom**

Use `Edit` (or `Write`) so the `var categoryToGRPC = map[...]{...}` block
appears after `ErrorInterceptor`. The map and its doc comment travel
together:

```go
// categoryToGRPC maps xerr categories to gRPC status codes.
var categoryToGRPC = map[xerr.Category]codes.Code{
	xerr.CategoryBadRequest:         codes.InvalidArgument,
	xerr.CategoryUnauthorized:       codes.Unauthenticated,
	xerr.CategoryForbidden:          codes.PermissionDenied,
	xerr.CategoryNotFound:           codes.NotFound,
	xerr.CategoryConflict:           codes.AlreadyExists,
	xerr.CategoryTooManyRequests:    codes.ResourceExhausted,
	xerr.CategoryInternal:           codes.Internal,
	xerr.CategoryServiceUnavailable: codes.Unavailable,
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/grpcx/interceptor.go`

Verify `var categoryToGRPC` is the last declaration.

---

### Task 6: Reorder lifecycle/manager.go

**Files:**
- Modify: `lifecycle/manager.go`

Current order:

```
L15 type Manager       (EXP)
L25 type entry         (unex)
L31 type Option        (EXP)
L36 func NewManager    (EXP)
L46 func WithStopTimeout (EXP)
L51 func Add                (EXP method)
L56 func AddStarter         (EXP method)
L61 func AddStopper         (EXP method)
L74 func Start              (EXP method)
L100 func Stop              (EXP method)
L150 func Run               (EXP method)
```

Target order:

```
type Manager
type Option
func NewManager
func WithStopTimeout
func Add
func AddStarter
func AddStopper
func Start
func Stop
func Run
// --- unexported below ---
type entry
```

Note: `lifecycle/lifecycle.go` is NOT modified — its `starterOnly`/`stopperOnly`
types are unexported and their methods follow the receiver, which is correct
under the rule.

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/lifecycle/manager.go`

- [ ] **Step 2: Move `type entry` to the bottom**

The `entry` type travels with its doc comment:

```go
// entry pairs a name with the registered Service.
type entry struct {
	name string
	svc  Service
}
```

Move it to the very bottom of the file.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/lifecycle/manager.go`

Verify `type entry` is the last declaration.

---

### Task 7: Reorder logging/logger.go

**Files:**
- Modify: `logging/logger.go`

Current order:

```
L14 type FileConfig       (EXP)
L23 type Config           (EXP)
L33 func newWriter        (unex)
L49 func Setup            (EXP)
L82 type prefixWriter     (unex)
L87 func (w *prefixWriter) Write  (EXP method on unexported type → unex)
```

Target order:

```
type FileConfig
type Config
func Setup
// --- unexported below ---
func newWriter
type prefixWriter
func (w *prefixWriter) Write
```

Note: `Write` has an exported name but its receiver `prefixWriter` is
unexported, so per the spec the method is treated as unexported and stays
grouped with `prefixWriter`.

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/logging/logger.go`

- [ ] **Step 2: Move `newWriter` to the bottom**

Use `Edit` (or `Write`) so that:
- `func Setup` appears immediately after `type Config`
- The unexported block (`newWriter`, `prefixWriter`, `Write`) appears at
  the bottom in the order: `newWriter`, then `prefixWriter` + its `Write`
  method (which must stay adjacent to `prefixWriter`).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/logging/logger.go`

Verify the unexported block (newWriter / prefixWriter / Write) appears
after `Setup`.

---

### Task 8: Reorder message/email/ (2 files)

**Files:**
- Modify: `message/email/mailgun/mailgun.go`
- Modify: `message/email/smtp/smtp.go`

#### 8a. `message/email/mailgun/mailgun.go`

Current order:

```
L18 type Config         (EXP)
L26 type Provider       (EXP)
L32 func NewProvider    (EXP)
L40 var _ (interface assertion, unex)
L42 func Name           (EXP method)
L45 func Send           (EXP method)
```

Target order:

```
type Config
type Provider
func NewProvider
func Name
func Send
// --- unexported below ---
var _ email.Provider = (*Provider)(nil)
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/message/email/mailgun/mailgun.go`

- [ ] **Step 2: Move the `var _` interface assertion to the bottom**

Move:

```go
// Compile-time check that Provider implements email.Provider.
var _ email.Provider = (*Provider)(nil)
```

to after `func Send` (last declaration in the file).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/message/email/mailgun/mailgun.go`

Verify `var _` is the last declaration.

#### 8b. `message/email/smtp/smtp.go`

Current order:

```
L14 type Config       (EXP)
L23 type Provider     (EXP)
L31 func NewProvider  (EXP)
L46 var _ (interface assertion, unex)
L48 func Name         (EXP method)
L51 func Send         (EXP method)
```

Target order: same shape as 8a.

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/message/email/smtp/smtp.go`

- [ ] **Step 2: Move the `var _` interface assertion to the bottom**

Same pattern as 8a — move the `// Compile-time check ...` block to after
`func Send`.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/message/email/smtp/smtp.go`

Verify `var _` is last.

---

### Task 9: Reorder message/sms/aliyun/aliyun.go

**Files:**
- Modify: `message/sms/aliyun/aliyun.go`

Current order:

```
L17 type smsSender                   (unex interface)
L22 type Config                      (EXP)
L30 type Provider                    (EXP)
L36 func NewProvider                 (EXP)
L58 func newProviderWithClient       (unex)
L63 var _ (interface assertion, unex)
L65 func Name                        (EXP method)
L68 func Send                        (EXP method)
```

Target order:

```
type Config
type Provider
func NewProvider
func Name
func Send
// --- unexported below ---
type smsSender
func newProviderWithClient
var _ sms.Provider = (*Provider)(nil)
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/message/sms/aliyun/aliyun.go`

- [ ] **Step 2: Move three unexported declarations to the bottom**

Use `Edit` (or `Write`) so that `type smsSender`,
`func newProviderWithClient`, and `var _ sms.Provider = (*Provider)(nil)`
all appear after `func Send`, in that order.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/message/sms/aliyun/aliyun.go`

Verify all three unexported declarations appear after `func Send`.

---

### Task 10: Reorder ratelimit/ratelimit.go

**Files:**
- Modify: `ratelimit/ratelimit.go`

Current order:

```
L15  type Rule                          (EXP)
L21  type Stat                          (EXP)
L31  type Config                        (EXP)
L41  type Limiter                       (EXP, interface)
L47  type RedisLimiter                  (EXP)
L54  func NewRedisLimiter               (EXP)
L92  func (l *RedisLimiter) Allow       (EXP method)
L132 func (l *RedisLimiter) key         (unex method)
L140 type scopedRule                    (unex)
L147 func (l *RedisLimiter) allScopedRules       (unex method)
L169 func (l *RedisLimiter) Stats                (EXP method)
L234 func (l *RedisLimiter) Reset                (EXP method)
L254 func (l *RedisLimiter) ResetPurpose         (EXP method)
L275 func (l *RedisLimiter) scopedRulesFor       (unex method)
```

Target order:

```
type Rule
type Stat
type Config
type Limiter
type RedisLimiter
func NewRedisLimiter
func (l *RedisLimiter) Allow
func (l *RedisLimiter) Stats
func (l *RedisLimiter) Reset
func (l *RedisLimiter) ResetPurpose
// --- unexported below ---
type scopedRule
func (l *RedisLimiter) key
func (l *RedisLimiter) allScopedRules
func (l *RedisLimiter) scopedRulesFor
```

The unexported methods can stay in any order at the bottom; preserving the
existing relative order (key, allScopedRules, scopedRulesFor) is fine. The
exported methods (`Stats`, `Reset`, `ResetPurpose`) need to move *up* to
sit between `Allow` and the unexported block.

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/ratelimit/ratelimit.go`

- [ ] **Step 2: Move the three exported methods (`Stats`, `Reset`, `ResetPurpose`) up**

Use `Write` for this file (it has the largest rearrangement). Output the
file in the target order. Each method's doc comment and body are preserved
verbatim — only the top-level ordering changes.

A clean way: in one `Write`:
1. Keep lines 1–130 unchanged (package, imports, `type Rule`, `type Stat`,
   `type Config`, `type Limiter`, `type RedisLimiter`, `NewRedisLimiter`,
   `Allow`).
2. Then write `Stats` (current L169-229), `Reset` (current L234-248),
   `ResetPurpose` (current L254-271).
3. Then write the unexported block: `type scopedRule` (current L140-143),
   `key` (current L132-135), `allScopedRules` (current L147-164),
   `scopedRulesFor` (current L275-298).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Run ratelimit tests**

Run: `go test ./ratelimit/...`
Expected: all tests pass (no logic change).

- [ ] **Step 5: Confirm**

Run: `Read /Users/moss/code/base/go-common/ratelimit/ratelimit.go`

Verify declaration order matches the target.

---

### Task 11: Reorder redisx/lock.go

**Files:**
- Modify: `redisx/lock.go`

Current order:

```
L13  var (ErrLockFailed, ErrUnlockFailed, ErrRenewFailed)  (EXP)
L19  const unlockScript                                    (unex)
L27  const renewScript                                     (unex)
L36  type LockConfig                                       (EXP)
L45  type Lock                                             (EXP)
L51  func NewLock                                          (EXP)
L70  func Acquire                                          (EXP method)
L94  func Release                                          (EXP method)
L107 func Renew                                            (EXP method)
L124 func KeepAlive                                        (EXP method)
L143 func key                                              (unex method)
```

Target order:

```
var (ErrLockFailed, ErrUnlockFailed, ErrRenewFailed)
type LockConfig
type Lock
func NewLock
func Acquire
func Release
func Renew
func KeepAlive
// --- unexported below ---
const unlockScript
const renewScript
func key
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/redisx/lock.go`

- [ ] **Step 2: Move the two Lua-script consts to the bottom**

Use `Edit` (or `Write`) so that `unlockScript` and `renewScript` appear
after `KeepAlive` and before the `key` method. Their bodies are unchanged:

```go
const unlockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`

const renewScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
	return 0
end
`
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/redisx/lock.go`

Verify both Lua consts and `key` are at the bottom, in that order.

---

### Task 12: Reorder tencent/mini/ (2 files)

**Files:**
- Modify: `tencent/mini/client.go`
- Modify: `tencent/mini/manager.go`

#### 12a. `tencent/mini/client.go`

Current order:

```
L17  const (defaultBaseURL, code2SessionPath, ..., sigMethodHMACSHA256)  (unex, 8 consts)
L30  type Client                    (EXP)
L38  func NewClient                 (EXP)
L50  func NewClientWithBaseURL      (EXP)
L57  func SignIn                    (EXP method)
L80  func GetStableAccessToken      (EXP method)
L108 func CheckLoginStatus          (EXP method)
L131 func GetPhoneNumber            (EXP method)
L156 func get                       (unex method)
L173 func post                      (unex method)
L191 func signSessionKey            (unex func)
L197 func checkErr                  (unex func)
```

Target order:

```
type Client
func NewClient
func NewClientWithBaseURL
func SignIn
func GetStableAccessToken
func CheckLoginStatus
func GetPhoneNumber
// --- unexported below ---
const (defaultBaseURL, code2SessionPath, ..., sigMethodHMACSHA256)
func get
func post
func signSessionKey
func checkErr
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/tencent/mini/client.go`

- [ ] **Step 2: Move the const block to the bottom**

Use `Write` to output the full file in the target order. The const block
(currently at the top, L17-27) travels as a unit to just before the
`func (c *Client) get` method (current L156). All other declarations
remain in their current relative order.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/tencent/mini/client.go`

Verify the const block sits between `GetPhoneNumber` and `get`.

#### 12b. `tencent/mini/manager.go`

Current order:

```
L13  const (renewBeforeSeconds, syncRefreshPoint)  (unex)
L21  type cachedToken                              (unex)
L28  type Manager                                  (EXP)
L37  func NewManager                               (EXP)
L53  func SignIn                                   (EXP method)
L62  func GetPhoneNumber                           (EXP method)
L75  func CheckLoginStatus                         (EXP method)
L87  func getClient                                (unex method)
L96  func getAccessToken                           (unex method)
L114 func getCachedToken                           (unex method)
L128 func needsAsyncRefresh                        (unex method)
L133 func backgroundRefresh                        (unex method)
L145 func refreshToken                             (unex method)
```

Target order:

```
type Manager
func NewManager
func SignIn
func GetPhoneNumber
func CheckLoginStatus
// --- unexported below ---
const (renewBeforeSeconds, syncRefreshPoint)
type cachedToken
func getClient
func getAccessToken
func getCachedToken
func needsAsyncRefresh
func backgroundRefresh
func refreshToken
```

- [ ] **Step 1: Read**

Run: `Read /Users/moss/code/base/go-common/tencent/mini/manager.go`

- [ ] **Step 2: Move const block and `cachedToken` type to the bottom**

Use `Write` to output the full file in the target order. The two unexported
top-of-file declarations (`const (...)` and `type cachedToken`) move to
just above `func (m *Manager) getClient` (current L87). Everything else
stays in relative order.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Confirm**

Run: `Read /Users/moss/code/base/go-common/tencent/mini/manager.go`

Verify the const block and `type cachedToken` appear between
`CheckLoginStatus` and `getClient`.

---

### Task 13: Full verification

**Files:** (none modified — verification only)

- [ ] **Step 1: Re-run the scanner over the whole repo**

Run:

```bash
python3 /tmp/symorder3.py /Users/moss/code/base/go-common
```

Expected output: `Total files with violations: 0`.

If the script file no longer exists (e.g. new session), recreate it from
the version saved in the spec's scan log. The key behavior: parse each
non-test `.go` file, classify each top-level declaration as exported or
unexported (methods follow receiver type visibility), and report any
exported declaration that appears after an unexported one.

If any file still shows violations, return to the corresponding task and
fix the missed move.

- [ ] **Step 2: Full build**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 3: Full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 4: Lint**

Run: `golangci-lint run ./...`
Expected: no findings (or only pre-existing findings unrelated to this
change; if `gofmt`/`goimports` findings appear on reordered files, run
them and re-lint).

- [ ] **Step 5: gofmt check**

Run: `gofmt -l ./...`
Expected: empty output. If any file appears in the list, run
`gofmt -w <file>` and re-check.

---

### Task 14: Commit, sync plan, sync Obsidian

**Files:**
- Create: `docs/superpowers/plans/2026-06-12-exported-symbol-ordering.md` (this file — already created by writing-plans)
- Modify: Obsidian `services/go-common/go-common.md` (append plan row)
- Modify: Obsidian `services/go-common/changes.md` (if it exists; create if not)

- [ ] **Step 1: Stage all reordered files**

```bash
git add captcha/captcha.go captcha/generator.go captcha/store.go \
        cronx/cronx.go dbx/dbx.go \
        grpcx/auth.go grpcx/interceptor.go \
        lifecycle/manager.go logging/logger.go \
        message/email/mailgun/mailgun.go message/email/smtp/smtp.go \
        message/sms/aliyun/aliyun.go \
        ratelimit/ratelimit.go redisx/lock.go \
        tencent/mini/client.go tencent/mini/manager.go
git status
```

Expected: 16 files staged as modified.

- [ ] **Step 2: Commit**

```bash
git commit -m "$(cat <<'EOF'
style: order exported symbols before unexported in non-test files

Reorder 16 .go files so each file's exported types, functions, methods,
constants, and variables appear before its unexported ones, per the new
"## 文件内符号顺序" rule in CLAUDE.md. Pure layout refactor — no logic,
signature, or behavior changes.

Files touched:
- captcha/captcha.go, captcha/generator.go, captcha/store.go
- cronx/cronx.go
- dbx/dbx.go
- grpcx/auth.go, grpcx/interceptor.go
- lifecycle/manager.go
- logging/logger.go
- message/email/mailgun/mailgun.go, message/email/smtp/smtp.go
- message/sms/aliyun/aliyun.go
- ratelimit/ratelimit.go
- redisx/lock.go
- tencent/mini/client.go, tencent/mini/manager.go

Verified via go build, go test, golangci-lint, and a symbol-order scanner.
EOF
)"
```

Expected: one commit created.

- [ ] **Step 3: Sync plan to Obsidian**

```bash
cp /Users/moss/code/base/go-common/docs/superpowers/plans/2026-06-12-exported-symbol-ordering.md \
   "/Users/moss/Library/Mobile Documents/iCloud~md~obsidian/Documents/only/services/go-common/plan/v1/7-exported-symbol-ordering.md"
```

- [ ] **Step 4: Append plan row to Obsidian go-common index**

```bash
obsidian vault=only append file="services/go-common/go-common" \
  content="| [[services/go-common/plan/v1/7-exported-symbol-ordering\|7-exported-symbol-ordering]] | 文件内导出符号前置规则实施计划 |"$'\n'
```

- [ ] **Step 5: Update spec `## 关联` section**

Edit `docs/superpowers/specs/2026-06-12-exported-symbol-ordering-design.md`
and change the implementation plan link from the placeholder to the actual
path:

```markdown
Implementation plan: `docs/superpowers/plans/2026-06-12-exported-symbol-ordering.md`.
```

(Remove the "to be written via writing-plans skill" suffix.)

Then commit the spec edit:

```bash
git add docs/superpowers/specs/2026-06-12-exported-symbol-ordering-design.md
git commit -m "docs(spec): link exported-symbol-ordering spec to plan"
```

---

## Self-Review Notes

- Every task specifies exact files, exact target order, exact verification
  commands. No "TBD" or "implement later" language.
- Tasks are sequenced so that line numbers cited in "current order" remain
  valid for files not yet touched (each task affects a disjoint set of
  files).
- The captcha task order (Task 2) touches three files in one task because
  they share a directory and the moves are simple; engineers comfortable
  with batch edits save commits. If the engineer prefers finer granularity,
  each sub-task (2a, 2b, 2c) can be its own commit.
- The plan uses `Edit` (preferred for small moves) or `Write` (for large
  rearrangements like ratelimit.go and tencent/mini/client.go). Engineer
  chooses per file.
- No `*_test.go` files are modified.
- Generated files (none in this repo currently, but if added) are exempt
  per the rule.
