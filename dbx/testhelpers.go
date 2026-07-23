package dbx

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// SetupTestDB starts a PostgreSQL testcontainer and returns a *gorm.DB.
// The container is automatically terminated when the test finishes.
func SetupTestDB(t *testing.T) *gorm.DB {
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
	t.Cleanup(func() { _ = c.Terminate(ctx) })

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
