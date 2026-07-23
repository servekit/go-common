# configx: Default Values from Struct Tags Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add automatic `default:"<value>"` struct tag support to `configx.Load`. Reflection collects tagged fields and feeds them to `viper.SetDefault`, reusing the existing decode hooks for type conversion. Remove `WithDefaults` option entirely (tag form replaces it).

**Architecture:** New file `configx/defaults.go` holds two unexported helpers: `collectDefaults(target any) map[string]any` (recursive struct walker that builds dotted-snake-case keys → tag string values) and `toSnakeCase(s string) string` (CamelCase → snake_case converter). Walking is **type-based**, not value-based — nil pointers are walked transparently because defaults are a static property of the type, not the runtime value. `Load` calls `collectDefaults(target)` and iterates the result into `v.SetDefault`, replacing the old `l.defaults` map. `WithDefaults` and the `loader.defaults` field are deleted; type conversion is fully delegated to viper's existing decode hooks.

**Tech Stack:** Go stdlib (`reflect`, `strings`, `unicode`), existing `github.com/spf13/viper` and `github.com/go-viper/mapstructure/v2`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-14-configx-default-tag-design.md`

---

## File Structure

**Created files:**

| File | Responsibility |
|---|---|
| `configx/defaults.go` | `collectDefaults` (recursive struct walker) + `toSnakeCase` (case converter); both unexported |
| `configx/defaults_test.go` | Unit tests for both helpers |

**Modified files:**

| File | Responsibility |
|---|---|
| `configx/configx.go` | Replace `l.defaults` loop with `collectDefaults(target)` loop; add "Default Values from Tags" section to package doc |
| `configx/options.go` | Delete `WithDefaults` function |
| `configx/configx_test.go` | Migrate `TestLoad_Defaults` from `WithDefaults` map to struct tags (new dedicated `TagDefaultsConfig` type); add new Load-level tests |

**Deleted symbols:**
- `WithDefaults` function (options.go)
- `loader.defaults` field (configx.go)

**Not modified:**
- `errors.go` — no new error variants (type-mismatch failures bubble through existing `ErrUnmarshal`).

---

## Task 1: Implement `defaults.go` (helpers + unit tests)

Pure-function foundation. No integration with Load yet — both helpers are unexported and called only from tests in this task.

**Files:**
- Create: `configx/defaults.go`
- Create: `configx/defaults_test.go`

- [ ] **Step 1: Write the failing tests for `toSnakeCase`**

Create `configx/defaults_test.go`:

```go
package configx

import (
	"reflect"
	"testing"
	"time"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"a", "a"},
		{"A", "a"},
		{"Simple", "simple"},
		{"GRPCAddr", "grpc_addr"},
		{"HTTPServer", "http_server"},
		{"OAuth", "oauth"},
		{"OAuth2", "oauth2"},
		{"ReadTimeout", "read_timeout"},
		{"HTTPSProxy", "https_proxy"},
		{"UserID", "user_id"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := toSnakeCase(tt.in); got != tt.want {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./configx/ -run TestToSnakeCase -v
```

Expected: FAIL with "undefined: toSnakeCase".

- [ ] **Step 3: Implement `toSnakeCase`**

Create `configx/defaults.go`:

```go
// Package-level implementation lives here. See package doc in configx.go
// for how the resulting defaults flow into viper.

package configx

import (
	"reflect"
	"strings"
	"unicode"
)

// collectDefaults walks target (expected to be a non-nil pointer to a struct)
// and returns a map of dotted snake_case keys to default tag string values.
// Returns an empty map if target is not a struct pointer; the subsequent
// viper.Unmarshal will surface that case as ErrUnmarshal.
//
// Walking is type-based, not value-based: nil pointer-to-struct fields are
// still walked (defaults are a static property of the type, not the value).
//
// Walk rules (see "Default Values from Tags" in the package doc):
//   - Nested structs are walked recursively; key paths are dotted.
//   - Embedded (anonymous) fields are walked transparently — the embedded
//     type name is NOT added to the key path.
//   - Tags on struct or pointer-to-struct fields are ignored (no scalar
//     default form); walk into them to collect inner-field defaults.
//   - Tags on primitive or pointer-to-primitive fields are recorded.
//   - Unexported fields are skipped.
//   - An empty `default:""` tag is treated as not set.
func collectDefaults(target any) map[string]any {
	out := make(map[string]any)
	t := reflect.TypeOf(target)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return out
	}
	walkStruct(t, "", out)
	return out
}

// walkStruct recurses through a struct type, recording default-tagged fields
// into out. prefix is the dotted key path accumulated so far.
func walkStruct(t reflect.Type, prefix string, out map[string]any) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		// Compute this field's key prefix.
		fieldKey := prefix
		if !field.Anonymous {
			if fieldKey != "" {
				fieldKey += "."
			}
			fieldKey += toSnakeCase(field.Name)
		}

		// Underlying type (deref pointer in the type domain).
		ft := field.Type
		underlying := ft
		if underlying.Kind() == reflect.Ptr {
			underlying = underlying.Elem()
		}

		// Record tag for non-anonymous, non-struct fields. Tags on struct
		// or pointer-to-struct fields don't have a scalar meaning — those
		// fields are containers, and defaults belong on their inner fields.
		if !field.Anonymous && underlying.Kind() != reflect.Struct {
			if def, ok := field.Tag.Lookup("default"); ok && def != "" {
				out[fieldKey] = def
			}
		}

		// Recurse into struct-like fields (struct or pointer-to-struct).
		// Nil pointer-to-struct is walked transparently via type domain.
		if underlying.Kind() == reflect.Struct {
			walkStruct(underlying, fieldKey, out)
		}
	}
}

// toSnakeCase converts CamelCase to snake_case.
//
//	GRPCAddr     → grpc_addr
//	HTTPServer   → http_server
//	OAuth        → oauth
//	OAuth2       → oauth2
//	Simple       → simple
//
// Algorithm:
//   - Insert '_' between [lowercase/digit] and uppercase: simpleField → simple_field.
//   - Insert '_' between consecutive uppercase letters when the last one
//     starts a new word (current is upper, next is lower, previous two
//     chars are upper): HTTPServer → http_server.
//   - Digits attach to the preceding word.
//   - Output is all lowercase.
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var sb strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				sb.WriteRune('_')
			} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) &&
				i >= 2 && unicode.IsUpper(runes[i-2]) {
				sb.WriteRune('_')
			}
		}
		sb.WriteRune(unicode.ToLower(r))
	}
	return sb.String()
}
```

- [ ] **Step 4: Run `toSnakeCase` tests to verify they pass**

```bash
go test ./configx/ -run TestToSnakeCase -v
```

Expected: every subtest PASSES.

- [ ] **Step 5: Write the failing tests for `collectDefaults`**

Append to `configx/defaults_test.go`:

```go
// --- collectDefaults test types ---

type defaultsNested struct {
	Inner struct {
		Field string `default:"inner-value"`
	}
}

type defaultsEmbedded struct {
	Embedded `default:"ignored-on-anonymous"`
}

type Embedded struct {
	EmbeddedField string `default:"embedded-value"`
}

type defaultsPointer struct {
	// Ptr is pointer-to-struct; its own tag (if any) is ignored —
	// struct/pointer-to-struct fields have no scalar default form.
	// Inner-field defaults are still collected via type-based walking.
	Ptr *defaultsNested
}

type defaultsPrimitivePtr struct {
	// Timeout is pointer-to-primitive; its tag IS recorded.
	Timeout *time.Duration `default:"30s"`
}

type defaultsMixed struct {
	String string `default:"s"`
	Int    int    `default:"42"`
	Bool   bool   `default:"true"`
	Skip   string // no tag
	private string `default:"ignored"`
}

type defaultsUnsupported struct {
	Map map[string]string `default:"{}"`
	Fn  func()            `default:""`
}

// --- collectDefaults tests ---

func TestCollectDefaults_basic(t *testing.T) {
	var cfg defaultsMixed
	got := collectDefaults(&cfg)
	want := map[string]any{
		"string": "s",
		"int":    "42",
		"bool":   "true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_nested(t *testing.T) {
	var cfg defaultsNested
	got := collectDefaults(&cfg)
	want := map[string]any{
		"inner.field": "inner-value",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_embedded(t *testing.T) {
	var cfg defaultsEmbedded
	got := collectDefaults(&cfg)
	want := map[string]any{
		"embedded_field": "embedded-value",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_pointerToStructWalkedByType(t *testing.T) {
	// Walking is type-based, so nil and non-nil pointers produce identical
	// results — defaults are a static property of the type.
	var nilCfg defaultsPointer
	nilGot := collectDefaults(&nilCfg)

	nonNilCfg := defaultsPointer{Ptr: &defaultsNested{}}
	nonNilGot := collectDefaults(&nonNilCfg)

	want := map[string]any{
		"ptr.inner.field": "inner-value",
	}
	if !reflect.DeepEqual(nilGot, want) {
		t.Errorf("nil pointer: got %v, want %v", nilGot, want)
	}
	if !reflect.DeepEqual(nonNilGot, want) {
		t.Errorf("non-nil pointer: got %v, want %v", nonNilGot, want)
	}
}

func TestCollectDefaults_primitivePointer(t *testing.T) {
	// Tags on pointer-to-primitive fields ARE recorded; viper dereferences
	// (and allocates if needed) during unmarshal.
	var cfg defaultsPrimitivePtr // Timeout is nil
	got := collectDefaults(&cfg)
	want := map[string]any{
		"timeout": "30s",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_unexportedSkipped(t *testing.T) {
	cfg := defaultsMixed{private: "x"}
	got := collectDefaults(&cfg)
	for k := range got {
		if strings.Contains(k, "private") {
			t.Errorf("unexported field leaked into defaults: %q", k)
		}
	}
}

func TestCollectDefaults_unsupportedTypes(t *testing.T) {
	var cfg defaultsUnsupported
	got := collectDefaults(&cfg)
	// Map's tag (`"{}"`) is non-empty and recorded; viper will later fail
	// to convert it, surfacing as ErrUnmarshal. Fn's tag is empty → skipped.
	want := map[string]any{
		"map": "{}",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_nonStructTarget(t *testing.T) {
	// *int — not a struct pointer.
	n := 42
	got := collectDefaults(&n)
	if len(got) != 0 {
		t.Errorf("expected empty map for *int target, got %v", got)
	}

	// nil pointer.
	got = collectDefaults((*defaultsMixed)(nil))
	if len(got) != 0 {
		t.Errorf("expected empty map for nil target, got %v", got)
	}

	// Non-pointer.
	got = collectDefaults("string")
	if len(got) != 0 {
		t.Errorf("expected empty map for string target, got %v", got)
	}
}
```

Add `"strings"` to the imports if not already present.

- [ ] **Step 6: Run tests to verify they fail (or pass if implementation is right)**

```bash
go test ./configx/ -run TestCollectDefaults -v
```

Expected: PASS — the implementation from Step 3 already covers all these cases.

Note: some of the assertions above were drafted during test writing and confirmed against the implementation. If any test fails, the failure is informative — re-read the walk rules in the spec (`docs/superpowers/specs/2026-06-14-configx-default-tag-design.md`).

- [ ] **Step 7: Commit**

```bash
git add configx/defaults.go configx/defaults_test.go
git commit -m "feat(configx): add collectDefaults and toSnakeCase helpers

Reflection-based struct walker that collects 'default:\"<value>\"' tags
into a dotted-snake-case key map. The map is intended to feed
viper.SetDefault in a subsequent task. toSnakeCase handles standard
CamelCase conventions including consecutive-uppercase runs (HTTPServer →
http_server) and digit suffixes (OAuth2 → oauth2)."
```

---

## Task 2: Wire `collectDefaults` into `Load`; remove `WithDefaults`

Breaking API change. Do the migration in the same commit so the package keeps compiling.

**Files:**
- Modify: `configx/configx.go` (Load body lines 110-113; package doc; `loader` struct definition)
- Modify: `configx/options.go` (delete `WithDefaults`)
- Modify: `configx/configx_test.go` (migrate `TestLoad_Defaults`)

- [ ] **Step 1: Replace the defaults loop in `configx/configx.go`**

Find lines 110-113 in `configx/configx.go`:

```go
	// Set defaults.
	for key, val := range l.defaults {
		v.SetDefault(key, val)
	}
```

Replace with:

```go
	// Set defaults from struct tags.
	for key, val := range collectDefaults(target) {
		v.SetDefault(key, val)
	}
```

- [ ] **Step 2: Remove the `defaults` field from the `loader` struct**

Find the `loader` struct (around lines 142-149) and delete the `defaults` field:

```go
type loader struct {
	serviceName string
	envPrefix   string
	configName  string
	configPaths []string
	decodeHooks []mapstructure.DecodeHookFunc
}
```

- [ ] **Step 3: Delete `WithDefaults` from `configx/options.go`**

Open `configx/options.go` and delete the `WithDefaults` function entirely (it's around lines 37-43):

```go
// WithDefaults sets viper default values.
func WithDefaults(m map[string]any) LoadOption {
	return func(l *loader) {
		l.defaults = m
	}
}
```

Also delete the `import "github.com/go-viper/mapstructure/v2"` from options.go if it becomes unused after this deletion (check whether other options still reference `mapstructure` — they do, via `WithDecodeHooks`, so the import stays).

- [ ] **Step 4: Add the "Default Values from Tags" section to the package doc**

In `configx/configx.go`, find the end of the existing package doc (the closing `*/` of the doc comment is around line 62). Insert this new section just before the existing `# Struct Mapping` section, or at the very end before the closing `*/` — placement should keep related sections together. Recommended: insert after `# Environment Variable Override` and before `# Struct Mapping`.

Insert this block:

```go
// # Default Values from Tags
//
// Struct fields may carry a `default:"<value>"` tag. When Load is called, all
// tagged fields are collected via reflection and fed to viper's SetDefault
// with the same priority as any other default — config file, env vars, and
// explicit overrides all take precedence.
//
// A CamelCase field path is converted to a dotted snake_case key, matching
// the same naming scheme used for file/env lookups:
//
//	Server.GRPCAddr        → server.grpc_addr
//	Log.Level             → log.level
//	HTTPServer.ReadTimeout → http_server.read_timeout
//
// The tag value is a string. Type conversion reuses viper's existing decode
// hooks, so the following all work without extra configuration:
//
//	GRPCAddr     string         `default:":9000"`
//	Retries      int            `default:"3"`
//	FeatureX     bool           `default:"true"`
//	ReadTimeout  time.Duration  `default:"30s"`
//	Hosts        []string       `default:"localhost,backup.local"`
//	BornAt       time.Time      `default:"2006-01-02T15:04:05Z"`
//
// Rules and edge cases:
//
//   - Nested structs are walked recursively; key paths are dotted.
//   - Embedded (anonymous) fields are walked transparently — the embedded
//     type name is NOT added to the key path.
//   - Pointer-to-struct fields are walked through transparently (by type,
//     not value), so nil pointers still contribute their inner-field
//     defaults. Viper allocates nil pointers during unmarshal.
//   - Tags on struct or pointer-to-struct fields are ignored (no scalar
//     default form); put defaults on the inner fields instead.
//   - Tags on primitive or pointer-to-primitive fields are recorded.
//   - Unexported fields are skipped.
//   - Map, channel, func, and interface fields: tags are recorded, but
//     viper will fail to convert them, surfacing as ErrUnmarshal.
//   - Slice/array fields use viper's StringToSliceHookFunc, splitting on
//     commas: `default:"a,b,c"` → []string{"a","b","c"}.
//   - An empty tag (`default:""`) is treated as not set.
//   - If the tag value cannot convert to the field type, Load returns
//     ErrUnmarshal (the existing error path; no new error variant).
//
```

- [ ] **Step 5: Migrate the existing `TestLoad_Defaults` to use struct tags**

In `configx/configx_test.go`, replace the existing `TestLoad_Defaults` (around lines 188-207):

```go
func TestLoad_Defaults(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
log:
  level: info
  format: json
`)
	chdir(t, dir)

	var cfg FullConfig
	require.NoError(t, Load(&cfg,
		WithDefaults(map[string]any{
			"server.grpc_addr": ":9090",
			"server.http_addr": ":8080",
		}),
	))
	require.Equal(t, ":9090", cfg.Server.GRPCAddr)
	require.Equal(t, ":8080", cfg.Server.HTTPAddr)
	require.Equal(t, "info", cfg.Log.Level)
}
```

with a new version using a dedicated config type with tags:

```go
// TagDefaultsConfig is a dedicated config type for tag-defaults tests.
// We use a separate type rather than modifying ServerConfig so other tests
// that depend on ServerConfig's zero-value behavior are not affected.
type TagDefaultsConfig struct {
	Server struct {
		GRPCAddr string `default:":9090"`
		HTTPAddr string `default:":8080"`
	}
	Log LogConfig
}

func TestLoad_Defaults(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
log:
  level: info
  format: json
`)
	chdir(t, dir)

	var cfg TagDefaultsConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":9090", cfg.Server.GRPCAddr)
	require.Equal(t, ":8080", cfg.Server.HTTPAddr)
	require.Equal(t, "info", cfg.Log.Level)
}
```

Place the `TagDefaultsConfig` type declaration at the top of the file near the other test types (around line 40), not inside the test function. The test function goes where the old `TestLoad_Defaults` was.

- [ ] **Step 6: Verify build compiles**

```bash
go build ./configx/...
```

Expected: success. No references to `WithDefaults` or `l.defaults` should remain.

If the build fails, search for any remaining references:

```bash
grep -rn "WithDefaults\|l\.defaults" /Users/moss/code/base/go-common/configx/
```

Expected: only matches in this plan file (which is outside the configx directory) or none.

- [ ] **Step 7: Run all configx tests**

```bash
go test ./configx/... -v
```

Expected: every test PASSES, including the migrated `TestLoad_Defaults`. The migration should be behaviorally identical: tags now provide what the map used to.

- [ ] **Step 8: Commit**

```bash
git add configx/configx.go configx/options.go configx/configx_test.go
git commit -m "feat(configx): wire default-tag reflection into Load

Load now collects 'default:\"<value>\"' tags from the target struct and
feeds them to viper.SetDefault, reusing the existing decode hooks for
type conversion. WithDefaults option is removed — tag form covers the
same use case more declaratively. TestLoad_Defaults migrated to tags.

BREAKING: WithDefaults(map[string]any) removed. Migrate by moving map
entries to 'default:\"<value>\"' tags on the corresponding struct fields."
```

---

## Task 3: Add Load-level integration tests (override, type conversion, error)

Verify the full pipeline works: file overrides defaults, env overrides defaults, type conversion via hooks, and type mismatch surfaces as ErrUnmarshal.

**Files:**
- Modify: `configx/configx_test.go` (add 4 new tests)

- [ ] **Step 1: Add test for file overriding tag defaults**

Append to `configx/configx_test.go`:

```go
func TestLoad_FileOverridesTagDefault(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: ":7000"
`)
	chdir(t, dir)

	var cfg TagDefaultsConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":7000", cfg.Server.GRPCAddr) // from file
	require.Equal(t, ":8080", cfg.Server.HTTPAddr) // from tag (not in file)
}

func TestLoad_EnvOverridesTagDefault(t *testing.T) {
	dir := tempDir(t)
	// Minimal config file so Load resolves successfully.
	writeTempConfig(t, dir, "config.yaml", `
log:
  level: debug
`)
	chdir(t, dir)
	setenv(t, "SERVER_GRPC_ADDR", ":6000")

	var cfg TagDefaultsConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":6000", cfg.Server.GRPCAddr) // from env
	require.Equal(t, ":8080", cfg.Server.HTTPAddr) // from tag
}
```

- [ ] **Step 2: Add test for type conversion through tags**

Append to `configx/configx_test.go`:

```go
// TagTypesConfig exercises viper's decode hooks via tag defaults.
type TagTypesConfig struct {
	Timeout time.Duration `default:"30s"`
	Labels  []string      `default:"a,b,c"`
	Retries int           `default:"5"`
	Verbose bool          `default:"true"`
}

func TestLoad_TagTypeConversion(t *testing.T) {
	dir := tempDir(t)
	// Empty-ish config so all values come from tags.
	writeTempConfig(t, dir, "config.yaml", ``)
	chdir(t, dir)

	var cfg TagTypesConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, 30*time.Second, cfg.Timeout)
	require.Equal(t, []string{"a", "b", "c"}, cfg.Labels)
	require.Equal(t, 5, cfg.Retries)
	require.True(t, cfg.Verbose)
}
```

- [ ] **Step 3: Add test for tag-value/field-type mismatch surfacing as ErrUnmarshal**

Append to `configx/configx_test.go`:

```go
// BadTagConfig has a default that cannot convert to the field type.
type BadTagConfig struct {
	Port int `default:"not-a-number"`
}

func TestLoad_TagInvalidValue(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", ``)
	chdir(t, dir)

	var cfg BadTagConfig
	err := Load(&cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnmarshal)
}
```

- [ ] **Step 4: Run all new tests**

```bash
go test ./configx/ -run "TestLoad_FileOverridesTagDefault|TestLoad_EnvOverridesTagDefault|TestLoad_TagTypeConversion|TestLoad_TagInvalidValue" -v
```

Expected: all 4 PASS.

- [ ] **Step 5: Run the full configx test suite**

```bash
go test ./configx/... -v
```

Expected: every test PASSES.

- [ ] **Step 6: Commit**

```bash
git add configx/configx_test.go
git commit -m "test(configx): add tag-default integration tests

Cover file/env override of tag defaults, type conversion via decode hooks
(Duration, slice, int, bool), and the type-mismatch error path."
```

---

## Task 4: Format, lint, coverage

Final polish. No behavior change.

**Files:** None modified directly; verifies Tasks 1-3 output.

- [ ] **Step 1: Run gofmt and goimports**

```bash
gofmt -w configx/*.go
goimports -w configx/*.go
```

- [ ] **Step 2: Check for any diff**

```bash
git diff configx/
```

Expected: empty. If non-empty, the formatter found something — investigate and stage.

- [ ] **Step 3: Run golangci-lint**

```bash
golangci-lint run ./configx/...
```

Expected: no warnings or errors.

- [ ] **Step 4: Run configx tests with coverage**

```bash
go test ./configx/... -cover
```

Expected: every test PASSES; coverage ≥ 85% (per CLAUDE.md).

- [ ] **Step 5: Run full repo test suite**

```bash
go test ./...
```

Expected: every package PASSES. Pre-existing failures (`cronx/TestOnlyWorkdays_Integration`, `dbx/TestAutoMigrate`) are environment-related and unaffected by this work.

- [ ] **Step 6: Commit if formatter changed anything**

```bash
git status
```

If changes from Step 1:

```bash
git add configx/
git commit -m "style(configx): gofmt and goimports"
```

Otherwise skip.

---

## Self-Review Checklist (for the implementer, run after Task 4)

Verify each against the spec (`docs/superpowers/specs/2026-06-14-configx-default-tag-design.md`):

- [ ] **Spec coverage — API Surface:** `default:"<value>"` tag honored ✓ (Task 1 Step 3 + Task 2 Step 1); `WithDefaults` removed ✓ (Task 2 Step 3); `loader.defaults` field removed ✓ (Task 2 Step 2).
- [ ] **Spec coverage — Reflection rules:** all walk cases tested ✓ (Task 1 Step 5 covers nested, embedded, pointer-to-struct (nil+non-nil), primitive pointer, unexported, unsupported types, non-struct target).
- [ ] **Spec coverage — snake_case:** all spec examples in test matrix ✓ (Task 1 Step 1).
- [ ] **Spec coverage — Decisions 1-7:** auto-enable ✓; viper.SetDefault is integration ✓; strings passed verbatim ✓; type-based walking with nil-pointer transparency ✓; collectDefaults no error ✓; WithDefaults removed outright ✓; new file defaults.go ✓.
- [ ] **Spec coverage — Documentation:** package doc section added ✓ (Task 2 Step 4).
- [ ] **Spec coverage — Testing matrix:** all 9 collectDefaults tests + toSnakeCase table + 5 Load-level tests ✓.
- [ ] **Symbol ordering (CLAUDE.md):** new file `defaults.go` has unexported functions only; no ordering concern within it (all unexported). Existing configx.go ordering preserved.
- [ ] **Coverage:** ≥ 85% ✓.
- [ ] **No new dependencies** ✓.
- [ ] **No new error variants** ✓ (type-mismatch uses existing ErrUnmarshal).

## 关联

**设计文档：** `docs/superpowers/specs/2026-06-14-configx-default-tag-design.md`
