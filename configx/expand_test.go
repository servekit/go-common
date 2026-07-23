package configx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandStrings_String(t *testing.T) {
	t.Setenv("EXPAND_TEST_HOST", "db.example.com")
	in := map[string]any{"host": "${EXPAND_TEST_HOST}"}
	expandStrings(in)
	require.Equal(t, "db.example.com", in["host"])
}

func TestExpandStrings_NestedMap(t *testing.T) {
	t.Setenv("EXPAND_TEST_PORT", "5432")
	in := map[string]any{
		"db": map[string]any{
			"port": "${EXPAND_TEST_PORT}",
		},
	}
	expandStrings(in)
	db := in["db"].(map[string]any)
	require.Equal(t, "5432", db["port"])
}

func TestExpandStrings_Slice(t *testing.T) {
	t.Setenv("EXPAND_TEST_H1", "a.com")
	in := map[string]any{
		"hosts": []any{"${EXPAND_TEST_H1}", "b.com"},
	}
	expandStrings(in)
	hosts := in["hosts"].([]any)
	require.Equal(t, "a.com", hosts[0])
	require.Equal(t, "b.com", hosts[1])
}

func TestExpandStrings_UnsetVariableBecomesEmpty(t *testing.T) {
	// os.ExpandEnv on unset var yields empty string (not the original ${VAR}).
	in := map[string]any{"k": "${DEFINITELY_UNSET_VAR_XYZ}"}
	expandStrings(in)
	require.Equal(t, "", in["k"])
}

func TestExpandStrings_NonStringUntouched(t *testing.T) {
	in := map[string]any{
		"port":   5432,
		"flag":   true,
		"nested": map[string]any{"n": 42},
		"arr":    []any{1, 2, 3},
	}
	expandStrings(in)
	require.Equal(t, 5432, in["port"])
	require.Equal(t, true, in["flag"])
	require.Equal(t, 42, in["nested"].(map[string]any)["n"])
	require.Equal(t, []any{1, 2, 3}, in["arr"])
}

func TestExpandStrings_StringWithoutVarUntouched(t *testing.T) {
	in := map[string]any{"k": "plain-value"}
	expandStrings(in)
	require.Equal(t, "plain-value", in["k"])
}

func TestExpandStrings_PartialExpansion(t *testing.T) {
	t.Setenv("EXPAND_TEST_USER", "admin")
	in := map[string]any{"dsn": "postgres://${EXPAND_TEST_USER}@db"}
	expandStrings(in)
	require.Equal(t, "postgres://admin@db", in["dsn"])
}
