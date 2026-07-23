// Package configx provides a general-purpose configuration loading library.
// It wraps viper to offer a single Load() function that handles config file
// resolution (pflag → env → filesystem search), environment variable binding,
// structured decoding with built-in hooks, and tagless struct matching.
//
// # Quick Start
//
// Define a config struct (no mapstructure tags needed) and call Load.
// Field defaults come from `default:"<value>"` tags:
//
//	type Config struct {
//	    Server struct {
//	        GRPCAddr string `default:":9000"`
//	        HTTPAddr string
//	    }
//	    Log struct {
//	        Level  string `default:"info"`
//	        Format string
//	    }
//	}
//
//	var cfg Config
//	if err := configx.Load(&cfg,
//	    configx.WithServiceName("my-service"),
//	    configx.WithEnvPrefix("MY_SERVICE"),
//	); err != nil {
//	    log.Fatal(err)
//	}
//
// # Config File Resolution
//
// Load searches for a config file in this order:
//
//  1. -config pflag (e.g. -config /etc/my-service/config.yaml)
//  2. <SERVICE_NAME>_CONFIG env var — only if WithServiceName is set.
//     Service name hyphens are converted to underscores, e.g. "my-app" → MY_APP_CONFIG.
//  3. Filesystem search: scans config.<ext> in configured paths.
//     Default paths: ["."] or [" .", "/etc/<name>"] if WithServiceName is set.
//     Supported formats: yaml, yml, json, toml, hcl, ini, properties.
//
// # Environment Variable Override
//
// Config values from file can be overridden by environment variables:
//
//	Without prefix:    SERVER_GRPC_ADDR=:3000       (key path, dots → underscores)
//	With prefix:       MY_SERVICE_SERVER_GRPC_ADDR=:3000  (requires WithEnvPrefix)
//
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
// # Struct Mapping
//
// snake_case config keys are automatically matched to CamelCase struct fields.
// No mapstructure tags are required:
//
//	server.grpc_addr  →  Server.GRPCAddr
//	log.level         →  Log.Level
//	read_timeout      →  ReadTimeout
//
// Built-in decode hooks handle time.Duration ("30s"), []string ("a,b,c"), and
// time.Time (RFC3339).
package configx

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// loader holds the resolved options for a single Load call.
type loader struct {
	serviceName string
	envPrefix   string
	configName  string
	configPaths []string
	decodeHooks []mapstructure.DecodeHookFunc
	expandEnv   bool
}

var (
	parseOnce sync.Once

	supportedExts = []string{"yaml", "yml", "json", "toml", "hcl", "ini", "properties"}
)

func newLoader(opts []LoadOption) *loader {
	l := &loader{
		configName:  "config",
		configPaths: []string{"."},
	}
	for _, opt := range opts {
		opt(l)
	}
	if l.serviceName != "" {
		l.configPaths = append(l.configPaths, fmt.Sprintf("/etc/%s", l.serviceName))
	}
	return l
}

// Load reads configuration into target.
// Config file is resolved in order:
//  1. -config pflag (e.g. -config /etc/app/config.yaml)
//  2. <SERVICE_NAME>_CONFIG env var (if WithServiceName is set)
//  3. Filesystem search: config.<ext> in configured paths
//
// Environment variables override file values.
// Built-in decode hooks handle time.Duration, []string, and time.Time.
// Struct fields do not need mapstructure tags — snake_case config keys
// are automatically matched to CamelCase field names.
func Load(target any, opts ...LoadOption) error {
	l := newLoader(opts)

	// Register -config flag (idempotent).
	if pflag.CommandLine.Lookup("config") == nil {
		pflag.String("config", "", "path to config file")
	}
	parseOnce.Do(func() { pflag.Parse() })

	// Resolve config file path.
	cfgPath, err := l.resolveConfigPath()
	if err != nil {
		return err
	}

	// Setup viper.
	v := viper.New()
	v.SetConfigFile(cfgPath)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if l.envPrefix != "" {
		v.SetEnvPrefix(l.envPrefix)
	}

	// Set defaults from struct tags.
	for key, val := range collectDefaults(target) {
		v.SetDefault(key, val)
	}

	// Read config file.
	if err := v.ReadInConfig(); err != nil {
		return ErrReadConfig.Wrap(err)
	}

	// Expand ${VAR} in string values if enabled.
	if l.expandEnv {
		settings := v.AllSettings()
		expandStrings(settings)
		if err := v.MergeConfigMap(settings); err != nil {
			return ErrReadConfig.Wrap(err)
		}
	}

	// Unmarshal with decode hooks and tagless matching.
	if err := v.Unmarshal(target, func(dc *mapstructure.DecoderConfig) {
		dc.DecodeHook = l.buildDecodeHook()
		dc.MatchName = func(mapKey, fieldName string) bool {
			return strings.EqualFold(
				strings.ReplaceAll(mapKey, "_", ""),
				fieldName,
			)
		}
	}); err != nil {
		return ErrUnmarshal.Wrap(err)
	}

	return nil
}

func (l *loader) resolveConfigPath() (string, error) {
	// 1. pflag
	cfgFile, err := pflag.CommandLine.GetString("config")
	if err != nil {
		return "", ErrConfigNotFound.New()
	}
	if cfgFile != "" {
		return cfgFile, nil
	}

	// 2. env var (<SERVICE_NAME>_CONFIG)
	if l.serviceName != "" {
		envKey := strings.ToUpper(strings.ReplaceAll(l.serviceName, "-", "_")) + "_CONFIG"
		if env := os.Getenv(envKey); env != "" {
			return env, nil
		}
	}

	// 3. filesystem search
	for _, dir := range l.configPaths {
		for _, ext := range supportedExts {
			path := fmt.Sprintf("%s/%s.%s", dir, l.configName, ext)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	return "", ErrConfigNotFound.New()
}

func (l *loader) buildDecodeHook() mapstructure.DecodeHookFunc {
	hooks := []mapstructure.DecodeHookFunc{
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructure.StringToTimeHookFunc(time.RFC3339),
	}
	hooks = append(hooks, l.decodeHooks...)
	return mapstructure.ComposeDecodeHookFunc(hooks...)
}
