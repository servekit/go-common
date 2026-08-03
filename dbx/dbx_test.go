package dbx

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// --- Test models ---

type amUser struct {
	ID    uint `gorm:"primarykey"`
	Name  string
	Email string
}

type amUserProfile struct {
	ID     uint `gorm:"primarykey"`
	UserID uint
	Bio    string
}

type amProduct struct {
	ID    uint `gorm:"primarykey"`
	Name  string
	Price float64
}

type amLegacyOrder struct {
	ID     uint `gorm:"primarykey"`
	Amount float64
}

func (amLegacyOrder) TableName() string {
	return "orders"
}

type amTag struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

type amCategory struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

type amDetailItem struct {
	ID   uint   `gorm:"primarykey"`
	Name string `gorm:"type:varchar(100);not null"`
}

// --- Helpers ---

// reopenWithPrefix returns a *gorm.DB with the given table prefix that shares
// the backing database of base (Postgres/MySQL) or opens a fresh SQLite file.
func reopenWithPrefix(t *testing.T, base *gorm.DB, drv Driver, prefix string) *gorm.DB {
	t.Helper()
	cfg := &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy:                           &schema.NamingStrategy{TablePrefix: prefix},
	}
	if drv == DriverSQLite {
		// A fresh file (not :memory:) keeps each prefixed session isolated.
		path := filepath.Join(t.TempDir(), "test.db")
		db, err := gorm.Open(sqlite.Open(path), cfg)
		require.NoError(t, err)
		return db
	}

	sqlDB, err := base.DB()
	require.NoError(t, err)

	var dialector gorm.Dialector
	switch drv {
	case DriverPostgres:
		dialector = pgdriver.New(pgdriver.Config{Conn: sqlDB})
	case DriverMySQL:
		dialector = gormmysql.New(gormmysql.Config{Conn: sqlDB})
	}

	db, err := gorm.Open(dialector, cfg)
	require.NoError(t, err)
	return db
}

func tableExists(t *testing.T, db *gorm.DB, drv Driver, name string) bool {
	t.Helper()
	var query string
	switch drv {
	case DriverSQLite:
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	case DriverMySQL:
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
	default: // DriverPostgres
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?"
	}
	var count int64
	require.NoError(t, db.Raw(query, name).Row().Scan(&count), "check table %q existence", name)
	return count > 0
}

func mustTableExist(t *testing.T, db *gorm.DB, drv Driver, name string) {
	t.Helper()
	if !tableExists(t, db, drv, name) {
		t.Errorf("expected table %q to exist", name)
	}
}

func mustTableNotExist(t *testing.T, db *gorm.DB, drv Driver, name string) {
	t.Helper()
	if tableExists(t, db, drv, name) {
		t.Errorf("expected table %q to NOT exist", name)
	}
}

func columnNotNull(t *testing.T, db *gorm.DB, drv Driver, table, column string) bool {
	t.Helper()
	switch drv {
	case DriverSQLite:
		var notnull int
		require.NoError(t,
			db.Raw(`SELECT "notnull" FROM pragma_table_info(?) WHERE name = ?`, table, column).Row().Scan(&notnull),
			"check nullable for %q.%q", table, column)
		return notnull == 1
	case DriverMySQL:
		var isNullable string
		require.NoError(t,
			db.Raw("SELECT is_nullable FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?", table, column).Row().Scan(&isNullable),
			"check nullable for %q.%q", table, column)
		return isNullable == "NO"
	default: // DriverPostgres
		var isNullable string
		require.NoError(t,
			db.Raw("SELECT is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?", table, column).Row().Scan(&isNullable),
			"check nullable for %q.%q", table, column)
		return isNullable == "NO"
	}
}

// --- Tests ---

func TestAutoMigrate(t *testing.T) {
	for _, drv := range []Driver{DriverSQLite, DriverPostgres, DriverMySQL} {
		drv := drv
		t.Run(string(drv), func(t *testing.T) {
			base := SetupTestDB(t, drv)
			runAutoMigrateCases(t, drv, base)
		})
	}
}

func runAutoMigrateCases(t *testing.T, drv Driver, base *gorm.DB) {
	t.Run("basic pluralization", func(t *testing.T) {
		require.NoError(t, AutoMigrate(base, &amUser{}))
		mustTableExist(t, base, drv, "am_users")
		mustTableNotExist(t, base, drv, "am_user")
	})

	t.Run("snake_case composite name", func(t *testing.T) {
		require.NoError(t, AutoMigrate(base, &amUserProfile{}))
		mustTableExist(t, base, drv, "am_user_profiles")
	})

	t.Run("table prefix applied with pluralization", func(t *testing.T) {
		prefixed := reopenWithPrefix(t, base, drv, "app_")
		require.NoError(t, AutoMigrate(prefixed, &amProduct{}))
		mustTableExist(t, prefixed, drv, "app_am_products")
		mustTableNotExist(t, prefixed, drv, "am_products")
	})

	t.Run("custom TableName overrides convention", func(t *testing.T) {
		require.NoError(t, AutoMigrate(base, &amLegacyOrder{}))
		mustTableExist(t, base, drv, "orders")
		mustTableNotExist(t, base, drv, "am_legacy_orders")
	})

	t.Run("custom TableName combined with prefix", func(t *testing.T) {
		// GORM does not prepend TablePrefix to a model's custom TableName():
		// the custom name wins, so the table stays "orders" (not "svc_orders").
		prefixed := reopenWithPrefix(t, base, drv, "svc_")
		require.NoError(t, AutoMigrate(prefixed, &amLegacyOrder{}))
		mustTableExist(t, prefixed, drv, "orders")
		mustTableNotExist(t, prefixed, drv, "svc_orders")
	})

	t.Run("multiple models in one call", func(t *testing.T) {
		require.NoError(t, AutoMigrate(base, &amTag{}, &amCategory{}))
		mustTableExist(t, base, drv, "am_tags")
		mustTableExist(t, base, drv, "am_categories")
	})

	t.Run("idempotent", func(t *testing.T) {
		// am_users already created in "basic pluralization"; migrating again must succeed.
		require.NoError(t, AutoMigrate(base, &amUser{}))
		require.NoError(t, AutoMigrate(base, &amUser{}))
	})

	t.Run("column nullability", func(t *testing.T) {
		require.NoError(t, AutoMigrate(base, &amDetailItem{}))
		mustTableExist(t, base, drv, "am_detail_items")
		if !columnNotNull(t, base, drv, "am_detail_items", "name") {
			t.Errorf("expected name column to be NOT NULL")
		}
	})
}

// TestPostgresSchema verifies that PostgresConfig.Schema sets the connection
// search_path so dbx.New routes unqualified tables to a custom schema.
func TestPostgresSchema(t *testing.T) {
	ctx := context.Background()
	c := startPostgresContainer(t)

	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	db, err := New(&Config{
		Driver: DriverPostgres,
		Postgres: &PostgresConfig{
			Host:     host,
			Port:     int(port.Num()),
			User:     "test",
			Password: "test",
			DBName:   "test_db",
			SSLMode:  "disable",
			Schema:   "app",
		},
		DisableFK: true,
	})
	require.NoError(t, err)

	// The target schema must exist before AutoMigrate creates tables in it.
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS app").Error)

	require.NoError(t, AutoMigrate(db, &amUser{}))
	require.True(t, tableExistsInSchema(t, db, "app", "am_users"),
		"expected am_users in schema app (search_path)")
	require.False(t, tableExistsInSchema(t, db, "public", "am_users"),
		"expected am_users NOT in schema public")
}

func tableExistsInSchema(t *testing.T, db *gorm.DB, schema, table string) bool {
	t.Helper()
	var count int64
	require.NoError(t,
		db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?", schema, table).Row().Scan(&count),
		"check table %q.%q existence", schema, table)
	return count > 0
}
