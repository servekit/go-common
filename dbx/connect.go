package dbx

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/servekit/go-common/lifecycle"
)

// Connect resolves the process database with the platform's inject-or-build
// contract: an injected pool is returned as-is — the caller owns its
// lifecycle and nothing is registered with mgr; otherwise a pool is built
// from cfg and a Stopper is registered so mgr.Stop closes it.
func Connect(cfg *Config, injected *gorm.DB, mgr *lifecycle.Manager) (*gorm.DB, error) {
	if injected != nil {
		return injected, nil
	}
	if cfg == nil {
		return nil, fmt.Errorf("dbx: config required when no pool injected")
	}
	db, err := New(cfg)
	if err != nil {
		return nil, err
	}
	mgr.AddStopper("db", lifecycle.StopFunc(func() {
		sqlDB, err := db.DB()
		if err != nil || sqlDB == nil {
			slog.Warn("dbx: get sql db for close", "error", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			slog.Warn("dbx: close db", "error", err)
		}
	}))
	return db, nil
}
