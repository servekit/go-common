package useragent

import (
	"sync"
	"testing"
)

// tests covers real user agents observed in production traffic plus edge
// cases. Key fields are asserted per case rather than the whole struct so
// additions to the rule data do not churn unrelated expectations.
var tests = []struct {
	name string
	ua   string
	want Result
}{
	{
		name: "quark on harmonyos next",
		ua:   "Mozilla/5.0 (phone; Android 13; OpenHarmony 7.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36 ArkWeb/4.1.6.1 Mobile Quark/7.4.6.681",
		want: Result{
			OS: "OpenHarmony", OSVersion: "7.0",
			Client: "Quark", ClientVersion: "7.4.6.681", ClientKind: KindBrowser,
			DeviceClass: ClassSmartphone,
		},
	},
	{
		name: "huawei browser on harmonyos",
		ua:   "Mozilla/5.0 (Linux; Android 12; HarmonyOS; ANA-AN00; HMSCore 6.13.0.302) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 HuaweiBrowser/14.0.5.310 Mobile Safari/537.36",
		want: Result{
			OS:     "HarmonyOS",
			Client: "Huawei Browser", ClientVersion: "14.0.5.310", ClientKind: KindBrowser,
			DeviceModel: "ANA-AN00", DeviceBrand: "Huawei",
			DeviceClass: ClassSmartphone,
		},
	},
	{
		name: "wechat web view on huawei",
		ua:   "Mozilla/5.0 (Linux; Android 10; HUAWEI P40; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/114.0.0.0 Mobile Safari/537.36 MicroMessenger/8.0.47",
		want: Result{
			OS: "Android", OSVersion: "10",
			Client: "WeChat", ClientVersion: "8.0.47", ClientKind: KindMobileApp,
			DeviceModel: "P40", DeviceBrand: "Huawei",
			DeviceClass: ClassSmartphone,
		},
	},
	{
		name: "iphone safari",
		ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1",
		want: Result{
			OS: "iOS", OSVersion: "17.2",
			Client: "Mobile Safari", ClientVersion: "17.2", ClientKind: KindBrowser,
			DeviceModel: "iPhone", DeviceBrand: "Apple",
			DeviceClass: ClassSmartphone,
		},
	},
	{
		name: "chrome on pixel",
		ua:   "Mozilla/5.0 (Linux; Android 14; Pixel 8 Pro) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Mobile Safari/537.36",
		want: Result{
			OS: "Android", OSVersion: "14",
			Client: "Chrome Mobile", ClientVersion: "152.0.0.0", ClientKind: KindBrowser,
			DeviceModel: "Pixel 8 Pro", DeviceBrand: "Google",
			DeviceClass: ClassSmartphone,
		},
	},
	{
		name: "chrome on mac",
		ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
		want: Result{
			OS: "macOS", OSVersion: "10.15.7",
			Client: "Chrome", ClientVersion: "152.0.0.0", ClientKind: KindBrowser,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "edge on windows",
		ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36 Edg/152.0.0.0",
		want: Result{
			OS:     "Windows",
			Client: "Microsoft Edge", ClientVersion: "152.0.0.0", ClientKind: KindBrowser,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "okhttp",
		ua:   "okhttp/4.12.0",
		want: Result{
			Client: "okhttp", ClientVersion: "4.12.0", ClientKind: KindLibrary,
		},
	},
	{
		name: "curl",
		ua:   "curl/8.5.0",
		want: Result{
			Client: "curl", ClientVersion: "8.5.0", ClientKind: KindLibrary,
		},
	},
	{
		name: "go http client",
		ua:   "Go-http-client/2.0",
		want: Result{
			Client: "Go-http-client", ClientVersion: "2.0", ClientKind: KindLibrary,
		},
	},
	{
		name: "grpc go",
		ua:   "grpc-go/1.6",
		want: Result{
			Client: "grpc-go", ClientVersion: "1.6", ClientKind: KindLibrary,
		},
	},
	{
		name: "empty",
		ua:   "",
	},
	{
		name: "digits only",
		ua:   "123456",
	},
	{
		name: "garbage",
		ua:   "!!!###$$$%%%&&&",
	},
	{
		name: "random bytes around a mozilla token",
		ua:   "\x00\x01Mozilla/\xff\xfe",
	},
	{
		name: "googlebot spoofs chrome",
		ua:   "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/114.0.0.0",
		want: Result{
			Bot: "Googlebot",
		},
	},
	{
		name: "android tablet without mobile token",
		ua:   "Mozilla/5.0 (Linux; Android 13; SM-X710) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		want: Result{
			OS: "Android", OSVersion: "13",
			Client: "Chrome", ClientVersion: "120.0.0.0", ClientKind: KindBrowser,
			DeviceModel: "SM-X710", DeviceBrand: "Samsung",
			DeviceClass: ClassTablet,
		},
	},
	{
		name: "ipad",
		ua:   "Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1",
		want: Result{
			OS: "iOS", OSVersion: "16.6",
			Client: "Mobile Safari", ClientVersion: "16.6", ClientKind: KindBrowser,
			DeviceModel: "iPad", DeviceBrand: "Apple",
			DeviceClass: ClassTablet,
		},
	},
	{
		name: "firefox on windows 8.1",
		ua:   "Mozilla/5.0 (Windows NT 6.3; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
		want: Result{
			OS: "Windows", OSVersion: "8.1",
			Client: "Firefox", ClientVersion: "120.0", ClientKind: KindBrowser,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "opera modern",
		ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36 OPR/105.0.0.0",
		want: Result{
			Client: "Opera", ClientVersion: "105.0.0.0", ClientKind: KindBrowser,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "opera legacy presto",
		ua:   "Opera/9.80 (Windows NT 6.1; U; en) Presto/2.8.131 Version/11.11",
		want: Result{
			OS: "Windows", OSVersion: "7",
			Client: "Opera", ClientVersion: "11.11", ClientKind: KindBrowser,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "qq browser mobile",
		ua:   "Mozilla/5.0 (Linux; U; Android 13; zh-cn; PGT-AN10 Build/HONORPGTAN00) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/114.0.0.0 Mobile Safari/537.36 MQQBrowser/14.9",
		want: Result{
			Client: "QQ Browser", ClientVersion: "14.9", ClientKind: KindBrowser,
		},
	},
	{
		name: "uc browser legacy token",
		ua:   "UCWEB/2.0 (Linux; U; Adr 4.4.4; zh-CN; Redmi Note 4) U2/1.0.0 UCBrowser/15.5.4.1187 Mobile",
		want: Result{
			Client: "UC Browser", ClientVersion: "15.5.4.1187", ClientKind: KindBrowser,
		},
	},
	{
		name: "wechat desktop windows",
		ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36 MicroMessenger/8.0.47.0 WindowsWechat(0x63090c33)",
		want: Result{
			OS:     "Windows",
			Client: "WeChat", ClientVersion: "8.0.47.0", ClientKind: KindMobileApp,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "samsung internet",
		ua:   "Mozilla/5.0 (Linux; Android 14; SAMSUNG SM-S928B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36",
		want: Result{
			Client: "Samsung Internet", ClientVersion: "26.0", ClientKind: KindBrowser,
			DeviceModel: "SM-S928B", DeviceBrand: "Samsung",
		},
	},
	{
		name: "huawei browser pc token",
		ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 HBPC/24.3.1.330",
		want: Result{
			OS:     "Windows",
			Client: "Huawei Browser", ClientVersion: "24.3.1.330", ClientKind: KindBrowser,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "chrome on android without brand prefix",
		ua:   "Mozilla/5.0 (Linux; Android 14; 2210132C Build/UKQ1.230917.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Mobile Safari/537.36",
		want: Result{
			OS: "Android", OSVersion: "14",
			Client: "Chrome Mobile", ClientVersion: "122.0.0.0", ClientKind: KindBrowser,
			DeviceModel: "2210132C",
			DeviceClass: ClassSmartphone,
		},
	},
	{
		name: "dalvik library",
		ua:   "Dalvik/2.1.0 (Linux; U; Android 14; zh-cn; Pixel 8) AppleWebKit/537.36",
		want: Result{
			OS: "Android", OSVersion: "14",
			Client: "Dalvik", ClientVersion: "2.1.0", ClientKind: KindLibrary,
		},
	},
	{
		name: "python requests",
		ua:   "python-requests/2.31.0",
		want: Result{
			Client: "python-requests", ClientVersion: "2.31.0", ClientKind: KindLibrary,
		},
	},
	{
		name: "apache http client",
		ua:   "Apache-HttpClient/4.5.14 (Java/17.0.9)",
		want: Result{
			Client: "Apache-HttpClient", ClientVersion: "4.5.14", ClientKind: KindLibrary,
		},
	},
	{
		name: "postman",
		ua:   "PostmanRuntime/7.36.0",
		want: Result{
			Client: "PostmanRuntime", ClientVersion: "7.36.0", ClientKind: KindLibrary,
		},
	},
	{
		name: "fxios",
		ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/128.0 Mobile/15E148 Safari/605.1.15",
		want: Result{
			Client: "Firefox", ClientVersion: "128.0", ClientKind: KindBrowser,
		},
	},
	{
		name: "redmi model",
		ua:   "Mozilla/5.0 (Linux; Android 13; Redmi Note 12 Pro) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Mobile Safari/537.36 XiaoMi/MiuiBrowser/18.2.201",
		want: Result{
			Client: "Mi Browser", ClientVersion: "18.2.201", ClientKind: KindBrowser,
			DeviceModel: "Note 12 Pro", DeviceBrand: "Xiaomi",
		},
	},
	{
		name: "vivo model",
		ua:   "Mozilla/5.0 (Linux; Android 14; vivo X200) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Mobile Safari/537.36 VivoBrowser/23.10.1",
		want: Result{
			Client: "vivo Browser", ClientVersion: "23.10.1", ClientKind: KindBrowser,
			DeviceModel: "X200", DeviceBrand: "vivo",
		},
	},
	{
		name: "linux desktop chrome",
		ua:   "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
		want: Result{
			OS:     "Linux",
			Client: "Chrome", ClientVersion: "150.0.0.0", ClientKind: KindBrowser,
			DeviceClass: ClassDesktop,
		},
	},
	{
		name: "safari bare token",
		ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko)",
		want: Result{
			OS: "macOS", OSVersion: "10.15.7",
			DeviceClass: ClassDesktop,
		},
	},
	{
		// "iPod touch" UAs also carry an "iPhone OS" token; the iPod rules
		// run first so the model is not misreported as iPhone.
		name: "ipod touch",
		ua:   "Mozilla/5.0 (iPod touch; CPU iPhone OS 15_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.6 Mobile/15E148 Safari/604.1",
		want: Result{
			OS: "iOS", OSVersion: "15.7",
			DeviceModel: "iPod", DeviceBrand: "Apple",
			DeviceClass: ClassSmartphone,
		},
	},
	{
		// Windows Phone UAs freeze an "Android" token; the Windows Phone
		// rule runs before the Android rules.
		name: "windows phone",
		ua:   "Mozilla/5.0 (Windows Phone 10.0; Android 6.0.1; Xbox; Xbox One) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 Edge/40.15250",
		want: Result{
			OS: "Windows Phone", OSVersion: "10.0",
			Client: "Microsoft Edge", ClientVersion: "40.15250", ClientKind: KindBrowser,
			DeviceClass: ClassSmartphone,
		},
	},
	{
		name: "bare ios version token",
		ua:   "wxwork/4.1.27 (iOS 17.2)",
		want: Result{
			OS: "iOS", OSVersion: "17.2",
			Client: "WeCom", ClientVersion: "4.1.27", ClientKind: KindMobileApp,
		},
	},
}

func TestParse(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.ua)
			checkResult(t, tt.ua, got, tt.want)
		})
	}
}

// checkResult compares the fields set in want; zero fields in want are not
// asserted (UA information we do not model is free to change with data
// updates).
func checkResult(t *testing.T, ua string, got, want Result) {
	t.Helper()
	if want.OS != "" && got.OS != want.OS {
		t.Errorf("Parse(%q).OS = %q, want %q", ua, got.OS, want.OS)
	}
	if want.OSVersion != "" && got.OSVersion != want.OSVersion {
		t.Errorf("Parse(%q).OSVersion = %q, want %q", ua, got.OSVersion, want.OSVersion)
	}
	if want.Client != "" && got.Client != want.Client {
		t.Errorf("Parse(%q).Client = %q, want %q", ua, got.Client, want.Client)
	}
	if want.ClientVersion != "" && got.ClientVersion != want.ClientVersion {
		t.Errorf("Parse(%q).ClientVersion = %q, want %q", ua, got.ClientVersion, want.ClientVersion)
	}
	if want.ClientKind != "" && got.ClientKind != want.ClientKind {
		t.Errorf("Parse(%q).ClientKind = %q, want %q", ua, got.ClientKind, want.ClientKind)
	}
	if want.DeviceModel != "" && got.DeviceModel != want.DeviceModel {
		t.Errorf("Parse(%q).DeviceModel = %q, want %q", ua, got.DeviceModel, want.DeviceModel)
	}
	if want.DeviceBrand != "" && got.DeviceBrand != want.DeviceBrand {
		t.Errorf("Parse(%q).DeviceBrand = %q, want %q", ua, got.DeviceBrand, want.DeviceBrand)
	}
	if want.DeviceClass != "" && got.DeviceClass != want.DeviceClass {
		t.Errorf("Parse(%q).DeviceClass = %q, want %q", ua, got.DeviceClass, want.DeviceClass)
	}
	if want.Bot != "" && got.Bot != want.Bot {
		t.Errorf("Parse(%q).Bot = %q, want %q", ua, got.Bot, want.Bot)
	}
	// A bot match must short-circuit everything else.
	if got.Bot != "" && (got.OS != "" || got.Client != "") {
		t.Errorf("Parse(%q) bot match leaked non-bot fields: %+v", ua, got)
	}
	// UAs with no bot and no client must never claim a class from thin air.
	if got.Bot == "" && got.Client == "" && got.DeviceClass == ClassDesktop && got.OS == "" {
		t.Errorf("Parse(%q) claimed desktop with no OS evidence", ua)
	}
}

// TestParseCustomParser verifies New with injected rule data: defaults are
// replaced per category and untouched categories keep the embedded data.
func TestParseCustomParser(t *testing.T) {
	p, err := New(WithOSRules([]OSRule{
		{Regex: `ServeOS (\d+)`, Name: "ServeOS", Version: "$1"},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got := p.Parse("Mozilla/5.0 ServeOS 7 Chrome/120.0.0.0")
	if got.OS != "ServeOS" || got.OSVersion != "7" {
		t.Errorf("custom os rules not applied: %+v", got)
	}
	// Untouched categories still come from the embedded data.
	if got.Client != "Chrome" || got.ClientKind != KindBrowser {
		t.Errorf("embedded client rules not used: %+v", got)
	}
}

// TestParseConcurrent exercises the lazy default parser and per-parser
// concurrency; run with -race to make data races fail loudly.
func TestParseConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, tt := range tests {
				if r := Parse(tt.ua); r.Bot != "" && r.Client != "" {
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkParse(b *testing.B) {
	uas := make([]string, 0, len(tests))
	for _, tt := range tests {
		if tt.ua != "" {
			uas = append(uas, tt.ua)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Parse(uas[i%len(uas)])
	}
}
