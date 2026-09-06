// Package useragent parses HTTP User-Agent strings into operating system,
// client (browser, mobile app, or HTTP library) and device information.
//
// The engine matches a user agent against ordered rule lists — most specific
// rule first, first match wins — independently for each category (OS, client,
// device, bot). Rules support "$1"-style interpolation of capture groups into
// names and versions. All regular expressions are compiled once when a
// Parser is built; a Parser is immutable and safe for concurrent use.
//
// Rule data embedded in this package is curated and adapted from the
// device-detector rule set of the Matomo project (LGPL-3.0-or-later). See
// NOTICE.md for attribution and licensing details.
package useragent

import (
	"strings"
	"sync"
)

// ClientKind classifies the client detected in a user agent.
type ClientKind string

// DeviceClass classifies the physical device implied by a user agent.
type DeviceClass string

// Result is the outcome of parsing one User-Agent string. Fields that could
// not be determined are left at their zero value.
type Result struct {
	// OS is the operating system name, e.g. "Android", "iOS", "Windows".
	OS string
	// OSVersion is the dotted OS version, e.g. "14", "17.2", when the user
	// agent carries one.
	OSVersion string
	// Client is the client name, e.g. "Chrome Mobile", "WeChat", "okhttp".
	Client string
	// ClientVersion is the client version, e.g. "8.0.47".
	ClientVersion string
	// ClientKind reports what kind of client produced the user agent. It is
	// empty when no client matched.
	ClientKind ClientKind
	// DeviceModel is the device model, e.g. "Pixel 8 Pro", "ANA-AN00",
	// "iPhone", when the user agent carries one.
	DeviceModel string
	// DeviceBrand is the device vendor, e.g. "Google", "Huawei", "Apple".
	DeviceBrand string
	// DeviceClass reports desktop / smartphone / tablet classification. It
	// is empty when the class cannot be inferred.
	DeviceClass DeviceClass
	// Bot holds the detected crawler name, e.g. "Googlebot". When non-empty
	// every other field is empty: crawlers spoof browser user agents, so
	// nothing else extracted from the string is trustworthy.
	Bot string
}

// Parser matches user agents against compiled rule sets. Build one with New
// and reuse it; a Parser is immutable and safe for concurrent use by
// multiple goroutines.
type Parser struct {
	osRules     []rule
	clientRules []rule
	deviceRules []rule
	botRules    []rule
}

// Option customizes the rule sets used by New. Each option replaces the
// built-in rule set of its category; categories left untouched fall back to
// the embedded default rules. This is mainly useful in tests.
type Option func(*options)

// New returns a Parser backed by the rule sets embedded in the package,
// adjusted by opts. It returns an error when an injected rule does not
// compile, references a missing capture group, or declares an unknown
// client kind or device class.
func New(opts ...Option) (*Parser, error) {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	osRules, err := o.loadOS()
	if err != nil {
		return nil, err
	}
	clientRules, err := o.loadClients()
	if err != nil {
		return nil, err
	}
	deviceRules, err := o.loadDevices()
	if err != nil {
		return nil, err
	}
	botRules, err := o.loadBots()
	if err != nil {
		return nil, err
	}
	return &Parser{
		osRules:     osRules,
		clientRules: clientRules,
		deviceRules: deviceRules,
		botRules:    botRules,
	}, nil
}

// WithOSRules replaces the built-in operating system rules.
func WithOSRules(rules []OSRule) Option {
	return func(o *options) { o.os = rules }
}

// WithClientRules replaces the built-in browser, mobile app and library rules.
func WithClientRules(rules []ClientRule) Option {
	return func(o *options) { o.clients = rules }
}

// WithDeviceRules replaces the built-in device model rules.
func WithDeviceRules(rules []DeviceRule) Option {
	return func(o *options) { o.devices = rules }
}

// WithBotRules replaces the built-in crawler rules.
func WithBotRules(rules []BotRule) Option {
	return func(o *options) { o.bots = rules }
}

const (
	// KindBrowser marks web browsers, including in-app browsers such as Quark.
	KindBrowser ClientKind = "browser"
	// KindMobileApp marks mobile applications that embed a web view (WeChat)
	// or speak HTTP on their own (Baidu Box App).
	KindMobileApp ClientKind = "mobile_app"
	// KindLibrary marks non-browser HTTP client libraries such as okhttp,
	// curl or grpc-go. user-service relies on this to file API sessions.
	KindLibrary ClientKind = "library"
)

const (
	// ClassDesktop marks non-mobile computers.
	ClassDesktop DeviceClass = "desktop"
	// ClassSmartphone marks handheld phones.
	ClassSmartphone DeviceClass = "smartphone"
	// ClassTablet marks tablets.
	ClassTablet DeviceClass = "tablet"
	// ClassMobile marks user agents that are clearly mobile but cannot be
	// told apart between smartphone and tablet.
	ClassMobile DeviceClass = "mobile"
)

var (
	defaultOnce   sync.Once
	defaultParser *Parser
)

// Parse parses ua with the package default Parser and is safe for concurrent
// use. The default Parser (and therefore all its regular expressions) is
// compiled lazily, once, on the first call. Empty or garbage input yields a
// zero Result; Parse never panics on untrusted input.
func Parse(ua string) Result {
	defaultOnce.Do(func() {
		p, err := New()
		if err != nil {
			// The embedded rule data is part of the package and covered by
			// tests; failing to load it is a programming error, not a
			// runtime condition. Must-style panic mirrors regexp.MustCompile.
			panic("useragent: load embedded rules: " + err.Error())
		}
		defaultParser = p
	})
	return defaultParser.Parse(ua)
}

// Parse parses ua with the parser's rule sets. Fields that no rule matched
// stay empty; untrusted input never panics.
func (p *Parser) Parse(ua string) Result {
	var res Result
	// Skip user agents that contain no letter at all ("", "12.3", "###"):
	// no rule can match and this keeps digit-only noise cheap.
	if !containsLetter(ua) {
		return res
	}
	lower := strings.ToLower(ua)

	if r, groups := matchRules(p.botRules, ua, lower); r != nil {
		res.Bot = interpolate(r.name, groups)
		return res
	}
	if r, groups := matchRules(p.osRules, ua, lower); r != nil {
		res.OS = interpolate(r.name, groups)
		res.OSVersion = normalizeVersion(interpolate(r.version, groups))
	}
	if r, groups := matchRules(p.clientRules, ua, lower); r != nil {
		res.Client = interpolate(r.name, groups)
		res.ClientVersion = normalizeVersion(interpolate(r.version, groups))
		res.ClientKind = r.kind
	}
	if r, groups := matchRules(p.deviceRules, ua, lower); r != nil {
		res.DeviceModel = cleanModel(interpolate(r.name, groups))
		res.DeviceBrand = r.brand
		res.DeviceClass = r.class
	}
	if res.DeviceClass == "" {
		res.DeviceClass = inferDeviceClass(res, ua)
	}
	return res
}

// inferDeviceClass derives a device class from the matched OS, the client
// kind and coarse tokens in the user agent, for UAs where no explicit device
// rule fired. It mirrors the heuristics of Matomo's device-detector:
// desktop OS names map to desktop; mobile OSes map to smartphone when the
// UA carries a "Mobile"/"Phone" token and to tablet otherwise (the Chrome
// for Android convention), except iOS which is a smartphone by default.
func inferDeviceClass(res Result, ua string) DeviceClass {
	if res.Bot != "" || res.ClientKind == KindLibrary {
		return ""
	}
	if desktopOSes[res.OS] {
		return ClassDesktop
	}
	if mobileOSes[res.OS] {
		switch {
		case strings.Contains(ua, "Tablet"):
			return ClassTablet
		case strings.Contains(ua, "Mobile"):
			return ClassSmartphone
		case strings.Contains(ua, "Phone"), strings.Contains(ua, "phone"):
			return ClassSmartphone
		case res.OS == "iOS", res.OS == "Windows Phone", res.OS == "Windows Mobile":
			return ClassSmartphone
		default:
			// Android-family UAs without a "Mobile" token are tablets.
			return ClassTablet
		}
	}
	if res.ClientKind == KindBrowser || res.ClientKind == KindMobileApp {
		if strings.Contains(ua, "Tablet") {
			return ClassTablet
		}
		if strings.Contains(ua, "Mobi") {
			return ClassMobile
		}
	}
	return ""
}

// containsLetter reports whether s contains at least one ASCII letter.
func containsLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}
