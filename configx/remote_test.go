package configx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type remoteInner struct {
	Addr string `default:"localhost:1"`
}

type remoteCfg struct {
	ThirdParty struct {
		GID *RemoteServiceConfig[*remoteInner]
	} `yaml:"third_party"`
}

// TestRemoteServiceConfig_DecodesFromYAML proves the Mode field (a named
// string kind) decodes from YAML through the standard configx load path,
// including the empty-value-=-module default.
func TestRemoteServiceConfig_DecodesFromYAML(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
third_party:
  gid:
    mode: grpc
    target: "localhost:19091"
`)
	chdir(t, dir)

	var cfg remoteCfg
	require.NoError(t, Load(&cfg))
	require.Equal(t, ModeGRPC, cfg.ThirdParty.GID.Mode)
	require.Equal(t, "localhost:19091", cfg.ThirdParty.GID.Target)
}

func TestRemoteServiceConfig_EmptyModeIsUnspecified(t *testing.T) {
	dir := tempDir(t)
	writeTempConfig(t, dir, "config.yaml", `
third_party:
  gid:
    target: x
`)
	chdir(t, dir)

	var cfg remoteCfg
	require.NoError(t, Load(&cfg))
	require.Equal(t, ModeUnspecified, cfg.ThirdParty.GID.Mode)
	require.Equal(t, "localhost:1", cfg.ThirdParty.GID.Config.Addr, "nested defaults still apply")
}

func TestMode_String(t *testing.T) {
	require.Equal(t, "grpc", ModeGRPC.String())
	require.Equal(t, "module", ModeModule.String())
	require.Equal(t, "", ModeUnspecified.String())
}
