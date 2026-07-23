package configx

import "github.com/servekit/go-common/xerr"

var (
	// ErrConfigNotFound is returned when no config file is found in any search path.
	ErrConfigNotFound = xerr.New("CONFIG_NOT_FOUND", xerr.CategoryInternal, 500, "config file not found")
	// ErrReadConfig is returned when the config file cannot be read or parsed.
	ErrReadConfig = xerr.New("READ_CONFIG_FAILED", xerr.CategoryInternal, 500, "failed to read config file")
	// ErrUnmarshal is returned when config values cannot be decoded into the target struct.
	ErrUnmarshal = xerr.New("UNMARSHAL_CONFIG_FAILED", xerr.CategoryInternal, 500, "failed to unmarshal config")
)
