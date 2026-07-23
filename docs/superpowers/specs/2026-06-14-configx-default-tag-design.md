# configx: Default Values from Struct Tags

Date: 2026-06-14

## Background

`configx.Load` currently sets defaults via the `WithDefaults(map[string]any)`
option, where the caller passes a flat dotted-key map (`"server.grpc_addr":
":9000"`). This works but separates defaults from the field definitions —
adding a field means also remembering to add a map entry, and the map's
string keys aren't checked at compile time.

The cleaner pattern is declarative defaults on the struct itself via tags:

```go
type Config struct {
    Server struct {
        GRPCAddr string `default:":9000"`
    }
    ReadTimeout time.Duration `default:"30s"`
}
```

The tag form keeps defaults co-located with fields, makes them visible in
godoc, and eliminates the dotted-key map bookkeeping.

## Goal

Add automatic `default:"<value>"` tag support to `configx.Load`. When the
caller passes a struct, reflect over it, collect every `default` tag into a
`dottedKey → "value"` map, and feed it to `viper.SetDefault` — the same
pipeline `WithDefaults` used. Then remove `WithDefaults` entirely, since
the tag form covers the same use case more elegantly.

## Non-Goals

- No new option for tag defaults (auto-enabled).
- No support for map, channel, func, or interface field defaults — these
  have no clean string representation.
- No new error variants. Type-conversion failures bubble up through the
  existing `ErrUnmarshal` path.
- No external library dependency (e.g., `github.com/creasty/defaults`).
  The reflection logic is small enough to inline.
- No backwards-compatibility shim for `WithDefaults`. The single in-repo
  caller (`configx_test.go`) is migrated in the same change.
- No handling for nil pointer fields — they are skipped (see Decision 4).

## Design

### API Surface

**New: struct tag `default:"<value>"`.** Auto-enabled in `Load`, no option
needed. Empty tag (`default:""`) is treated as not set.

**Removed: `WithDefaults` option and `loader.defaults` field.** Migrate by
moving the map entries to struct tags on the corresponding fields.

**Unchanged:** `Load` signature, `LoadOption` type, all other options, all
error variants in `errors.go`.

### Reflection Rules

A new unexported function in a new file `defaults.go`:

```go
// collectDefaults walks target (expected to be a non-nil pointer to a struct)
// and returns a map of dotted snake_case keys to default tag string values.
// Returns an empty map if target is not a struct pointer (the subsequent
// viper.Unmarshal will surface that as ErrUnmarshal).
func collectDefaults(target any) map[string]any
```

Walking is **type-based**, not value-based. `walkStruct` takes a `reflect.Type`
and recurses through field declarations without ever reading field values.
This means nil pointer-to-struct fields are walked transparently — defaults
are a static property of the type, not the runtime value. Viper allocates
nil pointers during unmarshal as needed.

Walk rules:

| Case | Behavior |
|---|---|
| target not a struct pointer / nil | Return empty map |
| struct | Recurse each field |
| non-anonymous field, non-struct type, has `default:"x"` tag | Record `dottedKey → "x"` |
| non-anonymous field, struct or pointer-to-struct type, has tag | Tag ignored (no scalar meaning) |
| field is struct (or pointer-to-struct) | Recurse; key path extended by field name |
| embedded (anonymous) field | Walk transparently, do not add type name to key path |
| unexported field | Skip |
| `default:""` empty tag | Treat as not set, skip |
| tag value type-mismatches field | Don't error here — pass through; viper.Unmarshal reports `ErrUnmarshal` |

### snake_case helper

```go
// toSnakeCase converts CamelCase to snake_case.
//	GRPCAddr     → grpc_addr
//	HTTPServer   → http_server
//	Simple       → simple
//	OAuth2       → oauth2
func toSnakeCase(s string) string
```

Algorithm (standard CamelCase → snake_case, ~15 lines):
- Consecutive uppercase letters form one word: `HTTP` → `http`.
- An uppercase letter followed by a lowercase letter starts a new word
  when preceded by another uppercase: `HTTPServer` → `http_server`.
- Digits attach to the preceding word: `OAuth2` → `oauth2`.
- Output is all lowercase, words joined by `_`.

### Load Integration

In `configx.go`, replace the current defaults loop (~lines 110-113):

```go
// Set defaults.
for key, val := range l.defaults {
    v.SetDefault(key, val)
}
```

with:

```go
// Set defaults from struct tags.
for key, val := range collectDefaults(target) {
    v.SetDefault(key, val)
}
```

Tag values are passed to `viper.SetDefault` as raw strings. Viper's
existing decode hooks (already wired in `buildDecodeHook`) handle the
string-to-field-type conversion during `v.Unmarshal`:

- `mapstructure.StringToTimeDurationHookFunc` → `"30s"` becomes `time.Duration`
- `mapstructure.StringToSliceHookFunc(",")` → `"a,b,c"` becomes `[]string`
- `mapstructure.StringToTimeHookFunc(time.RFC3339)` → RFC3339 strings become `time.Time`
- Viper's built-in weak typing → `"3"` becomes `int 3`, `"true"` becomes `bool true`

No new decode hooks, no new conversion code.

### Documentation

The package-level doc comment in `configx.go` gains a new section
**Default Values from Tags** that documents:

1. The `default` tag syntax and auto-enable behavior
2. CamelCase → dotted snake_case key translation (with examples)
3. Supported field types (string, int, bool, time.Duration, []string, time.Time)
4. Walk rules: nested structs, embedded fields, pointer-to-struct (nil
   walked transparently via type domain), unexported fields, struct/pointer-
   to-struct tag-ignored vs primitive tag-recorded
5. Empty tag is treated as not set
6. Type mismatch surfaces as `ErrUnmarshal` (no new error variant)
7. Note that `WithDefaults` is removed; use tags instead

`defaults.go` gets a file-level comment and function doc comments on
`collectDefaults` and `toSnakeCase` covering the algorithm essentials
(without repeating the package doc).

### Decisions

1. **Auto-enabled, no option.** Existing callers without tags are
   unaffected (no tag = no default = no behavior change). Adding an option
   for this would be YAGNI.
2. **`viper.SetDefault` is the integration point.** Same pipeline as the
   removed `WithDefaults`, so config file and env overrides continue to
   work with the same priority. No new code path.
3. **Strings passed verbatim; conversion delegated to viper's hooks.**
   Reuses the existing decode infrastructure. No type-specific branching
   in our reflection code.
4. **Type-based walking, nil pointers transparent.** The walker operates
   on `reflect.Type`, not `reflect.Value`. This means nil pointer-to-struct
   fields are walked through, and their inner-field defaults are collected.
   Viper allocates nil pointers during unmarshal. The alternative (value-
   based walking requiring non-nil pointers) would force callers to
   initialize every nested pointer just to get defaults, which is fragile.
5. **`collectDefaults` returns no error.** Non-struct targets produce an
   empty map; the subsequent `viper.Unmarshal` reports the structural
   problem via `ErrUnmarshal`. One less error path.
6. **Remove `WithDefaults` outright, no deprecation.** This is an internal
   foundation library; the only in-repo caller is a test, migrated in the
   same change. Deprecation would just delay the cleanup.
7. **New file `defaults.go`.** Keeps reflection logic isolated from the
   Load pipeline. `configx.go` stays focused on file/env/decode concerns.

## Testing

All tests use the existing configx test helpers (writing temp config files,
setting env vars). New file `defaults_test.go`.

### `collectDefaults` unit tests

- `TestCollectDefaults_basic` — flat struct with mixed-type tagged fields;
  assert map has all expected dotted keys and raw string values.
- `TestCollectDefaults_nested` — nested struct; assert dotted key paths
  combine correctly.
- `TestCollectDefaults_embedded` — embedded anonymous struct; assert
  embedded type name does NOT appear in key path.
- `TestCollectDefaults_pointerToStructWalkedByType` — `*Config` field,
  verify both nil and non-nil produce identical results (type-based walk).
- `TestCollectDefaults_primitivePointer` — `*time.Duration` field with
  tag; assert tag is recorded regardless of nil-ness.
- `TestCollectDefaults_unexportedSkipped` — unexported field with tag;
  assert it does not appear.
- `TestCollectDefaults_unsupportedTypesSkipped` — map, channel, func,
  interface fields with tags; assert their tags are recorded but know
  that viper will later fail to convert (the Load-level test in Task 3
  verifies the failure path).
- `TestCollectDefaults_emptyTagIgnored` — `default:""` is treated as not set.
- `TestCollectDefaults_nonStructTarget` — `*int`, `nil`, non-pointer;
  assert returns empty map without panicking.

### `toSnakeCase` unit tests

- Table-driven: `GRPCAddr` → `grpc_addr`, `HTTPServer` → `http_server`,
  `Simple` → `simple`, `OAuth2` → `oauth2`, `ReadTimeout` → `read_timeout`,
  empty string → empty string, single char → lowercase.

### End-to-end `Load` tests

- `TestLoad_tagDefaultsApplied` — struct with tags; write a minimal config
  file (e.g. just one field set) so Load resolves a file but leaves the
  tagged fields unset; assert tagged fields are populated with defaults.
- `TestLoad_fileOverridesTagDefault` — config file sets a value; assert
  file value wins over tag default.
- `TestLoad_envOverridesTagDefault` — env var sets a value; assert env
  wins over tag default.
- `TestLoad_tagTypeConversion` — tag `"30s"` on `time.Duration` field,
  `"a,b"` on `[]string` field, `"42"` on `int` field, `"true"` on `bool`
  field; assert all convert correctly via existing hooks.
- `TestLoad_tagInvalidValue` — tag `"abc"` on `int` field; assert Load
  returns `ErrUnmarshal`.

### Migration test

- The existing `TestLoad_withDefaults` (or equivalent) is rewritten to use
  struct tags instead of the `WithDefaults` option. Same assertions, just
  different mechanism.

Coverage target: maintain 85%. The new code is reflection + a small
helper; the test matrix above covers every branch.

## Migration

In-repo changes:

1. Add `configx/defaults.go` with `collectDefaults` and `toSnakeCase`.
2. Modify `configx/configx.go`:
   - Add the "Default Values from Tags" section to the package doc comment.
   - Replace the `l.defaults` loop with `collectDefaults(target)` loop.
3. Modify `configx/options.go`: delete `WithDefaults`.
4. Modify `configx/configx.go` `loader` struct: delete `defaults` field.
5. Modify `configx/configx_test.go`: migrate any test using `WithDefaults`
   to use tags instead.
6. Add `configx/defaults_test.go` with the new test matrix.
7. `gofmt -w`, `goimports -w`, `golangci-lint run ./configx/...`,
   `go test ./configx/... -cover`.

External callers of `WithDefaults` will see a compile error pointing at
the deleted function. The fix is mechanical: convert the map entries to
`default:"<value>"` tags on the corresponding struct fields.

## CHANGELOG

- **Added**: `default:"<value>"` struct tag support in `configx.Load`.
  Tagged fields automatically populate viper defaults; type conversion
  reuses the existing decode hooks (Duration, slice, time, weak typing).
- **Removed** (breaking): `configx.WithDefaults` option. Migrate by
  moving map entries to `default:"..."` tags on the corresponding fields.

## 关联

**相关设计：**
- [[services/go-common/design/v1/captcha-design|captcha-design]] （captcha 包也用类似的硬编码默认值模式，未来可同样迁移到 tag-based defaults）

**实现计划：** 待编写（`docs/superpowers/plans/2026-06-14-configx-default-tag.md`）
