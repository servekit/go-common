package dbx

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/servekit/go-common/lifecycle"
)

func TestConnect_InjectedReturnsAsIs(t *testing.T) {
	mgr := lifecycle.NewManager()
	injected := openTestDB(t)

	db, err := Connect(nil, injected, mgr)

	require.NoError(t, err)
	require.Same(t, injected, db)
	require.NoError(t, mgr.Stop())
	// The caller owns the injected pool: mgr.Stop must not have closed it.
	require.NoError(t, injected.WithContext(t.Context()).Exec("select 1").Error)
}

func TestConnect_NilCfgWithoutInjectionFails(t *testing.T) {
	_, err := Connect(nil, nil, lifecycle.NewManager())

	require.ErrorContains(t, err, "config required")
}

func TestConnect_BuildsAndRegistersStopper(t *testing.T) {
	mgr := lifecycle.NewManager()

	db, err := Connect(&Config{Driver: DriverSQLite}, nil, mgr)

	require.NoError(t, err)
	require.NoError(t, db.WithContext(t.Context()).Exec("select 1").Error)
	require.NoError(t, mgr.Stop())
	// mgr.Stop closed the self-built pool: further use must fail.
	require.Error(t, db.WithContext(t.Context()).Exec("select 1").Error)
}

// openTestDB returns a throwaway in-memory sqlite pool, bypassing New so the
// injected-path tests exercise Connect's policy, not dialector wiring.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}
