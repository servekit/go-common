package configx

import "github.com/go-viper/mapstructure/v2"

// LoadOption configures the config loading behavior.
type LoadOption func(*loader)

// WithServiceName sets the service name.
// This enables <NAME>_CONFIG env var lookup and adds /etc/<name> to search paths.
func WithServiceName(name string) LoadOption {
	return func(l *loader) {
		l.serviceName = name
	}
}

// WithEnvPrefix sets the viper env prefix for AutomaticEnv (e.g. "MY_SERVICE").
func WithEnvPrefix(prefix string) LoadOption {
	return func(l *loader) {
		l.envPrefix = prefix
	}
}

// WithConfigName sets the config file name without extension (default: "config").
func WithConfigName(name string) LoadOption {
	return func(l *loader) {
		l.configName = name
	}
}

// WithConfigPaths sets the directories to search for config files.
func WithConfigPaths(paths ...string) LoadOption {
	return func(l *loader) {
		l.configPaths = paths
	}
}

// WithDecodeHooks appends custom mapstructure decode hooks after the built-in ones.
func WithDecodeHooks(hooks ...mapstructure.DecodeHookFunc) LoadOption {
	return func(l *loader) {
		l.decodeHooks = append(l.decodeHooks, hooks...)
	}
}

// WithExpandEnv enables ${VAR} expansion in config file values.
// After ReadInConfig, all string values in viper.AllSettings() are passed
// through os.ExpandEnv. Useful for env-driven deployments where the YAML
// references environment variables by name (e.g. host: ${DB_HOST}).
//
// Unset variables expand to empty string (os.ExpandEnv semantics).
// Non-string values are not touched.
func WithExpandEnv() LoadOption {
	return func(l *loader) {
		l.expandEnv = true
	}
}
