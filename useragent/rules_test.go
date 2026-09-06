package useragent

import (
	"strings"
	"testing"
)

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name   string
		tmpl   string
		groups []string
		want   string
	}{
		{name: "plain", tmpl: "Chrome", groups: []string{"x"}, want: "Chrome"},
		{name: "first group", tmpl: "$1", groups: []string{"all", "114.0"}, want: "114.0"},
		{name: "embedded", tmpl: "v$1 beta", groups: []string{"all", "12"}, want: "v12 beta"},
		{name: "two groups", tmpl: "$1.$2", groups: []string{"all", "17", "2"}, want: "17.2"},
		{name: "missing group is empty", tmpl: "$1.$2", groups: []string{"all", "17"}, want: "17."},
		{name: "literal version", tmpl: "8.1", groups: nil, want: "8.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := interpolate(tt.tmpl, tt.groups); got != tt.want {
				t.Errorf("interpolate(%q, %v) = %q, want %q", tt.tmpl, tt.groups, got, tt.want)
			}
		})
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"17_2", "17.2"},
		{"13", "13"},
		{"8.0.47.0", "8.0.47.0"},
		{"114.", "114"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.in); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanModel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Pixel 8 Pro", "Pixel 8 Pro"},
		{"SM-S928B Build/UP1A.231005.007", "SM-S928B"},
		{"wv", ""},
		{"Build", ""},
		{"OpenHarmony 7.0", ""},
		{"HMSCore 6.13.0.302", ""},
		{"!!!", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := cleanModel(tt.in); got != tt.want {
			t.Errorf("cleanModel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestCompileRuleBoundary verifies the word-boundary wrapper: rules must not
// fire inside longer product tokens.
func TestCompileRuleBoundary(t *testing.T) {
	re, _, err := compileRule(`Chrome`)
	if err != nil {
		t.Fatalf("compileRule() error = %v", err)
	}
	positives := []string{"Chrome/120", "x Chrome", "Mobile Safari/537.36 (Chrome)"}
	for _, ua := range positives {
		if !re.MatchString(ua) {
			t.Errorf("boundary rule did not match %q", ua)
		}
	}
	negatives := []string{"NotChrome/1", "_Chrome/1", "0Chrome/1", "QuarkPC"} // QuarkPC has no Chrome token anyway
	for _, ua := range negatives {
		if re.MatchString(ua) {
			t.Errorf("boundary rule matched inside %q", ua)
		}
	}
}

// TestCompileRuleAnchored verifies that "^"-prefixed patterns skip the
// boundary wrapper and still match at the user agent start.
func TestCompileRuleAnchored(t *testing.T) {
	re, _, err := compileRule(`^Go /?(\d+[.\d]*)?(?: package http)?`)
	if err != nil {
		t.Fatalf("compileRule() error = %v", err)
	}
	groups := re.FindStringSubmatch("Go 1.21 package http")
	if groups == nil {
		t.Fatal("anchored rule did not match at start")
	}
	if groups[1] != "1.21" {
		t.Errorf("capture group = %q, want %q", groups[1], "1.21")
	}
	if re.MatchString("Mozilla/5.0 Go 1.21") {
		t.Error("anchored rule matched away from the start")
	}
}

// TestNewErrors covers the validation paths of New.
func TestNewErrors(t *testing.T) {
	tests := []struct {
		name string
		opt  Option
		want string
	}{
		{
			name: "invalid regex",
			opt:  WithOSRules([]OSRule{{Regex: `(unbalanced`, Name: "X"}}),
			want: "os rule: compile",
		},
		{
			name: "missing capture group",
			opt:  WithOSRules([]OSRule{{Regex: `ServeOS`, Name: "ServeOS", Version: "$1"}}),
			want: `os rule: rule "ServeOS": template "$1" references missing capture group $1`,
		},
		{
			name: "unknown client kind",
			opt:  WithClientRules([]ClientRule{{Regex: "x", Name: "X", Kind: "robot"}}),
			want: `client rule "x": unknown kind "robot"`,
		},
		{
			name: "unknown device class",
			opt:  WithDeviceRules([]DeviceRule{{Regex: "x", Model: "X", Class: "toaster"}}),
			want: `device rule "x": unknown class "toaster"`,
		},
		{
			name: "invalid client regex",
			opt:  WithClientRules([]ClientRule{{Regex: `a(`, Name: "X", Kind: KindBrowser}}),
			want: "client rule: compile",
		},
		{
			name: "invalid device regex",
			opt:  WithDeviceRules([]DeviceRule{{Regex: `a(`}}),
			want: "device rule: compile",
		},
		{
			name: "invalid bot regex",
			opt:  WithBotRules([]BotRule{{Regex: `a(`}}),
			want: "bot rule: compile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.opt)
			if err == nil {
				t.Fatalf("New() error = nil, want containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("New() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

// TestLoadRuleErrors covers YAML decoding failures.
func TestLoadRuleErrors(t *testing.T) {
	bad := []byte("not: [a, valid: rule, list")
	if _, err := loadOSRules(bad); err == nil {
		t.Error("loadOSRules(bad) error = nil, want error")
	}
	if _, err := loadClientRules(bad); err == nil {
		t.Error("loadClientRules(bad) error = nil, want error")
	}
	if _, err := loadDeviceRules(bad); err == nil {
		t.Error("loadDeviceRules(bad) error = nil, want error")
	}
	if _, err := loadBotRules(bad); err == nil {
		t.Error("loadBotRules(bad) error = nil, want error")
	}
}

// TestLoadRulesRoundTrip decodes a YAML snippet end to end, proving the yaml
// tags and the embedded file layout agree.
func TestLoadRulesRoundTrip(t *testing.T) {
	defs, err := loadClientRules([]byte("- regex: 'Quark/([\\d.]+)'\n  name: 'Quark'\n  version: '$1'\n  kind: browser\n"))
	if err != nil {
		t.Fatalf("loadClientRules() error = %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "Quark" || defs[0].Kind != KindBrowser {
		t.Fatalf("loadClientRules() = %+v, want one Quark browser rule", defs)
	}
	compiled, err := compileClientRules(defs)
	if err != nil {
		t.Fatalf("compileClientRules() error = %v", err)
	}
	if got := compiled[0].re.FindStringSubmatch("x Quark/7.4.6.681")[1]; got != "7.4.6.681" {
		t.Errorf("compiled rule captured %q, want 7.4.6.681", got)
	}
}

// TestInferDeviceClass covers the class fallback branches that the table in
// useragent_test.go does not reach (unknown OS with mobile markers, unknown
// OS without markers, library and bot suppression).
func TestInferDeviceClass(t *testing.T) {
	tests := []struct {
		name string
		res  Result
		ua   string
		want DeviceClass
	}{
		{name: "library has no class", res: Result{ClientKind: KindLibrary}, ua: "okhttp/4.12.0", want: ""},
		{name: "bot has no class", res: Result{Bot: "Googlebot"}, ua: "Googlebot", want: ""},
		{name: "desktop os", res: Result{OS: "Windows", ClientKind: KindBrowser}, ua: "Mozilla/5.0 (Windows NT 10.0)", want: ClassDesktop},
		{name: "mobile os with phone token", res: Result{OS: "OpenHarmony", ClientKind: KindBrowser}, ua: "(Phone; OpenHarmony 5.0)", want: ClassSmartphone},
		{name: "mobile os tablet default", res: Result{OS: "Android", ClientKind: KindBrowser}, ua: "(Linux; Android 13; SM-X710)", want: ClassTablet},
		{name: "ios defaults to phone", res: Result{OS: "iOS", ClientKind: KindBrowser}, ua: "iPhone-like", want: ClassSmartphone},
		{name: "windows phone defaults to phone", res: Result{OS: "Windows Phone", ClientKind: KindBrowser}, ua: "Windows Phone", want: ClassSmartphone},
		{name: "unknown os mobile marker", res: Result{ClientKind: KindBrowser}, ua: "Mozilla/5.0 (Mobi)", want: ClassMobile},
		{name: "unknown os tablet marker", res: Result{ClientKind: KindBrowser}, ua: "Mozilla/5.0 (Tablet)", want: ClassTablet},
		{name: "unknown os no markers", res: Result{ClientKind: KindBrowser}, ua: "Mozilla/5.0", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferDeviceClass(tt.res, tt.ua); got != tt.want {
				t.Errorf("inferDeviceClass(%+v, %q) = %q, want %q", tt.res, tt.ua, got, tt.want)
			}
		})
	}
}

// TestCompileRuleRejectsLookarounds documents that PCRE-only constructs fail
// at compile time (not silently at parse time) — the curated data must stay
// RE2-clean.
func TestCompileRuleRejectsLookarounds(t *testing.T) {
	if _, _, err := compileRule(`(?<!like )Mac OS X`); err == nil {
		t.Error("compileRule(lookbehind) error = nil, want error")
	}
}

// TestRequiredLiteral covers the prefilter-literal extraction: a leading
// literal run is a necessary condition for a match, everything else yields
// no literal and forces the regex to always run.
func TestRequiredLiteral(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
	}{
		{pattern: `Chrome(?:/(\d+[.\d]+))?`, want: "chrome"},
		{pattern: `MicroMessenger/(\d+[.\d]+)`, want: "micromessenger/"},
		{pattern: `SE (\d+[.\d]+)`, want: "se "},
		{pattern: `^Go /?(\d+[.\d]*)?`, want: "go "}, // the optional "/" is dropped from the run
		{pattern: `Chrome(?:/(\d+[.\d]+))? Mobile`, want: "chrome"},
		{pattern: `SE (\d+[.\d]+)`, want: "se "},
		{pattern: `(?:HarmonyOS|Hmos)(?:[/ ](\d+[.\d]*))?`, want: ""}, // starts with a group
		{pattern: `Android|Adr`, want: ""},                            // top-level alternation
		{pattern: `CrOS`, want: "cros"},
		{pattern: `Mi`, want: ""}, // too short
		{pattern: `Quark(?:/(\d+[.\d]+))?`, want: "quark"},
		{pattern: `Mac OS X[ /]?(?:Version )?(\d+(?:[_.]\d+)+)`, want: "mac os x"},
	}
	for _, tt := range tests {
		if got := requiredLiteral(tt.pattern); got != tt.want {
			t.Errorf("requiredLiteral(%q) = %q, want %q", tt.pattern, got, tt.want)
		}
	}
}

// TestMatchRulesPrefilter proves the strings.Contains skip never discards a
// real match: rule results must be identical with and without the literal.
func TestMatchRulesPrefilter(t *testing.T) {
	defs, err := compileClientRules([]ClientRule{
		{Regex: `Chrome(?:/(\d+[.\d]+))?`, Name: "Chrome", Version: "$1", Kind: KindBrowser},
		{Regex: `(?:iPhone|iPad).+Version/(\d+[.\d]+)`, Name: "Mobile Safari", Version: "$1", Kind: KindBrowser},
	})
	if err != nil {
		t.Fatalf("compileClientRules() error = %v", err)
	}
	for _, ua := range []string{
		"x Chrome/152.0.0.0",
		"iPhone; CPU iPhone OS 17_2 like Mac OS X) Version/17.2 Mobile/15E148 Safari/604.1",
		"nothing to match here",
	} {
		r, groups := matchRules(defs, ua, strings.ToLower(ua))
		r2, groups2 := matchRulesNoPrefilter(defs, ua)
		if (r == nil) != (r2 == nil) {
			t.Errorf("matchRules(%q) prefilter changed outcome: %v vs %v", ua, r, r2)
			continue
		}
		if r != nil && (r.name != r2.name || groups[1] != groups2[1]) {
			t.Errorf("matchRules(%q) prefilter changed match: %+v vs %+v", ua, *r, *r2)
		}
	}
}

// matchRulesNoPrefilter runs every regex regardless of its literal; used by
// TestMatchRulesPrefilter as the reference implementation.
func matchRulesNoPrefilter(rules []rule, ua string) (*rule, []string) {
	for i := range rules {
		if groups := rules[i].re.FindStringSubmatch(ua); groups != nil {
			return &rules[i], groups
		}
	}
	return nil, nil
}

func TestContainsLetter(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"123.45", false},
		{"!!!", false},
		{"okhttp/4.12.0", true},
		{"\x00\x01z", true},
	}
	for _, tt := range tests {
		if got := containsLetter(tt.in); got != tt.want {
			t.Errorf("containsLetter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
