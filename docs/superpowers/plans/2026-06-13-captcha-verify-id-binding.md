# captcha: Verify captchaID Binding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional `WithCaptchaID` option to `Captcha.Verify` that, when supplied, requires the captchaID to match the one returned by the originating `Generate` call. Defends against cross-context code-reuse attacks (SIM swap, shared numbers, SMS interception).

**Architecture:** All binding logic lives in the existing Redis Lua script. `Store.Verify` gains a positional `captchaID string` parameter (passed as `ARGV[2]`); empty string means "skip check". `Captcha.Verify` becomes variadic with a new `VerifyOption`/`WithCaptchaID` pair that flattens into the Store call. The Redis key layout, the `Record` struct, and the `VerifyResult` struct are unchanged.

**Tech Stack:** Go stdlib (`context`, `fmt`), `github.com/redis/go-redis/v9` (Lua script via `redis.NewScript`), `testify/require`, `redisx.NewTestClient` (miniredis). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-13-captcha-verify-id-binding-design.md`

---

## File Structure

**Modified files:**

| File | Responsibility |
|---|---|
| `captcha/store.go` | `Store` interface gains `captchaID` param; `RedisStore.Verify` passes it as `ARGV[2]`; Lua script adds `idMatch` check |
| `captcha/captcha.go` | Add `VerifyOption` type, `WithCaptchaID` func, `verifyConfig` struct; change `Captcha.Verify` to variadic |
| `captcha/store_test.go` | Update every `store.Verify(...)` call site to pass `""`; add 4 new tests for captchaID match/mismatch/empty/double-mismatch |
| `captcha/captcha_test.go` | Add 3 new tests for `WithCaptchaID` correct/wrong/empty; existing tests unchanged (variadic back-compat) |

**Not modified:**
- `Record` struct — `CaptchaID` field already exists.
- `VerifyResult` struct — `CaptchaID` field already exists, populated from stored Record.
- Redis key layout — still `prefix:purpose:channel:target`.
- `Captcha.Generate` — untouched.

**Symbol ordering (per CLAUDE.md):** New exported symbols `VerifyOption` and `WithCaptchaID` are placed in the exported section of `captcha.go`, immediately after the `GenerateOption`/`WithSend` pair (semantic grouping: Generate options, then Verify options, then constructor options). The unexported `verifyConfig` struct joins the unexported section next to `generateConfig`.

---

## Task 1: Extend `Store` interface with `captchaID` and update Lua script

This is the only breaking change at the Store layer. Do it first so the Captcha layer change in Task 2 has somewhere to send the captchaID.

**Files:**
- Modify: `captcha/store.go` (`Store` interface line 33-39; `RedisStore.Verify` line 93-142; Lua script inside `NewRedisStore` line 54-80)
- Modify: `captcha/store_test.go` (every `store.Verify(...)` call site)

- [ ] **Step 1: Update the `Store` interface signature in `captcha/store.go`**

Replace the interface (lines 33-39):

```go
// Store is the interface for verification code storage.
type Store interface {
	// Set stores a captcha record keyed by purpose+channel+target with the given TTL.
	Set(ctx context.Context, purpose, channel, target string, record *Record, ttl time.Duration) error
	// Verify atomically checks the code, increments attempts on mismatch,
	// and deletes the record on success or max attempts exceeded.
	// captchaID is the binding token returned by Generate; pass "" to skip the
	// captchaID match check, otherwise the stored Record.CaptchaID must equal it.
	Verify(ctx context.Context, purpose, channel, target, code, captchaID string) (*VerifyResult, error)
}
```

- [ ] **Step 2: Update the Lua script inside `NewRedisStore`**

Replace the script body (lines 54-80) with the captchaID-aware version. The only change inside the loop is the new `idMatch` local and the combined match condition; everything else is byte-identical:

```go
verify: redis.NewScript(`
			local data = redis.call('GET', KEYS[1])
			if not data then return nil end

			local r = cjson.decode(data)

			if r.attempts >= r.max_attempts then
				redis.call('DEL', KEYS[1])
				return cjson.encode({status="exhausted", captcha_id=r.captcha_id, target=r.target, purpose=r.purpose, remaining=0})
			end

			r.attempts = r.attempts + 1

			local idMatch = ARGV[2] == "" or ARGV[2] == r.captcha_id

			if idMatch and r.code == ARGV[1] then
				redis.call('DEL', KEYS[1])
				return cjson.encode({status="matched", captcha_id=r.captcha_id, target=r.target, purpose=r.purpose, remaining=-1})
			end

			local remaining = r.max_attempts - r.attempts
			if remaining <= 0 then
				redis.call('DEL', KEYS[1])
			else
				redis.call('SET', KEYS[1], cjson.encode(r), 'KEEPTTL')
			end

			return cjson.encode({status="mismatch", captcha_id=r.captcha_id, target=r.target, purpose=r.purpose, remaining=remaining})
		`),
```

- [ ] **Step 3: Update `RedisStore.Verify` signature and pass captchaID as ARGV[2]**

Replace the signature and the `Run` call (lines 94-95):

```go
// Verify atomically checks the code against the stored record.
// If captchaID is non-empty, the stored Record.CaptchaID must equal it;
// otherwise the call returns mismatch even when the code is correct.
// An empty captchaID skips the binding check (legacy behavior).
func (s *RedisStore) Verify(ctx context.Context, purpose, channel, target, code, captchaID string) (*VerifyResult, error) {
	raw, err := s.verify.Run(ctx, s.client, []string{s.key(purpose, channel, target)}, code, captchaID).Text()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return &VerifyResult{RemainingAttempts: 0}, nil
		}
		return nil, fmt.Errorf("run verify script: %w", err)
	}
	// ... rest of the function unchanged
```

The body after the `Run` call (the `json.Unmarshal` and switch on `result.Status`) is unchanged.

- [ ] **Step 4: Update existing call sites in `captcha/store_test.go`**

Every `store.Verify(ctx, purpose, channel, target, code)` call gains a trailing `""`. There are 18 call sites in `store_test.go` (some tests have multiple). Each becomes:

```go
result, err := store.Verify(context.Background(), "register", "email", "user@example.com", "123456", "")
```

`Edit` with `replace_all` is NOT safe here because each call has different arguments. Apply each edit individually. After all 18 sites are updated, run:

```bash
go build ./captcha/...
```

Expected: build succeeds.

- [ ] **Step 5: Run existing store tests to verify no behavior change**

```bash
go test ./captcha/ -run TestRedisStore -v
```

Expected: every existing `TestRedisStore_*` test PASSES. captchaID is `""` on every call, which the Lua script treats as "skip check" — so behavior is identical to before.

- [ ] **Step 6: Write the failing test for captchaID mismatch**

Append to `captcha/store_test.go`:

```go
func TestRedisStore_Verify_captchaIDMismatch(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	err := store.Set(ctx, "login", "sms", "user@example.com", &Record{
		CaptchaID: "correct-id", Code: "123456", Target: "user@example.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	// Correct code, wrong captchaID -> mismatch, one attempt consumed.
	result, err := store.Verify(ctx, "login", "sms", "user@example.com", "123456", "wrong-id")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, "correct-id", result.CaptchaID)
	require.Equal(t, 2, result.RemainingAttempts)

	// Now verify with correct captchaID + correct code -> matched, despite the
	// earlier mismatch having consumed one attempt.
	result, err = store.Verify(ctx, "login", "sms", "user@example.com", "123456", "correct-id")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "correct-id", result.CaptchaID)
}
```

- [ ] **Step 7: Run the new test to verify it passes**

```bash
go test ./captcha/ -run TestRedisStore_Verify_captchaIDMismatch -v
```

Expected: PASS. The Lua change from Step 2 already implements the behavior.

- [ ] **Step 8: Add captchaID-match and empty-captchaID regression tests**

Append to `captcha/store_test.go`:

```go
func TestRedisStore_Verify_captchaIDMatch(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	err := store.Set(ctx, "register", "email", "user@example.com", &Record{
		CaptchaID: "id-1", Code: "654321", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(ctx, "register", "email", "user@example.com", "654321", "id-1")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "id-1", result.CaptchaID)
}

func TestRedisStore_Verify_captchaIDEmptySkipsCheck(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	// Stored captchaID is "real-id" but caller passes "" -> skip check -> match.
	err := store.Set(ctx, "register", "email", "user@example.com", &Record{
		CaptchaID: "real-id", Code: "111222", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(ctx, "register", "email", "user@example.com", "111222", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "real-id", result.CaptchaID)
}

func TestRedisStore_Verify_captchaIDAndCodeBothWrong(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	err := store.Set(ctx, "login", "sms", "user@example.com", &Record{
		CaptchaID: "id-2", Code: "123456", Target: "user@example.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	// Both wrong: attempts must increment exactly once, not twice.
	result, err := store.Verify(ctx, "login", "sms", "user@example.com", "000000", "wrong-id")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 2, result.RemainingAttempts)
}
```

- [ ] **Step 9: Run all store tests**

```bash
go test ./captcha/ -run TestRedisStore -v
```

Expected: every test PASSES, including the 4 new ones.

- [ ] **Step 10: Commit**

```bash
git add captcha/store.go captcha/store_test.go
git commit -m "feat(captcha): bind Store.Verify to captchaID

Store.Verify gains a positional captchaID parameter (ARGV[2] in the Lua
script). Empty string skips the check; non-empty must equal the stored
Record.CaptchaID. Mismatch consumes one attempt, same as a wrong code."
```

---

## Task 2: Add `VerifyOption`, `WithCaptchaID`, change `Captcha.Verify` to variadic

The Captcha layer is the user-facing surface. Variadic options preserve backwards compatibility — every existing `c.Verify(ctx, target, code, purpose, channel)` call still compiles and behaves identically.

**Files:**
- Modify: `captcha/captcha.go` (add types near line 42; change `Captcha.Verify` at lines 198-201)
- Modify: `captcha/captcha_test.go` (add 3 new tests; existing tests untouched)

- [ ] **Step 1: Add `VerifyOption` type and `WithCaptchaID` function**

In `captcha/captcha.go`, insert this block immediately after the `WithSend` function (currently ends at line 42), before the `Option` type declaration (line 44):

```go
// VerifyOption configures a Verify call.
type VerifyOption func(*verifyConfig)

// WithCaptchaID binds the Verify call to the captchaID returned by Generate.
// If id is empty or the option is omitted, the captchaID check is skipped
// and Verify behaves as it did before this option existed.
//
// Use this to defend against cross-context code-reuse attacks: a code
// generated in one session (e.g., a browser flow) cannot be verified in
// another session (e.g., a mobile app flow) without the matching captchaID.
func WithCaptchaID(id string) VerifyOption {
	return func(c *verifyConfig) { c.captchaID = id }
}
```

- [ ] **Step 2: Change `Captcha.Verify` signature and body**

Replace the current `Verify` method (lines 198-201):

```go
// Verify checks a verification code by target, purpose, and channel.
// Pass WithCaptchaID to additionally require the captchaID returned by the
// originating Generate call; otherwise the check is skipped.
func (c *Captcha) Verify(
	ctx context.Context, target, code, purpose, channel string,
	opts ...VerifyOption,
) (*VerifyResult, error) {
	var vc verifyConfig
	for _, opt := range opts {
		opt(&vc)
	}
	return c.store.Verify(ctx, purpose, channel, target, code, vc.captchaID)
}
```

- [ ] **Step 3: Add the `verifyConfig` struct in the unexported section**

In `captcha/captcha.go`, insert this immediately after the existing `generateConfig` struct (currently lines 203-205):

```go
type verifyConfig struct {
	captchaID string
}
```

- [ ] **Step 4: Run existing captcha tests to verify backwards compatibility**

```bash
go test ./captcha/ -run TestCaptcha -v
```

Expected: every existing `TestCaptcha_*` test PASSES unchanged. Variadic options mean `c.Verify(ctx, target, code, purpose, channel)` calls (no opts) still compile and behave identically.

- [ ] **Step 5: Write the failing test for `WithCaptchaID` correct**

Append to `captcha/captcha_test.go`:

```go
func TestCaptcha_Verify_withCaptchaIDMatch(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	captchaID, code, err := c.Generate(ctx, "bind@example.com", "register", "email")
	require.NoError(t, err)
	require.NotEmpty(t, captchaID)

	result, err := c.Verify(ctx, "bind@example.com", code, "register", "email",
		WithCaptchaID(captchaID),
	)
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, captchaID, result.CaptchaID)
}
```

- [ ] **Step 6: Run the new test to verify it passes**

```bash
go test ./captcha/ -run TestCaptcha_Verify_withCaptchaIDMatch -v
```

Expected: PASS.

- [ ] **Step 7: Add test for `WithCaptchaID` mismatch**

Append to `captcha/captcha_test.go`:

```go
func TestCaptcha_Verify_withCaptchaIDMismatch(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	captchaID, code, err := c.Generate(ctx, "mismatch@example.com", "register", "email")
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Correct code, wrong captchaID -> mismatch, one attempt consumed.
	result, err := c.Verify(ctx, "mismatch@example.com", code, "register", "email",
		WithCaptchaID("00000000-0000-0000-0000-000000000000"),
	)
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, captchaID, result.CaptchaID)
	require.Equal(t, 2, result.RemainingAttempts)

	// Same code with the correct captchaID now succeeds (one attempt left).
	result, err = c.Verify(ctx, "mismatch@example.com", code, "register", "email",
		WithCaptchaID(captchaID),
	)
	require.NoError(t, err)
	require.True(t, result.Matched)
}
```

- [ ] **Step 8: Add test for `WithCaptchaID("")` equivalence to no option**

Append to `captcha/captcha_test.go`:

```go
func TestCaptcha_Verify_withCaptchaIDEmpty(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	_, code, err := c.Generate(ctx, "empty@example.com", "register", "email")
	require.NoError(t, err)

	// Empty captchaID is equivalent to not passing the option at all.
	result, err := c.Verify(ctx, "empty@example.com", code, "register", "email",
		WithCaptchaID(""),
	)
	require.NoError(t, err)
	require.True(t, result.Matched)
}
```

- [ ] **Step 9: Run all captcha tests**

```bash
go test ./captcha/ -v
```

Expected: every test PASSES, including the 3 new ones.

- [ ] **Step 10: Commit**

```bash
git add captcha/captcha.go captcha/captcha_test.go
git commit -m "feat(captcha): add WithCaptchaID option to Captcha.Verify

Captcha.Verify becomes variadic, accepting optional VerifyOption values.
WithCaptchaID forwards the binding token to Store.Verify, defending against
cross-context code-reuse attacks. Existing callers passing no options
behave identically to before."
```

---

## Task 3: Format, lint, coverage

Final polish. No behavior change.

**Files:** None modified directly; verifies Task 1 + Task 2 output.

- [ ] **Step 1: Run gofmt and goimports**

```bash
gofmt -w captcha/*.go
goimports -w captcha/*.go
```

- [ ] **Step 2: Check for any diff**

```bash
git diff captcha/
```

Expected: empty. If non-empty, the formatter found something — stage the changes and amend the previous commit per the user's preference (or commit separately if substantial).

- [ ] **Step 3: Run golangci-lint**

```bash
golangci-lint run ./captcha/...
```

Expected: no warnings or errors.

- [ ] **Step 4: Run full test suite with coverage**

```bash
go test ./captcha/... -cover
```

Expected: every test PASSES; coverage ≥ 85% (per CLAUDE.md). If coverage dipped, the new code paths in `Captcha.Verify` and `RedisStore.Verify` need additional cases — but the test matrix from Tasks 1 and 2 covers all new branches (captchaID match/mismatch/empty at both layers, plus the both-wrong case).

- [ ] **Step 5: Run full repo test suite (regression check)**

```bash
go test ./...
```

Expected: every package PASSES. Nothing outside `captcha/` was touched.

- [ ] **Step 6: Commit if formatter changed anything**

```bash
git status
```

If there are staged or unstaged changes from Step 1:

```bash
git add captcha/
git commit -m "style(captcha): gofmt and goimports"
```

Otherwise skip.

---

## Self-Review Checklist (for the implementer, run after Task 3)

Before declaring done, verify each of these against the spec (`docs/superpowers/specs/2026-06-13-captcha-verify-id-binding-design.md`):

- [ ] **Spec coverage — API Surface:** `VerifyOption` type ✓ (Task 2 Step 1), `WithCaptchaID` func ✓ (Task 2 Step 1), `Captcha.Verify` variadic ✓ (Task 2 Step 2), `Store.Verify` captchaID param ✓ (Task 1 Step 1, Step 3).
- [ ] **Spec coverage — Lua Script Change:** `idMatch = ARGV[2] == "" or ARGV[2] == r.captcha_id` ✓ (Task 1 Step 2).
- [ ] **Spec coverage — Decision 1 (no new status):** script returns existing `matched`/`mismatch`/`exhausted` only ✓.
- [ ] **Spec coverage — Decision 2 (captchaID mismatch consumes attempt):** increment happens before match check ✓; test `TestRedisStore_Verify_captchaIDMismatch` asserts `RemainingAttempts == 2` after one mismatch ✓.
- [ ] **Spec coverage — Decision 3 (captchaID before code):** `if idMatch and r.code == ARGV[1]` short-circuits ✓; test `TestRedisStore_Verify_captchaIDMismatch` confirms correct code + wrong ID = mismatch ✓.
- [ ] **Spec coverage — Decision 4 (empty = skip):** test `TestRedisStore_Verify_captchaIDEmptySkipsCheck` and `TestCaptcha_Verify_withCaptchaIDEmpty` ✓.
- [ ] **Spec coverage — VerifyResult unchanged:** no struct edit; tests assert `result.CaptchaID` echoes the stored value ✓.
- [ ] **Spec coverage — Backwards Compatibility:** existing Captcha tests pass without modification (variadic) ✓; existing store tests pass with trailing `""` added ✓.
- [ ] **Spec coverage — Testing matrix:** all 8 new tests from the spec are present ✓ (4 Store-layer + 3 Captcha-layer + 1 both-wrong).
- [ ] **Symbol ordering (CLAUDE.md):** `VerifyOption`/`WithCaptchaID` in exported section after `WithSend`; `verifyConfig` in unexported section after `generateConfig` ✓.
- [ ] **Coverage:** ≥ 85% ✓.

## 关联

**设计文档：** `docs/superpowers/specs/2026-06-13-captcha-verify-id-binding-design.md`
