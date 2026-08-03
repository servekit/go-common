package dbx

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormmysql "gorm.io/driver/mysql"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// SetupTestDB returns a *gorm.DB backed by a fresh database for the given
// dialect, suitable for integration tests. SQLite uses a per-test temp file
// (no Docker); Postgres and MySQL start a testcontainer that is terminated
// when the test finishes.
func SetupTestDB(t *testing.T, driver Driver) *gorm.DB {
	t.Helper()
	switch driver {
	case DriverSQLite:
		return setupSQLiteTestDB(t)
	case DriverPostgres:
		return setupPostgresTestDB(t)
	case DriverMySQL:
		return setupMySQLTestDB(t)
	default:
		t.Fatalf("dbx: unsupported test driver %q", driver)
		return nil
	}
}

func setupSQLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// A temp file (rather than :memory:) sidesteps the SQLite-in-pool gotcha
	// where each pooled connection would otherwise get its own database.
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite test database: %v", err)
	}
	return db
}

func startPostgresContainer(t *testing.T) *postgres.PostgresContainer {
	t.Helper()
	ctx := context.Background()

	c, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("test_db"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Terminate(ctx); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	return c
}

func setupPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	c := startPostgresContainer(t)

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	db, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	return db
}

func setupMySQLTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()

	c, err := tcmysql.Run(ctx,
		"mysql:8.0.36",
		tcmysql.WithDatabase("test_db"),
		tcmysql.WithUsername("test"),
		tcmysql.WithPassword("test"),
	)
	if err != nil {
		t.Fatalf("start mysql container: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Terminate(ctx); err != nil {
			t.Logf("terminate mysql container: %v", err)
		}
	})

	// Rely on the mysql module's default wait strategy; it is more reliable
	// than a custom ForLog("ready for connections") override, which returns
	// just before the server fully accepts connections.
	dsn, err := c.ConnectionString(ctx, "parseTime=true", "loc=Local")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	return db
}
