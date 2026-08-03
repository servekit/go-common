// Package dbx provides GORM database initialization with slog-based logging
// across PostgreSQL, MySQL, and SQLite. A single Config selects the dialect
// via Driver, so callers switch databases by changing configuration only.
package dbx

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Driver selects which database dialect New connects to.
type Driver string

const (
	// DriverPostgres connects via gorm.io/driver/postgres.
	DriverPostgres Driver = "postgres"
	// DriverMySQL connects via gorm.io/driver/mysql.
	DriverMySQL Driver = "mysql"
	// DriverSQLite connects via github.com/glebarez/sqlite (pure Go, no CGO).
	DriverSQLite Driver = "sqlite"
)

// Config holds database connection parameters and pool settings for any
// supported dialect. Only the sub-config matching Driver is used; the others
// are ignored. The shared pool and GORM options apply to every dialect.
type Config struct {
	// Driver selects the dialect; defaults to postgres when empty.
	Driver Driver `default:"postgres"`

	// Dialect sub-configs (only the one matching Driver is read).
	Postgres *PostgresConfig
	MySQL    *MySQLConfig
	SQLite   *SQLiteConfig

	// Shared connection-pool settings.
	MaxOpenConns    int           `default:"50"`
	MaxIdleConns    int           `default:"10"`
	ConnMaxLifetime time.Duration `default:"30m"`

	// Shared GORM options.
	LogLevel      string        `default:"warn"`  // silent, error, warn, info
	SlowThreshold time.Duration `default:"200ms"` // slow query warning threshold
	SkipDefaultTx bool          `default:"true"`  // skip default transaction for single operations (recommended for production)
	DisableFK     bool          `default:"true"`  // disable foreign key constraints in migration
	TablePrefix   string        // prefix for all table names, e.g. "stor_"
}

// PostgresConfig holds PostgreSQL-specific connection parameters.
type PostgresConfig struct {
	Host     string `default:"localhost"`
	Port     int    `default:"5432"`
	User     string
	Password string
	DBName   string
	SSLMode  string `default:"disable"`
	// Schema sets the connection search_path (per-connection, pool-safe):
	// unqualified table names resolve against it. Comma-separated values are
	// supported, e.g. "app,public". Empty uses the server default ("public").
	Schema string
}

// MySQLConfig holds MySQL-specific connection parameters.
type MySQLConfig struct {
	Host     string `default:"localhost"`
	Port     int    `default:"3306"`
	User     string
	Password string
	DBName   string
	// Params are appended to the DSN on top of the defaults
	// (parseTime=true, loc=Local, charset=utf8mb4).
	Params map[string]string
}

// SQLiteConfig holds SQLite-specific connection parameters.
type SQLiteConfig struct {
	// Path is the database file path; empty means an in-memory database.
	// foreign_keys and busy_timeout pragmas are applied automatically.
	Path string
}

// New creates a GORM connection for the dialect selected by cfg.Driver, with
// pool settings and a slog logger applied uniformly across all dialects.
func New(cfg *Config) (*gorm.DB, error) {
	dialector, err := buildDialector(cfg)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, gormConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("dbx: open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("dbx: get underlying db: %w", err)
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
		return fmt.Errorf("dbx: auto migrate: %w", err)
	}
	slog.Info("auto migrate completed")
	return nil
}

const (
	// defaultSQLitePath is used when SQLiteConfig.Path is empty.
	defaultSQLitePath = ":memory:"
	// sqlitePragmas enables per-connection safety settings for SQLite:
	// foreign key enforcement and a busy timeout to avoid write-lock errors.
	sqlitePragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
)

func buildDialector(cfg *Config) (gorm.Dialector, error) {
	switch cfg.Driver {
	case DriverPostgres, "":
		return postgres.Open(postgresDSN(cfg.Postgres)), nil
	case DriverMySQL:
		return gormmysql.Open(mysqlDSN(cfg.MySQL)), nil
	case DriverSQLite:
		return sqlite.Open(sqliteDSN(cfg.SQLite)), nil
	default:
		return nil, fmt.Errorf("dbx: unsupported driver %q (want postgres, mysql, or sqlite)", cfg.Driver)
	}
}

func gormConfig(cfg *Config) *gorm.Config {
	return &gorm.Config{
		SkipDefaultTransaction:                   cfg.SkipDefaultTx,
		DisableForeignKeyConstraintWhenMigrating: cfg.DisableFK,
		Logger:                                   newSlogLogger(parseLogLevel(cfg.LogLevel), cfg.SlowThreshold),
		NamingStrategy: &schema.NamingStrategy{
			TablePrefix: cfg.TablePrefix,
		},
	}
}

func postgresDSN(cfg *PostgresConfig) string {
	if cfg == nil {
		cfg = &PostgresConfig{}
	}
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, sslMode,
	)
	if cfg.Schema != "" {
		dsn += " search_path=" + cfg.Schema
	}
	return dsn
}

func mysqlDSN(cfg *MySQLConfig) string {
	if cfg == nil {
		cfg = &MySQLConfig{}
	}
	params := map[string]string{"charset": "utf8mb4"}
	for k, v := range cfg.Params {
		params[k] = v
	}
	mc := mysql.Config{
		User:      cfg.User,
		Passwd:    cfg.Password,
		Net:       "tcp",
		Addr:      net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		DBName:    cfg.DBName,
		ParseTime: true,
		Loc:       time.Local,
		Params:    params,
	}
	return mc.FormatDSN()
}

func sqliteDSN(cfg *SQLiteConfig) string {
	path := defaultSQLitePath
	if cfg != nil && cfg.Path != "" {
		path = cfg.Path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + sqlitePragmas
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
