# captcha: Verify captchaID Binding

Date: 2026-06-13

## Background

`Captcha.Verify` checks a code against the stored `Record` keyed by
`prefix:purpose:channel:target` (see `store.go:144`). The `captchaID` field,
returned by `Generate` and stored in `Record.CaptchaID`, is currently only
echoed back through `VerifyResult.CaptchaID` — it is **not** a verification
condition.

This opens a specific attack vector: a code generated in one session/context
(e.g., a web browser flow) can be verified in another context (e.g., a mobile
app flow) as long as the attacker has the `target` (phone/email) and the code.
Realistic scenarios where this matters:

- SIM swap attacks — attacker receives the victim's SMS but does not have the
  browser session's `captchaID`.
- Shared phone numbers — family or corporate numbers where one member's code
  could be used against another member's session.
- SMS interception (SS7, base-station hijacking, malicious carrier apps).

The attacker typically obtains the code but not the original session's
`captchaID`. Binding Verify to captchaID closes that gap.

## Goal

Add an **optional** captchaID binding to `Captcha.Verify`: when the caller
passes a captchaID via `WithCaptchaID`, it must match the one returned by the
originating `Generate` call. When the option is omitted, behavior is
unchanged (backwards compatible).

This follows the MessageBird Verify model (optional session-ID binding) rather
than the strict Firebase/Vonage model. Callers opt in based on their threat
model.

## Non-Goals

- Not a silver bullet. captchaID binding complements but does not replace
  HTTPS, CSRF tokens, rate limiting, or application-level session validation.
- No change to the single-active-code-per-target semantics — `Generate` still
  overwrites the stored Record on re-generation for the same
  `(purpose, channel, target)`.
- No new `VerifyResult` status for "captchaID mismatch" — see Decision 1.
- No change to the Redis key layout. captchaID lives inside the Record's JSON
  blob, not in the key.
- No frontend protocol design — the caller (typically a backend service)
  decides how to obtain, transport, and re-supply the captchaID between
  Generate and Verify.

## Design

### API Surface

**Captcha layer (user-facing, backwards-compatible via options pattern):**

```go
type VerifyOption func(*verifyConfig)

// WithCaptchaID enables binding the Verify call to the captchaID returned by
// Generate. If id is empty or the option is not passed, the captchaID check
// is skipped and behavior matches the legacy Verify.
func WithCaptchaID(id string) VerifyOption {
    return func(c *verifyConfig) { c.captchaID = id }
}

func (c *Captcha) Verify(
    ctx context.Context, target, code, purpose, channel string,
    opts ...VerifyOption,
) (*VerifyResult, error)
```

Existing callers passing no options compile and behave identically to today.

**Store layer (internal interface, breaking change):**

```go
type Store interface {
    Set(ctx context.Context, purpose, channel, target string, record *Record, ttl time.Duration) error
    Verify(ctx context.Context, purpose, channel, target, code, captchaID string) (*VerifyResult, error)
}
```

`Captcha.Verify` flattens `verifyConfig.captchaID` to the positional argument
for `Store.Verify`. Store stays positional because it has a single in-package
implementation (`RedisStore`); adding options at this layer would be
over-design.

### Lua Script Change

`RedisStore.verify` gains `ARGV[2]` (captchaID). The match condition becomes:

```lua
local idMatch = ARGV[2] == "" or ARGV[2] == r.captcha_id
if idMatch and r.code == ARGV[1] then
    -- matched
end
```

- `ARGV[2] == ""` → skip captchaID check (caller did not pass WithCaptchaID,
  or passed empty).
- `ARGV[2] == r.captcha_id` → captchaID matches.
- Otherwise → falls through to mismatch.

The mismatch path (increment `r.attempts`, decrement remaining, optionally
delete) is unchanged from today.

### Decisions

1. **No new status code for captchaID mismatch.** Returns the same `mismatch`
   status as a wrong code.
   - *Security:* distinguishing "code wrong" from "captchaID wrong" leaks
     information to attackers enumerating either field.
   - *UX:* caller's recovery action is the same either way — user retries or
     re-initiates the captcha flow.
2. **captchaID mismatch consumes one attempt.** Same as a wrong code;
   `r.attempts` is incremented before the match check (current behavior
   preserved). captchaID is a UUID (128 bits), so brute-force is infeasible —
   no realistic risk of attackers exhausting legitimate attempts via random
   captchaIDs.
3. **Order of checks: captchaID before code.** If captchaID is wrong, code
   correctness is irrelevant; the call returns mismatch. captchaID is a
   binding condition, not a tiebreaker. Implemented as a single boolean
   expression `idMatch and r.code == ARGV[1]`, so this ordering is implicit
   in the short-circuit.
4. **Empty string convention.** `""` means "do not check". Both
   `WithCaptchaID("")` and absence of the option produce identical behavior.
   The Lua script uses `ARGV[2] == ""` for the skip check.

### VerifyResult

Unchanged. The struct already has a `CaptchaID` field, populated from the
stored Record on every code path (matched, mismatch, exhausted). The
caller-supplied captchaID is never echoed back — only the stored one is.

### Error Handling

Unchanged. Redis errors, script errors, and JSON unmarshal errors are wrapped
and returned exactly as today. No new error paths.

## Backwards Compatibility

| Surface | Breaking? | Migration |
|---|---|---|
| `Captcha.Verify` | No (variadic option) | Existing calls compile and behave identically. |
| `Store` interface | Yes | Any external implementor must add the `captchaID string` parameter; pass `""` for legacy behavior. |

The single in-package `Store` implementation (`RedisStore`) is updated in the
same change. No external implementors are known.

## Testing

All tests use `redisx.NewTestClient` (miniredis).

### Captcha layer (`captcha_test.go`)

- `Verify` without option → regression; identical to today's behavior.
- `Verify` with `WithCaptchaID(correctID)` + correct code → matched.
- `Verify` with `WithCaptchaID(wrongID)` + correct code → mismatch;
  `RemainingAttempts` decremented by 1.
- `Verify` with `WithCaptchaID("")` → equivalent to no option.

### Store layer (`store_test.go`)

- Lua: captchaID="" + correct code → matched.
- Lua: captchaID=correct + correct code → matched.
- Lua: captchaID=wrong + correct code → mismatch, attempts incremented.
- Lua: captchaID=wrong + wrong code → mismatch, attempts incremented **once**
  (not twice).
- Lua: captchaID path triggers the `max_attempts` exhaustion branch.

Coverage target: maintain 85%. The new logic is one boolean condition plus
one option type; existing coverage is unaffected.

## Migration

1. Add `VerifyOption` / `WithCaptchaID` / `verifyConfig` to `captcha.go`.
2. Change `Captcha.Verify` signature to variadic, translate `verifyConfig`
   to the captchaID positional argument for `Store.Verify`.
3. Change `Store.Verify` signature; update `RedisStore.Verify` and its Lua
   script to accept `ARGV[2]`.
4. Update existing in-package tests (none call `Store.Verify` directly with
   captchaID yet).
5. `go test ./... -cover` to confirm coverage and behavior.

No deprecation shim — the only affected internal interface is `Store`, which
has a single in-package implementor updated in the same change.

## CHANGELOG

- **Added**: `captcha.WithCaptchaID` option for `Captcha.Verify`, enabling
  session binding to defend against cross-context code-reuse attacks
  (SIM swap, shared numbers, SMS interception).
- **Breaking** (internal `Store` interface): `Store.Verify` gains a
  `captchaID string` parameter; pass `""` to skip the check.

## 关联

**相关设计：**
- [[2026-05-22-captcha-design|captcha 初始设计]]

**实现计划：** [docs/superpowers/plans/2026-06-13-captcha-verify-id-binding.md](../plans/2026-06-13-captcha-verify-id-binding.md)（commit `8b916ce`..`0e4059c` 已合并到 main）
