package configx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/servekit/go-common/xerr"
	"github.com/stretchr/testify/require"
)

// --- test types (no mapstructure tags) ---

type ServerConfig struct {
	GRPCAddr string
	HTTPAddr string
}

type LogConfig struct {
	Level  string
	Format string
}

type FullConfig struct {
	Server ServerConfig
	Log    LogConfig
	Redis  RedisConfig
}

type RedisConfig struct {
	Addr string
}

type DecodeConfig struct {
	Timeout time.Duration
	Labels  []string
	BornAt  time.Time
}

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

// TagTypesConfig exercises viper's decode hooks via tag defaults.
type TagTypesConfig struct {
	Timeout time.Duration `default:"30s"`
	Labels  []string      `default:"a,b,c"`
	Retries int           `default:"5"`
	Verbose bool          `default:"true"`
}

// BadTagConfig has a default that cannot convert to the field type.
type BadTagConfig struct {
	Port int `default:"not-a-number"`
}

// --- helpers ---

func writeTempConfig(t *testing.T, dir, filename, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644))
}

func tempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func setenv(t *testing.T, key, value string) {
	t.Helper()
	orig := os.Getenv(key)
	require.NoError(t, os.Setenv(key, value))
	t.Cleanup(func() { _ = os.Setenv(key, orig) })
}

// --- tests ---

func TestLoad_YAML(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: ":9000"
  http_addr: ":8080"
log:
  level: debug
  format: text
`)
	chdir(t, dir)

	var cfg FullConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":9000", cfg.Server.GRPCAddr)
	require.Equal(t, ":8080", cfg.Server.HTTPAddr)
	require.Equal(t, "debug", cfg.Log.Level)
	require.Equal(t, "text", cfg.Log.Format)
}

func TestLoad_JSON(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.json", `{
  "server": {"grpc_addr": ":9001", "http_addr": ":8081"},
  "log": {"level": "info", "format": "json"}
}`)
	chdir(t, dir)

	var cfg FullConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":9001", cfg.Server.GRPCAddr)
	require.Equal(t, "info", cfg.Log.Level)
}

func TestLoad_TOML(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.toml", `
[server]
grpc_addr = ":9002"
http_addr = ":8082"

[log]
level = "warn"
format = "json"
`)
	chdir(t, dir)

	var cfg FullConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":9002", cfg.Server.GRPCAddr)
	require.Equal(t, "warn", cfg.Log.Level)
}

func TestLoad_ConfigNotFound(t *testing.T) {
	dir := tempDir(t)
	chdir(t, dir)

	var cfg FullConfig
	err := Load(&cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrConfigNotFound.New()))
}

func TestLoad_WithServiceName_EnvVar(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "my-config.yaml", `
server:
  grpc_addr: ":7000"
log:
  level: info
  format: json
`)
	chdir(t, dir)
	setenv(t, "MY_SERVICE_CONFIG", filepath.Join(dir, "my-config.yaml"))

	var cfg FullConfig
	require.NoError(t, Load(&cfg,
		WithServiceName("my-service"),
		WithConfigName("my-config"),
	))
	require.Equal(t, ":7000", cfg.Server.GRPCAddr)
}

func TestLoad_EnvOverride(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: ":9000"
log:
  level: info
  format: json
`)
	chdir(t, dir)
	setenv(t, "SERVER_GRPC_ADDR", ":3000")

	var cfg FullConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":3000", cfg.Server.GRPCAddr)
}

func TestLoad_EnvOverride_WithPrefix(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: ":9000"
log:
  level: info
  format: json
`)
	chdir(t, dir)
	setenv(t, "MYAPP_SERVER_GRPC_ADDR", ":4000")

	var cfg FullConfig
	require.NoError(t, Load(&cfg, WithEnvPrefix("MYAPP")))
	require.Equal(t, ":4000", cfg.Server.GRPCAddr)
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

func TestLoad_TagInvalidValue(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", ``)
	chdir(t, dir)

	var cfg BadTagConfig
	err := Load(&cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnmarshal.New()))
}

func TestLoad_DecodeHooks_Duration(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
timeout: 30s
labels:
  - a
  - b
  - c
born_at: 2024-01-15T10:30:00Z
`)
	chdir(t, dir)

	var cfg DecodeConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, 30*time.Second, cfg.Timeout)
	require.Equal(t, []string{"a", "b", "c"}, cfg.Labels)
	require.Equal(t, time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), cfg.BornAt)
}

func TestLoad_MatchName_NoTags(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: ":5000"
  http_addr: ":5001"
log:
  level: debug
  format: text
redis:
  addr: "localhost:6379"
`)
	chdir(t, dir)

	var cfg FullConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":5000", cfg.Server.GRPCAddr)
	require.Equal(t, ":5001", cfg.Server.HTTPAddr)
	require.Equal(t, "debug", cfg.Log.Level)
	require.Equal(t, "text", cfg.Log.Format)
	require.Equal(t, "localhost:6379", cfg.Redis.Addr)
}

func TestLoad_ExplicitPath_ViaEnv(t *testing.T) {
	dir := tempDir(t)
	configFile := filepath.Join(dir, "custom.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(`
server:
  grpc_addr: ":6060"
log:
  level: warn
  format: json
`), 0644))
	setenv(t, "MY_APP_CONFIG", configFile)

	var cfg FullConfig
	require.NoError(t, Load(&cfg, WithServiceName("my-app")))
	require.Equal(t, ":6060", cfg.Server.GRPCAddr)
}

func TestLoad_WithConfigPaths(t *testing.T) {
	dir := tempDir(t)
	sub := filepath.Join(dir, "etc")
	require.NoError(t, os.Mkdir(sub, 0755))
	writeTempConfig(t, sub, "config.yaml", `
server:
  grpc_addr: ":7070"
log:
  level: info
  format: json
`)

	var cfg FullConfig
	require.NoError(t, Load(&cfg, WithConfigPaths(sub)))
	require.Equal(t, ":7070", cfg.Server.GRPCAddr)
}

func TestLoad_ErrReadConfig_InvalidContent(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.toml", `
[invalid
  missing_bracket = true
`)
	chdir(t, dir)

	var cfg FullConfig
	err := Load(&cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrReadConfig.New()))
}

func TestLoad_ErrUnmarshal_TypeMismatch(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: ":9000"
  http_addr: ":8080"
log:
  level: info
  format: json
redis:
  addr: "localhost:6379"
`)
	chdir(t, dir)

	type BadConfig struct {
		Server ServerConfig
		Log    LogConfig
		Redis  int
	}
	var cfg BadConfig
	err := Load(&cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnmarshal.New()))
}

func TestLoad_ErrorIsXerr(t *testing.T) {
	dir := tempDir(t)
	chdir(t, dir)

	var cfg FullConfig
	err := Load(&cfg)

	var xerrErr *xerr.Error
	require.True(t, errors.As(err, &xerrErr))
	require.Equal(t, "CONFIG_NOT_FOUND", xerrErr.Code().Reason())
}

func TestLoad_YAMLFormatPriority(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: ":yaml"
log:
  level: info
  format: json
`)
	writeTempConfig(t, dir, "config.json", `{"server":{"grpc_addr":":json"}}`)
	chdir(t, dir)

	var cfg FullConfig
	require.NoError(t, Load(&cfg))
	require.Equal(t, ":yaml", cfg.Server.GRPCAddr)
}

func TestLoad_ExpandEnv(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: "${TEST_EXPAND_GRPC}"
log:
  level: "${TEST_EXPAND_LEVEL}"
  format: text
`)
	chdir(t, dir)
	setenv(t, "TEST_EXPAND_GRPC", ":9999")
	setenv(t, "TEST_EXPAND_LEVEL", "debug")

	var cfg FullConfig
	require.NoError(t, Load(&cfg, WithExpandEnv()))
	require.Equal(t, ":9999", cfg.Server.GRPCAddr)
	require.Equal(t, "debug", cfg.Log.Level)
}

func TestLoad_ExpandEnv_DisabledByDefault(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
server:
  grpc_addr: "${TEST_EXPAND_OFF}"
`)
	chdir(t, dir)
	setenv(t, "TEST_EXPAND_OFF", ":1111")

	var cfg FullConfig
	require.NoError(t, Load(&cfg))
	// Without WithExpandEnv, ${VAR} stays literal (then AutomaticEnv kicks in
	// for the env key TEST_EXPAND_OFF if it matches a field path — but here
	// the field path is server.grpc_addr, so the literal ${...} survives).
	require.Equal(t, "${TEST_EXPAND_OFF}", cfg.Server.GRPCAddr)
}
