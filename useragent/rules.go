package useragent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// OSRule is an uncompiled operating system rule: Regex is matched against
// the user agent, Name is the OS name template and Version the version
// template, both supporting "$1".."$3" interpolation from capture groups.
type OSRule struct {
	Regex   string `yaml:"regex"`
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// ClientRule is an uncompiled client rule for browsers, mobile apps and
// HTTP libraries. Kind must be one of KindBrowser, KindMobileApp or
// KindLibrary.
type ClientRule struct {
	Regex   string     `yaml:"regex"`
	Name    string     `yaml:"name"`
	Version string     `yaml:"version"`
	Kind    ClientKind `yaml:"kind"`
}

// DeviceRule is an uncompiled device rule. Model is the model template
// ("$1"-style interpolation supported); Brand and Class are optional fixed
// values attached to a match.
type DeviceRule struct {
	Regex string      `yaml:"regex"`
	Model string      `yaml:"model"`
	Brand string      `yaml:"brand"`
	Class DeviceClass `yaml:"class"`
}

// BotRule is an uncompiled crawler rule.
type BotRule struct {
	Regex string `yaml:"regex"`
	Name  string `yaml:"name"`
}

// rule is a compiled rule shared by all categories; fields that a category
// does not use stay zero. In device rules name holds the model template and
// version is unused.
type rule struct {
	re      *regexp.Regexp
	literal string // required lowercase literal; empty means "always run the regex"
	name    string
	version string
	kind    ClientKind
	brand   string
	class   DeviceClass
}

// maxCaptureRefs is the highest capture group index rules may interpolate.
const maxCaptureRefs = 3

// boundaryPrefix forces matches to start at a word boundary: either the
// beginning of the user agent or a character that cannot appear in a
// product token. Without it a rule for "Chrome" would also fire inside
// "NotChrome" or "QuarkPC". This mirrors the technique used by Matomo's
// device-detector ("fixUserAgentRegex"), adapted to RE2 syntax.
const boundaryPrefix = `(?i)(?:^|[^a-z0-9_-])(?:`

// compileRule compiles pattern and derives its prefilter literal.
// Patterns that start with "^" are anchored at the user agent start and
// skip the word-boundary wrapper.
func compileRule(pattern string) (*regexp.Regexp, string, error) {
	expr := boundaryPrefix + pattern + ")"
	if strings.HasPrefix(pattern, "^") {
		expr = `(?i)` + pattern
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, "", fmt.Errorf("compile %q: %w", pattern, err)
	}
	return re, requiredLiteral(pattern), nil
}

// validateRefs rejects templates that reference capture groups the pattern
// does not define, so a typo in the data fails at New instead of silently
// producing empty versions at runtime.
func validateRefs(re *regexp.Regexp, pattern string, templates ...string) error {
	for _, tmpl := range templates {
		for i := 1; i <= maxCaptureRefs; i++ {
			if !strings.Contains(tmpl, "$"+strconv.Itoa(i)) {
				continue
			}
			if i > re.NumSubexp() {
				return fmt.Errorf("rule %q: template %q references missing capture group $%d", pattern, tmpl, i)
			}
		}
	}
	return nil
}

// compileOSRules compiles OS rules, preserving order.
func compileOSRules(defs []OSRule) ([]rule, error) {
	rules := make([]rule, 0, len(defs))
	for _, def := range defs {
		re, literal, err := compileRule(def.Regex)
		if err != nil {
			return nil, fmt.Errorf("os rule: %w", err)
		}
		if err := validateRefs(re, def.Regex, def.Name, def.Version); err != nil {
			return nil, fmt.Errorf("os rule: %w", err)
		}
		rules = append(rules, rule{re: re, literal: literal, name: def.Name, version: def.Version})
	}
	return rules, nil
}

// compileClientRules compiles client rules, rejecting unknown kinds.
func compileClientRules(defs []ClientRule) ([]rule, error) {
	validKinds := map[ClientKind]bool{KindBrowser: true, KindMobileApp: true, KindLibrary: true}
	rules := make([]rule, 0, len(defs))
	for _, def := range defs {
		if !validKinds[def.Kind] {
			return nil, fmt.Errorf("client rule %q: unknown kind %q", def.Regex, def.Kind)
		}
		re, literal, err := compileRule(def.Regex)
		if err != nil {
			return nil, fmt.Errorf("client rule: %w", err)
		}
		if err := validateRefs(re, def.Regex, def.Name, def.Version); err != nil {
			return nil, fmt.Errorf("client rule: %w", err)
		}
		rules = append(rules, rule{re: re, literal: literal, name: def.Name, version: def.Version, kind: def.Kind})
	}
	return rules, nil
}

// compileDeviceRules compiles device rules, rejecting unknown classes.
func compileDeviceRules(defs []DeviceRule) ([]rule, error) {
	validClasses := map[DeviceClass]bool{
		ClassDesktop: true, ClassSmartphone: true, ClassTablet: true, ClassMobile: true,
	}
	rules := make([]rule, 0, len(defs))
	for _, def := range defs {
		if def.Class != "" && !validClasses[def.Class] {
			return nil, fmt.Errorf("device rule %q: unknown class %q", def.Regex, def.Class)
		}
		re, literal, err := compileRule(def.Regex)
		if err != nil {
			return nil, fmt.Errorf("device rule: %w", err)
		}
		if err := validateRefs(re, def.Regex, def.Model); err != nil {
			return nil, fmt.Errorf("device rule: %w", err)
		}
		rules = append(rules, rule{re: re, literal: literal, name: def.Model, brand: def.Brand, class: def.Class})
	}
	return rules, nil
}

// compileBotRules compiles crawler rules.
func compileBotRules(defs []BotRule) ([]rule, error) {
	rules := make([]rule, 0, len(defs))
	for _, def := range defs {
		re, literal, err := compileRule(def.Regex)
		if err != nil {
			return nil, fmt.Errorf("bot rule: %w", err)
		}
		rules = append(rules, rule{re: re, literal: literal, name: def.Name})
	}
	return rules, nil
}

// matchRules returns the first rule whose pattern matches ua, together with
// its capture groups. lowerUA must be strings.ToLower(ua); rules with a
// prefilter literal whose text is absent from lowerUA are skipped without
// running their regex.
func matchRules(rules []rule, ua, lowerUA string) (matched *rule, groups []string) {
	for i := range rules {
		if rules[i].literal != "" && !strings.Contains(lowerUA, rules[i].literal) {
			continue
		}
		if m := rules[i].re.FindStringSubmatch(ua); m != nil {
			return &rules[i], m
		}
	}
	return nil, nil
}

// requiredLiteral extracts a lowercase literal that any match of the pattern
// must contain, for use as a cheap strings.Contains prefilter. Because the
// rule regexes are matched unanchored, the text of every match starts with
// the expansion of the pattern's first element — so a leading run of
// mandatory literal characters is a necessary condition for a match.
//
// It returns "" (meaning "always run the regex") when the pattern starts
// with a group or class, contains a top-level alternation before the run
// ends, or has an optional quantifier trimming the run below minLiteralLen.
func requiredLiteral(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "^") // an anchor does not affect necessity
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == ' ', c == '/', c == '_', c == ':':
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			b.WriteByte(c)
		case c == '?', c == '*', c == '+':
			// The previous character is optional; drop it from the run.
			run := b.String()
			if len(run)-1 >= minLiteralLen {
				return run[:len(run)-1]
			}
			return ""
		case c == '|':
			// A top-level alternation offers a branch without the run.
			return ""
		default:
			// '(' '[' '.' '^' '$' '\' '{' '}' ')' and any other regex
			// metacharacter ends the run; the run stays necessary.
			if b.Len() >= minLiteralLen {
				return b.String()
			}
			return ""
		}
	}
	if b.Len() >= minLiteralLen {
		return b.String()
	}
	return ""
}

// minLiteralLen is the shortest prefix literal worth prefiltering with;
// shorter literals occur in too many user agents to skip work.
const minLiteralLen = 3

// interpolate expands "$1".."$3" in tmpl with the corresponding capture
// groups and trims the result. Groups that did not participate in the match
// expand to the empty string, so "Android $1" degrades to "Android".
func interpolate(tmpl string, groups []string) string {
	if !strings.Contains(tmpl, "$") {
		return strings.TrimSpace(tmpl)
	}
	for i := 1; i <= maxCaptureRefs; i++ {
		key := "$" + strconv.Itoa(i)
		if !strings.Contains(tmpl, key) {
			continue
		}
		value := ""
		if i < len(groups) {
			value = groups[i]
		}
		tmpl = strings.ReplaceAll(tmpl, key, value)
	}
	return strings.TrimSpace(tmpl)
}

// normalizeVersion turns "17_2"-style versions into dotted form and drops
// trailing dots left by non-participating capture groups.
func normalizeVersion(v string) string {
	return strings.TrimRight(strings.ReplaceAll(v, "_", "."), ".")
}

// junkModels are tokens that appear in the model position of an Android
// device fragment but carry no model information.
var junkModels = map[string]bool{
	"wv": true, "build": true, "mobile": true, "tablet": true, "phone": true,
	"android": true, "linux": true, "harmonyos": true, "openharmony": true,
	"hms": true, "u": true, "zh-cn": true, "en-us": true,
}

// junkModelPrefixes catch fragments like "OpenHarmony 7.0" or
// "HMSCore 6.13.0.302" that leak into the model position.
var junkModelPrefixes = []string{"openharmony", "harmonyos", "hmscore", "gms ", "android"}

// cleanModel normalizes a captured device model: strips "Build/..." tails,
// underscore separators and placeholder tokens. It returns "" when nothing
// model-like remains.
func cleanModel(m string) string {
	m = strings.TrimSpace(strings.ReplaceAll(m, "_", " "))
	if i := strings.Index(m, "Build/"); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	lower := strings.ToLower(m)
	if junkModels[lower] {
		return ""
	}
	for _, prefix := range junkModelPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	// A model must carry at least one letter or digit to be worth showing.
	hasAlnum := false
	for i := 0; i < len(m); i++ {
		c := m[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			hasAlnum = true
			break
		}
	}
	if !hasAlnum {
		return ""
	}
	return m
}

// desktopOSes and mobileOSes drive device-class inference for user agents
// where no explicit device rule fired.
var desktopOSes = map[string]bool{
	"Windows": true, "macOS": true, "Linux": true, "Ubuntu": true,
	"Fedora": true, "Debian": true, "CentOS": true, "Arch Linux": true,
	"Chrome OS": true,
}

var mobileOSes = map[string]bool{
	"iOS": true, "Android": true, "HarmonyOS": true, "OpenHarmony": true,
	"Windows Phone": true, "Windows Mobile": true,
}

// loadOSRules parses OS rules from YAML data.
func loadOSRules(data []byte) ([]OSRule, error) {
	var defs []OSRule
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("parse os yaml: %w", err)
	}
	return defs, nil
}

// loadClientRules parses client rules from YAML data.
func loadClientRules(data []byte) ([]ClientRule, error) {
	var defs []ClientRule
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("parse clients yaml: %w", err)
	}
	return defs, nil
}

// loadDeviceRules parses device rules from YAML data.
func loadDeviceRules(data []byte) ([]DeviceRule, error) {
	var defs []DeviceRule
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("parse devices yaml: %w", err)
	}
	return defs, nil
}

// loadBotRules parses crawler rules from YAML data.
func loadBotRules(data []byte) ([]BotRule, error) {
	var defs []BotRule
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("parse bots yaml: %w", err)
	}
	return defs, nil
}
