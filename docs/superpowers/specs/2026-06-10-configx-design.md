# Configx Library Design

## Overview

A general-purpose configuration loading library (`configx`) for Go services. Wraps [viper](https://github.com/spf13/viper) to provide a single `Load()` function that handles config file resolution (flag → env → filesystem search), environment variable binding, structured decoding, and defaults. Supports all viper-compatible file formats (YAML, JSON, TOML, HCL, INI, properties).

## Motivation

Every service (storage-service, gid-service, etc.) re-implements the same config loading pipeline:

```
parse flag → resolve file path → setup viper → read file → decode hooks → unmarshal → defaults
```

Differences across services (flag library choice, env prefix, decode hooks, defaults approach) cause subtle bugs (e.g., storage-service missing the Duration decode hook). `configx` extracts this into a shared, tested package.

## Requirements

- Single `Load(&cfg, opts...)` call — no file path argument, auto-discovers config
- Config file resolution: `-config` pflag → `<SERVICE_NAME>_CONFIG` env → filesystem search
- Filesystem search: scan config paths for `config.*` in supported formats (yaml, yml, json, toml, hcl, ini, properties)
- Built-in decode hooks: `StringToTimeDuration`, `StringToSlice(",")`, `StringToTime(RFC3339)`
- Extensible via functional options
- Errors use `xerr` (predefined codes for read/unmarshal failures)
- No validation — leave to caller

## API

### Core Function

```go
// Load reads configuration into target.
// Config file is resolved in order:
//  1. -config pflag (e.g. -config /etc/app/config.yaml)
//  2. <SERVICE_NAME>_CONFIG env var (if WithServiceName is set)
//  3. Filesystem search: config.yaml/config.json/... in configured paths
func Load(target any, opts ...LoadOption) error
```

### Options

| Option | Effect | Default |
|--------|--------|---------|
| `WithServiceName(name)` | Enables `<NAME>_CONFIG` env var, adds `/etc/<name>` to search paths | none (only `./` searched) |
| `WithEnvPrefix(prefix)` | Viper env prefix for `AutomaticEnv()` | none (bare keys) |
| `WithConfigName(name)` | Config file name without extension | `"config"` |
| `WithConfigPaths(paths)` | Directories to search for config file | `["."]` or `[".", "/etc/<name>"]` |
| `WithDefaults(m)` | Viper default values | none |
| `WithDecodeHooks(hooks)` | Additional mapstructure decode hooks | built-in Duration+Slice+Time |

### Error Codes

Predefined in `configx` using `xerr`:

| Code | Reason | When |
|------|--------|------|
| `ErrConfigNotFound` | `CONFIG_NOT_FOUND` | No config file found in any path |
| `ErrReadConfig` | `READ_CONFIG_FAILED` | viper `ReadInConfig` failed (parse error, permission, etc.) |
| `ErrUnmarshal` | `UNMARSHAL_CONFIG_FAILED` | viper `Unmarshal` failed (type mismatch, bad structure) |

## File Format Support

When no explicit file path is given, `configx` searches for files in this priority order:

```
config.yaml, config.yml, config.json, config.toml, config.hcl, config.ini, config.properties
```

The first file found in the highest-priority config path wins. When an explicit path is given (via flag or env), viper auto-detects the format from the file extension.

## Design Details

### pflag Integration

`Load()` registers a `-config` flag on `pflag.CommandLine`. Registration is guarded with `pflag.CommandLine.Lookup("config")` to prevent panics on double-registration. `pflag.Parse()` is called once (also guarded with `sync.Once` to avoid "flag redefined" panics when multiple components call `Load`).

### Struct Tags Not Required

`configx` configures mapstructure's `MatchName` to automatically match `snake_case` config keys to `CamelCase` struct fields. No `mapstructure` tags needed:

```go
type ServerConfig struct {
    GRPCAddr    string         // matches grpc_addr
    HTTPAddr    string         // matches http_addr
    ReadTimeout time.Duration  // matches read_timeout
}
```

Implementation:

```go
v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
    dc.MatchName = func(mapKey, fieldName string) bool {
        return strings.EqualFold(
            strings.ReplaceAll(mapKey, "_", ""),
            fieldName,
        )
    }
})
```

The only case requiring a tag is `mapstructure:",squash"` for flattening embedded structs.

### Viper Setup

- Uses `viper.New()` (not the global singleton) to avoid cross-service state leakage
- `SetEnvKeyReplacer(strings.NewReplacer(".", "_"))` for nested key mapping
- `AutomaticEnv()` enabled by default — env vars override file values
- Env prefix applied via `SetEnvPrefix()` when `WithEnvPrefix` is used

### Decode Hooks

Built-in hooks (always active):

```go
mapstructure.ComposeDecodeHookFunc(
    mapstructure.StringToTimeDurationHookFunc(),
    mapstructure.StringToSliceHookFunc(","),
    mapstructure.StringToTimeHookFunc(time.RFC3339),
)
```

User-provided hooks via `WithDecodeHooks` are appended after the built-in ones.

### File Search Algorithm

```
for each configPath:
    for each ext in supportedExts:
        if file exists at configPath/configName.ext:
            return that file
return ErrConfigNotFound
```

## File Structure

```
go-common/configx/
├── configx.go       # Load(), loader struct, search logic
├── options.go       # With* option functions
├── errors.go        # xerr error codes
└── configx_test.go  # tests with temp files
```

## Usage Examples

### Minimal (development, local config.yaml)

```go
var cfg MyConfig
if err := configx.Load(&cfg); err != nil {
    log.Fatal(err)
}
```

### Full (production service)

```go
var cfg MyConfig
if err := configx.Load(&cfg,
    configx.WithServiceName("storage-service"),
    configx.WithEnvPrefix("STORAGE_SERVICE"),
    configx.WithDefaults(map[string]any{
        "server.grpc_addr": ":9000",
        "log.level":        "info",
    }),
); err != nil {
    log.Fatal(err)
}
if err := cfg.Validate(); err != nil {
    log.Fatal(err)
}
```

### Custom config name

```go
var cfg MyConfig
if err := configx.Load(&cfg,
    configx.WithServiceName("worker"),
    configx.WithConfigName("worker-config"),
); err != nil {
    log.Fatal(err)
}
```

## Dependencies Introduced to go-common

| Dependency | Purpose |
|------------|---------|
| `github.com/spf13/viper` | Config file parsing + env binding |
| `github.com/spf13/pflag` | `-config` flag parsing |
| `github.com/mitchellh/mapstructure` | Struct decoding (viper dependency, may already be indirect) |

## Out of Scope

- Validation (caller's responsibility via `Validate()` method)
- Hot-reload / file watching
- Remote config (etcd, consul)
- Secret management
