package configx

import (
	"reflect"
	"strings"
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
	String  string `default:"s"`
	Int     int    `default:"42"`
	Bool    bool   `default:"true"`
	Skip    string // no tag
	private string `default:"ignored"`
}

type defaultsUnsupported struct {
	Map map[string]string `default:"{}"`
	Ch  chan int          `default:"ignored"`
	Fn  func()            `default:"noop"`
	Ifc any               `default:"null"`
}

type defaultsEmptyTag struct {
	Set   string `default:"value"`
	Empty string `default:""`
}

type defaultsCyclic struct {
	Next *defaultsCyclic
	Val  string `default:"v"`
}

type siblingDB struct {
	Addr string `default:"localhost"`
}

type defaultsSiblingSameType struct {
	Primary   siblingDB
	Secondary siblingDB
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
	// All four tags are non-empty and recorded; viper will later fail
	// to convert them, surfacing as ErrUnmarshal.
	want := map[string]any{
		"map": "{}",
		"ch":  "ignored",
		"fn":  "noop",
		"ifc": "null",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_emptyTagIgnored(t *testing.T) {
	// An empty `default:""` tag is treated as not set.
	var cfg defaultsEmptyTag
	got := collectDefaults(&cfg)
	want := map[string]any{
		"set": "value",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_cyclicType(t *testing.T) {
	// Cyclic types (e.g., linked-list nodes) must not cause infinite
	// recursion. The first hop into Next records val once; the second hop
	// (next.next) would re-enter defaultsCyclic, which is already in the
	// ancestor chain and therefore skipped.
	var cfg defaultsCyclic
	got := collectDefaults(&cfg)
	want := map[string]any{
		"val":      "v",
		"next.val": "v",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDefaults_siblingSameType(t *testing.T) {
	// Sibling fields of the same struct type must both contribute their
	// inner defaults. The ancestor-chain cycle guard (vs a global visited
	// set) is what makes this work.
	var cfg defaultsSiblingSameType
	got := collectDefaults(&cfg)
	want := map[string]any{
		"primary.addr":   "localhost",
		"secondary.addr": "localhost",
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
