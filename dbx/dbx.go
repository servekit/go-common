// Package dbx provides GORM database initialization with slog-based logging.
package dbx

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Config holds PostgreSQL connection parameters and pool settings.
type Config struct {
	Host            string `default:"localhost"`
	Port            int    `default:"5432"`
	User            string
	Password        string
	DBName          string
	SSLMode         string        `default:"disable"`
	MaxOpenConns    int           `default:"50"`
	MaxIdleConns    int           `default:"10"`
	ConnMaxLifetime time.Duration `default:"30m"`

	// GORM options
	LogLevel      string        `default:"warn"`  // silent, error, warn, info
	SlowThreshold time.Duration `default:"200ms"` // slow query warning threshold
	SkipDefaultTx bool          `default:"true"`  // skip default transaction for single operations (recommended for production)
	DisableFK     bool          `default:"true"`  // disable foreign key constraints in migration
	TablePrefix   string        // prefix for all table names, e.g. "stor_"
}

// New creates a new GORM database connection with pool settings and slog logger.
func New(cfg *Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)

	logLevel := parseLogLevel(cfg.LogLevel)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		SkipDefaultTransaction:                   cfg.SkipDefaultTx,
		DisableForeignKeyConstraintWhenMigrating: cfg.DisableFK,
		Logger:                                   newSlogLogger(logLevel, cfg.SlowThreshold),
		NamingStrategy: &schema.NamingStrategy{
			TablePrefix: cfg.TablePrefix,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying db: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	return db, nil
}

// AutoMigrate runs GORM AutoMigrate for the given models with logging.
func AutoMigrate(db *gorm.DB, models ...any) error {
	slog.Info("running auto migrate", "models", len(models))
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	slog.Info("auto migrate completed")
	return nil
}

func parseLogLevel(s string) gormLogLevel {
	switch s {
	case "silent":
		return gormLogLevelSilent
	case "error":
		return gormLogLevelError
	case "warn":
		return gormLogLevelWarn
	case "info":
		return gormLogLevelInfo
	default:
		return gormLogLevelWarn
	}
}
